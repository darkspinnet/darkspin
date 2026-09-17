package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

const (
	levelAssetType     = 0xb9193960
	markerSetAssetType = 0xa11d3144
	markerFixedOffset  = 0x28
	markerFixedSize    = 0xc8
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
	cameraPitch     float32
	cameraYaw       float32
	cameraDistance  float32
	directorEntries []levelDirectorAsset
}

type levelDirectorAsset struct {
	configurationOrdinal      int
	configurationEntryOrdinal int
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
	componentStrings        []string
	triggerProperties       map[string]markerTriggerProperty
	interactable            *markerInteractableProperty
}

type markerInteractableProperty struct {
	ability   string
	useLimit  int32
	challenge int32
}

type markerTriggerProperty struct {
	radius            float32
	isTriggerOnceOnly bool
	isServerOnly      bool
}

type markerSetAsset struct {
	groupName string
	weight    float32
	markers   []markerAsset
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
	pkg, err := dbpf.NewReader(r, fi.Size())
	if err != nil {
		return fmt.Errorf("assetPackage: %w", err)
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
		payload, readErr := readDecodedResource(pkg, entry)
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
	err = insertLevelAssets(ctx, transaction, pkg, levels, markerEntry, callbackChunkIDs)
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
	pkg, err := dbpf.NewReader(r, fi.Size())
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
		decoded, readErr := readDecodedResource(pkg, resource.entry)
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
}, callbackChunkIDs map[string][]int64) error {
	levelStatement, err := transaction.PrepareContext(ctx, `
		INSERT INTO level
		(id, content_source_resource_id, name, package_group_id, music, nav_mesh, physics_mesh,
		 rendering_config, planet_config, primary_type, secondary_type, camera_pitch, camera_yaw,
		 camera_distance, source_sha256, source_size, source_compression, source_payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
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
	markerStatement, err := transaction.PrepareContext(ctx, `
		INSERT INTO marker
		(id, level_marker_set_id, ordinal, marker_id, marker_name, noun_name,
		 position_x, position_y, position_z, rotation_x, rotation_y, rotation_z, scale,
		 dimension_x, dimension_y, dimension_z, is_visible, is_collision_enabled,
		 asset_override_id, target_marker_id, teleporter_trigger_radius, interactable_ability,
		 interactable_use_limit, interactable_challenge)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = setStatement.Close()
		_ = aliasStatement.Close()
		_ = levelStatement.Close()
		return fmt.Errorf("markerPrepare: %w", err)
	}
	eventStatement, err := transaction.PrepareContext(ctx, `
		INSERT INTO level_event
		(id, marker_id, ordinal, component_name, event_kind, event_slot, event_name, callback_name,
		 trigger_radius, is_trigger_once_only, is_server_only)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = markerStatement.Close()
		_ = setStatement.Close()
		_ = aliasStatement.Close()
		_ = levelStatement.Close()
		return fmt.Errorf("eventPrepare: %w", err)
	}
	directorStatement, err := transaction.PrepareContext(ctx, `
		INSERT INTO level_director_entry
		(id, level_id, config_kind, spawn_kind, configuration_ordinal,
		 configuration_entry_ordinal, ordinal, noun_name, minimum_difficulty,
		 maximum_difficulty, is_horde_legal)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
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
			level.renderingConfig, level.planetConfig, level.primaryType, level.secondaryType,
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
			_, err = directorStatement.ExecContext(ctx, directorID, levelID, configKind, "unknown",
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
				markerPayload, readErr := readDecodedResource(pkg, resource.entry)
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
			for markerOrdinal, marker := range set.markers {
				currentMarkerID := markerID
				var interactableAbility any
				var interactableUseLimit any
				var interactableChallenge any
				if marker.interactable != nil {
					interactableAbility = marker.interactable.ability
					interactableUseLimit = marker.interactable.useLimit
					interactableChallenge = marker.interactable.challenge
				}
				_, err = markerStatement.ExecContext(ctx, markerID, setID, markerOrdinal, int64(marker.markerID),
					marker.markerName, marker.nounName, marker.positionX, marker.positionY, marker.positionZ,
					marker.rotationX, marker.rotationY, marker.rotationZ, marker.scale,
					marker.dimensionX, marker.dimensionY, marker.dimensionZ, marker.isVisible,
					marker.isCollisionEnabled, "0x0", int64(marker.targetMarkerID),
					marker.teleporterTriggerRadius, interactableAbility,
					interactableUseLimit, interactableChallenge)
				if err != nil {
					return fmt.Errorf("markerInsert[%s:%d]: %w", markerSetName, markerOrdinal, err)
				}
				markerEvents := decodeMarkerEvents(marker.componentStrings, marker.triggerProperties, callbackChunkIDs)
				for eventOrdinal, event := range markerEvents {
					_, err = eventStatement.ExecContext(ctx, eventID, currentMarkerID, eventOrdinal,
						"SharedComponentData", event.kind, event.slot, event.eventName, event.callbackName,
						event.triggerRadius, event.isTriggerOnceOnly, event.isServerOnly)
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
		"director": directorStatement,
		"event":    eventStatement,
		"marker":   markerStatement,
		"set":      setStatement,
		"alias":    aliasStatement,
		"level":    levelStatement,
	} {
		err = statement.Close()
		if err != nil {
			return fmt.Errorf("%sClose: %w", name, err)
		}
	}
	return nil
}

func decodeLevelAsset(ordinal int, entry dbpf.Entry, payload []byte) (levelAsset, error) {
	if len(payload) < 0x80 {
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
		primaryType:    binary.LittleEndian.Uint32(payload[0x74:0x78]),
		secondaryType:  binary.LittleEndian.Uint32(payload[0x78:0x7c]),
		cameraPitch:    math.Float32frombits(binary.LittleEndian.Uint32(payload[len(payload)-12:])),
		cameraYaw:      math.Float32frombits(binary.LittleEndian.Uint32(payload[len(payload)-8:])),
		cameraDistance: math.Float32frombits(binary.LittleEndian.Uint32(payload[len(payload)-4:])),
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
	level.directorEntries = decodeLevelDirectorEntries(payload, fields)
	assignLevelDirectorKinds(level.name, level.directorEntries)
	return level, nil
}

func assignLevelDirectorKinds(levelName string, entries []levelDirectorAsset) {
	if !strings.EqualFold(levelName, "zelems_1") {
		return
	}
	kindByConfiguration := []string{"minion", "special", "agent", "captain"}
	for index := range entries {
		configurationOrdinal := entries[index].configurationOrdinal
		if configurationOrdinal < 0 || configurationOrdinal >= len(kindByConfiguration) {
			continue
		}
		entries[index].configKind = kindByConfiguration[configurationOrdinal]
	}
}

func decodeLevelDirectorEntries(payload []byte, fields []stringField) []levelDirectorAsset {
	entries := make([]levelDirectorAsset, 0, 16)
	configurationOrdinal := 0
	for fieldIndex := 0; fieldIndex < len(fields); {
		if !isNounField(fields[fieldIndex]) {
			fieldIndex++
			continue
		}
		runEnd := fieldIndex + 1
		for runEnd < len(fields) && isNounField(fields[runEnd]) &&
			fields[runEnd].offset == fields[runEnd-1].offset+len(fields[runEnd-1].text)+1 {
			runEnd++
		}
		nouns := fields[fieldIndex:runEnd]
		recordOffset := nouns[0].offset - len(nouns)*16
		if recordOffset >= 0 {
			records, isValid := decodeLevelDirectorRun(payload, recordOffset, configurationOrdinal, nouns)
			if isValid {
				entries = append(entries, records...)
				configurationOrdinal++
			}
		}
		fieldIndex = runEnd
	}
	return entries
}

func isNounField(field stringField) bool {
	return strings.HasSuffix(strings.ToLower(field.text), ".noun")
}

func decodeLevelDirectorRun(
	payload []byte, recordOffset int, configurationOrdinal int, nouns []stringField,
) ([]levelDirectorAsset, bool) {
	records := make([]levelDirectorAsset, 0, len(nouns))
	for index, noun := range nouns {
		offset := recordOffset + index*16
		if offset < 0 || offset+16 > len(payload) {
			return nil, false
		}
		pointer := binary.LittleEndian.Uint32(payload[offset : offset+4])
		minimumDifficulty := binary.LittleEndian.Uint32(payload[offset+4 : offset+8])
		maximumDifficulty := binary.LittleEndian.Uint32(payload[offset+8 : offset+12])
		isHordeLegal := binary.LittleEndian.Uint32(payload[offset+12 : offset+16])
		if pointer < 0x03000000 || pointer > 0x08000000 || minimumDifficulty > maximumDifficulty ||
			maximumDifficulty > 1000 || isHordeLegal > 1 {
			return nil, false
		}
		records = append(records, levelDirectorAsset{
			configurationOrdinal:      configurationOrdinal,
			configurationEntryOrdinal: index,
			nounName:                  noun.text,
			minimumDifficulty:         minimumDifficulty,
			maximumDifficulty:         maximumDifficulty,
			isHordeLegal:              isHordeLegal == 1,
		})
	}
	return records, true
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
	set := markerSetAsset{weight: math.Float32frombits(binary.LittleEndian.Uint32(payload[0x18:0x1c]))}
	if len(fields) > 0 && strings.EqualFold(fields[len(fields)-1].text, "none") {
		set.groupName = fields[len(fields)-1].text
	}
	for index, pair := range pairs {
		base := markerFixedOffset + index*markerFixedSize
		componentEnd := len(fields)
		if index+1 < len(pairs) {
			componentEnd = pairs[index+1].nameIndex
		}
		components := make([]string, 0, componentEnd-pair.nounIndex-1)
		triggerProperties := make(map[string]markerTriggerProperty)
		for _, field := range fields[pair.nounIndex+1 : componentEnd] {
			if strings.EqualFold(field.text, "none") {
				continue
			}
			components = append(components, field.text)
			if isMarkerTriggerCallback(field.text) && field.offset >= 59 {
				triggerProperties[field.text] = markerTriggerProperty{
					radius:            readFloat32(payload, field.offset-28),
					isTriggerOnceOnly: payload[field.offset-59] != 0,
					isServerOnly:      payload[field.offset-16] != 0,
				}
			}
		}
		interactable := decodeMarkerInteractable(
			payload, fields[pair.nounIndex], fields[pair.nounIndex+1:componentEnd],
		)
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
			targetMarkerID:     decodeTeleporterDestination(payload, fields[pair.nounIndex]),
			teleporterTriggerRadius: decodeTeleporterTriggerRadius(
				payload, fields[pair.nounIndex+1:componentEnd],
			),
			componentStrings:  components,
			triggerProperties: triggerProperties,
			interactable:      interactable,
		})
	}
	return set, nil
}

func decodeTeleporterDestination(payload []byte, noun stringField) uint32 {
	isSupported := strings.EqualFold(noun.text, "Teleporter.Noun") ||
		strings.EqualFold(noun.text, "TunnelTeleporter.Noun") ||
		strings.EqualFold(noun.text, "SecurityTeleporter.Noun") ||
		strings.EqualFold(noun.text, "BossSecurityTeleporter.Noun")
	if !isSupported {
		return 0
	}
	offset := noun.offset + len(noun.text) + 1
	if offset < 0 || offset+4 > len(payload) {
		return 0
	}
	return binary.LittleEndian.Uint32(payload[offset : offset+4])
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

func decodeMarkerInteractable(
	payload []byte, noun stringField, componentFields []stringField,
) *markerInteractableProperty {
	var ability *stringField
	for index := range componentFields {
		if !strings.HasPrefix(componentFields[index].text, "Interact") ||
			!isAuthoredIdentifier(componentFields[index].text) {
			continue
		}
		ability = &componentFields[index]
		break
	}
	if ability == nil {
		return nil
	}
	useOffset := noun.offset + len(noun.text) + 1
	challengeOffset := ability.offset - 4
	if useOffset < 0 || useOffset+4 > len(payload) || challengeOffset < useOffset+4 ||
		challengeOffset+4 > len(payload) {
		return nil
	}
	useLimit := int32(binary.LittleEndian.Uint32(payload[useOffset : useOffset+4]))
	challenge := int32(binary.LittleEndian.Uint32(payload[challengeOffset : challengeOffset+4]))
	if useLimit < -1 || useLimit > 1000 || challenge < 0 || challenge > 1000000 {
		return nil
	}
	return &markerInteractableProperty{
		ability: ability.text, useLimit: useLimit, challenge: challenge,
	}
}

func markerStringPairs(fields []stringField) []markerPair {
	pairs := make([]markerPair, 0, 64)
	for index := 0; index < len(fields); index++ {
		nameIndex := index
		markerName := fields[index].text
		if !isMarkerName(markerName) {
			continue
		}
		nounIndex := index
		isAdjacentNoun := index+1 < len(fields) &&
			fields[index+1].offset == fields[index].offset+len(fields[index].text)+1 &&
			strings.HasSuffix(strings.ToLower(fields[index+1].text), ".noun")
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
	kind              string
	slot              string
	eventName         string
	callbackName      string
	triggerRadius     float32
	isTriggerOnceOnly bool
	isServerOnly      bool
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

func readDecodedResource(pkg *dbpf.Reader, entry dbpf.Entry) ([]byte, error) {
	r, err := pkg.Open(entry)
	if err != nil {
		return nil, fmt.Errorf("resourceOpen: %w", err)
	}
	payload, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("resourceRead: %w", err)
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
		SELECT level_event.id, level_marker_set.level_id, level_event.callback_name
		FROM level_event
		JOIN marker ON marker.id=level_event.marker_id
		JOIN level_marker_set ON level_marker_set.id=marker.level_marker_set_id
		WHERE level_event.callback_name<>''
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
