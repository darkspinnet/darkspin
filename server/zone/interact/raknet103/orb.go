package raknet103

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

type OrbKind uint8

const (
	OrbHealth OrbKind = iota
	OrbMana
	OrbResurrection
)

type OrbPickupRequest struct {
	PickupObjectID uint32
	ActiveObjectID uint32
	Kind           OrbKind
	Position       raknet.Vector3
	HitPoint       float32
	ManaPoint      float32
	Restored       uint32
	IsFull         bool
}

func OrbPickup(req OrbPickupRequest) ([][]byte, error) {
	if req.PickupObjectID == 0 || req.ActiveObjectID == 0 ||
		(req.Kind != OrbHealth && req.Kind != OrbMana &&
			req.Kind != OrbResurrection) {
		return nil, errors.New("orb pickup invalid")
	}
	eventName := "health_orb_pickup.ServerEventDef"
	if req.Kind == OrbMana {
		eventName = "mana_orb_pickup.ServerEventDef"
	}
	if req.Kind == OrbResurrection {
		eventName = "resurrect_orb_pickup.ServerEventDef"
	}
	if req.IsFull {
		eventName = "health_orb_full.ServerEventDef"
		if req.Kind == OrbMana {
			eventName = "mana_orb_full.ServerEventDef"
		}
		if req.Kind == OrbResurrection {
			eventName = "resurrect_orb_full.ServerEventDef"
		}
	}
	messages := []raknet.ApplicationMessage{raknet.ServerEventMessage{
		Asset: util.HashID(eventName), ObjectID: req.ActiveObjectID,
		Position: req.Position, TextValue: req.Restored,
	}}
	if !req.IsFull && req.Kind != OrbResurrection {
		messages = append(messages, orbResourceDelta(req))
	}
	if !req.IsFull {
		messages = append(messages, raknet.ObjectDeleteMessage{
			ObjectID: []uint32{req.PickupObjectID},
		})
	}
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("orbPickupMarshal[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func orbResourceDelta(req OrbPickupRequest) raknet.CombatantDataDeltaMessage {
	message := raknet.CombatantDataDeltaMessage{ObjectID: req.ActiveObjectID}
	if req.Kind == OrbMana {
		message.ManaPoints = req.ManaPoint
		message.IsManaPointChanged = true
		return message
	}
	message.HitPoints = req.HitPoint
	message.IsHitPointChanged = true
	return message
}
