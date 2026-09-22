// Package ds reads and writes Game's human-readable conversion directory.
package ds

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/darkspinnet/darkspin/content/animation"
	"github.com/darkspinnet/darkspin/content/audio"
	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/darkspinnet/darkspin/content/movie"
	"github.com/darkspinnet/darkspin/content/render/gmsh"
	"github.com/darkspinnet/darkspin/content/render/prop"
	"github.com/darkspinnet/darkspin/content/render/rw4"
	"github.com/darkspinnet/darkspin/content/scaleform"
)

const (
	ManifestName = "_root.dse"
	version      = 1
)

// Document is the package-wide DS definition aggregate.
type Document struct {
	Name     string
	Manifest *dbpf.Manifest
}

// ConvertPath converts between an immutable DBPF package and an editable DS
// directory according to the source kind.
func ConvertPath(ctx context.Context, sourcePath, destinationPath string) error {
	return ConvertPathWithNames(ctx, sourcePath, destinationPath, nil)
}

// ConvertPathWithNames converts a package with optional recovered instance
// names used only for human-readable DS paths.
func ConvertPathWithNames(ctx context.Context, sourcePath, destinationPath string, names map[uint32]string) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	fi, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("sourceStat: %w", err)
	}
	if fi.IsDir() {
		isAudioBundle, bundleErr := IsAudioBundlePath(sourcePath)
		if bundleErr != nil {
			return fmt.Errorf("audioBundleRead: %w", bundleErr)
		}
		if isAudioBundle {
			err = PackAudioBundlePath(ctx, sourcePath, destinationPath)
			if err != nil {
				return fmt.Errorf("audioBundleWrite: %w", err)
			}
			return nil
		}
		document, readErr := ReadPath(sourcePath)
		if readErr != nil {
			return fmt.Errorf("dsRead: %w", readErr)
		}
		if strings.EqualFold(filepath.Ext(destinationPath), ".ds") {
			err = rewritePath(ctx, sourcePath, destinationPath, document, names)
			if err != nil {
				return fmt.Errorf("dsRewrite: %w", err)
			}
			return nil
		}
		err = PackPath(ctx, sourcePath, destinationPath, document)
		if err != nil {
			return fmt.Errorf("packageWrite: %w", err)
		}
		return nil
	}
	err = extractPackagePath(ctx, sourcePath, destinationPath, names)
	if err != nil {
		return fmt.Errorf("dsWrite: %w", err)
	}
	return nil
}

// RewritePath normalizes every DSE resource through the latest semantic
// codecs while preserving the source package snapshot and authored paths.
func RewritePath(ctx context.Context, sourcePath, destinationPath string, document *Document) error {
	return rewritePath(ctx, sourcePath, destinationPath, document, nil)
}

