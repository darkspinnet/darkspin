package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

const nonPlayerClassAssetType = uint32(0xd117afca)
const combatAttributeAssetType = uint32(0x474940a5)
const nonPlayerClassAssetGroup = uint32(0)
const nonPlayerClassPrefixSize = 0x7c
const classAttributeSize = 88

const NonPlayerAffixLimit = 6

type nonPlayerClassAsset struct {
	instanceID             uint32
	nounName               string
	attributeName          string
	displayName            string
	displayNameLocaleKey   string
	description            string
	descriptionLocaleKey   string
	affixes                []NonPlayerClassAffix
	challengeValue         int32
	npcRank                int32
	npcType                uint32
	creatureType           uint32
	dropTypes              []uint32
	isTargetable           bool
	isPlayerPet            bool
	playerCountHealthScale float32
	aggroRange             float32
	alertRange             float32
	dropAggroRange         float32
	idleMovementSpeed      float32
	baseCombatSpeed        float32
	hitPoint               float32
	powerPoint             float32
	strength               float32
	dexterity              float32
	mind                   float32
	dodgeRating            float32
	resistRating           float32
	criticalRating         float32
}

func writeNonPlayerClasses(ctx context.Context, transaction *sql.Tx, installPath string) error {
	packagePath := filepath.Join(installPath, "Data", "AssetData_Binary.package")
	r, err := os.Open(packagePath)
	if err != nil {
		return fmt.Errorf("packageOpen: %w", err)
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("packageStat: %w", err)
	}
	pkg, err := importPackageReader(ctx, r, fi.Size())
	if err != nil {
		return fmt.Errorf("packageRead: %w", err)
	}
	nounNamesByInstance, nounReferences, err := readNonPlayerNounReferences(pkg)
	if err != nil {
		return fmt.Errorf("nounReferences: %w", err)
	}
	statement, err := transaction.PrepareContext(ctx, `
		INSERT INTO non_player_class (
			content_source_resource_id, instance_id, noun_name, class_attribute_resource_id,
			display_name, display_name_locale_key, description, description_locale_key,
			challenge_value, npc_rank, npc_type, creature_type,
			is_targetable, is_player_pet, player_count_health_scale,
			aggro_range, alert_range, drop_aggro_range,
			idle_movement_speed, base_combat_speed,
			hit_point, power_point,
			strength, dexterity, mind, dodge_rating, resist_rating, critical_rating
		)
		SELECT id, instance_id, ?, (
			SELECT attribute.id FROM content_source_resource AS attribute
			WHERE attribute.content_source_package_id=content_source_resource.content_source_package_id
			  AND attribute.type_id=? AND attribute.group_id=? AND attribute.instance_id=?
		), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		FROM content_source_resource
		WHERE content_source_package_id=(
			SELECT id FROM content_source_package WHERE package_name='AssetData_Binary.package'
		) AND type_id=? AND group_id=? AND instance_id=?`)
	if err != nil {
		return fmt.Errorf("insertPrepare: %w", err)
	}
	defer statement.Close()
	affixStatement, err := transaction.PrepareContext(ctx, `
		INSERT INTO non_player_class_affix (
			non_player_class_resource_id, ordinal, asset_name, minimum_difficulty, maximum_difficulty
		)
		SELECT id, ?, ?, ?, ? FROM content_source_resource
		WHERE content_source_package_id=(
			SELECT id FROM content_source_package WHERE package_name='AssetData_Binary.package'
		) AND type_id=? AND group_id=? AND instance_id=?`)
	if err != nil {
		return fmt.Errorf("affixPrepare: %w", err)
	}
	defer affixStatement.Close()
	dropStatement, err := transaction.PrepareContext(ctx, `
		INSERT INTO non_player_class_drop_type (non_player_class_resource_id, ordinal, drop_type)
		SELECT id, ?, ? FROM content_source_resource
		WHERE content_source_package_id=(
			SELECT id FROM content_source_package WHERE package_name='AssetData_Binary.package'
		) AND type_id=? AND group_id=? AND instance_id=?`)
	if err != nil {
		return fmt.Errorf("dropPrepare: %w", err)
	}
	defer dropStatement.Close()
	attributesByInstance := make(map[uint32]nonPlayerClassAsset)
	for ordinal, entry := range pkg.Entries {
		if entry.Type != combatAttributeAssetType || entry.Group != nonPlayerClassAssetGroup {
			continue
		}
		decoded, openErr := pkg.Open(entry)
		if openErr != nil {
			return fmt.Errorf("payloadOpen[%d]: %w", ordinal, openErr)
		}
		payload, readErr := io.ReadAll(decoded)
		if readErr != nil {
			return fmt.Errorf("payloadRead[%d]: %w", ordinal, readErr)
		}
		class, decodeErr := decodeClassAttributes(entry, payload)
		if decodeErr != nil {
			return fmt.Errorf("payloadDecode[%d]: %w", ordinal, decodeErr)
		}
		attributesByInstance[class.instanceID] = class
	}
	for ordinal, entry := range pkg.Entries {
		if entry.Type != nonPlayerClassAssetType || entry.Group != nonPlayerClassAssetGroup {
			continue
		}
		decoded, openErr := pkg.Open(entry)
		if openErr != nil {
			return fmt.Errorf("classOpen[%d]: %w", ordinal, openErr)
		}
		payload, readErr := io.ReadAll(decoded)
		if readErr != nil {
			return fmt.Errorf("classRead[%d]: %w", ordinal, readErr)
		}
		class, decodeErr := decodeNonPlayerClass(entry, payload)
		if decodeErr != nil {
			return fmt.Errorf("classDecode[%d]: %w", ordinal, decodeErr)
		}
		class.nounName = nounNamesByInstance[class.instanceID]
		if class.nounName == "" {
			return fmt.Errorf("classNoun[%d]: missing %#x", ordinal, class.instanceID)
		}
		attributeInstanceID := hashID(strings.TrimSuffix(class.attributeName, ".ClassAttributes"))
		attributes, isAttributeFound := attributesByInstance[attributeInstanceID]
		if !isAttributeFound {
			return fmt.Errorf("attributeMissing[%d]: %s", ordinal, class.attributeName)
		}
		class.hitPoint = attributes.hitPoint
		class.powerPoint = attributes.powerPoint
		class.strength = attributes.strength
		class.dexterity = attributes.dexterity
		class.mind = attributes.mind
		class.dodgeRating = attributes.dodgeRating
		class.resistRating = attributes.resistRating
		class.criticalRating = attributes.criticalRating
		class.idleMovementSpeed = attributes.idleMovementSpeed
		class.baseCombatSpeed = attributes.baseCombatSpeed
		result, insertErr := statement.ExecContext(
			ctx, class.nounName, int64(combatAttributeAssetType),
			int64(nonPlayerClassAssetGroup), int64(attributeInstanceID),
			class.displayName, class.displayNameLocaleKey,
			class.description, class.descriptionLocaleKey,
			class.challengeValue, class.npcRank, class.npcType, class.creatureType, class.isTargetable,
			class.isPlayerPet, class.playerCountHealthScale,
			class.aggroRange, class.alertRange, class.dropAggroRange,
			class.idleMovementSpeed, class.baseCombatSpeed,
			class.hitPoint, class.powerPoint, class.strength, class.dexterity, class.mind,
			class.dodgeRating, class.resistRating, class.criticalRating,
			int64(entry.Type), int64(entry.Group), int64(entry.Instance),
		)
		if insertErr != nil {
			return fmt.Errorf("insert[%d]: %w", ordinal, insertErr)
		}
		count, countErr := result.RowsAffected()
		if countErr != nil {
			return fmt.Errorf("insertCount[%d]: %w", ordinal, countErr)
		}
		if count != 1 {
			return fmt.Errorf("insertCount[%d]: got %d", ordinal, count)
		}
		for affixIndex, affix := range class.affixes {
			affixResult, affixErr := affixStatement.ExecContext(
				ctx, affixIndex, affix.AssetName, affix.MinimumDifficulty, affix.MaximumDifficulty,
				int64(entry.Type), int64(entry.Group), int64(entry.Instance),
			)
			if affixErr != nil {
				return fmt.Errorf("affixInsert[%d:%d]: %w", ordinal, affixIndex, affixErr)
			}
			affixCount, countErr := affixResult.RowsAffected()
			if countErr != nil {
				return fmt.Errorf("affixCount[%d:%d]: %w", ordinal, affixIndex, countErr)
			}
			if affixCount != 1 {
				return fmt.Errorf(
					"affixCount[%d:%d]: got %d", ordinal, affixIndex, affixCount,
				)
			}
		}
		for dropIndex, dropType := range class.dropTypes {
			dropResult, dropErr := dropStatement.ExecContext(ctx, dropIndex, dropType,
				int64(entry.Type), int64(entry.Group), int64(entry.Instance))
			if dropErr != nil {
				return fmt.Errorf("dropInsert[%d:%d]: %w", ordinal, dropIndex, dropErr)
			}
			dropCount, countErr := dropResult.RowsAffected()
			if countErr != nil {
				return fmt.Errorf("dropCount[%d:%d]: %w", ordinal, dropIndex, countErr)
			}
			if dropCount != 1 {
				return fmt.Errorf("dropCount[%d:%d]: got %d", ordinal, dropIndex, dropCount)
			}
		}
	}
	err = writeNPCAffixes(ctx, transaction, pkg)
	if err != nil {
		return fmt.Errorf("npcAffixWrite: %w", err)
	}
	err = writeNonPlayerNounReferences(ctx, transaction, nounReferences)
	if err != nil {
		return fmt.Errorf("nounInsert: %w", err)
	}
	err = writeNounSpawnExtents(ctx, transaction, pkg, nounNamesByInstance)
	if err != nil {
		return fmt.Errorf("spawnExtentWrite: %w", err)
	}
	err = writeNounNavigations(ctx, transaction, pkg)
	if err != nil {
		return fmt.Errorf("nounNavigationWrite: %w", err)
	}
	return nil
}

