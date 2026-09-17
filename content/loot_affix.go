package content

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

const (
	LootAttributeCount       = 115
	lootPrefixModifierOffset = 36
	lootSuffixModifierOffset = 56
	lootPrefixPropertyType   = uint32(0x6a1812c6)
	lootSuffixPropertyType   = uint32(0x447dc2e5)
)

// LootAffix is one authored build-103 prefix or suffix definition.
type LootAffix struct {
	ID              uint16
	Kind            string
	IsUniqueFamily  bool
	IsBasicEligible bool
	MinimumLevel    uint32
	MaximumLevel    uint32
	ClassTypes      []string
	ScienceTypes    []string
	Modifier        []float32
	SourceOrdinal   int
}

// LoadLootAffixes reads every generated loot prefix and suffix from AssetData_Binary.package.
func LoadLootAffixes(ctx context.Context, packagePath string) ([]LootAffix, error) {
	r, pkg, err := openWebPackage(packagePath)
	if err != nil {
		return nil, fmt.Errorf("packageOpen: %w", err)
	}
	defer r.Close()
	affixByKey := make(map[string]LootAffix, 666)
	for ordinal, entry := range pkg.Entries {
		kind := ""
		minimumOffset := 0
		maximumOffset := 0
		switch entry.Type {
		case lootPrefixPropertyType:
			kind = "prefix"
			minimumOffset = 28
			maximumOffset = 32
		case lootSuffixPropertyType:
			kind = "suffix"
			minimumOffset = 20
			maximumOffset = 24
		default:
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
		affix, parseErr := parseLootAffix(payload, kind, minimumOffset, maximumOffset)
		if parseErr != nil {
			return nil, fmt.Errorf("propertyParse[%d]: %w", ordinal, parseErr)
		}
		affix.SourceOrdinal = ordinal
		key := fmt.Sprintf("%s/%d", affix.Kind, affix.ID)
		if _, isFound := affixByKey[key]; isFound {
			return nil, fmt.Errorf("affixDuplicate[%s]", key)
		}
		affixByKey[key] = affix
	}
	affixes := make([]LootAffix, 0, len(affixByKey))
	for _, affix := range affixByKey {
		affixes = append(affixes, affix)
	}
	sort.Slice(affixes, func(first, second int) bool {
		if affixes[first].Kind != affixes[second].Kind {
			return affixes[first].Kind < affixes[second].Kind
		}
		return affixes[first].ID < affixes[second].ID
	})
	return affixes, nil
}

func parseLootAffix(payload []byte, kind string, minimumOffset, maximumOffset int) (LootAffix, error) {
	// Build-103 sub_9CD3D0 copies suffix attributes from +56 and prefix
	// attributes from +36 through sub_9C9640. The suffix header includes
	// references and eligibility flags; treating it as attributes shifts every
	// stat by five slots, including Dexterity into DamageReduction.
	modifierOffset := lootPrefixModifierOffset
	if kind == "suffix" {
		modifierOffset = lootSuffixModifierOffset
	}
	modifierEnd := modifierOffset + LootAttributeCount*4
	if len(payload) < modifierEnd {
		return LootAffix{}, fmt.Errorf("payloadSize: got %d, need %d", len(payload), modifierEnd)
	}
	affix := LootAffix{Kind: kind}
	switch kind {
	case "prefix":
		id := binary.LittleEndian.Uint32(payload)
		if id == 0 || id > 338 {
			return LootAffix{}, fmt.Errorf("prefixID: %d", id)
		}
		affix.ID = uint16(id)
	case "suffix":
		id, isUniqueFamily, err := lootSuffixIdentity(printableNullStrings(payload))
		if err != nil {
			return LootAffix{}, fmt.Errorf("suffixID: %w", err)
		}
		affix.ID = id
		affix.IsUniqueFamily = isUniqueFamily
		affix.IsBasicEligible = payload[53] != 0
		if (payload[52] != 0) != affix.IsUniqueFamily {
			return LootAffix{}, errors.New("familyConflict")
		}
	default:
		return LootAffix{}, fmt.Errorf("kind: %q", kind)
	}
	affix.MinimumLevel = binary.LittleEndian.Uint32(payload[minimumOffset:])
	affix.MaximumLevel = binary.LittleEndian.Uint32(payload[maximumOffset:])
	if affix.MinimumLevel == 0 || affix.MinimumLevel > affix.MaximumLevel || affix.MaximumLevel > 1000 {
		return LootAffix{}, fmt.Errorf("levels: %d..%d", affix.MinimumLevel, affix.MaximumLevel)
	}
	classSet := make(map[string]bool, 3)
	scienceSet := make(map[string]bool, 5)
	for _, field := range printableNullStrings(payload) {
		if field == "ravager" || field == "sentinel" || field == "tempest" {
			classSet[field] = true
		}
		if field == "plasma" || field == "bio" || field == "cyber" || field == "necro" || field == "chrono" {
			scienceSet[field] = true
		}
	}
	for _, classType := range []string{"ravager", "sentinel", "tempest"} {
		if classSet[classType] {
			affix.ClassTypes = append(affix.ClassTypes, classType)
		}
	}
	for _, scienceType := range []string{"plasma", "bio", "cyber", "necro", "chrono"} {
		if scienceSet[scienceType] {
			affix.ScienceTypes = append(affix.ScienceTypes, scienceType)
		}
	}
	if len(affix.ClassTypes) == 0 || len(affix.ScienceTypes) == 0 {
		return LootAffix{}, errors.New("compatibilityMissing")
	}
	affix.Modifier = make([]float32, LootAttributeCount)
	for attributeIndex := range affix.Modifier {
		offset := modifierOffset + attributeIndex*4
		modifier := math.Float32frombits(binary.LittleEndian.Uint32(payload[offset:]))
		if math.IsNaN(float64(modifier)) || math.IsInf(float64(modifier), 0) {
			return LootAffix{}, fmt.Errorf("modifier[%d]: %g", attributeIndex, modifier)
		}
		affix.Modifier[attributeIndex] = modifier
	}
	return affix, nil
}

func lootSuffixIdentity(fields []string) (uint16, bool, error) {
	for _, field := range fields {
		for markerIndex, marker := range []string{"LootSuffixNames!0x", "LootUniqueSuffixNames!0x"} {
			if !strings.HasPrefix(field, marker) {
				continue
			}
			rawID := strings.TrimPrefix(field, marker)
			id, err := strconv.ParseUint(rawID, 16, 16)
			if err != nil {
				return 0, false, fmt.Errorf("parse: %w", err)
			}
			if id == 0 {
				return 0, false, errors.New("zero")
			}
			return uint16(id), markerIndex == 1, nil
		}
	}
	return 0, false, errors.New("missing")
}
