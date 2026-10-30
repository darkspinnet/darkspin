package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	companionraknet "github.com/darkspinnet/darkspin/server/zone/companion/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const arenaPetTargetPoll = 500 * time.Millisecond

type arenaPetMeleeStep struct {
	pet        *arenaCombatPetRun
	plan       zonecompanion.Attack
	ability    sim.AbilityDefinition
	start      summonCompanionAttackStart
	sourceTime uint64
}

type arenaPetPursuitStep struct {
	pet  *arenaCombatPetRun
	plan zonecompanion.Pursuit
}

func (e *arenaCombatPetRun) scheduleMeleePoll(delay time.Duration) error {
	producer := raknet.ScheduledPacketProducer{Delay: delay, Produce: e.startMelee}
	producers := e.runtime.registry.producerGuard.scheduledProducers(e.sessionKey,
		[]raknet.ScheduledPacketProducer{producer})
	cancel, err := e.packet.Autonomous().ScheduleProducers(producers)
	if err == nil && cancel == nil {
		err = errors.New("pet melee poll cancellation unavailable")
	}
	if err != nil {
		return fmt.Errorf("petMeleePoll: %w", err)
	}
	e.runtime.registry.mutex.Lock()
	_, _, isCurrent := e.currentLocked()
	if isCurrent {
		if e.cycleCancel != nil {
			e.cycleCancel()
		}
		e.cycleCancel = cancel
	} else {
		cancel()
	}
	e.runtime.registry.mutex.Unlock()
	return nil
}

func (e *arenaCombatPetRun) startMelee() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	owner, _, isCurrent := e.currentLocked()
	if !isCurrent || !e.isPublished || e.attackAbility.Kind != sim.AbilityKindMelee ||
		(e.kind != arenaCombatPetBeast && e.kind != arenaCombatPetPlasma && e.kind != arenaCombatPetSage) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	e.cycleCancel = nil
	ability := e.attackAbility
	damageRange, err := game.ResolveAbilityDamageRange(game.AbilityDamage{
		Minimum: ability.MinimumDamage, Maximum: ability.MaximumDamage,
		Coefficient: ability.DamageCoefficient,
	}, game.DamageProfile{PrimaryAttribute: e.petDamage, IsPrimaryAttributeFound: true})
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaPetDamageRange: %w", err)
	}
	ability.MinimumDamage = damageRange.Minimum * (1 + e.damageBuff)
	ability.MaximumDamage = damageRange.Maximum * (1 + e.damageBuff)
	timestamp := e.packet.SourceTime + uint64(max(time.Duration(0), e.runtime.now().Sub(e.startedAt))/time.Millisecond)
	targets := e.runtime.registry.abilityTargetsLocked(e.runtime, owner, ability, timestamp).(*arenaAbilityTargets)
	snapshots := targets.LiveSnapshots()
	plan, isPlanFound, err := e.world.companion.ReserveActorAttack(e.objectID, snapshots,
		ability.Range, e.runtime.now(), ability.HitDelay+ability.Cooldown)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaPetReserve: %w", err)
	}
	if !isPlanFound {
		pursuit, isPursuitFound, pursuitErr := e.world.companion.ReserveActorPursuit(
			e.objectID, snapshots, ability.Range, zonecompanion.CompatibilityAggroRadius,
			zonecompanion.CompatibilityMovementSpeed, e.runtime.now())
		e.runtime.registry.mutex.Unlock()
		if pursuitErr != nil {
			return nil, fmt.Errorf("arenaPetPursuit: %w", pursuitErr)
		}
		if !isPursuitFound {
			err = e.scheduleMeleePoll(arenaPetTargetPoll)
			if err != nil {
				return nil, fmt.Errorf("arenaPetRetry: %w", err)
			}
			return nil, nil
		}
		packets, marshalErr := companionraknet.Pursuit(pursuit)
		if marshalErr != nil {
			e.world.companion.CancelPursuit(pursuit.ObjectID, pursuit.TargetObjectID, pursuit.Position, pursuit.Revision)
			return nil, fmt.Errorf("arenaPetPursuitPacket: %w", marshalErr)
		}
		step := arenaPetPursuitStep{pet: e, plan: pursuit}
		producer := raknet.ScheduledPacketProducer{Delay: pursuit.TravelDuration, Produce: step.arrive}
		producers := e.runtime.registry.producerGuard.scheduledProducers(e.sessionKey,
			[]raknet.ScheduledPacketProducer{producer})
		cancel, scheduleErr := e.packet.Autonomous().ScheduleProducers(producers)
		if scheduleErr == nil && cancel == nil {
			scheduleErr = errors.New("pet pursuit cancellation unavailable")
		}
		if scheduleErr != nil {
			e.world.companion.CancelPursuit(pursuit.ObjectID, pursuit.TargetObjectID, pursuit.Position, pursuit.Revision)
			return nil, fmt.Errorf("arenaPetPursuitSchedule: %w", scheduleErr)
		}
		e.runtime.registry.mutex.Lock()
		_, _, isCurrent = e.currentLocked()
		if isCurrent {
			e.cycleCancel = cancel
		} else {
			cancel()
		}
		e.runtime.registry.mutex.Unlock()
		return packets, nil
	}
	ability = applyCompanionAttackBuff(ability, plan)
	start, err := newSummonCompanionAttack(summonCompanionAttackInput{
		Ability: ability, CompanionID: plan.ObjectID, TargetID: plan.TargetObjectID,
		Companion: sim.Position(plan.Position), Target: sim.Position(plan.TargetPosition),
		TargetHitPoint: plan.TargetHitPoint, Damage: ability.MinimumDamage,
		SourceTime: timestamp, CenterRange: plan.CenterRange, StartedAt: e.runtime.now(),
		ActorTeam: uint8(owner.binding.Team),
	})
	if err != nil {
		e.world.companion.ReleaseAttack(plan.ObjectID, plan.TargetObjectID)
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaPetMeleeStart: %w", err)
	}
	e.activeAttack = start.Run
	e.runtime.registry.mutex.Unlock()
	step := arenaPetMeleeStep{pet: e, plan: plan, ability: ability,
		start: start, sourceTime: timestamp}
	repeatDelay := max(ability.ReleaseDelay, ability.HitDelay+ability.Cooldown)
	producers := []raknet.ScheduledPacketProducer{
		{Delay: ability.HitDelay, Produce: step.hit},
		{Delay: ability.ReleaseDelay, Produce: step.release},
		{Delay: repeatDelay, Produce: step.repeat},
	}
	producers = e.runtime.registry.producerGuard.scheduledProducers(e.sessionKey, producers)
	var cancel raknet.CancelSchedule
	if e.packet.ScheduleGroupResult != nil {
		cancel, err = e.packet.ScheduleGroupResult(producers, step.fail)
	} else {
		cancel, err = e.packet.ScheduleGroup(producers)
	}
	if err == nil && cancel == nil {
		err = errors.New("pet melee cancellation unavailable")
	}
	if err != nil {
		step.fail(err)
		return nil, fmt.Errorf("arenaPetMeleeSchedule: %w", err)
	}
	e.runtime.registry.mutex.Lock()
	_, _, isCurrent = e.currentLocked()
	if isCurrent && e.activeAttack == start.Run {
		e.attackCancel = cancel
	} else {
		cancel()
	}
	e.runtime.registry.mutex.Unlock()
	return start.Packet, nil
}