func rewritePath(ctx context.Context, sourcePath, destinationPath string, document *Document, names map[uint32]string) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if document == nil || document.Manifest == nil {
		return errors.New("nil document")
	}
	_, err := os.Stat(destinationPath)
	if err == nil {
		return fmt.Errorf("destinationExists: %q", destinationPath)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("destinationStat: %w", err)
	}
	err = os.MkdirAll(destinationPath, 0o755)
	if err != nil {
		return fmt.Errorf("destinationMkdir: %w", err)
	}
	isComplete := false
	defer func() {
		if !isComplete {
			_ = os.RemoveAll(destinationPath)
		}
	}()
	resources := make([]dbpf.Resource, len(document.Manifest.Resources))
	copy(resources, document.Manifest.Resources)
	manifest := &dbpf.Manifest{
		HeaderBytes:      append([]byte(nil), document.Manifest.HeaderBytes...),
		IndexFlags:       document.Manifest.IndexFlags,
		SharedType:       document.Manifest.SharedType,
		SharedGroup:      document.Manifest.SharedGroup,
		SharedInstanceHi: document.Manifest.SharedInstanceHi,
		Resources:        resources,
	}
	names = packageResourceLinkNames(manifest.Resources, names, "@/")
	for ordinal, resource := range manifest.Resources {
		err = ctx.Err()
		if err != nil {
			return fmt.Errorf("resourceContext[%d]: %w", ordinal, err)
		}
		sourceDefinitionPath, pathErr := safePath(sourcePath, resource.PayloadPath)
		if pathErr != nil {
			return fmt.Errorf("sourcePath[%d]: %w", ordinal, pathErr)
		}
		payload, readErr := readResource(sourceDefinitionPath, resourceDefinitionIdentity(resource.PayloadPath), ordinal, resource.Entry.Type)
		if readErr != nil {
			return fmt.Errorf("resourceRead[%d]: %w", ordinal, readErr)
		}
		destinationDefinitionPath, pathErr := safePath(destinationPath, resource.PayloadPath)
		if pathErr != nil {
			return fmt.Errorf("destinationPath[%d]: %w", ordinal, pathErr)
		}
		err = os.MkdirAll(filepath.Dir(destinationDefinitionPath), 0o755)
		if err != nil {
			return fmt.Errorf("resourceMkdir[%d]: %w", ordinal, err)
		}
		err = writeResource(destinationDefinitionPath, resourceDefinitionIdentity(resource.PayloadPath), ordinal, resource.Entry, bytes.NewReader(payload), names)
		if err != nil {
			return fmt.Errorf("resourceWrite[%d]: %w", ordinal, err)
		}
	}
	err = finalizeScaleformTypeScriptWorkspace(destinationPath)
	if err != nil {
		return fmt.Errorf("scaleformWorkspace: %w", err)
	}
	err = copyAudioWAVs(sourcePath, destinationPath, manifest)
	if err != nil {
		return fmt.Errorf("audioProject: %w", err)
	}
	rewritten := &Document{Name: document.Name, Manifest: manifest}
	err = WritePath(destinationPath, rewritten)
	if err != nil {
		return fmt.Errorf("manifestWrite: %w", err)
	}
	isComplete = true
	return nil
}

// ExtractPackagePath writes a package as a lossless DS directory.
func ExtractPackagePath(ctx context.Context, sourcePath, destinationPath string) error {
	return extractPackagePath(ctx, sourcePath, destinationPath, nil)
}

// ExtractPackageResourcePath converts one selected package entry into DSE.
func ExtractPackageResourcePath(ctx context.Context, sourcePath, destinationPath string, ordinal int) error {
	return ExtractPackageResourcePathWithNames(ctx, sourcePath, destinationPath, ordinal, nil)
}

// ExtractPackageResourcePathWithNames converts one selected package entry and
// projects proven symbolic resource keys into its DSE definition.
func ExtractPackageResourcePathWithNames(ctx context.Context, sourcePath, destinationPath string, ordinal int, names map[uint32]string) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	_, err := os.Stat(destinationPath)
	if err == nil {
		return fmt.Errorf("destinationExists: %q", destinationPath)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("destinationStat: %w", err)
	}
	r, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("packageOpen: %w", err)
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("packageStat: %w", err)
	}
	reader, err := dbpf.NewReader(r, fi.Size())
	if err != nil {
		return fmt.Errorf("packageRead: %w", err)
	}
	if ordinal < 0 || ordinal >= len(reader.Entries) {
		return fmt.Errorf("ordinalRange: got %d, resources %d", ordinal, len(reader.Entries))
	}
	manifest := reader.ArchiveManifest()
	if manifest == nil {
		return errors.New("manifestMissing")
	}
	names, err = packageAnimationNames(reader, manifest.Resources, names)
	if err != nil {
		return fmt.Errorf("animationNames: %w", err)
	}
	resourcePaths := packageResourcePaths(manifest.Resources, names)
	for resourceOrdinal := range manifest.Resources {
		manifest.Resources[resourceOrdinal].PayloadPath = resourcePaths[resourceOrdinal]
	}
	names = packageResourceLinkNames(manifest.Resources, names, "@/")
	entry := reader.Entries[ordinal]
	var payload io.Reader
	if isDecodedDSEType(entry.Type) {
		payload, err = reader.Open(entry)
	} else {
		payload, err = reader.OpenRaw(entry)
	}
	if err != nil {
		return fmt.Errorf("resourceOpen: %w", err)
	}
	identity := strings.TrimSuffix(dbpf.ResourceName(ordinal, entry), ".bin")
	err = writeResource(destinationPath, identity, ordinal, entry, payload, names)
	if err != nil {
		return fmt.Errorf("resourceWrite: %w", err)
	}
	err = projectPackageAudioResource(ctx, reader, ordinal, destinationPath)
	if err != nil {
		return fmt.Errorf("audioProject: %w", err)
	}
	err = projectPackageMovieResource(ctx, reader, ordinal, destinationPath)
	if err != nil {
		_ = os.Remove(destinationPath)
		_ = os.Remove(strings.TrimSuffix(destinationPath, filepath.Ext(destinationPath)) + ".mkv")
		return fmt.Errorf("movieProject: %w", err)
	}
	return nil
}

