package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// LevelMarkerDefinition retains every placement before director-role filtering.
type LevelMarkerDefinition struct {
	Ordinal                         int
	MarkerID                        uint32
	NounName                        string
	PositionX, PositionY, PositionZ float32
	RotationX, RotationY, RotationZ float32
	Scale                           float32
	IsVisible                       bool
	IsCollisionEnabled              bool
	TeleporterTriggerRadius         float32
	Teleporter                      *TeleporterDefinition
}

func (e *Store) loadMarkerDefinitions(ctx context.Context, director *LevelDirector) error {
	rows, err := e.database.QueryContext(ctx, `
		SELECT level_marker_set.ordinal, marker.ordinal, marker.marker_id, marker.noun_name,
		       marker.position_x, marker.position_y, marker.position_z,
		       marker.rotation_x, marker.rotation_y, marker.rotation_z, marker.scale, marker.is_visible, marker.is_collision_enabled,
		       marker.teleporter_trigger_radius, marker.teleporter_definition
		FROM marker
		JOIN level_marker_set ON level_marker_set.id=marker.level_marker_set_id
		WHERE level_marker_set.level_id=?
		ORDER BY level_marker_set.ordinal, marker.ordinal`, director.LevelID)
	if err != nil {
		return fmt.Errorf("markerDefinitionQuery: %w", err)
	}
	setIndexes := make(map[int]int, len(director.MarkerSets))
	for index, set := range director.MarkerSets {
		setIndexes[set.Ordinal] = index
	}
	for rows.Next() {
		var setOrdinal int
		var definition LevelMarkerDefinition
		var encoded sql.NullString
		var isCollisionEnabled int
		var isVisible int
		err = rows.Scan(&setOrdinal, &definition.Ordinal, &definition.MarkerID, &definition.NounName,
			&definition.PositionX, &definition.PositionY, &definition.PositionZ,
			&definition.RotationX, &definition.RotationY, &definition.RotationZ, &definition.Scale,
			&isVisible, &isCollisionEnabled, &definition.TeleporterTriggerRadius, &encoded)
		if err != nil {
			closeErr := rows.Close()
			return fmt.Errorf("markerDefinitionScan: %w", errors.Join(err, closeErr))
		}
		definition.IsCollisionEnabled = isCollisionEnabled != 0
		definition.IsVisible = isVisible != 0
		if encoded.Valid {
			err = json.Unmarshal([]byte(encoded.String), &definition.Teleporter)
			if err != nil {
				closeErr := rows.Close()
				return fmt.Errorf("markerTeleporterDecode: %w", errors.Join(err, closeErr))
			}
		}
		setIndex, isFound := setIndexes[setOrdinal]
		if !isFound {
			closeErr := rows.Close()
			return fmt.Errorf("markerDefinitionSet[%d]: %w", setOrdinal,
				errors.Join(errors.New("missing set"), closeErr))
		}
		director.MarkerSets[setIndex].Definitions = append(director.MarkerSets[setIndex].Definitions, definition)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return fmt.Errorf("markerDefinitionRows: %w", errors.Join(err, closeErr))
	}
	if closeErr != nil {
		return fmt.Errorf("markerDefinitionClose: %w", closeErr)
	}
	return nil
}
