package sqlite

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
)

type EventListenerDefinition struct {
	Entries []EventListenerEntry
}

type EventListenerEntry struct {
	Ordinal            int
	EventHash          uint32
	EventName          *string
	NativeCallbackHash uint32
	NativeCallbackName *string
	LuaCallbackName    *string
}

func decodeOptionalEventListener(cursor *assetCursor, fieldOffset int) (*EventListenerDefinition, error) {
	base, err := cursor.optionalBody(fieldOffset, 8)
	if err != nil {
		return nil, fmt.Errorf("listenerBody: %w", err)
	}
	if base < 0 {
		return nil, nil
	}
	count := binary.LittleEndian.Uint32(cursor.payload[base+4:])
	start, err := cursor.reserve(count, 40)
	if err != nil {
		return nil, fmt.Errorf("listenerArray: %w", err)
	}
	definition := &EventListenerDefinition{Entries: make([]EventListenerEntry, 0, count)}
	for index := range int(count) {
		record := start + index*40
		entry := EventListenerEntry{Ordinal: index,
			EventHash:          binary.LittleEndian.Uint32(cursor.payload[record:]),
			NativeCallbackHash: binary.LittleEndian.Uint32(cursor.payload[record+16:])}
		for fieldIndex, reference := range []**string{&entry.EventName, &entry.NativeCallbackName, &entry.LuaCallbackName} {
			offsets := [...]int{12, 28, 36}
			*reference, err = cursor.reference(record + offsets[fieldIndex])
			if err != nil {
				return nil, fmt.Errorf("listenerReference[%d:%d]: %w", index, fieldIndex, err)
			}
		}
		// +32 is a process-local callback cache and is never projected.
		definition.Entries = append(definition.Entries, entry)
	}
	return definition, nil
}

// The shared tail visits audio, teleporter and listeners before spawn fields.
// The same traversal applies at marker+156 and noun+252.
func decodeSharedComponentPrefix(cursor *assetCursor, base int) (*TeleporterDefinition, *EventListenerDefinition, error) {
	audioBase, err := cursor.optionalBody(base, 32)
	if err != nil {
		return nil, nil, fmt.Errorf("componentAudio: %w", err)
	}
	if audioBase >= 0 {
		err = cursor.references(audioBase + 16)
		if err != nil {
			return nil, nil, fmt.Errorf("componentAudioSound: %w", err)
		}
		audioTrigger, triggerErr := decodeOptionalTriggerVolume(cursor, audioBase+28)
		if triggerErr != nil {
			return nil, nil, fmt.Errorf("componentAudioTrigger: %w", triggerErr)
		}
		_ = audioTrigger
	}
	var teleporter *TeleporterDefinition
	teleporterBase, err := cursor.optionalBody(base+4, 12)
	if err != nil {
		return nil, nil, fmt.Errorf("componentTeleporter: %w", err)
	}
	if teleporterBase >= 0 {
		trigger, triggerErr := decodeOptionalTriggerVolume(cursor, teleporterBase+4)
		if triggerErr != nil {
			return nil, nil, fmt.Errorf("componentTeleporterTrigger: %w", triggerErr)
		}
		teleporter = &TeleporterDefinition{TriggerVolume: trigger,
			DestinationMarkerID:       binary.LittleEndian.Uint32(cursor.payload[teleporterBase:]),
			IsTriggerCreationDeferred: cursor.payload[teleporterBase+8] != 0}
	}
	listener, err := decodeOptionalEventListener(cursor, base+8)
	if err != nil {
		return nil, nil, fmt.Errorf("componentListener: %w", err)
	}
	return teleporter, listener, nil
}

func decodeMarkerComponents(payload []byte, base, tailStart, tailEnd int) (sharedComponentDefinition, error) {
	definition := sharedComponentDefinition{}
	if base < 0 || base > len(payload)-markerFixedSize || tailStart < 0 ||
		tailStart > tailEnd || tailEnd > len(payload) {
		return definition, fmt.Errorf("componentBounds: invalid")
	}
	cursor := assetCursor{payload: payload[:tailEnd], offset: tailStart}
	err := cursor.references(base, base+8)
	if err != nil {
		return definition, fmt.Errorf("componentMarkerReferences: %w", err)
	}
	definition, err = decodeSharedComponents(&cursor, base+156)
	if err != nil {
		return definition, fmt.Errorf("markerComponents: %w", err)
	}
	return definition, nil
}

func encodeEventListenerDefinition(definition *EventListenerDefinition) (*string, error) {
	if definition == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(definition)
	if err != nil {
		return nil, fmt.Errorf("listenerMarshal: %w", err)
	}
	text := string(encoded)
	return &text, nil
}

func decodeListenerEvents(definition *EventListenerDefinition) []decodedMarkerEvent {
	bindings := make([]decodedMarkerEvent, 0, len(definition.Entries))
	for _, entry := range definition.Entries {
		binding := decodedMarkerEvent{kind: "listener", slot: "listener", eventHash: entry.EventHash,
			nativeCallbackHash: entry.NativeCallbackHash, luaCallbackName: entry.LuaCallbackName}
		if entry.EventName != nil {
			binding.eventName = *entry.EventName
		}
		if entry.NativeCallbackName != nil {
			binding.callbackName = *entry.NativeCallbackName
		}
		bindings = append(bindings, binding)
	}
	return bindings
}
