package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/darkspinnet/darkspin/content/render/prop"
)

// ReferencedPropertyAliases derives missing audioProp names from named owners
// whose property keys point at exactly one otherwise unnamed audio event.
func ReferencedPropertyAliases(directoryPath, audioPropertyPath string, names map[uint32]string) (map[uint32]string, error) {
	targets, err := audioPropertyInstances(audioPropertyPath)
	if err != nil {
		return nil, fmt.Errorf("targetRead: %w", err)
	}
	entries, err := os.ReadDir(directoryPath)
	if err != nil {
		return nil, fmt.Errorf("directoryRead: %w", err)
	}
	candidatesByInstance := make(map[uint32]map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".package") {
			continue
		}
		packagePath := filepath.Join(directoryPath, entry.Name())
		if strings.EqualFold(packagePath, audioPropertyPath) || strings.EqualFold(entry.Name(), "Audio.package") {
			continue
		}
		err = collectPropertyReferences(packagePath, targets, names, candidatesByInstance)
		if err != nil {
			return nil, fmt.Errorf("packageReferences[%s]: %w", entry.Name(), err)
		}
	}
	aliases := make(map[uint32]string)
	for instanceID, candidates := range candidatesByInstance {
		if names[instanceID] != "" || len(candidates) != 1 {
			continue
		}
		orderedCandidates := make([]string, 0, len(candidates))
		for candidate := range candidates {
			orderedCandidates = append(orderedCandidates, candidate)
		}
		sort.Strings(orderedCandidates)
		aliases[instanceID] = orderedCandidates[0]
	}
	return aliases, nil
}

func audioPropertyInstances(sourcePath string) (map[uint32]bool, error) {
	pkg, r, err := openPackage(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("packageOpen: %w", err)
	}
	defer r.Close()
	instances := make(map[uint32]bool, len(pkg.Entries))
	for _, entry := range pkg.Entries {
		if entry.Type == prop.AudioResourceType {
			instances[uint32(entry.Instance)] = true
		}
	}
	return instances, nil
}

func collectPropertyReferences(sourcePath string, targets map[uint32]bool, names map[uint32]string, candidatesByInstance map[uint32]map[string]bool) error {
	pkg, r, err := openPackage(sourcePath)
	if err != nil {
		return fmt.Errorf("packageOpen: %w", err)
	}
	defer r.Close()
	for ordinal, entry := range pkg.Entries {
		if !prop.IsResourceType(entry.Type) || entry.Type == prop.AudioResourceType {
			continue
		}
		ownerName := strings.TrimSuffix(names[uint32(entry.Instance)], "~")
		if ownerName == "" {
			continue
		}
		payloadReader, openErr := pkg.Open(entry)
		if openErr != nil {
			return fmt.Errorf("resourceOpen[%d]: %w", ordinal, openErr)
		}
		payload, readErr := io.ReadAll(payloadReader)
		if readErr != nil {
			return fmt.Errorf("resourceRead[%d]: %w", ordinal, readErr)
		}
		document, decodeErr := prop.Decode(payload)
		if decodeErr != nil {
			continue
		}
		for _, property := range document.Properties {
			roleName, isFound := prop.Name(property.ID)
			if !isFound {
				continue
			}
			for itemIndex, item := range property.Items {
				instanceID, alias, isReference := propertyAudioReference(property.Type, item, ownerName, roleName)
				if !isReference || !targets[instanceID] {
					continue
				}
				if len(property.Items) > 1 {
					alias += fmt.Sprintf("_%02d", itemIndex+1)
				}
				if candidatesByInstance[instanceID] == nil {
					candidatesByInstance[instanceID] = make(map[string]bool)
				}
				candidatesByInstance[instanceID][alias] = true
			}
		}
	}
	return nil
}

func propertyAudioReference(propertyType uint16, item []byte, ownerName, roleName string) (uint32, string, bool) {
	switch propertyType {
	case prop.TypeKey:
		if len(item) < 4 {
			return 0, "", false
		}
		return binary.LittleEndian.Uint32(item[:4]), ownerName + "_" + roleName, true
	case prop.TypeInt32, prop.TypeUInt32:
		if len(item) != 4 || !isAudioRole(roleName) {
			return 0, "", false
		}
		return binary.BigEndian.Uint32(item), ownerName + "_" + roleName, true
	case prop.TypeString8:
		if len(item) < 4 || !isAudioRole(roleName) {
			return 0, "", false
		}
		audioName := string(item[4:])
		return hashName(audioName), audioName, true
	default:
		return 0, "", false
	}
}

func isAudioRole(roleName string) bool {
	lowerName := strings.ToLower(roleName)
	for _, marker := range []string{"sound", "audio", "music", "voice", "vox", "sfx", "ambience"} {
		if strings.Contains(lowerName, marker) {
			return true
		}
	}
	return false
}

func hashName(name string) uint32 {
	hash := uint32(0x811c9dc5)
	for index := 0; index < len(name); index++ {
		hash *= 0x01000193
		character := name[index]
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}
		hash ^= uint32(character)
	}
	return hash
}

func openPackage(sourcePath string) (*dbpf.Reader, *os.File, error) {
	r, err := os.Open(sourcePath)
	if err != nil {
		return nil, nil, fmt.Errorf("fileOpen: %w", err)
	}
	fi, err := r.Stat()
	if err != nil {
		_ = r.Close()
		return nil, nil, fmt.Errorf("fileStat: %w", err)
	}
	pkg, err := dbpf.NewReader(r, fi.Size())
	if err != nil {
		_ = r.Close()
		return nil, nil, fmt.Errorf("dbpfRead: %w", err)
	}
	return pkg, r, nil
}
