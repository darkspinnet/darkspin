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

	"github.com/darkspinnet/darkspin/content/dbpf"
)

type indexedResource struct {
	ordinal  int
	resource dbpf.Resource
}

type directoryIndex struct {
	path      string
	children  map[string]*directoryIndex
	resources []indexedResource
}

func buildDirectoryIndex(manifest *dbpf.Manifest) (*directoryIndex, error) {
	root := &directoryIndex{children: make(map[string]*directoryIndex)}
	for ordinal, resource := range manifest.Resources {
		cleanPath := filepath.ToSlash(filepath.Clean(filepath.FromSlash(resource.PayloadPath)))
		if cleanPath == "." || strings.HasPrefix(cleanPath, "../") || filepath.IsAbs(cleanPath) {
			return nil, fmt.Errorf("resourcePath[%d]: %q", ordinal, resource.PayloadPath)
		}
		parts := strings.Split(cleanPath, "/")
		fileName := parts[len(parts)-1]
		if fileName == ManifestName {
			return nil, fmt.Errorf("resourceName[%d]: reserved %q", ordinal, fileName)
		}
		directory := root
		for _, part := range parts[:len(parts)-1] {
			if part == "" || part == "." || part == ".." {
				return nil, fmt.Errorf("directoryName[%d]: %q", ordinal, part)
			}
			child := directory.children[part]
			if child == nil {
				childPath := part
				if directory.path != "" {
					childPath = filepath.ToSlash(filepath.Join(directory.path, part))
				}
				child = &directoryIndex{path: childPath, children: make(map[string]*directoryIndex)}
				directory.children[part] = child
			}
			directory = child
		}
		resource.PayloadPath = fileName
		directory.resources = append(directory.resources, indexedResource{ordinal: ordinal, resource: resource})
	}
	return root, nil
}

func writeDirectoryContents(writer *bufio.Writer, directory *directoryIndex) error {
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
	for _, indexed := range directory.resources {
		if err != nil {
			break
		}
		err = writeIndexedResource(writer, indexed)
	}
	if err != nil {
		return fmt.Errorf("directoryOutput: %w", err)
	}
	return nil
}

func writeIndexedResource(writer *bufio.Writer, indexed indexedResource) error {
	return writeIndexedResourceDeclaration(writer, indexed, "RESOURCE")
}

func writeIndexedResourceDeclaration(writer *bufio.Writer, indexed indexedResource, declaration string) error {
	resource := indexed.resource
	isStoredSizeFlag := 0
	if resource.IsStoredSizeFlag {
		isStoredSizeFlag = 1
	}
	_, err := fmt.Fprintf(writer, "\t\t%s %q\n", declaration, resource.PayloadPath)
	if err == nil {
		_, err = fmt.Fprintf(writer, "\t\t\tORDINAL %d\n", indexed.ordinal)
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\t\t\tTYPE 0x%08X\n", resource.Entry.Type)
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\t\t\tGROUP 0x%08X\n", resource.Entry.Group)
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\t\t\tINSTANCE 0x%016X\n", resource.Entry.Instance)
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\t\t\tOFFSET %d\n", resource.Entry.Offset)
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\t\t\tSTOREDSIZE %d\n", resource.Entry.StoredSize)
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\t\t\tDECODEDSIZE %d\n", resource.Entry.Size)
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\t\t\tCOMPRESSION 0x%04X\n", resource.Entry.Compression)
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\t\t\tFLAGS 0x%04X\n", resource.Entry.Flags)
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\t\t\tSTOREDSIZEFLAG %d\n", isStoredSizeFlag)
	}
	if err != nil {
		return fmt.Errorf("resourceOutput: %w", err)
	}
	return nil
}

func writeChildDirectoryIndexes(destinationPath string, directory *directoryIndex) error {
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
		_, err = fmt.Fprintf(writer, "// darkspin directory dse v%d\nDIRECTORY %q\n\tVERSION %d\n", version, child.path, version)
		if err == nil {
			err = writeDirectoryContents(writer, child)
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
		err = writeChildDirectoryIndexes(destinationPath, child)
		if err != nil {
			return fmt.Errorf("childWrite[%s]: %w", child.path, err)
		}
	}
	return nil
}

