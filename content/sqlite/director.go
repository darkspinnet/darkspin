package sqlite

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
)

// LevelDirectorEntry is one difficulty-gated noun candidate in an authored
// level-director configuration.
type LevelDirectorEntry struct {
	Ordinal                   int
	ConfigurationEntryOrdinal int
	ConfigKind                string
	SpawnKind                 string
	NounName                  string
	MinimumDifficulty         uint32
	MaximumDifficulty         uint32
	IsHordeLegal              bool
}

// LevelDirectorPool preserves one structural configuration boundary from the
// level asset. Recovered config kinds are retained without deriving an
// entry-level spawn kind.
type LevelDirectorPool struct {
	ConfigurationOrdinal int
	ConfigKind           string
	SpawnKind            string
	Entries              []LevelDirectorEntry
}

// LevelDirectorEvent is one authored event binding on a director placement.
// It records content metadata without deciding when or how to spawn anything.
type LevelDirectorEvent struct {
	Ordinal           int
	ComponentName     string
	EventKind         string
	EventSlot         string
	EventName         string
	CallbackName      string
	TriggerRadius     float32
	IsTriggerOnceOnly bool
	IsServerOnly      bool
}

// LevelDirectorMarker is one authored director placement marker.
type LevelDirectorMarker struct {
	Ordinal                 int
	MarkerID                uint32
	Name                    string
	NounName                string
	SpawnKind               uint32
	PoolKind                string
	IsSpawnKindKnown        bool
	PositionX               float32
	PositionY               float32
	PositionZ               float32
	RotationX               float32
	RotationY               float32
	RotationZ               float32
	Scale                   float32
	IsVisible               bool
	IsCollisionEnabled      bool
	TargetMarkerID          uint32
	TeleporterTriggerRadius float32
	Events                  []LevelDirectorEvent
}

// LevelDirectorTrigger is one authored player-entry trigger associated with a
// director marker set. Its callback remains metadata until server policy owns
// the corresponding event publication.
type LevelDirectorTrigger struct {
	Ordinal   int
	MarkerID  uint32
	Name      string
	NounName  string
	PositionX float32
	PositionY float32
	PositionZ float32
	Events    []LevelDirectorEvent
}

// LevelDirectorMarkerSet preserves one authored placement-set boundary.
type LevelDirectorMarkerSet struct {
	Ordinal  int
	Name     string
	Weight   uint32
	Markers  []LevelDirectorMarker
	Triggers []LevelDirectorTrigger
}

// LevelScriptBinding links one authored level event to its imported Lua chunk.
// It is immutable content metadata and does not grant the script authority to
// mutate gameplay state.
type LevelScriptBinding struct {
	MarkerSetOrdinal      int
	MarkerSetName         string
	MarkerSetWeight       uint32
	MarkerOrdinal         int
	MarkerID              uint32
	MarkerName            string
	NounName              string
	PositionX             float32
	PositionY             float32
	PositionZ             float32
	RotationX             float32
	RotationY             float32
	RotationZ             float32
	Scale                 float32
	IsVisible             bool
	IsCollisionEnabled    bool
	InteractableAbility   string
	InteractableUseLimit  int32
	InteractableChallenge int32
	EventOrdinal          int
	EventName             string
	CallbackName          string
	LuaChunkID            int64
	LuaSourceName         string
	LuaSHA256             string
}

// LevelDirector is the immutable pool and placement projection for one level.
// It deliberately contains no selection, budget, encounter, or AI policy.
type LevelDirector struct {
	LevelID        int64
	Name           string
	EntryPositions [][3]float32
	Pools          []LevelDirectorPool
	MarkerSets     []LevelDirectorMarkerSet
	Scripts        []LevelScriptBinding
}

