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

	"github.com/darkspinnet/darkspin/content/dbpf"
)

const (
	magicNumberType                  = 0x36201b51
	magicNumberInstance              = 0x966534e4
	magicNumberSize                  = 76
	criticalDamageBonusOffset        = 48
	difficultyTuningType             = 0x8e94f44c
	difficultyTuningInstance         = 0x02ca2581
	difficultyTuningSize             = 2120
	difficultyStarHealthBaseOffset   = 56
	difficultyStarDamageBaseOffset   = 60
	difficultyHealthMultiplierOffset = 72
	difficultyDamageMultiplierOffset = 360
	difficultyExpectedLevelOffset    = 1544
	difficultyRatingConversionOffset = 1832
	difficultyTuningCount            = 72
)

// CriticalTuning is the immutable build-103 critical-hit tuning projection.
type CriticalTuning struct {
	DamageBonus       float32
	RatingConversions []float32
}

// DifficultyTuning is one authored build-103 campaign difficulty row. These
// operands are retained as content; their authoritative runtime stages remain
// separate from storage.
type DifficultyTuning struct {
	Difficulty          int
	HealthMultiplier    float32
	DamageMultiplier    float32
	ExpectedAvatarLevel int
	RatingConversion    float32
}

// DifficultyScaling retains the authored Star Mode bases and complete
// difficulty rows.
type DifficultyScaling struct {
	StarHealthBase float32
	StarDamageBase float32
	Rows           []DifficultyTuning
}

type combatTuningSource struct {
	tuning            CriticalTuning
	scaling           DifficultyScaling
	difficulty        []DifficultyTuning
	magicOrdinal      int
	difficultyOrdinal int
}

