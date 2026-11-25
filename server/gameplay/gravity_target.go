package gameplay

import (
	"fmt"
	"log"
	"time"

	"github.com/darkspinnet/darkspin/server/zone"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const gravityStormDropDuration = 966667 * time.Microsecond

// The packaged modifier animates the target itself; creating its HUD modifier
// does not execute the server-side Lua animation callbacks on the client.
func (e heroChannelAreaSchedule) animateTargetsLocked(animationName string, deadline time.Duration) error {
	packets := make([][]byte, 0)
	for _, target := range e.run.Targets() {
		npc, isFound := e.run.npc.NPC(target.snapshot.Plan.ObjectID)
		if !isFound || npc.IsDefeated || npc.HitPoint <= 0 {
			continue
		}
		packet, err := npcraknet.AnimationState(npc.Plan.ObjectID,
			animationName, e.packet.SourceTime+uint64(deadline/time.Millisecond))
		if err != nil {
			return fmt.Errorf("gravityTargetAnimation: %w", err)
		}
		packets = append(packets, packet)
	}
	e.runtime.registry.queueChannelPresentationLocked(e.run.zone, packets)
	return nil
}

func (e heroChannelAreaSchedule) getUp() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		return nil, nil
	}
	err := e.animateTargetsLocked("gravitystorm_target_getup", e.slamStart+gravityStormSlamDuration)
	if err != nil {
		return nil, fmt.Errorf("gravityGetUp: %w", err)
	}
	e.run.mutex.Lock()
	e.run.isGettingUp = true
	e.run.mutex.Unlock()
	return nil, nil
}

// Registry and run locks are held. Normal completion resets the get-up pose;
// interruption uses the authored drop before restoring ordinary animation.
func (e *heroChannelAreaRun) targetRetirementPacketsLocked() ([][]byte, error) {
	packets := make([][]byte, 0)
	timestamp := e.sourceTime + uint64(max(time.Duration(0), e.registry.now().Sub(e.startedAt))/time.Millisecond)
	for _, target := range e.targets {
		npc, isFound := e.npc.NPC(target.snapshot.Plan.ObjectID)
		if !target.isPublished || !isFound || npc.IsDefeated || npc.HitPoint <= 0 {
			continue
		}
		packet, err := npcraknet.ResetAnimation(npc.Plan.ObjectID, timestamp)
		if !e.isGettingUp && e.timer != nil {
			packet, err = npcraknet.AnimationState(npc.Plan.ObjectID, "gravitystorm_target_drop", timestamp)
		}
		if err != nil {
			return nil, fmt.Errorf("gravityRetireAnimation: %w", err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e *heroChannelAreaRun) finishInterruptedTargetsLocked() {
	if e.isGettingUp || e.timer == nil {
		return
	}
	for _, target := range e.targets {
		npc, isFound := e.npc.NPC(target.snapshot.Plan.ObjectID)
		if !target.isPublished || !isFound || npc.IsDefeated || npc.HitPoint <= 0 {
			continue
		}
		at := e.registry.now()
		instanceID, allocationErr := e.modifierPool.Allocate()
		if allocationErr != nil {
			log.Printf("RakNet gravity drop owner allocation failed target=%d: %v", npc.Plan.ObjectID, allocationErr)
			step := gravityStormDropResetStep{registry: e.registry, zone: e.zone, npc: e.npc, objectID: npc.Plan.ObjectID}
			step.resetLocked()
			continue
		}
		step := gravityStormDropResetStep{
			registry: e.registry, zone: e.zone, npc: e.npc, objectID: npc.Plan.ObjectID,
			modifierPool: e.modifierPool, instanceID: instanceID,
			expiresAt: at.Add(gravityStormDropDuration),
			timestamp: e.sourceTime + uint64(max(time.Duration(0), at.Sub(e.startedAt)+gravityStormDropDuration)/time.Millisecond),
		}
		isAdmitted, err := e.npc.AdmitStunModifier(step.objectID, instanceID, at, step.expiresAt)
		if err != nil {
			log.Printf("RakNet gravity drop control failed target=%d: %v", step.objectID, err)
		}
		if err != nil || !isAdmitted {
			step.resetLocked()
			continue
		}
		cancel, scheduleErr := e.timer.Schedule(gravityStormDropDuration, step.execute)
		if scheduleErr != nil || cancel == nil {
			if cancel != nil {
				cancel()
			}
			log.Printf("RakNet gravity drop reset scheduling failed target=%d: %v", step.objectID, scheduleErr)
			// Keep a failed scheduler from leaving the raised pose behind.
			step.resetLocked()
		}
	}
}

type gravityStormDropResetStep struct {
	registry     *gameplaySessionRegistry
	modifierPool *modifierPool
	instanceID   uint32
	zone         *zone.Zone
	npc          *zonenpc.Session
	objectID     uint32
	expiresAt    time.Time
	timestamp    uint64
}

func (e gravityStormDropResetStep) execute() {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	e.resetLocked()
}

func (e gravityStormDropResetStep) resetLocked() {
	e.npc.ClearStunModifier(e.objectID, e.instanceID)
	if e.modifierPool != nil && e.instanceID != 0 {
		releaseErr := e.modifierPool.Release(e.instanceID)
		if releaseErr != nil {
			log.Printf("RakNet gravity drop owner release failed target=%d: %v", e.objectID, releaseErr)
		}
	}
	npc, isFound := e.npc.NPC(e.objectID)
	if !isFound || npc.IsDefeated || npc.HitPoint <= 0 ||
		e.npc.StunRemaining(e.objectID, e.registry.now()) > 0 {
		return
	}
	packet, err := npcraknet.ResetAnimation(e.objectID, e.timestamp)
	if err != nil {
		log.Printf("RakNet gravity drop reset failed target=%d: %v", e.objectID, err)
		return
	}
	e.registry.queueChannelRemovalsLocked(e.zone, [][]byte{packet})
}
