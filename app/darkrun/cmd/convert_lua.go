package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/darkspinnet/darkspin/content/lua51"
	"github.com/darkspinnet/darkspin/content/render/ds"
	"github.com/spf13/cobra"
)

// Selected package entries use this route only when the requested output is Lua.
// Other entries retain the existing DSE conversion behavior.
func convertLuaTarget(command *cobra.Command, target packageTarget, args []string) (bool, error) {
	if target.Selector != "" {
		if len(args) != 2 || !strings.EqualFold(filepath.Ext(args[1]), ".lua") {
			return false, nil
		}
		payload, err := readLuaPackageTarget(command.Context(), target)
		if err != nil {
			return true, fmt.Errorf("entryRead: %w", err)
		}
		err = writeLuaConversion(command, target.Path, args[1], payload, true)
		if err != nil {
			return true, fmt.Errorf("entryConvert: %w", err)
		}
		return true, nil
	}
	fi, err := os.Stat(target.Path)
	if err != nil {
		return false, fmt.Errorf("sourceStat: %w", err)
	}
	if fi.IsDir() {
		return false, nil
	}
	r, err := os.Open(target.Path)
	if err != nil {
		return false, fmt.Errorf("sourceOpen: %w", err)
	}
	var header [4]byte
	count, readErr := io.ReadFull(r, header[:])
	closeErr := r.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return false, fmt.Errorf("headerRead: %w", readErr)
	}
	if closeErr != nil {
		return false, fmt.Errorf("sourceClose: %w", closeErr)
	}
	isBytecode := count == len(header) && string(header[:]) == "\x1bLua"
	extension := strings.ToLower(filepath.Ext(target.Path))
	if !isBytecode && extension != ".lua" && extension != ".luac" {
		return false, nil
	}
	if extension == ".luac" && !isBytecode {
		return true, errors.New("expected native Lua 5.1 bytecode in .luac input")
	}
	payload, err := os.ReadFile(target.Path)
	if err != nil {
		return true, fmt.Errorf("sourceRead: %w", err)
	}
	destinationPath := ""
	if len(args) == 2 && filepath.Clean(args[1]) != "." {
		destinationPath = args[1]
	} else {
		stem := strings.TrimSuffix(filepath.Base(target.Path), filepath.Ext(target.Path))
		destinationPath = stem + ".luac"
		if isBytecode {
			destinationPath = stem + ".lua"
			if extension == ".lua" {
				destinationPath = stem + ".decompiled.lua"
			}
		}
	}
	err = writeLuaConversion(command, target.Path, destinationPath, payload, false)
	if err != nil {
		return true, fmt.Errorf("fileConvert: %w", err)
	}
	return true, nil
}

func writeLuaRecoverySummary(command *cobra.Command, destinationPath string) error {
	fi, err := os.Stat(destinationPath)
	if err != nil {
		return fmt.Errorf("destinationStat: %w", err)
	}
	if !fi.IsDir() {
		if !strings.EqualFold(filepath.Ext(destinationPath), ".dse") {
			return nil
		}
		destinationPath = strings.TrimSuffix(destinationPath, filepath.Ext(destinationPath)) + ".lua.txt"
		_, err = os.Stat(destinationPath)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("diagnosticStat: %w", err)
		}
	}
	count := 0
	err = filepath.WalkDir(destinationPath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("diagnosticWalk: %w", walkErr)
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".lua.txt") {
			count++
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("summaryWalk: %w", err)
	}
	if count == 0 {
		return nil
	}
	_, err = fmt.Fprintf(command.ErrOrStderr(), "Lua source recovery unavailable for %d resource(s); supply their missing .lua sources before rebuilding the package. See the .lua.txt diagnostics.\n", count)
	if err != nil {
		return fmt.Errorf("summaryWrite: %w", err)
	}
	return nil
}

func writeLuaConversion(command *cobra.Command, sourcePath, destinationPath string, payload []byte, isSourceOutput bool) error {
	_, err := os.Lstat(destinationPath)
	if err == nil {
		return fmt.Errorf("destinationExists: %q", destinationPath)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("destinationStat: %w", err)
	}
	var converted []byte
	if bytes.HasPrefix(payload, []byte("\x1bLua")) {
		converted, err = lua51.Decompile(command.Context(), payload)
	} else {
		converted, err = lua51.CompileContext(command.Context(), payload, "@"+filepath.Base(sourcePath))
		if isSourceOutput && err == nil {
			converted = payload
		}
	}
	if err != nil {
		return fmt.Errorf("luaConvert: %w", err)
	}
	err = command.Context().Err()
	if err != nil {
		return fmt.Errorf("convertContext: %w", err)
	}
	w, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("destinationCreate: %w", err)
	}
	_, writeErr := w.Write(converted)
	closeErr := w.Close()
	if writeErr != nil || closeErr != nil {
		removeErr := os.Remove(destinationPath)
		return fmt.Errorf("destinationWrite: %w", errors.Join(writeErr, closeErr, removeErr))
	}
	_, err = fmt.Fprintf(command.OutOrStdout(), "Converted Lua: %s\n", destinationPath)
	if err != nil {
		return fmt.Errorf("outputWrite: %w", err)
	}
	return nil
}

func readLuaPackageTarget(ctx context.Context, target packageTarget) (payload []byte, resultErr error) {
	fi, err := os.Stat(target.Path)
	if err != nil {
		return nil, fmt.Errorf("sourceStat: %w", err)
	}
	if fi.IsDir() {
		document, readErr := ds.ReadPathContext(ctx, target.Path)
		if readErr != nil {
			return nil, fmt.Errorf("directoryRead: %w", readErr)
		}
		entries := make([]dbpf.Entry, len(document.Manifest.Resources))
		for ordinal, resource := range document.Manifest.Resources {
			entries[ordinal] = resource.Entry
		}
		ordinal, resolveErr := resolvePackageOrdinal(entries, target.Selector)
		if resolveErr != nil {
			return nil, fmt.Errorf("directorySelect: %w", resolveErr)
		}
		if entries[ordinal].Type != 0x3681d755 {
			return nil, errors.New("selected entry is not a Lua resource")
		}
		payload, entry, resourceErr := document.StoredResourceContext(ctx, target.Path, ordinal)
		if resourceErr != nil {
			return nil, fmt.Errorf("directoryResource: %w", resourceErr)
		}
		if entry.Type != entries[ordinal].Type {
			return nil, errors.New("selected resource type changed")
		}
		return payload, nil
	}
	r, err := os.Open(target.Path)
	if err != nil {
		return nil, fmt.Errorf("packageOpen: %w", err)
	}
	defer func() {
		closeErr := r.Close()
		if closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("packageClose: %w", closeErr))
		}
	}()
	fi, err = r.Stat()
	if err != nil {
		return nil, fmt.Errorf("packageStat: %w", err)
	}
	pkg, err := dbpf.NewReader(r, fi.Size())
	if err != nil {
		return nil, fmt.Errorf("packageRead: %w", err)
	}
	ordinal, err := resolvePackageOrdinal(pkg.Entries, target.Selector)
	if err != nil {
		return nil, fmt.Errorf("entryResolve: %w", err)
	}
	if pkg.Entries[ordinal].Type != 0x3681d755 {
		return nil, errors.New("selected entry is not a Lua resource")
	}
	reader, err := pkg.Open(pkg.Entries[ordinal])
	if err != nil {
		return nil, fmt.Errorf("entryOpen: %w", err)
	}
	payload, err = io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("payloadRead: %w", err)
	}
	return payload, nil
}
