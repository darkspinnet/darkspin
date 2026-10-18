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

var dnaRewardPropertyIDs = [...]uint32{0x95189906, 0x29259e01, 0x21903fa4, 0xfd06c1af, 0x7c5e2bbd}

// Nil fields retain missing authored settings. Minimum stage's native fallback
// is 1; later growth bases 1.02 and 1.01 are executable constants, not properties.
type DNARewardTuning struct {
	ContentSourceResourceID int64
	Chance                  *float32
	AmountScalar            *float32
	FirstGrowthBase         *float32
	AmountFactorRange       *[2]float32
	MinimumStage            *uint32
	Properties              []DNARewardProperty
}

type DNARewardProperty struct {
	ID          uint32
	Type        *uint16
	EncodedItem []byte
}

func writeDNARewardTuning(ctx context.Context, transaction *sql.Tx, installPath string) (resultErr error) {
	r, err := os.Open(filepath.Join(installPath, "Data", directorCompositionPackage))
	if err != nil {
		return fmt.Errorf("dnaOpen: %w", err)
	}
	defer closeSectionReader(r, &resultErr)
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("dnaStat: %w", err)
	}
	pkg, err := importPackageReader(ctx, r, fi.Size())
	if err != nil {
		return fmt.Errorf("dnaPackage: %w", err)
	}
	if len(pkg.Entries) == 0 {
		return errors.New("DNA tuning resource missing")
	}
	entry := pkg.Entries[0]
	if entry.Type != prop.ResourceType || entry.Group != directorCompositionIdentity || entry.Instance != uint64(directorCompositionIdentity) {
		return errors.New("DNA tuning resource identity mismatch")
	}
	payload, err := readDecodedResource(ctx, pkg, entry)
	if err != nil {
		return fmt.Errorf("dnaRead: %w", err)
	}
	document, err := prop.Decode(payload)
	if err != nil {
		return fmt.Errorf("dnaDecode: %w", err)
	}
	var resourceID int64
	err = transaction.QueryRowContext(ctx, `SELECT resource.id FROM content_source_resource AS resource
		JOIN content_source_package AS package ON package.id=resource.content_source_package_id
		WHERE package.package_name=? AND resource.ordinal=0 AND resource.type_id=?
		AND resource.group_id=? AND resource.instance_id=?`, directorCompositionPackage,
		prop.ResourceType, directorCompositionIdentity, directorCompositionIdentity).Scan(&resourceID)
	if err != nil {
		return fmt.Errorf("dnaSource: %w", err)
	}
	for _, propertyID := range dnaRewardPropertyIDs {
		var propertyType *uint16
		var encodedItem []byte
		for _, property := range document.Properties {
			if property.ID != propertyID {
				continue
			}
			if propertyType != nil {
				return fmt.Errorf("dnaDuplicate[%#x]", propertyID)
			}
			if property.IsArray || len(property.Items) != 1 {
				return fmt.Errorf("dnaShape[%#x]", propertyID)
			}
			err = validateDNARewardProperty(propertyID, property.Type, property.Items[0])
			if err != nil {
				return fmt.Errorf("dnaValidate: %w", err)
			}
			kind := property.Type
			propertyType = &kind
			encodedItem = property.Items[0]
		}
		result, insertErr := transaction.ExecContext(ctx, `INSERT INTO dna_reward_property
			(property_id, content_source_resource_id, property_type, encoded_item) VALUES (?, ?, ?, ?)`,
			propertyID, resourceID, propertyType, encodedItem)
		if insertErr != nil {
			return fmt.Errorf("dnaInsert[%#x]: %w", propertyID, insertErr)
		}
		count, countErr := result.RowsAffected()
		if countErr != nil {
			return fmt.Errorf("dnaRows: %w", countErr)
		}
		if count != 1 {
			return fmt.Errorf("dnaCount: %d", count)
		}
	}
	return nil
}

func validateDNARewardProperty(propertyID uint32, propertyType uint16, payload []byte) error {
	if propertyID == 0x7c5e2bbd {
		if (propertyType != prop.TypeInt32 && propertyType != prop.TypeUInt32) || len(payload) != 4 {
			return errors.New("invalid DNA minimum stage")
		}
		return nil
	}
	if propertyID == 0xfd06c1af {
		// Scalar vector2 occupies 16 bytes; the final eight bytes are padding.
		if propertyType != prop.TypeVector2 || len(payload) != 16 {
			return errors.New("invalid DNA range")
		}
		for index := 0; index < 2; index++ {
			number := math.Float32frombits(binary.LittleEndian.Uint32(payload[index*4:]))
			if math.IsNaN(float64(number)) || math.IsInf(float64(number), 0) {
				return errors.New("nonfinite DNA range")
			}
		}
		return nil
	}
	if propertyType != prop.TypeFloat {
		return fmt.Errorf("dnaType[%#x]: %#x", propertyID, propertyType)
	}
	err := validateDirectorCompositionProperty(prop.Property{Type: propertyType, Items: [][]byte{payload}})
	if err != nil {
		return fmt.Errorf("dnaFloat: %w", err)
	}
	return nil
}

func (e *Store) DNARewardTuning(ctx context.Context) (tuning DNARewardTuning, resultErr error) {
	if e == nil || e.database == nil || ctx == nil {
		return DNARewardTuning{}, errors.New("DNA tuning store unavailable")
	}
	rows, err := e.database.QueryContext(ctx, `SELECT content_source_resource_id, property_id, property_type, encoded_item
		FROM dna_reward_property ORDER BY property_id`)
	if err != nil {
		return DNARewardTuning{}, fmt.Errorf("dnaQuery: %w", err)
	}
	defer closeContentRows(rows, &resultErr)
	tuning = DNARewardTuning{Properties: make([]DNARewardProperty, 0, 5)}
	for rows.Next() {
		var property DNARewardProperty
		var propertyType sql.NullInt64
		err = rows.Scan(&tuning.ContentSourceResourceID, &property.ID, &propertyType, &property.EncodedItem)
		if err != nil {
			return DNARewardTuning{}, fmt.Errorf("dnaScan: %w", err)
		}
		if propertyType.Valid {
			kind := uint16(propertyType.Int64)
			property.Type = &kind
			err = validateDNARewardProperty(property.ID, kind, property.EncodedItem)
			if err != nil {
				return DNARewardTuning{}, fmt.Errorf("dnaStored: %w", err)
			}
			switch property.ID {
			case 0xfd06c1af:
				factorRange := [2]float32{math.Float32frombits(binary.LittleEndian.Uint32(property.EncodedItem)), math.Float32frombits(binary.LittleEndian.Uint32(property.EncodedItem[4:]))}
				tuning.AmountFactorRange = &factorRange
			case 0x7c5e2bbd:
				stage := binary.BigEndian.Uint32(property.EncodedItem)
				tuning.MinimumStage = &stage
			default:
				number := math.Float32frombits(binary.BigEndian.Uint32(property.EncodedItem))
				switch property.ID {
				case 0x95189906:
					tuning.Chance = &number
				case 0x29259e01:
					tuning.AmountScalar = &number
				case 0x21903fa4:
					tuning.FirstGrowthBase = &number
				}
			}
		}
		tuning.Properties = append(tuning.Properties, property)
	}
	err = rows.Err()
	if err != nil {
		return DNARewardTuning{}, fmt.Errorf("dnaReadRows: %w", err)
	}
	if len(tuning.Properties) != len(dnaRewardPropertyIDs) {
		return DNARewardTuning{}, fmt.Errorf("dnaPropertyCount: %d", len(tuning.Properties))
	}
	return tuning, nil
}