func writeCombatTuning(ctx context.Context, transaction *sql.Tx, installPath string) error {
	source, err := loadCombatTuning(filepath.Join(installPath, "Data", lootAssetPackage))
	if err != nil {
		return fmt.Errorf("tuningLoad: %w", err)
	}
	if source.magicOrdinal < 0 && source.difficultyOrdinal < 0 {
		return nil
	}
	var packageID int64
	err = transaction.QueryRowContext(ctx,
		"SELECT id FROM content_source_package WHERE package_name=?", lootAssetPackage,
	).Scan(&packageID)
	if err != nil {
		return fmt.Errorf("packageID: %w", err)
	}
	var magicResourceID, difficultyResourceID int64
	err = transaction.QueryRowContext(ctx, `
		SELECT id FROM content_source_resource
		WHERE content_source_package_id=? AND ordinal=?`, packageID, source.magicOrdinal,
	).Scan(&magicResourceID)
	if err != nil {
		return fmt.Errorf("magicSource: %w", err)
	}
	err = transaction.QueryRowContext(ctx, `
		SELECT id FROM content_source_resource
		WHERE content_source_package_id=? AND ordinal=?`, packageID, source.difficultyOrdinal,
	).Scan(&difficultyResourceID)
	if err != nil {
		return fmt.Errorf("difficultySource: %w", err)
	}
	_, err = transaction.ExecContext(ctx, `
		INSERT INTO combat_tuning
		(id, magic_content_source_resource_id, difficulty_content_source_resource_id,
		 critical_damage_bonus, health_party_base, damage_party_base)
		VALUES (1, ?, ?, ?, ?, ?)`, magicResourceID, difficultyResourceID,
		source.tuning.DamageBonus, source.scaling.StarHealthBase, source.scaling.StarDamageBase)
	if err != nil {
		return fmt.Errorf("tuningInsert: %w", err)
	}
	statement, err := transaction.PrepareContext(ctx, `
		INSERT INTO difficulty_tuning
		(difficulty, content_source_resource_id, health_multiplier, damage_multiplier,
		 expected_avatar_level, rating_conversion) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("ratingPrepare: %w", err)
	}
	defer statement.Close()
	for _, difficulty := range source.difficulty {
		_, execErr := statement.ExecContext(ctx,
			difficulty.Difficulty, difficultyResourceID, difficulty.HealthMultiplier,
			difficulty.DamageMultiplier, difficulty.ExpectedAvatarLevel,
			difficulty.RatingConversion,
		)
		if execErr != nil {
			return fmt.Errorf("difficultyInsert[%d]: %w", difficulty.Difficulty, execErr)
		}
	}
	err = statement.Close()
	if err != nil {
		return fmt.Errorf("ratingClose: %w", err)
	}
	return nil
}

func loadCombatTuning(packagePath string) (combatTuningSource, error) {
	r, err := os.Open(packagePath)
	if err != nil {
		return combatTuningSource{}, fmt.Errorf("packageOpen: %w", err)
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		return combatTuningSource{}, fmt.Errorf("packageStat: %w", err)
	}
	pkg, err := dbpf.NewReader(r, fi.Size())
	if err != nil {
		return combatTuningSource{}, fmt.Errorf("packageRead: %w", err)
	}
	source := combatTuningSource{magicOrdinal: -1, difficultyOrdinal: -1}
	if len(pkg.Entries) == 0 {
		return source, nil
	}
	for ordinal, entry := range pkg.Entries {
		isMagic := entry.Type == magicNumberType && uint32(entry.Instance) == magicNumberInstance
		isDifficulty := entry.Type == difficultyTuningType && uint32(entry.Instance) == difficultyTuningInstance
		if !isMagic && !isDifficulty {
			continue
		}
		decoded, openErr := pkg.Open(entry)
		if openErr != nil {
			return combatTuningSource{}, fmt.Errorf("payloadOpen[%d]: %w", ordinal, openErr)
		}
		payload, readErr := io.ReadAll(decoded)
		if readErr != nil {
			return combatTuningSource{}, fmt.Errorf("payloadRead[%d]: %w", ordinal, readErr)
		}
		if isMagic {
			bonus, decodeErr := decodeCriticalDamageBonus(payload)
			if decodeErr != nil {
				return combatTuningSource{}, fmt.Errorf("magicDecode[%d]: %w", ordinal, decodeErr)
			}
			source.tuning.DamageBonus = bonus
			source.magicOrdinal = ordinal
		}
		if isDifficulty {
			difficulty, decodeErr := decodeDifficultyTuning(payload)
			if decodeErr != nil {
				return combatTuningSource{}, fmt.Errorf("difficultyDecode[%d]: %w", ordinal, decodeErr)
			}
			source.difficulty = difficulty
			source.scaling.Rows = difficulty
			source.scaling.StarHealthBase, source.scaling.StarDamageBase, decodeErr =
				decodeDifficultyStarBases(payload)
			if decodeErr != nil {
				return combatTuningSource{}, fmt.Errorf("difficultyParty[%d]: %w", ordinal, decodeErr)
			}
			source.tuning.RatingConversions = make([]float32, len(difficulty))
			for index := range difficulty {
				source.tuning.RatingConversions[index] = difficulty[index].RatingConversion
			}
			source.difficultyOrdinal = ordinal
		}
	}
	if source.magicOrdinal < 0 || source.difficultyOrdinal < 0 {
		return combatTuningSource{}, errors.New("combat tuning resources missing")
	}
	return source, nil
}

func decodeCriticalDamageBonus(payload []byte) (float32, error) {
	if len(payload) != magicNumberSize {
		return 0, fmt.Errorf("payloadSize: got %d, want %d", len(payload), magicNumberSize)
	}
	bonus := math.Float32frombits(binary.LittleEndian.Uint32(
		payload[criticalDamageBonusOffset : criticalDamageBonusOffset+4],
	))
	if math.IsNaN(float64(bonus)) || math.IsInf(float64(bonus), 0) || bonus <= 0 {
		return 0, fmt.Errorf("damageBonus: %g", bonus)
	}
	return bonus, nil
}

func decodeCriticalRatingConversion(payload []byte) ([]float32, error) {
	difficulty, err := decodeDifficultyTuning(payload)
	if err != nil {
		return nil, err
	}
	conversion := make([]float32, len(difficulty))
	for index := range difficulty {
		conversion[index] = difficulty[index].RatingConversion
	}
	return conversion, nil
}

func decodeDifficultyTuning(payload []byte) ([]DifficultyTuning, error) {
	if len(payload) != difficultyTuningSize {
		return nil, fmt.Errorf("payloadSize: got %d, want %d", len(payload), difficultyTuningSize)
	}
	difficulty := make([]DifficultyTuning, difficultyTuningCount)
	for index := range difficulty {
		healthOffset := difficultyHealthMultiplierOffset + index*4
		damageOffset := difficultyDamageMultiplierOffset + index*4
		levelOffset := difficultyExpectedLevelOffset + index*4
		ratingOffset := difficultyRatingConversionOffset + index*4
		difficulty[index] = DifficultyTuning{
			Difficulty: index + 1,
			HealthMultiplier: math.Float32frombits(binary.LittleEndian.Uint32(
				payload[healthOffset : healthOffset+4],
			)),
			DamageMultiplier: math.Float32frombits(binary.LittleEndian.Uint32(
				payload[damageOffset : damageOffset+4],
			)),
			ExpectedAvatarLevel: int(int32(binary.LittleEndian.Uint32(
				payload[levelOffset : levelOffset+4],
			))),
			RatingConversion: math.Float32frombits(binary.LittleEndian.Uint32(
				payload[ratingOffset : ratingOffset+4],
			)),
		}
		row := difficulty[index]
		if invalidPositiveTuning(row.HealthMultiplier) ||
			(index > 0 && row.HealthMultiplier < difficulty[index-1].HealthMultiplier) {
			return nil, fmt.Errorf("healthMultiplier[%d]: %g", index, row.HealthMultiplier)
		}
		if invalidPositiveTuning(row.DamageMultiplier) ||
			(index > 0 && row.DamageMultiplier < difficulty[index-1].DamageMultiplier) {
			return nil, fmt.Errorf("damageMultiplier[%d]: %g", index, row.DamageMultiplier)
		}
		if row.ExpectedAvatarLevel < 0 {
			return nil, fmt.Errorf("expectedAvatarLevel[%d]: %d", index, row.ExpectedAvatarLevel)
		}
		if invalidPositiveTuning(row.RatingConversion) ||
			(index > 0 && row.RatingConversion < difficulty[index-1].RatingConversion) {
			return nil, fmt.Errorf("ratingConversion[%d]: %g", index, row.RatingConversion)
		}
	}
	return difficulty, nil
}

func decodeDifficultyStarBases(payload []byte) (float32, float32, error) {
	if len(payload) != difficultyTuningSize {
		return 0, 0, fmt.Errorf("payloadSize: got %d, want %d", len(payload), difficultyTuningSize)
	}
	health := math.Float32frombits(binary.LittleEndian.Uint32(
		payload[difficultyStarHealthBaseOffset : difficultyStarHealthBaseOffset+4],
	))
	damage := math.Float32frombits(binary.LittleEndian.Uint32(
		payload[difficultyStarDamageBaseOffset : difficultyStarDamageBaseOffset+4],
	))
	if invalidPositiveTuning(health) {
		return 0, 0, fmt.Errorf("starHealthBase: %g", health)
	}
	if invalidPositiveTuning(damage) {
		return 0, 0, fmt.Errorf("starDamageBase: %g", damage)
	}
	return health, damage, nil
}

func invalidPositiveTuning(number float32) bool {
	return math.IsNaN(float64(number)) || math.IsInf(float64(number), 0) || number <= 0
}

// CriticalTuning returns the authored critical damage base and all 72
// difficulty-indexed rating conversions.
func (s *Store) CriticalTuning(ctx context.Context) (CriticalTuning, error) {
	if s == nil || s.database == nil {
		return CriticalTuning{}, errors.New("nil content store")
	}
	var tuning CriticalTuning
	err := s.database.QueryRowContext(ctx,
		"SELECT critical_damage_bonus FROM combat_tuning WHERE id=1",
	).Scan(&tuning.DamageBonus)
	if err != nil {
		return CriticalTuning{}, fmt.Errorf("criticalTuning: %w", err)
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT difficulty, rating_conversion
		FROM difficulty_tuning ORDER BY difficulty`)
	if err != nil {
		return CriticalTuning{}, fmt.Errorf("ratingQuery: %w", err)
	}
	defer rows.Close()
	tuning.RatingConversions = make([]float32, 0, difficultyTuningCount)
	for rows.Next() {
		var difficulty int
		var conversion float32
		err = rows.Scan(&difficulty, &conversion)
		if err != nil {
			return CriticalTuning{}, fmt.Errorf("ratingScan: %w", err)
		}
		if difficulty != len(tuning.RatingConversions)+1 {
			return CriticalTuning{}, fmt.Errorf("ratingDifficulty: got %d, want %d",
				difficulty, len(tuning.RatingConversions)+1)
		}
		tuning.RatingConversions = append(tuning.RatingConversions, conversion)
	}
	err = rows.Err()
	if err != nil {
		return CriticalTuning{}, fmt.Errorf("ratingRows: %w", err)
	}
	if len(tuning.RatingConversions) != difficultyTuningCount {
		return CriticalTuning{}, fmt.Errorf("ratingCount: got %d, want %d",
			len(tuning.RatingConversions), difficultyTuningCount)
	}
	return tuning, nil
}

