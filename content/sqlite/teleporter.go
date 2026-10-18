package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

type TeleporterDefinition struct {
	DestinationMarkerID       uint32
	TriggerVolume             *TriggerVolumeDefinition
	IsTriggerCreationDeferred bool
}

type MarkerTeleporter struct {
	ID         int64
	MarkerID   uint32
	Definition *TeleporterDefinition
}

// Decode the reflected marker components using one bounded tail cursor.
func decodeMarkerTeleporter(payload []byte, base, tailStart, tailEnd int) (*TeleporterDefinition, error) {
	definition, err := decodeMarkerComponents(payload, base, tailStart, tailEnd)
	if err != nil {
		return nil, fmt.Errorf("teleporterComponents: %w", err)
	}
	return definition.teleporter, nil
}
func encodeTeleporterDefinition(definition *TeleporterDefinition) (*string, error) {
	if definition == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(definition)
	if err != nil {
		return nil, fmt.Errorf("teleporterMarshal: %w", err)
	}
	text := string(encoded)
	return &text, nil
}

// MarkerTeleporters preserves absence for every imported marker, independent
// of its noun name or its admission to a live gameplay object roster.
func (e *Store) MarkerTeleporters(ctx context.Context) ([]MarkerTeleporter, error) {
	if e == nil || e.database == nil || ctx == nil {
		return nil, errors.New("teleporter store or context unavailable")
	}
	rows, err := e.database.QueryContext(ctx, `SELECT id, marker_id, teleporter_definition FROM marker ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("teleporterQuery: %w", err)
	}
	teleporters := make([]MarkerTeleporter, 0)
	for rows.Next() {
		var teleporter MarkerTeleporter
		var encoded sql.NullString
		err = rows.Scan(&teleporter.ID, &teleporter.MarkerID, &encoded)
		if err != nil {
			closeErr := rows.Close()
			return nil, fmt.Errorf("teleporterScan: %w", errors.Join(err, closeErr))
		}
		if encoded.Valid {
			err = json.Unmarshal([]byte(encoded.String), &teleporter.Definition)
			if err != nil {
				closeErr := rows.Close()
				return nil, fmt.Errorf("teleporterDecode: %w", errors.Join(err, closeErr))
			}
		}
		teleporters = append(teleporters, teleporter)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return nil, fmt.Errorf("teleporterRows: %w", errors.Join(err, closeErr))
	}
	if closeErr != nil {
		return nil, fmt.Errorf("teleporterClose: %w", closeErr)
	}
	return teleporters, nil
}
