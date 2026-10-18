package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

type EliteStageTuning struct {
	Stage                    uint32
	MinimumAffixCount        int32
	MaximumAffixCount        int32
	Chance                   float32
	SpecialMinimumAffixCount int32
	SpecialMaximumAffixCount int32
	SpecialChance            float32
}

func isFiniteNonNegative(number float32) bool {
	return number >= 0 && !math.IsNaN(float64(number)) && !math.IsInf(float64(number), 0)
}

type ElitePromotionTuning struct {
	MinionMinimumStage       uint32
	SpecialMinimumStage      uint32
	SpecialReplacementChance float32
	StarModeEliteChanceAdd   float32
}

func writeEliteTuning(ctx context.Context, transaction *sql.Tx, installPath string) (resultErr error) {
	r, err := os.Open(filepath.Join(installPath, "Data", lootAssetPackage))
	if err != nil {
		return fmt.Errorf("eliteOpen: %w", err)
	}
	defer closeSectionReader(r, &resultErr)
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("eliteStat: %w", err)
	}
	pkg, err := importPackageReader(ctx, r, fi.Size())
	if err != nil {
		return fmt.Errorf("elitePackage: %w", err)
	}
	isStageFound, isDifficultyFound := false, false
	for ordinal, entry := range pkg.Entries {
		if entry.Group != 0 {
			continue
		}
		isStage := entry.Type == 0xe5c2d838 && uint32(entry.Instance) == 0xb174e8ce
		isDifficulty := entry.Type == 0x8e94f44c && uint32(entry.Instance) == 0x02ca2581
		if !isStage && !isDifficulty {
			continue
		}
		payload, readErr := readDecodedResource(ctx, pkg, entry)
		if readErr != nil {
			return fmt.Errorf("eliteRead[%d]: %w", ordinal, readErr)
		}
		if isDifficulty {
			if len(payload) < 72 {
				return fmt.Errorf("eliteDifficultySize: %d", len(payload))
			}
			bonus := readFloat32(payload, 64)
			if !isFiniteNonNegative(bonus) {
				return errors.New("elite star-mode bonus invalid")
			}
			result, insertErr := transaction.ExecContext(ctx, `INSERT INTO elite_promotion_tuning
				(id, content_source_resource_id, star_mode_elite_chance_add)
				SELECT 1, resource.id, ? FROM content_source_resource AS resource
				JOIN content_source_package AS package ON package.id=resource.content_source_package_id
				WHERE package.package_name=? AND resource.ordinal=?`, bonus, lootAssetPackage, ordinal)
			if insertErr != nil {
				return fmt.Errorf("eliteDifficultyInsert: %w", insertErr)
			}
			count, countErr := result.RowsAffected()
			if countErr != nil {
				return fmt.Errorf("eliteDifficultyCount: %w", countErr)
			}
			if count != 1 {
				return fmt.Errorf("eliteDifficultyCount: got %d", count)
			}
			isDifficultyFound = true
			continue
		}
		if len(payload) != 24+73*24 || binary.LittleEndian.Uint32(payload[4:]) != 73 {
			return fmt.Errorf("eliteStageSize: %d", len(payload))
		}
		for stage := range 73 {
			base := 24 + stage*24
			minimum := int32(binary.LittleEndian.Uint32(payload[base:]))
			maximum := int32(binary.LittleEndian.Uint32(payload[base+4:]))
			chance := readFloat32(payload, base+8)
			specialMinimum := int32(binary.LittleEndian.Uint32(payload[base+12:]))
			specialMaximum := int32(binary.LittleEndian.Uint32(payload[base+16:]))
			specialChance := readFloat32(payload, base+20)
			if minimum < 0 || maximum < minimum || specialMinimum < 0 || specialMaximum < specialMinimum ||
				!isFiniteNonNegative(chance) || !isFiniteNonNegative(specialChance) {
				return fmt.Errorf("eliteStage[%d]: invalid", stage)
			}
			result, insertErr := transaction.ExecContext(ctx, `INSERT INTO elite_stage_tuning
				(stage, content_source_resource_id, minimum_affix_count, maximum_affix_count, chance,
				 special_minimum_affix_count, special_maximum_affix_count, special_chance)
				SELECT ?, resource.id, ?, ?, ?, ?, ?, ? FROM content_source_resource AS resource
				JOIN content_source_package AS package ON package.id=resource.content_source_package_id
				WHERE package.package_name=? AND resource.ordinal=?`, stage, minimum, maximum, chance,
				specialMinimum, specialMaximum, specialChance, lootAssetPackage, ordinal)
			if insertErr != nil {
				return fmt.Errorf("eliteStageInsert[%d]: %w", stage, insertErr)
			}
			count, countErr := result.RowsAffected()
			if countErr != nil {
				return fmt.Errorf("eliteStageCount[%d]: %w", stage, countErr)
			}
			if count != 1 {
				return fmt.Errorf("eliteStageCount[%d]: got %d", stage, count)
			}
		}
		isStageFound = true
	}
	if !isStageFound || !isDifficultyFound {
		return errors.New("elite tuning resource missing")
	}
	return nil
}

