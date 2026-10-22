package sqlite

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
)

// AudioTriggerDefinition preserves client-owned audio and its nested trigger.
// Marker overrides and inherited noun definitions are stored separately; this
// projection does not install a second server playback callback.
type AudioTriggerDefinition struct {
	Type                 uint32
	SoundHash            uint32
	SoundName            *string
	Is3D                 bool
	IsRetrigger          bool
	IsHardStop           bool
	IsVoiceover          bool
	VoiceLifetimeSeconds float32
	TriggerVolume        *TriggerVolumeDefinition
}

func decodeOptionalAudioTrigger(cursor *assetCursor, fieldOffset int) (*AudioTriggerDefinition, error) {
	base, err := cursor.optionalBody(fieldOffset, 32)
	if err != nil {
		return nil, fmt.Errorf("audioBody: %w", err)
	}
	if base < 0 {
		return nil, nil
	}
	soundName, err := cursor.reference(base + 16)
	if err != nil {
		return nil, fmt.Errorf("audioSound: %w", err)
	}
	trigger, err := decodeOptionalTriggerVolume(cursor, base+28)
	if err != nil {
		return nil, fmt.Errorf("audioTrigger: %w", err)
	}
	return &AudioTriggerDefinition{
		Type:                 binary.LittleEndian.Uint32(cursor.payload[base:]),
		SoundHash:            binary.LittleEndian.Uint32(cursor.payload[base+4:]),
		SoundName:            soundName,
		Is3D:                 cursor.payload[base+20] != 0,
		IsRetrigger:          cursor.payload[base+21] != 0,
		IsHardStop:           cursor.payload[base+22] != 0,
		IsVoiceover:          cursor.payload[base+23] != 0,
		VoiceLifetimeSeconds: readFloat32(cursor.payload, base+24),
		TriggerVolume:        trigger,
	}, nil
}

func encodeAudioTriggerDefinition(definition *AudioTriggerDefinition) (*string, error) {
	if definition == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(definition)
	if err != nil {
		return nil, fmt.Errorf("audioMarshal: %w", err)
	}
	text := string(encoded)
	return &text, nil
}
