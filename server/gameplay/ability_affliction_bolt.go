package gameplay

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const afflictionBoltScanInterval = 100 * time.Millisecond
const afflictionBoltRetargetRange = 20
const afflictionBoltTurnRate = 6

type heroAfflictionBoltContact struct {
	targetID   uint32
	instanceID uint32
	expiresAt  time.Time
	deadline   time.Duration
}

type heroAfflictionBoltStep struct {
	schedule  heroProjectileStatusSchedule
	deadline  time.Duration
	targetID  uint32
	expiresAt time.Time
}

func (e heroAfflictionBoltStep) scan() ([][]byte, error) {
	e.schedule.runtime.registry.mutex.Lock()
	peerSession, isFound := e.schedule.runtime.registry.sessions[e.schedule.sessionKey]
	if !e.schedule.isCurrent(peerSession, isFound) {
		e.schedule.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	snapshot := e.schedule.run.projectile.Snapshot(e.schedule.runtime.now())
	if !snapshot.IsActive {
		e.schedule.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	previousPosition, currentPosition := e.schedule.run.advanceScanPosition(
		game.Vec3(snapshot.Position),
	)
	contacts := make([]heroAfflictionBoltContact, 0)
	liveNPCs := peerSession.zone.NPCs().LiveSnapshots()
	for _, candidate := range liveNPCs {
		objectID := candidate.Plan.ObjectID
		contactPosition := candidate.Plan.Position
		contactPosition.Z += candidate.Plan.NPCProfile.FootprintRadius
		if candidate.Plan.IsFixture || !candidate.IsPublished || candidate.IsDefeated ||
			candidate.HitPoint <= 0 ||
			candidate.Faction != zonenpc.FactionNonPlayerAligned ||
			e.schedule.run.HasSplash(objectID) ||
			peerSession.zone.NPCs().IsStealthed(objectID) ||
			peerSession.zone.NPCs().CurseRemaining(
				objectID, e.schedule.runtime.now(),
			) > 0 || !zonegeometry.SegmentIntersectsSphere(
			previousPosition, currentPosition, contactPosition,
			e.schedule.definition.Radius,
		) {
			continue
		}
		expiresAt := e.schedule.runtime.now().Add(
			e.schedule.definition.StatusDuration,
		)
		err := peerSession.zone.NPCs().ApplyCurse(objectID, expiresAt)
		if err != nil {
			e.schedule.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("afflictionCurse[%d]: %w", objectID, err)
		}
		instanceID, err := e.schedule.runtime.modifierPool.Allocate()
		if err != nil {
			peerSession.zone.NPCs().ClearCurse(objectID, expiresAt)
			e.schedule.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("afflictionModifier[%d]: %w", objectID, err)
		}
		if !e.schedule.run.ApplySplash(objectID, instanceID, expiresAt) {
			peerSession.zone.NPCs().ClearCurse(objectID, expiresAt)
			_ = e.schedule.runtime.modifierPool.Release(instanceID)
			continue
		}
		contacts = append(contacts, heroAfflictionBoltContact{
			targetID: objectID, instanceID: instanceID, expiresAt: expiresAt,
			deadline: e.deadline,
		})
	}
	retarget := zonenpc.Snapshot{}
	retargetDistance := float32(afflictionBoltRetargetRange + 1)
	for _, candidate := range liveNPCs {
		objectID := candidate.Plan.ObjectID
		distance := zonegeometry.Distance(
			candidate.Plan.Position, game.Vec3(snapshot.Position),
		)
		if candidate.Plan.IsFixture || !candidate.IsPublished || candidate.IsDefeated ||
			candidate.HitPoint <= 0 ||
			candidate.Faction != zonenpc.FactionNonPlayerAligned ||
			e.schedule.run.HasSplash(objectID) ||
			peerSession.zone.NPCs().IsStealthed(objectID) ||
			peerSession.zone.NPCs().CurseRemaining(
				objectID, e.schedule.runtime.now(),
			) > 0 || distance > afflictionBoltRetargetRange ||
			distance >= retargetDistance {
			continue
		}
		retarget = candidate
		retargetDistance = distance
	}
	e.schedule.runtime.registry.sessions[e.schedule.sessionKey] = peerSession
	e.schedule.runtime.registry.mutex.Unlock()

	packets := make([][]byte, 0, len(contacts)+1)
	if retarget.Plan.ObjectID != 0 {
		targetPosition := sim.Position(retarget.Plan.Position)
		targetPosition.Z += retarget.Plan.NPCProfile.FootprintRadius
		retargetPackets, err := e.schedule.run.projectile.Retarget(
			e.schedule.runtime.now(), retarget.Plan.ObjectID, targetPosition,
			afflictionBoltTurnRate,
		)
		if err != nil {
			return nil, fmt.Errorf("afflictionRetarget: %w", err)
		}
		packets = append(packets, retargetPackets...)
	} else if len(contacts) > 0 {
		continuePackets, err := e.schedule.run.projectile.Retarget(
			e.schedule.runtime.now(), 0, snapshot.Position,
			afflictionBoltTurnRate,
		)
		if err != nil {
			return nil, fmt.Errorf("afflictionContinue: %w", err)
		}
		packets = append(packets, continuePackets...)
	}
	for index, contact := range contacts {
		packet, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
			TargetID:     contact.targetID,
			ModifierGUID: e.schedule.definition.RootModifierID,
			InstanceID:   contact.instanceID,
			DurationMilliseconds: uint32(
				e.schedule.definition.StatusDuration.Milliseconds(),
			),
			StackCount: 1,
			StartMilliseconds: e.schedule.packet.SourceTime +
				uint64(e.deadline/time.Millisecond),
			SourceID: e.schedule.sourceObjectID,
		})
		if err != nil {
			return nil, fmt.Errorf("afflictionModifierMarshal[%d]: %w", index, err)
		}
		packets = append(packets, packet)
		err = e.schedule.scheduleAfflictionContact(contact)
		if err != nil {
			return nil, fmt.Errorf("afflictionContactSchedule[%d]: %w", index, err)
		}
	}
	return packets, nil
}

