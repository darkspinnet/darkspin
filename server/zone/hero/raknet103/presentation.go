package raknet103

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

type SwitchRequest struct {
	PlayerIndex     uint8
	SourceObjectID  uint32
	TargetObjectID  uint32
	CreatureIndex   uint32
	SourceHitPoint  float32
	SourceManaPoint float32
	TargetHitPoint  float32
	TargetManaPoint float32
	SourceBeamName  string
	TargetBeamName  string
	Position        game.Vec3
	Timestamp       uint64
}

func Switch(req SwitchRequest) ([][]byte, error) {
	if req.SourceObjectID == 0 || req.TargetObjectID == 0 ||
		req.SourceBeamName == "" || req.TargetBeamName == "" ||
		!isFinitePosition(req.Position) {
		return nil, errors.New("hero switch invalid")
	}
	position := vector(req.Position)
	messages := []raknet.ApplicationMessage{
		raknet.PositionedEffectMessage{
			Asset:    util.HashID(req.SourceBeamName + ".ServerEventDef"),
			Position: position,
		},
		raknet.SetAnimationStateMessage{
			ObjectID:  req.SourceObjectID,
			State:     util.HashID("character_teleport_out"),
			Timestamp: req.Timestamp,
			Scale:     1,
		},
		raknet.ObjectUpdateMessage{
			ObjectID:  req.SourceObjectID,
			PositionX: req.Position.X,
			PositionY: req.Position.Y,
			PositionZ: req.Position.Z,
		},
		raknet.ObjectTeleportMessage{
			ObjectID: req.TargetObjectID,
			Position: position,
		},
		raknet.ObjectUpdateMessage{
			ObjectID:  req.TargetObjectID,
			PositionX: req.Position.X,
			PositionY: req.Position.Y,
			PositionZ: req.Position.Z,
			IsVisible: true,
		},
		raknet.CombatantDataDeltaMessage{
			ObjectID:           req.SourceObjectID,
			HitPoints:          req.SourceHitPoint,
			IsHitPointChanged:  true,
			ManaPoints:         req.SourceManaPoint,
			IsManaPointChanged: true,
		},
		raknet.CombatantDataDeltaMessage{
			ObjectID:           req.TargetObjectID,
			HitPoints:          req.TargetHitPoint,
			IsHitPointChanged:  true,
			ManaPoints:         req.TargetManaPoint,
			IsManaPointChanged: true,
		},
		raknet.LabsPlayerControlledObjectMessage{
			Slot:     req.PlayerIndex,
			ObjectID: req.TargetObjectID,
		},
		raknet.PlayerCharacterDeployMessage{
			PlayerIndex:   req.PlayerIndex,
			CreatureIndex: req.CreatureIndex,
			ObjectID:      req.TargetObjectID,
		},
	}
	messages = append(messages, ArrivalMessages(
		req.TargetObjectID, req.TargetBeamName, req.Timestamp,
	)...)
	return marshalMessages(messages, "heroSwitch")
}

func BeamIn(
	objectID uint32, beamName string, position game.Vec3, timestamp uint64,
) ([][]byte, error) {
	if objectID == 0 || beamName == "" || !isFinitePosition(position) {
		return nil, errors.New("hero beam in invalid")
	}
	messages := []raknet.ApplicationMessage{
		raknet.ObjectUpdateMessage{
			ObjectID:  objectID,
			PositionX: position.X,
			PositionY: position.Y,
			PositionZ: position.Z,
			IsVisible: true,
		},
	}
	messages = append(messages, ArrivalMessages(objectID, beamName, timestamp)...)
	return marshalMessages(messages, "heroBeamIn")
}

// ArrivalMessages follows DeployModifier (Lua chunk 703): the beam animation
// owns its landing markers and the element effect is bound to the hero. The
// shorter teleporter animation emits a second beam and skips the deploy landing.
func ArrivalMessages(
	objectID uint32, beamName string, timestamp uint64,
) []raknet.ApplicationMessage {
	asset := util.HashID(beamName + ".ServerEventDef")
	return []raknet.ApplicationMessage{
		raknet.SetAnimationStateMessage{
			ObjectID:  objectID,
			State:     util.HashID("character_beam_in"),
			Timestamp: timestamp,
			Scale:     1,
		},
		raknet.ServerEventContractMessage{
			Asset: &asset, ObjectID: &objectID,
		},
	}
}

func BeamOut(
	objectID uint32, beamName string, position game.Vec3, timestamp uint64,
) ([][]byte, error) {
	if objectID == 0 || beamName == "" || !isFinitePosition(position) {
		return nil, errors.New("hero beam out invalid")
	}
	messages := []raknet.ApplicationMessage{
		raknet.PositionedEffectMessage{
			Asset:    util.HashID(beamName + ".ServerEventDef"),
			Position: vector(position),
		},
		raknet.SetAnimationStateMessage{
			ObjectID:  objectID,
			State:     util.HashID("character_teleport_out"),
			Timestamp: timestamp,
			Scale:     1,
		},
		raknet.ObjectUpdateMessage{
			ObjectID:  objectID,
			PositionX: position.X,
			PositionY: position.Y,
			PositionZ: position.Z,
			IsVisible: false,
		},
	}
	return marshalMessages(messages, "heroBeamOut")
}

func marshalMessages(
	messages []raknet.ApplicationMessage, operation string,
) ([][]byte, error) {
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("%sMarshal[%d]: %w", operation, index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func vector(position game.Vec3) raknet.Vector3 {
	return raknet.Vector3{X: position.X, Y: position.Y, Z: position.Z}
}

func isFinitePosition(position game.Vec3) bool {
	return !math.IsNaN(float64(position.X)) &&
		!math.IsInf(float64(position.X), 0) &&
		!math.IsNaN(float64(position.Y)) &&
		!math.IsInf(float64(position.Y), 0) &&
		!math.IsNaN(float64(position.Z)) &&
		!math.IsInf(float64(position.Z), 0)
}
