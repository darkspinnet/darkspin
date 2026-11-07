package ds

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/darkspinnet/darkspin/content/lua51"
	"golang.org/x/sync/errgroup"
)

// The resource identity is shared with the ServerData importer.
const luaResourceType uint32 = 0x3681d755

func writePackageLuaResources(ctx context.Context, pkg *dbpf.Reader, destinationPath string, resources []dbpf.Resource) error {
	group, workerContext := errgroup.WithContext(ctx)
	// Bound guest memory and CPU while sharing the compiled WebAssembly module.
	group.SetLimit(4)
	for ordinal, resource := range resources {
		if resource.Entry.Type != luaResourceType {
			continue
		}
		if workerContext.Err() != nil {
			break
		}
		group.Go(func() error {
			err := writePackageLuaResource(workerContext, pkg, destinationPath, resource)
			if err != nil {
				return fmt.Errorf("luaResource[%d]: %w", ordinal, err)
			}
			return nil
		})
	}
	err := group.Wait()
	if err != nil {
		return fmt.Errorf("luaExport: %w", err)
	}
	err = ctx.Err()
	if err != nil {
		return fmt.Errorf("luaContext: %w", err)
	}
	return nil
}

func writePackageLuaResource(ctx context.Context, pkg *dbpf.Reader, destinationPath string, resource dbpf.Resource) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("resourceContext: %w", err)
	}
	definitionPath, err := safePath(destinationPath, resource.PayloadPath)
	if err != nil {
		return fmt.Errorf("resourcePath: %w", err)
	}
	err = os.MkdirAll(filepath.Dir(definitionPath), 0o755)
	if err != nil {
		return fmt.Errorf("resourceMkdir: %w", err)
	}
	r, err := pkg.Open(resource.Entry)
	if err != nil {
		return fmt.Errorf("resourceOpen: %w", err)
	}
	payload, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("payloadRead: %w", err)
	}
	err = writeLuaResource(ctx, definitionPath, resourceDefinitionIdentity(resource.PayloadPath), payload)
	if err != nil {
		return fmt.Errorf("resourceWrite: %w", err)
	}
	return nil
}

// Lua resources retain editable source and ABI metadata, never original bytes.
type luaResourceDefinition struct {
	format     string
	source     string
	sourceName string
	diagnostic string
	header     []byte
}

func writeLuaResource(ctx context.Context, destinationPath, identity string, payload []byte) error {
	definition := luaResourceDefinition{format: "source", sourceName: "@" + identity,
		header: []byte{0x1b, 'L', 'u', 'a', 0x51, 0, 1, 4, 4, 4, 4, 0}}
	stem := strings.TrimSuffix(filepath.Base(destinationPath), filepath.Ext(destinationPath))
	definition.source = stem + ".lua"
	source := payload
	if bytes.HasPrefix(payload, []byte("\x1bLua")) {
		if len(payload) >= 12 && payload[4] == 0x51 && payload[5] == 0 {
			definition.header = append([]byte(nil), payload[:12]...)
		}
		chunk, inspectErr := lua51.Inspect(payload)
		if inspectErr == nil {
			definition.sourceName = chunk.SourceName
		}
		var err error
		source, err = lua51.Decompile(ctx, payload)
		if err != nil {
			contextErr := ctx.Err()
			if contextErr != nil {
				return fmt.Errorf("sourceContext: %w", contextErr)
			}
			definition.format = "disassembly"
			definition.diagnostic = stem + ".lua.txt"
			source, err = luaRecoveryDiagnostic(payload, err)
			if err != nil {
				return fmt.Errorf("sourceDiagnostic: %w", err)
			}
		}
	} else {
		compiled, err := lua51.CompileContext(ctx, source, definition.sourceName)
		if err != nil {
			return fmt.Errorf("sourceValidate: %w", err)
		}
		if len(compiled) == 0 {
			return errors.New("compiler returned empty bytecode")
		}
	}
	endian := "little"
	if definition.header[6] == 0 {
		endian = "big"
	}
	text := fmt.Sprintf("%sLUA %q\n	VERSION %d\n	FORMAT %s\n	SOURCE %q\n	SOURCENAME %q\n	ENDIAN %s\n	INTEGERSIZE %d\n	SIZETSIZE %d\n	NUMBERSIZE %d\n	ISINTEGRAL %d\n",
		resourceDSEHeader, identity, version, definition.format, definition.source, definition.sourceName,
		endian, definition.header[7], definition.header[8], definition.header[10], definition.header[11])
	sidecarName := definition.source
	if definition.diagnostic != "" {
		text += fmt.Sprintf("	DIAGNOSTIC %q\n", definition.diagnostic)
		sidecarName = definition.diagnostic
	}
	paths := []string{filepath.Join(filepath.Dir(destinationPath), sidecarName), destinationPath}
	payloads := [][]byte{source, []byte(text)}
	for _, path := range paths {
		_, err := os.Lstat(path)
		if err == nil {
			return fmt.Errorf("destinationExists: %q", path)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("destinationStat: %w", err)
		}
	}
	for index, path := range paths {
		err := writeLuaFile(path, payloads[index])
		if err == nil {
			continue
		}
		for _, writtenPath := range paths[:index] {
			removeErr := os.Remove(writtenPath)
			if removeErr != nil {
				err = errors.Join(err, fmt.Errorf("partialRemove: %w", removeErr))
			}
		}
		return fmt.Errorf("sidecarWrite: %w", err)
	}
	return nil
}