func readDirectoryContents(sourcePath, directoryPath string, parser *parser, resourcesByOrdinal map[int]dbpf.Resource, visitedPaths map[string]bool) error {
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
		resource, ordinal, readErr := readIndexedResource(parser, directoryPath)
		if readErr != nil {
			return fmt.Errorf("resource[%d]: %w", resourceIndex, readErr)
		}
		if _, isFound := resourcesByOrdinal[ordinal]; isFound {
			return fmt.Errorf("resourceOrdinal[%d]: duplicate", ordinal)
		}
		resourcesByOrdinal[ordinal] = resource
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
		if readErr == nil && (len(definition) != 2 || definition[0] != "DIRECTORY" && definition[0] != "DARKSPINDIRECTORY" || definition[1] != childDirectoryPath) {
			readErr = fmt.Errorf("definition: got %v, want DIRECTORY %q", definition, childDirectoryPath)
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
			readErr = readDirectoryContents(sourcePath, childDirectoryPath, childParser, resourcesByOrdinal, visitedPaths)
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

func readIndexedResource(parser *parser, directoryPath string) (dbpf.Resource, int, error) {
	resourceFields, err := parser.property("RESOURCE", 1)
	if err != nil {
		return dbpf.Resource{}, 0, fmt.Errorf("resourceRead: %w", err)
	}
	return readIndexedResourceBody(parser, directoryPath, resourceFields[0])
}

func readIndexedResourceBody(parser *parser, directoryPath, fileName string) (dbpf.Resource, int, error) {
	if filepath.Base(fileName) != fileName || fileName == ManifestName || !strings.EqualFold(filepath.Ext(fileName), ".dse") {
		return dbpf.Resource{}, 0, fmt.Errorf("resourcePath: %q", fileName)
	}
	ordinalFields, err := parser.property("ORDINAL", 1)
	if err != nil {
		return dbpf.Resource{}, 0, fmt.Errorf("ordinalRead: %w", err)
	}
	ordinal, err := strconv.Atoi(ordinalFields[0])
	if err != nil || ordinal < 0 {
		return dbpf.Resource{}, 0, fmt.Errorf("ordinal: %q", ordinalFields[0])
	}
	resource := dbpf.Resource{PayloadPath: filepath.ToSlash(filepath.Join(directoryPath, fileName))}
	resource.Entry.Type, err = parser.uint32Property("TYPE")
	if err != nil {
		return dbpf.Resource{}, 0, fmt.Errorf("type: %w", err)
	}
	resource.Entry.Group, err = parser.uint32Property("GROUP")
	if err != nil {
		return dbpf.Resource{}, 0, fmt.Errorf("group: %w", err)
	}
	resource.Entry.Instance, err = parser.uint64Property("INSTANCE")
	if err != nil {
		return dbpf.Resource{}, 0, fmt.Errorf("instance: %w", err)
	}
	resource.Entry.Offset, err = parser.uint32Property("OFFSET")
	if err != nil {
		return dbpf.Resource{}, 0, fmt.Errorf("offset: %w", err)
	}
	resource.Entry.StoredSize, err = parser.uint32Property("STOREDSIZE")
	if err != nil {
		return dbpf.Resource{}, 0, fmt.Errorf("storedSize: %w", err)
	}
	resource.Entry.Size, err = parser.uint32Property("DECODEDSIZE")
	if err != nil {
		return dbpf.Resource{}, 0, fmt.Errorf("decodedSize: %w", err)
	}
	compression, err := parser.uint32Property("COMPRESSION")
	if err != nil || compression > uint32(^uint16(0)) {
		return dbpf.Resource{}, 0, fmt.Errorf("compression: %d: %v", compression, err)
	}
	resource.Entry.Compression = uint16(compression)
	flags, err := parser.uint32Property("FLAGS")
	if err != nil || flags > uint32(^uint16(0)) {
		return dbpf.Resource{}, 0, fmt.Errorf("flags: %d: %v", flags, err)
	}
	resource.Entry.Flags = uint16(flags)
	storedFlagFields, err := parser.property("STOREDSIZEFLAG", 1)
	if err != nil {
		return dbpf.Resource{}, 0, fmt.Errorf("storedSizeFlag: %w", err)
	}
	switch storedFlagFields[0] {
	case "0":
		resource.IsStoredSizeFlag = false
	case "1":
		resource.IsStoredSizeFlag = true
	default:
		return dbpf.Resource{}, 0, fmt.Errorf("storedSizeFlag: %q", storedFlagFields[0])
	}
	return resource, ordinal, nil
}