func (e heroProjectileStatusSchedule) scheduleAfflictionContact(
	contact heroAfflictionBoltContact,
) error {
	producers := make([]raknet.ScheduledPacketProducer, 0, len(e.definition.HitDelays)+1)
	for _, tickDelay := range e.definition.HitDelays {
		step := heroAfflictionBoltStep{
			schedule: e, deadline: contact.deadline + tickDelay,
			targetID: contact.targetID, expiresAt: contact.expiresAt,
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: tickDelay, Produce: step.tick,
		})
	}
	expiry := heroAfflictionBoltStep{
		schedule: e, deadline: contact.deadline + e.definition.StatusDuration,
		targetID: contact.targetID, expiresAt: contact.expiresAt,
	}
	producers = append(producers, raknet.ScheduledPacketProducer{
		Delay: e.definition.StatusDuration, Produce: expiry.expire,
	})
	producers = e.runtime.registry.producerGuard.scheduledProducers(
		e.sessionKey, producers,
	)
	_, err := e.packet.ScheduleProducers(producers)
	if err != nil {
		deleted := e.run.RemoveSplash(contact.targetID, contact.expiresAt)
		if deleted.instanceID == 0 {
			return fmt.Errorf("contactSchedule: %w", err)
		}
		return fmt.Errorf("contactScheduleRollback: %w", err)
	}
	return nil
}

func (e heroAfflictionBoltStep) tick() ([][]byte, error) {
	e.schedule.runtime.registry.mutex.Lock()
	peerSession, isFound := e.schedule.runtime.registry.sessions[e.schedule.sessionKey]
	if !e.schedule.isCurrent(peerSession, isFound) ||
		!e.schedule.run.HasSplash(e.targetID) {
		e.schedule.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	target, isTargetFound := peerSession.zone.NPCs().NPC(e.targetID)
	if !isTargetFound || target.IsDefeated || target.HitPoint <= 0 {
		e.schedule.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	definition := e.schedule.definition
	definition.MinimumDamage = definition.MinimumDamagePerTick
	definition.MaximumDamage = definition.MaximumDamagePerTick
	plan := zoneability.AreaPlan{
		SourceObjectID: e.schedule.sourceObjectID,
		AbilityID:      e.schedule.projectileObjectID,
		Definition:     definition, Damage: e.schedule.damage,
		Center: target.Plan.Position, Target: []zonenpc.Snapshot{target},
	}
	results, err := zoneability.CommitArea(
		peerSession.zone.Population().Random(), peerSession.zone.NPCs(), plan,
		e.schedule.creature, peerSession.binding.Difficulty,
		e.schedule.runtime.program.Critical,
	)
	if err != nil {
		e.schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("afflictionDamage: %w", err)
	}
	e.schedule.run.AdvanceSplashTick(e.targetID)
	transitions := make([]campaignDamageTransition, 0, len(results))
	for index, result := range results {
		transition, transitionErr := peerSession.applyCampaignDamageTransition(
			result.Damage,
		)
		if transitionErr != nil {
			e.schedule.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("afflictionTransition[%d]: %w", index, transitionErr)
		}
		transitions = append(transitions, transition)
	}
	e.schedule.runtime.registry.sessions[e.schedule.sessionKey] = peerSession
	e.schedule.runtime.registry.mutex.Unlock()
	packets, err := e.schedule.runtime.damage.publishAreaResults(
		e.schedule.packet, e.schedule.sessionKey, e.schedule.generation,
		e.schedule.sourceObjectID,
		e.schedule.packet.SourceTime+uint64(e.deadline/time.Millisecond),
		e.schedule.binding, results, transitions, nil, false,
	)
	if err != nil {
		return nil, fmt.Errorf("afflictionPublish: %w", err)
	}
	return packets, nil
}

func (e heroAfflictionBoltStep) expire() ([][]byte, error) {
	e.schedule.runtime.registry.mutex.RLock()
	peerSession, isFound := e.schedule.runtime.registry.sessions[e.schedule.sessionKey]
	isCurrent := e.schedule.isCurrent(peerSession, isFound)
	e.schedule.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	deleted := e.schedule.run.RemoveSplash(e.targetID, e.expiresAt)
	if deleted.instanceID == 0 {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.ModifierDeletedMessage{
		TargetID: deleted.targetID, InstanceID: deleted.instanceID,
	})
	if err != nil {
		return nil, fmt.Errorf("afflictionDelete: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e heroProjectileStatusSchedule) finishAfflictionFlight() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packets, _, err := e.run.projectile.DeleteProjectile(context.Background())
	if err != nil {
		return nil, fmt.Errorf("afflictionProjectileDelete: %w", err)
	}
	return packets, nil
}

func afflictionBoltFlightDuration(definition sim.AbilityDefinition) time.Duration {
	if definition.Speed <= 0 || definition.Distance <= 0 {
		return 0
	}
	return time.Duration(
		float64(definition.Distance) / float64(definition.Speed) * float64(time.Second),
	)
}
