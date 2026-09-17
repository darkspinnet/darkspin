package sqlite

import (
	"context"
	"errors"
	"fmt"
)

// ArenaSpawnMarker retains one authored Arena staging or combat placement.
type ArenaSpawnMarker struct {
	LevelName       string
	NounName        string
	PositionX       float32
	PositionY       float32
	PositionZ       float32
	RotationDegrees float32
}

// ArenaSpawnMarkers returns every numbered team placement from shipped PVP levels.
func (s *Store) ArenaSpawnMarkers(ctx context.Context) ([]ArenaSpawnMarker, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("nil store")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT level.name, marker.noun_name,
		       marker.position_x, marker.position_y, marker.position_z, marker.rotation_z
		FROM marker
		JOIN level_marker_set ON level_marker_set.id=marker.level_marker_set_id
		JOIN level ON level.id=level_marker_set.level_id
		WHERE substr(level.name, -4) = '_PVP'
		  AND marker.noun_name IN (
		      'SpawnPoint_Team1.Noun', 'SpawnPoint_Team2.Noun',
		      'SpawnPoint_Team3.Noun', 'SpawnPoint_Team4.Noun')
		ORDER BY level.name COLLATE NOCASE, marker.noun_name COLLATE NOCASE, marker.ordinal`)
	if err != nil {
		return nil, fmt.Errorf("arenaSpawnQuery: %w", err)
	}
	defer rows.Close()
	markers := make([]ArenaSpawnMarker, 0, 96)
	for rows.Next() {
		var marker ArenaSpawnMarker
		err = rows.Scan(
			&marker.LevelName, &marker.NounName,
			&marker.PositionX, &marker.PositionY, &marker.PositionZ,
			&marker.RotationDegrees,
		)
		if err != nil {
			return nil, fmt.Errorf("arenaSpawnScan[%d]: %w", len(markers), err)
		}
		markers = append(markers, marker)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("arenaSpawnRows: %w", err)
	}
	return markers, nil
}
