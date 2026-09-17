package raknet103

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
)

func Pursuit(plan zonecompanion.Pursuit) ([][]byte, error) {
	if plan.ObjectID == 0 || plan.TargetObjectID == 0 ||
		plan.DesiredStopDistance <= 0 || plan.TravelDuration <= 0 {
		return nil, errors.New("companion pursuit invalid")
	}
	facingX := plan.TargetPosition.X - plan.Position.X
	facingY := plan.TargetPosition.Y - plan.Position.Y
	facingZ := plan.TargetPosition.Z - plan.Position.Z
	facingLength := float32(math.Sqrt(float64(
		facingX*facingX + facingY*facingY + facingZ*facingZ,
	)))
	if facingLength <= 0 {
		return nil, errors.New("companion pursuit direction invalid")
	}
	goal := raknet.Vector3{
		X: plan.TargetPosition.X, Y: plan.TargetPosition.Y,
		Z: plan.TargetPosition.Z,
	}
	movePacket, err := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{
		ObjectID: plan.ObjectID, GoalFlags: 0x41,
		GoalPosition: goal,
		Facing: raknet.Vector3{
			X: facingX / facingLength, Y: facingY / facingLength,
			Z: facingZ / facingLength,
		},
		AllowedStopDistance: plan.DesiredStopDistance,
		DesiredStopDistance: plan.DesiredStopDistance,
		TargetPosition:      goal,
		TargetObjectID:      plan.TargetObjectID,
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