func (e *Store) EliteStageTunings(ctx context.Context) ([]EliteStageTuning, error) {
	if e == nil || e.database == nil || ctx == nil {
		return nil, errors.New("elite store or context unavailable")
	}
	rows, err := e.database.QueryContext(ctx, `SELECT stage, minimum_affix_count, maximum_affix_count, chance,
		special_minimum_affix_count, special_maximum_affix_count, special_chance FROM elite_stage_tuning ORDER BY stage`)
	if err != nil {
		return nil, fmt.Errorf("eliteStageQuery: %w", err)
	}
	tunings := make([]EliteStageTuning, 0, 73)
	for rows.Next() {
		var tuning EliteStageTuning
		err = rows.Scan(&tuning.Stage, &tuning.MinimumAffixCount, &tuning.MaximumAffixCount, &tuning.Chance,
			&tuning.SpecialMinimumAffixCount, &tuning.SpecialMaximumAffixCount, &tuning.SpecialChance)
		if err != nil {
			closeErr := rows.Close()
			return nil, fmt.Errorf("eliteStageScan: %w", errors.Join(err, closeErr))
		}
		tunings = append(tunings, tuning)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return nil, fmt.Errorf("eliteStageRows: %w", errors.Join(err, closeErr))
	}
	if closeErr != nil {
		return nil, fmt.Errorf("eliteStageClose: %w", closeErr)
	}
	return tunings, nil
}

func (e *Store) ElitePromotionTuning(ctx context.Context) (ElitePromotionTuning, error) {
	if e == nil || e.database == nil || ctx == nil {
		return ElitePromotionTuning{}, errors.New("elite store or context unavailable")
	}
	var tuning ElitePromotionTuning
	err := e.database.QueryRowContext(ctx, `SELECT star_mode_elite_chance_add FROM elite_promotion_tuning WHERE id=1`).Scan(&tuning.StarModeEliteChanceAdd)
	if err != nil {
		return ElitePromotionTuning{}, fmt.Errorf("elitePromotionQuery: %w", err)
	}
	minimum, err := e.directorInteger(ctx, 0xaf478c01)
	if err != nil {
		return ElitePromotionTuning{}, fmt.Errorf("eliteMinionStage: %w", err)
	}
	tuning.MinionMinimumStage = minimum
	minimum, err = e.directorInteger(ctx, 0x0d57df60)
	if err != nil {
		return ElitePromotionTuning{}, fmt.Errorf("eliteSpecialStage: %w", err)
	}
	tuning.SpecialMinimumStage = minimum
	tuning.SpecialReplacementChance, err = e.directorFloat(ctx, 0xb7a59b8b)
	if err != nil {
		return ElitePromotionTuning{}, fmt.Errorf("eliteReplacementChance: %w", err)
	}
	return tuning, nil
}
