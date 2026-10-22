package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
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
	ConfigurationName    string
	ConfigKind           string
	SpawnKind            string
	Entries              []LevelDirectorEntry
}

// LevelDirectorEvent is one authored event binding on a director placement.
// It records content metadata without deciding when or how to spawn anything.
type LevelDirectorEvent struct {
	EventHash          uint32
	NativeCallbackHash uint32
	LuaCallbackName    *string
	Ordinal            int
	ComponentName      string
	EventKind          string
	EventSlot          string
	EventName          string
	CallbackName       string
	TriggerRadius      float32
	IsTriggerOnceOnly  bool
	IsServerOnly       bool
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
	SpawnSectionType        uint32
	IsSpawnSectionKnown     bool
	IsSpikeActive           bool
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
	SpatialRadius           float32
	ExclusionRadius         float32
	IsExclusionVolume       bool
	Events                  []LevelDirectorEvent
	SpawnTrigger            *SpawnTriggerDefinition
	EventListener           *EventListenerDefinition
	Interactable            *InteractableDefinition
	Combatant               *CombatantDefinition
}

// LevelDirectorTrigger is one authored player-entry trigger associated with a
// director marker set. Its callback remains metadata until server policy owns
// the corresponding event publication.
type LevelDirectorTrigger struct {
	RotationX         float32
	RotationY         float32
	RotationZ         float32
	ExclusionRadius   float32
	IsExclusionVolume bool
	Ordinal           int
	MarkerID          uint32
	Name              string
	NounName          string
	PositionX         float32
	PositionY         float32
	PositionZ         float32
	Events            []LevelDirectorEvent
	SpawnTrigger      *SpawnTriggerDefinition
	EventListener     *EventListenerDefinition
	Interactable      *InteractableDefinition
	Combatant         *CombatantDefinition
}

// LevelDirectorMarkerSet preserves one authored placement-set boundary.
type LevelDirectorMarkerSet struct {
	Definitions       []LevelMarkerDefinition
	Ordinal           int
	Name              string
	CatalogOrdinal    *uint32
	CatalogAssetName  string
	CatalogSourceName string
	GroupName         string
	Weight            uint32
	Conditions        []uint32
	Markers           []LevelDirectorMarker
	Triggers          []LevelDirectorTrigger
}

