package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	contentdata "github.com/darkspinnet/darkspin/content"
)

const lootAssetPackage = "AssetData_Binary.package"

// LootRigblock is the immutable profile-facing projection of one base item.
type LootRigblock struct {
	ID             uint16
	ContentFlags   uint8
	MinimumLevel   uint32
	MaximumLevel   uint32
	IsUniqueFamily bool
	SlotType       string
	ClassType      string
	ScienceType    string
	WeaponSlotType string
}

// LootAffix is the immutable authored modifier vector for one prefix or suffix.
type LootAffix struct {
	Kind            string
	ID              uint16
	MinimumLevel    uint32
	MaximumLevel    uint32
	ClassType       string
	ScienceType     string
	Modifier        []float32
	IsUniqueFamily  bool
	IsBasicEligible bool
}

func writeLootRigblocks(ctx context.Context, transaction *sql.Tx, installPath string) error {
	rigblocks, err := contentdata.LoadLootRigblocks(ctx, filepath.Join(installPath, "Data", lootAssetPackage))
	if err != nil {
		return fmt.Errorf("rigblockLoad: %w", err)
	}
	if len(rigblocks) == 0 {
		return nil
	}
	var packageID int64
	err = transaction.QueryRowContext(ctx,
		"SELECT id FROM content_source_package WHERE package_name=?", lootAssetPackage,
	).Scan(&packageID)
	if err != nil {
		return fmt.Errorf("packageID: %w", err)
	}
	statement, err := transaction.PrepareContext(ctx, `
		INSERT INTO loot_rigblock
		(id, content_source_resource_id, slot_type, class_type, science_type,
		 image_group_id, image_instance_id, image_name, weapon_noun_id,
		 content_flags, minimum_level, maximum_level, is_unique_family)
		SELECT ?, id, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		FROM content_source_resource
		WHERE content_source_package_id=? AND ordinal=?`)
	if err != nil {
		return fmt.Errorf("statementPrepare: %w", err)
	}
	defer statement.Close()
	for index, rigblock := range rigblocks {
		weaponNounID := uint32(0)
		if rigblock.WeaponNoun != "" {
			weaponNounID = hashID(rigblock.WeaponNoun)
		}
		result, execErr := statement.ExecContext(ctx,
			rigblock.ID, rigblock.SlotType, strings.Join(rigblock.ClassTypes, ","),
			strings.Join(rigblock.ScienceTypes, ","), rigblock.ImageGroup,
			rigblock.ImageInstance, rigblock.ImageName, weaponNounID,
			rigblock.ContentFlags, rigblock.MinimumLevel, rigblock.MaximumLevel,
			rigblock.IsUniqueFamily,
			packageID, rigblock.SourceOrdinal,
		)
		if execErr != nil {
			return fmt.Errorf("rigblockInsert[%d]: %w", index, execErr)
		}
		rowCount, rowErr := result.RowsAffected()
		if rowErr != nil {
			return fmt.Errorf("rigblockRows[%d]: %w", index, rowErr)
		}
		if rowCount != 1 {
			return fmt.Errorf("rigblockSource[%d]: got %d rows", index, rowCount)
		}
	}
	err = statement.Close()
	if err != nil {
		return fmt.Errorf("statementClose: %w", err)
	}
	return nil
}

