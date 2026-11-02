package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type arenaFirePetAttackStep struct {
	run            *fireTempestActiveRun
	pet            *arenaCombatPetRun
	plan           zonecompanion.Attack
	projectile     *abilityraknet.ProjectileRun
	ability        sim.AbilityDefinition
	geometry       zonenpc.ProjectileGeometry
	launchPosition game.Vec3
	targetPosition game.Vec3
	distance       float32
	damage         float32
	sourceTime     uint64
	launchDelay    time.Duration
	impactDelay    time.Duration
	repeatDelay    time.Duration
}

func (r campaignDamageRuntime) startArenaFireTempestPetAttack(
	packet raknet.Packet, sessionKey string, generation uint64, sourceTime uint64,
) ([][]byte, error) {
	r.registry.mutex.Lock()
	member, isFound := r.registry.sessions[sessionKey]
	run := member.fireTempestActive
	if !isFound || member.generation != generation || run == nil || run.arenaWorld == nil ||
		run.attack != nil || member.deployedObjectID != run.ownerObjectID ||
		member.deployedHitPoint() <= 0 || !member.isArenaCombatActive {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	match := r.registry.arenaMatches[run.arenaGameID]
	pet := run.arenaWorld.combatPets[run.petObjectID]
	if match == nil || !match.isCombatActive || match.abilityWorld != run.arenaWorld ||
		pet == nil || !pet.isPublished {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	ability := r.npc.program.FireTempestPetBasic
	if ability.ReleaseDelay < ability.HitDelay {
		ability.ReleaseDelay = ability.HitDelay
	}
	targets := r.registry.abilityTargetsLocked(run.runtime, member, ability, sourceTime).(*arenaAbilityTargets)
	plan, isPlanFound, err := run.arenaWorld.companion.ReserveActorAttack(
		pet.objectID, targets.LiveSnapshots(), ability.Range, r.npc.now(), ability.Cooldown)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaFireReserve: %w", err)
	}
	if !isPlanFound {
		r.registry.mutex.Unlock()
		err = r.scheduleArenaFirePetPoll(packet, sessionKey, generation, sourceTime, fireTempestOwnerPoll)
		if err != nil {
			return nil, fmt.Errorf("arenaFireRetry: %w", err)
		}
		return nil, nil
	}
	target, isTargetFound := targets.NPC(plan.TargetObjectID)
	if !isTargetFound || target.IsDefeated || target.HitPoint <= 0 {
		run.arenaWorld.companion.ReleaseAttack(plan.ObjectID, plan.TargetObjectID)
		r.registry.mutex.Unlock()
		return nil, nil
	}
	geometry, _, err := campaignTargetProjectileGeometry(r.npc.program, target)
	if err != nil {
		run.arenaWorld.companion.ReleaseAttack(plan.ObjectID, plan.TargetObjectID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaFireGeometry: %w", err)
	}
	projectileObjectID, err := r.registry.reserveAbilityObjectIDsLocked(&member, 1, 2000)
	if err != nil {
		run.arenaWorld.companion.ReleaseAttack(plan.ObjectID, plan.TargetObjectID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaFireProjectileID: %w", err)
	}
	ability = applyCompanionAttackBuff(ability, plan)
	damageRange, err := game.ResolveAbilityDamageRange(game.AbilityDamage{
		Minimum: ability.MinimumDamage, Maximum: ability.MaximumDamage,
		Coefficient: ability.DamageCoefficient},
		game.DamageProfile{PrimaryAttribute: pet.petDamage, IsPrimaryAttributeFound: true})
	if err != nil {
		run.arenaWorld.companion.ReleaseAttack(plan.ObjectID, plan.TargetObjectID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaFireDamageRange: %w", err)
	}
	damage, err := sim.SelectRankDamage(match.random, sim.DamageRange{
		Minimum: damageRange.Minimum, Maximum: damageRange.Maximum})
	if err != nil {
		run.arenaWorld.companion.ReleaseAttack(plan.ObjectID, plan.TargetObjectID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaFireDamage: %w", err)
	}
	targetPosition := campaignProjectileAimPosition(plan.TargetPosition,
		target.Plan.NPCProfile.FootprintRadius, geometry)
	launchPosition := campaignProjectileLaunchPosition(plan.Position,
		targetPosition, fireTempestFallbackFootprint)
	distance := launchPosition.Sub(targetPosition).Length()
	if ability.Speed <= 0 {
		run.arenaWorld.companion.ReleaseAttack(plan.ObjectID, plan.TargetObjectID)
		r.registry.mutex.Unlock()
		return nil, errors.New("Arena Fire projectile speed unavailable")
	}
	travelDuration := time.Duration(float64(distance/ability.Speed) * float64(time.Second))
	impactDelay := ability.HitDelay + travelDuration
	facing := targetPosition.Sub(plan.Position)
	if facing.Length() > 0 {
		facing = facing.Scale(1 / facing.Length())
	}
	projectile, packets, err := abilityraknet.NewProjectileRun(abilityraknet.ProjectileInput{
		Ability: ability, ActorObjectID: pet.objectID, TargetObjectID: plan.TargetObjectID,
		ProjectileObjectID: projectileObjectID, ActorPosition: sim.Position(plan.Position),
		TargetPosition: sim.Position(targetPosition), ActorFacing: sim.Position(facing),
		ImpactPosition: sim.Position(targetPosition), FootprintRadius: fireTempestFallbackFootprint,
		CollisionDelay: travelDuration, IsDirectHit: true, IsCollisionExternallyDriven: true,
		Damage: damage, TargetHitPoint: plan.TargetHitPoint,
		ActorTeam: uint8(member.binding.Team), SourceTime: sourceTime,
		IsActivationSuppressed: true, IsCooldownSuppressed: true, IsReleaseSuppressed: true,
	})
	if err != nil {
		run.arenaWorld.companion.ReleaseAttack(plan.ObjectID, plan.TargetObjectID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaFireProjectile: %w", err)
	}
	run.attack = projectile
	run.attackTargetObjectID = plan.TargetObjectID
	run.isAttackLaunched = false
	r.registry.mutex.Unlock()
	step := arenaFirePetAttackStep{run: run, pet: pet, plan: plan, projectile: projectile,
		ability: ability, geometry: geometry, launchPosition: launchPosition,
		targetPosition: targetPosition, distance: distance, damage: damage,
		sourceTime: sourceTime, launchDelay: ability.HitDelay,
		impactDelay: impactDelay, repeatDelay: max(impactDelay, ability.Cooldown)}
	producers := r.registry.producerGuard.scheduledProducers(sessionKey,
		[]raknet.ScheduledPacketProducer{{Delay: step.launchDelay, Produce: step.launch},
			{Delay: step.impactDelay, Produce: step.impact}})
	var cancel raknet.CancelSchedule
	if packet.ScheduleGroupResult != nil {
		cancel, err = packet.ScheduleGroupResult(producers, step.fail)
	} else {
		cancel, err = packet.ScheduleGroup(producers)
	}
	if err == nil && cancel == nil {
		err = errors.New("Arena Fire attack cancellation unavailable")
	}
	if err != nil {
		step.fail(err)
		return nil, fmt.Errorf("arenaFireSchedule: %w", err)
	}
	projectile.SetCancel(cancel)
	return packets, nil
}

func (r campaignDamageRuntime) scheduleArenaFirePetPoll(packet raknet.Packet, sessionKey string,
	generation uint64, sourceTime uint64, delay time.Duration) error {
	step := arenaFirePetPollStep{runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, sourceTime: sourceTime + uint64(delay/time.Millisecond)}
	producer := raknet.ScheduledPacketProducer{Delay: delay, Produce: step.produce}
	producers := r.registry.producerGuard.scheduledProducers(sessionKey, []raknet.ScheduledPacketProducer{producer})
	_, err := packet.Autonomous().ScheduleProducers(producers)
	if err != nil {
		return fmt.Errorf("arenaFirePoll: %w", err)
	}
	return nil
}

type arenaFirePetPollStep struct {
	runtime    campaignDamageRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	sourceTime uint64
}

func (e arenaFirePetPollStep) produce() ([][]byte, error) {
	return e.runtime.startArenaFireTempestPetAttack(e.packet, e.sessionKey, e.generation, e.sourceTime)
}

func (e arenaFirePetAttackStep) currentLocked() bool {
	member, isFound := e.run.runtime.registry.sessions[e.run.sessionKey]
	match := e.run.runtime.registry.arenaMatches[e.run.arenaGameID]
	return isFound && member.generation == e.run.generation && member.fireTempestActive == e.run &&
		member.binding.Mode == game.ModeArena && member.binding.GameID == e.run.arenaGameID &&
		member.deployedObjectID == e.run.ownerObjectID && member.deployedHitPoint() > 0 &&
		match != nil && match.isCombatActive && match.abilityWorld == e.run.arenaWorld &&
		e.run.arenaWorld.combatPets[e.pet.objectID] == e.pet && e.pet.isPublished &&
		e.run.attack == e.projectile
}

func (e arenaFirePetAttackStep) launch() ([][]byte, error) {
	e.run.runtime.registry.mutex.Lock()
	if !e.currentLocked() {
		e.run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	packets, err := e.projectile.Advance(context.Background(), e.launchDelay)
	if err == nil {
		e.run.isAttackLaunched = true
	}
	e.run.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("arenaFireLaunch: %w", err)
	}
	return packets, nil
}

func (e arenaFirePetAttackStep) impact() ([][]byte, error) {
	e.run.runtime.registry.mutex.Lock()
	if !e.currentLocked() || !e.run.isAttackLaunched {
		e.run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	remaining := e.projectile.RemainingFlightDelay(e.run.runtime.now())
	if remaining > abilityraknet.ProjectileCollisionTick {
		e.run.runtime.registry.mutex.Unlock()
		err := scheduleNPCProducer(e.run.runtime.registry, e.run.packet,
			min(remaining, campaignProjectileMotionPollInterval), e.impact)
		if err != nil {
			return nil, fmt.Errorf("arenaFireSlowResume: %w", err)
		}
		return nil, nil
	}
	member := e.run.runtime.registry.sessions[e.run.sessionKey]
	targets := e.run.runtime.registry.abilityTargetsLocked(e.run.runtime, member, e.ability,
		e.sourceTime+uint64(e.impactDelay/time.Millisecond)).(*arenaAbilityTargets)
	target, isFound := targets.NPC(e.plan.TargetObjectID)
	isTargetValid := isFound && !target.IsDefeated && target.HitPoint > 0
	collision := sim.ProjectileBoxCollision{Position: sim.Position(e.targetPosition)}
	var err error
	if isTargetValid {
		collision, err = zonenpc.ResolveProjectileCollision(e.launchPosition,
			e.targetPosition, target.Plan.Position, e.distance, e.geometry)
		if err != nil {
			e.run.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaFireCollision: %w", err)
		}
	}
	isHit := isTargetValid && collision.IsDirectHit
	results := make([]zoneability.AreaResult, 0, 1)
	if isHit {
		result, hitErr := targets.Hit(zonenpc.HitRequest{
			SourceObjectID: e.pet.objectID, TargetObjectID: target.Plan.ObjectID,
			Damage: e.damage, SourcePosition: &e.launchPosition,
			Metadata: zoneability.NPCDamageMetadata(e.ability)})
		if hitErr != nil {
			e.run.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaFireHit: %w", hitErr)
		}
		results = append(results, zoneability.AreaResult{Snapshot: target, Damage: result,
			Definition: e.ability})
	}
	impactPosition := game.Vec3(collision.Position)
	targetHitPoint := e.plan.TargetHitPoint
	if isFound {
		targetHitPoint = target.HitPoint
	}
	facing := impactPosition.Sub(e.plan.Position)
	if facing.Length() > 0 {
		facing = facing.Scale(1 / facing.Length())
	}
	projectilePackets, err := e.projectile.ResolveCollision(context.Background(), e.impactDelay,
		isHit, isHit, targetHitPoint, e.damage, false,
		sim.Position(impactPosition), sim.Position(facing))
	e.run.attack = nil
	e.run.attackTargetObjectID = 0
	e.run.isAttackLaunched = false
	e.run.arenaWorld.companion.ReleaseAttack(e.plan.ObjectID, e.plan.TargetObjectID)
	damagePackets, packetErr := targets.packets(results)
	e.run.runtime.registry.mutex.Unlock()
	targets.finish()
	if err != nil {
		return nil, fmt.Errorf("arenaFireImpact: %w", err)
	}
	if packetErr != nil {
		return nil, fmt.Errorf("arenaFireDamagePacket: %w", packetErr)
	}
	packets := append(filterProjectileCombatPackets(projectilePackets), damagePackets...)
	repeat := arenaFirePetPollStep{runtime: e.run.runtime.damage, packet: e.run.packet,
		sessionKey: e.run.sessionKey, generation: e.run.generation,
		sourceTime: e.sourceTime + uint64(e.repeatDelay/time.Millisecond)}
	producer := raknet.ScheduledPacketProducer{Delay: max(time.Millisecond, e.repeatDelay-e.impactDelay), Produce: repeat.produce}
	producers := e.run.runtime.registry.producerGuard.scheduledProducers(e.run.sessionKey,
		[]raknet.ScheduledPacketProducer{producer})
	_, err = e.run.packet.Autonomous().ScheduleProducers(producers)
	if err != nil {
		return nil, fmt.Errorf("arenaFireRepeat: %w", err)
	}
	return packets, nil
}

func (e arenaFirePetAttackStep) fail(cause error) {
	e.run.runtime.registry.mutex.Lock()
	if e.currentLocked() {
		e.run.attack = nil
		e.run.attackTargetObjectID = 0
		e.run.isAttackLaunched = false
		e.run.arenaWorld.companion.ReleaseAttack(e.plan.ObjectID, e.plan.TargetObjectID)
	}
	e.run.runtime.registry.mutex.Unlock()
	e.projectile.Stop()
	if cause != nil && e.run.runtime.logger != nil {
		e.run.runtime.logger.Printf("Arena Fire pet projectile failed object=%d: %v", e.pet.objectID, cause)
	}
}
