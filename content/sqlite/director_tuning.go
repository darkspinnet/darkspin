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

	"github.com/darkspinnet/darkspin/content/render/prop"
)

const directorTuningType = uint32(0x9342c4d3)
const directorTuningInstance = uint32(0x4c24cf9a)
const directorTuningDifficultyCount = 72
const directorCompositionPackage = "ServerData.package"
const directorCompositionIdentity = uint32(0x3b01d7f6)
const groupChallengeMultiplierProperty = uint32(0x2ccf993d)

// Packaged properties consumed by composition, promotion, and rewards.
// Composition/global pairs come from sub_9D4160 (103: sub_9CF190); the
// allowlist identifies references, while settings come from the package.
var directorCompositionPropertyIDs = [...]uint32{
	campaignMajorStageCountProperty,
	campaignMinorStageCountProperty,
	sidekickProgressionGapProperty,
	forceSidekickingProperty,
	resurrectionHealthFractionProperty,
	0xee91716a, // packaged locomotion acceleration
	0x3b27f50b, // packaged locomotion deceleration
	0x8cd645ab, // packaged locomotion turn rate
	0xaf478c01, // packaged minion promotion stage
	0x0d57df60, // packaged special promotion stage
	0x52356bef, // packaged elite reward bonus
	0xd1720c6b, // 0x14D2404
	0x647501e6, // 0x14D2408
	0x3bc621ab, // 0x14D240C
	0x488a5926, // 0x14D2410
	0x0d455dd1, // 0x14D2414
	0x45b39995, // 0x14D2420
	0x65648990, // 0x14D2417
	0xa11bb5d8, // 0x14D2428
	0x1906c273, // 0x14D242C
	0x2dd23a06, // 0x14D2430
	0xf50d4766, // 0x14D2434
	0x3c6b0e8a, // 0x14D2438
	0x4823753e, // 0x14D243C
	0x98a33e2f, // 0x14D2440
	0x13501682, // 0x14D2444
	0x8c1fb30b, // 0x14D2448
	0x883ab9be, // 0x14D244C
	0xb7efceb1, // 0x14D2450
	0xe3c01650, // 0x14D2454
	0x39f53715, // 0x14D2458
	0xc1f816c8, // 0x14D245C
	0xe05bbc10, // 0x14D2460
	0x28d06252, // 0x14D2464
	0xa3284d88, // 0x14D2468
	0x1b8c7beb, // 0x14D246C
	0xb177ff33, // 0x14D2470
	0xabc25225, // 0x14D2474
	0xdff847fb, // 0x14D2478
	0x7c61d765, // 0x14D247C
	0x94437b1f, // 0x14D2480
	0x6652a7f2, // 0x14D2484
	0xb7a59b8b, // 0x14D2488
	0xb7a59b8c, // 0x14D248C
	0x2747107a, // 0x14D2490
	0x057c913b, // 0x14D2494
	0x61735c75, // 0x14D2498
	0x5b780357, // 0x14D249C
	0xa547e347, // 0x14D24A0
	0xdf7e68d1, // 0x14D24A4
	0x2edb3805, // 0x14D24A8
	0xe0da6cd6, // 0x14D24AC
	0x9931d732, // 0x14D24B0
	0x2ccf993d, // 0x14D24B4
	0xaa3e7e3b, // 0x14D24B8
	0xb23e8a95, // 0x14D24BC
	0xb041970d, // 0x14D24C0
	0xd2415d2d, // 0x14D24C4
	0x379b0a59, // 0x14D24C8
	0xe96a97dc, // 0x14D24CC
	0x1125c67b, // 0x14D24D0
	0x5f812b5b, // 0x14D24D4
	0xadbf46ca, // 0x14D24D8
	0xd70d9b4f, // 0x14D24DC
	0x7fb03f2c, // 0x14D24E0
	0xcf00e30d, // 0x14D24E4
	0xc8110459, // 0x14D24E8
	0x0d693b80, // 0x14D24EC
	0x0fa852ca, // 0x14D24F0
	0xa280a5fb, // 0x14D24F4
	0x6f47f962, // 0x14D24F8
	0xbd1f8de2, // 0x14D2500
	0xa635e885, // 0x14D24FC
	0x891f086d, // 0x14D2504
	0x114a1875, // 0x14D2508
	0x4f999e18, // 0x14D250C
	0x873b5a85, // 0x14D2510
	0xf2b39d4d, // 0x14D2514
	0x983472a5, // 0x14D2518
	0xc8c61126, // 0x14D251C
	0x7f3cbc6e, // 0x14D2520
	0x7da32098, // 0x14D2524
	0x4fa63095, // 0x14D2528
	0x0011614b, // 0x14D252C
	0xd5fc586d, // 0x14D2530
	0x0cbe627a, // 0x14D2534
	0x7ada01b8, // 0x14D2538
	0xd496f904, // 0x14D253C
	0xa7247820, // 0x14D254C
	0x69f62791, // 0x14D2550
	0x394bd60b, // 0x14D2554
	0x0bfc355e, // 0x14D2540
	0xd2d6eaf8, // 0x14D2544
	0x31aff984, // 0x14D2548
	0xeedfb45f, // 0x14D2558
	0xc1e41c7a, // 0x14D255C
	0x96fd722d, // 0x14D2560
	0xe5d8b7ea, // 0x14D2564
	0xbb2e9927, // 0x14D2568
	0x2c30a311, // 0x14D256C
	0x69b037c9, // 0x14D2570
	0xeb1220ab, // 0x14D2574
	0x941bb9c0, // 0x14D2578
	0x49efa7d0, // 0x14D257C
	0x3e562103, // 0x14D2580
	0x3ecacc2e, // 0x14D2584
	0x7bec4ca4, // 0x14D2588
	0xeec36eba, // 0x14D2589
}