func writeLuaFile(path string, payload []byte) error {
	w, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("fileCreate: %w", err)
	}
	_, writeErr := w.Write(payload)
	closeErr := w.Close()
	if writeErr != nil || closeErr != nil {
		removeErr := os.Remove(path)
		return fmt.Errorf("fileWrite: %w", errors.Join(writeErr, closeErr, removeErr))
	}
	return nil
}

func readLuaResource(ctx context.Context, sourcePath, identity string) ([]byte, error) {
	err := ctx.Err()
	if err != nil {
		return nil, fmt.Errorf("readContext: %w", err)
	}
	isLua, err := hasRenderDefinition(sourcePath, "LUA")
	if err != nil {
		return nil, fmt.Errorf("definitionCheck: %w", err)
	}
	if !isLua {
		payload, readErr := readOpaqueResource(sourcePath, identity)
		if readErr != nil {
			return nil, fmt.Errorf("legacyRead: %w", readErr)
		}
		return payload, nil
	}
	definition, err := readLuaDefinition(sourcePath, identity)
	if err != nil {
		return nil, fmt.Errorf("definitionRead: %w", err)
	}
	source, err := readLuaSidecar(sourcePath, definition.source, ".lua")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && definition.format == "disassembly" {
			return nil, fmt.Errorf("sourceMissing: provide valid Lua source in %q before rebuilding; the diagnostic listing cannot be compiled", definition.source)
		}
		return nil, fmt.Errorf("sourceRead: %w", err)
	}
	payload, err := lua51.CompileWithHeaderContext(ctx, source, definition.sourceName, definition.header)
	if err != nil {
		return nil, fmt.Errorf("sourceCompile: %w", err)
	}
	return payload, nil
}