func decodeNonPlayerClass(entry dbpf.Entry, payload []byte) (nonPlayerClassAsset, error) {
	if len(payload) < nonPlayerClassPrefixSize {
		return nonPlayerClassAsset{}, fmt.Errorf("size: got %d, want >=%d", len(payload), nonPlayerClassPrefixSize)
	}
	decodeBool := func(offset int) (bool, error) {
		raw := binary.LittleEndian.Uint32(payload[offset : offset+4])
		if raw > 1 {
			return false, fmt.Errorf("bool[%#x]: %d", offset, raw)
		}
		return raw == 1, nil
	}
	isTargetable, err := decodeBool(0x4c)
	if err != nil {
		return nonPlayerClassAsset{}, fmt.Errorf("targetable: %w", err)
	}
	isPlayerPet, err := decodeBool(0x78)
	if err != nil {
		return nonPlayerClassAsset{}, fmt.Errorf("playerPet: %w", err)
	}
	playerCountHealthScale := math.Float32frombits(binary.LittleEndian.Uint32(payload[0x64:0x68]))
	if math.IsNaN(float64(playerCountHealthScale)) || math.IsInf(float64(playerCountHealthScale), 0) ||
		playerCountHealthScale < 0 {
		return nonPlayerClassAsset{}, errors.New("player count health scale invalid")
	}
	aggroRange := math.Float32frombits(binary.LittleEndian.Uint32(payload[0x38:0x3c]))
	alertRange := math.Float32frombits(binary.LittleEndian.Uint32(payload[0x3c:0x40]))
	dropAggroRange := math.Float32frombits(binary.LittleEndian.Uint32(payload[0x40:0x44]))
	for _, radius := range []float32{aggroRange, alertRange, dropAggroRange} {
		if math.IsNaN(float64(radius)) || math.IsInf(float64(radius), 0) || radius < 0 {
			return nonPlayerClassAsset{}, errors.New("non-player awareness range invalid")
		}
	}
	challengeValue := int32(binary.LittleEndian.Uint32(payload[0x24:0x28]))
	npcRank := int32(binary.LittleEndian.Uint32(payload[0x48:0x4c]))
	if challengeValue < 0 || npcRank < 0 {
		return nonPlayerClassAsset{}, errors.New("non-player class scalar invalid")
	}
	metadata, err := decodeNonPlayerClassMetadata(payload)
	if err != nil {
		return nonPlayerClassAsset{}, fmt.Errorf("metadata: %w", err)
	}
	metadata.instanceID = uint32(entry.Instance)
	metadata.challengeValue = challengeValue
	metadata.npcRank = npcRank
	metadata.npcType = binary.LittleEndian.Uint32(payload[0x44:0x48])
	metadata.creatureType = binary.LittleEndian.Uint32(payload[0x04:0x08])
	if metadata.creatureType > 5 {
		return nonPlayerClassAsset{}, fmt.Errorf("creatureType: %d", metadata.creatureType)
	}
	metadata.isTargetable = isTargetable
	metadata.isPlayerPet = isPlayerPet
	metadata.playerCountHealthScale = playerCountHealthScale
	metadata.aggroRange = aggroRange
	metadata.alertRange = alertRange
	metadata.dropAggroRange = dropAggroRange
	return metadata, nil
}

