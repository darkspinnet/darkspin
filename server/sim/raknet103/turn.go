package raknet103

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
)

// encodeTurnGoal publishes a captured turn to the owner as well as observers.
// Native 5393D0 skips 0x91 for the controlled hero; 539900 applies reflection
// without that ownership gate. Also initialize the partial goal so a previous
// route cannot move the actor during the turn.
func (e *Encoder) encodeTurnGoal(ctx context.Context, intent sim.LocomotionStopIntent) ([]byte, error) {
	binding, err := e.resolveRole(ctx, intent.Role)
	if err != nil {
		return nil, fmt.Errorf("turnRole: %w", err)
	}
	position := raknet.Vector3{
		X: binding.Position.X, Y: binding.Position.Y, Z: binding.Position.Z,
	}
	goalFlags := uint32(0x42)
	targetObjectID := uint32(0)
	externalMotion := raknet.Vector3{}
	stopDistance := float32(0)
	facing := raknet.Vector3(intent.Facing)
	targetPosition := raknet.Vector3(intent.TargetPosition)
	packet, err := marshalApplication(raknet.LocomotionUpdateContractMessage{
		ObjectID: binding.ObjectID,
		Locomotion: raknet.LocomotionReflection{
			GoalFlags: &goalFlags, GoalPosition: &position,
			PartialGoalPosition: &position, Facing: &facing,
			TargetPosition: &targetPosition, TargetObjectID: &targetObjectID,
			ExternalLinearVelocity: &externalMotion, ExternalForce: &externalMotion,
			AllowedStopDistance: &stopDistance, DesiredStopDistance: &stopDistance,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("turnMarshal: %w", err)
	}
	return packet, nil
}