func (e *Store) directorInteger(ctx context.Context, propertyID uint32) (uint32, error) {
	propertyType, encodedItem, err := e.directorProperty(ctx, propertyID)
	if err != nil {
		return 0, fmt.Errorf("integerProperty: %w", err)
	}
	if (propertyType != prop.TypeInt32 && propertyType != prop.TypeUInt32) || len(encodedItem) != 4 {
		return 0, fmt.Errorf("integerShape[%#x]: type %#x size %d", propertyID, propertyType, len(encodedItem))
	}
	return binary.BigEndian.Uint32(encodedItem), nil
}

func (e *Store) directorFloat(ctx context.Context, propertyID uint32) (float32, error) {
	propertyType, encodedItem, err := e.directorProperty(ctx, propertyID)
	if err != nil {
		return 0, fmt.Errorf("floatProperty: %w", err)
	}
	if propertyType != prop.TypeFloat || len(encodedItem) != 4 {
		return 0, fmt.Errorf("floatShape[%#x]: type %#x size %d", propertyID, propertyType, len(encodedItem))
	}
	return math.Float32frombits(binary.BigEndian.Uint32(encodedItem)), nil
}

func (e *Store) directorProperty(ctx context.Context, propertyID uint32) (uint16, []byte, error) {
	if e == nil || e.database == nil || ctx == nil {
		return 0, nil, errors.New("director property store unavailable")
	}
	var propertyType uint16
	var encodedItem []byte
	err := e.database.QueryRowContext(ctx, `SELECT property_type, encoded_item FROM director_composition_property
		WHERE property_id=?`, int64(propertyID)).Scan(&propertyType, &encodedItem)
	if err != nil {
		return 0, nil, fmt.Errorf("propertyQuery[%#x]: %w", propertyID, err)
	}
	return propertyType, encodedItem, nil
}

// DirectorTuning is authored difficulty data, independent of a level director.
type DirectorTuning struct {
	Difficulty          int
	OrbDifficultyScale  float32
	HordeDifficultyWave uint32
}

// DirectorCompositionTuning projects only settings with a proven runtime use.
// The remaining consumed properties retain their authored types in storage.
type DirectorCompositionTuning struct {
	GroupChallengeMultiplier float32
	ResurrectionPickup       ResurrectionPickupTuning
}

