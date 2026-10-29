package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const (
	// Ability chunk 579 waits until cast time 8s to remove the well; modifier
	// chunk 17 waits 1s after its creation at the 1s hit point before stabilizing.
	scaldronSinkholeActiveDuration = 6 * time.Second
	scaldronSinkholePulseDuration  = 50 * time.Millisecond
)

type campaignScaldronSinkholePulse struct {
	runtime            campaignNPCActionRuntime
	sessionKey         string
	generation         uint64
	objectID           uint32
	timestamp          uint64
	profile            zonenpc.ActionProfile
	reductionExpiresAt time.Time
	effectSlot         uint8
	isEffectAllocated  bool
	isFirst            bool
}

func (e campaignScaldronSinkholePulse) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(e.generation, e.objectID)
	if !isCurrent {
		if isFound && peerSession.zone != nil && peerSession.zone.NPCs() != nil {
			peerSession.zone.NPCs().ClearDamageReduction(
				e.objectID, e.reductionExpiresAt,
			)
		}
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(e.objectID)
	if !isSourceFound || source.IsDefeated {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if e.isFirst {
		if peerSession.campaignNPCSinkholeFields == nil {
			peerSession.campaignNPCSinkholeFields = make(map[uint32]sinkholeField)
		}
		peerSession.campaignNPCSinkholeFields[e.objectID] = sinkholeField{
			expiresAt: e.reductionExpiresAt, radius: e.profile.Radius, strength: e.profile.ForcedMovementSpeed,
		}
		err := peerSession.zone.NPCs().ApplyDamageReduction(
			e.objectID, e.profile.SelfDamageReduction, e.reductionExpiresAt,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("sinkholeReductionApply: %w", err)
		}
	}
	packets := make([][]byte, 0)
	if e.isFirst && e.isEffectAllocated {
		removePacket, err := npcraknet.ForcedMovementEffect(e.objectID, e.effectSlot, "", true)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("sinkholeStartupRemove: %w", err)
		}
		packets = append(packets, removePacket)
		stablePacket, err := npcraknet.ForcedMovementEffect(
			e.objectID, e.effectSlot,
			"spacetime_ScaldronBasicSinkhole_well_stable.ServerEventDef",
			false,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("sinkholeStableEffect: %w", err)
		}
		packets = append(packets, stablePacket)
	}
	now := e.runtime.now()
	seenProjectiles := make(map[uint32]struct{})
	for _, member := range e.runtime.registry.sessions {
		if member.zone != peerSession.zone {
			continue
		}
		for projectileObjectID, run := range member.campaignNPCProjectiles {
			if _, isSeen := seenProjectiles[projectileObjectID]; isSeen {
				continue
			}
			seenProjectiles[projectileObjectID] = struct{}{}
			snapshot := run.Snapshot(now)
			if !snapshot.IsActive || zonegeometry.Distance(
				source.Plan.Position,
				game.Vec3{X: snapshot.Position.X, Y: snapshot.Position.Y, Z: snapshot.Position.Z},
			) > e.profile.Radius {
				continue
			}
			gravityPackets, err := run.ApplyGravity(
				now,
				sim.Position{
					X: source.Plan.Position.X, Y: source.Plan.Position.Y,
					Z: source.Plan.Position.Z,
				},
				e.profile.ProjectileGravity, scaldronSinkholePulseDuration,
			)
			if err != nil {
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf(
					"sinkholeProjectileGravity[%d]: %w", projectileObjectID, err,
				)
			}
			packets = append(packets, gravityPackets...)
		}
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	return packets, nil
}

type campaignScaldronSinkholeFizzle struct {
	runtime            campaignNPCActionRuntime
	sessionKey         string
	generation         uint64
	objectID           uint32
	reductionExpiresAt time.Time
	effectSlot         uint8
	isEffectAllocated  bool
}

func (e campaignScaldronSinkholeFizzle) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		if e.isEffectAllocated && e.runtime.effectPool != nil {
			isEffectReleased := e.runtime.effectPool.Release(e.objectID, e.effectSlot)
			if !isEffectReleased {
				// A newer cleanup already returned this presentation slot.
			}
		}
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	peerSession.zone.NPCs().ClearDamageReduction(e.objectID, e.reductionExpiresAt)
	source, isSourceFound := peerSession.zone.NPCs().NPC(e.objectID)
	e.runtime.registry.mutex.Unlock()
	cleanup := campaignDamageRuntime{
		registry: e.runtime.registry, npc: e.runtime, effectPool: e.runtime.effectPool,
	}
	packets, err := cleanup.stopSinkholeFields(e.sessionKey, e.generation, e.objectID)
	if err != nil {
		return nil, fmt.Errorf("sinkholeExpiry: %w", err)
	}
	if !isSourceFound || source.IsDefeated {
		return packets, nil
	}
	packet, err := npcraknet.PositionedEffect(
		"spacetime_ScaldronBasicSinkhole_well_fizzle.ServerEventDef",
		source.Plan.Position,
	)
	if err != nil {
		return nil, fmt.Errorf("sinkholeFizzleEffect: %w", err)
	}
	return append(packets, packet), nil
}

