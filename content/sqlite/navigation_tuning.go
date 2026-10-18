package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

const navigationTuningType = uint32(0xbba5400d)
const navigationTuningInstance = uint64(0x57572dac)
const navigationTuningRowSize = 24

type NavigationTuningRow struct {
	Ordinal                 int
	Name                    string
	VoxelTestSize           float32
	AgentRadius             float32
	AgentHeight             float32
	MaxStepSize             float32
	MaxWalkableSlopeDegrees float32
}

func writeNavigationTuning(ctx context.Context, transaction *sql.Tx, installPath string) (resultErr error) {
	packagePath := filepath.Join(installPath, "Data", lootAssetPackage)
	r, err := os.Open(packagePath)
	if err != nil {
		return fmt.Errorf("navigationTuningOpen: %w", err)
	}
	defer closeSectionReader(r, &resultErr)
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("navigationTuningStat: %w", err)
	}
	pkg, err := importPackageReader(ctx, r, fi.Size())
	if err != nil {
		return fmt.Errorf("navigationTuningPackage: %w", err)
	}
	for ordinal, entry := range pkg.Entries {
		if entry.Type != navigationTuningType || entry.Group != 0 || entry.Instance != navigationTuningInstance {
			continue
		}
		payload, readErr := readDecodedResource(ctx, pkg, entry)
		if readErr != nil {
			return fmt.Errorf("navigationTuningRead: %w", readErr)
		}
		rows, decodeErr := decodeNavigationTuning(payload)
		if decodeErr != nil {
			return fmt.Errorf("navigationTuningDecode: %w", decodeErr)
		}
		var resourceID int64
		err = transaction.QueryRowContext(ctx, `
			SELECT content_source_resource.id FROM content_source_resource
			JOIN content_source_package ON content_source_package.id=content_source_resource.content_source_package_id
			WHERE content_source_package.package_name=? AND content_source_resource.ordinal=?`,
			lootAssetPackage, ordinal).Scan(&resourceID)
		if err != nil {
			return fmt.Errorf("navigationTuningSource: %w", err)
		}
		for _, row := range rows {
			_, err = transaction.ExecContext(ctx, `
				INSERT INTO navigation_tuning
				(ordinal, content_source_resource_id, name, voxel_test_size, agent_radius,
				 agent_height, max_step_size, max_walkable_slope_degrees)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				row.Ordinal, resourceID, row.Name, row.VoxelTestSize, row.AgentRadius,
				row.AgentHeight, row.MaxStepSize, row.MaxWalkableSlopeDegrees)
			if err != nil {
				return fmt.Errorf("navigationTuningInsert[%d]: %w", row.Ordinal, err)
			}
		}
		return nil
	}
	return errors.New("navigation tuning resource missing")
}

func decodeNavigationTuning(payload []byte) ([]NavigationTuningRow, error) {
	if len(payload) < 12 {
		return nil, errors.New("navigation tuning header truncated")
	}
	count := binary.LittleEndian.Uint32(payload[8:12])
	if count == 0 || count > 32 || binary.LittleEndian.Uint32(payload[4:8]) == 0 {
		return nil, errors.New("navigation tuning header invalid")
	}
	namesOffset := 12 + int(count)*navigationTuningRowSize
	if namesOffset > len(payload) {
		return nil, errors.New("navigation tuning rows truncated")
	}
	slope := math.Float32frombits(binary.LittleEndian.Uint32(payload[0:4]))
	if !isNavigationTuningScalar(slope) {
		return nil, errors.New("navigation tuning slope invalid")
	}
	rows := make([]NavigationTuningRow, 0, count)
	for index := 0; index < int(count); index++ {
		offset := 12 + index*navigationTuningRowSize
		if binary.LittleEndian.Uint32(payload[offset:offset+4]) == 0 {
			return nil, fmt.Errorf("navigationTuningName[%d]: missing", index)
		}
		row := NavigationTuningRow{
			Ordinal:                 index,
			VoxelTestSize:           math.Float32frombits(binary.LittleEndian.Uint32(payload[offset+4:])),
			AgentRadius:             math.Float32frombits(binary.LittleEndian.Uint32(payload[offset+8:])),
			AgentHeight:             math.Float32frombits(binary.LittleEndian.Uint32(payload[offset+12:])),
			MaxStepSize:             math.Float32frombits(binary.LittleEndian.Uint32(payload[offset+16:])),
			MaxWalkableSlopeDegrees: math.Float32frombits(binary.LittleEndian.Uint32(payload[offset+20:])),
		}
		if !isNavigationTuningScalar(row.VoxelTestSize) || !isNavigationTuningScalar(row.AgentRadius) ||
			!isNavigationTuningScalar(row.AgentHeight) || !isNavigationTuningScalar(row.MaxStepSize) ||
			!isNavigationTuningScalar(row.MaxWalkableSlopeDegrees) {
			return nil, fmt.Errorf("navigationTuningRow[%d]: invalid", index)
		}
		nameEnd := bytes.IndexByte(payload[namesOffset:], 0)
		if nameEnd <= 0 {
			return nil, fmt.Errorf("navigationTuningName[%d]: invalid", index)
		}
		row.Name = string(payload[namesOffset : namesOffset+nameEnd])
		namesOffset += nameEnd + 1
		rows = append(rows, row)
	}
	if namesOffset != len(payload) {
		return nil, errors.New("navigation tuning trailing data")
	}
	return rows, nil
}

func isNavigationTuningScalar(number float32) bool {
	return number > 0 && !math.IsNaN(float64(number)) && !math.IsInf(float64(number), 0)
}

func (s *Store) NavigationTuning(ctx context.Context) ([]NavigationTuningRow, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("navigation tuning store unavailable")
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT ordinal, name, voxel_test_size, agent_radius, agent_height,
		       max_step_size, max_walkable_slope_degrees
		FROM navigation_tuning ORDER BY ordinal`)
	if err != nil {
		return nil, fmt.Errorf("navigationTuningQuery: %w", err)
	}
	tunings := make([]NavigationTuningRow, 0)
	for rows.Next() {
		var row NavigationTuningRow
		err = rows.Scan(&row.Ordinal, &row.Name, &row.VoxelTestSize, &row.AgentRadius,
			&row.AgentHeight, &row.MaxStepSize, &row.MaxWalkableSlopeDegrees)
		if err != nil {
			closeErr := rows.Close()
			return nil, fmt.Errorf("navigationTuningScan: %w", errors.Join(err, closeErr))
		}
		if row.Ordinal != len(tunings) || row.Name == "" || !isNavigationTuningScalar(row.AgentRadius) {
			closeErr := rows.Close()
			return nil, fmt.Errorf("navigationTuningOrdinal[%d]: %w", row.Ordinal,
				errors.Join(errors.New("invalid row"), closeErr))
		}
		tunings = append(tunings, row)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil || closeErr != nil {
		return nil, fmt.Errorf("navigationTuningRows: %w", errors.Join(err, closeErr))
	}
	if len(tunings) == 0 {
		return nil, errors.New("navigation tuning absent")
	}
	return tunings, nil
}
