package raknet103

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
)

func Follow(plans []zonecompanion.Follow) ([][]byte, error) {
	packets := make([][]byte, 0, len(plans)*2)
	for index, current := range plans {
		if current.ObjectID == 0 || current.OwnerObjectID == 0 ||
			(!current.IsExact && current.DesiredStopDistance <= 0) {
			return nil, fmt.Errorf("follow[%d]: invalid", index)
		}
		// TravelDuration and Revision belong to scheduled server arrival.
		goalPosition := current.Goal
		if current.IsWaypointKnown {
			goalPosition = current.Waypoint
		}
		facingX := goalPosition.X - current.Position.X
		facingY := goalPosition.Y - current.Position.Y
		facingZ := goalPosition.Z - current.Position.Z
		facingLength := float32(math.Sqrt(float64(
			facingX*facingX + facingY*facingY + facingZ*facingZ,
		)))
		if facingLength <= 0 && !current.IsWaypointKnown {
			return nil, fmt.Errorf("follow[%d]: zero direction", index)
		}
		goalFlags := uint32(0x41)
		targetID := current.OwnerObjectID
		stopDistance := current.DesiredStopDistance
		if current.IsWaypointKnown {
			goalFlags = 1
			targetID = 0
			stopDistance = 0
			if facingLength <= 0 {
				goalFlags = 0x20
				facingLength = 1
			}
		}
		if current.IsExact {
			// Build 103 MoveToPointExact (A147D0) sets goal flag 1.
			goalFlags = 1
			targetID = 0
		}
		movePacket, err := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{
			ObjectID: current.ObjectID, GoalFlags: goalFlags,
			GoalPosition: raknet.Vector3{
				X: goalPosition.X, Y: goalPosition.Y, Z: goalPosition.Z,
			},
			Facing: raknet.Vector3{
				X: facingX / facingLength,
				Y: facingY / facingLength,
				Z: facingZ / facingLength,
			},
			AllowedStopDistance: stopDistance,
			DesiredStopDistance: stopDistance,
			TargetPosition: raknet.Vector3{
				X: goalPosition.X, Y: goalPosition.Y, Z: goalPosition.Z,
			},
			TargetObjectID: targetID,
		})
		if err != nil {
			return nil, fmt.Errorf("followMarshal[%d]: %w", index, err)
		}
		if len(movePacket) == 0 {
			return nil, errors.New("follow packet empty")
		}
		goal := raknet.Vector3{
			X: goalPosition.X, Y: goalPosition.Y, Z: goalPosition.Z,
		}
		correctionPacket, err := raknet.MarshalApplication(
			raknet.LocomotionUnreliableMessage{
				ObjectID: current.ObjectID, GoalPosition: goal,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("followCorrectionMarshal[%d]: %w", index, err)
		}
		packets = append(packets, movePacket, correctionPacket)
	}
	return packets, nil
}

// FollowArrival fixes the final authoritative pose before retiring the goal.
func FollowArrival(plan zonecompanion.Follow) ([][]byte, error) {
	if plan.ObjectID == 0 || plan.Revision == 0 {
		return nil, errors.New("companion follow arrival invalid")
	}
	position, err := raknet.MarshalApplication(raknet.ObjectUpdateMessage{
		ObjectID: plan.ObjectID, PositionX: plan.Destination.X,
		PositionY: plan.Destination.Y, PositionZ: plan.Destination.Z,
		IsVisible: true,
	})
	if err != nil {
		return nil, fmt.Errorf("arrivalPosition: %w", err)
	}
	stop, err := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{
		ObjectID: plan.ObjectID, GoalFlags: 0x20,
		GoalPosition: raknet.Vector3(plan.Destination),
	})
	if err != nil {
		return nil, fmt.Errorf("arrivalStop: %w", err)
	}
	return [][]byte{position, stop}, nil
}
