package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/darkspinnet/darkspin/content/audio"
	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/darkspinnet/darkspin/content/movie"
	"github.com/darkspinnet/darkspin/content/render/gmsh"
	"github.com/darkspinnet/darkspin/content/render/rw4"
	serverutil "github.com/darkspinnet/darkspin/server/util"
)

func readRenderNames(sourcePath string) (map[uint32]string, error) {
	r, err := os.Open(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("registryOpen: %w", err)
	}
	defer r.Close()
	names := make(map[uint32]string)
	scanner := bufio.NewScanner(r)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(strings.SplitN(scanner.Text(), "//", 2)[0])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		name := strings.TrimSpace(fields[0])
		if name == "" {
			return nil, fmt.Errorf("registryName[%d]: empty", lineNumber)
		}
		nameID := serverutil.HashID(name)
		if len(fields) > 1 {
			parsedID, parseErr := strconv.ParseUint(strings.TrimSpace(fields[1]), 0, 32)
			if parseErr != nil {
				return nil, fmt.Errorf("registryID[%d]: %w", lineNumber, parseErr)
			}
			nameID = uint32(parsedID)
		}
		names[nameID] = name
	}
	err = scanner.Err()
	if err != nil {
		return nil, fmt.Errorf("registryRead: %w", err)
	}
	return names, nil
}

func discoverRenderNames(sourcePath string) (map[uint32]string, error) {
	if isSporeAudioPackageName(filepath.Base(sourcePath)) {
		return audio.SporeNames(), nil
	}
	names, err := discoverNames(sourcePath, "reg_file.txt")
	if err != nil {
		return nil, fmt.Errorf("fileRegistry: %w", err)
	}
	if names == nil {
		names = make(map[uint32]string)
	}
	for instanceID, name := range movie.KnownNames() {
		if names[instanceID] == "" {
			names[instanceID] = name
		}
	}
	return names, nil
}

func discoverNames(sourcePath, registryName string) (map[uint32]string, error) {
	absoluteSourcePath, err := filepath.Abs(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("sourcePath: %w", err)
	}
	directories := make([]string, 0, 12)
	for directoryPath := filepath.Dir(absoluteSourcePath); ; directoryPath = filepath.Dir(directoryPath) {
		directories = append(directories, directoryPath)
		parentPath := filepath.Dir(directoryPath)
		if parentPath == directoryPath {
			break
		}
	}
	workingPath, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("workingPath: %w", err)
	}
	directories = append(directories, workingPath)
	seenPaths := make(map[string]bool)
	for _, directoryPath := range directories {
		candidates := []string{
			filepath.Join(directoryPath, registryName),
			filepath.Join(directoryPath, ".cache", "sporemodder-fx", registryName),
			filepath.Join(directoryPath, ".cache", "research", "SporeModder-FX", registryName),
		}
		for _, candidatePath := range candidates {
			pathKey := strings.ToLower(filepath.Clean(candidatePath))
			if seenPaths[pathKey] {
				continue
			}
			seenPaths[pathKey] = true
			fi, statErr := os.Stat(candidatePath)
			if errors.Is(statErr, os.ErrNotExist) {
				continue
			}
			if statErr != nil {
				return nil, fmt.Errorf("registryStat: %w", statErr)
			}
			if fi.IsDir() {
				continue
			}
			names, readErr := readRenderNames(candidatePath)
			if readErr != nil {
				return nil, fmt.Errorf("registryRead: %w", readErr)
			}
			return names, nil
		}
	}
	return nil, nil
}

func applyRenderNames(resources []renderResource, names map[uint32]string) {
	groupsByName := make(map[string]map[uint32]bool)
	for resourceIndex := range resources {
		resource := &resources[resourceIndex]
		resource.Name = renderDisplayName(names[uint32(resource.Entry.Instance)])
		resource.GroupName = renderDisplayName(names[resource.Entry.Group])
		if resource.Name == "" {
			continue
		}
		nameKey := strings.ToLower(resource.Name)
		if groupsByName[nameKey] == nil {
			groupsByName[nameKey] = make(map[uint32]bool)
		}
		groupsByName[nameKey][resource.Entry.Group] = true
	}
	for resourceIndex := range resources {
		resource := &resources[resourceIndex]
		resource.Category = renderCategory(resource.Name)
		if resource.Entry.Group == 0x40606100 {
			resource.LODName = "LOD1"
			continue
		}
		if resource.Entry.Group == 0x40606000 && groupsByName[strings.ToLower(resource.Name)][0x40606100] {
			resource.LODName = "LOD0"
		}
	}
}

