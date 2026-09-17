package content

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

const (
	lootTuningPropertyType     = uint32(0x61bf29aa)
	lootTuningPropertyInstance = uint64(0x534b4478)
	lootTuningPayloadSize      = 1212
)

// LootLevelBand applies one exponential base through an inclusive level boundary.
type LootLevelBand struct {
	Base         float32
	MaximumLevel uint32
}

// LootRarityDistribution contains the native suffix/prefix/standard block scales.
type LootRarityDistribution struct {
	Suffix   float32
	Prefix   float32
	Prefix2  float32
	Standard float32
}

// LootTuning contains the build-103 operands used to turn item modifiers into stats.
type LootTuning struct {
	SourceOrdinal          int
	RarityLevelStep        uint32
	BasePoint              float32
	ExtraStatBonusFactor   float32
	LevelBands             []LootLevelBand
	PointCosts             [13]uint32
	RarityDistributions    [4]LootRarityDistribution
	HandLevelScale         float32
	HandFlat               float32
	FootLevelScale         float32
	FootFlat               float32
	DefenseFlat            float32
	OffenseFlat            float32
	UtilityFlat            float32
	WeaponDamageMultiplier float32
	PriceBase              float32
	PriceCurve             float32
	PriceIncrement         uint32
	HandMinimumLevel       uint32
	FootMinimumLevel       uint32
	WeaponMinimumLevel     uint32
	UncommonChances        [19]float32
	RareChances            [19]float32
	EpicChances            [19]float32
}

// LoadLootTuning reads the one generated LootPreferences resource used by build 103.
func LoadLootTuning(ctx context.Context, packagePath string) (LootTuning, error) {
	r, pkg, err := openWebPackage(packagePath)
	if err != nil {
		return LootTuning{}, fmt.Errorf("packageOpen: %w", err)
	}
	defer r.Close()
	for ordinal, entry := range pkg.Entries {
		if entry.Type != lootTuningPropertyType || entry.Instance != lootTuningPropertyInstance {
			continue
		}
		err = ctx.Err()
		if err != nil {
			return LootTuning{}, fmt.Errorf("context[%d]: %w", ordinal, err)
		}
		decoded, openErr := pkg.Open(entry)
		if openErr != nil {
			return LootTuning{}, fmt.Errorf("propertyOpen[%d]: %w", ordinal, openErr)
		}
		payload, readErr := io.ReadAll(decoded)
		if readErr != nil {
			return LootTuning{}, fmt.Errorf("propertyRead[%d]: %w", ordinal, readErr)
		}
		tuning, parseErr := parseLootTuning(payload)
		if parseErr != nil {
			return LootTuning{}, fmt.Errorf("propertyParse[%d]: %w", ordinal, parseErr)
		}
		tuning.SourceOrdinal = ordinal
		return tuning, nil
	}
	return LootTuning{}, errors.New("propertyMissing")
}