// The packaged pointer is a runtime address, not a file offset. The raw
// values remain in the serialized tail; prefer its suffix when zeros overlap.
func decodeNonPlayerDropTypes(payload []byte) ([]uint32, error) {
	count := int(binary.LittleEndian.Uint32(payload[0x2c:0x30]))
	mask := binary.LittleEndian.Uint32(payload[0x30:0x34])
	if count == 0 {
		if mask != 0 && mask != math.MaxUint32 {
			return nil, fmt.Errorf("emptyMask: %d", mask)
		}
		return nil, nil
	}
	if count > 16 || len(payload)-nonPlayerClassPrefixSize < count*4 {
		return nil, fmt.Errorf("count: %d", count)
	}
	var matches []uint32
	matchCount := 0
	var suffixMatches []uint32
	for offset := nonPlayerClassPrefixSize; offset+count*4 <= len(payload); offset++ {
		dropTypes := make([]uint32, 0, count)
		combined := uint32(0)
		for index := range count {
			dropType := binary.LittleEndian.Uint32(payload[offset+index*4:])
			if dropType != 0 && dropType != 1 && dropType != 2 && dropType != 4 &&
				dropType != 8 && dropType != 16 {
				break
			}
			dropTypes = append(dropTypes, dropType)
			combined |= dropType
		}
		if len(dropTypes) != count || combined != mask {
			continue
		}
		matches = dropTypes
		matchCount++
		if offset+count*4 == len(payload) {
			suffixMatches = dropTypes
		}
	}
	if matchCount > 1 && suffixMatches != nil {
		return suffixMatches, nil
	}
	if matchCount != 1 {
		return nil, fmt.Errorf("matchCount: %d for count %d mask %d", matchCount, count, mask)
	}
	return matches, nil
}