type campaignScaldronSinkholeEnd struct {
	runtime          campaignNPCActionRuntime
	packet           raknet.Packet
	sessionKey       string
	generation       uint64
	objectID         uint32
	actionGeneration uint64
	timestamp        uint64
	profile          zonenpc.ActionProfile
}

func (e campaignScaldronSinkholeEnd) next() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isManual := false
	isWellOnCooldown := false
	if isFound && peerSession.generation == e.generation && peerSession.zone != nil {
		actor, isActorFound := peerSession.zone.NPCs().NPC(e.objectID)
		isManual = isActorFound && actor.IsScenarioControlled() &&
			!actor.IsScenarioAIEnabled() && actor.IsActionStarted &&
			actor.ActionGeneration == e.actionGeneration
		isWellOnCooldown = peerSession.campaignNPCSinkholeReadiness[e.objectID] > e.timestamp
	}
	e.runtime.registry.mutex.RUnlock()
	if isManual && !isWellOnCooldown {
		e.runtime.finishScenarioNPCManualAction(e.sessionKey, e.generation,
			e.objectID, e.actionGeneration)
		return nil, nil
	}
	packets, err := e.runtime.produceZelemShot(
		e.packet, e.sessionKey, e.generation, e.objectID, e.timestamp,
	)
	if err != nil {
		e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
		return nil, fmt.Errorf("sinkholeNext: %w", err)
	}
	return packets, nil
}

func (e campaignScaldronSinkholeEnd) produce() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		e.generation, e.objectID,
	)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	animationPacket, err := npcraknet.AnimationState(
		e.objectID, e.profile.EndAnimationName, e.timestamp,
	)
	if err != nil {
		e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
		return nil, fmt.Errorf("sinkholeEndAnimation: %w", err)
	}
	e.timestamp += uint64(e.profile.EndAnimationDelay / time.Millisecond)
	_, err = scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{{
		Delay: e.profile.EndAnimationDelay, Produce: e.next,
	}})
	if err != nil {
		e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
		return nil, fmt.Errorf("sinkholeRecoverySchedule: %w", err)
	}
	return [][]byte{animationPacket}, nil
}