func writeDirectorTuning(ctx context.Context, transaction *sql.Tx, installPath string) (resultErr error) {
	packagePath := filepath.Join(installPath, "Data", lootAssetPackage)
	r, err := os.Open(packagePath)
	if err != nil {
		return fmt.Errorf("tuningOpen: %w", err)
	}
	defer closeSectionReader(r, &resultErr)
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("tuningStat: %w", err)
	}
	pkg, err := importPackageReader(ctx, r, fi.Size())
	if err != nil {
		return fmt.Errorf("tuningPackage: %w", err)
	}
	for ordinal, entry := range pkg.Entries {
		if entry.Type != directorTuningType || uint32(entry.Instance) != directorTuningInstance {
			continue
		}
		payload, readErr := readDecodedResource(ctx, pkg, entry)
		if readErr != nil {
			return fmt.Errorf("tuningRead: %w", readErr)
		}
		tunings, decodeErr := decodeDirectorTuning(payload)
		if decodeErr != nil {
			return fmt.Errorf("tuningDecode: %w", decodeErr)
		}
		var resourceID int64
		err = transaction.QueryRowContext(ctx, `
			SELECT content_source_resource.id FROM content_source_resource
			JOIN content_source_package ON content_source_package.id=content_source_resource.content_source_package_id
			WHERE content_source_package.package_name=? AND content_source_resource.ordinal=?`,
			lootAssetPackage, ordinal).Scan(&resourceID)
		if err != nil {
			return fmt.Errorf("tuningSource: %w", err)
		}
		statement, prepareErr := transaction.PrepareContext(ctx, `
			INSERT INTO director_tuning
			(difficulty, content_source_resource_id, orb_difficulty_scale, horde_difficulty_wave)
			VALUES (?, ?, ?, ?)`)
		if prepareErr != nil {
			return fmt.Errorf("tuningPrepare: %w", prepareErr)
		}
		for _, tuning := range tunings {
			_, execErr := statement.ExecContext(ctx, tuning.Difficulty, resourceID,
				tuning.OrbDifficultyScale, tuning.HordeDifficultyWave)
			if execErr != nil {
				closeErr := statement.Close()
				return fmt.Errorf("tuningInsert[%d]: %w", tuning.Difficulty, errors.Join(execErr, closeErr))
			}
		}
		err = statement.Close()
		if err != nil {
			return fmt.Errorf("tuningClose: %w", err)
		}
		err = writeDirectorCompositionTuning(ctx, transaction, installPath)
		if err != nil {
			return fmt.Errorf("compositionWrite: %w", err)
		}
		return nil
	}
	return errors.New("directorTuning missing")
}

func decodeDirectorTuning(payload []byte) ([]DirectorTuning, error) {
	const headerSize = 16
	const elementSize = 4
	if len(payload) != headerSize+directorTuningDifficultyCount*elementSize*2 {
		return nil, fmt.Errorf("tuningSize: %d", len(payload))
	}
	if binary.LittleEndian.Uint32(payload[4:8]) != directorTuningDifficultyCount ||
		binary.LittleEndian.Uint32(payload[12:16]) != directorTuningDifficultyCount {
		return nil, errors.New("tuningCount mismatch")
	}
	tunings := make([]DirectorTuning, 0, directorTuningDifficultyCount)
	for index := range directorTuningDifficultyCount {
		scaleOffset := headerSize + index*elementSize
		waveOffset := headerSize + directorTuningDifficultyCount*elementSize + index*elementSize
		scale := math.Float32frombits(binary.LittleEndian.Uint32(payload[scaleOffset:]))
		wave := binary.LittleEndian.Uint32(payload[waveOffset:])
		if math.IsNaN(float64(scale)) || math.IsInf(float64(scale), 0) || scale < 0 {
			return nil, fmt.Errorf("tuningScale[%d]: invalid", index)
		}
		tunings = append(tunings, DirectorTuning{Difficulty: index + 1,
			OrbDifficultyScale: scale, HordeDifficultyWave: wave})
	}
	return tunings, nil
}

