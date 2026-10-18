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
)

const (
	sectionConfigType     = 0xa38fa119
	sectionConfigInstance = 0x45b7b07c
	sectionBucketCount    = 72
	sectionBucketSize     = 16
)

// SectionBucket is one authored SectionConfig archetype-count choice.
// Its counts do not specify how many markers become occupied.
type SectionBucket struct {
	Ordinal      int
	Difficulty   int
	MinionCount  int
	SpecialCount int
	Chance       float32
}

func writeSectionBuckets(ctx context.Context, transaction *sql.Tx, installPath string) (resultErr error) {
	packagePath := filepath.Join(installPath, "Data", lootAssetPackage)
	r, err := os.Open(packagePath)
	if err != nil {
		return fmt.Errorf("sectionPackageOpen: %w", err)
	}
	defer closeSectionReader(r, &resultErr)
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("sectionPackageStat: %w", err)
	}
	pkg, err := importPackageReader(ctx, r, fi.Size())
	if err != nil {
		return fmt.Errorf("sectionPackageRead: %w", err)
	}
	for ordinal, entry := range pkg.Entries {
		if entry.Type != sectionConfigType || uint32(entry.Instance) != sectionConfigInstance {
			continue
		}
		decoded, openErr := pkg.Open(entry)
		if openErr != nil {
			return fmt.Errorf("sectionPayloadOpen: %w", openErr)
		}
		payload, readErr := io.ReadAll(decoded)
		if readErr != nil {
			return fmt.Errorf("sectionPayloadRead: %w", readErr)
		}
		buckets, decodeErr := decodeSectionBuckets(payload)
		if decodeErr != nil {
			return fmt.Errorf("sectionDecode: %w", decodeErr)
		}
		var resourceID int64
		err = transaction.QueryRowContext(ctx, `
			SELECT content_source_resource.id FROM content_source_resource
			JOIN content_source_package ON content_source_package.id=content_source_resource.content_source_package_id
			WHERE content_source_package.package_name=? AND content_source_resource.ordinal=?`,
			lootAssetPackage, ordinal).Scan(&resourceID)
		if err != nil {
			return fmt.Errorf("sectionSource: %w", err)
		}
		statement, prepareErr := transaction.PrepareContext(ctx, `
			INSERT INTO section_bucket
			(ordinal, content_source_resource_id, difficulty, minion_count, special_count, chance)
			VALUES (?, ?, ?, ?, ?, ?)`)
		if prepareErr != nil {
			return fmt.Errorf("sectionPrepare: %w", prepareErr)
		}
		for _, bucket := range buckets {
			insertResult, execErr := statement.ExecContext(ctx, bucket.Ordinal, resourceID,
				bucket.Difficulty, bucket.MinionCount, bucket.SpecialCount, bucket.Chance)
			if execErr != nil {
				closeErr := statement.Close()
				return fmt.Errorf("sectionInsert[%d]: %w", bucket.Ordinal, errors.Join(execErr, closeErr))
			}
			insertedCount, countErr := insertResult.RowsAffected()
			if countErr != nil {
				closeErr := statement.Close()
				return fmt.Errorf("sectionRowsAffected[%d]: %w", bucket.Ordinal, errors.Join(countErr, closeErr))
			}
			if insertedCount != 1 {
				closeErr := statement.Close()
				if closeErr != nil {
					return fmt.Errorf("sectionInsertedClose[%d]: %w", bucket.Ordinal, closeErr)
				}
				return fmt.Errorf("sectionInserted[%d]: got %d", bucket.Ordinal, insertedCount)
			}
		}
		err = statement.Close()
		if err != nil {
			return fmt.Errorf("sectionClose: %w", err)
		}
		return nil
	}
	return errors.New("sectionConfig missing")
}

func closeContentRows(rows *sql.Rows, resultErr *error) {
	err := rows.Close()
	if err != nil {
		*resultErr = errors.Join(*resultErr, fmt.Errorf("rowsClose: %w", err))
	}
}

func closeSectionReader(r *os.File, resultErr *error) {
	closeErr := r.Close()
	if closeErr != nil {
		*resultErr = errors.Join(*resultErr, fmt.Errorf("sectionPackageClose: %w", closeErr))
	}
}

func decodeSectionBuckets(payload []byte) ([]SectionBucket, error) {
	if len(payload) != 8+sectionBucketCount*sectionBucketSize {
		return nil, fmt.Errorf("sectionSize: got %d", len(payload))
	}
	if binary.LittleEndian.Uint32(payload[4:8]) != sectionBucketCount {
		return nil, errors.New("sectionCount mismatch")
	}
	buckets := make([]SectionBucket, 0, sectionBucketCount)
	for ordinal := 0; ordinal < sectionBucketCount; ordinal++ {
		offset := 8 + ordinal*sectionBucketSize
		bucket := SectionBucket{
			Ordinal:      ordinal,
			MinionCount:  int(binary.LittleEndian.Uint32(payload[offset : offset+4])),
			SpecialCount: int(binary.LittleEndian.Uint32(payload[offset+4 : offset+8])),
			Difficulty:   int(binary.LittleEndian.Uint32(payload[offset+8 : offset+12])),
			Chance:       math.Float32frombits(binary.LittleEndian.Uint32(payload[offset+12 : offset+16])),
		}
		if bucket.Difficulty != ordinal/4+1 || bucket.MinionCount < 1 || bucket.SpecialCount < 1 ||
			math.IsNaN(float64(bucket.Chance)) || math.IsInf(float64(bucket.Chance), 0) {
			return nil, fmt.Errorf("sectionBucket[%d]: invalid", ordinal)
		}
		buckets = append(buckets, bucket)
	}
	return buckets, nil
}

// SectionBuckets reads all matching authored choices in source order.
func (s *Store) SectionBuckets(ctx context.Context, difficulty uint32) ([]SectionBucket, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("nil store")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT ordinal, difficulty, minion_count, special_count, chance
		FROM section_bucket WHERE difficulty=? ORDER BY ordinal`, difficulty)
	if err != nil {
		return nil, fmt.Errorf("sectionQuery: %w", err)
	}
	buckets := make([]SectionBucket, 0, 4)
	for rows.Next() {
		var bucket SectionBucket
		err = rows.Scan(&bucket.Ordinal, &bucket.Difficulty, &bucket.MinionCount,
			&bucket.SpecialCount, &bucket.Chance)
		if err != nil {
			closeErr := rows.Close()
			return nil, fmt.Errorf("sectionScan: %w", errors.Join(err, closeErr))
		}
		buckets = append(buckets, bucket)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return nil, fmt.Errorf("sectionRows: %w", err)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("sectionRowsClose: %w", closeErr)
	}
	return buckets, nil
}
