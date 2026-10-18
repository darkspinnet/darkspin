package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

// NounNavigation preserves sub_F689D0's immutable noun inputs to sub_9EC530.
// Numeric enums retain their authored bits. Actor transforms, movement state,
// collision enablement and obstacle handles belong to the runtime object.
type NounNavigation struct {
	ResourceID                  int64
	InstanceID                  uint64
	NounType                    uint32
	LifetimeSeconds             float32
	IsFixed                     bool
	MinimumX                    float32
	MinimumY                    float32
	MinimumZ                    float32
	MaximumX                    float32
	MaximumY                    float32
	MaximumZ                    float32
	PresetExtents               uint32
	PhysicsType                 uint32
	IsDynamicWall               bool
	IsDoor                      bool
	IsSwitch                    bool
	IsPressureSwitch            bool
	IsProjectilePresent         *bool
	IsClickToOpen               *bool
	IsClickToClose              *bool
	DoorInitialState            *uint32
	LocomotionTuning            *NounLocomotionTuning
	TriggerVolume               *TriggerVolumeDefinition
	EventListener               *EventListenerDefinition
	Interactable                *InteractableDefinition
	Combatant                   *CombatantDefinition
	CrystalDefinition           *NounCrystalDefinition
	IsCombatantComponentPresent bool
}

func decodeNounNavigation(payload []byte) (NounNavigation, error) {
	if len(payload) < 219 {
		return NounNavigation{}, fmt.Errorf("nounHeaderSize: %d", len(payload))
	}
	noun := NounNavigation{
		NounType: binary.LittleEndian.Uint32(payload), IsFixed: payload[5] != 0,
		LifetimeSeconds: readFloat32(payload, 12),
		MinimumX:        readFloat32(payload, 56), MinimumY: readFloat32(payload, 60),
		MinimumZ: readFloat32(payload, 64), MaximumX: readFloat32(payload, 68),
		MaximumY: readFloat32(payload, 72), MaximumZ: readFloat32(payload, 76),
		PresetExtents:               binary.LittleEndian.Uint32(payload[80:]),
		PhysicsType:                 binary.LittleEndian.Uint32(payload[184:]),
		IsDynamicWall:               payload[209] != 0,
		IsCombatantComponentPresent: payload[218] != 0,
	}
	if !isFinite(noun.LifetimeSeconds) || noun.LifetimeSeconds < 0 {
		return NounNavigation{}, fmt.Errorf("nounLifetime: invalid %g", noun.LifetimeSeconds)
	}
	minimums := [...]float32{noun.MinimumX, noun.MinimumY, noun.MinimumZ}
	maximums := [...]float32{noun.MaximumX, noun.MaximumY, noun.MaximumZ}
	// Some shipped nouns have unordered bounds. This projection preserves
	// their authored coordinates; geometry consumers decide their usability.
	for axis, minimum := range minimums {
		maximum := maximums[axis]
		if !isFinite(minimum) || !isFinite(maximum) {
			return NounNavigation{}, fmt.Errorf("nounBounds[%d]: nonfinite %g..%g", axis, minimum, maximum)
		}
	}
	err := decodeNounTail(payload, &noun)
	if err != nil {
		return NounNavigation{}, fmt.Errorf("nounTail: %w", err)
	}
	return noun, nil
}

