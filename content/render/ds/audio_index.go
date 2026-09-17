package ds

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/content/audio"
	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/darkspinnet/darkspin/content/render/prop"
)

const (
	audioResourceDeclaration     = "AUDIORESOURCE"
	audioPropResourceDeclaration = "AUDIOPROPSRESOURCE"
)

type audioBundleIndexedResource struct {
	archiveName string
	indexedResource
}

type audioBundleDirectoryIndex struct {
	path      string
	children  map[string]*audioBundleDirectoryIndex
	resources []audioBundleIndexedResource
}

type audioBundleArchive struct {
	name     string
	manifest *dbpf.Manifest
}

type audioBundleIndexBuilder struct {
	root            *audioBundleDirectoryIndex
	resourcesByPath map[string][]audioBundleIndexedResource
}

func buildAudioBundleDirectoryIndex(document *audioBundleDocument) (*audioBundleDirectoryIndex, error) {
	builder := &audioBundleIndexBuilder{
		root:            &audioBundleDirectoryIndex{children: make(map[string]*audioBundleDirectoryIndex)},
		resourcesByPath: make(map[string][]audioBundleIndexedResource),
	}
	archives := []audioBundleArchive{
		{name: "Audio.package", manifest: document.audioManifest},
		{name: "AudioProps.package", manifest: document.propertyManifest},
	}
	for _, archive := range archives {
		err := builder.addArchive(archive)
		if err != nil {
			return nil, fmt.Errorf("archive[%s]: %w", archive.name, err)
		}
	}
	return builder.root, nil
}

func (e *audioBundleIndexBuilder) addArchive(archive audioBundleArchive) error {
	for ordinal, resource := range archive.manifest.Resources {
		err := e.addResource(archive.name, ordinal, resource)
		if err != nil {
			return fmt.Errorf("resource[%d]: %w", ordinal, err)
		}
	}
	return nil
}

func (e *audioBundleIndexBuilder) addResource(archiveName string, ordinal int, resource dbpf.Resource) error {
	cleanPath, err := cleanAudioBundleResourcePath(resource.PayloadPath)
	if err != nil {
		return fmt.Errorf("path: %w", err)
	}
	indexed := audioBundleIndexedResource{
		archiveName:     archiveName,
		indexedResource: indexedResource{ordinal: ordinal, resource: resource},
	}
	pathKey := strings.ToLower(cleanPath)
	err = checkAudioBundleCollision(cleanPath, indexed, e.resourcesByPath[pathKey])
	if err != nil {
		return fmt.Errorf("collision: %w", err)
	}
	directory, fileName, err := audioBundleResourceDirectory(e.root, cleanPath)
	if err != nil {
		return fmt.Errorf("directory: %w", err)
	}
	e.resourcesByPath[pathKey] = append(e.resourcesByPath[pathKey], indexed)
	resource.PayloadPath = fileName
	indexed.resource = resource
	directory.resources = append(directory.resources, indexed)
	return nil
}

func cleanAudioBundleResourcePath(resourcePath string) (string, error) {
	cleanPath := filepath.ToSlash(filepath.Clean(filepath.FromSlash(resourcePath)))
	if cleanPath == "." || strings.HasPrefix(cleanPath, "../") || filepath.IsAbs(cleanPath) {
		return "", fmt.Errorf("invalid %q", resourcePath)
	}
	return cleanPath, nil
}

func checkAudioBundleCollision(
	cleanPath string,
	indexed audioBundleIndexedResource,
	priorResources []audioBundleIndexedResource,
) error {
	for _, prior := range priorResources {
		if prior.archiveName == indexed.archiveName || areAudioBundleCompanions(prior.resource.Entry, indexed.resource.Entry) {
			continue
		}
		return fmt.Errorf("%q owned by %s", cleanPath, prior.archiveName)
	}
	return nil
}

func audioBundleResourceDirectory(
	root *audioBundleDirectoryIndex,
	cleanPath string,
) (*audioBundleDirectoryIndex, string, error) {
	parts := strings.Split(cleanPath, "/")
	fileName := parts[len(parts)-1]
	if fileName == ManifestName {
		return nil, "", fmt.Errorf("reserved resource name %q", fileName)
	}
	directory := root
	for _, part := range parts[:len(parts)-1] {
		if part == "" || part == "." || part == ".." {
			return nil, "", fmt.Errorf("invalid name %q", part)
		}
		directory = audioBundleChildDirectory(directory, part)
	}
	return directory, fileName, nil
}

