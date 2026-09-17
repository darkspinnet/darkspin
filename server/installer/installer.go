// Package installer prepares runtime assets from a Game installation.
package installer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

type Tools struct{ DBPFUnpacker, Unluac, RecapParser string }
type Options struct {
	InstallPath, WorkingDirectory, Version string
	Tools                                  Tools
	Log                                    io.Writer
}

type Installer struct {
	mu        sync.RWMutex
	isRunning bool
	label     string
}

func (i *Installer) IsRunning() bool {
	i.mu.RLock()
	value := i.isRunning
	i.mu.RUnlock()
	return value
}
func (i *Installer) Label() string { i.mu.RLock(); value := i.label; i.mu.RUnlock(); return value }
func (i *Installer) setStatus(isRunning bool, label string) {
	i.mu.Lock()
	i.isRunning = isRunning
	i.label = label
	i.mu.Unlock()
}

func (i *Installer) Prepare(ctx context.Context, options Options) error {
	working, err := filepath.Abs(options.WorkingDirectory)
	if err != nil {
		return fmt.Errorf("workingPath: %w", err)
	}
	install, err := filepath.Abs(options.InstallPath)
	if err != nil {
		return fmt.Errorf("installPath: %w", err)
	}
	versionPath := filepath.Join(working, "assets", "data", "version_bin.txt")
	_, err = os.Stat(versionPath)
	if err == nil {
		i.setStatus(false, "Server files are ready.")
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("versionStat: %w", err)
	}
	i.setStatus(true, "Preparing server files...")
	defer i.setStatus(false, "Server data preparation stopped.")
	tools := defaultTools(options.Tools)
	serverData := filepath.Join(working, "ServerData")
	assetData := filepath.Join(working, "AssetData_Binary")
	finalData := filepath.Join(working, "ServerData_final")

	err = run(ctx, options.Log, tools.DBPFUnpacker, filepath.Join(install, "Data", "ServerData.package"), serverData)
	if err != nil {
		return fmt.Errorf("serverUnpack: %w", err)
	}
	err = run(ctx, options.Log, tools.DBPFUnpacker, filepath.Join(install, "Data", "AssetData_Binary.package"), assetData)
	if err != nil {
		return fmt.Errorf("assetsUnpack: %w", err)
	}
	err = filepath.WalkDir(serverData, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("serverWalk[%q]: %w", path, walkErr)
		}
		if entry.IsDir() || filepath.Ext(path) != ".lua" {
			return nil
		}
		relative, relativeErr := filepath.Rel(serverData, path)
		if relativeErr != nil {
			return fmt.Errorf("luaRelative: %w", relativeErr)
		}
		output := filepath.Join(finalData, relative)
		mkdirErr := os.MkdirAll(filepath.Dir(output), 0o755)
		if mkdirErr != nil {
			return fmt.Errorf("luaMkdir: %w", mkdirErr)
		}
		file, createErr := os.Create(output)
		if createErr != nil {
			return fmt.Errorf("luaCreate[%q]: %w", output, createErr)
		}
		command := exec.CommandContext(ctx, tools.Unluac, path)
		command.Stdout = file
		command.Stderr = options.Log
		commandErr := command.Run()
		closeErr := file.Close()
		if commandErr != nil {
			return fmt.Errorf("luaDecompile[%q]: %w", path, commandErr)
		}
		if closeErr != nil {
			return fmt.Errorf("luaClose[%q]: %w", output, closeErr)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("luaWalk: %w", err)
	}
	serverDataOutput := filepath.Join(working, "assets", "data")
	err = run(ctx, options.Log, tools.RecapParser, "--recursive", "--silent", "--sort-ext", "--xml", "-o", serverDataOutput, assetData)
	if err != nil {
		return fmt.Errorf("assetsParse: %w", err)
	}
	err = MergeDirectories(filepath.Join(finalData, "lua"), filepath.Join(serverDataOutput, "lua"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("luaMerge: %w", err)
	}
	err = MergeDirectories(filepath.Join(finalData, "Abilities"), filepath.Join(serverDataOutput, "Abilities"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("abilityMerge: %w", err)
	}
	err = verifyTemporaryPath(working, assetData, serverData, finalData)
	if err != nil {
		return fmt.Errorf("tempValidate: %w", err)
	}
	err = os.RemoveAll(assetData)
	if err != nil {
		return fmt.Errorf("assetsRemove: %w", err)
	}
	err = os.RemoveAll(serverData)
	if err != nil {
		return fmt.Errorf("serverRemove: %w", err)
	}
	err = os.RemoveAll(finalData)
	if err != nil {
		return fmt.Errorf("finalRemove: %w", err)
	}
	err = os.MkdirAll(filepath.Dir(versionPath), 0o755)
	if err != nil {
		return fmt.Errorf("versionMkdir: %w", err)
	}
	err = os.WriteFile(versionPath, []byte(options.Version+"\n"), 0o644)
	if err != nil {
		return fmt.Errorf("versionWrite: %w", err)
	}
	i.setStatus(false, "Server files are ready.")
	return nil
}

func MergeDirectories(source, destination string) error {
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("mergeWalk[%q]: %w", path, err)
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return fmt.Errorf("mergeRelative: %w", err)
		}
		target := filepath.Join(destination, relative)
		_, err = os.Stat(target)
		if err == nil {
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("targetStat[%q]: %w", target, err)
		}
		err = os.MkdirAll(filepath.Dir(target), 0o755)
		if err != nil {
			return fmt.Errorf("targetMkdir: %w", err)
		}
		input, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("sourceOpen[%q]: %w", path, err)
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			_ = input.Close()
			return fmt.Errorf("targetCreate[%q]: %w", target, err)
		}
		_, copyErr := io.Copy(output, input)
		closeOutputErr := output.Close()
		closeInputErr := input.Close()
		if copyErr != nil {
			return fmt.Errorf("fileCopy[%q>%q]: %w", path, target, copyErr)
		}
		if closeOutputErr != nil {
			return fmt.Errorf("targetClose[%q]: %w", target, closeOutputErr)
		}
		if closeInputErr != nil {
			return fmt.Errorf("sourceClose[%q]: %w", path, closeInputErr)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("directoryMerge[%q>%q]: %w", source, destination, err)
	}
	return nil
}

func run(ctx context.Context, log io.Writer, executable string, arguments ...string) error {
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Stdout = log
	command.Stderr = log
	err := command.Run()
	if err != nil {
		return fmt.Errorf("toolRun[%s]: %w", filepath.Base(executable), err)
	}
	return nil
}

func defaultTools(tools Tools) Tools {
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	if tools.DBPFUnpacker == "" {
		tools.DBPFUnpacker = "dbpf_unpacker" + suffix
	}
	if tools.Unluac == "" {
		tools.Unluac = "unluac" + suffix
	}
	if tools.RecapParser == "" {
		tools.RecapParser = "recap_parser" + suffix
	}
	return tools
}

func verifyTemporaryPath(root string, paths ...string) error {
	for _, path := range paths {
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("tempRelative[%q]: %w", path, err)
		}
		if relative == "." || relative == ".." || len(relative) >= 3 && relative[:3] == ".."+string(os.PathSeparator) {
			return fmt.Errorf("temporary path escapes working directory: %s", path)
		}
	}
	return nil
}
