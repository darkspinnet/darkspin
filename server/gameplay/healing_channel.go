package gameplay

import (
	"fmt"

	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
)

func (e *heroHealingTicksRun) interruptionPacketsAt(timestamp uint64) ([][]byte, error) {
	if e == nil || !e.isChanneled {
		return nil, nil
	}
	packets := make([][]byte, 0, 4)
	e.retireEffects()
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