func audioBundleChildDirectory(parent *audioBundleDirectoryIndex, name string) *audioBundleDirectoryIndex {
	child := parent.children[name]
	if child != nil {
		return child
	}
	childPath := name
	if parent.path != "" {
		childPath = filepath.ToSlash(filepath.Join(parent.path, name))
	}
	child = &audioBundleDirectoryIndex{path: childPath, children: make(map[string]*audioBundleDirectoryIndex)}
	parent.children[name] = child
	return child
}

func areAudioBundleCompanions(first, second dbpf.Entry) bool {
	isFirstProperty := prop.IsResourceType(first.Type)
	isSecondProperty := prop.IsResourceType(second.Type)
	if isFirstProperty && isSecondProperty {
		return true
	}
	if isFirstProperty {
		return audio.IsStreamType(second.Type) || audio.IsPatchType(second.Type)
	}
	if isSecondProperty {
		return audio.IsStreamType(first.Type) || audio.IsPatchType(first.Type)
	}
	return false
}

func writeAudioBundleDirectoryContents(writer *bufio.Writer, directory *audioBundleDirectoryIndex) error {
	childNames := make([]string, 0, len(directory.children))
	for childName := range directory.children {
		childNames = append(childNames, childName)
	}
	sort.Strings(childNames)
	_, err := fmt.Fprintf(writer, "\tNUMINCLUDES %d\n", len(childNames))
	for _, childName := range childNames {
		if err != nil {
			break
		}
		_, err = fmt.Fprintf(writer, "\t\tINCLUDE %q\n", filepath.ToSlash(filepath.Join(childName, ManifestName)))
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\tNUMRESOURCES %d\n", len(directory.resources))
	}
	for _, resource := range directory.resources {
		if err != nil {
			break
		}
		declaration := audioResourceDeclaration
		if resource.archiveName == "AudioProps.package" {
			declaration = audioPropResourceDeclaration
		}
		err = writeIndexedResourceDeclaration(writer, resource.indexedResource, declaration)
	}
	if err != nil {
		return fmt.Errorf("directoryOutput: %w", err)
	}
	return nil
}

func writeAudioBundleChildIndexes(destinationPath string, directory *audioBundleDirectoryIndex) error {
	childNames := make([]string, 0, len(directory.children))
	for childName := range directory.children {
		childNames = append(childNames, childName)
	}
	sort.Strings(childNames)
	for _, childName := range childNames {
		child := directory.children[childName]
		indexPath, err := safePath(destinationPath, filepath.ToSlash(filepath.Join(child.path, ManifestName)))
		if err != nil {
			return fmt.Errorf("indexPath[%s]: %w", child.path, err)
		}
		w, err := os.OpenFile(indexPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return fmt.Errorf("indexCreate[%s]: %w", child.path, err)
		}
		writer := bufio.NewWriter(w)
		_, err = fmt.Fprintf(writer, "// darkspin audio directory dse v%d\nAUDIODIRECTORY %q\n\tVERSION %d\n", version, child.path, version)
		if err == nil {
			err = writeAudioBundleDirectoryContents(writer, child)
		}
		if err == nil {
			err = writer.Flush()
		}
		closeErr := w.Close()
		if err != nil {
			return fmt.Errorf("indexOutput[%s]: %w", child.path, err)
		}
		if closeErr != nil {
			return fmt.Errorf("indexClose[%s]: %w", child.path, closeErr)
		}
		err = writeAudioBundleChildIndexes(destinationPath, child)
		if err != nil {
			return fmt.Errorf("childWrite[%s]: %w", child.path, err)
		}
	}
	return nil
}