// DirectorTunings returns the full difficulty series without selecting a level.
func (s *Store) DirectorTunings(ctx context.Context) ([]DirectorTuning, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("nil store")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT difficulty, orb_difficulty_scale, horde_difficulty_wave
		FROM director_tuning ORDER BY difficulty`)
	if err != nil {
		return nil, fmt.Errorf("tuningQuery: %w", err)
	}
	tunings := make([]DirectorTuning, 0, directorTuningDifficultyCount)
	for rows.Next() {
		var tuning DirectorTuning
		err = rows.Scan(&tuning.Difficulty, &tuning.OrbDifficultyScale, &tuning.HordeDifficultyWave)
		if err != nil {
			closeErr := rows.Close()
			return nil, fmt.Errorf("tuningScan: %w", errors.Join(err, closeErr))
		}
		tunings = append(tunings, tuning)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return nil, fmt.Errorf("tuningRows: %w", errors.Join(err, closeErr))
	}
	if closeErr != nil {
		return nil, fmt.Errorf("tuningClose: %w", closeErr)
	}
	return tunings, nil
}

func writeDirectorCompositionTuning(
	ctx context.Context, transaction *sql.Tx, installPath string,
) (resultErr error) {
	r, err := os.Open(filepath.Join(installPath, "Data", directorCompositionPackage))
	if err != nil {
		return fmt.Errorf("compositionOpen: %w", err)
	}
	defer closeSectionReader(r, &resultErr)
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("compositionStat: %w", err)
	}
	pkg, err := importPackageReader(ctx, r, fi.Size())
	if err != nil {
		return fmt.Errorf("compositionPackage: %w", err)
	}
	if len(pkg.Entries) == 0 {
		return errors.New("composition resource missing")
	}
	entry := pkg.Entries[0]
	if entry.Type != prop.ResourceType || entry.Group != directorCompositionIdentity ||
		entry.Instance != uint64(directorCompositionIdentity) {
		return errors.New("composition resource identity mismatch")
	}
	payload, err := readDecodedResource(ctx, pkg, entry)
	if err != nil {
		return fmt.Errorf("compositionRead: %w", err)
	}
	properties, err := decodeDirectorCompositionProperties(payload)
	if err != nil {
		return fmt.Errorf("compositionDecode: %w", err)
	}
	var resourceID int64
	err = transaction.QueryRowContext(ctx, `
		SELECT resource.id FROM content_source_resource AS resource
		JOIN content_source_package AS package ON package.id=resource.content_source_package_id
		WHERE package.package_name=? AND resource.ordinal=0
		  AND resource.type_id=? AND resource.group_id=? AND resource.instance_id=?`,
		directorCompositionPackage, int64(prop.ResourceType),
		int64(directorCompositionIdentity), int64(directorCompositionIdentity)).Scan(&resourceID)
	if err != nil {
		return fmt.Errorf("compositionSource: %w", err)
	}
	statement, err := transaction.PrepareContext(ctx, `
		INSERT INTO director_composition_property
		(property_id, content_source_resource_id, property_type, encoded_item)
		VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("compositionPrepare: %w", err)
	}
	for _, property := range properties {
		result, execErr := statement.ExecContext(ctx, int64(property.ID), resourceID,
			int64(property.Type), property.Items[0])
		if execErr != nil {
			closeErr := statement.Close()
			return fmt.Errorf("compositionInsert[%#x]: %w", property.ID, errors.Join(execErr, closeErr))
		}
		count, countErr := result.RowsAffected()
		if countErr != nil {
			closeErr := statement.Close()
			return fmt.Errorf("compositionCount[%#x]: %w", property.ID, errors.Join(countErr, closeErr))
		}
		if count != 1 {
			closeErr := statement.Close()
			return fmt.Errorf("compositionCount[%#x]: %w", property.ID,
				errors.Join(fmt.Errorf("got %d", count), closeErr))
		}
	}
	err = statement.Close()
	if err != nil {
		return fmt.Errorf("compositionClose: %w", err)
	}
	return nil
}