// LevelScriptBinding links one authored level event to its imported Lua chunk.
// It is immutable content metadata and does not grant the script authority to
// mutate gameplay state.
type LevelScriptBinding struct {
	Interactable          *InteractableDefinition
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
	Camera            *LevelCameraSettings
	ObjectivePool     LevelObjectivePool
	LevelID           int64
	Name              string
	CatalogOrdinal    *uint32
	CatalogAssetName  string
	CatalogSourceName string
	// PlanetConfigName identifies an imported external configuration.
	PlanetConfigName string
	PrimaryType      uint32
	SecondaryType    uint32
	TertiaryType     uint32
	QuaternaryType   uint32
	EntryPositions   [][3]float32
	Pools            []LevelDirectorPool
	ExternalPools    []LevelDirectorPool
	MarkerSets       []LevelDirectorMarkerSet
	Scripts          []LevelScriptBinding
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
	var levelCatalogOrdinal sql.NullInt64
	err := s.database.QueryRowContext(ctx, `
		SELECT level.id, level.name, level.planet_config,
		       level.primary_type, level.secondary_type, level.tertiary_type, level.quaternary_type,
		       catalog.ordinal, COALESCE(catalog.asset_name, ''), COALESCE(catalog.source_file_name, '')
		FROM level
		JOIN level_alias ON level_alias.level_id=level.id
		LEFT JOIN asset_catalog AS catalog ON catalog.ordinal=(
		    SELECT MIN(ordinal) FROM asset_catalog WHERE content_source_resource_id=level.content_source_resource_id)
		WHERE level_alias.alias=? COLLATE NOCASE
		LIMIT 1`, levelName).Scan(&director.LevelID, &director.Name, &director.PlanetConfigName,
		&director.PrimaryType, &director.SecondaryType, &director.TertiaryType,
		&director.QuaternaryType,
		&levelCatalogOrdinal, &director.CatalogAssetName, &director.CatalogSourceName)
	if err != nil {
		return LevelDirector{}, fmt.Errorf("directorLevel[%s]: %w", levelName, err)
	}
	if levelCatalogOrdinal.Valid {
		if levelCatalogOrdinal.Int64 < 0 || levelCatalogOrdinal.Int64 > math.MaxUint32 {
			return LevelDirector{}, fmt.Errorf("directorLevelCatalog[%s]: %d", levelName, levelCatalogOrdinal.Int64)
		}
		ordinal := uint32(levelCatalogOrdinal.Int64)
		director.CatalogOrdinal = &ordinal
	}
	director.Camera, err = s.LevelCamera(ctx, director.LevelID)
	if err != nil {
		return LevelDirector{}, fmt.Errorf("directorCamera: %w", err)
	}
	director.ObjectivePool, err = s.LevelObjectivePool(ctx, levelName)
	if err != nil {
		return LevelDirector{}, fmt.Errorf("directorObjectivePool: %w", err)
	}
	director.ExternalPools, err = s.externalDirectorPools(ctx)
	if err != nil {
		return LevelDirector{}, fmt.Errorf("directorExternal: %w", err)
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
		       config_kind, configuration_name, spawn_kind, noun_name, minimum_difficulty,
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
		var configurationName string
		var minimumDifficulty int64
		var maximumDifficulty int64
		var isHordeLegal int
		err = rows.Scan(&configurationOrdinal, &entry.ConfigurationEntryOrdinal, &entry.Ordinal,
			&entry.ConfigKind, &configurationName, &entry.SpawnKind, &entry.NounName, &minimumDifficulty,
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
				ConfigurationName:    configurationName,
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
		       level_marker_set.group_name, level_marker_set.weight, marker.marker_id, marker.marker_name,
		       marker.noun_name, marker.position_x, marker.position_y, marker.position_z,
		       marker.rotation_x, marker.rotation_y, marker.rotation_z, marker.scale,
		       marker.is_visible, marker.is_collision_enabled, marker.target_marker_id,
		       marker.teleporter_trigger_radius, marker.spawn_section_type, marker.is_spike_active,
		       marker.spatial_radius, marker.exclusion_radius, marker.spawn_trigger_definition,
		       marker.event_listener_definition,
		       marker.interactable_definition, marker.combatant_definition,
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
		  AND (marker.event_listener_definition IS NOT NULL
		       OR marker.interactable_definition IS NOT NULL
		       OR marker.noun_name COLLATE NOCASE IN (
		           'prefab_boss_obelisk.Noun', 'boss_obelisk.Noun', 'prefab_health_obelisk.Noun')
		       OR marker.spawn_trigger_definition IS NOT NULL
		       OR marker.noun_name LIKE 'SpawnPoint_Director%.Noun' COLLATE NOCASE
		       OR marker.exclusion_radius IS NOT NULL
		       OR marker.noun_name LIKE 'Tutorial%.Noun' COLLATE NOCASE
		       OR marker.noun_name='DEST_prefab_islands_instrument_scitech_11.Noun' COLLATE NOCASE
		       OR marker.noun_name='DEST_prefab_islands_instrument_scitech_3.Noun' COLLATE NOCASE
		       OR marker.noun_name='DEST_prefab_islands_instrument_scitech_2.Noun' COLLATE NOCASE
		       OR marker.noun_name='DEST_prefab_islands_instrument_scitech_11_noShadow.Noun' COLLATE NOCASE
		       OR marker.noun_name='DEST_prefab_islands_instrument_scitech_7.Noun' COLLATE NOCASE
		       OR marker.noun_name='DEST_prefab_islands_instrument_scitech_7_noShadow.Noun' COLLATE NOCASE
		       OR marker.noun_name='DEST_prefab_cryos_ice_crack2.Noun' COLLATE NOCASE
		       OR marker.noun_name='DEST_prefab_cryos_acunit_small.Noun' COLLATE NOCASE
		       OR marker.noun_name='DEST_prefab_cryos_ACunit_small_animated.Noun' COLLATE NOCASE
		       OR marker.noun_name='DEST_prefab_cryos_plants_shascope.Noun' COLLATE NOCASE
		       OR marker.noun_name='DEST_prefab_cryos_ice_crack1.Noun' COLLATE NOCASE
		       OR marker.noun_name='DEST_nocturna_herotree_yellow_1.Noun' COLLATE NOCASE
		       OR marker.noun_name='DEST_nocturna_plant_expl.Noun' COLLATE NOCASE
		       OR marker.noun_name='DEST_citadel_factoryvent_boss.Noun' COLLATE NOCASE
		       OR marker.noun_name='DEST_prefab_citadel_factorypipe_plasma.Noun' COLLATE NOCASE
		       OR marker.noun_name='DEST_prefab_citadel_factorypipe_smoke.Noun' COLLATE NOCASE
		       OR marker.noun_name='DEST_prefab_citadel_factorypipe.Noun' COLLATE NOCASE
		       OR marker.noun_name='DEST_prefab_scaldron_plant_large_1.Noun' COLLATE NOCASE
		       OR marker.noun_name='DEST_prefab_scaldron_plant_small_3.Noun' COLLATE NOCASE
		       OR marker.noun_name='DEST_prefab_scaldron_tem_column_statue.Noun' COLLATE NOCASE
		       OR marker.noun_name='DEST_tota_headstatue_b.Noun' COLLATE NOCASE
		       OR marker.noun_name='DEST_tota_headstatue_c.Noun' COLLATE NOCASE
		       OR marker.noun_name='DEST_tota_heroplant_p3_b.Noun' COLLATE NOCASE
		       OR marker.noun_name='DEST_prefab_tota_heroplant_p3_b.Noun' COLLATE NOCASE
		       OR level_marker_set.asset_name COLLATE NOCASE IN (
		              'verdanth_3_Smart_Objects_1.Markerset',
		              'verdanth_3_Smart_Objects_2.Markerset',
		              'verdanth_3_Smart_Objects_3.Markerset',
		              'cryos_3_Smart_Object_1.Markerset',
		              'cryos_3_Smart_Object_2.Markerset',
		              'cryos_3_Smart_Object_3.Markerset',
		              'nocturna_1_Obelisk_1.Markerset',
		              'nocturna_1_Obelisk_2.Markerset',
		              'nocturna_1_Obelisk_3.Markerset',
		              'nocturna_1_Smart_Object_1.Markerset',
		              'nocturna_1_Smart_Object_2.Markerset',
		              'nocturna_1_Smart_Object_3.Markerset',
		              'nocturna_4_Smart_Objects_1.Markerset',
		              'nocturna_4_Smart_Objects_2.Markerset',
		              'nocturna_4_Smart_Objects_3.Markerset',
		              'nocturna_3_Smart_Objects_1.Markerset',
		              'nocturna_3_Smart_Objects_2.Markerset',
		              'nocturna_3_Smart_Objects_3.Markerset',
		              'nocturna_2_Smart_Objects_1.Markerset',
		              'nocturna_2_Smart_Objects_2.Markerset',
		              'nocturna_2_Smart_Objects_3.Markerset',
		              'infinity_2_Obelisk_1.Markerset',
		              'infinity_2_Obelisk_2.Markerset',
		              'infinity_2_Obelisk_3.Markerset',
		              'infinity_2_Smart_Object_1.Markerset',
		              'infinity_2_Smart_Object_2.Markerset',
		              'infinity_2_Smart_Object_3.Markerset',
		              'infinity_1_Obelisk_1.Markerset',
		              'infinity_1_Obelisk_2.Markerset',
		              'infinity_1_Obelisk_3.Markerset',
		              'infinity_1_Smart_Objects_1.Markerset',
		              'infinity_1_Smart_Objects_2.Markerset',
		              'infinity_1_Smart_Objects_3.Markerset',
		              'infinity_4_Obelisk_1.Markerset',
		              'infinity_4_Obelisk_2.Markerset',
		              'infinity_4_Obelisk_3.Markerset',
		              'infinity_4_Smart_Objects_1.Markerset',
		              'infinity_4_Smart_Objects_2.Markerset',
		              'infinity_4_Smart_Objects_3.Markerset',
		              'scaldron_2_Obelisk_1.Markerset',
		              'scaldron_2_Obelisk_2.Markerset',
		              'scaldron_2_Obelisk_3.Markerset',
		              'scaldron_2_Smart_Objects_1.Markerset',
		              'scaldron_2_Smart_Objects_2.Markerset',
		              'scaldron_2_Smart_Objects_3.Markerset',
		              'scaldron_1_Obelisk_1.Markerset',
		              'scaldron_1_Obelisk_2.Markerset',
		              'scaldron_1_Obelisk_3.Markerset',
		              'scaldron_1_Smart_Object_1.Markerset',
		              'scaldron_1_Smart_Object_2.Markerset',
		              'scaldron_1_Smart_Object_3.Markerset',
		              'scaldron_3_Obelisk_1.Markerset',
		              'scaldron_3_Obelisk_2.Markerset',
		              'scaldron_3_Obelisk_3.Markerset',
		              'scaldron_3_Smart_Objects_1.Markerset',
		              'scaldron_3_Smart_Objects_2.Markerset',
		              'scaldron_3_Smart_Objects_3.Markerset',
		              'scaldron_4_Obelisk_1.Markerset',
		              'scaldron_4_Obelisk_2.Markerset',
		              'scaldron_4_Obelisk_3.Markerset',
		              'scaldron_4_Smart_Objects_1.Markerset',
		              'scaldron_4_Smart_Objects_2.Markerset',
		              'scaldron_4_Smart_Objects_3.Markerset',
		              'infinity_3_Smart_Objects_1.Markerset',
		              'infinity_3_Smart_Objects_2.Markerset',
		              'infinity_3_Smart_Objects_3.Markerset',
		              'cryos_1_Smart_Object_1.Markerset',
		              'cryos_1_Smart_Object_2.Markerset',
		              'cryos_1_Smart_Object_3.Markerset',
		              'cryos_2_smart_objects_1.Markerset',
		              'cryos_2_smart_objects_2.Markerset',
		              'cryos_2_smart_objects_3.Markerset')
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
		var markerSetGroup string
		var marker LevelDirectorMarker
		var markerID int64
		var targetMarkerID int64
		var markerSetWeight int64
		var spawnSectionType *int64
		var isSpikeActive *int
		var exclusionRadius *float32
		var spawnDefinition sql.NullString
		var listenerDefinition sql.NullString
		var interactableDefinition, combatantDefinition sql.NullString
		var isTrigger int
		var isVisible int
		var isCollisionEnabled int
		err = rows.Scan(&markerSetOrdinal, &markerDatabaseID, &marker.Ordinal, &markerSetName,
			&markerSetGroup, &markerSetWeight, &markerID, &marker.Name, &marker.NounName,
			&marker.PositionX, &marker.PositionY, &marker.PositionZ,
			&marker.RotationX, &marker.RotationY, &marker.RotationZ, &marker.Scale,
			&isVisible, &isCollisionEnabled, &targetMarkerID,
			&marker.TeleporterTriggerRadius, &spawnSectionType, &isSpikeActive,
			&marker.SpatialRadius, &exclusionRadius, &spawnDefinition, &listenerDefinition,
			&interactableDefinition, &combatantDefinition, &isTrigger)
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
		if spawnDefinition.Valid {
			err = json.Unmarshal([]byte(spawnDefinition.String), &marker.SpawnTrigger)
			if err != nil {
				closeErr := rows.Close()
				return LevelDirector{}, fmt.Errorf("directorSpawnDecode: %w", errors.Join(err, closeErr))
			}
		}
		marker.MarkerID = uint32(markerID)
		if listenerDefinition.Valid {
			err = json.Unmarshal([]byte(listenerDefinition.String), &marker.EventListener)
			if err != nil {
				closeErr := rows.Close()
				return LevelDirector{}, fmt.Errorf("directorListenerDecode: %w", errors.Join(err, closeErr))
			}
		}
		marker.TargetMarkerID = uint32(targetMarkerID)
		if interactableDefinition.Valid {
			err = json.Unmarshal([]byte(interactableDefinition.String), &marker.Interactable)
			if err != nil {
				closeErr := rows.Close()
				return LevelDirector{}, fmt.Errorf("directorInteractableDecode: %w", errors.Join(err, closeErr))
			}
		}
		if combatantDefinition.Valid {
			err = json.Unmarshal([]byte(combatantDefinition.String), &marker.Combatant)
			if err != nil {
				closeErr := rows.Close()
				return LevelDirector{}, fmt.Errorf("directorCombatantDecode: %w", errors.Join(err, closeErr))
			}
		}
		marker.IsVisible = isVisible != 0
		marker.IsCollisionEnabled = isCollisionEnabled != 0
		if exclusionRadius != nil {
			marker.ExclusionRadius = *exclusionRadius
			marker.IsExclusionVolume = true
		}
		if spawnSectionType != nil {
			if *spawnSectionType < 0 || *spawnSectionType > 3 {
				_ = rows.Close()
				return LevelDirector{}, fmt.Errorf("directorSection[%d]: %d", marker.Ordinal, *spawnSectionType)
			}
			marker.SpawnSectionType = uint32(*spawnSectionType)
			marker.IsSpawnSectionKnown = true
		}
		if isSpikeActive != nil {
			marker.IsSpikeActive = *isSpikeActive != 0
		}
		marker.SpawnKind, marker.PoolKind, marker.IsSpawnKindKnown = classifyDirectorMarker(marker.NounName)
		markerSetIndex := len(director.MarkerSets) - 1
		if markerSetIndex < 0 || director.MarkerSets[markerSetIndex].Ordinal != markerSetOrdinal {
			director.MarkerSets = append(director.MarkerSets, LevelDirectorMarkerSet{
				Ordinal: markerSetOrdinal, Name: markerSetName, GroupName: markerSetGroup,
				Weight: uint32(markerSetWeight),
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
			RotationX: marker.RotationX, RotationY: marker.RotationY, RotationZ: marker.RotationZ,
			Ordinal: marker.Ordinal, MarkerID: marker.MarkerID, Name: marker.Name,
			NounName: marker.NounName, PositionX: marker.PositionX,
			PositionY: marker.PositionY, PositionZ: marker.PositionZ,
			ExclusionRadius: marker.ExclusionRadius, IsExclusionVolume: marker.IsExclusionVolume,
			SpawnTrigger: marker.SpawnTrigger, EventListener: marker.EventListener,
			Interactable: marker.Interactable, Combatant: marker.Combatant,
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
		       level_event.is_trigger_once_only, level_event.is_server_only,
		       level_event.event_hash, level_event.native_callback_hash, level_event.lua_callback_name
		FROM level_event
		JOIN marker ON marker.id=level_event.marker_id
		JOIN level_marker_set ON level_marker_set.id=marker.level_marker_set_id
		WHERE level_marker_set.level_id=?
		  AND (marker.event_listener_definition IS NOT NULL
		       OR marker.noun_name LIKE 'SpawnPoint_Director%.Noun' COLLATE NOCASE
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
			&event.TriggerRadius, &isTriggerOnceOnly, &isServerOnly,
			&event.EventHash, &event.NativeCallbackHash, &event.LuaCallbackName)
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
		       marker.interactable_challenge, marker.interactable_definition,
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
		var interactableDefinition sql.NullString
		err = rows.Scan(&script.MarkerSetOrdinal, &script.MarkerSetName, &markerSetWeight,
			&script.MarkerOrdinal, &markerID,
			&script.MarkerName, &script.NounName,
			&script.PositionX, &script.PositionY, &script.PositionZ,
			&script.RotationX, &script.RotationY, &script.RotationZ,
			&script.Scale, &isVisible, &isCollisionEnabled,
			&interactableAbility, &interactableUseLimit, &interactableChallenge, &interactableDefinition,
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
		if interactableDefinition.Valid {
			err = json.Unmarshal([]byte(interactableDefinition.String), &script.Interactable)
			if err != nil {
				closeErr := rows.Close()
				return LevelDirector{}, fmt.Errorf("scriptInteractable: %w", errors.Join(err, closeErr))
			}
		}
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
	// Placement loading above intentionally filters out unrelated scenery.
	// Layout selection still needs every authored set, including empty
	// alternatives, to preserve native random draws.
	// The importer also retains unresolved level references as placeholder
	// rows without a resource, group, or payload (infinity_4_plants is one).
	// Those are not loaded marker sets and must not enter layout selection.
	loadedSetIndexes := make(map[int]int, len(director.MarkerSets))
	for index, markerSet := range director.MarkerSets {
		loadedSetIndexes[markerSet.Ordinal] = index
	}
	rows, err = s.database.QueryContext(ctx, `
		SELECT level_marker_set.ordinal, level_marker_set.asset_name,
		       level_marker_set.group_name, level_marker_set.weight,
		       catalog.ordinal, COALESCE(catalog.asset_name, ''),
		       COALESCE(catalog.source_file_name, '')
		FROM level_marker_set
		LEFT JOIN asset_catalog AS catalog ON catalog.ordinal=(
		    SELECT MIN(ordinal) FROM asset_catalog
		    WHERE content_source_resource_id=level_marker_set.content_source_resource_id)
		WHERE level_marker_set.level_id=?
		  AND level_marker_set.content_source_resource_id IS NOT NULL
		ORDER BY level_marker_set.ordinal`, director.LevelID)
	if err != nil {
		return LevelDirector{}, fmt.Errorf("directorLayoutSetQuery: %w", err)
	}
	for rows.Next() {
		var markerSet LevelDirectorMarkerSet
		var weight int64
		var catalogOrdinal sql.NullInt64
		err = rows.Scan(&markerSet.Ordinal, &markerSet.Name, &markerSet.GroupName, &weight,
			&catalogOrdinal, &markerSet.CatalogAssetName, &markerSet.CatalogSourceName)
		if err != nil {
			closeErr := rows.Close()
			return LevelDirector{}, fmt.Errorf("directorLayoutSetScan: %w", errors.Join(err, closeErr))
		}
		if weight < 0 || weight > math.MaxUint32 {
			closeErr := rows.Close()
			weightErr := fmt.Errorf("directorLayoutSetWeight[%d]: %d", markerSet.Ordinal, weight)
			return LevelDirector{}, errors.Join(weightErr, closeErr)
		}
		if catalogOrdinal.Valid {
			if catalogOrdinal.Int64 < 0 || catalogOrdinal.Int64 > math.MaxUint32 {
				closeErr := rows.Close()
				ordinalErr := fmt.Errorf("directorLayoutSetCatalog[%d]: %d", markerSet.Ordinal, catalogOrdinal.Int64)
				return LevelDirector{}, errors.Join(ordinalErr, closeErr)
			}
			ordinal := uint32(catalogOrdinal.Int64)
			markerSet.CatalogOrdinal = &ordinal
		}
		loadedIndex, isLoaded := loadedSetIndexes[markerSet.Ordinal]
		if isLoaded {
			director.MarkerSets[loadedIndex].CatalogOrdinal = markerSet.CatalogOrdinal
			director.MarkerSets[loadedIndex].CatalogAssetName = markerSet.CatalogAssetName
			director.MarkerSets[loadedIndex].CatalogSourceName = markerSet.CatalogSourceName
			continue
		}
		markerSet.Weight = uint32(weight)
		director.MarkerSets = append(director.MarkerSets, markerSet)
		loadedSetIndexes[markerSet.Ordinal] = len(director.MarkerSets) - 1
	}
	err = rows.Err()
	closeErr = rows.Close()
	if err != nil {
		return LevelDirector{}, fmt.Errorf("directorLayoutSetRows: %w", errors.Join(err, closeErr))
	}
	if closeErr != nil {
		return LevelDirector{}, fmt.Errorf("directorLayoutSetClose: %w", closeErr)
	}
	sort.Slice(director.MarkerSets, func(left, right int) bool {
		return director.MarkerSets[left].Ordinal < director.MarkerSets[right].Ordinal
	})
	setIndexes := make(map[int]int, len(director.MarkerSets))
	for index, markerSet := range director.MarkerSets {
		setIndexes[markerSet.Ordinal] = index
	}
	rows, err = s.database.QueryContext(ctx, `
		SELECT level_marker_set.ordinal, level_marker_set_condition.condition
		FROM level_marker_set_condition
		JOIN level_marker_set ON level_marker_set.id=level_marker_set_condition.level_marker_set_id
		WHERE level_marker_set.level_id=?
		ORDER BY level_marker_set.ordinal, level_marker_set_condition.ordinal`, director.LevelID)
	if err != nil {
		return LevelDirector{}, fmt.Errorf("directorConditionQuery: %w", err)
	}
	for rows.Next() {
		var setOrdinal int
		var condition uint32
		err = rows.Scan(&setOrdinal, &condition)
		if err != nil {
			_ = rows.Close()
			return LevelDirector{}, fmt.Errorf("directorConditionScan: %w", err)
		}
		setIndex, isSetFound := setIndexes[setOrdinal]
		if !isSetFound {
			_ = rows.Close()
			return LevelDirector{}, fmt.Errorf("directorConditionSet[%d]: missing", setOrdinal)
		}
		director.MarkerSets[setIndex].Conditions = append(director.MarkerSets[setIndex].Conditions, condition)
	}
	err = rows.Err()
	closeErr = rows.Close()
	if err != nil {
		return LevelDirector{}, fmt.Errorf("directorConditionRows: %w", err)
	}
	if closeErr != nil {
		return LevelDirector{}, fmt.Errorf("directorConditionClose: %w", closeErr)
	}
	err = s.loadMarkerDefinitions(ctx, &director)
	if err != nil {
		return LevelDirector{}, fmt.Errorf("directorDefinitions: %w", err)
	}
	return director, nil
}

func (e *Store) externalDirectorPools(ctx context.Context) ([]LevelDirectorPool, error) {
	// Materialize empty roles too, so an empty configuration remains distinct
	// from a missing resource when campaign composition resolves its sources.
	rows, err := e.database.QueryContext(ctx, `
		WITH role(ordinal, name) AS (
			VALUES (0, 'minion'), (1, 'special'), (2, 'boss'), (3, 'agent'), (4, 'captain')
		)
		SELECT external_config.id * 5 + role.ordinal, external_config.name, role.name,
		       COALESCE(external_config_entry.configuration_entry_ordinal, -1),
		       COALESCE(external_config_entry.noun_name, ''),
		       COALESCE(external_config_entry.minimum_difficulty, 0),
		       COALESCE(external_config_entry.maximum_difficulty, 0),
		       COALESCE(external_config_entry.is_horde_legal, 0)
		FROM external_config CROSS JOIN role
		LEFT JOIN external_config_entry
		  ON external_config_entry.external_config_id=external_config.id
		 AND external_config_entry.configuration_ordinal=role.ordinal
		ORDER BY external_config.id, role.ordinal, external_config_entry.configuration_entry_ordinal`)
	if err != nil {
		return nil, fmt.Errorf("externalQuery: %w", err)
	}
	defer rows.Close()
	pools := make([]LevelDirectorPool, 0, 70)
	for rows.Next() {
		var configurationOrdinal int
		var configurationName, configKind string
		var entry LevelDirectorEntry
		err = rows.Scan(&configurationOrdinal, &configurationName, &configKind,
			&entry.ConfigurationEntryOrdinal, &entry.NounName,
			&entry.MinimumDifficulty, &entry.MaximumDifficulty, &entry.IsHordeLegal)
		if err != nil {
			return nil, fmt.Errorf("externalScan: %w", err)
		}
		poolIndex := len(pools) - 1
		if poolIndex < 0 || pools[poolIndex].ConfigurationOrdinal != configurationOrdinal {
			pools = append(pools, LevelDirectorPool{
				ConfigurationOrdinal: configurationOrdinal, ConfigurationName: configurationName,
				ConfigKind: configKind, SpawnKind: "unknown",
			})
			poolIndex++
		}
		if entry.ConfigurationEntryOrdinal < 0 {
			continue
		}
		entry.Ordinal = entry.ConfigurationEntryOrdinal
		entry.ConfigKind = configKind
		entry.SpawnKind = "unknown"
		pools[poolIndex].Entries = append(pools[poolIndex].Entries, entry)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("externalRows: %w", err)
	}
	err = rows.Close()
	if err != nil {
		return nil, fmt.Errorf("externalClose: %w", err)
	}
	return pools, nil
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