func extractPackagePath(ctx context.Context, sourcePath, destinationPath string, names map[uint32]string) error {
	return extractPackagePathWithLinkRoot(ctx, sourcePath, destinationPath, names, "@/")
}

func extractPackagePathWithLinkRoot(ctx context.Context, sourcePath, destinationPath string, names map[uint32]string, linkRoot string) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	_, err := os.Stat(destinationPath)
	if err == nil {
		return fmt.Errorf("destinationExists: %q", destinationPath)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("destinationStat: %w", err)
	}
	r, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("packageOpen: %w", err)
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("packageStat: %w", err)
	}
	reader, err := dbpf.NewReader(r, fi.Size())
	if err != nil {
		return fmt.Errorf("packageRead: %w", err)
	}
	manifest := reader.ArchiveManifest()
	if manifest == nil {
		return errors.New("manifestMissing")
	}
	names, err = packageAnimationNames(reader, manifest.Resources, names)
	if err != nil {
		return fmt.Errorf("animationNames: %w", err)
	}
	resourcePaths := packageResourcePaths(manifest.Resources, names)
	for ordinal := range manifest.Resources {
		manifest.Resources[ordinal].PayloadPath = resourcePaths[ordinal]
	}
	names = packageResourceLinkNames(manifest.Resources, names, linkRoot)
	err = os.MkdirAll(destinationPath, 0o755)
	if err != nil {
		return fmt.Errorf("destinationMkdir: %w", err)
	}
	isComplete := false
	defer func() {
		if !isComplete {
			_ = os.RemoveAll(destinationPath)
		}
	}()
	for ordinal, resource := range manifest.Resources {
		err = ctx.Err()
		if err != nil {
			return fmt.Errorf("resourceContext[%d]: %w", ordinal, err)
		}
		payloadPath, pathErr := safePath(destinationPath, resource.PayloadPath)
		if pathErr != nil {
			return fmt.Errorf("resourcePath[%d]: %w", ordinal, pathErr)
		}
		err = os.MkdirAll(filepath.Dir(payloadPath), 0o755)
		if err != nil {
			return fmt.Errorf("resourceMkdir[%d]: %w", ordinal, err)
		}
		var payload io.Reader
		var openErr error
		if isDecodedDSEType(resource.Entry.Type) {
			payload, openErr = reader.Open(resource.Entry)
		} else {
			payload, openErr = reader.OpenRaw(resource.Entry)
		}
		if openErr != nil {
			return fmt.Errorf("resourceOpen[%d]: %w", ordinal, openErr)
		}
		err = writeResource(payloadPath, resourceDefinitionIdentity(resource.PayloadPath), ordinal, resource.Entry, payload, names)
		if err != nil {
			return fmt.Errorf("resourceWrite[%d]: %w", ordinal, err)
		}
	}
	err = finalizeScaleformTypeScriptWorkspace(destinationPath)
	if err != nil {
		return fmt.Errorf("scaleformWorkspace: %w", err)
	}
	err = projectAudioResources(ctx, reader, destinationPath, manifest)
	if err != nil {
		return fmt.Errorf("audioProject: %w", err)
	}
	err = projectMovieResources(ctx, reader, destinationPath, manifest)
	if err != nil {
		return fmt.Errorf("movieProject: %w", err)
	}
	document := &Document{Name: filepath.Base(sourcePath), Manifest: manifest}
	err = WritePath(destinationPath, document)
	if err != nil {
		return fmt.Errorf("manifestWrite: %w", err)
	}
	isComplete = true
	return nil
}

