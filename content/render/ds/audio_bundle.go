package ds

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/content/audio"
	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/darkspinnet/darkspin/content/render/prop"
)

const audioBundleName = "Audio"

type audioBundleDocument struct {
	audioManifest    *dbpf.Manifest
	propertyManifest *dbpf.Manifest
}

// ExtractAudioBundlePath converts Audio.package and its required sibling
// AudioProps.package into one flattened, navigable DS namespace.
func ExtractAudioBundlePath(ctx context.Context, audioPath, propertyPath, destinationPath string, names map[uint32]string) error {
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
	audioReader, audioHandle, audioManifest, err := openAudioBundleArchive(audioPath)
	if err != nil {
		return fmt.Errorf("audioOpen: %w", err)
	}
	defer audioHandle.Close()
	propertyReader, propertyHandle, propertyManifest, err := openAudioBundleArchive(propertyPath)
	if err != nil {
		return fmt.Errorf("audioPropsOpen: %w", err)
	}
	defer propertyHandle.Close()
	audioPaths := packageResourcePaths(audioManifest.Resources, names)
	for ordinal := range audioManifest.Resources {
		audioManifest.Resources[ordinal].PayloadPath = audioPaths[ordinal]
	}
	links := packageResourceLinks(audioManifest.Resources, "@/", audio.SNRResourceType)
	instances, err := audio.SampleInstances(propertyPath)
	if err != nil {
		return fmt.Errorf("sampleInstances: %w", err)
	}
	propertyNames := make(map[uint32]string, len(names)+len(links))
	for nameID, name := range names {
		propertyNames[nameID] = name
	}
	for instanceID := range instances {
		if propertyNames[instanceID] == "" && links[instanceID] != "" {
			propertyNames[instanceID] = links[instanceID]
		}
	}
	propertyPaths := packageResourcePaths(propertyManifest.Resources, names)
	for ordinal := range propertyManifest.Resources {
		propertyManifest.Resources[ordinal].PayloadPath = propertyPaths[ordinal]
	}
	resolveAudioBundlePathCollisions(audioManifest, propertyManifest)
	propertyNames = packageResourceLinkNames(propertyManifest.Resources, propertyNames, "@/")
	document := &audioBundleDocument{audioManifest: audioManifest, propertyManifest: propertyManifest}
	_, err = buildAudioBundleDirectoryIndex(document)
	if err != nil {
		return fmt.Errorf("directoryBuild: %w", err)
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
	err = writeAudioBundleResources(ctx, propertyReader, destinationPath, propertyManifest, propertyNames, "PROPSLIST")
	if err != nil {
		return fmt.Errorf("audioPropsWrite: %w", err)
	}
	err = writeAudioBundleResources(ctx, audioReader, destinationPath, audioManifest, names, "AUDIOLIST")
	if err != nil {
		return fmt.Errorf("audioWrite: %w", err)
	}
	err = projectAudioResources(ctx, audioReader, destinationPath, audioManifest)
	if err != nil {
		return fmt.Errorf("audioProject: %w", err)
	}
	err = writeAudioBundlePath(destinationPath, document)
	if err != nil {
		return fmt.Errorf("bundleWrite: %w", err)
	}
	isComplete = true
	return nil
}

func resolveAudioBundlePathCollisions(audioManifest, propertyManifest *dbpf.Manifest) {
	audioTypesByPath := make(map[string][]uint32)
	usedPaths := make(map[string]bool)
	for _, resource := range audioManifest.Resources {
		pathKey := strings.ToLower(resource.PayloadPath)
		audioTypesByPath[pathKey] = append(audioTypesByPath[pathKey], resource.Entry.Type)
		usedPaths[pathKey] = true
	}
	for ordinal := range propertyManifest.Resources {
		resource := &propertyManifest.Resources[ordinal]
		pathKey := strings.ToLower(resource.PayloadPath)
		isMergeable := len(audioTypesByPath[pathKey]) != 0
		for _, resourceType := range audioTypesByPath[pathKey] {
			if !audio.IsStreamType(resourceType) && !audio.IsPatchType(resourceType) && !prop.IsResourceType(resourceType) {
				isMergeable = false
				break
			}
		}
		if !usedPaths[pathKey] || isMergeable {
			usedPaths[pathKey] = true
			continue
		}
		extension := filepath.Ext(resource.PayloadPath)
		basePath := strings.TrimSuffix(resource.PayloadPath, extension)
		resource.PayloadPath = fmt.Sprintf("%s__%08x_%08x_%016x%s", basePath, resource.Entry.Type, resource.Entry.Group, resource.Entry.Instance, extension)
		usedPaths[strings.ToLower(resource.PayloadPath)] = true
	}
}

func openAudioBundleArchive(sourcePath string) (*dbpf.Reader, *os.File, *dbpf.Manifest, error) {
	r, err := os.Open(sourcePath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("packageOpen: %w", err)
	}
	fi, err := r.Stat()
	if err != nil {
		_ = r.Close()
		return nil, nil, nil, fmt.Errorf("packageStat: %w", err)
	}
	reader, err := dbpf.NewReader(r, fi.Size())
	if err != nil {
		_ = r.Close()
		return nil, nil, nil, fmt.Errorf("packageRead: %w", err)
	}
	manifest := reader.ArchiveManifest()
	if manifest == nil {
		_ = r.Close()
		return nil, nil, nil, errors.New("manifestMissing")
	}
	return reader, r, manifest, nil
}

func writeAudioBundleResources(ctx context.Context, reader *dbpf.Reader, destinationPath string, manifest *dbpf.Manifest, names map[uint32]string, propertyDeclaration string) error {
	for ordinal, resource := range manifest.Resources {
		err := ctx.Err()
		if err != nil {
			return fmt.Errorf("resourceContext[%d]: %w", ordinal, err)
		}
		definitionPath, err := safePath(destinationPath, resource.PayloadPath)
		if err != nil {
			return fmt.Errorf("resourcePath[%d]: %w", ordinal, err)
		}
		err = os.MkdirAll(filepath.Dir(definitionPath), 0o755)
		if err != nil {
			return fmt.Errorf("resourceMkdir[%d]: %w", ordinal, err)
		}
		var payload io.Reader
		if isDecodedDSEType(resource.Entry.Type) {
			payload, err = reader.Open(resource.Entry)
		} else {
			payload, err = reader.OpenRaw(resource.Entry)
		}
		if err != nil {
			return fmt.Errorf("resourceOpen[%d]: %w", ordinal, err)
		}
		err = writeResourceWithPropertyDeclaration(definitionPath, resourceDefinitionIdentity(resource.PayloadPath), ordinal, resource.Entry, payload, names, propertyDeclaration)
		if err != nil {
			return fmt.Errorf("resourceWrite[%d]: %w", ordinal, err)
		}
	}
	return nil
}

// PackAudioBundlePath rebuilds both source archives into a destination directory.
func PackAudioBundlePath(ctx context.Context, sourcePath, destinationPath string) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	document, err := readAudioBundlePath(sourcePath)
	if err != nil {
		return fmt.Errorf("bundleRead: %w", err)
	}
	fi, err := os.Stat(destinationPath)
	if err != nil {
		return fmt.Errorf("destinationStat: %w", err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("destinationType: expected directory %q", destinationPath)
	}
	archives := []struct {
		name                string
		manifest            *dbpf.Manifest
		propertyDeclaration string
	}{
		{name: "Audio.package", manifest: document.audioManifest, propertyDeclaration: "AUDIOLIST"},
		{name: "AudioProps.package", manifest: document.propertyManifest, propertyDeclaration: "PROPSLIST"},
	}
	writtenPaths := make([]string, 0, len(archives))
	defer func() {
		if len(writtenPaths) != len(archives) {
			for _, writtenPath := range writtenPaths {
				_ = os.Remove(writtenPath)
			}
		}
	}()
	for _, archive := range archives {
		packagePath := filepath.Join(destinationPath, archive.name)
		_, statErr := os.Stat(packagePath)
		if statErr == nil {
			return fmt.Errorf("packageExists: %q", packagePath)
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf("packageStat: %w", statErr)
		}
		temporaryPath, materializedManifest, materializeErr := materializeResourcesWithPropertyDeclaration(ctx, sourcePath, packagePath, archive.manifest, archive.propertyDeclaration)
		if materializeErr != nil {
			return fmt.Errorf("%sMaterialize: %w", archive.name, materializeErr)
		}
		packErr := dbpf.PackManifestPath(ctx, temporaryPath, packagePath, materializedManifest)
		removeErr := os.RemoveAll(temporaryPath)
		if packErr != nil {
			return fmt.Errorf("%sWrite: %w", archive.name, packErr)
		}
		if removeErr != nil {
			return fmt.Errorf("%sCleanup: %w", archive.name, removeErr)
		}
		writtenPaths = append(writtenPaths, packagePath)
	}
	return nil
}

// IsAudioBundlePath reports whether a DS directory has the fixed audio-bundle root.
func IsAudioBundlePath(sourcePath string) (bool, error) {
	r, err := os.Open(filepath.Join(sourcePath, ManifestName))
	if err != nil {
		return false, fmt.Errorf("manifestOpen: %w", err)
	}
	defer r.Close()
	definition, err := newParser(r).next()
	if err != nil {
		return false, fmt.Errorf("definitionRead: %w", err)
	}
	return len(definition) == 2 && (definition[0] == "AUDIO" || definition[0] == "DARKSPINAUDIO"), nil
}

func writeAudioBundlePath(destinationPath string, document *audioBundleDocument) error {
	directory, err := buildAudioBundleDirectoryIndex(document)
	if err != nil {
		return fmt.Errorf("directoryBuild: %w", err)
	}
	w, err := os.OpenFile(filepath.Join(destinationPath, ManifestName), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("manifestCreate: %w", err)
	}
	writer := bufio.NewWriter(w)
	_, err = fmt.Fprintf(writer, "// darkspin audio ds v%d\nAUDIO %q\n\tVERSION %d\n", version, audioBundleName, version)
	if err == nil {
		err = writeAudioArchiveMetadata(writer, "AUDIO", "Audio.package", document.audioManifest)
	}
	if err == nil {
		err = writeAudioArchiveMetadata(writer, "AUDIOPROPS", "AudioProps.package", document.propertyManifest)
	}
	if err == nil {
		err = writeAudioBundleDirectoryContents(writer, directory)
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
	err = writeAudioBundleChildIndexes(destinationPath, directory)
	if err != nil {
		return fmt.Errorf("directoryWrite: %w", err)
	}
	return nil
}

func writeAudioArchiveMetadata(writer *bufio.Writer, prefix, name string, manifest *dbpf.Manifest) error {
	_, err := fmt.Fprintf(writer, "\t%s %q\n", prefix, name)
	if err == nil {
		_, err = fmt.Fprintf(writer, "\t%sHEADER %s\n", prefix, strings.ToUpper(hex.EncodeToString(manifest.HeaderBytes)))
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\t%sINDEXFLAGS 0x%08X\n", prefix, manifest.IndexFlags)
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\t%sSHAREDTYPE 0x%08X\n", prefix, manifest.SharedType)
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\t%sSHAREDGROUP 0x%08X\n", prefix, manifest.SharedGroup)
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\t%sSHAREDINSTANCEHIGH 0x%08X\n", prefix, manifest.SharedInstanceHi)
	}
	return err
}

func readAudioBundlePath(sourcePath string) (*audioBundleDocument, error) {
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
	if len(definition) != 2 || definition[0] != "AUDIO" && definition[0] != "DARKSPINAUDIO" || definition[1] != audioBundleName {
		return nil, fmt.Errorf("definition: got %v", definition)
	}
	versionFields, err := parser.property("VERSION", 1)
	if err != nil {
		return nil, fmt.Errorf("versionRead: %w", err)
	}
	if versionFields[0] != strconv.Itoa(version) {
		return nil, fmt.Errorf("versionUnsupported: %q", versionFields[0])
	}
	audioManifest, err := readAudioArchiveMetadata(parser, "AUDIO", "Audio.package")
	if err != nil {
		return nil, fmt.Errorf("audioMetadata: %w", err)
	}
	propertyManifest, err := readAudioArchiveMetadata(parser, "AUDIOPROPS", "AudioProps.package")
	if err != nil {
		return nil, fmt.Errorf("audioPropsMetadata: %w", err)
	}
	resourcesByArchive := map[string]map[int]dbpf.Resource{
		"Audio.package":      make(map[int]dbpf.Resource),
		"AudioProps.package": make(map[int]dbpf.Resource),
	}
	rootPath := filepath.Clean(filepath.Join(sourcePath, ManifestName))
	visitedPaths := map[string]bool{strings.ToLower(rootPath): true}
	err = readAudioBundleDirectoryContents(sourcePath, "", parser, resourcesByArchive, visitedPaths)
	if err != nil {
		return nil, fmt.Errorf("directoryRead: %w", err)
	}
	err = assignAudioBundleResources(audioManifest, resourcesByArchive["Audio.package"])
	if err != nil {
		return nil, fmt.Errorf("audioResources: %w", err)
	}
	err = assignAudioBundleResources(propertyManifest, resourcesByArchive["AudioProps.package"])
	if err != nil {
		return nil, fmt.Errorf("audioPropsResources: %w", err)
	}
	document := &audioBundleDocument{audioManifest: audioManifest, propertyManifest: propertyManifest}
	err = verifyAudioBundleFiles(sourcePath, document)
	if err != nil {
		return nil, fmt.Errorf("directoryVerify: %w", err)
	}
	return document, nil
}

func readAudioArchiveMetadata(parser *parser, prefix, expectedName string) (*dbpf.Manifest, error) {
	nameFields, err := parser.property(prefix, 1)
	if err != nil {
		return nil, fmt.Errorf("nameRead: %w", err)
	}
	if nameFields[0] != expectedName {
		return nil, fmt.Errorf("name: got %q, want %q", nameFields[0], expectedName)
	}
	headerFields, err := parser.property(prefix+"HEADER", 1)
	if err != nil {
		return nil, fmt.Errorf("headerRead: %w", err)
	}
	headerBytes, err := hex.DecodeString(headerFields[0])
	if err != nil {
		return nil, fmt.Errorf("headerDecode: %w", err)
	}
	if len(headerBytes) != dbpf.HeaderSize {
		return nil, fmt.Errorf("headerSize: got %d, want %d", len(headerBytes), dbpf.HeaderSize)
	}
	manifest := &dbpf.Manifest{HeaderBytes: headerBytes}
	manifest.IndexFlags, err = parser.uint32Property(prefix + "INDEXFLAGS")
	if err != nil {
		return nil, fmt.Errorf("indexFlags: %w", err)
	}
	manifest.SharedType, err = parser.uint32Property(prefix + "SHAREDTYPE")
	if err != nil {
		return nil, fmt.Errorf("sharedType: %w", err)
	}
	manifest.SharedGroup, err = parser.uint32Property(prefix + "SHAREDGROUP")
	if err != nil {
		return nil, fmt.Errorf("sharedGroup: %w", err)
	}
	manifest.SharedInstanceHi, err = parser.uint32Property(prefix + "SHAREDINSTANCEHIGH")
	if err != nil {
		return nil, fmt.Errorf("sharedInstance: %w", err)
	}
	return manifest, nil
}

func assignAudioBundleResources(manifest *dbpf.Manifest, resourcesByOrdinal map[int]dbpf.Resource) error {
	manifest.Resources = make([]dbpf.Resource, len(resourcesByOrdinal))
	for ordinal := range manifest.Resources {
		resource, isFound := resourcesByOrdinal[ordinal]
		if !isFound {
			return fmt.Errorf("resourceOrdinal[%d]: missing", ordinal)
		}
		manifest.Resources[ordinal] = resource
	}
	for ordinal := range resourcesByOrdinal {
		if ordinal >= len(manifest.Resources) {
			return fmt.Errorf("resourceOrdinal[%d]: exceeds count %d", ordinal, len(manifest.Resources))
		}
	}
	return nil
}

func verifyAudioBundleFiles(sourcePath string, document *audioBundleDocument) error {
	expectedPaths := map[string]bool{filepath.Clean(ManifestName): true}
	for _, manifest := range []*dbpf.Manifest{document.audioManifest, document.propertyManifest} {
		for _, resource := range manifest.Resources {
			resourcePath := filepath.Clean(filepath.FromSlash(resource.PayloadPath))
			expectedPaths[resourcePath] = true
			for directoryPath := filepath.Dir(resourcePath); directoryPath != "."; directoryPath = filepath.Dir(directoryPath) {
				expectedPaths[filepath.Join(directoryPath, ManifestName)] = true
			}
			if resource.Entry.Type == audio.SNRResourceType {
				expectedPaths[strings.TrimSuffix(resourcePath, filepath.Ext(resourcePath))+".wav"] = true
			}
		}
	}
	foundPaths := make(map[string]bool, len(expectedPaths))
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
		if !expectedPaths[relativePath] {
			return fmt.Errorf("unexpectedFile: %q", filepath.ToSlash(relativePath))
		}
		foundPaths[relativePath] = true
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