func (r campaignNPCActionRuntime) produceScaldronBasicSinkhole(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, bool, error) {
	if sessionKey == "" || generation == 0 || objectID == 0 {
		return nil, false, errors.New("sinkhole request invalid")
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		generation, objectID,
	)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, source.TargetObjectID,
	)
	profile, isProfileFound := zonenpc.ScaldronBasicSinkholeWellProfile(
		source.Plan.NounName,
	)
	readyTimestamp := peerSession.campaignNPCSinkholeReadiness[objectID]
	isInRange := isTargetFound && zonegeometry.Distance(
		source.Plan.Position, target.Position,
	) <= profile.Range+target.FootprintRadius
	if !isSourceFound || !isProfileFound || !isInRange ||
		readyTimestamp > timestamp {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	profile = zonenpc.ApplyAffixTiming(profile, source.Plan)
	plan := zonenpc.AttackPlan{
		ActionGeneration: source.ActionGeneration,
		SourceObjectID:   objectID, TargetObjectID: target.ObjectID,
		SourcePosition: source.Plan.Position, TargetPosition: target.Position,
		Profile: profile,
	}
	if peerSession.campaignNPCSinkholeReadiness == nil {
		peerSession.campaignNPCSinkholeReadiness = make(map[uint32]uint64)
	}
	peerSession.campaignNPCSinkholeReadiness[objectID] = timestamp +
		uint64(profile.Cooldown/time.Millisecond)
	effectSlot := uint8(0)
	isEffectAllocated := false
	if r.effectPool != nil {
		effectSlot, isEffectAllocated = r.effectPool.Allocate(objectID)
	}
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	startPackets, err := r.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		if isEffectAllocated && r.effectPool != nil {
			isEffectReleased := r.effectPool.Release(objectID, effectSlot)
			if !isEffectReleased {
				// The slot was concurrently reclaimed with the failed action.
			}
		}
		r.releaseAction(sessionKey, generation, objectID)
		return nil, false, fmt.Errorf("sinkholeStart: %w", err)
	}
	reductionExpiresAt := r.now().Add(
		profile.HitDelay + time.Second + scaldronSinkholeActiveDuration,
	)
	producers := make([]raknet.ScheduledPacketProducer, 0, 123)
	startup := campaignScaldronSinkholePulse{
		runtime: r, sessionKey: sessionKey, generation: generation,
		objectID: objectID, effectSlot: effectSlot, isEffectAllocated: isEffectAllocated,
	}
	producers = append(producers, raknet.ScheduledPacketProducer{Delay: profile.HitDelay, Produce: startup.startup})
	pulseCount := int(scaldronSinkholeActiveDuration / scaldronSinkholePulseDuration)
	for pulseIndex := 0; pulseIndex < pulseCount; pulseIndex++ {
		delay := profile.HitDelay + time.Second +
			time.Duration(pulseIndex)*scaldronSinkholePulseDuration
		pulse := campaignScaldronSinkholePulse{
			runtime: r, sessionKey: sessionKey, generation: generation,
			objectID: objectID, timestamp: timestamp + uint64(delay/time.Millisecond),
			profile: profile, reductionExpiresAt: reductionExpiresAt,
			effectSlot: effectSlot, isEffectAllocated: isEffectAllocated,
			isFirst: pulseIndex == 0,
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: delay, Produce: pulse.produce,
		})
	}
	fizzle := campaignScaldronSinkholeFizzle{
		runtime: r, sessionKey: sessionKey, generation: generation,
		objectID: objectID, reductionExpiresAt: reductionExpiresAt,
		effectSlot: effectSlot, isEffectAllocated: isEffectAllocated,
	}
	producers = append(producers, raknet.ScheduledPacketProducer{
		Delay:   profile.HitDelay + time.Second + scaldronSinkholeActiveDuration,
		Produce: fizzle.produce,
	})
	end := campaignScaldronSinkholeEnd{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID,
		actionGeneration: source.ActionGeneration,
		timestamp:        timestamp + uint64((profile.ReleaseDelay-2*time.Second)/time.Millisecond),
		profile:          profile,
	}
	producers = append(producers, raknet.ScheduledPacketProducer{
		Delay: profile.ReleaseDelay - 2*time.Second, Produce: end.produce,
	})
	_, scheduleErr := scheduleNPCProducers(r.registry, packet, producers)
	if scheduleErr != nil {
		if isEffectAllocated && r.effectPool != nil {
			isEffectReleased := r.effectPool.Release(objectID, effectSlot)
			if !isEffectReleased {
				// The slot was concurrently reclaimed with the failed schedule.
			}
		}
		r.releaseAction(sessionKey, generation, objectID)
		return nil, false, fmt.Errorf("sinkholeSchedule: %w", scheduleErr)
	}
	if isEffectAllocated {
		r.registry.mutex.Lock()
		latestSession, isLatestFound := r.registry.sessions[sessionKey]
		isLatest := isLatestFound && latestSession.generation == generation
		if isLatest {
			if latestSession.campaignNPCSinkholeEffectSlots == nil {
				latestSession.campaignNPCSinkholeEffectSlots = make(map[uint32]uint8)
			}
			latestSession.campaignNPCSinkholeEffectSlots[objectID] = effectSlot
			r.registry.sessions[sessionKey] = latestSession
		}
		r.registry.mutex.Unlock()
		if !isLatest && r.effectPool != nil {
			isEffectReleased := r.effectPool.Release(objectID, effectSlot)
			if !isEffectReleased {
				// The replacement session already reclaimed this unused slot.
			}
		}
	}
	return startPackets, true, nil
}

func (s *gameplayPeerSession) stopCampaignSinkholeEffect(
	objectID uint32, effectPool *attachedEffectPool,
) ([]byte, error) {
	if s == nil || objectID == 0 || effectPool == nil {
		return nil, nil
	}
	effectSlot, isFound := s.campaignNPCSinkholeEffectSlots[objectID]
	if !isFound {
		return nil, nil
	}
	delete(s.campaignNPCSinkholeEffectSlots, objectID)
	isEffectReleased := effectPool.Release(objectID, effectSlot)
	if !isEffectReleased {
		// The hard-stop packet is still required after slot ownership is reclaimed.
	}
	packet, err := npcraknet.ForcedMovementEffect(objectID, effectSlot, "", true)
	if err != nil {
		return nil, fmt.Errorf("sinkholeDeathRemove: %w", err)
	}
	return packet, nil
}

func (e campaignScaldronSinkholePulse) startup() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && member.isCampaignNPCSourceActive(e.generation, e.objectID)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent || !e.isEffectAllocated {
		return nil, nil
	}
	packet, err := npcraknet.ForcedMovementEffect(e.objectID, e.effectSlot,
		"spacetime_ScaldronBasicSinkhole_well_startup.ServerEventDef", false)
	if err != nil {
		return nil, fmt.Errorf("sinkholeStartup: %w", err)
	}
	return [][]byte{packet}, nil
}
