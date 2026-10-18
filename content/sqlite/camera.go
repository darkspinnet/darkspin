package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
)

type LevelCameraSettings struct {
	Pitch    float32
	Yaw      float32
	Distance float32
}

// levelConfigurationCursor follows reflected fields through the references
// preceding levelConfig (+88) and firstTimeConfig (+92). Their nullable words
// determine which configuration bodies follow; scanning for headers can also
// match the second configuration and lose its first-time identity.
func levelConfigurationCursor(payload []byte) (assetCursor, error) {
	cursor := assetCursor{payload: payload}
	base, err := cursor.reserve(1, 152)
	if err != nil {
		return assetCursor{}, fmt.Errorf("levelHeader: %w", err)
	}
	markerCount := binary.LittleEndian.Uint32(payload[base+4:])
	markersOffset, err := cursor.reserve(markerCount, 4)
	if err != nil {
		return assetCursor{}, fmt.Errorf("levelMarkers: %w", err)
	}
	for index := range int(markerCount) {
		markerName, referenceErr := cursor.reference(markersOffset + index*4)
		if referenceErr != nil {
			return assetCursor{}, fmt.Errorf("markerReference: %w", referenceErr)
		}
		_ = markerName // Only advance the authored reference here.
	}
	for _, offset := range []int{20, 36, 52, 68, 84} {
		reference, referenceErr := cursor.reference(offset)
		if referenceErr != nil {
			return assetCursor{}, fmt.Errorf("levelReference[%d]: %w", offset, referenceErr)
		}
		_ = reference
	}
	return cursor, nil
}

func decodeLevelCamera(payload []byte) (*LevelCameraSettings, error) {
	cursor, err := levelConfigurationCursor(payload)
	if err != nil {
		return nil, fmt.Errorf("cameraCursor: %w", err)
	}
	for _, offset := range []int{88, 92} {
		if binary.LittleEndian.Uint32(payload[offset:]) == 0 {
			continue
		}
		err = walkLevelConfiguration(&cursor)
		if err != nil {
			return nil, fmt.Errorf("levelConfiguration[%d]: %w", offset, err)
		}
	}
	for _, offset := range []int{96, 100, 144} {
		reference, referenceErr := cursor.reference(offset)
		if referenceErr != nil {
			return nil, fmt.Errorf("levelTailReference[%d]: %w", offset, referenceErr)
		}
		_ = reference
	}
	var camera *LevelCameraSettings
	if binary.LittleEndian.Uint32(payload[148:]) != 0 {
		offset, cameraErr := cursor.reserve(1, 12)
		if cameraErr != nil {
			return nil, fmt.Errorf("cameraPayload: %w", cameraErr)
		}
		camera = &LevelCameraSettings{Pitch: readFloat32(payload, offset),
			Yaw: readFloat32(payload, offset+4), Distance: readFloat32(payload, offset+8)}
	}
	err = cursor.finish()
	if err != nil {
		return nil, fmt.Errorf("cameraTail: %w", err)
	}
	return camera, nil
}

func walkLevelConfiguration(cursor *assetCursor) error {
	base, err := cursor.reserve(1, 40)
	if err != nil {
		return fmt.Errorf("configHeader: %w", err)
	}
	for roleIndex := range 5 {
		count := binary.LittleEndian.Uint32(cursor.payload[base+20+roleIndex*4:])
		start, arrayErr := cursor.reserve(count, 16)
		if arrayErr != nil {
			return fmt.Errorf("configArray[%d]: %w", roleIndex, arrayErr)
		}
		for index := range int(count) {
			nounName, referenceErr := cursor.reference(start + index*16)
			if referenceErr != nil {
				return fmt.Errorf("configNoun[%d]: %w", index, referenceErr)
			}
			_ = nounName
		}
	}
	return nil
}

// LevelCamera preserves absence. Consumers choose their own fallback (the
// native pet perception query uses 315 degrees for absent or zero yaw).
func (e *Store) LevelCamera(ctx context.Context, levelID int64) (*LevelCameraSettings, error) {
	if e == nil || e.database == nil || ctx == nil {
		return nil, errors.New("camera store or context unavailable")
	}
	var pitch, yaw, distance sql.NullFloat64
	err := e.database.QueryRowContext(ctx, `SELECT camera_pitch, camera_yaw, camera_distance
		FROM level WHERE id=?`, levelID).Scan(&pitch, &yaw, &distance)
	if err != nil {
		return nil, fmt.Errorf("cameraQuery: %w", err)
	}
	if !pitch.Valid && !yaw.Valid && !distance.Valid {
		return nil, nil
	}
	if !pitch.Valid || !yaw.Valid || !distance.Valid {
		return nil, errors.New("partial level camera settings")
	}
	return &LevelCameraSettings{Pitch: float32(pitch.Float64), Yaw: float32(yaw.Float64),
		Distance: float32(distance.Float64)}, nil
}
