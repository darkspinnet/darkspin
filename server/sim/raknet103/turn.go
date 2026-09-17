package raknet103

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
)

// encodeTurnGoal initializes the remote mover's partial goal before 0x91.
// The fixed turn command updates the main goal but leaves the partial goal
// unchanged, so remote NPCs otherwise walk toward zero or a previous route.
func (e *Encoder) encodeTurnGoal(ctx context.Context, intent sim.LocomotionStopIntent) ([]byte, error) {
	binding, err := e.resolveRole(ctx, intent.Role)
	if err != nil {
		return nil, fmt.Errorf("turnRole: %w", err)
	}
	position := raknet.Vector3{
		X: binding.Position.X, Y: binding.Position.Y, Z: binding.Position.Z,
	}
	packet, err := marshalApplication(raknet.LocomotionUpdateContractMessage{
		ObjectID:   binding.ObjectID,
		Locomotion: raknet.LocomotionReflection{PartialGoalPosition: &position},
	})
	if err != nil {
		return nil, fmt.Errorf("turnMarshal: %w", err)
	}
	return packet, nil
}
