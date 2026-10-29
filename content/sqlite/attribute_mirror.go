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

const mirrorMagicType = uint32(0x36201b51)
const mirrorMagicInstance = uint32(0x966534e4)
const mirrorAttributeCount = 115

// AttributeMirrorTuning retains the installed build-103 derivation inputs.
type AttributeMirrorTuning struct {
	GlobalBaseMaximums          [mirrorAttributeCount]float32
	GlobalMaximums              [mirrorAttributeCount]float32
	HealthPerStrength           float32
	ManaPerMind                 float32
	PhysicalDefensePerDexterity float32
	EnergyDefensePerMind        float32
	CriticalPerDexterity        float32
}

func writeAttributeMirrorTuning(ctx context.Context, transaction *sql.Tx, installPath string) error {
	magic, magicResourceID, err := readMirrorTuningResource(ctx, transaction, installPath,
		"AssetData_Binary.package", mirrorMagicType, 0, mirrorMagicInstance)
	if err != nil {
		return fmt.Errorf("mirrorMagic: %w", err)
	}
	if len(magic) != 76 {
		return fmt.Errorf("mirrorMagicSize: got %d", len(magic))
	}
	labs, labsResourceID, err := readMirrorTuningResource(ctx, transaction, installPath,
		"ServerData.package", prop.ResourceType, directorCompositionIdentity, directorCompositionIdentity)
	if err != nil {
		return fmt.Errorf("mirrorLabs: %w", err)
	}
	document, err := prop.Decode(labs)
	if err != nil {
		return fmt.Errorf("mirrorProperties: %w", err)
	}
	baseMaximums, maximums := make([]byte, mirrorAttributeCount*4), make([]byte, mirrorAttributeCount*4)
	for _, property := range document.Properties {
		var payload []byte
		switch property.ID {
		case 0x93b7e2b5:
			payload = baseMaximums
		case 0xd5ef1d1f:
			payload = maximums
		default:
			continue
		}
		if property.Type != prop.TypeFloat || !property.IsArray {
			return fmt.Errorf("mirrorCapShape[%#x]: expected float array", property.ID)
		}
		// Native 9CF190 initializes 115 zero slots and copies at most 115.
		for index, item := range property.Items[:min(len(property.Items), mirrorAttributeCount)] {
			if len(item) != 4 {
				return fmt.Errorf("mirrorCapSize[%#x:%d]: got %d", property.ID, index, len(item))
			}
			binary.LittleEndian.PutUint32(payload[index*4:], binary.BigEndian.Uint32(item))
		}
	}
	_, err = transaction.ExecContext(ctx, `CREATE TABLE attribute_mirror_tuning (
		id INTEGER PRIMARY KEY CHECK (id=1),
		magic_source_resource_id INTEGER NOT NULL REFERENCES content_source_resource(id),
		cap_source_resource_id INTEGER NOT NULL REFERENCES content_source_resource(id),
		magic_number BLOB NOT NULL,
		global_base_maximum BLOB NOT NULL,
		global_maximum BLOB NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("mirrorSchema: %w", err)
	}
	_, err = transaction.ExecContext(ctx, `INSERT INTO attribute_mirror_tuning
		(id, magic_source_resource_id, cap_source_resource_id, magic_number, global_base_maximum, global_maximum)
		VALUES (1, ?, ?, ?, ?, ?)`, magicResourceID, labsResourceID, magic, baseMaximums, maximums)
	if err != nil {
		return fmt.Errorf("mirrorInsert: %w", err)
	}
	return nil
}

func readMirrorTuningResource(ctx context.Context, transaction *sql.Tx, installPath string,
	packageName string, typeID uint32, groupID uint32, instanceID uint32,
) (payload []byte, resourceID int64, resultErr error) {
	r, err := os.Open(filepath.Join(installPath, "Data", packageName))
	if err != nil {
		return nil, 0, fmt.Errorf("mirrorOpen: %w", err)
	}
	defer closeSectionReader(r, &resultErr)
	fi, err := r.Stat()
	if err != nil {
		return nil, 0, fmt.Errorf("mirrorStat: %w", err)
	}
	pkg, err := importPackageReader(ctx, r, fi.Size())
	if err != nil {
		return nil, 0, fmt.Errorf("mirrorPackage: %w", err)
	}
	for ordinal, entry := range pkg.Entries {
		if entry.Type != typeID || entry.Group != groupID || entry.Instance != uint64(instanceID) {
			continue
		}
		payload, err = readDecodedResource(ctx, pkg, entry)
		if err != nil {
			return nil, 0, fmt.Errorf("mirrorRead: %w", err)
		}
		err = transaction.QueryRowContext(ctx, `SELECT resource.id FROM content_source_resource AS resource
			JOIN content_source_package AS package ON package.id=resource.content_source_package_id
			WHERE package.package_name=? AND resource.ordinal=?
			AND resource.type_id=? AND resource.group_id=? AND resource.instance_id=?`,
			packageName, ordinal, int64(typeID), int64(groupID), int64(instanceID)).Scan(&resourceID)
		if err != nil {
			return nil, 0, fmt.Errorf("mirrorSource: %w", err)
		}
		return payload, resourceID, nil
	}
	return nil, 0, fmt.Errorf("mirrorMissing: %s/%#x/%#x", packageName, typeID, instanceID)
}

func (e *Store) AttributeMirrorTuning(ctx context.Context) (AttributeMirrorTuning, error) {
	if e == nil || e.database == nil || ctx == nil {
		return AttributeMirrorTuning{}, errors.New("attribute mirror tuning unavailable")
	}
	var magic, baseMaximums, maximums []byte
	err := e.database.QueryRowContext(ctx, `SELECT magic_number, global_base_maximum, global_maximum
		FROM attribute_mirror_tuning WHERE id=1`).Scan(&magic, &baseMaximums, &maximums)
	if err != nil {
		return AttributeMirrorTuning{}, fmt.Errorf("mirrorQuery: %w", err)
	}
	if len(magic) != 76 || len(baseMaximums) != mirrorAttributeCount*4 || len(maximums) != mirrorAttributeCount*4 {
		return AttributeMirrorTuning{}, errors.New("attribute mirror tuning shape invalid")
	}
	tuning := AttributeMirrorTuning{
		HealthPerStrength:           math.Float32frombits(binary.LittleEndian.Uint32(magic[3*4:])),
		ManaPerMind:                 math.Float32frombits(binary.LittleEndian.Uint32(magic[7*4:])),
		PhysicalDefensePerDexterity: math.Float32frombits(binary.LittleEndian.Uint32(magic[4*4:])),
		EnergyDefensePerMind:        math.Float32frombits(binary.LittleEndian.Uint32(magic[6*4:])),
		CriticalPerDexterity:        math.Float32frombits(binary.LittleEndian.Uint32(magic[5*4:])),
	}
	for index := range tuning.GlobalBaseMaximums {
		tuning.GlobalBaseMaximums[index] = math.Float32frombits(binary.LittleEndian.Uint32(baseMaximums[index*4:]))
		tuning.GlobalMaximums[index] = math.Float32frombits(binary.LittleEndian.Uint32(maximums[index*4:]))
	}
	return tuning, nil
}
