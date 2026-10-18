package gameplay

import (
	"log"
	"time"

	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
)

// The registry lock owns channel state. Projectile lifetime remains with the
// burst run, so interrupting the channel does not remove rockets in flight.
type rocketBarrageChannel struct {
	run           *abilityraknet.BurstRun
	startedAt     time.Time
	sourceTime    uint64
	objectID      uint32
	releasePacket []byte
	isInterrupted bool
}

func (e *gameplayPeerSession) interruptRocketBarrage(now time.Time) {
	channel := e.rocketBarrage
	if channel == nil {
		return
	}
	e.rocketBarrage = nil
	channel.isInterrupted = true
	channel.run.StopLaunching()
	e.resetAbilityRelease()
	timestamp := channel.sourceTime + uint64(max(time.Duration(0), now.Sub(channel.startedAt))/time.Millisecond)
	animation, err := abilityraknet.AnimationReset(channel.objectID, timestamp)
	if err != nil {
		log.Printf("Rocket Barrage animation cleanup failed object=%d: %v", channel.objectID, err)
		e.queuePackets([][]byte{channel.releasePacket})
		return
	}
	e.queuePackets([][]byte{channel.releasePacket, animation})
}