func renderResourceName(resource renderResource) string {
	identity := strings.TrimSuffix(dbpf.ResourceName(resource.Ordinal, resource.Entry), ".bin")
	kind := renderResourceKind(resource.Entry.Type)
	groupName := resource.GroupName
	if groupName == "" {
		groupName = fmt.Sprintf("group_%08x", resource.Entry.Group)
	}
	if resource.Name == "" {
		return filepath.Join("unresolved", safeRenderName(groupName), kind, identity+".gltf")
	}
	name := safeRenderName(resource.Name)
	if resource.LODName != "" {
		name += "_" + resource.LODName
	}
	return filepath.Join(resource.Category, kind, name+".gltf")
}

func renderResourceKind(typeCode uint32) string {
	switch typeCode {
	case rw4.ResourceType:
		return "rw4"
	case gmsh.ResourceType:
		return "gmdl"
	default:
		return fmt.Sprintf("type_%08x", typeCode)
	}
}

func applyDSRenderPath(resource *renderResource, payloadPath string) {
	parts := strings.Split(filepath.ToSlash(payloadPath), "/")
	if len(parts) > 0 && parts[0] == "resource" {
		parts = parts[1:]
	}
	if len(parts) < 3 {
		return
	}
	resource.Category = filepath.Join(parts[:len(parts)-2]...)
	if parts[0] == "unresolved" {
		return
	}
	name := strings.TrimSuffix(parts[len(parts)-1], filepath.Ext(parts[len(parts)-1]))
	if strings.HasSuffix(strings.ToUpper(name), "_LOD0") || strings.HasSuffix(strings.ToUpper(name), "_LOD1") {
		resource.LODName = strings.ToUpper(name[len(name)-4:])
		name = name[:len(name)-5]
	}
	resource.Name = name
}

func applyRenderSelector(resource *renderResource, selector string) {
	if selector == "" {
		return
	}
	identity := strings.TrimSuffix(dbpf.ResourceName(resource.Ordinal, resource.Entry), ".bin")
	name := strings.TrimSuffix(filepath.Base(selector), filepath.Ext(selector))
	if strings.EqualFold(name, identity) {
		return
	}
	if _, err := strconv.Atoi(name); err == nil {
		return
	}
	if strings.HasSuffix(strings.ToUpper(name), "_LOD0") || strings.HasSuffix(strings.ToUpper(name), "_LOD1") {
		resource.LODName = strings.ToUpper(name[len(name)-4:])
		name = name[:len(name)-5]
	}
	resource.Name = name
	resource.Category = renderCategory(name)
}

func renderDisplayName(name string) string {
	return strings.TrimSuffix(name, "~")
}

func renderCategory(name string) string {
	parts := strings.Split(strings.ToLower(name), "_")
	if len(parts) == 0 || parts[0] == "" {
		return "unresolved"
	}
	switch parts[0] {
	case "ce":
		return filepath.Join("creature", renderPartCategory(parts))
	case "cl":
		return filepath.Join("cell", renderPartCategory(parts))
	case "ta":
		return filepath.Join("tribal", renderAccessoryCategory(parts))
	case "ca":
		return filepath.Join("civilization", renderAccessoryCategory(parts))
	case "sa", "space":
		return filepath.Join("space", renderAccessoryCategory(parts))
	case "ap1":
		return "adventure"
	case "ep1":
		return "expansion"
	case "ui", "loading", "planet", "paint":
		return "ui"
	case "feature", "flower", "frost", "ice":
		return "world"
	case "arm01", "arm02", "arm03", "arm04", "arm05", "arm06", "arm07", "arm08", "arm09", "arm10", "arm11", "arm12",
		"leg01", "leg02", "leg03", "leg04", "leg05", "leg06", "leg07", "leg08", "leg09", "leg10", "leg11", "leg12", "axes":
		return "rig"
	default:
		if strings.Contains(strings.ToLower(name), "background") {
			return "ui"
		}
		return "misc"
	}
}