// WritePath writes the strict, count-led DS package definition.
func WritePath(destinationPath string, document *Document) error {
	if document == nil || document.Manifest == nil {
		return errors.New("nil document")
	}
	directory, err := buildDirectoryIndex(document.Manifest)
	if err != nil {
		return fmt.Errorf("directoryBuild: %w", err)
	}
	w, err := os.OpenFile(filepath.Join(destinationPath, ManifestName), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("manifestCreate: %w", err)
	}
	writer := bufio.NewWriter(w)
	_, err = fmt.Fprintf(writer, "// darkspin ds v%d\n", version)
	if err == nil {
		_, err = fmt.Fprintf(writer, "PACKAGE %q\n", document.Name)
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\tVERSION %d\n", version)
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\tHEADER %s\n", strings.ToUpper(hex.EncodeToString(document.Manifest.HeaderBytes)))
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\tINDEXFLAGS 0x%08X\n", document.Manifest.IndexFlags)
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\tSHAREDTYPE 0x%08X\n", document.Manifest.SharedType)
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\tSHAREDGROUP 0x%08X\n", document.Manifest.SharedGroup)
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\tSHAREDINSTANCEHIGH 0x%08X\n", document.Manifest.SharedInstanceHi)
	}
	if err == nil {
		err = writeDirectoryContents(writer, directory)
	}
	if err == nil {
		err = writer.Flush()
	}
	closeErr := w.Close()
	if err != nil {
		return fmt.Errorf("manifestOutput: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("manifestClose: %w", closeErr)
	}
	err = writeChildDirectoryIndexes(destinationPath, directory)
	if err != nil {
		return fmt.Errorf("directoryWrite: %w", err)
	}
	return nil
}

// ReadPath strictly parses a DS directory and verifies its stored payloads.
func ReadPath(sourcePath string) (*Document, error) {
	r, err := os.Open(filepath.Join(sourcePath, ManifestName))
	if err != nil {
		return nil, fmt.Errorf("manifestOpen: %w", err)
	}
	defer r.Close()
	parser := newParser(r)
	definition, err := parser.next()
	if err != nil {
		return nil, fmt.Errorf("definitionRead: %w", err)
	}
	if len(definition) != 2 || definition[0] != "PACKAGE" && definition[0] != "DARKSPINPACKAGE" {
		return nil, fmt.Errorf("definition: got %v", definition)
	}
	document := &Document{Name: definition[1], Manifest: &dbpf.Manifest{}}
	versionFields, err := parser.property("VERSION", 1)
	if err != nil {
		return nil, err
	}
	parsedVersion, err := parseUint(versionFields[0], 32)
	if err != nil {
		return nil, fmt.Errorf("versionParse: %w", err)
	}
	if parsedVersion != version {
		return nil, fmt.Errorf("versionUnsupported: %d", parsedVersion)
	}
	headerFields, err := parser.property("HEADER", 1)
	if err != nil {
		return nil, err
	}
	document.Manifest.HeaderBytes, err = hex.DecodeString(headerFields[0])
	if err != nil {
		return nil, fmt.Errorf("headerDecode: %w", err)
	}
	if len(document.Manifest.HeaderBytes) != dbpf.HeaderSize {
		return nil, fmt.Errorf("headerSize: got %d, want %d", len(document.Manifest.HeaderBytes), dbpf.HeaderSize)
	}
	document.Manifest.IndexFlags, err = parser.uint32Property("INDEXFLAGS")
	if err != nil {
		return nil, err
	}
	document.Manifest.SharedType, err = parser.uint32Property("SHAREDTYPE")
	if err != nil {
		return nil, err
	}
	document.Manifest.SharedGroup, err = parser.uint32Property("SHAREDGROUP")
	if err != nil {
		return nil, err
	}
	document.Manifest.SharedInstanceHi, err = parser.uint32Property("SHAREDINSTANCEHIGH")
	if err != nil {
		return nil, err
	}
	resourcesByOrdinal := make(map[int]dbpf.Resource)
	rootPath := filepath.Clean(filepath.Join(sourcePath, ManifestName))
	visitedPaths := map[string]bool{strings.ToLower(rootPath): true}
	err = readDirectoryContents(sourcePath, "", parser, resourcesByOrdinal, visitedPaths)
	if err != nil {
		return nil, fmt.Errorf("directoryRead: %w", err)
	}
	document.Manifest.Resources = make([]dbpf.Resource, len(resourcesByOrdinal))
	for ordinal := range document.Manifest.Resources {
		resource, isFound := resourcesByOrdinal[ordinal]
		if !isFound {
			return nil, fmt.Errorf("resourceOrdinal[%d]: missing", ordinal)
		}
		document.Manifest.Resources[ordinal] = resource
	}
	for ordinal := range resourcesByOrdinal {
		if ordinal >= len(document.Manifest.Resources) {
			return nil, fmt.Errorf("resourceOrdinal[%d]: exceeds count %d", ordinal, len(document.Manifest.Resources))
		}
	}
	for ordinal, resource := range document.Manifest.Resources {
		payloadPath, pathErr := safePath(sourcePath, resource.PayloadPath)
		if pathErr != nil {
			return nil, fmt.Errorf("resourcePath[%d]: %w", ordinal, pathErr)
		}
		storedPayload, payloadErr := readResource(payloadPath, resourceDefinitionIdentity(resource.PayloadPath), ordinal, resource.Entry.Type)
		if payloadErr != nil {
			return nil, fmt.Errorf("resourceRead[%d]: %w", ordinal, payloadErr)
		}
		expectedSize := resource.Entry.StoredSize
		if isDecodedDSEType(resource.Entry.Type) {
			expectedSize = resource.Entry.Size
		}
		if resource.Entry.Type != animation.ResourceType && !audio.IsStreamType(resource.Entry.Type) && !audio.IsPatchType(resource.Entry.Type) && resource.Entry.Type != movie.ResourceType && !scaleform.IsResourceType(resource.Entry.Type) && len(storedPayload) != int(expectedSize) {
			return nil, fmt.Errorf("resourceSize[%d]: got %d, want %d", ordinal, len(storedPayload), expectedSize)
		}
	}
	err = verifyFiles(sourcePath, document.Manifest)
	if err != nil {
		return nil, fmt.Errorf("directoryVerify: %w", err)
	}
	return document, nil
}

// PackPath rebuilds a package exclusively from its visible DS definitions.
func PackPath(ctx context.Context, sourcePath, destinationPath string, document *Document) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if document == nil || document.Manifest == nil {
		return errors.New("nil document")
	}
	err := packResourcesWithPropertyDeclaration(ctx, sourcePath, destinationPath, document.Manifest, "PROPERTYLIST")
	if err != nil {
		return fmt.Errorf("packageRebuild: %w", err)
	}
	return nil
}

