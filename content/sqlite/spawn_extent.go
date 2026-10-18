package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

// NounSpawnExtent is the ObjectExtents size-class projection used by the
// native navigation and summon-placement queries (sub_9DE1D0/sub_9DE330).
type NounSpawnExtent struct {
	NounName string
	X        float32
	Y        float32
	Z        float32
}

func writeNounSpawnExtents(
	ctx context.Context, transaction *sql.Tx, pkg *dbpf.Reader, nounNamesByInstance map[uint32]string,
) error {
	var extentPayload []byte
	for _, entry := range pkg.Entries {
		if entry.Type != 0x3583564e || entry.Group != 0 || uint32(entry.Instance) != hashID("Global") {
			continue
		}
		payload, err := readDecodedResource(ctx, pkg, entry)
		if err != nil {
			return fmt.Errorf("extentRead: %w", err)
		}
		extentPayload = payload
		break
	}
	if len(extentPayload) != 17*16 {
		return fmt.Errorf("extentSize: %d", len(extentPayload))
	}
	for ordinal, entry := range pkg.Entries {
		if entry.Type != nounAssetType || entry.Group != nounAssetGroup {
			continue
		}
		nounName, isNamed := nounNamesByInstance[uint32(entry.Instance)]
		if !isNamed {
			continue
		}
		payload, err := readDecodedResource(ctx, pkg, entry)
		if err != nil {
			return fmt.Errorf("extentNounRead[%d]: %w", ordinal, err)
		}
		if len(payload) < 84 {
			return fmt.Errorf("extentNounSize[%d]: %d", ordinal, len(payload))
		}
		sizeClass := binary.LittleEndian.Uint32(payload[80:84])
		if sizeClass < 1 || sizeClass > 17 {
			// The native helper returns false without a supported size class;
			// placement then falls back to the supplied origin without draws.
			continue
		}
		offset := int(sizeClass-1) * 16
		x := readFloat32(extentPayload, offset)
		y := readFloat32(extentPayload, offset+4)
		z := readFloat32(extentPayload, offset+8)
		if !isFinitePositive(x) || !isFinitePositive(y) || !isFinitePositive(z) {
			return fmt.Errorf("extentClass[%d]: invalid", sizeClass)
		}
		result, err := transaction.ExecContext(ctx, `
			INSERT INTO noun_spawn_extent (noun_name, size_x, size_y, size_z)
			VALUES (?, ?, ?, ?)`, nounName, x, y, z)
		if err != nil {
			return fmt.Errorf("extentInsert[%d]: %w", ordinal, err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("extentCount[%d]: %w", ordinal, err)
		}
		if count != 1 {
			return fmt.Errorf("extentRow[%d]: %d", ordinal, count)
		}
	}
	return nil
}

func (e *Store) NounSpawnExtents(ctx context.Context) ([]NounSpawnExtent, error) {
	if e == nil || e.database == nil || ctx == nil {
		return nil, errors.New("spawn extent store or context unavailable")
	}
	rows, err := e.database.QueryContext(ctx, `
		SELECT noun_name, size_x, size_y, size_z FROM noun_spawn_extent ORDER BY noun_name`)
	if err != nil {
		return nil, fmt.Errorf("extentQuery: %w", err)
	}
	extents := make([]NounSpawnExtent, 0)
	for rows.Next() {
		var extent NounSpawnExtent
		err = rows.Scan(&extent.NounName, &extent.X, &extent.Y, &extent.Z)
		if err != nil {
			closeErr := rows.Close()
			return nil, fmt.Errorf("extentScan: %w", errors.Join(err, closeErr))
		}
		extents = append(extents, extent)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil || closeErr != nil {
		return nil, fmt.Errorf("extentRows: %w", errors.Join(err, closeErr))
	}
	return extents, nil
}