func renderPartCategory(parts []string) string {
	if len(parts) < 2 {
		return "misc"
	}
	switch parts[1] {
	case "weapon":
		return "weapons"
	case "grasper", "graspertech":
		return "graspers"
	case "sense", "senseeye", "sensetech":
		return "senses"
	case "movement", "movementtech":
		return "movement"
	case "mouth", "mouthtech":
		return "mouths"
	case "details", "detail":
		return "details"
	case "limb", "leg", "vertebra", "vertebra-hull":
		return "limbs"
	case "wing":
		return "wings"
	case "chest", "helmet", "shoulder":
		return "body"
	case "cell":
		return "cell-parts"
	default:
		return safeRenderName(parts[1])
	}
}

func renderAccessoryCategory(parts []string) string {
	if len(parts) < 2 {
		return "misc"
	}
	return safeRenderName(parts[1])
}

func safeRenderName(name string) string {
	name = strings.Map(func(character rune) rune {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || character == '-' || character == '_' || character == '.' {
			return character
		}
		return '_'
	}, name)
	name = strings.Trim(name, ".")
	if len(name) > 96 {
		name = name[:96]
	}
	if name == "" {
		return "unnamed"
	}
	return name
}

func writeRenderDirectoryIndex(destinationPath string, resources []renderResource) error {
	indexPath := filepath.Join(destinationPath, "_root.dse")
	w, err := os.OpenFile(indexPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("indexCreate: %w", err)
	}
	writer := bufio.NewWriter(w)
	projectedResources := make([]renderResource, 0, len(resources))
	for _, resource := range resources {
		resourcePath := filepath.Join(destinationPath, renderResourceName(resource))
		_, statErr := os.Stat(resourcePath)
		if statErr == nil {
			projectedResources = append(projectedResources, resource)
			continue
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			_ = w.Close()
			return fmt.Errorf("resourceStat[%d]: %w", resource.Ordinal, statErr)
		}
	}
	_, err = fmt.Fprintln(writer, "// darkspin gltf resource index dse v1")
	if err == nil {
		_, err = fmt.Fprintf(writer, "RENDERDIRECTORY %q\n", filepath.Base(destinationPath))
	}
	if err == nil {
		_, err = fmt.Fprintln(writer, "\tVERSION 1")
	}
	if err == nil {
		_, err = fmt.Fprintf(writer, "\tNUMRESOURCES %d\n", len(projectedResources))
	}
	for _, resource := range projectedResources {
		if err == nil {
			_, err = fmt.Fprintf(writer, "\t\tRESOURCE %q\n", filepath.ToSlash(renderResourceName(resource)))
		}
		if err == nil {
			_, err = fmt.Fprintf(writer, "\t\t\tORDINAL %d\n\t\t\tTYPE 0x%08X\n\t\t\tGROUP 0x%08X\n\t\t\tINSTANCE 0x%016X\n", resource.Ordinal, resource.Entry.Type, resource.Entry.Group, resource.Entry.Instance)
		}
		if err == nil && resource.Name == "" {
			_, err = fmt.Fprintln(writer, "\t\t\tNAME? NULL\n\t\t\tNAMESOURCE? NULL")
		}
		if err == nil && resource.Name != "" {
			_, err = fmt.Fprintf(writer, "\t\t\tNAME? %q\n\t\t\tNAMESOURCE? %q\n", resource.Name, "SporeModder-FX reg_file.txt")
		}
		if err == nil && resource.GroupName == "" {
			_, err = fmt.Fprintln(writer, "\t\t\tGROUPNAME? NULL")
		}
		if err == nil && resource.GroupName != "" {
			_, err = fmt.Fprintf(writer, "\t\t\tGROUPNAME? %q\n", resource.GroupName)
		}
		if err == nil && resource.LODName == "" {
			_, err = fmt.Fprintln(writer, "\t\t\tLOD? NULL")
		}
		if err == nil && resource.LODName != "" {
			_, err = fmt.Fprintf(writer, "\t\t\tLOD? %q\n", resource.LODName)
		}
		if err == nil {
			_, err = fmt.Fprintf(writer, "\t\t\tCATEGORY %q\n", filepath.ToSlash(resource.Category))
		}
	}
	if err == nil {
		err = writer.Flush()
	}
	closeErr := w.Close()
	if err != nil {
		return fmt.Errorf("indexOutput: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("indexClose: %w", closeErr)
	}
	return nil
}
