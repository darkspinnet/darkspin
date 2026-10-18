package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

const (
	levelAssetType       = 0xb9193960
	markerSetAssetType   = 0xa11d3144
	levelConfigAssetType = 0x52d095f6
	markerFixedOffset    = 0x28
	markerFixedSize      = 0xc8
	// These are authored resource names, not launcher branding.
	tutorialLevelName  = "Darkspore_Tutorial_cryos_1"
	tutorialLevelAlias = tutorialLevelName + "_v2"
)

type stringField struct {
	offset int
	text   string
}

type levelAsset struct {
	ordinal         int
	entry           dbpf.Entry
	payload         []byte
	name            string
	markerSetNames  []string
	music           string
	navMesh         string
	physicsMesh     string
	renderingConfig string
	planetConfig    string
	primaryType     uint32
	secondaryType   uint32
	tertiaryType    uint32
	quaternaryType  uint32
	cameraPitch     *float32
	cameraYaw       *float32
	cameraDistance  *float32
	directorEntries []levelDirectorAsset
}

type levelDirectorAsset struct {
	configurationOrdinal      int
	configurationEntryOrdinal int
	configurationName         string
	configKind                string
	nounName                  string
	minimumDifficulty         uint32
	maximumDifficulty         uint32
	isHordeLegal              bool
}

type markerAsset struct {
	markerID                uint32
	markerName              string
	nounName                string
	positionX               float32
	positionY               float32
	positionZ               float32
	rotationX               float32
	rotationY               float32
	rotationZ               float32
	scale                   float32
	dimensionX              float32
	dimensionY              float32
	dimensionZ              float32
	isVisible               bool
	isCollisionEnabled      bool
	targetMarkerID          uint32
	teleporterTriggerRadius float32
	teleporter              *TeleporterDefinition
	spawnTrigger            *SpawnTriggerDefinition
	eventListener           *EventListenerDefinition
	componentStrings        []string
	triggerProperties       map[string]markerTriggerProperty
	interactable            *InteractableDefinition
	combatant               *CombatantDefinition
	spawnSectionType        *uint32
	isSpikeActive           *bool
	spatialRadius           float32
	exclusionRadius         *float32
}

type markerTriggerProperty struct {
	radius            float32
	isTriggerOnceOnly bool
	isServerOnly      bool
}

type markerSetAsset struct {
	groupName  string
	weight     float32
	conditions []uint32
	markers    []markerAsset
}

type markerPair struct {
	nameIndex int
	nounIndex int
}

func writeLevels(ctx context.Context, transaction *sql.Tx, installPath string) error {
	packagePath := filepath.Join(installPath, "Data", "AssetData_Binary.package")
	r, err := os.Open(packagePath)
	if err != nil {
		return fmt.Errorf("assetOpen: %w", err)
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("assetStat: %w", err)
	}
	pkg, err := importPackageReader(ctx, r, fi.Size())
	if err != nil {
		return fmt.Errorf("assetPackage: %w", err)
	}
	err = insertExternalConfigs(ctx, transaction, pkg)
	if err != nil {
		return fmt.Errorf("externalWrite: %w", err)
	}
	spikeSpatialRadius, err := readSpikeSpatialRadius(ctx, pkg)
	if err != nil {
		return fmt.Errorf("spikeSpatialRead: %w", err)
	}
	markerEntry := make(map[uint32]struct {
		ordinal int
		entry   dbpf.Entry
	})
	levels := make([]levelAsset, 0, 61)
	for ordinal, entry := range pkg.Entries {
		if entry.Type == markerSetAssetType {
			markerEntry[uint32(entry.Instance)] = struct {
				ordinal int
				entry   dbpf.Entry
			}{ordinal: ordinal, entry: entry}
			continue
		}
		if entry.Type != levelAssetType {
			continue
		}
		payload, readErr := readDecodedResource(ctx, pkg, entry)
		if readErr != nil {
			return fmt.Errorf("levelRead[%d]: %w", ordinal, readErr)
		}
		level, decodeErr := decodeLevelAsset(ordinal, entry, payload)
		if decodeErr != nil {
			return fmt.Errorf("levelDecode[%d]: %w", ordinal, decodeErr)
		}
		levels = append(levels, level)
	}
	sort.Slice(levels, func(left, right int) bool {
		return strings.ToLower(levels[left].name) < strings.ToLower(levels[right].name)
	})
	callbackChunkIDs, err := loadLuaCallbackChunkIDs(ctx, transaction)
	if err != nil {
		return fmt.Errorf("callbackLoad: %w", err)
	}
	err = insertLevelAssets(ctx, transaction, pkg, levels, markerEntry, callbackChunkIDs, spikeSpatialRadius)
	if err != nil {
		return fmt.Errorf("levelWrite: %w", err)
	}
	err = insertLevelNavigation(ctx, transaction, installPath, levels)
	if err != nil {
		return fmt.Errorf("navigationWrite: %w", err)
	}
	return nil
}