func readNonPlayerClassString(payload []byte, offset int) (string, int, error) {
	if offset < 0 || offset >= len(payload) {
		return "", offset, errors.New("offset invalid")
	}
	end := offset
	for end < len(payload) && payload[end] != 0 {
		if payload[end] < 0x20 || payload[end] > 0x7e {
			return "", offset, fmt.Errorf("ascii[%#x]: invalid", end)
		}
		end++
	}
	if end >= len(payload) {
		return "", offset, errors.New("unterminated")
	}
	return string(payload[offset:end]), end + 1, nil
}

func nonPlayerLocaleKey(reference string) (string, error) {
	const prefix = "AssetStrings!"
	if !strings.HasPrefix(reference, prefix) {
		return "", fmt.Errorf("reference: invalid %q", reference)
	}
	key := strings.ToLower(strings.TrimPrefix(reference, prefix))
	if len(key) != len("0x00000000") || !strings.HasPrefix(key, "0x") {
		return "", fmt.Errorf("key: invalid %q", key)
	}
	for _, digit := range key[2:] {
		if (digit < '0' || digit > '9') && (digit < 'a' || digit > 'f') {
			return "", fmt.Errorf("key: invalid %q", key)
		}
	}
	return key, nil
}

func isNonPlayerAffixName(name string) bool {
	stem := strings.TrimSuffix(name, ".NPCAffix")
	if stem == "" {
		return false
	}
	for _, character := range stem {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '_' {
			continue
		}
		return false
	}
	return true
}

func decodeClassAttributes(entry dbpf.Entry, payload []byte) (nonPlayerClassAsset, error) {
	if len(payload) != classAttributeSize {
		return nonPlayerClassAsset{}, fmt.Errorf("size: got %d, want %d", len(payload), classAttributeSize)
	}
	field := func(index int) float32 {
		offset := index * 4
		return math.Float32frombits(binary.LittleEndian.Uint32(payload[offset : offset+4]))
	}
	asset := nonPlayerClassAsset{
		instanceID: uint32(entry.Instance), hitPoint: field(0), powerPoint: field(1),
		strength: field(2), dexterity: field(3), mind: field(4),
		dodgeRating:       field(5) + field(3)*6,
		resistRating:      field(7) + field(4)*6,
		criticalRating:    field(8) + field(3)*4,
		idleMovementSpeed: field(10),
		baseCombatSpeed:   field(9),
	}
	for name, stat := range map[string]float32{
		"hitPoint": asset.hitPoint, "powerPoint": asset.powerPoint,
		"strength": asset.strength, "dexterity": asset.dexterity, "mind": asset.mind,
		"dodgeRating": asset.dodgeRating, "resistRating": asset.resistRating,
		"criticalRating":    asset.criticalRating,
		"idleMovementSpeed": asset.idleMovementSpeed,
		"baseCombatSpeed":   asset.baseCombatSpeed,
	} {
		if math.IsNaN(float64(stat)) || math.IsInf(float64(stat), 0) || stat < 0 {
			return nonPlayerClassAsset{}, fmt.Errorf("%s: invalid", name)
		}
	}
	return asset, nil
}
