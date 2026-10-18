package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Definition presence is independent of the noun's combatant-admission flag.
// Marker identity remains available even for non-director scenery.
type MarkerObjectDefinition struct {
	ID           int64
	MarkerSetID  int64
	ResourceID   *int64
	MarkerID     uint32
	Ordinal      int
	MarkerName   string
	NounName     string
	Interactable *InteractableDefinition
	Combatant    *CombatantDefinition
}

func (e *Store) MarkerObjectDefinitions(ctx context.Context) (definitions []MarkerObjectDefinition, resultErr error) {
	if e == nil || e.database == nil || ctx == nil {
		return nil, errors.New("marker object store or context unavailable")
	}
	rows, err := e.database.QueryContext(ctx, `SELECT marker.id, marker.level_marker_set_id,
		marker_set.content_source_resource_id, marker.marker_id, marker.ordinal,
		marker.marker_name, marker.noun_name, marker.interactable_definition, marker.combatant_definition
		FROM marker JOIN level_marker_set AS marker_set ON marker_set.id=marker.level_marker_set_id
		ORDER BY marker.level_marker_set_id, marker.ordinal`)
	if err != nil {
		return nil, fmt.Errorf("markerObjectQuery: %w", err)
	}
	defer closeContentRows(rows, &resultErr)
	for rows.Next() {
		var definition MarkerObjectDefinition
		var interactable, combatant sql.NullString
		err = rows.Scan(&definition.ID, &definition.MarkerSetID, &definition.ResourceID,
			&definition.MarkerID, &definition.Ordinal, &definition.MarkerName, &definition.NounName,
			&interactable, &combatant)
		if err != nil {
			return nil, fmt.Errorf("markerObjectScan: %w", err)
		}
		if interactable.Valid {
			err = json.Unmarshal([]byte(interactable.String), &definition.Interactable)
			if err != nil {
				return nil, fmt.Errorf("markerInteractableDecode: %w", err)
			}
		}
		if combatant.Valid {
			err = json.Unmarshal([]byte(combatant.String), &definition.Combatant)
			if err != nil {
				return nil, fmt.Errorf("markerCombatantDecode: %w", err)
			}
		}
		definitions = append(definitions, definition)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("markerObjectRows: %w", err)
	}
	return definitions, nil
}