func writeLootAffixes(ctx context.Context, transaction *sql.Tx, installPath string) error {
	affixes, err := contentdata.LoadLootAffixes(ctx, filepath.Join(installPath, "Data", lootAssetPackage))
	if err != nil {
		return fmt.Errorf("affixLoad: %w", err)
	}
	if len(affixes) == 0 {
		return nil
	}
	var packageID int64
	err = transaction.QueryRowContext(ctx,
		"SELECT id FROM content_source_package WHERE package_name=?", lootAssetPackage,
	).Scan(&packageID)
	if err != nil {
		return fmt.Errorf("packageID: %w", err)
	}
	statement, err := transaction.PrepareContext(ctx, `
		INSERT INTO loot_affix
		(kind, id, content_source_resource_id, minimum_level, maximum_level,
		 class_type, science_type, modifier, is_unique_family, is_basic_eligible)
		SELECT ?, ?, id, ?, ?, ?, ?, ?, ?, ?
		FROM content_source_resource
		WHERE content_source_package_id=? AND ordinal=?`)
	if err != nil {
		return fmt.Errorf("statementPrepare: %w", err)
	}
	defer statement.Close()
	for index, affix := range affixes {
		modifier := encodeLootModifier(affix.Modifier)
		result, execErr := statement.ExecContext(ctx,
			affix.Kind, affix.ID, affix.MinimumLevel, affix.MaximumLevel,
			strings.Join(affix.ClassTypes, ","), strings.Join(affix.ScienceTypes, ","),
			modifier, affix.IsUniqueFamily, affix.IsBasicEligible, packageID, affix.SourceOrdinal,
		)
		if execErr != nil {
			return fmt.Errorf("affixInsert[%d]: %w", index, execErr)
		}
		rowCount, rowErr := result.RowsAffected()
		if rowErr != nil {
			return fmt.Errorf("affixRows[%d]: %w", index, rowErr)
		}
		if rowCount != 1 {
			return fmt.Errorf("affixSource[%d]: got %d rows", index, rowCount)
		}
	}
	err = statement.Close()
	if err != nil {
		return fmt.Errorf("statementClose: %w", err)
	}
	return nil
}

