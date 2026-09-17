package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
)

// teleportOwnedCompanions keeps living player-owned summons on the same side
// of an authored route transition as their owner. Ordinary follow movement is
// not suitable here because campaign sections are separated by disconnected
// navigation islands.
func (e *gameplayPeerSession) teleportOwnedCompanions(
	destination game.Vec3, orientation game.Quaternion,
) ([][]byte, error) {
	if e == nil || e.zone == nil || e.zone.Companion() == nil {
		return nil, nil
	}
	packets := make([][]byte, 0)
	for _, companion := range e.zone.Companion().Snapshots() {
		if companion.UserID != e.binding.UserID ||
			companion.PeerGeneration != e.generation ||
			companion.OwnerObjectID != e.deployedObjectID ||
			companion.HitPoint <= 0 {
			continue
		}
		if companion.TargetObjectID != 0 {
			e.zone.Companion().ReleaseAttack(
				companion.ObjectID, companion.TargetObjectID,
			)
		}
		activation, isActivationFound :=
			e.sagePassiveActivations[companion.ObjectID]
		if isActivationFound {
			if activation.Attack != nil {
				activation.Attack.Stop()
				activation.Attack = nil
			}
			activation.Position.X = destination.X
			activation.Position.Y = destination.Y
			activation.Position.Z = destination.Z
			activation.TargetObjectID = 0
			e.sagePassiveActivations[companion.ObjectID] = activation
		}
		err := e.zone.Companion().SetPosition(companion.ObjectID, destination)
		if err != nil {
			return nil, fmt.Errorf(
				"companionTeleportPosition[%d]: %w", companion.ObjectID, err,
			)
		}
		companionPackets, err := actionraknet.Teleport(actionraknet.TeleportRequest{
			ObjectID: companion.ObjectID, Position: destination,
			Orientation: orientation,
		})
		if err != nil {
			return nil, fmt.Errorf(
				"companionTeleportMarshal[%d]: %w", companion.ObjectID, err,
			)
		}
		packets = append(packets, companionPackets...)
	}
	return packets, nil
}
