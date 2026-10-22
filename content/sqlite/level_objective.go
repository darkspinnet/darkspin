package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const levelObjectivesAssetType uint32 = 0x68232164

// These categories are the reflected LevelObjectives fields, not five
// objective slots. Affix selection and ordinary objective selection differ.
var objectivePoolCategories = [...]string{
	"objective", "affix", "positiveAffix", "minorAffix", "majorAffix",
}

// ObjectivePoolEntry preserves a LevelKey without interpreting its unresolved
// three base words as weights, eligibility or selection policy.
type ObjectivePoolEntry struct {
	Category  string
	Ordinal   uint32
	Keys      [3]uint32
	AssetName *string
}

// LevelObjectivePool preserves an absent level binding separately from an
// authored pool with no ordinary objectives (for example, Juggernaut).
type LevelObjectivePool struct {
	AssetReference *string
	ResourceID     *int64
	Entries        []ObjectivePoolEntry
}

func decodeObjectivePool(payload []byte) ([]ObjectivePoolEntry, error) {
	cursor := assetCursor{payload: payload}
	base, err := cursor.reserve(1, 40)
	if err != nil {
		return nil, fmt.Errorf("poolHeader: %w", err)
	}
	entries := make([]ObjectivePoolEntry, 0)
	for categoryIndex, category := range objectivePoolCategories {
		fieldOffset := base + categoryIndex*8
		pointer := binary.LittleEndian.Uint32(payload[fieldOffset:])
		count := binary.LittleEndian.Uint32(payload[fieldOffset+4:])
		if (pointer == 0) != (count == 0) {
			return nil, fmt.Errorf("poolArray[%s]: inconsistent presence", category)
		}
		start, arrayErr := cursor.reserve(count, 16)
		if arrayErr != nil {
			return nil, fmt.Errorf("poolRecords[%s]: %w", category, arrayErr)
		}
		for index := range int(count) {
			offset := start + index*16
			name, referenceErr := cursor.reference(offset + 12)
			if referenceErr != nil {
				return nil, fmt.Errorf("poolName[%s:%d]: %w", category, index, referenceErr)
			}
			entries = append(entries, ObjectivePoolEntry{
				Category: category, Ordinal: uint32(index), AssetName: name,
				Keys: [3]uint32{binary.LittleEndian.Uint32(payload[offset:]),
					binary.LittleEndian.Uint32(payload[offset+4:]),
					binary.LittleEndian.Uint32(payload[offset+8:])},
			})
		}
	}
	err = cursor.finish()
	if err != nil {
		return nil, fmt.Errorf("poolTail: %w", err)
	}
	return entries, nil
}

func decodeLevelObjectiveReference(payload []byte) (*string, error) {
	cursor, err := levelConfigurationCursor(payload)
	if err != nil {
		return nil, fmt.Errorf("poolCursor: %w", err)
	}
	for _, offset := range []int{88, 92} {
		if binary.LittleEndian.Uint32(payload[offset:]) == 0 {
			continue
		}
		err = walkLevelConfiguration(&cursor)
		if err != nil {
			return nil, fmt.Errorf("poolConfig[%d]: %w", offset, err)
		}
	}
	err = cursor.references(96)
	if err != nil {
		return nil, fmt.Errorf("poolPreceding: %w", err)
	}
	reference, err := cursor.reference(100)
	if err != nil {
		return nil, fmt.Errorf("poolReference: %w", err)
	}
	if reference != nil && !strings.HasSuffix(strings.ToLower(*reference), ".levelobjectives") {
		return nil, fmt.Errorf("poolSuffix: %q", *reference)
	}
	return reference, nil
}