func writeLootTuning(ctx context.Context, transaction *sql.Tx, installPath string) error {
	var resourceCount int
	err := transaction.QueryRowContext(ctx,
		"SELECT resource_count FROM content_source_package WHERE package_name=?", lootAssetPackage,
	).Scan(&resourceCount)
	if err != nil {
		return fmt.Errorf("packageCount: %w", err)
	}
	if resourceCount == 0 {
		return nil
	}
	tuning, err := contentdata.LoadLootTuning(ctx, filepath.Join(installPath, "Data", lootAssetPackage))
	if err != nil {
		return fmt.Errorf("tuningLoad: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `
		INSERT INTO loot_tuning
		(id, content_source_resource_id, rarity_level_step, base_point, extra_stat_bonus_factor,
		 level_band, point_cost, rarity_distribution, hand_level_scale, hand_flat,
		 foot_level_scale, foot_flat, defense_flat, offense_flat, utility_flat,
		 weapon_damage_multiplier, price_base, price_curve, price_increment,
		 hand_minimum_level, foot_minimum_level, weapon_minimum_level,
		 uncommon_chance, rare_chance, epic_chance)
		SELECT 1, content_source_resource.id, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		FROM content_source_resource
		JOIN content_source_package ON content_source_package.id=content_source_resource.content_source_package_id
		WHERE content_source_package.package_name=? AND content_source_resource.ordinal=?`,
		tuning.RarityLevelStep, tuning.BasePoint, tuning.ExtraStatBonusFactor,
		encodeLootLevelBand(tuning.LevelBands), encodeLootPointCost(tuning.PointCosts),
		encodeLootRarityDistribution(tuning.RarityDistributions),
		tuning.HandLevelScale, tuning.HandFlat, tuning.FootLevelScale, tuning.FootFlat,
		tuning.DefenseFlat, tuning.OffenseFlat, tuning.UtilityFlat, tuning.WeaponDamageMultiplier,
		tuning.PriceBase, tuning.PriceCurve, tuning.PriceIncrement,
		tuning.HandMinimumLevel, tuning.FootMinimumLevel, tuning.WeaponMinimumLevel,
		encodeLootChances(tuning.UncommonChances), encodeLootChances(tuning.RareChances),
		encodeLootChances(tuning.EpicChances),
		lootAssetPackage, tuning.SourceOrdinal,
	)
	if err != nil {
		return fmt.Errorf("tuningInsert: %w", err)
	}
	rowCount, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("tuningRows: %w", err)
	}
	if rowCount != 1 {
		return fmt.Errorf("tuningSource: got %d rows", rowCount)
	}
	return nil
}

func encodeLootModifier(modifier []float32) []byte {
	payload := make([]byte, len(modifier)*4)
	for index, amount := range modifier {
		binary.LittleEndian.PutUint32(payload[index*4:], math.Float32bits(amount))
	}
	return payload
}

func encodeLootLevelBand(levelBands []contentdata.LootLevelBand) []byte {
	payload := make([]byte, len(levelBands)*8)
	for index, levelBand := range levelBands {
		binary.LittleEndian.PutUint32(payload[index*8:], math.Float32bits(levelBand.Base))
		binary.LittleEndian.PutUint32(payload[index*8+4:], levelBand.MaximumLevel)
	}
	return payload
}

func encodeLootPointCost(pointCosts [13]uint32) []byte {
	payload := make([]byte, len(pointCosts)*4)
	for index, cost := range pointCosts {
		binary.LittleEndian.PutUint32(payload[index*4:], cost)
	}
	return payload
}

func encodeLootRarityDistribution(distributions [4]contentdata.LootRarityDistribution) []byte {
	payload := make([]byte, len(distributions)*16)
	for index, rarity := range distributions {
		offset := index * 16
		binary.LittleEndian.PutUint32(payload[offset:], math.Float32bits(rarity.Suffix))
		binary.LittleEndian.PutUint32(payload[offset+4:], math.Float32bits(rarity.Prefix))
		binary.LittleEndian.PutUint32(payload[offset+8:], math.Float32bits(rarity.Prefix2))
		binary.LittleEndian.PutUint32(payload[offset+12:], math.Float32bits(rarity.Standard))
	}
	return payload
}

func encodeLootChances(chances [19]float32) []byte {
	payload := make([]byte, len(chances)*4)
	for index, chance := range chances {
		binary.LittleEndian.PutUint32(payload[index*4:], math.Float32bits(chance))
	}
	return payload
}

// LootRigblocks reads the complete base-item profile catalog.
func (s *Store) LootRigblocks(ctx context.Context) ([]LootRigblock, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("nil store")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT loot_rigblock.id, slot_type, loot_rigblock.class_type, loot_rigblock.science_type,
		       content_flags, minimum_level, maximum_level, is_unique_family,
		       CASE WHEN slot_type != 'weapon' THEN ''
		            WHEN creature_template.is_hand_present=1 THEN 'grasper'
		            WHEN creature_template.is_foot_present=1 THEN 'foot'
		            ELSE '' END
		FROM loot_rigblock
		LEFT JOIN creature_template ON creature_template.id=loot_rigblock.weapon_noun_id
		ORDER BY loot_rigblock.id`)
	if err != nil {
		return nil, fmt.Errorf("rigblockQuery: %w", err)
	}
	defer rows.Close()
	rigblocks := make([]LootRigblock, 0, 2288)
	for rows.Next() {
		var rigblock LootRigblock
		err = rows.Scan(&rigblock.ID, &rigblock.SlotType, &rigblock.ClassType, &rigblock.ScienceType,
			&rigblock.ContentFlags, &rigblock.MinimumLevel, &rigblock.MaximumLevel,
			&rigblock.IsUniqueFamily,
			&rigblock.WeaponSlotType)
		if err != nil {
			return nil, fmt.Errorf("rigblockScan: %w", err)
		}
		rigblocks = append(rigblocks, rigblock)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("rigblockRows: %w", err)
	}
	err = rows.Close()
	if err != nil {
		return nil, fmt.Errorf("rigblockClose: %w", err)
	}
	return rigblocks, nil
}

// LootAffixes reads all authored prefix and suffix modifier vectors.
func (s *Store) LootAffixes(ctx context.Context) ([]LootAffix, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("nil store")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT kind, id, minimum_level, maximum_level, class_type, science_type, modifier,
		       is_unique_family, is_basic_eligible
		FROM loot_affix
		ORDER BY kind, id`)
	if err != nil {
		return nil, fmt.Errorf("affixQuery: %w", err)
	}
	defer rows.Close()
	affixes := make([]LootAffix, 0, 666)
	for rows.Next() {
		var affix LootAffix
		var payload []byte
		err = rows.Scan(&affix.Kind, &affix.ID, &affix.MinimumLevel, &affix.MaximumLevel,
			&affix.ClassType, &affix.ScienceType, &payload,
			&affix.IsUniqueFamily, &affix.IsBasicEligible)
		if err != nil {
			return nil, fmt.Errorf("affixScan: %w", err)
		}
		if len(payload) != contentdata.LootAttributeCount*4 {
			return nil, fmt.Errorf("affixModifier[%s/%d]: got %d bytes", affix.Kind, affix.ID, len(payload))
		}
		affix.Modifier = make([]float32, contentdata.LootAttributeCount)
		for index := range affix.Modifier {
			affix.Modifier[index] = math.Float32frombits(binary.LittleEndian.Uint32(payload[index*4:]))
		}
		affixes = append(affixes, affix)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("affixRows: %w", err)
	}
	err = rows.Close()
	if err != nil {
		return nil, fmt.Errorf("affixClose: %w", err)
	}
	return affixes, nil
}

// LootTuning reads the one build-103 item-stat tuning projection.
func (s *Store) LootTuning(ctx context.Context) (contentdata.LootTuning, error) {
	if s == nil || s.database == nil {
		return contentdata.LootTuning{}, errors.New("nil store")
	}
	if ctx == nil {
		return contentdata.LootTuning{}, errors.New("nil context")
	}
	var tuning contentdata.LootTuning
	var levelBands []byte
	var pointCosts []byte
	var rarityDistributions []byte
	var uncommonChances []byte
	var rareChances []byte
	var epicChances []byte
	err := s.database.QueryRowContext(ctx, `
		SELECT rarity_level_step, base_point, extra_stat_bonus_factor,
		       level_band, point_cost, rarity_distribution, hand_level_scale, hand_flat,
		       foot_level_scale, foot_flat, defense_flat, offense_flat, utility_flat,
		       weapon_damage_multiplier, price_base, price_curve, price_increment,
		       hand_minimum_level, foot_minimum_level, weapon_minimum_level,
		       uncommon_chance, rare_chance, epic_chance
		FROM loot_tuning WHERE id=1`).Scan(
		&tuning.RarityLevelStep, &tuning.BasePoint, &tuning.ExtraStatBonusFactor,
		&levelBands, &pointCosts, &rarityDistributions, &tuning.HandLevelScale, &tuning.HandFlat,
		&tuning.FootLevelScale, &tuning.FootFlat, &tuning.DefenseFlat, &tuning.OffenseFlat,
		&tuning.UtilityFlat, &tuning.WeaponDamageMultiplier,
		&tuning.PriceBase, &tuning.PriceCurve, &tuning.PriceIncrement,
		&tuning.HandMinimumLevel, &tuning.FootMinimumLevel, &tuning.WeaponMinimumLevel,
		&uncommonChances, &rareChances, &epicChances,
	)
	if err != nil {
		return contentdata.LootTuning{}, fmt.Errorf("tuningQuery: %w", err)
	}
	if len(levelBands) == 0 || len(levelBands)%8 != 0 || len(pointCosts) != 52 ||
		len(rarityDistributions) != 64 || len(uncommonChances) != 76 ||
		len(rareChances) != 76 || len(epicChances) != 76 {
		return contentdata.LootTuning{}, errors.New("tuningPayload: invalid")
	}
	for offset := 0; offset < len(levelBands); offset += 8 {
		tuning.LevelBands = append(tuning.LevelBands, contentdata.LootLevelBand{
			Base:         math.Float32frombits(binary.LittleEndian.Uint32(levelBands[offset:])),
			MaximumLevel: binary.LittleEndian.Uint32(levelBands[offset+4:]),
		})
	}
	for index := range tuning.PointCosts {
		tuning.PointCosts[index] = binary.LittleEndian.Uint32(pointCosts[index*4:])
	}
	for index := range tuning.RarityDistributions {
		offset := index * 16
		tuning.RarityDistributions[index] = contentdata.LootRarityDistribution{
			Suffix:   math.Float32frombits(binary.LittleEndian.Uint32(rarityDistributions[offset:])),
			Prefix:   math.Float32frombits(binary.LittleEndian.Uint32(rarityDistributions[offset+4:])),
			Prefix2:  math.Float32frombits(binary.LittleEndian.Uint32(rarityDistributions[offset+8:])),
			Standard: math.Float32frombits(binary.LittleEndian.Uint32(rarityDistributions[offset+12:])),
		}
	}
	for index := range tuning.UncommonChances {
		tuning.UncommonChances[index] = math.Float32frombits(binary.LittleEndian.Uint32(uncommonChances[index*4:]))
		tuning.RareChances[index] = math.Float32frombits(binary.LittleEndian.Uint32(rareChances[index*4:]))
		tuning.EpicChances[index] = math.Float32frombits(binary.LittleEndian.Uint32(epicChances[index*4:]))
	}
	return tuning, nil
}