func hashPath(sourcePath string) (string, error) {
	r, err := os.Open(sourcePath)
	if err != nil {
		return "", fmt.Errorf("sourceOpen: %w", err)
	}
	defer r.Close()
	hash := sha256.New()
	_, err = io.Copy(hash, r)
	if err != nil {
		return "", fmt.Errorf("sourceHash: %w", err)
	}
	return strings.ToUpper(hex.EncodeToString(hash.Sum(nil))), nil
}

func verifyFiles(sourcePath string, manifest *dbpf.Manifest) error {
	expectedPaths := map[string]struct{}{
		filepath.Clean(ManifestName): {},
	}
	resourcesByPath := make(map[string]dbpf.Resource)
	isScaleformWorkspace := false
	for ordinal, resource := range manifest.Resources {
		resourcePath := filepath.Clean(filepath.FromSlash(resource.PayloadPath))
		priorResource, isFound := resourcesByPath[resourcePath]
		if isFound && !areResourceCompanions(priorResource.Entry, resource.Entry) {
			return fmt.Errorf("duplicatePath[%d]: %q", ordinal, resource.PayloadPath)
		}
		if !isFound {
			resourcesByPath[resourcePath] = resource
		}
		expectedPaths[resourcePath] = struct{}{}
		for directoryPath := filepath.Dir(resourcePath); directoryPath != "."; directoryPath = filepath.Dir(directoryPath) {
			expectedPaths[filepath.Join(directoryPath, ManifestName)] = struct{}{}
		}
		if resource.Entry.Type == audio.SNRResourceType {
			wavResourcePath := strings.TrimSuffix(resourcePath, filepath.Ext(resourcePath)) + ".wav"
			expectedPaths[wavResourcePath] = struct{}{}
		}
		if resource.Entry.Type == movie.ResourceType {
			aviResourcePath := strings.TrimSuffix(resourcePath, filepath.Ext(resourcePath)) + ".avi"
			expectedPaths[aviResourcePath] = struct{}{}
		}
		if scaleform.IsResourceType(resource.Entry.Type) {
			if resource.Entry.Type == scaleform.MovieResourceType {
				isScaleformWorkspace = true
			}
			definitionPath := filepath.Join(sourcePath, resourcePath)
			sidecarNames, sidecarErr := scaleformSidecarNames(definitionPath, resource.Entry.Type)
			if sidecarErr != nil {
				return fmt.Errorf("scaleformFiles[%d]: %w", ordinal, sidecarErr)
			}
			for _, sidecarName := range sidecarNames {
				expectedPaths[filepath.Join(filepath.Dir(resourcePath), sidecarName)] = struct{}{}
			}
		}
	}
	if isScaleformWorkspace {
		expectedPaths[filepath.Clean("scaleform.d.ts")] = struct{}{}
		expectedPaths[filepath.Clean("tsconfig.json")] = struct{}{}
	}
	foundPaths := make(map[string]struct{}, len(expectedPaths))
	err := filepath.WalkDir(sourcePath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("pathWalk: %w", walkErr)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinkUnsupported: %q", path)
		}
		if entry.IsDir() {
			return nil
		}
		relativePath, relativeErr := filepath.Rel(sourcePath, path)
		if relativeErr != nil {
			return fmt.Errorf("pathRelative: %w", relativeErr)
		}
		relativePath = filepath.Clean(relativePath)
		if _, isFound := expectedPaths[relativePath]; !isFound {
			return fmt.Errorf("unexpectedFile: %q", filepath.ToSlash(relativePath))
		}
		foundPaths[relativePath] = struct{}{}
		return nil
	})
	if err != nil {
		return err
	}
	if len(foundPaths) != len(expectedPaths) {
		return fmt.Errorf("fileCount: got %d, want %d", len(foundPaths), len(expectedPaths))
	}
	return nil
}

