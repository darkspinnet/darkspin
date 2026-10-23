// Package spore reads creation resources from a local Spore installation.
package spore

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"
)

const CreatureType = uint32(0x0f43029a)
const CreatureGroup = uint32(0x40626200)

// Creation identifies an assembled creature and its source package.
type Creation struct {
	ID          string
	Name        string
	PackagePath string
	Instance    uint64
	IsPersonal  bool
}

// Installation accepts the installation root or its executable.
func Installation(path string) (string, error) {
	path, err := filepath.Abs(strings.TrimSpace(strings.Trim(path, `"`)))
	if err != nil {
		return "", fmt.Errorf("sourcePath: %w", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("sourceStat: %w", err)
	}
	if !fi.IsDir() {
		path = filepath.Dir(path)
	}
	if strings.EqualFold(filepath.Base(path), "SporeBin") {
		path = filepath.Dir(path)
	}
	for _, name := range []string{"Spore_Game.package", "Spore_Graphics.package", "Spore_Content.package"} {
		fi, err = os.Stat(filepath.Join(path, "Data", name))
		if err != nil {
			return "", fmt.Errorf("packageStat: %w", err)
		}
		if fi.IsDir() {
			return "", errors.New("Spore package is a directory")
		}
	}
	return path, nil
}

// Scan reads installed creations followed by local downloads and editor saves.
// Later packages supersede earlier revisions with the same resource identity.
func Scan(ctx context.Context, root, userPath string) ([]Creation, error) {
	paths := []string{filepath.Join(root, "Data", "Spore_Content.package")}
	if userPath != "" {
		paths = append(paths, filepath.Join(userPath, "Pollination.package"), filepath.Join(userPath, "EditorSaves.package"))
	}
	creationsByInstance := make(map[uint64]Creation)
	for index, path := range paths {
		err := scanPackage(ctx, path, index != 0, creationsByInstance)
		if errors.Is(err, os.ErrNotExist) && index != 0 {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("creationScan[%s]: %w", filepath.Base(path), err)
		}
	}
	creations := make([]Creation, 0, len(creationsByInstance))
	for _, creation := range creationsByInstance {
		creations = append(creations, creation)
	}
	sort.Slice(creations, func(left, right int) bool {
		if creations[left].IsPersonal != creations[right].IsPersonal {
			return creations[left].IsPersonal
		}
		return strings.ToLower(creations[left].Name) < strings.ToLower(creations[right].Name)
	})
	return creations, nil
}

func scanPackage(ctx context.Context, path string, isPersonal bool, creationsByInstance map[uint64]Creation) error {
	r, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("packageOpen: %w", err)
	}
	defer closeSource(r)
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("packageStat: %w", err)
	}
	pkg, err := openCreationPackage(r, fi.Size())
	if err != nil {
		return fmt.Errorf("packageIndex: %w", err)
	}
	metadataEntries := make(map[uint64]creationEntry)
	for _, entry := range pkg.entries {
		if entry.Type == 0x030bdee3 && entry.Group == CreatureGroup {
			metadataEntries[entry.Instance] = entry
		}
	}
	for _, entry := range pkg.entries {
		if entry.Type != CreatureType || entry.Group != CreatureGroup {
			continue
		}
		err = ctx.Err()
		if err != nil {
			return fmt.Errorf("scanContext: %w", err)
		}
		name := fmt.Sprintf("Creature %08X", entry.Instance)
		metadataEntry, isFound := metadataEntries[entry.Instance]
		if isFound {
			payload, readErr := readEntry(pkg, metadataEntry)
			if readErr != nil {
				return fmt.Errorf("metadataRead: %w", readErr)
			}
			metadataName := creationName(payload)
			if metadataName != "" {
				name = metadataName
			}
		}
		digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%016x", path, entry.Instance)))
		creationsByInstance[entry.Instance] = Creation{ID: hex.EncodeToString(digest[:16]), Name: name, PackagePath: path, Instance: entry.Instance, IsPersonal: isPersonal}
	}
	return nil
}

// Metadata stores length-prefixed UTF-16LE strings with big-endian lengths.
func creationName(payload []byte) string {
	for offset := 64; offset+8 < len(payload) && offset < 128; offset++ {
		author, next, isAuthor := metadataString(payload, offset)
		if !isAuthor || author == "" {
			continue
		}
		name, end, isName := metadataString(payload, next)
		if isName && end > next && name != "" {
			return name
		}
	}
	return ""
}

func metadataString(payload []byte, offset int) (string, int, bool) {
	if offset+4 > len(payload) {
		return "", offset, false
	}
	count := int(binary.BigEndian.Uint32(payload[offset:]))
	if count <= 0 || count > 256 || offset+4+count*2 > len(payload) {
		return "", offset, false
	}
	units := make([]uint16, count)
	for index := range units {
		units[index] = binary.LittleEndian.Uint16(payload[offset+4+index*2:])
		if units[index] < 32 || units[index] == 0xffff {
			return "", offset, false
		}
	}
	return string(utf16.Decode(units)), offset + 4 + count*2, true
}

// Resource extracts one decoded resource, without writing into its source.
func Resource(creation Creation, resourceType, group uint32) ([]byte, error) {
	r, err := os.Open(creation.PackagePath)
	if err != nil {
		return nil, fmt.Errorf("resourceOpen: %w", err)
	}
	defer closeSource(r)
	fi, err := r.Stat()
	if err != nil {
		return nil, fmt.Errorf("resourceStat: %w", err)
	}
	pkg, err := openCreationPackage(r, fi.Size())
	if err != nil {
		return nil, fmt.Errorf("resourceIndex: %w", err)
	}
	for _, entry := range pkg.entries {
		if entry.Type == resourceType && entry.Group == group && entry.Instance == creation.Instance {
			payload, readErr := readEntry(pkg, entry)
			if readErr != nil {
				return nil, fmt.Errorf("resourceRead: %w", readErr)
			}
			return payload, nil
		}
	}
	return nil, os.ErrNotExist
}

func closeSource(r *os.File) {
	err := r.Close()
	if err != nil {
		// Read-only handles have no pending writes; their resources were already
		// decoded or the read error returned before this final handle release.
		return
	}
}
