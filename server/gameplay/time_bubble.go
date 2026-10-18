package gameplay

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

type timeBubbleChange struct {
	objectID   uint32
	projectile heroAuraAreaProjectile
	packets    [][]byte
	isAdding   bool
}

type timeBubbleInstanceRelease struct {
	modifierPool *modifierPool
	instanceID   uint32
}

type timeBubbleControlBatch struct {
	packets  [][]byte
	releases []timeBubbleInstanceRelease
}

// Caller holds the registry and this bubble's mutex. Prepare all allocations
// and encodings before changing an existing membership or its effective speed.
func (e heroAuraAreaSchedule) reconcileTimeBubbleLocked(peerSession *gameplayPeerSession, deadline time.Duration, now time.Time) error {
	liveProjectiles := make(map[uint32]*abilityraknet.ProjectileRun)
	for _, owner := range e.runtime.registry.hostileProjectileOwnersLocked(peerSession, e.sessionKey) {
		snapshot := owner.run.Snapshot(now)
		if snapshot.IsActive && zonegeometry.Distance(game.Vec3(e.center), game.Vec3(snapshot.Position)) <= e.definition.Radius {
			liveProjectiles[owner.objectID] = owner.run
		}
	}
	changes := make([]timeBubbleChange, 0)
	allocations := make([]uint32, 0)
	for objectID, tracked := range e.run.projectiles {
		if liveProjectiles[objectID] == tracked.run {
			continue
		}
		packets, err := e.run.prepareBubbleRemovalLocked(objectID, tracked, now)
		if err != nil {
			return fmt.Errorf("bubbleExit[%d]: %w", objectID, err)
		}
		changes = append(changes, timeBubbleChange{objectID: objectID, projectile: tracked, packets: packets})
	}
	for objectID, run := range liveProjectiles {
		if tracked, isTracked := e.run.projectiles[objectID]; isTracked && tracked.run == run {
			continue
		}
		instanceID, err := e.run.modifierPool.Allocate()
		if err != nil {
			e.run.releaseBubbleAllocations(allocations)
			return fmt.Errorf("bubbleAllocate[%d]: %w", objectID, err)
		}
		allocations = append(allocations, instanceID)
		packet, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
			TargetID: objectID, ModifierGUID: e.definition.RootModifierID,
			InstanceID: instanceID, DurationMilliseconds: 0, StackCount: 1,
			StartMilliseconds: e.packet.SourceTime + uint64(deadline/time.Millisecond), SourceID: e.sourceObjectID,
		})
		if err != nil {
			e.run.releaseBubbleAllocations(allocations)
			return fmt.Errorf("bubbleCreate[%d]: %w", objectID, err)
		}
		trajectory, err := marshalTimeBubbleTrajectory(run.PreviewTimeBubble(now, nil, true), run.Definition().Distance)
		if err != nil {
			e.run.releaseBubbleAllocations(allocations)
			return fmt.Errorf("bubbleEnterMotion[%d]: %w", objectID, err)
		}
		packets := [][]byte{packet}
		if len(trajectory) != 0 {
			packets = append(packets, trajectory)
		}
		changes = append(changes, timeBubbleChange{objectID: objectID, projectile: heroAuraAreaProjectile{instanceID: instanceID, run: run}, packets: packets, isAdding: true})
	}
	for _, change := range changes {
		if change.isAdding {
			change.projectile.lease = change.projectile.run.AcquireTimeBubble(now)
			if change.projectile.lease == nil {
				e.run.releaseBubbleAllocations([]uint32{change.projectile.instanceID})
				continue
			}
			e.run.projectiles[change.objectID] = change.projectile
			e.runtime.registry.queueTimeBubbleControlLocked(e.run.originalZone, change.projectile.run, change.packets, nil)
			continue
		}
		e.run.removeBubbleProjectileLocked(change.objectID, change.projectile, change.packets, now)
	}
	*peerSession = e.runtime.registry.sessions[e.sessionKey]
	return nil
}

