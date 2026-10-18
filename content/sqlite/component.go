package sqlite

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
)

type InteractableDefinition struct {
	UseLimit          int32
	AbilityHash       uint32
	AbilityName       *string
	StartEventHash    uint32
	StartEventName    *string
	EndEventHash      uint32
	EndEventName      *string
	OptionalEventHash uint32
	OptionalEventName *string
	Challenge         int32
}

type CombatantDefinition struct {
	DeathEventHash uint32
	DeathEventName *string
}

func decodeOptionalInteractable(cursor *assetCursor, fieldOffset int) (*InteractableDefinition, error) {
	base, err := cursor.optionalBody(fieldOffset, 72)
	if err != nil {
		return nil, fmt.Errorf("interactableBody: %w", err)
	}
	if base < 0 {
		return nil, nil
	}
	definition := &InteractableDefinition{
		UseLimit:          int32(binary.LittleEndian.Uint32(cursor.payload[base:])),
		AbilityHash:       binary.LittleEndian.Uint32(cursor.payload[base+4:]),
		StartEventHash:    binary.LittleEndian.Uint32(cursor.payload[base+20:]),
		EndEventHash:      binary.LittleEndian.Uint32(cursor.payload[base+36:]),
		OptionalEventHash: binary.LittleEndian.Uint32(cursor.payload[base+52:]),
		Challenge:         int32(binary.LittleEndian.Uint32(cursor.payload[base+68:])),
	}
	references := []**string{&definition.AbilityName, &definition.StartEventName,
		&definition.EndEventName, &definition.OptionalEventName}
	for index, reference := range references {
		*reference, err = cursor.reference(base + 16 + index*16)
		if err != nil {
			return nil, fmt.Errorf("interactableReference[%d]: %w", index, err)
		}
	}
	return definition, nil
}

func decodeOptionalCombatant(cursor *assetCursor, fieldOffset int) (*CombatantDefinition, error) {
	base, err := cursor.optionalBody(fieldOffset, 16)
	if err != nil {
		return nil, fmt.Errorf("combatantBody: %w", err)
	}
	if base < 0 {
		return nil, nil
	}
	name, err := cursor.reference(base + 12)
	if err != nil {
		return nil, fmt.Errorf("combatantDeathEvent: %w", err)
	}
	return &CombatantDefinition{DeathEventHash: binary.LittleEndian.Uint32(cursor.payload[base:]),
		DeathEventName: name}, nil
}

type sharedComponentDefinition struct {
	teleporter   *TeleporterDefinition
	listener     *EventListenerDefinition
	spawn        *SpawnTriggerDefinition
	interactable *InteractableDefinition
	combatant    *CombatantDefinition
}

// SharedComponentData is inline in markers and nouns. Reflection visits
// spawnPoint before spawnTrigger, then interactable, defaultGfx and combatant.
func decodeSharedComponents(cursor *assetCursor, base int) (sharedComponentDefinition, error) {
	definition := sharedComponentDefinition{}
	var err error
	definition.teleporter, definition.listener, err = decodeSharedComponentPrefix(cursor, base)
	if err != nil {
		return definition, fmt.Errorf("sharedPrefix: %w", err)
	}
	pointBase, err := cursor.optionalBody(base+16, 8)
	if err != nil {
		return definition, fmt.Errorf("sharedSpawnPoint: %w", err)
	}
	_ = pointBase // SpawnPointDef has only fixed scalars.
	definition.spawn, err = decodeOptionalSpawnTrigger(cursor, base+12)
	if err != nil {
		return definition, fmt.Errorf("sharedSpawnTrigger: %w", err)
	}
	definition.interactable, err = decodeOptionalInteractable(cursor, base+20)
	if err != nil {
		return definition, fmt.Errorf("sharedInteractable: %w", err)
	}
	graphicsBase, err := cursor.optionalBody(base+24, 24)
	if err != nil {
		return definition, fmt.Errorf("sharedGraphics: %w", err)
	}
	if graphicsBase >= 0 {
		err = cursor.references(graphicsBase + 12)
		if err != nil {
			return definition, fmt.Errorf("sharedGraphicsReference: %w", err)
		}
	}
	definition.combatant, err = decodeOptionalCombatant(cursor, base+28)
	if err != nil {
		return definition, fmt.Errorf("sharedCombatant: %w", err)
	}
	return definition, nil
}

func encodeInteractableDefinition(definition *InteractableDefinition) (*string, error) {
	if definition == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(definition)
	if err != nil {
		return nil, fmt.Errorf("interactableMarshal: %w", err)
	}
	text := string(encoded)
	return &text, nil
}

func encodeCombatantDefinition(definition *CombatantDefinition) (*string, error) {
	if definition == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(definition)
	if err != nil {
		return nil, fmt.Errorf("combatantMarshal: %w", err)
	}
	text := string(encoded)
	return &text, nil
}