func insertLevelNavigation(
	ctx context.Context, transaction *sql.Tx, installPath string, levels []levelAsset,
) error {
	packagePath := filepath.Join(installPath, "Data", "Levels.package")
	r, err := os.Open(packagePath)
	if err != nil {
		return fmt.Errorf("navigationOpen: %w", err)
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("navigationStat: %w", err)
	}
	pkg, err := importPackageReader(ctx, r, fi.Size())
	if err != nil {
		return fmt.Errorf("navigationPackage: %w", err)
	}
	type navigationEntry struct {
		ordinal int
		entry   dbpf.Entry
	}
	entryByGroup := make(map[uint32][]navigationEntry)
	for ordinal, entry := range pkg.Entries {
		if entry.Type != bfxNavigationType {
			continue
		}
		entryByGroup[entry.Group] = append(entryByGroup[entry.Group], navigationEntry{
			ordinal: ordinal, entry: entry,
		})
	}
	statement, err := transaction.PrepareContext(ctx, `
		INSERT INTO level_navigation
			(level_id, content_ordinal, type_id, group_id, instance_id, decoded_size,
			 decoded_sha256, decoded_compression, decoded_payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("navigationPrepare: %w", err)
	}
	defer statement.Close()
	for levelIndex, level := range levels {
		select {
		case <-ctx.Done():
			return fmt.Errorf("navigationContext: %w", ctx.Err())
		default:
		}
		groupID := uint32(level.entry.Instance)
		entries := entryByGroup[groupID]
		if len(entries) == 0 {
			continue
		}
		if len(entries) != 1 {
			return fmt.Errorf("navigationEntry[%s]: got %d, want 1", level.name, len(entries))
		}
		resource := entries[0]
		decoded, readErr := readDecodedResource(ctx, pkg, resource.entry)
		if readErr != nil {
			return fmt.Errorf("navigationRead[%s]: %w", level.name, readErr)
		}
		compressed, compressErr := compressContent(decoded)
		if compressErr != nil {
			return fmt.Errorf("navigationCompress[%s]: %w", level.name, compressErr)
		}
		digest := sha256.Sum256(decoded)
		_, err = statement.ExecContext(
			ctx, levelIndex+1, resource.ordinal, resource.entry.Type, resource.entry.Group,
			resource.entry.Instance, len(decoded), fmt.Sprintf("%x", digest), "zlib", compressed,
		)
		if err != nil {
			return fmt.Errorf("navigationInsert[%s]: %w", level.name, err)
		}
	}
	return nil
}

func insertLevelAssets(ctx context.Context, transaction *sql.Tx, pkg *dbpf.Reader, levels []levelAsset, markerEntry map[uint32]struct {
	ordinal int
	entry   dbpf.Entry
}, callbackChunkIDs map[string][]int64, spikeSpatialRadius float32) error {
	levelStatement, err := transaction.PrepareContext(ctx, `
		INSERT INTO level
		(id, content_source_resource_id, name, package_group_id, music, nav_mesh, physics_mesh,
		 rendering_config, planet_config, primary_type, secondary_type, tertiary_type, quaternary_type, camera_pitch, camera_yaw,
		 camera_distance, source_sha256, source_size, source_compression, source_payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("levelPrepare: %w", err)
	}
	aliasStatement, err := transaction.PrepareContext(ctx, `
		INSERT INTO level_alias (id, level_id, alias, alias_kind) VALUES (?, ?, ?, ?)`)
	if err != nil {
		_ = levelStatement.Close()
		return fmt.Errorf("aliasPrepare: %w", err)
	}
	setStatement, err := transaction.PrepareContext(ctx, `
		INSERT INTO level_marker_set
		(id, level_id, content_source_resource_id, ordinal, asset_name, group_name, weight,
		 source_sha256, source_size, source_compression, source_payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = aliasStatement.Close()
		_ = levelStatement.Close()
		return fmt.Errorf("setPrepare: %w", err)
	}
	conditionStatement, err := transaction.PrepareContext(ctx, `
		INSERT INTO level_marker_set_condition (level_marker_set_id, ordinal, condition)
		VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("conditionPrepare: %w", err)
	}
	markerStatement, err := transaction.PrepareContext(ctx, `
		INSERT INTO marker
		(id, level_marker_set_id, ordinal, marker_id, marker_name, noun_name,
		 position_x, position_y, position_z, rotation_x, rotation_y, rotation_z, scale,
		 dimension_x, dimension_y, dimension_z, is_visible, is_collision_enabled,
		 asset_override_id, target_marker_id, teleporter_trigger_radius, interactable_ability,
		 interactable_use_limit, interactable_challenge, spawn_section_type, is_spike_active,
		 spatial_radius, exclusion_radius, teleporter_definition, is_trigger_creation_deferred,
		 spawn_trigger_definition, event_listener_definition, interactable_definition, combatant_definition)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = setStatement.Close()
		_ = aliasStatement.Close()
		_ = levelStatement.Close()
		return fmt.Errorf("markerPrepare: %w", err)
	}
	eventStatement, err := transaction.PrepareContext(ctx, `
		INSERT INTO level_event
		(id, marker_id, ordinal, component_name, event_kind, event_slot, event_name, callback_name,
		 trigger_radius, is_trigger_once_only, is_server_only, event_hash, native_callback_hash, lua_callback_name)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = markerStatement.Close()
		_ = setStatement.Close()
		_ = aliasStatement.Close()
		_ = levelStatement.Close()
		return fmt.Errorf("eventPrepare: %w", err)
	}
	directorStatement, err := transaction.PrepareContext(ctx, `
		INSERT INTO level_director_entry
		(id, level_id, config_kind, configuration_name, spawn_kind, configuration_ordinal,
		 configuration_entry_ordinal, ordinal, noun_name, minimum_difficulty,
		 maximum_difficulty, is_horde_legal)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = eventStatement.Close()
		_ = markerStatement.Close()
		_ = setStatement.Close()
		_ = aliasStatement.Close()
		_ = levelStatement.Close()
		return fmt.Errorf("directorPrepare: %w", err)
	}

	aliasID := int64(1)
	setID := int64(1)
	markerID := int64(1)
	eventID := int64(1)
	directorID := int64(1)
	for levelIndex, level := range levels {
		select {
		case <-ctx.Done():
			return fmt.Errorf("levelContext: %w", ctx.Err())
		default:
		}
		levelID := int64(levelIndex + 1)
		sourceDigest := sha256.Sum256(level.payload)
		compressedPayload, compressErr := compressContent(level.payload)
		if compressErr != nil {
			return fmt.Errorf("levelCompress[%s]: %w", level.name, compressErr)
		}
		_, err = levelStatement.ExecContext(ctx, levelID, level.ordinal+1, level.name,
			int64(uint32(level.entry.Instance)), level.music, level.navMesh, level.physicsMesh,
			level.renderingConfig, level.planetConfig, level.primaryType, level.secondaryType, level.tertiaryType,
			level.quaternaryType,
			level.cameraPitch, level.cameraYaw, level.cameraDistance, fmt.Sprintf("%x", sourceDigest),
			len(level.payload), "zlib", compressedPayload)
		if err != nil {
			return fmt.Errorf("levelInsert[%s]: %w", level.name, err)
		}
		aliases := []struct {
			name string
			kind string
		}{
			{name: level.name, kind: "asset_name"},
			{name: level.name + ".Level", kind: "asset_reference"},
		}
		if strings.EqualFold(level.name, tutorialLevelName) {
			aliases = append(aliases,
				struct {
					name string
					kind string
				}{name: tutorialLevelAlias, kind: "client_alias"},
				struct {
					name string
					kind string
				}{name: tutorialLevelAlias + ".Level", kind: "client_alias"},
			)
		}
		for _, alias := range aliases {
			_, err = aliasStatement.ExecContext(ctx, aliasID, levelID, alias.name, alias.kind)
			if err != nil {
				return fmt.Errorf("aliasInsert[%s]: %w", alias.name, err)
			}
			aliasID++
		}
		for ordinal, director := range level.directorEntries {
			configKind := director.configKind
			if configKind == "" {
				configKind = "unknown"
			}
			_, err = directorStatement.ExecContext(ctx, directorID, levelID, configKind, director.configurationName, "unknown",
				director.configurationOrdinal, director.configurationEntryOrdinal, ordinal,
				director.nounName, director.minimumDifficulty, director.maximumDifficulty, director.isHordeLegal)
			if err != nil {
				return fmt.Errorf("directorInsert[%s:%d]: %w", level.name, ordinal, err)
			}
			directorID++
		}
		for setOrdinal, markerSetName := range level.markerSetNames {
			markerSetKey := hashID(strings.TrimSuffix(markerSetName, ".Markerset"))
			resource, isFound := markerEntry[markerSetKey]
			var contentSourceResourceID any
			var sourceSHA256 any
			var sourceSize any
			var sourceCompression any
			var sourcePayload any
			set := markerSetAsset{weight: 1}
			if isFound {
				markerPayload, readErr := readDecodedResource(ctx, pkg, resource.entry)
				if readErr != nil {
					return fmt.Errorf("setRead[%s]: %w", markerSetName, readErr)
				}
				set, readErr = decodeMarkerSetAsset(markerPayload)
				if readErr != nil {
					return fmt.Errorf("setDecode[%s]: %w", markerSetName, readErr)
				}
				digest := sha256.Sum256(markerPayload)
				compressed, compressionErr := compressContent(markerPayload)
				if compressionErr != nil {
					return fmt.Errorf("setCompress[%s]: %w", markerSetName, compressionErr)
				}
				contentSourceResourceID = resource.ordinal + 1
				sourceSHA256 = fmt.Sprintf("%x", digest)
				sourceSize = len(markerPayload)
				sourceCompression = "zlib"
				sourcePayload = compressed
			}
			_, err = setStatement.ExecContext(ctx, setID, levelID, contentSourceResourceID, setOrdinal,
				markerSetName, set.groupName, set.weight, sourceSHA256, sourceSize, sourceCompression, sourcePayload)
			if err != nil {
				return fmt.Errorf("setInsert[%s]: %w", markerSetName, err)
			}
			for conditionOrdinal, condition := range set.conditions {
				_, err = conditionStatement.ExecContext(ctx, setID, conditionOrdinal, condition)
				if err != nil {
					return fmt.Errorf("conditionInsert[%s:%d]: %w", markerSetName, conditionOrdinal, err)
				}
			}
			for markerOrdinal, marker := range set.markers {
				if strings.EqualFold(marker.nounName, "SpawnPoint_DirectorSpike.Noun") {
					marker.spatialRadius = spikeSpatialRadius * marker.scale
				}
				currentMarkerID := markerID
				var interactableAbility any
				var interactableUseLimit any
				var interactableChallenge any
				var spawnSectionType any
				var isSpikeActive any
				if marker.spawnSectionType != nil {
					spawnSectionType = *marker.spawnSectionType
				}
				if marker.isSpikeActive != nil {
					isSpikeActive = *marker.isSpikeActive
				}
				if marker.interactable != nil {
					if marker.interactable.AbilityName != nil {
						interactableAbility = *marker.interactable.AbilityName
					}
					interactableUseLimit = marker.interactable.UseLimit
					interactableChallenge = marker.interactable.Challenge
				}
				teleporterDefinition, encodeErr := encodeTeleporterDefinition(marker.teleporter)
				if encodeErr != nil {
					return fmt.Errorf("markerTeleporterEncode[%s:%d]: %w", markerSetName, markerOrdinal, encodeErr)
				}
				var isTriggerCreationDeferred *bool
				spawnTriggerDefinition, encodeErr := encodeSpawnTriggerDefinition(marker.spawnTrigger)
				if encodeErr != nil {
					return fmt.Errorf("markerSpawnEncode[%s:%d]: %w", markerSetName, markerOrdinal, encodeErr)
				}
				listenerDefinition, encodeErr := encodeEventListenerDefinition(marker.eventListener)
				if encodeErr != nil {
					return fmt.Errorf("markerListenerEncode[%s:%d]: %w", markerSetName, markerOrdinal, encodeErr)
				}
				interactableDefinition, encodeErr := encodeInteractableDefinition(marker.interactable)
				if encodeErr != nil {
					return fmt.Errorf("markerInteractableEncode[%s:%d]: %w", markerSetName, markerOrdinal, encodeErr)
				}
				combatantDefinition, encodeErr := encodeCombatantDefinition(marker.combatant)
				if encodeErr != nil {
					return fmt.Errorf("markerCombatantEncode[%s:%d]: %w", markerSetName, markerOrdinal, encodeErr)
				}
				if marker.teleporter != nil {
					isTriggerCreationDeferred = &marker.teleporter.IsTriggerCreationDeferred
				}
				_, err = markerStatement.ExecContext(ctx, markerID, setID, markerOrdinal, int64(marker.markerID),
					marker.markerName, marker.nounName, marker.positionX, marker.positionY, marker.positionZ,
					marker.rotationX, marker.rotationY, marker.rotationZ, marker.scale,
					marker.dimensionX, marker.dimensionY, marker.dimensionZ, marker.isVisible,
					marker.isCollisionEnabled, "0x0", int64(marker.targetMarkerID),
					marker.teleporterTriggerRadius, interactableAbility,
					interactableUseLimit, interactableChallenge, spawnSectionType, isSpikeActive,
					marker.spatialRadius, marker.exclusionRadius, teleporterDefinition, isTriggerCreationDeferred,
					spawnTriggerDefinition, listenerDefinition, interactableDefinition, combatantDefinition)
				if err != nil {
					return fmt.Errorf("markerInsert[%s:%d]: %w", markerSetName, markerOrdinal, err)
				}
				var markerEvents []decodedMarkerEvent
				if marker.eventListener != nil {
					markerEvents = decodeListenerEvents(marker.eventListener)
				} else if marker.interactable == nil && marker.combatant == nil && marker.spawnTrigger == nil {
					markerEvents = decodeMarkerEvents(marker.componentStrings, marker.triggerProperties, callbackChunkIDs)
				}
				if marker.spawnTrigger != nil {
					spawnEvents, decodeErr := decodeSpawnTriggerEvents(marker.spawnTrigger)
					if decodeErr != nil {
						return fmt.Errorf("spawnEventDecode[%s:%d]: %w", markerSetName, markerOrdinal, decodeErr)
					}
					markerEvents = append(markerEvents, spawnEvents...)
				}
				for eventOrdinal, event := range markerEvents {
					componentName := "SharedComponentData"
					if event.kind == "listener" {
						componentName = "EventListenerDef"
					}
					_, err = eventStatement.ExecContext(ctx, eventID, currentMarkerID, eventOrdinal,
						componentName, event.kind, event.slot, event.eventName, event.callbackName,
						event.triggerRadius, event.isTriggerOnceOnly, event.isServerOnly,
						int64(event.eventHash), int64(event.nativeCallbackHash), event.luaCallbackName)
					if err != nil {
						return fmt.Errorf("eventInsert[%s:%d:%d]: %w", markerSetName, markerOrdinal, eventOrdinal, err)
					}
					eventID++
				}
				markerID++
			}
			setID++
		}
	}
	for name, statement := range map[string]*sql.Stmt{
		"condition": conditionStatement,
		"director":  directorStatement,
		"event":     eventStatement,
		"marker":    markerStatement,
		"set":       setStatement,
		"alias":     aliasStatement,
		"level":     levelStatement,
	} {
		err = statement.Close()
		if err != nil {
			return fmt.Errorf("%sClose: %w", name, err)
		}
	}
	return nil
}

func decodeLevelAsset(ordinal int, entry dbpf.Entry, payload []byte) (levelAsset, error) {
	if len(payload) < 0x84 {
		return levelAsset{}, fmt.Errorf("payloadSize: %d", len(payload))
	}
	fields := scanCStringFields(payload)
	markerSetNames := make([]string, 0, 32)
	for _, field := range fields {
		if strings.HasSuffix(strings.ToLower(field.text), ".markerset") {
			markerSetNames = append(markerSetNames, field.text)
		}
	}
	name := resolveLevelName(uint32(entry.Instance), markerSetNames, fields)
	level := levelAsset{
		ordinal: ordinal, entry: entry, payload: payload, name: name, markerSetNames: markerSetNames,
		primaryType:   binary.LittleEndian.Uint32(payload[0x74:0x78]),
		secondaryType: binary.LittleEndian.Uint32(payload[0x78:0x7c]),
		tertiaryType:  binary.LittleEndian.Uint32(payload[0x7c:0x80]),
		// The client calls the fourth science field "quadernaryType" (+128).
		quaternaryType: binary.LittleEndian.Uint32(payload[0x80:0x84]),
	}
	camera, cameraErr := decodeLevelCamera(payload)
	if cameraErr != nil {
		return levelAsset{}, fmt.Errorf("cameraDecode: %w", cameraErr)
	}
	if camera != nil {
		level.cameraPitch = &camera.Pitch
		level.cameraYaw = &camera.Yaw
		level.cameraDistance = &camera.Distance
	}
	for _, field := range fields {
		lowerText := strings.ToLower(field.text)
		switch {
		case strings.HasPrefix(lowerText, "music_"):
			level.music = field.text
		case strings.HasSuffix(lowerText, ".bfx"):
			level.navMesh = field.text
		case strings.HasSuffix(lowerText, ".bin"):
			level.physicsMesh = field.text
		case strings.HasSuffix(lowerText, "renderingconfig"):
			level.renderingConfig = field.text
		case strings.HasSuffix(lowerText, ".levelconfig"):
			level.planetConfig = field.text
		}
	}
	entries, decodeErr := decodeLevelDirectorEntries(payload)
	if decodeErr != nil {
		return levelAsset{}, fmt.Errorf("directorConfig: %w", decodeErr)
	}
	level.directorEntries = entries
	return level, nil
}

func decodeLevelDirectorEntries(payload []byte) ([]levelDirectorAsset, error) {
	cursor, err := levelConfigurationCursor(payload)
	if err != nil {
		return nil, fmt.Errorf("configCursor: %w", err)
	}
	entries := make([]levelDirectorAsset, 0)
	for configIndex, offset := range []int{88, 92} {
		if binary.LittleEndian.Uint32(payload[offset:]) == 0 {
			continue
		}
		configEntries, configEnd, decodeErr := decodeLevelDirectorConfig(
			payload, cursor.offset, len(payload), configIndex,
		)
		if decodeErr != nil {
			return nil, fmt.Errorf("configDecode[%d]: %w", configIndex, decodeErr)
		}
		entries = append(entries, configEntries...)
		cursor.offset = configEnd
	}
	return entries, nil
}

func decodeLevelDirectorConfig(
	payload []byte, cursor int, configEnd int, configIndex int,
) ([]levelDirectorAsset, int, error) {
	roles := [...]string{"minion", "special", "boss", "agent", "captain"}
	configNames := [...]string{"levelConfig", "firstTimeConfig"}
	if cursor+40 > configEnd {
		return nil, cursor, fmt.Errorf("configHeader[%s]: truncated", configNames[configIndex])
	}
	counts := [5]int{}
	for roleIndex := range roles {
		pointer := binary.LittleEndian.Uint32(payload[cursor+roleIndex*4:])
		count := binary.LittleEndian.Uint32(payload[cursor+20+roleIndex*4:])
		if count > 100 || (pointer == 0) != (count == 0) ||
			(pointer != 0 && (pointer < 0x03000000 || pointer >= 0x09000000)) {
			return nil, cursor, fmt.Errorf("configHeader[%s:%s]: invalid", configNames[configIndex], roles[roleIndex])
		}
		counts[roleIndex] = int(count)
	}
	cursor += 40
	entries := make([]levelDirectorAsset, 0)
	for roleIndex, role := range roles {
		count := counts[roleIndex]
		if count > (configEnd-cursor)/16 {
			return nil, cursor, fmt.Errorf("configRecords[%s:%s]: truncated", configNames[configIndex], role)
		}
		recordOffset := cursor
		cursor += count * 16
		for entryIndex := range count {
			offset := recordOffset + entryIndex*16
			pointer := binary.LittleEndian.Uint32(payload[offset:])
			minimum := binary.LittleEndian.Uint32(payload[offset+4:])
			maximum := binary.LittleEndian.Uint32(payload[offset+8:])
			isHordeLegal := binary.LittleEndian.Uint32(payload[offset+12:])
			if pointer < 0x03000000 || pointer >= 0x09000000 ||
				minimum > maximum || maximum > 1000 || isHordeLegal > 1 {
				return nil, cursor, fmt.Errorf("configRecord[%s:%s:%d]: invalid", configNames[configIndex], role, entryIndex)
			}
			if cursor >= configEnd {
				return nil, cursor, fmt.Errorf("configNoun[%s:%s:%d]: missing", configNames[configIndex], role, entryIndex)
			}
			end := bytes.IndexByte(payload[cursor:configEnd], 0)
			if end <= 0 {
				return nil, cursor, fmt.Errorf("configNoun[%s:%s:%d]: missing", configNames[configIndex], role, entryIndex)
			}
			nounName := string(payload[cursor : cursor+end])
			if !strings.HasSuffix(strings.ToLower(nounName), ".noun") || !isAuthoredIdentifier(nounName) {
				return nil, cursor, fmt.Errorf("configNoun[%s:%s:%d]: %q", configNames[configIndex], role, entryIndex, nounName)
			}
			cursor += end + 1
			entries = append(entries, levelDirectorAsset{
				configurationOrdinal:      configIndex*len(roles) + roleIndex,
				configurationEntryOrdinal: entryIndex,
				configurationName:         configNames[configIndex], configKind: role,
				nounName: nounName, minimumDifficulty: minimum, maximumDifficulty: maximum,
				isHordeLegal: isHordeLegal == 1,
			})
		}
	}
	return entries, cursor, nil
}

func insertExternalConfigs(ctx context.Context, transaction *sql.Tx, pkg *dbpf.Reader) error {
	// Instance hashes use filename stems; the resource type carries the suffix.
	// Planets is a valid, empty configuration and still gets a parent row.
	// TNX-173 is EnemyPortal's default (ordinal 4677, instance 0x8e423355).
	configNames := []string{
		"Zelem", "Verdanth", "Sentios", "Nocturna", "Cryos", "Scaldron",
		"Bio", "Chrono", "Cyber", "Necro", "Plasma", "Generic", "Planets",
		"TNX-173",
	}
	configNamesByInstance := make(map[uint32]string, len(configNames))
	for _, configName := range configNames {
		configNamesByInstance[hashID(configName)] = configName + ".LevelConfig"
	}
	configStatement, err := transaction.PrepareContext(ctx, `
		INSERT INTO external_config (id, content_source_resource_id, name) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("externalPrepare: %w", err)
	}
	defer configStatement.Close()
	entryStatement, err := transaction.PrepareContext(ctx, `
		INSERT INTO external_config_entry
		(external_config_id, configuration_ordinal, configuration_entry_ordinal,
		 config_kind, noun_name, minimum_difficulty, maximum_difficulty, is_horde_legal)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("externalEntryPrepare: %w", err)
	}
	defer entryStatement.Close()
	for ordinal, entry := range pkg.Entries {
		if entry.Type != levelConfigAssetType {
			continue
		}
		configName, isRequired := configNamesByInstance[uint32(entry.Instance)]
		if !isRequired {
			continue
		}
		payload, readErr := readDecodedResource(ctx, pkg, entry)
		if readErr != nil {
			return fmt.Errorf("externalRead[%s]: %w", configName, readErr)
		}
		entries, cursor, decodeErr := decodeLevelDirectorConfig(payload, 0, len(payload), 0)
		if decodeErr != nil {
			return fmt.Errorf("externalDecode[%s]: %w", configName, decodeErr)
		}
		if cursor != len(payload) {
			return fmt.Errorf("externalSize[%s]: decoded %d of %d", configName, cursor, len(payload))
		}
		configID := ordinal + 1
		_, err = configStatement.ExecContext(ctx, configID, configID, configName)
		if err != nil {
			return fmt.Errorf("externalInsert[%s]: %w", configName, err)
		}
		for _, director := range entries {
			_, err = entryStatement.ExecContext(ctx, configID, director.configurationOrdinal,
				director.configurationEntryOrdinal, director.configKind, director.nounName,
				director.minimumDifficulty, director.maximumDifficulty, director.isHordeLegal)
			if err != nil {
				return fmt.Errorf("externalEntryInsert[%s]: %w", configName, err)
			}
		}
		delete(configNamesByInstance, uint32(entry.Instance))
	}
	if len(configNamesByInstance) != 0 {
		return fmt.Errorf("externalMissing: %d configurations", len(configNamesByInstance))
	}
	err = entryStatement.Close()
	if err != nil {
		return fmt.Errorf("externalEntryClose: %w", err)
	}
	err = configStatement.Close()
	if err != nil {
		return fmt.Errorf("externalClose: %w", err)
	}
	return nil
}

func decodeMarkerSetAsset(payload []byte) (markerSetAsset, error) {
	if len(payload) < markerFixedOffset {
		return markerSetAsset{}, fmt.Errorf("payloadSize: %d", len(payload))
	}
	fields := scanCStringFields(payload)
	pairs := markerStringPairs(fields)
	markerCount := int(binary.LittleEndian.Uint32(payload[4:8]))
	if markerCount != len(pairs) {
		return markerSetAsset{}, fmt.Errorf("markerCount: header %d, strings %d", markerCount, len(pairs))
	}
	if markerFixedOffset+len(pairs)*markerFixedSize > len(payload) {
		return markerSetAsset{}, fmt.Errorf("markerBounds: %d markers in %d bytes", len(pairs), len(payload))
	}
	conditions, err := decodeMarkerSetConditions(payload)
	if err != nil {
		return markerSetAsset{}, fmt.Errorf("conditions: %w", err)
	}
	set := markerSetAsset{weight: math.Float32frombits(binary.LittleEndian.Uint32(payload[0x18:0x1c])), conditions: conditions}
	if len(fields) > 0 && hashID(fields[len(fields)-1].text) == binary.LittleEndian.Uint32(payload[8:12]) {
		set.groupName = fields[len(fields)-1].text
	}
	for index, pair := range pairs {
		base := markerFixedOffset + index*markerFixedSize
		markerEnd := len(payload)
		if index+1 < len(pairs) {
			markerEnd = fields[pairs[index+1].nameIndex].offset
		}
		definition, componentErr := decodeMarkerComponents(payload, base, fields[pair.nameIndex].offset, markerEnd)
		if componentErr != nil {
			return markerSetAsset{}, fmt.Errorf("markerComponents[%d]: %w", index, componentErr)
		}
		spawnTrigger := definition.spawn
		var exclusionRadius *float32
		if spawnTrigger != nil && spawnTrigger.TriggerVolume != nil &&
			(strings.EqualFold(fields[pair.nounIndex].text, "SpawnPoint_HordeTrigger.Noun") ||
				strings.EqualFold(fields[pair.nounIndex].text, "SpawnPoint_DirectorBoss.Noun")) {
			radius, radiusErr := triggerSpatialRadius(spawnTrigger.TriggerVolume)
			if radiusErr != nil {
				return markerSetAsset{}, fmt.Errorf("spawnExclusion[%d]: %w", index, radiusErr)
			}
			exclusionRadius = &radius
		}
		componentEnd := len(fields)
		if index+1 < len(pairs) {
			componentEnd = pairs[index+1].nameIndex
		}
		if index+1 == len(pairs) && set.groupName != "" && componentEnd > pair.nounIndex+1 {
			componentEnd--
		}
		var spawnSectionType *uint32
		var isSpikeActive *bool
		if strings.EqualFold(fields[pair.nounIndex].text, "SpawnPoint_DirectorSpike.Noun") ||
			strings.EqualFold(fields[pair.nounIndex].text, "SpawnPoint_DirectorWanderer.Noun") {
			sectionOffset := fields[pair.nounIndex].offset + len(fields[pair.nounIndex].text) + 1
			if sectionOffset+8 > len(payload) {
				return markerSetAsset{}, fmt.Errorf("spawnPointBounds[%d]: truncated", index)
			}
			section := binary.LittleEndian.Uint32(payload[sectionOffset:])
			active := binary.LittleEndian.Uint32(payload[sectionOffset+4:])
			if section > 3 || active > 1 {
				return markerSetAsset{}, fmt.Errorf("spawnPointDef[%d]: %d/%d", index, section, active)
			}
			spawnSectionType = &section
			isActive := active == 1
			isSpikeActive = &isActive
		}
		components := make([]string, 0, componentEnd-pair.nounIndex-1)
		triggerProperties := make(map[string]markerTriggerProperty)
		for _, field := range fields[pair.nounIndex+1 : componentEnd] {
			if strings.EqualFold(field.text, "none") {
				continue
			}
			components = append(components, field.text)
			if spawnTrigger == nil && isMarkerTriggerCallback(field.text) && field.offset >= 59 {
				triggerProperties[field.text] = markerTriggerProperty{
					radius:            readFloat32(payload, field.offset-28),
					isTriggerOnceOnly: payload[field.offset-59] != 0,
					isServerOnly:      payload[field.offset-16] != 0,
				}
			}
		}
		teleporter := definition.teleporter
		targetMarkerID := uint32(0)
		if teleporter != nil {
			targetMarkerID = teleporter.DestinationMarkerID
		}
		set.markers = append(set.markers, markerAsset{
			markerID:           binary.LittleEndian.Uint32(payload[base+4 : base+8]),
			markerName:         fields[pair.nameIndex].text,
			nounName:           fields[pair.nounIndex].text,
			positionX:          readFloat32(payload, base+0x1c),
			positionY:          readFloat32(payload, base+0x20),
			positionZ:          readFloat32(payload, base+0x24),
			rotationX:          readFloat32(payload, base+0x28),
			rotationY:          readFloat32(payload, base+0x2c),
			rotationZ:          readFloat32(payload, base+0x30),
			scale:              readFloat32(payload, base+0x34),
			dimensionX:         readFloat32(payload, base+0x38),
			dimensionY:         readFloat32(payload, base+0x3c),
			dimensionZ:         readFloat32(payload, base+0x40),
			isVisible:          payload[base+0x44] != 0,
			isCollisionEnabled: binary.LittleEndian.Uint32(payload[base+0x48:base+0x4c]) != 0,
			targetMarkerID:     targetMarkerID,
			teleporter:         teleporter,
			spawnTrigger:       spawnTrigger,
			eventListener:      definition.listener,
			teleporterTriggerRadius: decodeTeleporterTriggerRadius(
				payload, fields[pair.nounIndex+1:componentEnd],
			),
			componentStrings:  components,
			triggerProperties: triggerProperties,
			interactable:      definition.interactable,
			combatant:         definition.combatant,
			spawnSectionType:  spawnSectionType,
			isSpikeActive:     isSpikeActive,
			exclusionRadius:   exclusionRadius,
		})
	}
	return set, nil
}

func decodeMarkerSetConditions(payload []byte) ([]uint32, error) {
	count := int(binary.LittleEndian.Uint32(payload[0x24:0x28]))
	if count == 0 {
		return nil, nil
	}
	if count > 16 || len(payload)-markerFixedOffset < count*4 {
		return nil, fmt.Errorf("conditionCount: %d", count)
	}
	var matches []uint32
	matchCount := 0
	for offset := markerFixedOffset; offset+count*4 <= len(payload); offset++ {
		conditions := make([]uint32, 0, count)
		for index := range count {
			condition := binary.LittleEndian.Uint32(payload[offset+index*4:])
			if condition > 1 {
				break
			}
			conditions = append(conditions, condition)
		}
		if len(conditions) != count {
			continue
		}
		matches = conditions
		matchCount++
	}
	if matchCount != 1 {
		return nil, fmt.Errorf("conditionMatchCount: %d", matchCount)
	}
	return matches, nil
}

func decodeTeleporterTriggerRadius(payload []byte, componentFields []stringField) float32 {
	for _, field := range componentFields {
		isTeleporterEnter := strings.EqualFold(field.text, "Teleporter_OnEnter") ||
			strings.EqualFold(field.text, "TunnelTeleporter_OnEnter")
		if !isTeleporterEnter || field.offset < 40 {
			continue
		}
		radius := readFloat32(payload, field.offset-40)
		if radius > 0 && radius <= 100 {
			return radius
		}
	}
	return 0
}

func markerStringPairs(fields []stringField) []markerPair {
	pairs := make([]markerPair, 0, 64)
	for index := 0; index < len(fields); index++ {
		nameIndex := index
		markerName := fields[index].text
		nounIndex := index
		isAdjacentNoun := index+1 < len(fields) &&
			fields[index+1].offset == fields[index].offset+len(fields[index].text)+1 &&
			strings.HasSuffix(strings.ToLower(fields[index+1].text), ".noun")
		// A component binding can immediately precede a marker with a noun-
		// shaped name. Its actual noun reference may differ from that name.
		// Prefer the complete name/noun pair over binding/name.
		isFollowingNounPair := isAdjacentNoun && index+2 < len(fields) &&
			fields[index+2].offset == fields[index+1].offset+len(fields[index+1].text)+1 &&
			strings.HasSuffix(strings.ToLower(fields[index+2].text), ".noun")
		if !isMarkerName(markerName) && isFollowingNounPair {
			continue
		}
		// Marker names are arbitrary authored labels, such as "Teleporter".
		// Both reflected references occupy consecutive strings in the tail.
		if !isAdjacentNoun && !isMarkerName(markerName) {
			continue
		}
		if isAdjacentNoun {
			nounIndex = index + 1
			index++
		} else if !strings.HasSuffix(strings.ToLower(markerName), ".noun") {
			continue
		}
		pairs = append(pairs, markerPair{nameIndex: nameIndex, nounIndex: nounIndex})
	}
	return pairs
}

func isMarkerName(name string) bool {
	lowerName := strings.ToLower(name)
	nounIndex := strings.LastIndex(lowerName, ".noun")
	if nounIndex < 1 {
		return false
	}
	suffix := lowerName[nounIndex+len(".noun"):]
	if suffix == "" {
		return true
	}
	if len(suffix) < 2 || suffix[0] != '-' {
		return false
	}
	for _, character := range suffix[1:] {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func markerStem(markerName string) string {
	stem := markerName
	if nounIndex := strings.Index(strings.ToLower(stem), ".noun"); nounIndex >= 0 {
		stem = stem[:nounIndex]
	}
	separator := strings.LastIndex(stem, "-")
	if separator < 0 || separator == len(stem)-1 {
		return stem
	}
	for _, character := range stem[separator+1:] {
		if character < '0' || character > '9' {
			return stem
		}
	}
	return stem[:separator]
}

func resolveLevelName(instance uint32, markerSetNames []string, fields []stringField) string {
	for _, field := range fields {
		lowerText := strings.ToLower(field.text)
		if !strings.HasSuffix(lowerText, ".bfx") || !strings.Contains(field.text, "!") {
			continue
		}
		candidate := strings.SplitN(field.text, "!", 2)[0]
		if hashID(candidate) == instance {
			return candidate
		}
	}
	for _, markerSetName := range markerSetNames {
		candidate := strings.TrimSuffix(markerSetName, ".Markerset")
		for candidate != "" {
			if hashID(candidate) == instance {
				return candidate
			}
			separator := strings.LastIndex(candidate, "_")
			if separator < 0 {
				break
			}
			candidate = candidate[:separator]
		}
	}
	return fmt.Sprintf("0x%08X", instance)
}

type decodedMarkerEvent struct {
	eventHash          uint32
	nativeCallbackHash uint32
	luaCallbackName    *string
	kind               string
	slot               string
	eventName          string
	callbackName       string
	triggerRadius      float32
	isTriggerOnceOnly  bool
	isServerOnly       bool
}

func decodeMarkerEvents(
	componentStrings []string, triggerProperties map[string]markerTriggerProperty,
	callbackChunkIDs map[string][]int64,
) []decodedMarkerEvent {
	filtered := make([]string, 0, len(componentStrings))
	for _, component := range componentStrings {
		lowerComponent := strings.ToLower(component)
		if strings.HasPrefix(lowerComponent, "boss_") || strings.Contains(component, ",") ||
			strings.HasSuffix(lowerComponent, ".noun") || !isAuthoredIdentifier(component) {
			continue
		}
		filtered = append(filtered, component)
	}
	events := make([]decodedMarkerEvent, 0, len(filtered))
	used := make([]bool, len(filtered))
	for index, component := range filtered {
		_, _, isLocator := parseLuaLocator(component)
		if isLocator {
			property := triggerProperties[component]
			events = append(events, decodedMarkerEvent{
				kind: "triggerVolume", slot: "luaCallbackOnEnter", callbackName: component,
				triggerRadius: property.radius, isTriggerOnceOnly: property.isTriggerOnceOnly,
				isServerOnly: property.isServerOnly,
			})
			used[index] = true
			continue
		}
		property, isNativeTrigger := triggerProperties[component]
		if !isNativeTrigger || index+1 >= len(filtered) || isLikelyCallback(filtered[index+1], callbackChunkIDs) {
			continue
		}
		events = append(events, decodedMarkerEvent{
			kind: "triggerVolume", slot: "callbackOnEnter", eventName: filtered[index+1], callbackName: component,
			triggerRadius: property.radius, isTriggerOnceOnly: property.isTriggerOnceOnly,
			isServerOnly: property.isServerOnly,
		})
		used[index] = true
		used[index+1] = true
	}
	for index := 0; index+1 < len(filtered); index++ {
		if used[index] || used[index+1] {
			continue
		}
		left := filtered[index]
		right := filtered[index+1]
		isLeftCallback := isLikelyCallback(left, callbackChunkIDs)
		isRightCallback := isLikelyCallback(right, callbackChunkIDs)
		if isLeftCallback == isRightCallback {
			continue
		}
		event := decodedMarkerEvent{kind: "listener_or_trigger", slot: "unknown"}
		if isLeftCallback {
			event.callbackName = left
			event.eventName = right
		} else {
			event.eventName = left
			event.callbackName = right
		}
		events = append(events, event)
		used[index] = true
		used[index+1] = true
		index++
	}
	for index, component := range filtered {
		if used[index] {
			continue
		}
		if !isLikelyCallback(component, callbackChunkIDs) {
			continue
		}
		events = append(events, decodedMarkerEvent{
			kind: "callback", slot: "unknown", callbackName: component,
		})
	}
	return events
}

func isMarkerTriggerCallback(callbackName string) bool {
	_, _, isLocator := parseLuaLocator(callbackName)
	return isLocator || strings.EqualFold(callbackName, "HordeTrigger_OnEnterPlayer")
}

func parseLuaLocator(locator string) (string, string, bool) {
	if strings.Count(locator, ".") != 1 {
		return "", "", false
	}
	part := strings.SplitN(locator, ".", 2)
	if !isLuaCallbackName(part[0]) || !isLuaCallbackName(part[1]) {
		return "", "", false
	}
	if strings.EqualFold(part[1], "noun") {
		return "", "", false
	}
	return part[0], part[1], true
}

func resolveLuaLocatorChunkIDs(locator string, callbackChunkIDs map[string][]int64) []int64 {
	moduleName, callbackName, isLocator := parseLuaLocator(locator)
	if !isLocator {
		return append([]int64(nil), callbackChunkIDs[locator]...)
	}
	moduleChunk := make(map[int64]struct{}, len(callbackChunkIDs[moduleName]))
	for _, chunkID := range callbackChunkIDs[moduleName] {
		moduleChunk[chunkID] = struct{}{}
	}
	matched := make([]int64, 0, 1)
	seen := make(map[int64]struct{})
	for _, chunkID := range callbackChunkIDs[callbackName] {
		if _, isModuleChunk := moduleChunk[chunkID]; !isModuleChunk {
			continue
		}
		if _, isSeen := seen[chunkID]; isSeen {
			continue
		}
		seen[chunkID] = struct{}{}
		matched = append(matched, chunkID)
	}
	sort.Slice(matched, func(left int, right int) bool { return matched[left] < matched[right] })
	if len(matched) != 1 {
		return nil
	}
	return matched
}

func isLikelyCallback(name string, callbackChunkIDs map[string][]int64) bool {
	if _, isCallback := callbackChunkIDs[name]; isCallback {
		return true
	}
	if strings.Contains(name, " ") || name == "" || name[0] < 'A' || name[0] > 'Z' {
		return false
	}
	return strings.Contains(name, "_") || strings.HasPrefix(name, "Director") ||
		strings.HasPrefix(name, "Interact")
}

func loadLuaCallbackChunkIDs(ctx context.Context, transaction *sql.Tx) (map[string][]int64, error) {
	chunks, err := loadLuaChunkImports(ctx, transaction)
	if err != nil {
		return nil, fmt.Errorf("chunkLoad: %w", err)
	}
	callbacks := make(map[string][]int64)
	for _, chunk := range chunks {
		seen := make(map[string]struct{})
		for _, constant := range chunk.strings {
			if !isLuaCallbackName(constant) {
				continue
			}
			if _, isSeen := seen[constant]; isSeen {
				continue
			}
			seen[constant] = struct{}{}
			callbacks[constant] = append(callbacks[constant], chunk.id)
		}
	}
	return callbacks, nil
}

func isLuaCallbackName(name string) bool {
	if len(name) < 3 || len(name) > 128 || strings.ContainsAny(name, "/\\.! ") {
		return false
	}
	for index, character := range name {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character == '_' || index > 0 && character >= '0' && character <= '9' {
			continue
		}
		return false
	}
	return true
}

func isAuthoredIdentifier(text string) bool {
	if len(text) < 2 || len(text) > 256 {
		return false
	}
	for _, character := range text {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || strings.ContainsRune("_ -.!", character) {
			continue
		}
		return false
	}
	return true
}

func scanCStringFields(payload []byte) []stringField {
	fields := make([]stringField, 0, 128)
	for offset := 0; offset < len(payload); {
		if payload[offset] < 0x20 || payload[offset] > 0x7e {
			offset++
			continue
		}
		start := offset
		for offset < len(payload) && payload[offset] >= 0x20 && payload[offset] <= 0x7e {
			offset++
		}
		if offset >= len(payload) || payload[offset] != 0 || offset-start < 2 {
			continue
		}
		text := string(payload[start:offset])
		if isAuthoredIdentifier(text) || strings.Contains(text, ",") ||
			strings.HasSuffix(strings.ToLower(text), ".markerset") ||
			strings.HasSuffix(strings.ToLower(text), ".noun") {
			fields = append(fields, stringField{offset: start, text: text})
		}
		offset++
	}
	return fields
}

func readDecodedResource(ctx context.Context, pkg *dbpf.Reader, entry dbpf.Entry) ([]byte, error) {
	payload, err := readImportResource(ctx, pkg, entry)
	if err != nil {
		return nil, fmt.Errorf("resourceDecode: %w", err)
	}
	return payload, nil
}

func readFloat32(payload []byte, offset int) float32 {
	result := math.Float32frombits(binary.LittleEndian.Uint32(payload[offset : offset+4]))
	if math.IsNaN(float64(result)) || math.IsInf(float64(result), 0) {
		return 0
	}
	return result
}

func writeLevelScripts(ctx context.Context, transaction *sql.Tx) error {
	callbackChunkIDs, err := loadLuaCallbackChunkIDs(ctx, transaction)
	if err != nil {
		return fmt.Errorf("callbackLoad: %w", err)
	}
	rows, err := transaction.QueryContext(ctx, `
		SELECT level_event.id, level_marker_set.level_id,
		       CASE WHEN level_event.event_kind='listener' THEN level_event.lua_callback_name
		            ELSE level_event.callback_name END AS script_callback_name
		FROM level_event
		JOIN marker ON marker.id=level_event.marker_id
		JOIN level_marker_set ON level_marker_set.id=marker.level_marker_set_id
		WHERE (level_event.event_kind='listener' AND COALESCE(level_event.lua_callback_name, '')<>'')
		   OR (level_event.event_kind<>'listener' AND level_event.callback_name<>'')
		ORDER BY level_event.id`)
	if err != nil {
		return fmt.Errorf("eventQuery: %w", err)
	}
	type eventLink struct {
		eventID      int64
		levelID      int64
		callbackName string
	}
	events := make([]eventLink, 0)
	for rows.Next() {
		var event eventLink
		err = rows.Scan(&event.eventID, &event.levelID, &event.callbackName)
		if err != nil {
			_ = rows.Close()
			return fmt.Errorf("eventScan: %w", err)
		}
		events = append(events, event)
	}
	err = rows.Err()
	if err != nil {
		_ = rows.Close()
		return fmt.Errorf("eventRows: %w", err)
	}
	err = rows.Close()
	if err != nil {
		return fmt.Errorf("eventClose: %w", err)
	}
	statement, err := transaction.PrepareContext(ctx, `
		INSERT INTO level_script (id, level_id, level_event_id, lua_chunk_id, callback_name)
		VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("scriptPrepare: %w", err)
	}
	scriptID := int64(1)
	for _, event := range events {
		for _, chunkID := range resolveLuaLocatorChunkIDs(event.callbackName, callbackChunkIDs) {
			_, err = statement.ExecContext(ctx, scriptID, event.levelID, event.eventID, chunkID, event.callbackName)
			if err != nil {
				_ = statement.Close()
				return fmt.Errorf("scriptInsert[%d]: %w", scriptID, err)
			}
			scriptID++
		}
	}
	err = statement.Close()
	if err != nil {
		return fmt.Errorf("scriptClose: %w", err)
	}
	return nil
}
