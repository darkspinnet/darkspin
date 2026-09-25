package raknet103

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
)

func PursuitTransfer(syncStamp uint8, objectID uint32) ([][]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.ActionCommandResponseMessage{
		SyncStamp: syncStamp, ResponseType: raknet.ActionResponsePursuit, ObjectID: objectID,
	})
	if err != nil {
		return nil, fmt.Errorf("pursuitResponse: %w", err)
	}
	return [][]byte{packet}, nil
}

func PursuitStep(objectID uint32, goal game.Vec3) ([]byte, error) {
	if objectID == 0 || !isFinitePosition(goal) {
		return nil, errors.New("invalid pursuit step")
	}
	packet, err := raknet.MarshalApplication(raknet.LocomotionUnreliableMessage{
		ObjectID: objectID,
		GoalPosition: raknet.Vector3{
			X: goal.X,
			Y: goal.Y,
			Z: goal.Z,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("pursuitStep: %w", err)
	}
	return packet, nil
}

func PursuitRedirect(
	objectID uint32, source game.Vec3, targetObjectID uint32,
	target game.Vec3, stopDistance float32,
) ([][]byte, error) {
	if objectID == 0 || targetObjectID == 0 ||
		!isFinitePosition(source) || !isFinitePosition(target) ||
		stopDistance <= 0 || math.IsNaN(float64(stopDistance)) ||
		math.IsInf(float64(stopDistance), 0) {
		return nil, errors.New("invalid pursuit redirect")
	}
	goal := pursuitGoal(source, target, stopDistance)
	facing := pursuitDirection(source, target)
	movePacket, err := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{
		ObjectID: objectID, GoalFlags: 0x41,
		GoalPosition: raknet.Vector3{
			X: goal.X,
			Y: goal.Y,
			Z: goal.Z,
		},
		Facing:              facing,
		DesiredStopDistance: 0.1,
		TargetPosition: raknet.Vector3{
			X: target.X,
			Y: target.Y,
			Z: target.Z,
		},
		TargetObjectID: targetObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("pursuitRedirectMove: %w", err)
	}
	stepPacket, err := PursuitStep(objectID, goal)
	if err != nil {
		return nil, fmt.Errorf("pursuitRedirectStep: %w", err)
	}
	return [][]byte{movePacket, stepPacket}, nil
}

func ChargeMove(
	objectID uint32, source game.Vec3, destination game.Vec3,
	targetObjectID uint32, target game.Vec3,
) ([][]byte, error) {
	if objectID == 0 || !isFinitePosition(source) || !isFinitePosition(destination) ||
		!isFinitePosition(target) {
		return nil, errors.New("invalid charge move")
	}
	facing := pursuitDirection(source, destination)
	goalFlags := uint32(0x01)
	if targetObjectID != 0 {
		goalFlags |= 0x40
	}
	movePacket, err := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{
		ObjectID: objectID, GoalFlags: goalFlags,
		GoalPosition:        vector(destination),
		Facing:              facing,
		DesiredStopDistance: 0.1,
		TargetPosition:      vector(target),
		TargetObjectID:      targetObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("chargeMoveMarshal: %w", err)
	}
	stepPacket, err := PursuitStep(objectID, destination)
	if err != nil {
		return nil, fmt.Errorf("chargeStep: %w", err)
	}
	return [][]byte{movePacket, stepPacket}, nil
}

func pursuitGoal(source game.Vec3, target game.Vec3, stopDistance float32) game.Vec3 {
	deltaX := source.X - target.X
	deltaY := source.Y - target.Y
	deltaZ := source.Z - target.Z
	distance := float32(math.Sqrt(float64(
		deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ,
	)))
	if distance <= stopDistance || distance == 0 {
		return source
	}
	ratio := stopDistance / distance
	return game.Vec3{
		X: target.X + deltaX*ratio,
		Y: target.Y + deltaY*ratio,
		Z: target.Z + deltaZ*ratio,
	}
}

func pursuitDirection(source game.Vec3, target game.Vec3) raknet.Vector3 {
	deltaX := target.X - source.X
	deltaY := target.Y - source.Y
	deltaZ := target.Z - source.Z
	distance := float32(math.Sqrt(float64(
		deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ,
	)))
	if distance == 0 {
		return raknet.Vector3{}
	}
	return raknet.Vector3{
		X: deltaX / distance,
		Y: deltaY / distance,
		Z: deltaZ / distance,
	}
}

func isFinitePosition(position game.Vec3) bool {
	return !math.IsNaN(float64(position.X)) &&
		!math.IsNaN(float64(position.Y)) &&
		!math.IsNaN(float64(position.Z)) &&
		!math.IsInf(float64(position.X), 0) &&
		!math.IsInf(float64(position.Y), 0) &&
		!math.IsInf(float64(position.Z), 0)
}
