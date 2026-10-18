package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/darkspinnet/darkspin/content/render/prop"
)

const equipmentGatePackage = "Config_Ship.package"
const equipmentGateInstance = uint32(0x08ea246b)
const equipmentGateProperty = uint32(0x33184138)

// EquipmentDropSetting preserves a missing resource separately from a missing
// property and from an authored false. Native lookup defaults to false; this
// projection does not store that fallback as if the package authored it.
type EquipmentDropSetting struct {
	SourceResourceID       *int64
	PropertyID             uint32
	IsEquipmentDropEnabled *bool
}

func writeEquipmentDropSetting(ctx context.Context, transaction *sql.Tx, installPath string) (resultErr error) {
	r, err := os.Open(filepath.Join(installPath, "Data", equipmentGatePackage))
	if err != nil {
		return fmt.Errorf("equipmentGateOpen: %w", err)
	}
	defer closeSectionReader(r, &resultErr)
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("equipmentGateStat: %w", err)
	}
	pkg, err := importPackageReader(ctx, r, fi.Size())
	if err != nil {
		return fmt.Errorf("equipmentGatePackage: %w", err)
	}
	setting := EquipmentDropSetting{PropertyID: equipmentGateProperty}
	for ordinal, entry := range pkg.Entries {
		if entry.Type != prop.ResourceType || entry.Group != 0 || entry.Instance != uint64(equipmentGateInstance) {
			continue
		}
		if setting.SourceResourceID != nil {
			return errors.New("duplicate equipment gate resource")
		}
		var resourceID int64
		err = transaction.QueryRowContext(ctx, `SELECT resource.id FROM content_source_resource AS resource
			JOIN content_source_package AS package ON package.id=resource.content_source_package_id
			WHERE package.package_name=? AND resource.ordinal=?`, equipmentGatePackage, ordinal).Scan(&resourceID)
		if err != nil {
			return fmt.Errorf("equipmentGateSource: %w", err)
		}
		setting.SourceResourceID = &resourceID
		payload, readErr := readDecodedResource(ctx, pkg, entry)
		if readErr != nil {
			return fmt.Errorf("equipmentGateRead: %w", readErr)
		}
		document, decodeErr := prop.Decode(payload)
		if decodeErr != nil {
			return fmt.Errorf("equipmentGateDecode: %w", decodeErr)
		}
		for _, property := range document.Properties {
			if property.ID != equipmentGateProperty {
				continue
			}
			if setting.IsEquipmentDropEnabled != nil {
				return errors.New("duplicate equipment gate property")
			}
			if property.Type != prop.TypeBool || property.IsArray || len(property.Items) != 1 ||
				len(property.Items[0]) != 1 || property.Items[0][0] > 1 {
				return errors.New("equipment gate property is not a scalar boolean")
			}
			isEnabled := property.Items[0][0] == 1
			setting.IsEquipmentDropEnabled = &isEnabled
		}
	}
	result, err := transaction.ExecContext(ctx, `INSERT INTO equipment_drop_setting
		(id, content_source_resource_id, property_id, is_equipment_drop_enabled) VALUES (1, ?, ?, ?)`,
		setting.SourceResourceID, int64(setting.PropertyID), setting.IsEquipmentDropEnabled)
	if err != nil {
		return fmt.Errorf("equipmentGateInsert: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("equipmentGateCount: %w", err)
	}
	if count != 1 {
		return fmt.Errorf("equipmentGateCount: got %d", count)
	}
	return nil
}

func verifyEquipmentDropSetting(ctx context.Context, database *sql.DB) error {
	var count int
	err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM equipment_drop_setting AS setting
		LEFT JOIN content_source_resource AS resource ON resource.id=setting.content_source_resource_id
		LEFT JOIN content_source_package AS package ON package.id=resource.content_source_package_id
		WHERE setting.id=1 AND setting.property_id=? AND
		 ((setting.content_source_resource_id IS NULL AND setting.is_equipment_drop_enabled IS NULL)
		  OR (package.package_name=? AND resource.type_id=? AND resource.group_id=0 AND resource.instance_id=?))`,
		int64(equipmentGateProperty), equipmentGatePackage, int64(prop.ResourceType), int64(equipmentGateInstance)).Scan(&count)
	if err != nil {
		return fmt.Errorf("equipmentGateVerifyQuery: %w", err)
	}
	if count != 1 {
		return fmt.Errorf("equipmentGateVerifyCount: got %d", count)
	}
	return nil
}

func (e *Store) EquipmentDropSetting(ctx context.Context) (EquipmentDropSetting, error) {
	if e == nil || e.database == nil || ctx == nil {
		return EquipmentDropSetting{}, errors.New("equipment gate store or context unavailable")
	}
	var setting EquipmentDropSetting
	var resourceID sql.NullInt64
	var isEnabled sql.NullBool
	err := e.database.QueryRowContext(ctx, `SELECT content_source_resource_id, property_id, is_equipment_drop_enabled
		FROM equipment_drop_setting WHERE id=1`).Scan(&resourceID, &setting.PropertyID, &isEnabled)
	if err != nil {
		return EquipmentDropSetting{}, fmt.Errorf("equipmentGateQuery: %w", err)
	}
	if resourceID.Valid {
		setting.SourceResourceID = &resourceID.Int64
	}
	if isEnabled.Valid {
		setting.IsEquipmentDropEnabled = &isEnabled.Bool
	}
	return setting, nil
}
