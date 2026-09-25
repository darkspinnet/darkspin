package gameplay

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const repulsionKnockbackDistance = float32(8)
const repulsionKnockbackSpeed = float32(20)
const repulsionKnockbackOutro = time.Second

type heroRepulsionSchedule struct {
	runtime             campaignAbilityCommandRuntime
	packet              raknet.Packet
	sessionKey          string
	generation          uint64
	areaGeneration      uint64
	sourceObjectID      uint32
	creatureIndex       uint32
	previousManaPoint   float32
	center              game.Vec3
	creature            game.GameplayCreature
	definition          sim.AbilityDefinition
	damage              game.DamageRange
	cooldownReservation zoneability.CooldownReservation
	releaseReservation  zoneaction.ReleaseReservation
	releasePacket       []byte
}

func (e heroRepulsionSchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.areaBasicGeneration == e.areaGeneration
}

func (e heroRepulsionSchedule) hit() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	packets := make([][]byte, 0)
	projectilePackets, reflectedProjectileCount, err :=
		reflectHostileProjectilesLocked(
			&peerSession, e.center, e.definition.Radius, e.runtime.now(),
		)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroRepulsionProjectile: %w", err)
	}
	packets = append(packets, projectilePackets...)
	targets := make([]zonenpc.Snapshot, 0)
	for _, target := range peerSession.zone.NPCs().LiveSnapshots() {
		if target.Faction != zonenpc.FactionNonPlayerAligned || target.Plan.IsFixture ||
			target.IsTurtleActive {
			continue
		}
		delta := target.Plan.Position.Sub(e.center)
		distance := delta.Length()
		contactRadius := e.definition.Radius +
			max(float32(0), target.Plan.NPCProfile.FootprintRadius)
		if distance > contactRadius {
			continue
		}
		targets = append(targets, target)
		if distance <= 0 {
			delta = game.Vec3{X: 1}
			distance = 1
		}
		desired := target.Plan.Position.Add(
			delta.Scale(repulsionKnockbackDistance / distance),
		)
		destination, isDestinationFound, err :=
			zoneaction.NPCDirectMovementDestination(
				peerSession.zone.Navigation(), target.Plan.Position, desired,
				max(target.Plan.NPCProfile.FootprintRadius, float32(0.25)),
			)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroRepulsionDestination: %w", err)
		}
		if !isDestinationFound {
			continue
		}
		interruptionPackets, err :=
			e.runtime.registry.interruptCampaignNPCForForcedMovementLocked(
				e.sessionKey, &peerSession, target.Plan.ObjectID,
			)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroRepulsionInterrupt: %w", err)
		}
		packets = append(packets, interruptionPackets...)
		plan := zonenpc.AttackPlan{
			SourceObjectID: e.sourceObjectID,
			TargetObjectID: target.Plan.ObjectID,
			SourcePosition: e.center,
			TargetPosition: target.Plan.Position,
			Profile: zonenpc.ActionProfile{
				ForcedMovementSpeed:        repulsionKnockbackSpeed,
				ForcedMovementDistance:     repulsionKnockbackDistance,
				ForcedMovementReactionName: "react_knockback",
			},
		}
		movementPackets, err := npcraknet.ForcedMovement(
			plan, destination,
			e.packet.SourceTime+uint64(e.definition.HitDelay/time.Millisecond),
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroRepulsionMarshal: %w", err)
		}
		err = peerSession.zone.NPCs().SetPosition(target.Plan.ObjectID, destination)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroRepulsionMove: %w", err)
		}
		packets = append(packets, movementPackets...)
		movementDistance := destination.Sub(target.Plan.Position).Length()
		movementDuration := time.Duration(
			float64(movementDistance/repulsionKnockbackSpeed) * float64(time.Second),
		)
		resetDelay := movementDuration + repulsionKnockbackOutro
		resetTimestamp := e.packet.SourceTime +
			uint64(e.definition.HitDelay/time.Millisecond) +
			uint64(resetDelay/time.Millisecond)
		reset := campaignAcceptedHitPushResetStep{
			runtime: e.runtime.damage, sessionKey: e.sessionKey,
			generation: e.generation, objectID: target.Plan.ObjectID,
			timestamp: resetTimestamp,
		}
		resetScheduleErr := errors.New("schedule unavailable")
		if e.packet.ScheduleFunc != nil {
			resetScheduleErr = e.packet.ScheduleFunc(resetDelay, reset.produce)
		}
		if resetScheduleErr == nil {
			continue
		}
		resetPacket, resetErr := npcraknet.ResetAnimation(
			target.Plan.ObjectID,
			e.packet.SourceTime+uint64(e.definition.HitDelay/time.Millisecond),
		)
		if resetErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroRepulsionResetFallback: %w", resetErr)
		}
		packets = append(packets, resetPacket)
		if e.runtime.logger != nil {
			e.runtime.logger.Printf(
				"RakNet hero Repulsion Wave knockback reset sent immediately target=%d schedule_error=%v",
				target.Plan.ObjectID, resetScheduleErr,
			)
		}
	}
	results := make([]zoneability.AreaResult, 0)
	transitions := make([]campaignDamageTransition, 0)
	if e.damage.Maximum > 0 && len(targets) > 0 {
		plan := zoneability.AreaPlan{
			SourceObjectID: e.sourceObjectID,
			AbilityID:      util.HashID(e.definition.Name),
			Definition:     e.definition,
			Damage:         e.damage,
			Center:         e.center,
			Target:         targets,
		}
		var err error
		results, err = zoneability.CommitArea(
			peerSession.zone.Population().Random(), peerSession.zone.NPCs(), plan,
			e.creature, peerSession.binding.Difficulty, e.runtime.program.Critical,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroRepulsionDamage: %w", err)
		}
		for index, result := range results {
			transition, transitionErr := peerSession.applyCampaignDamageTransition(
				result.Damage,
			)
			if transitionErr != nil {
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf(
					"heroRepulsionTransition[%d]: %w", index, transitionErr,
				)
			}
			transitions = append(transitions, transition)
		}
	}
	binding := peerSession.binding
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	if e.definition.StatusKind == sim.AbilityStatusKindSlow && len(targets) > 0 {
		targetObjectIDs := make([]uint32, 0, len(targets))
		for _, target := range targets {
			targetObjectIDs = append(targetObjectIDs, target.Plan.ObjectID)
		}
		statusPackets, err := e.runtime.applyTargetStatus(
			e.packet, e.sessionKey, e.generation, e.sourceObjectID,
			e.packet.SourceTime+uint64(e.definition.HitDelay/time.Millisecond),
			zoneability.AreaPlan{
				SourceObjectID: e.sourceObjectID,
				AbilityID:      util.HashID(e.definition.Name), Definition: e.definition,
			},
			targetObjectIDs,
		)
		if err != nil {
			return nil, fmt.Errorf("heroRepulsionSlow: %w", err)
		}
		packets = append(packets, statusPackets...)
	}
	if len(results) > 0 {
		damagePackets, err := e.runtime.damage.publishAreaResults(
			e.packet, e.sessionKey, e.generation, e.sourceObjectID,
			e.packet.SourceTime+uint64(e.definition.HitDelay/time.Millisecond),
			binding, results, transitions, nil, false,
		)
		if err != nil {
			return nil, fmt.Errorf("heroRepulsionPublish: %w", err)
		}
		packets = append(packets, damagePackets...)
	}
	if reflectedProjectileCount > 0 {
		e.runtime.logger.Printf(
			"RakNet hero Repulsion Wave reflected projectiles source=%d count=%d",
			e.sourceObjectID, reflectedProjectileCount,
		)
	}
	return packets, nil
}