// LevelDirector reads the authored director candidates and placement markers
// for one level alias.
func (s *Store) LevelDirector(ctx context.Context, levelName string) (LevelDirector, error) {
	if s == nil || s.database == nil {
		return LevelDirector{}, errors.New("nil store")
	}
	if ctx == nil {
		return LevelDirector{}, errors.New("nil context")
	}
	if levelName == "" {
		return LevelDirector{}, errors.New("empty level name")
	}

	director := LevelDirector{Name: levelName}
	err := s.database.QueryRowContext(ctx, `
		SELECT level.id, level.name
		FROM level
		JOIN level_alias ON level_alias.level_id=level.id
		WHERE level_alias.alias=? COLLATE NOCASE
		LIMIT 1`, levelName).Scan(&director.LevelID, &director.Name)
	if err != nil {
		return LevelDirector{}, fmt.Errorf("directorLevel[%s]: %w", levelName, err)
	}

	rows, err := s.database.QueryContext(ctx, `
		SELECT marker.position_x, marker.position_y, marker.position_z
		FROM level_marker_set
		JOIN marker ON marker.level_marker_set_id=level_marker_set.id
		WHERE level_marker_set.level_id=?
		  AND marker.noun_name='CameraSpawnPoint.Noun' COLLATE NOCASE
		ORDER BY level_marker_set.ordinal, marker.ordinal`, director.LevelID)
	if err != nil {
		return LevelDirector{}, fmt.Errorf("directorEntryPositionQuery: %w", err)
	}
	for rows.Next() {
		var position [3]float32
		err = rows.Scan(&position[0], &position[1], &position[2])
		if err != nil {
			_ = rows.Close()
			return LevelDirector{}, fmt.Errorf("directorEntryPositionScan: %w", err)
		}
		director.EntryPositions = append(director.EntryPositions, position)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return LevelDirector{}, fmt.Errorf("directorEntryPositionRows: %w", err)
	}
	if closeErr != nil {
		return LevelDirector{}, fmt.Errorf("directorEntryPositionClose: %w", closeErr)
	}

	rows, err = s.database.QueryContext(ctx, `
		SELECT configuration_ordinal, configuration_entry_ordinal, ordinal,
		       config_kind, spawn_kind, noun_name, minimum_difficulty,
		       maximum_difficulty, is_horde_legal
		FROM level_director_entry
		WHERE level_id=?
		ORDER BY configuration_ordinal, configuration_entry_ordinal`, director.LevelID)
	if err != nil {
		return LevelDirector{}, fmt.Errorf("directorEntryQuery: %w", err)
	}
	for rows.Next() {
		var configurationOrdinal int
		var entry LevelDirectorEntry
		var minimumDifficulty int64
		var maximumDifficulty int64
		var isHordeLegal int
		err = rows.Scan(&configurationOrdinal, &entry.ConfigurationEntryOrdinal, &entry.Ordinal,
			&entry.ConfigKind, &entry.SpawnKind, &entry.NounName, &minimumDifficulty,
			&maximumDifficulty, &isHordeLegal)
		if err != nil {
			_ = rows.Close()
			return LevelDirector{}, fmt.Errorf("directorEntryScan: %w", err)
		}
		if minimumDifficulty < 0 || minimumDifficulty > math.MaxUint32 ||
			maximumDifficulty < 0 || maximumDifficulty > math.MaxUint32 {
			_ = rows.Close()
			return LevelDirector{}, fmt.Errorf("directorDifficulty[%d]: %d/%d", entry.Ordinal,
				minimumDifficulty, maximumDifficulty)
		}
		entry.MinimumDifficulty = uint32(minimumDifficulty)
		entry.MaximumDifficulty = uint32(maximumDifficulty)
		entry.IsHordeLegal = isHordeLegal != 0
		poolIndex := len(director.Pools) - 1
		if poolIndex < 0 || director.Pools[poolIndex].ConfigurationOrdinal != configurationOrdinal {
			director.Pools = append(director.Pools, LevelDirectorPool{
				ConfigurationOrdinal: configurationOrdinal,
				ConfigKind:           entry.ConfigKind,
				SpawnKind:            entry.SpawnKind,
			})
			poolIndex++
		}
		director.Pools[poolIndex].Entries = append(director.Pools[poolIndex].Entries, entry)
	}
	err = rows.Err()
	closeErr = rows.Close()
	if err != nil {
		return LevelDirector{}, fmt.Errorf("directorEntryRows: %w", err)
	}
	if closeErr != nil {
		return LevelDirector{}, fmt.Errorf("directorEntryClose: %w", closeErr)
	}

	rows, err = s.database.QueryContext(ctx, `
		SELECT level_marker_set.ordinal, marker.id, marker.ordinal, level_marker_set.asset_name,
		       level_marker_set.weight, marker.marker_id, marker.marker_name,
		       marker.noun_name, marker.position_x, marker.position_y, marker.position_z,
		       marker.rotation_x, marker.rotation_y, marker.rotation_z, marker.scale,
		       marker.is_visible, marker.is_collision_enabled, marker.target_marker_id,
		       marker.teleporter_trigger_radius,
		       CASE WHEN marker.noun_name<>'TunnelTeleporter.Noun' COLLATE NOCASE
		             AND marker.noun_name<>'Teleporter.Noun' COLLATE NOCASE
		             AND marker.noun_name<>'SecurityTeleporter.Noun' COLLATE NOCASE
		             AND marker.noun_name<>'BossSecurityTeleporter.Noun' COLLATE NOCASE
		             AND EXISTS (
		           SELECT 1 FROM level_event AS trigger_event
		           WHERE trigger_event.marker_id=marker.id
		             AND trigger_event.trigger_radius>0
		             AND (trigger_event.event_name<>'' OR trigger_event.callback_name<>''))
		            THEN 1 ELSE 0 END
		FROM level_marker_set
		JOIN marker ON marker.level_marker_set_id=level_marker_set.id
		WHERE level_marker_set.level_id=?
		  AND (marker.noun_name LIKE 'SpawnPoint_Director%.Noun' COLLATE NOCASE
		       OR marker.noun_name LIKE 'Tutorial%.Noun' COLLATE NOCASE
		       OR marker.noun_name='DEST_prefab_islands_instrument_scitech_11.Noun' COLLATE NOCASE
		       OR marker.noun_name='DEST_nocturna_herotree_yellow_1.Noun' COLLATE NOCASE
		       OR marker.noun_name='HordeGateTeleporter.Noun' COLLATE NOCASE
		       OR marker.noun_name='TestDoor_design_blockin_horde_open.Noun' COLLATE NOCASE
		       OR marker.noun_name='Teleporter.Noun' COLLATE NOCASE
		       OR marker.noun_name='TunnelTeleporter.Noun' COLLATE NOCASE
		       OR marker.noun_name='SecurityTeleporter.Noun' COLLATE NOCASE
		       OR marker.noun_name='BossSecurityTeleporter.Noun' COLLATE NOCASE
		       OR marker.noun_name='TeleporterSpawnPoint.Noun' COLLATE NOCASE
		       OR EXISTS (
		           SELECT 1 FROM level_event AS trigger_event
		           WHERE trigger_event.marker_id=marker.id
		             AND trigger_event.trigger_radius>0
		             AND (trigger_event.event_name<>'' OR trigger_event.callback_name<>'')))
		ORDER BY level_marker_set.ordinal, marker.ordinal`, director.LevelID)
	if err != nil {
		return LevelDirector{}, fmt.Errorf("directorMarkerQuery: %w", err)
	}
	type markerLocationIndex struct {
		markerSetIndex int
		placementIndex int
		isTrigger      bool
	}
	markerLocation := make(map[int64]markerLocationIndex)
	for rows.Next() {
		var markerSetOrdinal int
		var markerDatabaseID int64
		var markerSetName string
		var marker LevelDirectorMarker
		var markerID int64
		var targetMarkerID int64
		var markerSetWeight int64
		var isTrigger int
		var isVisible int
		var isCollisionEnabled int
		err = rows.Scan(&markerSetOrdinal, &markerDatabaseID, &marker.Ordinal, &markerSetName,
			&markerSetWeight, &markerID, &marker.Name, &marker.NounName,
			&marker.PositionX, &marker.PositionY, &marker.PositionZ,
			&marker.RotationX, &marker.RotationY, &marker.RotationZ, &marker.Scale,
			&isVisible, &isCollisionEnabled, &targetMarkerID,
			&marker.TeleporterTriggerRadius, &isTrigger)
		if err != nil {
			_ = rows.Close()
			return LevelDirector{}, fmt.Errorf("directorMarkerScan: %w", err)
		}
		if markerID < 0 || markerID > math.MaxUint32 || targetMarkerID < 0 ||
			targetMarkerID > math.MaxUint32 || markerSetWeight < 0 || markerSetWeight > math.MaxUint32 {
			_ = rows.Close()
			return LevelDirector{}, fmt.Errorf("directorMarkerID[%d]: %d/%d", marker.Ordinal,
				markerID, markerSetWeight)
		}
		marker.MarkerID = uint32(markerID)
		marker.TargetMarkerID = uint32(targetMarkerID)
		marker.IsVisible = isVisible != 0
		marker.IsCollisionEnabled = isCollisionEnabled != 0
		marker.SpawnKind, marker.PoolKind, marker.IsSpawnKindKnown = classifyDirectorMarker(marker.NounName)
		markerSetIndex := len(director.MarkerSets) - 1
		if markerSetIndex < 0 || director.MarkerSets[markerSetIndex].Ordinal != markerSetOrdinal {
			director.MarkerSets = append(director.MarkerSets, LevelDirectorMarkerSet{
				Ordinal: markerSetOrdinal, Name: markerSetName, Weight: uint32(markerSetWeight),
			})
			markerSetIndex++
		}
		if isTrigger == 0 {
			director.MarkerSets[markerSetIndex].Markers = append(
				director.MarkerSets[markerSetIndex].Markers, marker,
			)
			markerLocation[markerDatabaseID] = markerLocationIndex{
				markerSetIndex: markerSetIndex,
				placementIndex: len(director.MarkerSets[markerSetIndex].Markers) - 1,
			}
			continue
		}
		trigger := LevelDirectorTrigger{
			Ordinal: marker.Ordinal, MarkerID: marker.MarkerID, Name: marker.Name,
			NounName: marker.NounName, PositionX: marker.PositionX,
			PositionY: marker.PositionY, PositionZ: marker.PositionZ,
		}
		director.MarkerSets[markerSetIndex].Triggers = append(
			director.MarkerSets[markerSetIndex].Triggers, trigger,
		)
		markerLocation[markerDatabaseID] = markerLocationIndex{
			markerSetIndex: markerSetIndex,
			placementIndex: len(director.MarkerSets[markerSetIndex].Triggers) - 1,
			isTrigger:      true,
		}
	}
	err = rows.Err()
	closeErr = rows.Close()
	if err != nil {
		return LevelDirector{}, fmt.Errorf("directorMarkerRows: %w", err)
	}
	if closeErr != nil {
		return LevelDirector{}, fmt.Errorf("directorMarkerClose: %w", closeErr)
	}

	rows, err = s.database.QueryContext(ctx, `
		SELECT level_event.marker_id, level_event.ordinal, level_event.component_name,
		       level_event.event_kind, level_event.event_slot, level_event.event_name,
		       level_event.callback_name, level_event.trigger_radius,
		       level_event.is_trigger_once_only, level_event.is_server_only
		FROM level_event
		JOIN marker ON marker.id=level_event.marker_id
		JOIN level_marker_set ON level_marker_set.id=marker.level_marker_set_id
		WHERE level_marker_set.level_id=?
		  AND (marker.noun_name LIKE 'SpawnPoint_Director%.Noun' COLLATE NOCASE
		       OR EXISTS (
		           SELECT 1 FROM level_event AS trigger_event
		           WHERE trigger_event.marker_id=marker.id
		             AND trigger_event.trigger_radius>0
		             AND (trigger_event.event_name<>'' OR trigger_event.callback_name<>'')))
		ORDER BY level_marker_set.ordinal, marker.ordinal, level_event.ordinal`, director.LevelID)
	if err != nil {
		return LevelDirector{}, fmt.Errorf("directorEventQuery: %w", err)
	}
	for rows.Next() {
		var markerDatabaseID int64
		var event LevelDirectorEvent
		var isTriggerOnceOnly int
		var isServerOnly int
		err = rows.Scan(&markerDatabaseID, &event.Ordinal, &event.ComponentName,
			&event.EventKind, &event.EventSlot, &event.EventName, &event.CallbackName,
			&event.TriggerRadius, &isTriggerOnceOnly, &isServerOnly)
		if err != nil {
			_ = rows.Close()
			return LevelDirector{}, fmt.Errorf("directorEventScan: %w", err)
		}
		location, isFound := markerLocation[markerDatabaseID]
		if !isFound {
			_ = rows.Close()
			return LevelDirector{}, fmt.Errorf("directorEventMarker[%d]: missing", markerDatabaseID)
		}
		event.IsTriggerOnceOnly = isTriggerOnceOnly != 0
		event.IsServerOnly = isServerOnly != 0
		if location.isTrigger {
			trigger := &director.MarkerSets[location.markerSetIndex].Triggers[location.placementIndex]
			trigger.Events = append(trigger.Events, event)
			continue
		}
		marker := &director.MarkerSets[location.markerSetIndex].Markers[location.placementIndex]
		marker.Events = append(marker.Events, event)
	}
	err = rows.Err()
	closeErr = rows.Close()
	if err != nil {
		return LevelDirector{}, fmt.Errorf("directorEventRows: %w", err)
	}
	if closeErr != nil {
		return LevelDirector{}, fmt.Errorf("directorEventClose: %w", closeErr)
	}

	rows, err = s.database.QueryContext(ctx, `
		SELECT level_marker_set.ordinal, level_marker_set.asset_name, level_marker_set.weight,
		       marker.ordinal, marker.marker_id,
		       marker.marker_name, marker.noun_name,
		       marker.position_x, marker.position_y, marker.position_z,
		       marker.rotation_x, marker.rotation_y, marker.rotation_z,
		       marker.scale, marker.is_visible, marker.is_collision_enabled,
		       marker.interactable_ability, marker.interactable_use_limit,
		       marker.interactable_challenge,
		       level_event.ordinal,
		       level_event.event_name, level_script.callback_name,
		       lua_chunk.id, lua_chunk.source_name, lua_chunk.bytecode_sha256
		FROM level_script
		JOIN level_event ON level_event.id=level_script.level_event_id
		JOIN marker ON marker.id=level_event.marker_id
		JOIN level_marker_set ON level_marker_set.id=marker.level_marker_set_id
		JOIN lua_chunk ON lua_chunk.id=level_script.lua_chunk_id
		WHERE level_marker_set.level_id=?
		ORDER BY level_marker_set.ordinal, marker.ordinal, level_event.ordinal,
		         lua_chunk.source_name, level_script.callback_name`, director.LevelID)
	if err != nil {
		return LevelDirector{}, fmt.Errorf("directorScriptQuery: %w", err)
	}
	for rows.Next() {
		var script LevelScriptBinding
		var markerID int64
		var markerSetWeight int64
		var isVisible int
		var isCollisionEnabled int
		var interactableAbility *string
		var interactableUseLimit *int32
		var interactableChallenge *int32
		err = rows.Scan(&script.MarkerSetOrdinal, &script.MarkerSetName, &markerSetWeight,
			&script.MarkerOrdinal, &markerID,
			&script.MarkerName, &script.NounName,
			&script.PositionX, &script.PositionY, &script.PositionZ,
			&script.RotationX, &script.RotationY, &script.RotationZ,
			&script.Scale, &isVisible, &isCollisionEnabled,
			&interactableAbility, &interactableUseLimit, &interactableChallenge,
			&script.EventOrdinal,
			&script.EventName, &script.CallbackName, &script.LuaChunkID,
			&script.LuaSourceName, &script.LuaSHA256)
		if err != nil {
			_ = rows.Close()
			return LevelDirector{}, fmt.Errorf("directorScriptScan: %w", err)
		}
		if markerID < 0 || markerID > math.MaxUint32 ||
			markerSetWeight < 0 || markerSetWeight > math.MaxUint32 {
			_ = rows.Close()
			return LevelDirector{}, fmt.Errorf("directorScriptMarker[%d]: marker=%d weight=%d",
				script.MarkerOrdinal, markerID, markerSetWeight)
		}
		script.MarkerID = uint32(markerID)
		script.MarkerSetWeight = uint32(markerSetWeight)
		script.IsVisible = isVisible != 0
		script.IsCollisionEnabled = isCollisionEnabled != 0
		if interactableAbility != nil && interactableUseLimit != nil && interactableChallenge != nil {
			script.InteractableAbility = *interactableAbility
			script.InteractableUseLimit = *interactableUseLimit
			script.InteractableChallenge = *interactableChallenge
		}
		director.Scripts = append(director.Scripts, script)
	}
	err = rows.Err()
	closeErr = rows.Close()
	if err != nil {
		return LevelDirector{}, fmt.Errorf("directorScriptRows: %w", err)
	}
	if closeErr != nil {
		return LevelDirector{}, fmt.Errorf("directorScriptClose: %w", closeErr)
	}
	return director, nil
}

func classifyDirectorMarker(nounName string) (uint32, string, bool) {
	switch {
	case strings.EqualFold(nounName, "SpawnPoint_DirectorHorde.Noun"):
		return 5, "agent", true
	case strings.EqualFold(nounName, "SpawnPoint_DirectorWanderer.Noun"):
		return 7, "minion", true
	case strings.EqualFold(nounName, "SpawnPoint_DirectorSpike.Noun"):
		return 8, "minion", true
	}
	return 0, "", false
}