// Only the configured speed, direction and range are encoded. Frozen remains
// attribute-owned; speed is never permanently written as zero during a freeze.
func marshalTimeBubbleTrajectory(snapshot abilityraknet.ProjectileSnapshot, distance float32) ([]byte, error) {
	if !snapshot.IsActive {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.LocomotionUpdateContractMessage{
		ObjectID: snapshot.ObjectID,
		Locomotion: raknet.LocomotionReflection{Projectile: &raknet.ProjectileParameter{
			Speed: snapshot.Speed, Range: distance, Direction: raknet.Vector3(snapshot.Direction),
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("bubbleTrajectory: %w", err)
	}
	return packet, nil
}

func (e *heroAuraAreaRun) prepareBubbleRemovalLocked(objectID uint32, tracked heroAuraAreaProjectile, now time.Time) ([][]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.ModifierDeletedMessage{TargetID: objectID, InstanceID: tracked.instanceID})
	if err != nil {
		return nil, fmt.Errorf("bubbleDelete: %w", err)
	}
	packets := [][]byte{packet}
	if !e.runtime.registry.isHostileProjectileCurrentLocked(e.originalZone, objectID, tracked.run) {
		return packets, nil
	}
	trajectory, err := marshalTimeBubbleTrajectory(tracked.run.PreviewTimeBubble(now, tracked.lease, false), tracked.run.Definition().Distance)
	if err != nil {
		return nil, fmt.Errorf("bubbleExitMotion: %w", err)
	}
	if len(trajectory) != 0 {
		packets = append(packets, trajectory)
	}
	return packets, nil
}

func (e *heroAuraAreaRun) removeBubbleProjectileLocked(objectID uint32, tracked heroAuraAreaProjectile, packets [][]byte, now time.Time) {
	releases := []timeBubbleInstanceRelease{{modifierPool: e.modifierPool, instanceID: tracked.instanceID}}
	e.runtime.registry.queueTimeBubbleControlLocked(e.originalZone, tracked.run, packets, releases)
	tracked.lease.Release(now)
	delete(e.projectiles, objectID)
}

func (e *heroAuraAreaRun) releaseBubbleAllocations(instances []uint32) {
	for _, instanceID := range instances {
		err := e.modifierPool.Release(instanceID)
		if err != nil && e.runtime.logger != nil {
			e.runtime.logger.Printf("RakNet Time Bubble modifier release failed instance=%d: %v", instanceID, err)
		}
	}
}

func (e *gameplaySessionRegistry) queueTimeBubbleControlLocked(originZone *zone.Zone, run *abilityraknet.ProjectileRun, packets [][]byte, releases []timeBubbleInstanceRelease) {
	retirement := e.projectileRetirements[run]
	if retirement != nil && retirement.activeProducerCount > 0 {
		retirement.controlPacketBatches = append(retirement.controlPacketBatches, timeBubbleControlBatch{packets: clonePendingPackets(packets), releases: releases})
		return
	}
	if len(releases) != 0 {
		e.queueProjectileRetirementLocked(originZone, packets)
	} else {
		e.queueTimeBubblePresentationLocked(originZone, packets)
	}
	e.releaseTimeBubbleInstancesLocked(releases)
}

func (e *gameplaySessionRegistry) releaseTimeBubbleInstancesLocked(releases []timeBubbleInstanceRelease) {
	for _, release := range releases {
		err := release.modifierPool.Release(release.instanceID)
		if err != nil && e.logger != nil {
			e.logger.Printf("RakNet Time Bubble deferred modifier release failed instance=%d: %v", release.instanceID, err)
		}
	}
}

func (e *gameplaySessionRegistry) queueTimeBubblePresentationLocked(originZone *zone.Zone, packets [][]byte) {
	for sessionKey, member := range e.sessions {
		if member.zone != originZone || member.isRejoinPending || !member.isCampaignPresentationAvailable() {
			continue
		}
		err := member.queueCampaignPresentation(packets)
		if err != nil && e.logger != nil {
			e.logger.Printf("RakNet Time Bubble presentation failed user=%d: %v", member.binding.UserID, err)
		}
		e.sessions[sessionKey] = member
	}
}

func (e *gameplaySessionRegistry) retireTimeBubbleProjectileLocked(originZone *zone.Zone, objectID uint32, run *abilityraknet.ProjectileRun, now time.Time) {
	for bubble := range e.timeBubbles {
		if bubble.originalZone != originZone {
			continue
		}
		bubble.mutex.Lock()
		tracked, isTracked := bubble.projectiles[objectID]
		if isTracked && tracked.run == run {
			packet, err := raknet.MarshalApplication(raknet.ModifierDeletedMessage{TargetID: objectID, InstanceID: tracked.instanceID})
			if err != nil {
				if e.logger != nil {
					e.logger.Printf("RakNet Time Bubble flight deletion failed projectile=%d: %v", objectID, err)
				}
			} else {
				bubble.removeBubbleProjectileLocked(objectID, tracked, [][]byte{packet}, now)
			}
		}
		bubble.mutex.Unlock()
	}
}

type timeBubbleRetirement struct{ run *heroAuraAreaRun }

func (e timeBubbleRetirement) execute() {
	e.run.runtime.registry.mutex.Lock()
	err := e.run.retireTimeBubbleLocked(e.run.runtime.now())
	e.run.runtime.registry.mutex.Unlock()
	if err != nil && e.run.runtime.logger != nil {
		e.run.runtime.logger.Printf("RakNet Time Bubble retirement failed object=%d: %v", e.run.objectID, err)
	}
}

func (e *heroAuraAreaRun) stopTimeBubble() {
	e.mutex.Lock()
	if e.isCleaned || e.isStopping {
		e.mutex.Unlock()
		return
	}
	e.isStopping = true
	cancel := e.cancel
	e.cancel = nil
	e.mutex.Unlock()
	if cancel != nil {
		cancel()
	}
	retirement := timeBubbleRetirement{run: e}
	if e.runtime.registry.timer == nil {
		go retirement.execute()
		return
	}
	timerCancel, err := e.runtime.registry.timer.Schedule(0, retirement.execute)
	if err != nil || timerCancel == nil {
		if timerCancel != nil {
			timerCancel()
		}
		if e.runtime.logger != nil {
			e.runtime.logger.Printf("RakNet Time Bubble retirement schedule failed object=%d: %v", e.objectID, err)
		}
		go retirement.execute()
	}
}

func (e *heroAuraAreaRun) retainTimeBubbleLocked() {
	if e.runtime.registry.timeBubbles == nil {
		e.runtime.registry.timeBubbles = make(map[*heroAuraAreaRun]struct{})
	}
	e.runtime.registry.timeBubbles[e] = struct{}{}
	retirement := timeBubbleRetirement{run: e}
	e.stopContext = context.AfterFunc(e.originalZone.Context(), retirement.execute)
}

func (e *heroAuraAreaRun) retireTimeBubbleLocked(now time.Time) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isCleaned {
		return nil
	}
	changes := make([]timeBubbleChange, 0, len(e.projectiles))
	for objectID, tracked := range e.projectiles {
		packets, err := e.prepareBubbleRemovalLocked(objectID, tracked, now)
		if err != nil {
			return fmt.Errorf("bubbleRetireFlight: %w", err)
		}
		changes = append(changes, timeBubbleChange{objectID: objectID, projectile: tracked, packets: packets})
	}
	deletes := make([]heroAuraAreaTargetDelete, 0, len(e.targets))
	for objectID, target := range e.targets {
		deletes = append(deletes, heroAuraAreaTargetDelete{objectID: objectID, instanceID: target.instanceID})
	}
	packets, err := marshalAuraAreaDeletes(deletes)
	if err != nil {
		return fmt.Errorf("bubbleRetireTargets: %w", err)
	}
	if e.isObjectPublished {
		effectPacket, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{Slot: 1, IsRemovalRequested: true, IsHardStop: true, ObjectID: e.objectID})
		if err != nil {
			return fmt.Errorf("bubbleRetireEffect: %w", err)
		}
		deletePacket, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{e.objectID}})
		if err != nil {
			return fmt.Errorf("bubbleRetireObject: %w", err)
		}
		packets = append(packets, effectPacket, deletePacket)
	}
	e.runtime.registry.queueProjectileRetirementLocked(e.originalZone, packets)
	for _, change := range changes {
		e.removeBubbleProjectileLocked(change.objectID, change.projectile, change.packets, now)
	}
	for objectID, target := range e.targets {
		e.clearStatus(objectID, target.expiresAt)
		e.releaseBubbleAllocations([]uint32{target.instanceID})
	}
	e.targets = nil
	e.isCleaned = true
	e.isStopping = true
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	if e.stopContext != nil {
		isStopped := e.stopContext()
		if !isStopped {
			// The dispatched callback observes this cleaned source identity.
		}
		e.stopContext = nil
	}
	delete(e.runtime.registry.timeBubbles, e)
	return nil
}

func (e *gameplayPeerSession) retireTimeBubblesLocked() {
	for _, bubble := range e.heroAuraAreas {
		if bubble == nil || !bubble.isTimeBubble {
			continue
		}
		registry := bubble.runtime.registry
		ownerSessionKey := ""
		for sessionKey, member := range registry.sessions {
			if member.zone == e.zone && member.binding.UserID == e.binding.UserID && member.generation == e.generation {
				ownerSessionKey = sessionKey
				registry.sessions[sessionKey] = *e
				break
			}
		}
		err := bubble.retireTimeBubbleLocked(bubble.runtime.now())
		if err != nil && bubble.runtime.logger != nil {
			bubble.runtime.logger.Printf("RakNet Time Bubble caster retirement failed object=%d: %v", bubble.objectID, err)
		}
		if ownerSessionKey != "" {
			*e = registry.sessions[ownerSessionKey]
		}
	}
}
