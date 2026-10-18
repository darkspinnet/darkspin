package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type RewardWeight struct {
	Ordinal      uint32
	Name         string
	Weight       float32
	MinimumStage uint32
}

type RewardTuning struct {
	MajorLevelMultiplier  uint32
	MinorLevelMultiplier  uint32
	LastMinorLevelBonus   uint32
	RarityLevelMultiplier uint32
	SlotWeights           []RewardWeight
	ScienceWeights        []RewardWeight
	EliteRewardBonus      uint32
	DNA                   DNARewardTuning          `json:"-"` // Loaded from its separate ServerData source.
	Progression           RewardProgressionTuning  `json:"-"`
	Resurrection          ResurrectionPickupTuning `json:"-"`
}

func writeRewardTuning(ctx context.Context, transaction *sql.Tx, installPath string) (resultErr error) {
	r, err := os.Open(filepath.Join(installPath, "Data", lootAssetPackage))
	if err != nil {
		return fmt.Errorf("rewardOpen: %w", err)
	}
	defer closeSectionReader(r, &resultErr)
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("rewardStat: %w", err)
	}
	pkg, err := importPackageReader(ctx, r, fi.Size())
	if err != nil {
		return fmt.Errorf("rewardPackage: %w", err)
	}
	for ordinal, entry := range pkg.Entries {
		if entry.Type != 0x61bf29aa || entry.Group != 0 || uint32(entry.Instance) != 0x534b4478 {
			continue
		}
		payload, readErr := readDecodedResource(ctx, pkg, entry)
		if readErr != nil {
			return fmt.Errorf("rewardRead: %w", readErr)
		}
		if len(payload) != 1212 {
			return fmt.Errorf("rewardSize: %d", len(payload))
		}
		tuning := RewardTuning{MajorLevelMultiplier: binary.LittleEndian.Uint32(payload[556:]),
			MinorLevelMultiplier:  binary.LittleEndian.Uint32(payload[560:]),
			LastMinorLevelBonus:   binary.LittleEndian.Uint32(payload[564:]),
			RarityLevelMultiplier: binary.LittleEndian.Uint32(payload[568:])}
		slotNames := []string{"offense", "defense", "utility", "weapon", "foot", "grasper"}
		minimumOffsets := []int{872, 876, 880, 868, 864, 860}
		for index, name := range slotNames {
			weight := readFloat32(payload, index*4)
			if !isFiniteNonNegative(weight) {
				return fmt.Errorf("rewardSlot[%d]: invalid", index)
			}
			tuning.SlotWeights = append(tuning.SlotWeights, RewardWeight{Ordinal: uint32(index), Name: name, Weight: weight,
				MinimumStage: binary.LittleEndian.Uint32(payload[minimumOffsets[index]:])})
		}
		for index := range 5 {
			weight := readFloat32(payload, 24+index*4)
			if !isFiniteNonNegative(weight) {
				return fmt.Errorf("rewardScience[%d]: invalid", index)
			}
			tuning.ScienceWeights = append(tuning.ScienceWeights, RewardWeight{Ordinal: uint32(index), Weight: weight, MinimumStage: 1})
		}
		encoded, marshalErr := json.Marshal(tuning)
		if marshalErr != nil {
			return fmt.Errorf("rewardMarshal: %w", marshalErr)
		}
		result, insertErr := transaction.ExecContext(ctx, `INSERT INTO reward_tuning (id, content_source_resource_id, decoded_tuning)
			SELECT 1, resource.id, ? FROM content_source_resource AS resource
			JOIN content_source_package AS package ON package.id=resource.content_source_package_id
			WHERE package.package_name=? AND resource.ordinal=?`, string(encoded), lootAssetPackage, ordinal)
		if insertErr != nil {
			return fmt.Errorf("rewardInsert: %w", insertErr)
		}
		count, countErr := result.RowsAffected()
		if countErr != nil {
			return fmt.Errorf("rewardCount: %w", countErr)
		}
		if count != 1 {
			return fmt.Errorf("rewardCount: got %d", count)
		}
		return nil
	}
	return errors.New("reward tuning resource missing")
}

func (e *Store) RewardTuning(ctx context.Context) (RewardTuning, error) {
	if e == nil || e.database == nil || ctx == nil {
		return RewardTuning{}, errors.New("reward store or context unavailable")
	}
	var encoded string
	err := e.database.QueryRowContext(ctx, `SELECT decoded_tuning FROM reward_tuning WHERE id=1`).Scan(&encoded)
	if err != nil {
		return RewardTuning{}, fmt.Errorf("rewardQuery: %w", err)
	}
	var tuning RewardTuning
	err = json.Unmarshal([]byte(encoded), &tuning)
	if err != nil {
		return RewardTuning{}, fmt.Errorf("rewardDecode: %w", err)
	}
	tuning.EliteRewardBonus, err = e.directorInteger(ctx, 0x52356bef)
	if err != nil {
		return RewardTuning{}, fmt.Errorf("rewardEliteBonus: %w", err)
	}
	tuning.DNA, err = e.DNARewardTuning(ctx)
	if err != nil {
		return RewardTuning{}, fmt.Errorf("rewardDNA: %w", err)
	}
	tuning.Progression, err = e.RewardProgressionTuning(ctx)
	if err != nil {
		return RewardTuning{}, fmt.Errorf("rewardProgression: %w", err)
	}
	tuning.Resurrection, err = e.ResurrectionPickupTuning(ctx)
	if err != nil {
		return RewardTuning{}, fmt.Errorf("rewardResurrection: %w", err)
	}
	return tuning, nil
}