// DifficultyTuning returns all 72 authored campaign rows without applying the
// operands to server-authoritative combat or progression.
func (s *Store) DifficultyTuning(ctx context.Context) ([]DifficultyTuning, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("nil content store")
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT difficulty, health_multiplier, damage_multiplier,
		       expected_avatar_level, rating_conversion
		FROM difficulty_tuning ORDER BY difficulty`)
	if err != nil {
		return nil, fmt.Errorf("difficultyQuery: %w", err)
	}
	defer rows.Close()
	difficulty := make([]DifficultyTuning, 0, difficultyTuningCount)
	for rows.Next() {
		var row DifficultyTuning
		err = rows.Scan(
			&row.Difficulty, &row.HealthMultiplier, &row.DamageMultiplier,
			&row.ExpectedAvatarLevel, &row.RatingConversion,
		)
		if err != nil {
			return nil, fmt.Errorf("difficultyScan: %w", err)
		}
		if row.Difficulty != len(difficulty)+1 {
			return nil, fmt.Errorf("difficultyOrdinal: got %d, want %d", row.Difficulty, len(difficulty)+1)
		}
		difficulty = append(difficulty, row)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("difficultyRows: %w", err)
	}
	if len(difficulty) != difficultyTuningCount {
		return nil, fmt.Errorf("difficultyCount: got %d, want %d", len(difficulty), difficultyTuningCount)
	}
	return difficulty, nil
}

// DifficultyScaling returns the authored Star Mode bases and all difficulty rows.
func (s *Store) DifficultyScaling(ctx context.Context) (DifficultyScaling, error) {
	if s == nil || s.database == nil {
		return DifficultyScaling{}, errors.New("nil content store")
	}
	var scaling DifficultyScaling
	err := s.database.QueryRowContext(ctx, `
		SELECT health_party_base, damage_party_base FROM combat_tuning WHERE id=1`,
	).Scan(&scaling.StarHealthBase, &scaling.StarDamageBase)
	if err != nil {
		return DifficultyScaling{}, fmt.Errorf("difficultyScaling: %w", err)
	}
	scaling.Rows, err = s.DifficultyTuning(ctx)
	if err != nil {
		return DifficultyScaling{}, fmt.Errorf("difficultyRows: %w", err)
	}
	return scaling, nil
}
