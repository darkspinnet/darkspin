package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
)

func (e *heroHealingTicksRun) interruptionPacketsAt(timestamp uint64) ([][]byte, error) {
	if e == nil || !e.isChanneled {
		return nil, nil
	}
	packets := make([][]byte, 0, 4)
	for _, effect := range e.ReleaseEffects() {
		packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
			Slot: effect.slot + 1, IsRemovalRequested: true,
			IsHardStop: true, ObjectID: effect.objectID,
		})
		if err != nil {
			return nil, fmt.Errorf("healingEffectStop: %w", err)
		}
		packets = append(packets, packet)
	}
	resetPacket, err := abilityraknet.AnimationReset(e.sourceObjectID, timestamp)
	if err != nil {
		return nil, fmt.Errorf("healingAnimationStop: %w", err)
	}
	packets = append(packets, resetPacket)
	if len(e.releasePacket) != 0 {
		packets = append(packets, e.releasePacket)
	}
	return packets, nil
}