type resourcePackSource struct {
	ctx                 context.Context
	sourcePath          string
	propertyDeclaration string
}

func (e resourcePackSource) open(ordinal int, resource dbpf.Resource) (io.ReadCloser, dbpf.Resource, error) {
	err := e.ctx.Err()
	if err != nil {
		return nil, dbpf.Resource{}, fmt.Errorf("resourceContext[%d]: %w", ordinal, err)
	}
	definitionPath, err := safePath(e.sourcePath, resource.PayloadPath)
	if err != nil {
		return nil, dbpf.Resource{}, fmt.Errorf("definitionPath[%d]: %w", ordinal, err)
	}
	storedPayload, err := readResourceWithPropertyDeclaration(definitionPath, resourceDefinitionIdentity(resource.PayloadPath), ordinal, resource.Entry.Type, e.propertyDeclaration)
	if err != nil {
		return nil, dbpf.Resource{}, fmt.Errorf("definitionRead[%d]: %w", ordinal, err)
	}
	if isDecodedDSEType(resource.Entry.Type) {
		resource.Entry.StoredSize = uint32(len(storedPayload))
		resource.Entry.Size = uint32(len(storedPayload))
		resource.Entry.Compression = 0
		resource.IsStoredSizeFlag = false
	}
	return io.NopCloser(bytes.NewReader(storedPayload)), resource, nil
}