func (e arenaPetPursuitStep) arrive() ([][]byte, error) {
	e.pet.runtime.registry.mutex.Lock()
	_, _, isCurrent := e.pet.currentLocked()
	if !isCurrent || !e.pet.world.companion.ReleasePursuit(
		e.plan.ObjectID, e.plan.TargetObjectID, e.plan.Revision) {
		e.pet.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	e.pet.cycleCancel = nil
	e.pet.runtime.registry.mutex.Unlock()
	return e.pet.startMelee()
}

func (e arenaPetMeleeStep) hit() ([][]byte, error) {
	e.pet.runtime.registry.mutex.Lock()
	owner, actor, isCurrent := e.pet.currentLocked()
	if !isCurrent || e.pet.activeAttack != e.start.Run {
		e.pet.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	timestamp := e.sourceTime + uint64(e.ability.HitDelay/time.Millisecond)
	targets := e.pet.runtime.registry.abilityTargetsLocked(e.pet.runtime, owner, e.ability, timestamp).(*arenaAbilityTargets)
	target, isFound := targets.NPC(e.plan.TargetObjectID)
	isHitValid := isFound && !target.IsDefeated && target.HitPoint > 0 &&
		actor.Position.Sub(target.Plan.Position).Length() <= e.plan.CenterRange
	damage := e.ability.MinimumDamage
	isCritical := false
	if isHitValid {
		random, randomErr := e.pet.runtime.registry.abilityRandomLocked(owner)
		if randomErr != nil {
			e.pet.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaPetRandom: %w", randomErr)
		}
		selectedDamage, damageErr := sim.SelectRankDamage(random, sim.DamageRange{
			Minimum: e.ability.MinimumDamage, Maximum: e.ability.MaximumDamage})
		if damageErr != nil {
			e.pet.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaPetSelect: %w", damageErr)
		}
		damage = selectedDamage
		nounName := "BeastSentinelPet"
		if e.pet.kind == arenaCombatPetPlasma {
			nounName = "PlasmaSentinelPet"
		}
		profile, isProfileFound := e.pet.runtime.program.NonPlayerCritical[util.HashID(nounName)]
		if isProfileFound || e.pet.runtime.program.Critical.DamageBonus == 0 {
			critical, criticalErr := resolveZoneNonPlayerCritical(random, profile,
				e.pet.runtime.program.Critical, damage)
			if criticalErr != nil {
				e.pet.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("arenaPetCritical: %w", criticalErr)
			}
			damage = critical.Damage
			isCritical = critical.IsCritical
		}
	}
	targetPosition := e.plan.TargetPosition
	targetHitPoint := e.plan.TargetHitPoint
	if isFound {
		targetPosition = target.Plan.Position
		targetHitPoint = target.HitPoint
	}
	facing := targetPosition.Sub(actor.Position)
	if facing.Length() > 0 {
		facing = facing.Scale(1 / facing.Length())
	}
	err := e.start.Run.PrepareHit(isHitValid, max(targetHitPoint, damage), damage,
		isCritical, sim.Position(targetPosition), sim.Position(facing))
	if err != nil {
		e.pet.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaPetPrepare: %w", err)
	}
	results := make([]zoneability.AreaResult, 0, 1)
	if isHitValid {
		result, hitErr := targets.Hit(zonenpc.HitRequest{
			SourceObjectID: e.pet.objectID, TargetObjectID: target.Plan.ObjectID,
			Damage: damage, SourcePosition: &actor.Position,
			Metadata: zoneability.NPCDamageMetadata(e.ability),
		})
		if hitErr != nil {
			e.pet.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaPetHit: %w", hitErr)
		}
		results = append(results, zoneability.AreaResult{
			Snapshot: target, Damage: result, IsCritical: isCritical, Definition: e.ability})
	}
	damagePackets, err := targets.packets(results)
	e.pet.runtime.registry.mutex.Unlock()
	targets.finish()
	if err != nil {
		return nil, fmt.Errorf("arenaPetPackets: %w", err)
	}
	posePackets, err := e.start.Run.Advance(context.Background(), e.start.Run.HitDeadline())
	if err != nil {
		return nil, fmt.Errorf("arenaPetHitPose: %w", err)
	}
	posePackets = summonCompanionPresentationPackets(posePackets)
	return append(posePackets, damagePackets...), nil
}

func (e arenaPetMeleeStep) release() ([][]byte, error) {
	e.pet.runtime.registry.mutex.Lock()
	_, _, isCurrent := e.pet.currentLocked()
	if !isCurrent || e.pet.activeAttack != e.start.Run {
		e.pet.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	packets, err := e.start.Run.Advance(context.Background(), e.start.Run.ReleaseDeadline())
	if err == nil {
		e.pet.activeAttack = nil
		e.pet.world.companion.ReleaseAttack(e.plan.ObjectID, e.plan.TargetObjectID)
	}
	e.pet.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("arenaPetRelease: %w", err)
	}
	return packets, nil
}

func (e arenaPetMeleeStep) repeat() ([][]byte, error) {
	e.pet.runtime.registry.mutex.Lock()
	_, _, isCurrent := e.pet.currentLocked()
	if isCurrent {
		e.pet.attackCancel = nil
	}
	e.pet.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	return e.pet.startMelee()
}

func (e arenaPetMeleeStep) fail(cause error) {
	e.pet.runtime.registry.mutex.Lock()
	_, _, isCurrent := e.pet.currentLocked()
	if isCurrent && e.pet.activeAttack == e.start.Run {
		e.start.Run.Stop()
		e.pet.activeAttack = nil
		e.pet.attackCancel = nil
		e.pet.world.companion.ReleaseAttack(e.plan.ObjectID, e.plan.TargetObjectID)
	}
	e.pet.runtime.registry.mutex.Unlock()
	if cause != nil && e.pet.runtime.logger != nil {
		e.pet.runtime.logger.Printf("Arena pet melee failed object=%d: %v", e.pet.objectID, cause)
	}
}