func writeLevelObjectivePools(ctx context.Context, transaction *sql.Tx, installPath string) (resultErr error) {
	r, err := os.Open(filepath.Join(installPath, "Data", lootAssetPackage))
	if err != nil {
		return fmt.Errorf("poolOpen: %w", err)
	}
	defer closeSectionReader(r, &resultErr)
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("poolStat: %w", err)
	}
	pkg, err := importPackageReader(ctx, r, fi.Size())
	if err != nil {
		return fmt.Errorf("poolPackage: %w", err)
	}
	for ordinal, entry := range pkg.Entries {
		if entry.Type != levelObjectivesAssetType {
			continue
		}
		payload, readErr := readDecodedResource(ctx, pkg, entry)
		if readErr != nil {
			return fmt.Errorf("poolRead[%d]: %w", ordinal, readErr)
		}
		entries, decodeErr := decodeObjectivePool(payload)
		if decodeErr != nil {
			return fmt.Errorf("poolDecode[%d]: %w", ordinal, decodeErr)
		}
		result, insertErr := transaction.ExecContext(ctx, `INSERT INTO objective_pool (id, instance_id)
			SELECT resource.id, resource.instance_id FROM content_source_resource AS resource
			JOIN content_source_package AS package ON package.id=resource.content_source_package_id
			WHERE package.package_name=? AND resource.ordinal=?`, lootAssetPackage, ordinal)
		if insertErr != nil {
			return fmt.Errorf("poolInsert[%d]: %w", ordinal, insertErr)
		}
		err = requireObjectivePoolRow(result)
		if err != nil {
			return fmt.Errorf("poolCount[%d]: %w", ordinal, err)
		}
		for _, poolEntry := range entries {
			result, insertErr = transaction.ExecContext(ctx, `INSERT INTO objective_pool_entry
				(objective_pool_id, category, ordinal, key_first, key_second, key_third, asset_name)
				SELECT pool.id, ?, ?, ?, ?, ?, ? FROM objective_pool AS pool
				WHERE pool.instance_id=?`, poolEntry.Category, poolEntry.Ordinal,
				poolEntry.Keys[0], poolEntry.Keys[1], poolEntry.Keys[2], poolEntry.AssetName, entry.Instance)
			if insertErr != nil {
				return fmt.Errorf("poolEntry[%d:%s:%d]: %w", ordinal, poolEntry.Category, poolEntry.Ordinal, insertErr)
			}
			err = requireObjectivePoolRow(result)
			if err != nil {
				return fmt.Errorf("poolEntryCount[%d:%s:%d]: %w", ordinal, poolEntry.Category, poolEntry.Ordinal, err)
			}
		}
	}
	for ordinal, entry := range pkg.Entries {
		if entry.Type != levelAssetType {
			continue
		}
		payload, readErr := readDecodedResource(ctx, pkg, entry)
		if readErr != nil {
			return fmt.Errorf("poolLevelRead[%d]: %w", ordinal, readErr)
		}
		reference, decodeErr := decodeLevelObjectiveReference(payload)
		if decodeErr != nil {
			return fmt.Errorf("poolLevelDecode[%d]: %w", ordinal, decodeErr)
		}
		var poolID *int64
		if reference != nil {
			stem := (*reference)[:len(*reference)-len(".LevelObjectives")]
			var resourceID int64
			err = transaction.QueryRowContext(ctx, `SELECT id FROM objective_pool WHERE instance_id=?`, hashID(stem)).Scan(&resourceID)
			if err != nil {
				return fmt.Errorf("poolResolve[%s]: %w", *reference, err)
			}
			poolID = &resourceID
		}
		result, insertErr := transaction.ExecContext(ctx, `INSERT INTO level_objective_pool
			(level_id, asset_reference, objective_pool_id)
			SELECT id, ?, ? FROM level WHERE package_group_id=?`, reference, poolID, entry.Instance)
		if insertErr != nil {
			return fmt.Errorf("poolBinding[%d]: %w", ordinal, insertErr)
		}
		err = requireObjectivePoolRow(result)
		if err != nil {
			return fmt.Errorf("poolBindingCount[%d]: %w", ordinal, err)
		}
	}
	return nil
}

func requireObjectivePoolRow(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("poolAffected: %w", err)
	}
	if count != 1 {
		return fmt.Errorf("poolCount: got %d, want 1", count)
	}
	return nil
}

func (e *Store) LevelObjectivePool(ctx context.Context, levelName string) (LevelObjectivePool, error) {
	if e == nil || e.database == nil || ctx == nil {
		return LevelObjectivePool{}, errors.New("objective pool store or context unavailable")
	}
	pool := LevelObjectivePool{}
	err := e.database.QueryRowContext(ctx, `SELECT binding.asset_reference, binding.objective_pool_id
		FROM level_objective_pool AS binding JOIN level_alias ON level_alias.level_id=binding.level_id
		WHERE level_alias.alias=? COLLATE NOCASE LIMIT 1`, levelName).Scan(&pool.AssetReference, &pool.ResourceID)
	if err != nil {
		return LevelObjectivePool{}, fmt.Errorf("poolLevel: %w", err)
	}
	if pool.ResourceID == nil {
		return pool, nil
	}
	rows, err := e.database.QueryContext(ctx, `SELECT category, ordinal, key_first, key_second, key_third, asset_name
		FROM objective_pool_entry WHERE objective_pool_id=?
		ORDER BY CASE category WHEN 'objective' THEN 0 WHEN 'affix' THEN 1
		WHEN 'positiveAffix' THEN 2 WHEN 'minorAffix' THEN 3 ELSE 4 END, ordinal`, *pool.ResourceID)
	if err != nil {
		return LevelObjectivePool{}, fmt.Errorf("poolQuery: %w", err)
	}
	for rows.Next() {
		entry := ObjectivePoolEntry{}
		err = rows.Scan(&entry.Category, &entry.Ordinal, &entry.Keys[0], &entry.Keys[1], &entry.Keys[2], &entry.AssetName)
		if err != nil {
			closeErr := rows.Close()
			return LevelObjectivePool{}, fmt.Errorf("poolScan: %w", errors.Join(err, closeErr))
		}
		pool.Entries = append(pool.Entries, entry)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil || closeErr != nil {
		return LevelObjectivePool{}, fmt.Errorf("poolRows: %w", errors.Join(err, closeErr))
	}
	return pool, nil
}
