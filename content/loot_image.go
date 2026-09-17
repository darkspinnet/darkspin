package content

import (
	"bytes"
	"context"
	"encoding/binary"
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

const (
	lootPropertyType = uint32(0x1bced3d7)
	pngResourceType  = uint32(0x2f7d0004)
)

var pngSignature = []byte{'\x89', 'P', 'N', 'G', '\r', '\n', '\x1a', '\n'}

// LootRigblock is one authored build-103 base item definition.
type LootRigblock struct {
	ID             uint16
	ContentFlags   uint8
	MinimumLevel   uint32
	MaximumLevel   uint32
	IsUniqueFamily bool
	SlotType       string
	ClassTypes     []string
	ScienceTypes   []string
	ImageGroup     uint32
	ImageInstance  uint64
	ImageName      string
	WeaponNoun     string
	SourceOrdinal  int
}

func prepareLootImages(ctx context.Context, installPath, staticPath string) error {
	rigblocks, err := LoadLootRigblocks(ctx, filepath.Join(installPath, "Data", "AssetData_Binary.package"))
	if err != nil {
		return fmt.Errorf("referenceLoad: %w", err)
	}
	r, pkg, err := openWebPackage(filepath.Join(installPath, "Data", "UI.package"))
	if err != nil {
		return fmt.Errorf("uiOpen: %w", err)
	}
	defer r.Close()
	entriesByID := make(map[string]dbpf.Entry, len(pkg.Entries))
	for _, entry := range pkg.Entries {
		entriesByID[webResourceID(entry.Type, entry.Group, entry.Instance)] = entry
	}
	for index, rigblock := range rigblocks {
		err = ctx.Err()
		if err != nil {
			return fmt.Errorf("context[%d]: %w", index, err)
		}
		entry, isFound := entriesByID[webResourceID(pngResourceType, rigblock.ImageGroup, rigblock.ImageInstance)]
		if !isFound {
			return fmt.Errorf("imageMissing[%d]: rigblock %d key 0x%08X!%s", index,
				rigblock.ID, rigblock.ImageGroup, rigblock.ImageName)
		}
		decoded, openErr := pkg.Open(entry)
		if openErr != nil {
			return fmt.Errorf("imageOpen[%d]: %w", index, openErr)
		}
		payload, readErr := io.ReadAll(decoded)
		if readErr != nil {
			return fmt.Errorf("imageRead[%d]: %w", index, readErr)
		}
		if !bytes.HasPrefix(payload, pngSignature) {
			return fmt.Errorf("imageFormat[%d]: rigblock %d is not PNG", index, rigblock.ID)
		}
		target := fmt.Sprintf("assets/images/loot/%d.png", rigblock.ID)
		err = writePreparedWebPayload(staticPath, target, payload)
		if err != nil {
			return fmt.Errorf("imageWrite[%d]: %w", index, err)
		}
	}
	return nil
}

// LoadLootRigblocks reads every standard loot rigblock from AssetData_Binary.package.
func LoadLootRigblocks(ctx context.Context, packagePath string) ([]LootRigblock, error) {
	r, pkg, err := openWebPackage(packagePath)
	if err != nil {
		return nil, fmt.Errorf("packageOpen: %w", err)
	}
	defer r.Close()
	rigblocksByID := make(map[uint16]LootRigblock)
	for ordinal, entry := range pkg.Entries {
		if entry.Type != lootPropertyType {
			continue
		}
		err = ctx.Err()
		if err != nil {
			return nil, fmt.Errorf("context[%d]: %w", ordinal, err)
		}
		decoded, openErr := pkg.Open(entry)
		if openErr != nil {
			return nil, fmt.Errorf("propertyOpen[%d]: %w", ordinal, openErr)
		}
		payload, readErr := io.ReadAll(decoded)
		if readErr != nil {
			return nil, fmt.Errorf("propertyRead[%d]: %w", ordinal, readErr)
		}
		rigblock, isLoot, parseErr := parseLootRigblock(payload)
		if parseErr != nil {
			return nil, fmt.Errorf("propertyParse[%d]: %w", ordinal, parseErr)
		}
		if !isLoot {
			continue
		}
		rigblock.SourceOrdinal = ordinal
		existing, isFound := rigblocksByID[rigblock.ID]
		if isFound && !sameLootRigblock(existing, rigblock) {
			return nil, fmt.Errorf("rigblockConflict[%d]: %#v/%#v", rigblock.ID, existing, rigblock)
		}
		rigblocksByID[rigblock.ID] = rigblock
	}
	rigblocks := make([]LootRigblock, 0, len(rigblocksByID))
	for _, rigblock := range rigblocksByID {
		rigblocks = append(rigblocks, rigblock)
	}
	sort.Slice(rigblocks, func(first, second int) bool {
		return rigblocks[first].ID < rigblocks[second].ID
	})
	return rigblocks, nil
}

func parseLootRigblock(payload []byte) (LootRigblock, bool, error) {
	if len(payload) < 120 {
		return LootRigblock{}, false, fmt.Errorf("payloadSize: got %d, need 120", len(payload))
	}
	var rigblock LootRigblock
	isRigblockFound := false
	isImageFound := false
	isSlotFound := false
	classSet := make(map[string]bool, 3)
	scienceSet := make(map[string]bool, 5)
	for _, field := range printableNullStrings(payload) {
		rawID, isUniqueFamily, isIDFound := lootRigblockIdentity(field)
		if isIDFound {
			if len(rawID) != 8 {
				return LootRigblock{}, false, fmt.Errorf("rigblockID: %q", rawID)
			}
			id, err := strconv.ParseUint(rawID, 16, 16)
			if err != nil {
				return LootRigblock{}, false, fmt.Errorf("rigblockID: %w", err)
			}
			rigblock.ID = uint16(id)
			rigblock.IsUniqueFamily = isUniqueFamily
			isRigblockFound = true
			continue
		}
		if slotType, isFound := lootSlotType(field); isFound {
			if isSlotFound && rigblock.SlotType != slotType {
				return LootRigblock{}, false, fmt.Errorf("slotConflict: %q/%q", rigblock.SlotType, slotType)
			}
			rigblock.SlotType = slotType
			isSlotFound = true
			continue
		}
		if field == "ravager" || field == "sentinel" || field == "tempest" {
			classSet[field] = true
			continue
		}
		if field == "plasma" || field == "bio" || field == "cyber" || field == "necro" || field == "chrono" {
			scienceSet[field] = true
			continue
		}
		if strings.HasPrefix(strings.ToLower(field), "pc_") && strings.HasSuffix(field, ".Noun") {
			if rigblock.WeaponNoun == "" {
				rigblock.WeaponNoun = field
			}
			continue
		}
		separator := strings.IndexByte(field, '!')
		if separator != 10 || !strings.HasPrefix(field, "0x") ||
			!strings.EqualFold(filepath.Ext(field), ".png") {
			continue
		}
		group, err := strconv.ParseUint(field[2:separator], 16, 32)
		if err != nil {
			return LootRigblock{}, false, fmt.Errorf("imageGroup: %w", err)
		}
		name := field[separator+1:]
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		candidateGroup := uint32(group)
		candidateInstance := uint64(webHashID(stem))
		if isImageFound && (rigblock.ImageGroup != candidateGroup || rigblock.ImageInstance != candidateInstance || rigblock.ImageName != name) {
			return LootRigblock{}, false, fmt.Errorf("imageConflict: %q/%q", rigblock.ImageName, name)
		}
		rigblock.ImageGroup = candidateGroup
		rigblock.ImageInstance = candidateInstance
		rigblock.ImageName = name
		isImageFound = true
	}
	if !isRigblockFound {
		return LootRigblock{}, false, nil
	}
	rigblock.MinimumLevel = binary.LittleEndian.Uint32(payload[64:])
	rigblock.MaximumLevel = binary.LittleEndian.Uint32(payload[68:])
	rigblock.ContentFlags = payload[108]
	if rigblock.MinimumLevel > rigblock.MaximumLevel || rigblock.MaximumLevel > 1000 {
		return LootRigblock{}, false, fmt.Errorf("levels: %d..%d", rigblock.MinimumLevel, rigblock.MaximumLevel)
	}
	if (payload[104] != 0) != rigblock.IsUniqueFamily {
		return LootRigblock{}, false, errors.New("familyConflict")
	}
	if !isImageFound {
		return LootRigblock{}, false, errors.New("imageMissing")
	}
	if !isSlotFound {
		return LootRigblock{}, false, errors.New("slotMissing")
	}
	for _, classType := range []string{"ravager", "sentinel", "tempest"} {
		if classSet[classType] {
			rigblock.ClassTypes = append(rigblock.ClassTypes, classType)
		}
	}
	if len(rigblock.ClassTypes) == 0 {
		return LootRigblock{}, false, errors.New("classMissing")
	}
	for _, scienceType := range []string{"plasma", "bio", "cyber", "necro", "chrono"} {
		if scienceSet[scienceType] {
			rigblock.ScienceTypes = append(rigblock.ScienceTypes, scienceType)
		}
	}
	if len(rigblock.ScienceTypes) == 0 {
		return LootRigblock{}, false, errors.New("scienceMissing")
	}
	if rigblock.SlotType == "weapon" && rigblock.WeaponNoun == "" {
		return LootRigblock{}, false, errors.New("weaponNounMissing")
	}
	return rigblock, true, nil
}

func lootRigblockIdentity(field string) (string, bool, bool) {
	const ordinaryPrefix = "LootRigblockNames!0x"
	if strings.HasPrefix(field, ordinaryPrefix) {
		return strings.TrimPrefix(field, ordinaryPrefix), false, true
	}
	const uniquePrefix = "LootUniqueRigblockNames!0x"
	if strings.HasPrefix(field, uniquePrefix) {
		return strings.TrimPrefix(field, uniquePrefix), true, true
	}
	return "", false, false
}

func lootSlotType(field string) (string, bool) {
	const prefix = "@palette_config!CE_Category_"
	if !strings.HasPrefix(field, prefix) {
		return "", false
	}
	slotType, isFound := map[string]string{
		"Weapon": "weapon", "Hands": "grasper", "Feet": "foot",
		"Defense": "defense", "Offense": "offense", "Utility": "utility",
	}[strings.TrimPrefix(field, prefix)]
	return slotType, isFound
}

func sameLootRigblock(first, second LootRigblock) bool {
	return first.ID == second.ID && first.ContentFlags == second.ContentFlags &&
		first.MinimumLevel == second.MinimumLevel &&
		first.MaximumLevel == second.MaximumLevel && first.IsUniqueFamily == second.IsUniqueFamily &&
		first.SlotType == second.SlotType &&
		strings.Join(first.ClassTypes, ",") == strings.Join(second.ClassTypes, ",") &&
		strings.Join(first.ScienceTypes, ",") == strings.Join(second.ScienceTypes, ",") &&
		first.ImageGroup == second.ImageGroup &&
		first.ImageInstance == second.ImageInstance && first.ImageName == second.ImageName &&
		strings.EqualFold(first.WeaponNoun, second.WeaponNoun)
}

func printableNullStrings(payload []byte) []string {
	fields := make([]string, 0)
	start := -1
	for index, character := range payload {
		if character >= 0x20 && character <= 0x7e {
			if start < 0 {
				start = index
			}
			continue
		}
		if character == 0 && start >= 0 {
			fields = append(fields, string(payload[start:index]))
		}
		start = -1
	}
	return fields
}

func webHashID(name string) uint32 {
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

func openWebPackage(packagePath string) (*os.File, *dbpf.Reader, error) {
	r, err := os.Open(packagePath)
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
		return nil, nil, fmt.Errorf("packageRead: %w", err)
	}
	return r, pkg, nil
}

func writePreparedWebPayload(staticPath, target string, payload []byte) error {
	targetPath, err := webTargetPath(staticPath, target)
	if err != nil {
		return fmt.Errorf("targetPath: %w", err)
	}
	existing, err := os.ReadFile(targetPath)
	if err == nil && bytes.Equal(existing, payload) {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("targetRead: %w", err)
	}
	err = os.MkdirAll(filepath.Dir(targetPath), 0o755)
	if err != nil {
		return fmt.Errorf("targetCreate: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(targetPath), ".web-*.tmp")
	if err != nil {
		return fmt.Errorf("temporaryCreate: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	_, err = temporary.Write(payload)
	if err != nil {
		_ = temporary.Close()
		return fmt.Errorf("temporaryWrite: %w", err)
	}
	err = temporary.Close()
	if err != nil {
		return fmt.Errorf("temporaryClose: %w", err)
	}
	err = os.Remove(targetPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("targetRemove: %w", err)
	}
	err = os.Rename(temporaryPath, targetPath)
	if err != nil {
		return fmt.Errorf("targetInstall: %w", err)
	}
	return nil
}
