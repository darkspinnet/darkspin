package sqlite

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
)

type TriggerVolumeEvents struct {
	OnEnter *string
	OnExit  *string
}

// TriggerVolumeDefinition is authored geometry and callback data, not a live
// trigger handle. References remain nullable and associated with this body.
type TriggerVolumeDefinition struct {
	OnEnterHash             uint32
	OnExitHash              uint32
	OnStayHash              uint32
	OnEnter                 *string
	OnExit                  *string
	OnStay                  *string
	Events                  *TriggerVolumeEvents
	IsUsingObjectDimensions bool
	IsKinematic             bool
	Shape                   uint32
	OffsetX                 float32
	OffsetY                 float32
	OffsetZ                 float32
	TimeToActivate          float32
	IsPersistentTimer       bool
	IsTriggerOnceOnly       bool
	IsTriggerIfNotBeaten    bool
	TriggerActivationType   uint32
	LuaCallbackOnEnter      *string
	LuaCallbackOnExit       *string
	LuaCallbackOnStay       *string
	BoxWidth                float32
	BoxLength               float32
	BoxHeight               float32
	SphereRadius            float32
	CapsuleHeight           float32
	CapsuleRadius           float32
	IsServerOnly            bool
}

func decodeOptionalTriggerVolume(cursor *assetCursor, fieldOffset int) (*TriggerVolumeDefinition, error) {
	base, err := cursor.optionalBody(fieldOffset, 136)
	if err != nil {
		return nil, fmt.Errorf("triggerBody: %w", err)
	}
	if base < 0 {
		return nil, nil
	}
	trigger := &TriggerVolumeDefinition{}
	refFields := []**string{&trigger.OnEnter, &trigger.OnExit, &trigger.OnStay}
	for index, offset := range []int{12, 28, 44} {
		reference, referenceErr := cursor.reference(base + offset)
		if referenceErr != nil {
			return nil, fmt.Errorf("triggerReference[%d]: %w", offset, referenceErr)
		}
		*refFields[index] = reference
	}
	eventsBase, err := cursor.optionalBody(base+48, 32)
	if err != nil {
		return nil, fmt.Errorf("triggerEvents: %w", err)
	}
	if eventsBase >= 0 {
		onEnter, referenceErr := cursor.reference(eventsBase + 12)
		if referenceErr != nil {
			return nil, fmt.Errorf("triggerEnterEvent: %w", referenceErr)
		}
		onExit, referenceErr := cursor.reference(eventsBase + 28)
		if referenceErr != nil {
			return nil, fmt.Errorf("triggerExitEvent: %w", referenceErr)
		}
		trigger.Events = &TriggerVolumeEvents{OnEnter: onEnter, OnExit: onExit}
	}
	refFields = []**string{&trigger.LuaCallbackOnEnter, &trigger.LuaCallbackOnExit, &trigger.LuaCallbackOnStay}
	for index, offset := range []int{84, 88, 92} {
		reference, referenceErr := cursor.reference(base + offset)
		if referenceErr != nil {
			return nil, fmt.Errorf("triggerLuaReference[%d]: %w", offset, referenceErr)
		}
		*refFields[index] = reference
	}
	payload := cursor.payload
	trigger.OnEnterHash = binary.LittleEndian.Uint32(payload[base:])
	trigger.OnExitHash = binary.LittleEndian.Uint32(payload[base+16:])
	trigger.OnStayHash = binary.LittleEndian.Uint32(payload[base+32:])
	trigger.IsUsingObjectDimensions, trigger.IsKinematic = payload[base+52] != 0, payload[base+53] != 0
	trigger.Shape = binary.LittleEndian.Uint32(payload[base+56:])
	trigger.OffsetX, trigger.OffsetY, trigger.OffsetZ = readFloat32(payload, base+60),
		readFloat32(payload, base+64), readFloat32(payload, base+68)
	trigger.TimeToActivate = readFloat32(payload, base+72)
	trigger.IsPersistentTimer, trigger.IsTriggerOnceOnly, trigger.IsTriggerIfNotBeaten =
		payload[base+76] != 0, payload[base+77] != 0, payload[base+78] != 0
	trigger.TriggerActivationType = binary.LittleEndian.Uint32(payload[base+80:])
	trigger.BoxWidth, trigger.BoxLength, trigger.BoxHeight = readFloat32(payload, base+96),
		readFloat32(payload, base+100), readFloat32(payload, base+104)
	trigger.SphereRadius, trigger.CapsuleHeight, trigger.CapsuleRadius = readFloat32(payload, base+108),
		readFloat32(payload, base+112), readFloat32(payload, base+116)
	trigger.IsServerOnly = payload[base+120] != 0
	return trigger, nil
}

func encodeTriggerVolumeDefinition(trigger *TriggerVolumeDefinition) (*string, error) {
	if trigger == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(trigger)
	if err != nil {
		return nil, fmt.Errorf("triggerMarshal: %w", err)
	}
	text := string(encoded)
	return &text, nil
}