func parseLootTuning(payload []byte) (LootTuning, error) {
	if len(payload) != lootTuningPayloadSize {
		return LootTuning{}, fmt.Errorf("payloadSize: got %d, want %d", len(payload), lootTuningPayloadSize)
	}
	tuning := LootTuning{
		RarityLevelStep:        tuningUint32(payload, 142),
		BasePoint:              tuningFloat32(payload, 143),
		ExtraStatBonusFactor:   tuningFloat32(payload, 176),
		HandLevelScale:         tuningFloat32(payload, 208),
		HandFlat:               tuningFloat32(payload, 209),
		FootLevelScale:         tuningFloat32(payload, 210),
		FootFlat:               tuningFloat32(payload, 211),
		DefenseFlat:            tuningFloat32(payload, 213),
		OffenseFlat:            tuningFloat32(payload, 212),
		UtilityFlat:            tuningFloat32(payload, 214),
		WeaponDamageMultiplier: tuningFloat32(payload, 221),
		PriceBase:              tuningFloat32(payload, 204),
		PriceCurve:             tuningFloat32(payload, 205),
		PriceIncrement:         tuningUint32(payload, 206),
		HandMinimumLevel:       tuningUint32(payload, 215),
		FootMinimumLevel:       tuningUint32(payload, 216),
		WeaponMinimumLevel:     tuningUint32(payload, 217),
	}
	for index := range tuning.PointCosts {
		tuning.PointCosts[index] = tuningUint32(payload, 177+index)
	}
	for index := range tuning.UncommonChances {
		tuning.UncommonChances[index] = tuningFloat32(payload, 243+index)
		tuning.RareChances[index] = tuningFloat32(payload, 262+index)
		tuning.EpicChances[index] = tuningFloat32(payload, 281+index)
	}
	tuning.RarityDistributions = [4]LootRarityDistribution{
		{Suffix: tuningFloat32(payload, 191), Standard: tuningFloat32(payload, 190)},
		{Suffix: tuningFloat32(payload, 192), Prefix: tuningFloat32(payload, 193), Prefix2: tuningFloat32(payload, 194), Standard: tuningFloat32(payload, 195)},
		{Suffix: tuningFloat32(payload, 196), Prefix: tuningFloat32(payload, 197), Prefix2: tuningFloat32(payload, 198), Standard: tuningFloat32(payload, 199)},
		{Suffix: tuningFloat32(payload, 200), Prefix: tuningFloat32(payload, 201), Prefix2: tuningFloat32(payload, 202), Standard: tuningFloat32(payload, 203)},
	}
	bandText := string(payload[576:])
	if terminator := strings.IndexByte(bandText, 0); terminator >= 0 {
		bandText = bandText[:terminator]
	}
	fields := strings.Split(bandText, ",")
	if len(fields) == 0 || len(fields)%2 != 0 {
		return LootTuning{}, fmt.Errorf("levelBands: %q", bandText)
	}
	for index := 0; index < len(fields); index += 2 {
		base, err := strconv.ParseFloat(strings.TrimSpace(fields[index]), 32)
		if err != nil {
			return LootTuning{}, fmt.Errorf("bandBase[%d]: %w", index/2, err)
		}
		maximum, err := strconv.ParseUint(strings.TrimSpace(fields[index+1]), 10, 32)
		if err != nil {
			return LootTuning{}, fmt.Errorf("bandMaximum[%d]: %w", index/2, err)
		}
		tuning.LevelBands = append(tuning.LevelBands, LootLevelBand{Base: float32(base), MaximumLevel: uint32(maximum)})
	}
	err := validateLootTuning(tuning)
	if err != nil {
		return LootTuning{}, err
	}
	return tuning, nil
}

func validateLootTuning(tuning LootTuning) error {
	if tuning.RarityLevelStep == 0 || tuning.BasePoint <= 0 || tuning.ExtraStatBonusFactor < 0 ||
		tuning.WeaponDamageMultiplier <= 0 || tuning.PriceBase <= 0 || tuning.PriceCurve <= 0 ||
		tuning.PriceIncrement == 0 || math.IsNaN(float64(tuning.PriceBase)) ||
		math.IsInf(float64(tuning.PriceBase), 0) || math.IsNaN(float64(tuning.PriceCurve)) ||
		math.IsInf(float64(tuning.PriceCurve), 0) {
		return errors.New("scaleInvalid")
	}
	previousMaximum := uint32(0)
	for index, band := range tuning.LevelBands {
		if band.Base <= 0 || math.IsNaN(float64(band.Base)) || math.IsInf(float64(band.Base), 0) ||
			band.MaximumLevel <= previousMaximum {
			return fmt.Errorf("levelBand[%d]: %#v", index, band)
		}
		previousMaximum = band.MaximumLevel
	}
	for index, pointCost := range tuning.PointCosts {
		if pointCost == 0 && index != 6 && index != 7 && index != 9 {
			return fmt.Errorf("pointCost[%d]: zero", index)
		}
	}
	for index := range tuning.UncommonChances {
		uncommonChance := tuning.UncommonChances[index]
		rareChance := tuning.RareChances[index]
		epicChance := tuning.EpicChances[index]
		if uncommonChance < rareChance || rareChance < epicChance || epicChance < 0 || uncommonChance > 100 {
			return fmt.Errorf("rarityChance[%d]: %g/%g/%g", index, uncommonChance, rareChance, epicChance)
		}
	}
	return nil
}

func tuningUint32(payload []byte, index int) uint32 {
	return binary.LittleEndian.Uint32(payload[index*4:])
}

func tuningFloat32(payload []byte, index int) float32 {
	return math.Float32frombits(tuningUint32(payload, index))
}