func packResourcesWithPropertyDeclaration(ctx context.Context, sourcePath, destinationPath string, manifest *dbpf.Manifest, propertyDeclaration string) error {
	source := resourcePackSource{ctx: ctx, sourcePath: sourcePath, propertyDeclaration: propertyDeclaration}
	err := dbpf.PackManifestReaders(ctx, destinationPath, manifest, source.open)
	if err != nil {
		return fmt.Errorf("packageWrite: %w", err)
	}
	return nil
}

// StoredResource reads and decodes one DS resource by package ordinal.
func (e *Document) StoredResource(sourcePath string, ordinal int) ([]byte, dbpf.Entry, error) {
	if e == nil || e.Manifest == nil {
		return nil, dbpf.Entry{}, errors.New("nil document")
	}
	if ordinal < 0 || ordinal >= len(e.Manifest.Resources) {
		return nil, dbpf.Entry{}, fmt.Errorf("ordinalRange: got %d, resources %d", ordinal, len(e.Manifest.Resources))
	}
	resource := e.Manifest.Resources[ordinal]
	payloadPath, err := safePath(sourcePath, resource.PayloadPath)
	if err != nil {
		return nil, dbpf.Entry{}, fmt.Errorf("payloadPath: %w", err)
	}
	storedPayload, err := readResource(payloadPath, resourceDefinitionIdentity(resource.PayloadPath), ordinal, resource.Entry.Type)
	if err != nil {
		return nil, dbpf.Entry{}, fmt.Errorf("payloadRead: %w", err)
	}
	if isDecodedDSEType(resource.Entry.Type) {
		return storedPayload, resource.Entry, nil
	}
	decodedPayload, err := dbpf.Decode(resource.Entry, storedPayload)
	if err != nil {
		return nil, dbpf.Entry{}, fmt.Errorf("payloadDecode: %w", err)
	}
	return decodedPayload, resource.Entry, nil
}

func isDecodedDSEType(resourceType uint32) bool {
	return resourceType == animation.ResourceType || resourceType == rw4.ResourceType || resourceType == gmsh.ResourceType || resourceType == movie.ResourceType || scaleform.IsResourceType(resourceType) || prop.IsResourceType(resourceType) || audio.IsStreamType(resourceType) || audio.IsPatchType(resourceType)
}

type parser struct {
	scanner *bufio.Scanner
	line    int
	pending []string
}

func newParser(r io.Reader) *parser {
	return &parser{scanner: bufio.NewScanner(r)}
}

func (e *parser) next() ([]string, error) {
	if e.pending != nil {
		fields := e.pending
		e.pending = nil
		return fields, nil
	}
	for e.scanner.Scan() {
		e.line++
		fields, err := tokenize(e.scanner.Text())
		if err != nil {
			return nil, fmt.Errorf("line[%d]: %w", e.line, err)
		}
		if len(fields) == 0 {
			continue
		}
		return fields, nil
	}
	err := e.scanner.Err()
	if err != nil {
		return nil, fmt.Errorf("lineRead: %w", err)
	}
	return nil, io.EOF
}