func readLuaDefinition(sourcePath, identity string) (luaResourceDefinition, error) {
	payload, err := readRenderDefinition(sourcePath, "LUA")
	if err != nil {
		return luaResourceDefinition{}, fmt.Errorf("definitionRead: %w", err)
	}
	parser := newParser(bytes.NewReader(payload))
	fields, err := parser.next()
	if err != nil {
		return luaResourceDefinition{}, fmt.Errorf("headerRead: %w", err)
	}
	if len(fields) != 2 || fields[0] != "LUA" || fields[1] != identity {
		return luaResourceDefinition{}, fmt.Errorf("headerMismatch: expected LUA %q", identity)
	}
	resourceVersion, err := parser.uint32Property("VERSION")
	if err != nil {
		return luaResourceDefinition{}, fmt.Errorf("versionRead: %w", err)
	}
	if resourceVersion != version {
		return luaResourceDefinition{}, fmt.Errorf("versionUnsupported: %d", resourceVersion)
	}
	definition := luaResourceDefinition{header: []byte{0x1b, 'L', 'u', 'a', 0x51, 0, 1, 4, 4, 4, 4, 0}}
	for _, property := range []string{"FORMAT", "SOURCE", "SOURCENAME", "ENDIAN"} {
		fields, propertyErr := parser.property(property, 1)
		if propertyErr != nil {
			return luaResourceDefinition{}, fmt.Errorf("propertyRead[%s]: %w", property, propertyErr)
		}
		switch property {
		case "FORMAT":
			definition.format = fields[0]
		case "SOURCE":
			definition.source = fields[0]
		case "SOURCENAME":
			definition.sourceName = fields[0]
		case "ENDIAN":
			if fields[0] != "little" && fields[0] != "big" {
				return luaResourceDefinition{}, errors.New("invalid Lua endianness")
			}
			if fields[0] == "big" {
				definition.header[6] = 0
			}
		}
	}
	if definition.format != "source" && definition.format != "disassembly" {
		return luaResourceDefinition{}, fmt.Errorf("formatUnsupported: %q", definition.format)
	}
	for index, property := range []string{"INTEGERSIZE", "SIZETSIZE", "NUMBERSIZE", "ISINTEGRAL"} {
		number, propertyErr := parser.uint32Property(property)
		if propertyErr != nil {
			return luaResourceDefinition{}, fmt.Errorf("layoutRead[%s]: %w", property, propertyErr)
		}
		if index == 3 {
			if number > 1 {
				return luaResourceDefinition{}, errors.New("invalid Lua integral-number marker")
			}
			definition.header[11] = byte(number)
			continue
		}
		if number != 4 && number != 8 {
			return luaResourceDefinition{}, fmt.Errorf("layoutUnsupported: %s %d", property, number)
		}
		offsets := []int{7, 8, 10}
		definition.header[offsets[index]] = byte(number)
	}
	if definition.format == "disassembly" {
		fields, propertyErr := parser.property("DIAGNOSTIC", 1)
		if propertyErr != nil {
			return luaResourceDefinition{}, fmt.Errorf("diagnosticProperty: %w", propertyErr)
		}
		definition.diagnostic = fields[0]
	}
	fields, err = parser.next()
	if !errors.Is(err, io.EOF) {
		if err != nil {
			return luaResourceDefinition{}, fmt.Errorf("trailingRead: %w", err)
		}
		return luaResourceDefinition{}, fmt.Errorf("trailingFields: %v", fields)
	}
	return definition, nil
}

// Validation does not compile scripts: a diagnostic-only resource remains
// inspectable, but readLuaResource rejects rebuilding until source is supplied.
func validateLuaResource(ctx context.Context, sourcePath, identity string) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("validateContext: %w", err)
	}
	isLua, err := hasRenderDefinition(sourcePath, "LUA")
	if err != nil {
		return fmt.Errorf("definitionCheck: %w", err)
	}
	if !isLua {
		payload, readErr := readOpaqueResource(sourcePath, identity)
		if readErr != nil {
			return fmt.Errorf("legacyRead: %w", readErr)
		}
		if payload == nil {
			return errors.New("legacy Lua payload is missing")
		}
		return nil
	}
	definition, err := readLuaDefinition(sourcePath, identity)
	if err != nil {
		return fmt.Errorf("definitionRead: %w", err)
	}
	_, err = readLuaSidecar(sourcePath, definition.source, ".lua")
	if err != nil && !(errors.Is(err, os.ErrNotExist) && definition.format == "disassembly") {
		return fmt.Errorf("sourceValidate: %w", err)
	}
	if definition.diagnostic != "" {
		_, err = readLuaSidecar(sourcePath, definition.diagnostic, ".lua.txt")
		if err != nil {
			return fmt.Errorf("diagnosticValidate: %w", err)
		}
	}
	return nil
}

