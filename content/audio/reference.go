package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	references := make([]propertyReference, 0, len(targets)*2)
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".package") {
			continue
		}
		packagePath := filepath.Join(directoryPath, entry.Name())
		if strings.EqualFold(entry.Name(), "Audio.package") {
			continue
		}
		packageReferences, referenceErr := collectPropertyReferences(packagePath, targets)
		err = referenceErr
		if err != nil {
			return nil, fmt.Errorf("packageReferences[%s]: %w", entry.Name(), err)
		}
		references = append(references, packageReferences...)
	}
	return resolveReferencedPropertyAliases(references, targets, names), nil
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

func collectPropertyReferences(sourcePath string, audioTargets map[uint32]bool) ([]propertyReference, error) {
	pkg, r, err := openPackage(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("packageOpen: %w", err)
	}
	defer r.Close()
	references := make([]propertyReference, 0)
	for ordinal, entry := range pkg.Entries {
		if !prop.IsResourceType(entry.Type) {
			continue
		}
		payloadReader, openErr := pkg.Open(entry)
		if openErr != nil {
			return nil, fmt.Errorf("resourceOpen[%d]: %w", ordinal, openErr)
		}
		payload, readErr := io.ReadAll(payloadReader)
		if readErr != nil {
			return nil, fmt.Errorf("resourceRead[%d]: %w", ordinal, readErr)
		}
		document, decodeErr := prop.Decode(payload)
		if decodeErr != nil {
			continue
		}
		for _, property := range document.Properties {
			roleName, isFound := prop.Name(property.ID)
			if !isFound {
				roleName = ""
			}
			roleName = ReadablePointerName(roleName)
			for itemIndex, item := range property.Items {
				instanceID, isReference := propertyReferenceInstance(property.Type, item, roleName, audioTargets)
				if !isReference {
					continue
				}
				targetName := ""
				if property.Type == prop.TypeString8 {
					targetName = string(item[4:])
				}
				references = append(references, propertyReference{
					ownerID: uint32(entry.Instance), targetID: instanceID, targetName: targetName, roleName: roleName,
					itemIndex: itemIndex, itemCount: len(property.Items),
				})
			}
		}
	}
	return references, nil
}

func propertyReferenceInstance(propertyType uint16, item []byte, roleName string, audioTargets map[uint32]bool) (uint32, bool) {
	switch propertyType {
	case prop.TypeKey:
		if len(item) < 4 {
			return 0, false
		}
		return binary.LittleEndian.Uint32(item[:4]), true
	case prop.TypeInt32, prop.TypeUInt32:
		if len(item) != 4 || !isAudioRole(roleName) {
			return 0, false
		}
		instanceID := binary.BigEndian.Uint32(item)
		return instanceID, audioTargets[instanceID]
	case prop.TypeString8:
		if len(item) < 4 || !isAudioRole(roleName) {
			return 0, false
		}
		audioName := string(item[4:])
		instanceID := hashName(audioName)
		return instanceID, audioTargets[instanceID]
	default:
		return 0, false
	}
}

func resolveReferencedPropertyAliases(references []propertyReference, targets map[uint32]bool, names map[uint32]string) map[uint32]string {
	resolvedNames := make(map[uint32]string, len(names)+len(references))
	for instanceID, name := range names {
		resolvedNames[instanceID] = strings.TrimSuffix(name, "~")
	}
	for _, reference := range references {
		if reference.targetName != "" && resolvedNames[reference.targetID] == "" {
			resolvedNames[reference.targetID] = reference.targetName
		}
	}
	for {
		candidatesByInstance := make(map[uint32]map[string]bool)
		for _, reference := range references {
			if resolvedNames[reference.targetID] != "" {
				continue
			}
			ownerName := resolvedNames[reference.ownerID]
			if ownerName == "" {
				continue
			}
			alias := ownerName
			if reference.roleName != "" {
				alias += "_" + reference.roleName
			}
			if reference.itemCount > 1 {
				alias += fmt.Sprintf("_%02d", reference.itemIndex+1)
			}
			if candidatesByInstance[reference.targetID] == nil {
				candidatesByInstance[reference.targetID] = make(map[string]bool)
			}
			candidatesByInstance[reference.targetID][alias] = true
		}
		addedCount := 0
		for instanceID, candidates := range candidatesByInstance {
			if len(candidates) != 1 {
				continue
			}
			for alias := range candidates {
				resolvedNames[instanceID] = alias
				addedCount++
			}
		}
		if addedCount == 0 {
			break
		}
	}
	aliases := make(map[uint32]string)
	for instanceID := range targets {
		if names[instanceID] == "" && resolvedNames[instanceID] != "" {
			aliases[instanceID] = resolvedNames[instanceID]
		}
	}
	return aliases
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