func writeNounNavigations(ctx context.Context, transaction *sql.Tx, pkg *dbpf.Reader) error {
	for ordinal, entry := range pkg.Entries {
		if entry.Type != nounAssetType || entry.Group != nounAssetGroup {
			continue
		}
		payload, err := readDecodedResource(ctx, pkg, entry)
		if err != nil {
			return fmt.Errorf("nounNavigationRead[%d]: %w", ordinal, err)
		}
		noun, err := decodeNounNavigation(payload)
		if err != nil {
			return fmt.Errorf("nounNavigationDecode[%d]: %w", ordinal, err)
		}
		var acceleration, deceleration, turnRate *float32
		if noun.LocomotionTuning != nil {
			acceleration = &noun.LocomotionTuning.Acceleration
			deceleration = &noun.LocomotionTuning.Deceleration
			turnRate = &noun.LocomotionTuning.TurnRate
		}
		triggerDefinition, err := encodeTriggerVolumeDefinition(noun.TriggerVolume)
		if err != nil {
			return fmt.Errorf("nounTriggerEncode[%d]: %w", ordinal, err)
		}
		listenerDefinition, err := encodeEventListenerDefinition(noun.EventListener)
		if err != nil {
			return fmt.Errorf("nounListenerEncode[%d]: %w", ordinal, err)
		}
		interactableDefinition, err := encodeInteractableDefinition(noun.Interactable)
		if err != nil {
			return fmt.Errorf("nounInteractableEncode[%d]: %w", ordinal, err)
		}
		combatantDefinition, err := encodeCombatantDefinition(noun.Combatant)
		if err != nil {
			return fmt.Errorf("nounCombatantEncode[%d]: %w", ordinal, err)
		}
		result, err := transaction.ExecContext(ctx, `
			INSERT INTO noun_navigation (content_source_resource_id, noun_type, is_fixed,
			 minimum_x, minimum_y, minimum_z, maximum_x, maximum_y, maximum_z,
			 preset_extents, physics_type, is_dynamic_wall, is_door, is_switch, is_pressure_switch,
			 is_click_to_open, is_click_to_close, door_initial_state, acceleration, deceleration, turn_rate,
			 is_projectile_present, lifetime_seconds, trigger_definition, event_listener_definition,
			 interactable_definition, combatant_definition, is_combatant_component_present)
			SELECT resource.id, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
			FROM content_source_resource AS resource
			JOIN content_source_package AS package ON package.id=resource.content_source_package_id
			WHERE package.package_name=? AND resource.ordinal=?`,
			int64(noun.NounType), noun.IsFixed,
			noun.MinimumX, noun.MinimumY, noun.MinimumZ, noun.MaximumX, noun.MaximumY, noun.MaximumZ,
			int64(noun.PresetExtents), int64(noun.PhysicsType), noun.IsDynamicWall,
			noun.IsDoor, noun.IsSwitch, noun.IsPressureSwitch, noun.IsClickToOpen, noun.IsClickToClose,
			noun.DoorInitialState, acceleration, deceleration, turnRate, noun.IsProjectilePresent, noun.LifetimeSeconds,
			triggerDefinition, listenerDefinition, interactableDefinition, combatantDefinition,
			noun.IsCombatantComponentPresent, lootAssetPackage, ordinal)
		if err != nil {
			return fmt.Errorf("nounNavigationInsert[%d]: %w", ordinal, err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("nounNavigationCount[%d]: %w", ordinal, err)
		}
		if count != 1 {
			return fmt.Errorf("nounNavigationCount[%d]: got %d", ordinal, count)
		}
		if noun.CrystalDefinition != nil {
			crystal := noun.CrystalDefinition
			result, err = transaction.ExecContext(ctx, `
				INSERT INTO noun_crystal_definition
				(content_source_resource_id, modifier_hash, modifier_name, color, rarity)
				SELECT resource.id, ?, ?, ?, ?
				FROM content_source_resource AS resource
				JOIN content_source_package AS package ON package.id=resource.content_source_package_id
				WHERE package.package_name=? AND resource.ordinal=?`,
				int64(crystal.ModifierHash), crystal.ModifierName, int64(crystal.Color), int64(crystal.Rarity),
				lootAssetPackage, ordinal)
			if err != nil {
				return fmt.Errorf("nounCrystalInsert[%d]: %w", ordinal, err)
			}
			count, err = result.RowsAffected()
			if err != nil {
				return fmt.Errorf("nounCrystalCount[%d]: %w", ordinal, err)
			}
			if count != 1 {
				return fmt.Errorf("nounCrystalCount[%d]: got %d", ordinal, count)
			}
		}
	}
	return nil
}

func verifyNounNavigations(ctx context.Context, database *sql.DB) error {
	var sourceCount, coveredCount, projectionCount int
	err := database.QueryRowContext(ctx, `
		SELECT COUNT(*), COUNT(noun.content_source_resource_id),
		       (SELECT COUNT(*) FROM noun_navigation)
		FROM content_source_resource AS resource
		JOIN content_source_package AS package ON package.id=resource.content_source_package_id
		LEFT JOIN noun_navigation AS noun ON noun.content_source_resource_id=resource.id
		WHERE package.package_name=? AND resource.type_id=? AND resource.group_id=?`,
		lootAssetPackage, int64(nounAssetType), int64(nounAssetGroup),
	).Scan(&sourceCount, &coveredCount, &projectionCount)
	if err != nil {
		return fmt.Errorf("nounCoverageQuery: %w", err)
	}
	if sourceCount != coveredCount || sourceCount != projectionCount {
		return fmt.Errorf("nounCoverageCount: source %d covered %d projected %d",
			sourceCount, coveredCount, projectionCount)
	}
	// CrystalTuning lists ordinary drop candidates, not every noun carrying a
	// CrystalDef. Extra authored definitions must remain imported; verify the
	// drop table's references rather than equating the two table sizes.
	err = verifyNounCrystalReferences(ctx, database)
	if err != nil {
		return fmt.Errorf("nounCrystalCoverage: %w", err)
	}
	return nil
}

func verifyNounCrystalReferences(ctx context.Context, database *sql.DB) (resultErr error) {
	rows, err := database.QueryContext(ctx, "SELECT ordinal, noun_reference FROM crystal_definition ORDER BY ordinal")
	if err != nil {
		return fmt.Errorf("crystalReferenceQuery: %w", err)
	}
	defer func() {
		closeErr := rows.Close()
		if closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("crystalReferenceClose: %w", closeErr))
		}
	}()
	references := make([]struct {
		ordinal  int
		nounName string
	}, 0)
	for rows.Next() {
		var ordinal int
		var nounReference string
		err = rows.Scan(&ordinal, &nounReference)
		if err != nil {
			return fmt.Errorf("crystalReferenceScan: %w", err)
		}
		references = append(references, struct {
			ordinal  int
			nounName string
		}{ordinal: ordinal, nounName: nounReference})
	}
	err = rows.Err()
	if err != nil {
		return fmt.Errorf("crystalReferenceRows: %w", err)
	}
	err = rows.Close()
	if err != nil {
		return fmt.Errorf("crystalReferenceRelease: %w", err)
	}
	for _, reference := range references {
		if !strings.HasSuffix(strings.ToLower(reference.nounName), ".noun") {
			return fmt.Errorf("crystalReferenceName[%d]: %q", reference.ordinal, reference.nounName)
		}
		stem := reference.nounName[:len(reference.nounName)-len(".Noun")]
		var count int
		err = database.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM noun_crystal_definition AS crystal
			JOIN content_source_resource AS resource ON resource.id=crystal.content_source_resource_id
			JOIN content_source_package AS package ON package.id=resource.content_source_package_id
			WHERE package.package_name=? AND resource.type_id=? AND resource.group_id=? AND resource.instance_id=?`,
			lootAssetPackage, int64(nounAssetType), int64(nounAssetGroup), int64(hashID(stem))).Scan(&count)
		if err != nil {
			return fmt.Errorf("crystalNounQuery[%d]: %w", reference.ordinal, err)
		}
		if count != 1 {
			return fmt.Errorf("crystalNounCount[%d]: %q got %d, want 1", reference.ordinal, reference.nounName, count)
		}
	}
	return nil
}

// NounNavigationEntries includes unnamed nouns and nouns without an NPC class.
// ResourceID is the storage identity; InstanceID comes from the DBPF source.
func (e *Store) NounNavigationEntries(ctx context.Context) ([]NounNavigation, error) {
	if e == nil || e.database == nil || ctx == nil {
		return nil, errors.New("noun navigation store or context unavailable")
	}
	rows, err := e.database.QueryContext(ctx, `
		SELECT noun.content_source_resource_id, resource.instance_id, noun.noun_type, noun.is_fixed,
		       noun.minimum_x, noun.minimum_y, noun.minimum_z, noun.maximum_x, noun.maximum_y,
		       noun.maximum_z, noun.preset_extents, noun.physics_type, noun.is_dynamic_wall,
		       noun.is_door, noun.is_switch, noun.is_pressure_switch, noun.is_click_to_open,
		       noun.is_click_to_close, noun.door_initial_state, noun.acceleration, noun.deceleration, noun.turn_rate,
		       noun.is_projectile_present, noun.lifetime_seconds, noun.trigger_definition, noun.event_listener_definition,
		       noun.interactable_definition, noun.combatant_definition, noun.is_combatant_component_present,
		       crystal.modifier_hash, crystal.modifier_name, crystal.color, crystal.rarity
		FROM noun_navigation AS noun
		JOIN content_source_resource AS resource ON resource.id=noun.content_source_resource_id
		LEFT JOIN noun_crystal_definition AS crystal ON crystal.content_source_resource_id=noun.content_source_resource_id
		ORDER BY noun.content_source_resource_id`)
	if err != nil {
		return nil, fmt.Errorf("nounNavigationQuery: %w", err)
	}
	nouns := make([]NounNavigation, 0)
	for rows.Next() {
		var noun NounNavigation
		var isClickToOpen, isClickToClose sql.NullBool
		var doorInitialState sql.NullInt64
		var acceleration, deceleration, turnRate sql.NullFloat64
		var triggerDefinition sql.NullString
		var listenerDefinition sql.NullString
		var interactableDefinition, combatantDefinition sql.NullString
		var modifierHash, crystalColor, crystalRarity sql.NullInt64
		var modifierName sql.NullString
		err = rows.Scan(&noun.ResourceID, &noun.InstanceID, &noun.NounType, &noun.IsFixed,
			&noun.MinimumX, &noun.MinimumY, &noun.MinimumZ, &noun.MaximumX, &noun.MaximumY,
			&noun.MaximumZ, &noun.PresetExtents, &noun.PhysicsType, &noun.IsDynamicWall,
			&noun.IsDoor, &noun.IsSwitch, &noun.IsPressureSwitch, &isClickToOpen, &isClickToClose,
			&doorInitialState, &acceleration, &deceleration, &turnRate, &noun.IsProjectilePresent, &noun.LifetimeSeconds,
			&triggerDefinition, &listenerDefinition, &interactableDefinition, &combatantDefinition,
			&noun.IsCombatantComponentPresent, &modifierHash, &modifierName, &crystalColor, &crystalRarity)
		if err != nil {
			closeErr := rows.Close()
			return nil, fmt.Errorf("nounNavigationScan: %w", errors.Join(err, closeErr))
		}
		if isClickToOpen.Valid {
			noun.IsClickToOpen = &isClickToOpen.Bool
		}
		if isClickToClose.Valid {
			noun.IsClickToClose = &isClickToClose.Bool
		}
		if doorInitialState.Valid {
			initialState := uint32(doorInitialState.Int64)
			noun.DoorInitialState = &initialState
		}
		if acceleration.Valid && deceleration.Valid && turnRate.Valid {
			noun.LocomotionTuning = &NounLocomotionTuning{Acceleration: float32(acceleration.Float64),
				Deceleration: float32(deceleration.Float64), TurnRate: float32(turnRate.Float64)}
		}
		if modifierHash.Valid {
			noun.CrystalDefinition = &NounCrystalDefinition{
				ModifierHash: uint32(modifierHash.Int64), Color: uint32(crystalColor.Int64),
				Rarity: uint32(crystalRarity.Int64),
			}
			if modifierName.Valid {
				noun.CrystalDefinition.ModifierName = &modifierName.String
			}
		}
		if triggerDefinition.Valid {
			err = json.Unmarshal([]byte(triggerDefinition.String), &noun.TriggerVolume)
			if err != nil {
				closeErr := rows.Close()
				return nil, fmt.Errorf("nounTriggerDecode: %w", errors.Join(err, closeErr))
			}
		}
		if listenerDefinition.Valid {
			err = json.Unmarshal([]byte(listenerDefinition.String), &noun.EventListener)
			if err != nil {
				closeErr := rows.Close()
				return nil, fmt.Errorf("nounListenerDecode: %w", errors.Join(err, closeErr))
			}
		}
		if interactableDefinition.Valid {
			err = json.Unmarshal([]byte(interactableDefinition.String), &noun.Interactable)
			if err != nil {
				closeErr := rows.Close()
				return nil, fmt.Errorf("nounInteractableDecode: %w", errors.Join(err, closeErr))
			}
		}
		if combatantDefinition.Valid {
			err = json.Unmarshal([]byte(combatantDefinition.String), &noun.Combatant)
			if err != nil {
				closeErr := rows.Close()
				return nil, fmt.Errorf("nounCombatantDecode: %w", errors.Join(err, closeErr))
			}
		}
		nouns = append(nouns, noun)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return nil, fmt.Errorf("nounNavigationRows: %w", errors.Join(err, closeErr))
	}
	if closeErr != nil {
		return nil, fmt.Errorf("nounNavigationClose: %w", closeErr)
	}
	return nouns, nil
}

// NounCrystalDefinitions returns only nouns with an authored CrystalDef.
// ResourceID and InstanceID retain the source identity for consumers.
type NounCrystalEntry struct {
	ResourceID int64
	InstanceID uint64
	NounCrystalDefinition
}

func (e *Store) NounCrystalDefinitions(ctx context.Context) ([]NounCrystalEntry, error) {
	if e == nil || e.database == nil || ctx == nil {
		return nil, errors.New("noun crystal store or context unavailable")
	}
	rows, err := e.database.QueryContext(ctx, `
		SELECT crystal.content_source_resource_id, resource.instance_id,
		       crystal.modifier_hash, crystal.modifier_name, crystal.color, crystal.rarity
		FROM noun_crystal_definition AS crystal
		JOIN content_source_resource AS resource ON resource.id=crystal.content_source_resource_id
		ORDER BY crystal.content_source_resource_id`)
	if err != nil {
		return nil, fmt.Errorf("nounCrystalQuery: %w", err)
	}
	defer rows.Close()
	entries := make([]NounCrystalEntry, 0)
	for rows.Next() {
		var entry NounCrystalEntry
		var modifierName sql.NullString
		err = rows.Scan(&entry.ResourceID, &entry.InstanceID, &entry.ModifierHash,
			&modifierName, &entry.Color, &entry.Rarity)
		if err != nil {
			return nil, fmt.Errorf("nounCrystalScan: %w", err)
		}
		if modifierName.Valid {
			entry.ModifierName = &modifierName.String
		}
		entries = append(entries, entry)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("nounCrystalRows: %w", err)
	}
	return entries, nil
}
