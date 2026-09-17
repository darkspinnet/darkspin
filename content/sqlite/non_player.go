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
	displayName            string
	displayNameLocaleKey   string
	description            string
	descriptionLocaleKey   string
	affixNames             []string
	challengeValue         int32
	npcRank                int32
	isTargetable           bool
	isPlayerPet            bool
	playerCountHealthScale float32
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
	pkg, err := dbpf.NewReader(r, fi.Size())
	if err != nil {
		return fmt.Errorf("packageRead: %w", err)
	}
	statement, err := transaction.PrepareContext(ctx, `
		INSERT INTO non_player_class (
			content_source_resource_id, instance_id, noun_name,
			display_name, display_name_locale_key, description, description_locale_key,
			challenge_value, npc_rank,
			is_targetable, is_player_pet, player_count_health_scale,
			hit_point, power_point,
			strength, dexterity, mind, dodge_rating, resist_rating, critical_rating
		)
		SELECT id, instance_id, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
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
			non_player_class_resource_id, ordinal, asset_name
		)
		SELECT id, ?, ? FROM content_source_resource
		WHERE content_source_package_id=(
			SELECT id FROM content_source_package WHERE package_name='AssetData_Binary.package'
		) AND type_id=? AND group_id=? AND instance_id=?`)
	if err != nil {
		return fmt.Errorf("affixPrepare: %w", err)
	}
	defer affixStatement.Close()
	attributeByInstance := make(map[uint32]nonPlayerClassAsset)
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
		attributeByInstance[class.instanceID] = class
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
		attributes, isAttributeFound := attributeByInstance[class.instanceID]
		if isAttributeFound {
			class.hitPoint = attributes.hitPoint
			class.powerPoint = attributes.powerPoint
			class.strength = attributes.strength
			class.dexterity = attributes.dexterity
			class.mind = attributes.mind
			class.dodgeRating = attributes.dodgeRating
			class.resistRating = attributes.resistRating
			class.criticalRating = attributes.criticalRating
		}
		result, insertErr := statement.ExecContext(
			ctx, class.nounName, class.displayName, class.displayNameLocaleKey,
			class.description, class.descriptionLocaleKey,
			class.challengeValue, class.npcRank, class.isTargetable,
			class.isPlayerPet, class.playerCountHealthScale,
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
		for affixIndex, affixName := range class.affixNames {
			affixResult, affixErr := affixStatement.ExecContext(
				ctx, affixIndex, affixName,
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
	metadata.isTargetable = isTargetable
	metadata.isPlayerPet = isPlayerPet
	metadata.playerCountHealthScale = playerCountHealthScale
	return metadata, nil
}

func decodeNonPlayerClassMetadata(payload []byte) (nonPlayerClassAsset, error) {
	const classSuffix = ".ClassAttributes"
	firstMetadata, offset, err := readNonPlayerClassString(payload, nonPlayerClassPrefixSize)
	if err != nil {
		return nonPlayerClassAsset{}, fmt.Errorf("firstMetadata: %w", err)
	}
	displayName := ""
	displayReference := ""
	className := ""
	if strings.HasSuffix(firstMetadata, classSuffix) {
		className = firstMetadata
	} else if strings.HasPrefix(firstMetadata, "AssetStrings!") {
		displayReference = firstMetadata
		className, offset, err = readNonPlayerClassString(payload, offset)
		if err != nil {
			return nonPlayerClassAsset{}, fmt.Errorf("className: %w", err)
		}
	} else {
		displayName = firstMetadata
		classCandidate, nextOffset, classErr := readNonPlayerClassString(payload, offset)
		if classErr != nil {
			return nonPlayerClassAsset{}, fmt.Errorf("classCandidate: %w", classErr)
		}
		offset = nextOffset
		if strings.HasPrefix(classCandidate, "AssetStrings!") {
			displayReference = classCandidate
			className, offset, err = readNonPlayerClassString(payload, offset)
			if err != nil {
				return nonPlayerClassAsset{}, fmt.Errorf("className: %w", err)
			}
		} else {
			className = classCandidate
		}
	}
	description := ""
	descriptionReference := ""
	if offset < len(payload) && payload[offset] >= 0x20 && payload[offset] <= 0x7e {
		description, offset, err = readNonPlayerClassString(payload, offset)
		if err != nil {
			return nonPlayerClassAsset{}, fmt.Errorf("description: %w", err)
		}
		if offset < len(payload) && strings.HasPrefix(
			string(payload[offset:]), "AssetStrings!",
		) {
			descriptionReference, _, err = readNonPlayerClassString(payload, offset)
			if err != nil {
				return nonPlayerClassAsset{}, fmt.Errorf("descriptionReference: %w", err)
			}
		}
	}
	if !strings.HasSuffix(className, classSuffix) {
		return nonPlayerClassAsset{}, fmt.Errorf("className: invalid %q", className)
	}
	displayNameLocaleKey := ""
	if displayReference != "" {
		displayNameLocaleKey, err = nonPlayerLocaleKey(displayReference)
		if err != nil {
			return nonPlayerClassAsset{}, fmt.Errorf("displayKey: %w", err)
		}
	}
	descriptionLocaleKey := ""
	if descriptionReference != "" {
		descriptionLocaleKey, err = nonPlayerLocaleKey(descriptionReference)
		if err != nil {
			return nonPlayerClassAsset{}, fmt.Errorf("descriptionKey: %w", err)
		}
	}
	affixNames := nonPlayerAffixNames(payload)
	if len(affixNames) > NonPlayerAffixLimit {
		return nonPlayerClassAsset{}, fmt.Errorf(
			"affixCount: got %d, want <=%d", len(affixNames), NonPlayerAffixLimit,
		)
	}
	return nonPlayerClassAsset{
		nounName:    strings.TrimSuffix(className, classSuffix) + ".Noun",
		displayName: displayName, displayNameLocaleKey: displayNameLocaleKey,
		description: description, descriptionLocaleKey: descriptionLocaleKey,
		affixNames: affixNames,
	}, nil
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

func nonPlayerAffixNames(payload []byte) []string {
	affixNames := make([]string, 0, 2)
	for offset := 0; offset < len(payload); {
		if payload[offset] < 0x20 || payload[offset] > 0x7e {
			offset++
			continue
		}
		end := offset
		for end < len(payload) && payload[end] >= 0x20 && payload[end] <= 0x7e {
			end++
		}
		if end < len(payload) && payload[end] == 0 {
			candidate := string(payload[offset:end])
			if strings.HasSuffix(candidate, ".NPCAffix") &&
				isNonPlayerAffixName(candidate) {
				affixNames = append(affixNames, candidate)
			}
		}
		offset = max(end+1, offset+1)
	}
	return affixNames
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
		dodgeRating:    field(5) + field(3)*6,
		resistRating:   field(7) + field(4)*6,
		criticalRating: field(8) + field(3)*4,
	}
	for name, stat := range map[string]float32{
		"hitPoint": asset.hitPoint, "powerPoint": asset.powerPoint,
		"strength": asset.strength, "dexterity": asset.dexterity, "mind": asset.mind,
		"dodgeRating": asset.dodgeRating, "resistRating": asset.resistRating,
		"criticalRating": asset.criticalRating,
	} {
		if math.IsNaN(float64(stat)) || math.IsInf(float64(stat), 0) || stat < 0 {
			return nonPlayerClassAsset{}, fmt.Errorf("%s: invalid", name)
		}
	}
	return asset, nil
}