func readAudioBundleDirectoryContents(sourcePath, directoryPath string, parser *parser, resourcesByArchive map[string]map[int]dbpf.Resource, visitedPaths map[string]bool) error {
	includeFields, err := parser.property("NUMINCLUDES", 1)
	if err != nil {
		return fmt.Errorf("includeCountRead: %w", err)
	}
	includeCount, err := strconv.Atoi(includeFields[0])
	if err != nil || includeCount < 0 {
		return fmt.Errorf("includeCount: %q", includeFields[0])
	}
	includes := make([]string, includeCount)
	for includeIndex := range includes {
		fields, readErr := parser.property("INCLUDE", 1)
		if readErr != nil {
			return fmt.Errorf("include[%d]: %w", includeIndex, readErr)
		}
		includePath := filepath.ToSlash(filepath.Clean(filepath.FromSlash(fields[0])))
		parts := strings.Split(includePath, "/")
		if len(parts) != 2 || parts[0] == "" || parts[0] == "." || parts[0] == ".." || parts[1] != ManifestName {
			return fmt.Errorf("includePath[%d]: %q", includeIndex, fields[0])
		}
		includes[includeIndex] = includePath
	}
	resourceFields, err := parser.property("NUMRESOURCES", 1)
	if err != nil {
		return fmt.Errorf("resourceCountRead: %w", err)
	}
	resourceCount, err := strconv.Atoi(resourceFields[0])
	if err != nil || resourceCount < 0 {
		return fmt.Errorf("resourceCount: %q", resourceFields[0])
	}
	for resourceIndex := 0; resourceIndex < resourceCount; resourceIndex++ {
		definition, readErr := parser.next()
		if readErr != nil {
			return fmt.Errorf("resourceDefinition[%d]: %w", resourceIndex, readErr)
		}
		if len(definition) != 2 || definition[0] != audioResourceDeclaration && definition[0] != audioPropResourceDeclaration {
			return fmt.Errorf("resourceDefinition[%d]: %v", resourceIndex, definition)
		}
		archiveName := "Audio.package"
		if definition[0] == audioPropResourceDeclaration {
			archiveName = "AudioProps.package"
		}
		resource, ordinal, bodyErr := readIndexedResourceBody(parser, directoryPath, definition[1])
		if bodyErr != nil {
			return fmt.Errorf("resource[%d]: %w", resourceIndex, bodyErr)
		}
		if _, isFound := resourcesByArchive[archiveName][ordinal]; isFound {
			return fmt.Errorf("resourceOrdinal[%s:%d]: duplicate", archiveName, ordinal)
		}
		resourcesByArchive[archiveName][ordinal] = resource
	}
	remaining, err := parser.next()
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("directoryTrailing: %w", err)
	}
	if len(remaining) != 0 {
		return fmt.Errorf("directoryTrailing: %v", remaining)
	}
	for includeIndex, includePath := range includes {
		childName := strings.Split(includePath, "/")[0]
		childDirectoryPath := childName
		if directoryPath != "" {
			childDirectoryPath = filepath.ToSlash(filepath.Join(directoryPath, childName))
		}
		childIndexPath, pathErr := safePath(sourcePath, filepath.ToSlash(filepath.Join(childDirectoryPath, ManifestName)))
		if pathErr != nil {
			return fmt.Errorf("includePath[%d]: %w", includeIndex, pathErr)
		}
		pathKey := strings.ToLower(filepath.Clean(childIndexPath))
		if visitedPaths[pathKey] {
			return fmt.Errorf("includeCycle[%d]: %q", includeIndex, includePath)
		}
		visitedPaths[pathKey] = true
		r, openErr := os.Open(childIndexPath)
		if openErr != nil {
			return fmt.Errorf("includeOpen[%d]: %w", includeIndex, openErr)
		}
		childParser := newParser(r)
		definition, readErr := childParser.next()
		if readErr == nil && (len(definition) != 2 || definition[0] != "AUDIODIRECTORY" && definition[0] != "DARKSPINAUDIODIRECTORY" || definition[1] != childDirectoryPath) {
			readErr = fmt.Errorf("definition: got %v, want AUDIODIRECTORY %q", definition, childDirectoryPath)
		}
		if readErr == nil {
			versionFields, versionErr := childParser.property("VERSION", 1)
			if versionErr != nil {
				readErr = fmt.Errorf("versionRead: %w", versionErr)
			} else if versionFields[0] != strconv.Itoa(version) {
				readErr = fmt.Errorf("versionUnsupported: %q", versionFields[0])
			}
		}
		if readErr == nil {
			readErr = readAudioBundleDirectoryContents(sourcePath, childDirectoryPath, childParser, resourcesByArchive, visitedPaths)
		}
		closeErr := r.Close()
		if readErr != nil {
			return fmt.Errorf("includeRead[%d]: %w", includeIndex, readErr)
		}
		if closeErr != nil {
			return fmt.Errorf("includeClose[%d]: %w", includeIndex, closeErr)
		}
	}
	return nil
}
