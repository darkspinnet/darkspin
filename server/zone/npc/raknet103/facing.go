package raknet103

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// RestorePose relocates the rendered root, unlike a locomotion goal update.
// Preserve the saved heading; the subsequent ability owns any intentional turn.
func RestorePose(objectID uint32, position game.Vec3, facing game.Vec3) ([]byte, error) {
	if objectID == 0 || !isFiniteVec3(position) || !isFiniteVec3(facing) || facing.Length() <= 0 {
		return nil, errors.New("invalid restored npc pose")
	}
	yaw := math.Atan2(-float64(facing.X), float64(facing.Y))
	pitch := math.Atan2(float64(facing.Z), math.Hypot(float64(facing.X), float64(facing.Y)))
	sx, cx := math.Sincos(pitch / 2)
	sz, cz := math.Sincos(yaw / 2)
	packet, err := raknet.MarshalApplication(raknet.ObjectTeleportMessage{
		ObjectID: objectID, Position: vector(position),
		Orientation: raknet.Quaternion{
			X: float32(cz * sx), Y: float32(sz * sx),
			Z: float32(sz * cx), W: float32(cz * cx),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("restorePose: %w", err)
	}
	return packet, nil
}

// attackTurn follows build 103's captured-point constructor (0x42). A stop
// (0x20) ignores Facing; a turn requires TargetPosition as well as Facing.
func attackTurn(plan zonenpc.AttackPlan) raknet.ObjectPlayerMoveMessage {
	message := raknet.ObjectPlayerMoveMessage{
		ObjectID: plan.SourceObjectID, GoalFlags: 0x20,
		GoalPosition: vector(plan.SourcePosition),
	}
	if !plan.IsFacingTarget() {
		return message
	}
	message.GoalFlags = 0x42
	message.TargetPosition = vector(plan.TargetPosition)
	message.Facing = direction(message.GoalPosition, message.TargetPosition)
	return message
}

// FaceTarget publishes a turn without an activation animation. Tracking uses
// the native object-facing constructor; captured turns leave the target ID zero.
func FaceTarget(plan zonenpc.AttackPlan, isTracking bool) ([][]byte, error) {
	if plan.SourceObjectID == 0 || !isFiniteVec3(plan.SourcePosition) ||
		!isFiniteVec3(plan.TargetPosition) {
		return nil, errors.New("invalid npc turn")
	}
	if !plan.IsFacingTarget() {
		return nil, nil
	}
	message := attackTurn(plan)
	if isTracking {
		message.GoalFlags = 0x102
		message.TargetObjectID = plan.TargetObjectID
		message.TargetPosition = raknet.Vector3{}
	}
	return marshalMessages([]raknet.ApplicationMessage{message}, "faceTarget")
}

// RestoreFacing applies the saved heading without changing the spawn position.
func RestoreFacing(objectID uint32, position game.Vec3, facing game.Vec3) ([][]byte, error) {
	if objectID == 0 || !isFiniteVec3(position) || !isFiniteVec3(facing) || facing.Length() <= 0 {
		return nil, errors.New("invalid restored npc facing")
	}
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.ObjectPlayerMoveMessage{
			ObjectID: objectID, GoalFlags: 0x06, GoalPosition: vector(position),
			Facing: vector(facing.Scale(1 / facing.Length())),
		},
	}, "restoreFacing")
}