func luaResourceSidecarPaths(rootPath, definitionPath string) ([]string, error) {
	isLua, err := hasRenderDefinition(definitionPath, "LUA")
	if err != nil {
		return nil, fmt.Errorf("definitionCheck: %w", err)
	}
	if !isLua {
		return nil, nil
	}
	definition, err := readLuaDefinition(definitionPath, resourceDefinitionIdentity(definitionPath))
	if err != nil {
		return nil, fmt.Errorf("definitionRead: %w", err)
	}
	names := []string{definition.source}
	if definition.diagnostic != "" {
		names = append(names, definition.diagnostic)
	}
	paths := make([]string, 0, len(names))
	for _, name := range names {
		suffix := ".lua"
		if name == definition.diagnostic {
			suffix = ".lua.txt"
		}
		_, readErr := readLuaSidecar(definitionPath, name, suffix)
		if readErr != nil {
			if name == definition.source && definition.format == "disassembly" && errors.Is(readErr, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("sidecarRead: %w", readErr)
		}
		path, relativeErr := filepath.Rel(rootPath, filepath.Join(filepath.Dir(definitionPath), name))
		if relativeErr != nil {
			return nil, fmt.Errorf("sidecarRelative: %w", relativeErr)
		}
		if path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
			return nil, errors.New("Lua sidecar escapes DS root")
		}
		paths = append(paths, filepath.Clean(path))
	}
	return paths, nil
}

func luaRecoveryDiagnostic(payload []byte, recoveryErr error) ([]byte, error) {
	var output bytes.Buffer
	_, err := fmt.Fprintf(&output, "Lua source recovery unavailable: %s\n\nSupply valid Lua source in the neighboring .lua file before rebuilding this resource. The listing below is diagnostic disassembly, not editable Lua source.\n\n", recoveryErr)
	if err != nil {
		return nil, fmt.Errorf("diagnosticWrite: %w", err)
	}
	chunk, err := lua51.Inspect(payload)
	if err != nil {
		_, writeErr := fmt.Fprintf(&output, "Bytecode inspection failed: %s\n", err)
		if writeErr != nil {
			return nil, fmt.Errorf("inspectionWrite: %w", writeErr)
		}
		return output.Bytes(), nil
	}
	err = lua51.WriteDisassembly(&output, chunk)
	if err != nil {
		return nil, fmt.Errorf("disassemblyWrite: %w", err)
	}
	return output.Bytes(), nil
}

func readLuaSidecar(definitionPath, name, suffix string) ([]byte, error) {
	if filepath.Base(name) != name || strings.ContainsAny(name, "/\\:") || !strings.HasSuffix(name, suffix) {
		return nil, fmt.Errorf("sidecarName: %q", name)
	}
	path := filepath.Join(filepath.Dir(definitionPath), name)
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("sidecarStat: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("sidecarType: %q is not a regular file", name)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("sidecarRead: %w", err)
	}
	return payload, nil
}

// Older DS directories represented Lua as opaque stored (possibly compressed)
// bytes. Normalize those at the resource boundary, retaining read compatibility.
func decodeLegacyLuaResource(path string, entry dbpf.Entry, payload []byte) ([]byte, error) {
	if entry.Type != luaResourceType {
		return payload, nil
	}
	isLua, err := hasRenderDefinition(path, "LUA")
	if err != nil {
		return nil, fmt.Errorf("definitionCheck: %w", err)
	}
	if isLua {
		return payload, nil
	}
	decoded, err := dbpf.Decode(entry, payload)
	if err != nil {
		return nil, fmt.Errorf("legacyDecode: %w", err)
	}
	return decoded, nil
}
