package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/zone"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

func (e *heroAuraAreaRun) queuePresentationLocked(packets [][]byte, isRemoval bool) {
	e.runtime.registry.queueAuraPresentationLocked(e.originalZone, e.arenaGameID, packets, isRemoval)
}

func (e *gameplaySessionRegistry) queueAuraPresentationLocked(
	originZone *zone.Zone, arenaGameID uint32, packets [][]byte, isRemoval bool,
) {
	if arenaGameID == 0 {
		if isRemoval {
			e.queueProjectileRetirementLocked(originZone, packets)
		} else {
			e.queueTimeBubblePresentationLocked(originZone, packets)
		}
		return
	}
	for key, member := range e.sessions {
		if member.binding.Mode != game.ModeArena || member.binding.GameID != arenaGameID ||
			member.isRejoinPending || !member.stage.IsDungeon() || (!isRemoval && !member.isArenaCombatActive) {
			continue
		}
		member.queuePackets(packets)
		e.sessions[key] = member
	}
}

// The authored DelayingSphereDrag modifier is Unique. Multiple fields retain
// independent membership identities, while their numerical slow applies once.
func (e gameplayPeerSession) arenaSphereMovementBuff() float32 {
	for _, objectID := range e.arenaSphereSlows {
		if objectID == e.deployedObjectID && objectID != 0 {
			return -0.60
		}
	}
	return 0
}

func (e gameplayPeerSession) arenaSphereAttackSpeed() float32 {
	if e.arenaSphereMovementBuff() != 0 {
		return -0.40
	}
	return 0
}

func (e *heroAuraAreaRun) refreshArenaSphereMotionLocked(member *gameplayPeerSession) error {
	if member.playerMotion == nil {
		return nil
	}
	increase := max(float32(-0.9),
		e.runtime.registry.passiveMovementIncrease(*member)+member.enemyMovementSpeedBuff())
	err := member.playerMotion.SetSpeed(e.runtime.now(), zonePlayerMoveSpeed*(1+increase))
	if err != nil {
		return fmt.Errorf("sphereMotion: %w", err)
	}
	return nil
}

func (e *heroAuraAreaRun) clearArenaSphereSlowLocked(objectID uint32) {
	target, isTracked := e.targets[objectID]
	if !isTracked {
		return
	}
	for key, member := range e.runtime.registry.sessions {
		if member.binding.Mode != game.ModeArena || member.binding.GameID != e.arenaGameID ||
			member.arenaSphereSlows[target.instanceID] != objectID {
			continue
		}
		delete(member.arenaSphereSlows, target.instanceID)
		e.runtime.registry.sessions[key] = member
		if member.deployedObjectID != objectID {
			continue
		}
		motionErr := e.refreshArenaSphereMotionLocked(&member)
		if motionErr != nil && e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena Delaying Sphere movement restoration failed object=%d: %v", objectID, motionErr)
		}
		packet, err := marshalCampaignNPCSlowAttributes(objectID, member.enemyAttackSpeed(), member.enemyMovementSpeedBuff())
		if err != nil {
			if e.runtime.logger != nil {
				e.runtime.logger.Printf("Arena Delaying Sphere slow restoration failed object=%d: %v", objectID, err)
			}
			continue
		}
		e.queuePresentationLocked([][]byte{packet}, true)
	}
}

func (e heroAuraAreaSchedule) reconcileArenaSphereLocked(
	peerSession *gameplayPeerSession, deadline time.Duration, expiresAt time.Time,
) error {
	if !e.run.isTimeBubble {
		return errors.New("Arena aura runtime unavailable")
	}
	e.runtime.registry.sessions[e.sessionKey] = *peerSession
	targets := arenaAbilityTargets{runtime: e.runtime, source: *peerSession}
	liveTargets := make(map[uint32]string)
	for _, target := range targets.LiveSnapshots() {
		if zonegeometry.Distance(game.Vec3(e.center), target.Plan.Position) > e.definition.Radius {
			continue
		}
		key, member, isFound := targets.member(target.Plan.ObjectID)
		if !isFound || member.isHeroDebuffImmune(target.Plan.ObjectID) || member.isOverdriveActiveAt(e.runtime.now()) {
			continue
		}
		liveTargets[target.Plan.ObjectID] = key
	}
	e.run.mutex.Lock()
	defer e.run.mutex.Unlock()
	if e.run.isCleaned || e.run.isStopping {
		return nil
	}
	for objectID, tracked := range e.run.targets {
		if _, isInside := liveTargets[objectID]; isInside {
			continue
		}
		packets, err := marshalAuraAreaDeletes([]heroAuraAreaTargetDelete{{objectID: objectID, instanceID: tracked.instanceID}})
		if err != nil {
			return fmt.Errorf("arenaAuraExit: %w", err)
		}
		e.run.queuePresentationLocked(packets, true)
		e.run.clearArenaSphereSlowLocked(objectID)
		delete(e.run.targets, objectID)
		e.run.releaseAuraInstance(tracked.instanceID)
	}
	for objectID, key := range liveTargets {
		if _, isTracked := e.run.targets[objectID]; isTracked {
			continue
		}
		instanceID, err := e.run.modifierPool.Allocate()
		if err != nil {
			return fmt.Errorf("arenaAuraAllocate: %w", err)
		}
		packets, err := e.prepareNPCMembership(objectID, instanceID, deadline)
		if err != nil {
			e.run.releaseAuraInstance(instanceID)
			return fmt.Errorf("arenaAuraMembership: %w", err)
		}
		member := e.runtime.registry.sessions[key]
		if member.arenaSphereSlows == nil {
			member.arenaSphereSlows = make(map[uint32]uint32)
		}
		member.arenaSphereSlows[instanceID] = objectID
		attributePacket, err := marshalCampaignNPCSlowAttributes(objectID, member.enemyAttackSpeed(), member.enemyMovementSpeedBuff())
		if err != nil {
			delete(member.arenaSphereSlows, instanceID)
			e.run.releaseAuraInstance(instanceID)
			return fmt.Errorf("arenaAuraAttributes: %w", err)
		}
		err = e.run.refreshArenaSphereMotionLocked(&member)
		if err != nil {
			delete(member.arenaSphereSlows, instanceID)
			e.run.releaseAuraInstance(instanceID)
			return fmt.Errorf("arenaAuraMotion: %w", err)
		}
		e.runtime.registry.sessions[key] = member
		e.run.targets[objectID] = heroAuraAreaTarget{instanceID: instanceID, expiresAt: expiresAt}
		packets = append(packets, attributePacket)
		e.run.queuePresentationLocked(packets, false)
	}
	*peerSession = e.runtime.registry.sessions[e.sessionKey]
	err := e.reconcileTimeBubbleLocked(peerSession, deadline, e.runtime.now())
	if err != nil {
		return fmt.Errorf("arenaAuraProjectiles: %w", err)
	}
	return nil
}
