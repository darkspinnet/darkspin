package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
)

// Both gameplay realms settle the detached action before publishing movement.
func (e campaignMovementInterruption) stopPackets(objectID uint32, timestamp uint64) ([][]byte, error) {
	packets := make([][]byte, 0, 5)
	if e.basicAttack != nil {
		e.basicAttack.Stop()
		cancelPacket, err := raknet.MarshalApplication(raknet.ActionCommandResponseMessage{
			SyncStamp: e.basicSyncStamp, ResponseType: raknet.ActionResponseRejected,
			ObjectID: objectID, UserData: 0xffffffff,
		})
		if err != nil {
			return nil, fmt.Errorf("moveAttackCancel: %w", err)
		}
		resetPacket, err := abilityraknet.AnimationReset(objectID, timestamp)
		if err != nil {
			return nil, fmt.Errorf("moveAttackReset: %w", err)
		}
		packets = append(packets, cancelPacket, resetPacket)
	}
	if e.heroDrain != nil {
		drainPackets, err := e.heroDrain.interruptionPacketsAt(timestamp)
		e.heroDrain.Stop()
		if err != nil {
			return nil, fmt.Errorf("moveDrainStop: %w", err)
		}
		packets = append(packets, drainPackets...)
	}
	if e.healingChannel != nil {
		healingPackets, err := e.healingChannel.interruptionPacketsAt(timestamp)
		e.healingChannel.Stop()
		if err != nil {
			return nil, fmt.Errorf("moveHealingStop: %w", err)
		}
		packets = append(packets, healingPackets...)
	}
	if e.isDanceStopped {
		resetPacket, err := abilityraknet.AnimationReset(objectID, timestamp)
		if err != nil {
			return nil, fmt.Errorf("moveDanceReset: %w", err)
		}
		packets = append(packets, resetPacket)
	}
	return packets, nil
}
