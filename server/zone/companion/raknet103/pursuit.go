package raknet103

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
)

func Pursuit(plan zonecompanion.Pursuit) ([][]byte, error) {
	if plan.ObjectID == 0 || plan.TargetObjectID == 0 ||
		plan.DesiredStopDistance <= 0 || plan.TravelDuration <= 0 {
		return nil, errors.New("companion pursuit invalid")
	}
	goalPosition := plan.TargetPosition
	goalFlags := uint32(0x41)
	targetID := plan.TargetObjectID
	stopDistance := plan.DesiredStopDistance
	if plan.IsWaypointKnown {
		goalPosition = plan.Waypoint
		goalFlags = 1
		targetID = 0
		stopDistance = 0
	}
	facingX := goalPosition.X - plan.Position.X
	facingY := goalPosition.Y - plan.Position.Y
	facingZ := goalPosition.Z - plan.Position.Z
	facingLength := float32(math.Sqrt(float64(
		facingX*facingX + facingY*facingY + facingZ*facingZ,
	)))
	if facingLength <= 0 && !plan.IsWaypointKnown {
		return nil, errors.New("companion pursuit direction invalid")
	}
	if facingLength <= 0 {
		goalFlags = 0x20
		facingLength = 1
	}
	goal := raknet.Vector3{
		X: goalPosition.X, Y: goalPosition.Y,
		Z: goalPosition.Z,
	}
	movePacket, err := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{
		ObjectID: plan.ObjectID, GoalFlags: goalFlags,
		GoalPosition: goal,
		Facing: raknet.Vector3{
			X: facingX / facingLength, Y: facingY / facingLength,
			Z: facingZ / facingLength,
		},
		AllowedStopDistance: stopDistance,
		DesiredStopDistance: stopDistance,
		TargetPosition:      goal,
		TargetObjectID:      targetID,
	})
	if err != nil {
		return nil, fmt.Errorf("pursuitMarshal: %w", err)
	}
	correctionPacket, err := raknet.MarshalApplication(
		raknet.LocomotionUnreliableMessage{
			ObjectID: plan.ObjectID, GoalPosition: goal,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("pursuitCorrectionMarshal: %w", err)
	}
	return [][]byte{movePacket, correctionPacket}, nil
}

// PursuitStop corrects a campaign bot's visible position before it selects a
// nearer target. The old server arrival is fenced by the pursuit revision.
func PursuitStop(objectID uint32, position game.Vec3) ([][]byte, error) {
	if objectID == 0 {
		return nil, errors.New("companion pursuit stop invalid")
	}
	positionPacket, err := raknet.MarshalApplication(raknet.ObjectUpdateMessage{
		ObjectID: objectID, PositionX: position.X, PositionY: position.Y,
		PositionZ: position.Z, IsVisible: true,
	})
	if err != nil {
		return nil, fmt.Errorf("pursuitStopPosition: %w", err)
	}
	stopPacket, err := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{
		ObjectID: objectID, GoalFlags: 0x20, GoalPosition: raknet.Vector3(position),
	})
	if err != nil {
		return nil, fmt.Errorf("pursuitStopMove: %w", err)
	}
	return [][]byte{positionPacket, stopPacket}, nil
}