func (e *parser) optionalProperty(name string, count int) ([]string, bool, error) {
	fields, err := e.next()
	if err != nil {
		return nil, false, fmt.Errorf("%sRead: %w", name, err)
	}
	if len(fields) == 0 || fields[0] != name {
		e.pending = fields
		return nil, false, nil
	}
	if len(fields) != count+1 {
		return nil, false, fmt.Errorf("line[%d]: expected %s with %d arguments, got %v", e.line, name, count, fields)
	}
	return fields[1:], true, nil
}

func (e *parser) property(name string, count int) ([]string, error) {
	fields, err := e.next()
	if err != nil {
		return nil, fmt.Errorf("%sRead: %w", name, err)
	}
	if len(fields) != count+1 || fields[0] != name {
		return nil, fmt.Errorf("line[%d]: expected %s with %d arguments, got %v", e.line, name, count, fields)
	}
	return fields[1:], nil
}

func (e *parser) uint32Property(name string) (uint32, error) {
	fields, err := e.property(name, 1)
	if err != nil {
		return 0, err
	}
	parsed, err := parseUint(fields[0], 32)
	if err != nil {
		return 0, fmt.Errorf("%sParse: %w", name, err)
	}
	return uint32(parsed), nil
}

func (e *parser) uint64Property(name string) (uint64, error) {
	fields, err := e.property(name, 1)
	if err != nil {
		return 0, err
	}
	parsed, err := parseUint(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("%sParse: %w", name, err)
	}
	return parsed, nil
}

func parseUint(raw string, bits int) (uint64, error) {
	parsed, err := strconv.ParseUint(raw, 0, bits)
	if err != nil {
		return 0, fmt.Errorf("%q: %w", raw, err)
	}
	return parsed, nil
}

func tokenize(line string) ([]string, error) {
	fields := make([]string, 0, 4)
	for offset := 0; offset < len(line); {
		for offset < len(line) && unicode.IsSpace(rune(line[offset])) {
			offset++
		}
		if offset >= len(line) || strings.HasPrefix(line[offset:], "//") {
			break
		}
		if line[offset] == '"' {
			start := offset
			offset++
			for offset < len(line) {
				if line[offset] == '\\' {
					offset += 2
					continue
				}
				if line[offset] == '"' {
					break
				}
				offset++
			}
			if offset >= len(line) {
				return nil, errors.New("unterminated quote")
			}
			offset++
			field, err := strconv.Unquote(line[start:offset])
			if err != nil {
				return nil, fmt.Errorf("quotedValue: %w", err)
			}
			fields = append(fields, field)
			continue
		}
		start := offset
		for offset < len(line) && !unicode.IsSpace(rune(line[offset])) {
			if strings.HasPrefix(line[offset:], "//") {
				break
			}
			offset++
		}
		if start != offset {
			fields = append(fields, line[start:offset])
		}
		if strings.HasPrefix(line[offset:], "//") {
			break
		}
	}
	return fields, nil
}

func safePath(rootPath, relativePath string) (string, error) {
	if relativePath == "" || filepath.IsAbs(relativePath) {
		return "", errors.New("invalid relative path")
	}
	rootPath, err := filepath.Abs(rootPath)
	if err != nil {
		return "", fmt.Errorf("rootResolve: %w", err)
	}
	candidatePath, err := filepath.Abs(filepath.Join(rootPath, filepath.FromSlash(relativePath)))
	if err != nil {
		return "", fmt.Errorf("pathResolve: %w", err)
	}
	relativeCandidate, err := filepath.Rel(rootPath, candidatePath)
	if err != nil {
		return "", fmt.Errorf("pathRelative: %w", err)
	}
	if relativeCandidate == ".." || filepath.IsAbs(relativeCandidate) || strings.HasPrefix(relativeCandidate, ".."+string(os.PathSeparator)) {
		return "", errors.New("path escapes DS directory")
	}
	return candidatePath, nil
}