func decodeDirectorCompositionProperties(payload []byte) ([]prop.Property, error) {
	document, err := prop.Decode(payload)
	if err != nil {
		return nil, fmt.Errorf("propertyDecode: %w", err)
	}
	propertyIDs := make(map[uint32]bool, len(directorCompositionPropertyIDs))
	for _, propertyID := range directorCompositionPropertyIDs {
		propertyIDs[propertyID] = false
	}
	properties := make([]prop.Property, 0, len(propertyIDs))
	for _, property := range document.Properties {
		isFound, isConsumed := propertyIDs[property.ID]
		if !isConsumed {
			continue
		}
		if isFound {
			return nil, fmt.Errorf("propertyDuplicate[%#x]", property.ID)
		}
		if property.IsArray || len(property.Items) != 1 {
			return nil, fmt.Errorf("propertyShape[%#x]: expected scalar", property.ID)
		}
		err = validateDirectorCompositionProperty(property)
		if err != nil {
			return nil, fmt.Errorf("propertyValidate[%#x]: %w", property.ID, err)
		}
		propertyIDs[property.ID] = true
		properties = append(properties, property)
	}
	for _, propertyID := range directorCompositionPropertyIDs {
		// Missing movement properties retain absence for the native per-key
		// fallback; the reflected noun defaults are a separate input.
		if propertyID == 0xee91716a || propertyID == 0x3b27f50b || propertyID == 0x8cd645ab {
			continue
		}
		// Reward progression properties retain absence separately from the
		// initializer's compiled defaults (10, 4, 4, false).
		if propertyID == campaignMajorStageCountProperty || propertyID == campaignMinorStageCountProperty || propertyID == sidekickProgressionGapProperty ||
			propertyID == forceSidekickingProperty || propertyID == resurrectionHealthFractionProperty {
			continue
		}
		// The initializer requests these three properties, but build 103
		// does not package them. Preserve absence instead of fabricating an
		// authored override from the executable's fallback literal.
		if propertyID == 0xb7a59b8c || propertyID == 0x7bec4ca4 || propertyID == 0xeec36eba {
			continue
		}
		if !propertyIDs[propertyID] {
			return nil, fmt.Errorf("propertyMissing[%#x]", propertyID)
		}
	}
	return properties, nil
}

func validateDirectorCompositionProperty(property prop.Property) error {
	if property.ID == resurrectionHealthFractionProperty &&
		(property.Type != prop.TypeFloat || len(property.Items[0]) != 4) {
		return errors.New("resurrection fraction must be a float")
	}
	if property.ID == campaignMajorStageCountProperty || property.ID == campaignMinorStageCountProperty || property.ID == sidekickProgressionGapProperty ||
		property.ID == forceSidekickingProperty {
		err := validateRewardProgressionProperty(property.ID, property.Type, property.Items[0])
		if err != nil {
			return fmt.Errorf("rewardProgression: %w", err)
		}
		return nil
	}
	item := property.Items[0]
	switch property.Type {
	case prop.TypeBool:
		if len(item) != 1 || item[0] > 1 {
			return errors.New("invalid bool")
		}
	case prop.TypeInt32, prop.TypeUInt32:
		if len(item) != 4 {
			return errors.New("invalid integer")
		}
	case prop.TypeFloat:
		if len(item) != 4 {
			return errors.New("invalid float size")
		}
		number := math.Float32frombits(binary.BigEndian.Uint32(item))
		if math.IsNaN(float64(number)) || math.IsInf(float64(number), 0) {
			return errors.New("invalid float")
		}
	default:
		return fmt.Errorf("unsupported type %#x", property.Type)
	}
	return nil
}

// DirectorCompositionTuning loads packaged settings used by group composition.
func (e *Store) DirectorCompositionTuning(ctx context.Context) (DirectorCompositionTuning, error) {
	if e == nil || e.database == nil || ctx == nil {
		return DirectorCompositionTuning{}, errors.New("composition store unavailable")
	}
	var propertyType uint16
	var encodedItem []byte
	err := e.database.QueryRowContext(ctx, `
		SELECT property_type, encoded_item FROM director_composition_property
		WHERE property_id=?`, int64(groupChallengeMultiplierProperty)).Scan(&propertyType, &encodedItem)
	if err != nil {
		return DirectorCompositionTuning{}, fmt.Errorf("compositionQuery: %w", err)
	}
	if propertyType != prop.TypeFloat || len(encodedItem) != 4 {
		return DirectorCompositionTuning{}, errors.New("group multiplier property invalid")
	}
	multiplier := math.Float32frombits(binary.BigEndian.Uint32(encodedItem))
	if math.IsNaN(float64(multiplier)) || math.IsInf(float64(multiplier), 0) || multiplier < 0 {
		return DirectorCompositionTuning{}, errors.New("group multiplier invalid")
	}
	resurrection, err := e.ResurrectionPickupTuning(ctx)
	if err != nil {
		return DirectorCompositionTuning{}, fmt.Errorf("compositionResurrection: %w", err)
	}
	return DirectorCompositionTuning{GroupChallengeMultiplier: multiplier, ResurrectionPickup: resurrection}, nil
}