func (e heroRepulsionSchedule) release() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	return [][]byte{e.releasePacket}, nil
}

func (e heroRepulsionSchedule) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		_ = peerSession.setCampaignCharacterManaPoints(
			e.creatureIndex, e.previousManaPoint,
		)
		peerSession.abilityCooldownSession().Rollback(e.cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(e.releaseReservation)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if isCurrent {
		e.runtime.logger.Printf(
			"RakNet hero repulsion stopped after schedule failure for %s: %v",
			e.sessionKey, scheduleErr,
		)
	}
}

func (r campaignAbilityCommandRuntime) handleHeroRepulsion(
	req campaignCharacterAbilityRequest, peerSession gameplayPeerSession,
	creature game.GameplayCreature, definition sim.AbilityDefinition,
	activeAbilityID uint32, sessionKey string, abilityStartTime time.Time,
) ([][]byte, error) {
	if activeAbilityID == 0 || definition.Kind != sim.AbilityKindRepulsion ||
		definition.Name != "RepulsionWave" || definition.Radius <= 0 ||
		definition.AnimationName == "" || definition.HitDelay < 0 ||
		definition.ReleaseDelay < definition.HitDelay ||
		math.IsNaN(float64(definition.Radius)) ||
		math.IsInf(float64(definition.Radius), 0) {
		r.registry.mutex.Unlock()
		return req.reject("repulsion definition unavailable")
	}
	projected, err := zoneability.ProjectTiming(creature, definition)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroRepulsionTiming: %w", err)
	}
	rank := int32(1)
	if req.command.Ability.Rank > 0 {
		rank = req.command.Ability.Rank
	}
	if rank%2 == 0 {
		projected.Cooldown = 4 * time.Second
	}
	if rank == 5 || rank == 6 {
		projected.MinimumDamage = 10
		projected.MaximumDamage = 14
		projected.DamageCoefficient = 0.04
	}
	if rank == 7 || rank == 8 {
		projected.StatusKind = sim.AbilityStatusKindSlow
		projected.StatusDuration = 4 * time.Second
		projected.StatusMovementScale = 0.50
		projected.StatusAttackScale = 1
		projected.RootModifierID = util.HashID("RepulsionWave_SlowUpgrade")
	}
	damage, err := zoneability.ProjectDamage(
		creature, projected, projected.MinimumDamage, projected.MaximumDamage,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroRepulsionDamageProjection: %w", err)
	}
	manaCost, err := game.ResolveAbilityManaCost(
		projected.ManaCost, creature.DamageProfile.PrimaryAttribute, projected.ManaCoefficient,
		peerSession.isOverdriveActiveAt(abilityStartTime),
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroRepulsionMana: %w", err)
	}
	if peerSession.deployedManaPoint() < manaCost {
		r.registry.mutex.Unlock()
		return req.reject("power unavailable")
	}
	cooldown, err := zoneability.ProjectCooldown(creature, projected)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroRepulsionCooldown: %w", err)
	}
	ackPacket, err := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp:    req.command.Common.Unknown[0],
		ResponseType: raknet.ActionResponseAccepted,
		ObjectID:     activeAbilityID, AbilityIndex: req.command.Ability.Index,
		SourceStartMilliseconds: req.packet.SourceTime,
		SourceCommitMilliseconds: req.packet.SourceTime +
			uint64(projected.HitDelay/time.Millisecond),
		SourceEndMilliseconds: req.packet.SourceTime +
			uint64(projected.ReleaseDelay/time.Millisecond),
	})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroRepulsionAck: %w", err)
	}
	releasePacket, err := abilityraknet.ReleaseResponse(
		req.command.Common.Unknown[0], activeAbilityID,
		req.command.Ability.Index, req.packet.SourceTime,
		projected.HitDelay, projected.ReleaseDelay,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroRepulsionRelease: %w", err)
	}
	remainingManaPoint := peerSession.deployedManaPoint() - manaCost
	startPackets, err := abilityraknet.StartSpendPresentation(
		req.command.Common.ObjectID, activeAbilityID, projected.AnimationName,
		cooldown, req.packet.SourceTime, remainingManaPoint,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroRepulsionStart: %w", err)
	}
	cooldownReservation, isCooldownReserved :=
		peerSession.abilityCooldownSession().Reserve(
			zoneability.HeroAbilityCooldown(activeAbilityID), abilityStartTime, cooldown,
		)
	if !isCooldownReserved {
		r.registry.mutex.Unlock()
		return req.reject("repulsion cooldown unavailable")
	}
	releaseReservation, isReleaseReserved :=
		peerSession.abilityReleaseSession().Reserve(
			abilityStartTime, projected.ReleaseDelay,
		)
	if !isReleaseReserved {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		r.registry.mutex.Unlock()
		return req.reject("repulsion release unavailable")
	}
	previousManaPoint := peerSession.deployedManaPoint()
	err = peerSession.stopPlayerMovement(abilityStartTime)
	if err == nil {
		err = peerSession.setDeployedManaPoints(remainingManaPoint)
	}
	if err != nil {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroRepulsionCommit: %w", err)
	}
	peerSession.areaBasicGeneration++
	schedule := heroRepulsionSchedule{
		runtime: r, packet: req.packet, sessionKey: sessionKey,
		generation:        peerSession.generation,
		areaGeneration:    peerSession.areaBasicGeneration,
		sourceObjectID:    req.command.Common.ObjectID,
		creatureIndex:     peerSession.deployedCreatureIndex,
		previousManaPoint: previousManaPoint,
		center:            game.Vec3(peerSession.playerPosition), creature: creature,
		definition: projected, damage: damage,
		cooldownReservation: cooldownReservation,
		releaseReservation:  releaseReservation, releasePacket: releasePacket,
	}
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	producers := r.registry.producerGuard.scheduledProducers(
		sessionKey, []raknet.ScheduledPacketProducer{
			{Delay: projected.HitDelay, Produce: schedule.hit},
			{Delay: projected.ReleaseDelay, Produce: schedule.release},
		},
	)
	if req.packet.ScheduleGroupResult != nil {
		_, err = req.packet.ScheduleGroupResult(producers, schedule.fail)
	} else if req.packet.ScheduleGroup != nil {
		_, err = req.packet.ScheduleGroup(producers)
	} else {
		err = errors.New("schedule unavailable")
	}
	if err != nil {
		schedule.fail(err)
		return nil, fmt.Errorf("heroRepulsionSchedule: %w", err)
	}
	if projected.ActivationEffectName != "" {
		effectPacket, marshalErr := abilityraknet.CursorAreaImpact(
			projected.ActivationEffectName, peerSession.playerPosition,
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("heroRepulsionEffect: %w", marshalErr)
		}
		startPackets = append(startPackets, effectPacket)
	}
	return append([][]byte{ackPacket}, startPackets...), nil
}
