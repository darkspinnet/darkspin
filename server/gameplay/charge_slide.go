package gameplay

import (
	"fmt"
	"log"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
)

func (e heroChargeSchedule) beginPhantomSlide() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		return nil, nil
	}
	jumpPacket, err := actionraknet.ChargeSlide(
		e.sourceObjectID, game.Vec3(e.startPosition), game.Vec3(e.destination),
		zonePlayerMoveSpeed*e.definition.MovementSpeedMultiplier,
	)
	if err != nil {
		return nil, fmt.Errorf("phantomSlide: %w", err)
	}
	e.run.mutex.Lock()
	if e.run.isCleaned {
		e.run.mutex.Unlock()
		return nil, nil
	}
	e.run.mutex.Unlock()
	// ObjectUpdate field 17 destroys the navigation agent. SetNavCollision
	// changes agent avoidance instead; a flat jump needs the agent alive and
	// suspends physics collision itself during its native travel lifecycle.
	e.runtime.registry.queueChargePacketsLocked(e.run.zone, [][]byte{jumpPacket})
	return nil, nil
}

// Retire the pose before admitting another action, even when the charge's
// silence modifiers must remain alive. Late expiry must not reset a new pose.
// The registry lock is held by every caller.
func (e *heroChargeRun) resetPhantomAnimationLocked() [][]byte {
	e.mutex.Lock()
	if !e.isAnimationActive {
		e.mutex.Unlock()
		return nil
	}
	e.isAnimationActive = false
	timestamp := e.animationTimestamp
	if e.now != nil {
		timestamp += uint64(max(time.Duration(0), e.now().Sub(e.animationStartTime)) / time.Millisecond)
	}
	e.mutex.Unlock()
	if e.registry != nil {
		isCurrent := false
		for _, member := range e.registry.sessions {
			if member.zone == e.zone && member.binding.UserID == e.userID &&
				member.generation == e.generation && member.heroCharge == e &&
				member.deployedObjectID == e.ownerObjectID {
				isCurrent = true
				break
			}
		}
		if !isCurrent {
			return nil
		}
	}
	packet, err := abilityraknet.AnimationReset(e.ownerObjectID, timestamp)
	if err != nil {
		log.Printf("RakNet Phantom Charge animation retirement failed owner=%d: %v", e.ownerObjectID, err)
		return nil
	}
	packets := [][]byte{packet}
	if e.registry != nil {
		e.registry.queueChargePacketsLocked(e.zone, packets)
		return nil
	}
	return packets
}
