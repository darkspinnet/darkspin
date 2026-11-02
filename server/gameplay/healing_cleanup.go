package gameplay

import (
	"log"
	"time"

	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
)

// Build 103's FieldMedicSupport.Deactivate resets the animation and removes
// its effects. Canceling the scheduled release must publish the same cleanup.
// The caller holds the session registry lock.
func (e *gameplayPeerSession) stopHealingTicksPresentation() {
	run := e.heroHealingTicks
	if run == nil {
		return
	}
	e.heroHealingTicks = nil
	run.Stop()
	if !run.isChanneled {
		return
	}
	packets := [][]byte{run.releasePacket}
	elapsed := max(time.Duration(0), time.Since(run.startedAt))
	resetPacket, err := abilityraknet.AnimationReset(
		run.sourceObjectID, run.sourceTime+uint64(elapsed/time.Millisecond),
	)
	if err != nil {
		log.Printf("RakNet healing channel recovery animation failed user=%d: %v", e.binding.UserID, err)
	} else {
		packets = append(packets, resetPacket)
	}
	e.queueCampaignPackets(packets)
}
