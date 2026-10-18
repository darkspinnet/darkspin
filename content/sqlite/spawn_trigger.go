package sqlite

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
)

type SpawnTriggerDefinition struct {
	TriggerVolume     *TriggerVolumeDefinition
	DeathEvent        *string
	DeathEventHash    uint32
	ChallengeOverride int32
	WaveOverride      int32
}

// SpawnTriggerDef follows the shared prefix and SpawnPointDef descendants.
func decodeOptionalSpawnTrigger(cursor *assetCursor, fieldOffset int) (*SpawnTriggerDefinition, error) {
	spawnBase, err := cursor.optionalBody(fieldOffset, 28)
	if err != nil {
		return nil, fmt.Errorf("spawnBody: %w", err)
	}
	if spawnBase < 0 {
		return nil, nil
	}
	trigger, err := decodeOptionalTriggerVolume(cursor, spawnBase)
	if err != nil {
		return nil, fmt.Errorf("spawnTrigger: %w", err)
	}
	deathEvent, err := cursor.reference(spawnBase + 16)
	if err != nil {
		return nil, fmt.Errorf("spawnDeathEvent: %w", err)
	}
	return &SpawnTriggerDefinition{TriggerVolume: trigger, DeathEvent: deathEvent,
		DeathEventHash:    binary.LittleEndian.Uint32(cursor.payload[spawnBase+4:]),
		ChallengeOverride: int32(binary.LittleEndian.Uint32(cursor.payload[spawnBase+20:])),
		WaveOverride:      int32(binary.LittleEndian.Uint32(cursor.payload[spawnBase+24:]))}, nil
}

func encodeSpawnTriggerDefinition(definition *SpawnTriggerDefinition) (*string, error) {
	if definition == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(definition)
	if err != nil {
		return nil, fmt.Errorf("spawnTriggerMarshal: %w", err)
	}
	text := string(encoded)
	return &text, nil
}

func triggerSpatialRadius(trigger *TriggerVolumeDefinition) (float32, error) {
	var radius float32
	switch trigger.Shape {
	case 0:
		radius = trigger.SphereRadius
	case 1:
		radius = float32(math.Sqrt(float64(trigger.BoxWidth*trigger.BoxWidth + trigger.BoxLength*trigger.BoxLength)))
	case 2:
		radius = trigger.CapsuleRadius
	default:
		return 0, fmt.Errorf("triggerShape: %d", trigger.Shape)
	}
	if !isFinite(radius) || radius < 0 {
		return 0, fmt.Errorf("triggerRadius: invalid %g", radius)
	}
	return radius, nil
}

// Preserve each authored slot rather than pairing printable strings. Native
// callbacks and named events share a slot; Lua bindings remain separate.
func decodeSpawnTriggerEvents(definition *SpawnTriggerDefinition) ([]decodedMarkerEvent, error) {
	trigger := definition.TriggerVolume
	if trigger == nil {
		return nil, nil
	}
	radius, err := triggerSpatialRadius(trigger)
	if err != nil {
		return nil, fmt.Errorf("spawnEventRadius: %w", err)
	}
	names := []*string{trigger.OnEnter, trigger.OnExit, trigger.OnStay}
	events := []*string{nil, nil, nil}
	if trigger.Events != nil {
		events[0], events[1] = trigger.Events.OnEnter, trigger.Events.OnExit
	}
	callbacks := []*string{trigger.LuaCallbackOnEnter, trigger.LuaCallbackOnExit, trigger.LuaCallbackOnStay}
	slots := []string{"OnEnter", "OnExit", "OnStay"}
	bindings := make([]decodedMarkerEvent, 0, 6)
	for index, slot := range slots {
		if names[index] != nil || events[index] != nil {
			binding := decodedMarkerEvent{kind: "triggerVolume", slot: "callback" + slot,
				triggerRadius: radius, isTriggerOnceOnly: trigger.IsTriggerOnceOnly, isServerOnly: trigger.IsServerOnly}
			if names[index] != nil {
				binding.callbackName = *names[index]
			}
			if events[index] != nil {
				binding.eventName = *events[index]
			}
			bindings = append(bindings, binding)
		}
		if callbacks[index] != nil {
			bindings = append(bindings, decodedMarkerEvent{kind: "triggerVolume", slot: "luaCallback" + slot,
				callbackName: *callbacks[index], triggerRadius: radius,
				isTriggerOnceOnly: trigger.IsTriggerOnceOnly, isServerOnly: trigger.IsServerOnly})
		}
	}
	return bindings, nil
}
