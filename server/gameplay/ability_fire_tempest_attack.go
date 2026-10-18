package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const fireTempestPetPrimaryStatOffset = float32(1)

type fireTempestPetAttackStep struct {
	runtime            campaignDamageRuntime
	packet             raknet.Packet
	sessionKey         string
	generation         uint64
	sourceTime         uint64
	ownerRun           *fireTempestActiveRun
	zone               *zone.Zone
	attackRevision     uint64
	movementRevision   uint64
	plan               zonecompanion.Attack
	projectileObjectID uint32
	damage             float32
	launchDeadline     time.Duration
	impactDeadline     time.Duration
	repeatDeadline     time.Duration
	launchPosition     game.Vec3
	targetPosition     game.Vec3
	travelDistance     float32
	geometry           zonenpc.ProjectileGeometry
	run                *abilityraknet.ProjectileRun
}

func (r campaignDamageRuntime) startFireTempestPetAttack(
	packet raknet.Packet, sessionKey string, generation uint64, sourceTime uint64,
) ([][]byte, error) {
	if packet.ScheduleGroup == nil && packet.ScheduleGroupResult == nil {
		return nil, errors.New("fire tempest pet schedule unavailable")
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	ownerRun := peerSession.fireTempestActive
	isCurrent := isFound && peerSession.generation == generation && ownerRun != nil &&
		ownerRun.attack == nil && peerSession.zone != nil &&
		ownerRun.generation == generation &&
		peerSession.deployedObjectID == ownerRun.ownerObjectID &&
		peerSession.deployedHitPoint() > 0 &&
		peerSession.zone.Companion() != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	ability := r.npc.program.FireTempestPetBasic
	if ability.ReleaseDelay < ability.HitDelay {
		ability.ReleaseDelay = ability.HitDelay
	}
	plan, isPlanFound, err := peerSession.zone.Companion().ReserveActorAttack(
		ownerRun.petObjectID, peerSession.zone.NPCs().LiveSnapshots(),
		ability.Range, r.npc.now(), ability.Cooldown,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("fireTempestPetReserve: %w", err)
	}
	if !isPlanFound {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	pet, isPetFound := peerSession.zone.Companion().Snapshot(plan.ObjectID)
	if !isPetFound || pet.HitPoint <= 0 ||
		pet.UserID != peerSession.binding.UserID ||
		pet.PeerGeneration != generation ||
		pet.OwnerObjectID != ownerRun.ownerObjectID ||
		peerSession.deployedObjectID != ownerRun.ownerObjectID {
		peerSession.zone.Companion().ReleaseAttack(plan.ObjectID, plan.TargetObjectID)
		r.registry.mutex.Unlock()
		return nil, nil
	}
	ability = applyCompanionAttackBuff(ability, plan)
	target, isTargetFound := peerSession.zone.NPCs().NPC(plan.TargetObjectID)
	if !isTargetFound || target.IsDefeated || target.HitPoint <= 0 {
		peerSession.zone.Companion().ReleaseAttack(plan.ObjectID, plan.TargetObjectID)
		r.registry.mutex.Unlock()
		return nil, nil
	}
	geometry, _, geometryErr := campaignTargetProjectileGeometry(r.npc.program, target)
	if geometryErr != nil {
		peerSession.zone.Companion().ReleaseAttack(plan.ObjectID, plan.TargetObjectID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("fireTempestPetGeometry: %w", geometryErr)
	}
	projectileObjectID, objectIDErr := peerSession.reserveCampaignProjectileIDs(1, 2000)
	if objectIDErr != nil {
		peerSession.zone.Companion().ReleaseAttack(plan.ObjectID, plan.TargetObjectID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("fireTempestPetProjectileID: %w", objectIDErr)
	}
	creature := peerSession.binding.Creatures[peerSession.deployedCreatureIndex]
	damageRange, damageErr := game.ResolveAbilityDamageRange(
		game.AbilityDamage{
			Minimum: ability.MinimumDamage, Maximum: ability.MaximumDamage,
			Coefficient: ability.DamageCoefficient,
		},
		game.DamageProfile{
			PrimaryAttribute:        creature.PetDamage + fireTempestPetPrimaryStatOffset,
			IsPrimaryAttributeFound: true,
		},
	)
	if damageErr != nil {
		peerSession.zone.Companion().ReleaseAttack(plan.ObjectID, plan.TargetObjectID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("fireTempestPetDamageRange: %w", damageErr)
	}
	damage, damageErr := sim.SelectRankDamage(
		peerSession.zone.NPCRandom(),
		sim.DamageRange{Minimum: damageRange.Minimum, Maximum: damageRange.Maximum},
	)
	if damageErr != nil {
		peerSession.zone.Companion().ReleaseAttack(plan.ObjectID, plan.TargetObjectID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("fireTempestPetDamage: %w", damageErr)
	}
	targetPosition := campaignProjectileAimPosition(
		plan.TargetPosition, target.Plan.NPCProfile.FootprintRadius, geometry,
	)
	launchPosition := campaignProjectileLaunchPosition(
		plan.Position, targetPosition, fireTempestFallbackFootprint,
	)
	distance := launchPosition.Sub(targetPosition).Length()
	travelDuration := time.Duration(float64(distance/ability.Speed) * float64(time.Second))
	impactDeadline := ability.HitDelay + travelDuration
	repeatDeadline := max(impactDeadline, ability.Cooldown)
	facing := targetPosition.Sub(plan.Position)
	if facing.Length() > 0 {
		facing = facing.Scale(1 / facing.Length())
	}
	projectileRun, packets, runErr := abilityraknet.NewProjectileRun(
		abilityraknet.ProjectileInput{
			Ability: ability, ActorObjectID: plan.ObjectID,
			TargetObjectID: plan.TargetObjectID, ProjectileObjectID: projectileObjectID,
			ActorPosition: sim.Position(plan.Position), TargetPosition: sim.Position(targetPosition),
			ActorFacing: sim.Position(facing), ImpactPosition: sim.Position(targetPosition),
			FootprintRadius: fireTempestFallbackFootprint, CollisionDelay: travelDuration,
			IsDirectHit: true, IsCollisionExternallyDriven: true,
			Damage: damage, TargetHitPoint: plan.TargetHitPoint,
			ActorTeam: 1, SourceTime: sourceTime,
			IsActivationSuppressed: true, IsCooldownSuppressed: true,
			IsReleaseSuppressed: true,
		},
	)
	if runErr != nil {
		peerSession.zone.Companion().ReleaseAttack(plan.ObjectID, plan.TargetObjectID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("fireTempestPetProjectile: %w", runErr)
	}
	ownerRun.attack = projectileRun
	ownerRun.attackTargetObjectID = plan.TargetObjectID
	ownerRun.attackRevision++
	ownerRun.isAttackLaunched = false
	ownerRun.isAttackResumeNeeded = false
	attackRevision := ownerRun.attackRevision
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	step := fireTempestPetAttackStep{
		runtime: r, packet: packet, sessionKey: sessionKey, generation: generation,
		sourceTime: sourceTime, ownerRun: ownerRun, plan: plan,
		zone: peerSession.zone, attackRevision: attackRevision,
		movementRevision:   pet.FollowRevision,
		projectileObjectID: projectileObjectID, damage: damage,
		launchDeadline: ability.HitDelay,
		impactDeadline: impactDeadline, launchPosition: launchPosition,
		repeatDeadline: repeatDeadline,
		targetPosition: targetPosition, travelDistance: distance,
		geometry: geometry, run: projectileRun,
	}
	producers := r.registry.producerGuard.scheduledProducers(
		sessionKey, []raknet.ScheduledPacketProducer{
			{Delay: ability.HitDelay, Produce: step.launch},
			{Delay: impactDeadline, Produce: step.impact},
		},
	)
	var cancel raknet.CancelSchedule
	if packet.ScheduleGroupResult != nil {
		cancel, err = packet.ScheduleGroupResult(producers, step.fail)
	} else {
		cancel, err = packet.ScheduleGroup(producers)
	}
	if err != nil {
		step.fail(err)
		return nil, fmt.Errorf("fireTempestPetSchedule: %w", err)
	}
	r.registry.mutex.Lock()
	latest, isLatestFound := r.registry.sessions[sessionKey]
	isAttackCurrent := step.isCurrent(latest, isLatestFound)
	if isAttackCurrent {
		projectileRun.SetCancel(cancel)
	}
	r.registry.mutex.Unlock()
	if !isAttackCurrent {
		if cancel != nil {
			cancel()
		}
		return nil, nil
	}
	r.logger.Printf(
		"RakNet projectile trajectory launched kind=fire-tempest-pet projectile=%d source=%d target=%d ability=%q origin=(%.3f,%.3f,%.3f) aim=(%.3f,%.3f,%.3f) travel=%.3f delay_ms=%d",
		projectileObjectID, plan.ObjectID, plan.TargetObjectID, ability.Name,
		plan.Position.X, plan.Position.Y, plan.Position.Z,
		targetPosition.X, targetPosition.Y, targetPosition.Z,
		distance, travelDuration.Milliseconds(),
	)
	return packets, nil
}

func (e fireTempestPetAttackStep) isCurrent(peerSession gameplayPeerSession, isFound bool) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.fireTempestActive == e.ownerRun && e.ownerRun.attack == e.run &&
		e.ownerRun.attackRevision == e.attackRevision
}

func (e fireTempestPetAttackStep) launch() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if e.ownerRun.isAttackLaunched {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	isPetCurrent := peerSession.zone == e.zone && e.zone != nil &&
		e.zone.Companion() != nil &&
		peerSession.deployedObjectID == e.ownerRun.ownerObjectID &&
		peerSession.deployedHitPoint() > 0 &&
		e.ownerRun.petObjectID == e.plan.ObjectID
	if isPetCurrent {
		pet, isPetFound := e.zone.Companion().Snapshot(e.plan.ObjectID)
		isPetCurrent = isPetFound && pet.HitPoint > 0 &&
			pet.UserID == peerSession.binding.UserID &&
			pet.PeerGeneration == e.generation &&
			pet.OwnerObjectID == e.ownerRun.ownerObjectID &&
			pet.FollowRevision == e.movementRevision &&
			pet.TargetObjectID == e.plan.TargetObjectID && pet.Position == e.plan.Position
	}
	if !isPetCurrent {
		e.ownerRun.attack = nil
		e.ownerRun.attackTargetObjectID = 0
		e.ownerRun.isAttackResumeNeeded = true
		if e.zone != nil && e.zone.Companion() != nil {
			e.zone.Companion().ReleaseAttack(e.plan.ObjectID, e.plan.TargetObjectID)
		}
		e.run.Stop()
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	// Serialize launch with teleport invalidation and pose mutation.
	packets, err := e.run.Advance(
		context.Background(), e.launchDeadline,
	)
	if err == nil {
		e.ownerRun.isAttackLaunched = true
	}
	e.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("fireTempestPetLaunch: %w", err)
	}
	return packets, nil
}

func (e fireTempestPetAttackStep) impact() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound) && peerSession.zone != nil &&
		e.ownerRun.isAttackLaunched
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	remaining := e.run.RemainingFlightDelay(e.runtime.npc.now())
	if remaining > abilityraknet.ProjectileCollisionTick {
		e.runtime.registry.mutex.Unlock()
		err := scheduleNPCProducer(e.runtime.registry, e.packet,
			min(remaining, campaignProjectileMotionPollInterval), e.impact)
		if err != nil {
			return nil, fmt.Errorf("tempestSlowResume: %w", err)
		}
		return nil, nil
	}
	target, isTargetFound := peerSession.zone.NPCs().NPC(e.plan.TargetObjectID)
	isTargetValid := isTargetFound && !target.IsDefeated && target.HitPoint > 0
	var err error
	collision := sim.ProjectileBoxCollision{Position: sim.Position(e.targetPosition)}
	if isTargetValid {
		collision, err = zonenpc.ResolveProjectileCollision(
			e.launchPosition, e.targetPosition, target.Plan.Position,
			e.travelDistance, e.geometry,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("fireTempestPetCollision: %w", err)
		}
	}
	isHit := isTargetValid && collision.IsDirectHit
	result := zonenpc.DamageResult{}
	transition := campaignDamageTransition{}
	if isHit {
		result, err = peerSession.zone.NPCs().Hit(zonenpc.HitRequest{
			SourceObjectID: e.plan.ObjectID, TargetObjectID: e.plan.TargetObjectID, Damage: e.damage,
			SourcePosition: &e.launchPosition, Metadata: zoneability.NPCDamageMetadata(e.runtime.npc.program.FireTempestPetBasic),
		})
		if err == nil {
			transition, err = peerSession.applyCampaignDamageTransition(result)
		}
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("fireTempestPetCommit: %w", err)
		}
	}
	impactPosition := game.Vec3(collision.Position)
	targetHitPoint := e.plan.TargetHitPoint
	if isTargetFound {
		targetHitPoint = target.HitPoint
	}
	facing := impactPosition.Sub(e.plan.Position)
	if facing.Length() > 0 {
		facing = facing.Scale(1 / facing.Length())
	}
	packets, err := e.run.ResolveCollision(
		context.Background(), e.impactDeadline, isHit, isHit,
		targetHitPoint, e.damage, false, sim.Position(impactPosition), sim.Position(facing),
	)
	e.ownerRun.attack = nil
	e.ownerRun.attackTargetObjectID = 0
	e.ownerRun.isAttackLaunched = false
	peerSession.zone.Companion().ReleaseAttack(e.plan.ObjectID, e.plan.TargetObjectID)
	binding := peerSession.binding
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("fireTempestPetImpact: %w", err)
	}
	packets = filterProjectileCombatPackets(packets)
	if isHit {
		resultPackets, publishErr := e.runtime.publishAreaResults(
			e.packet, e.sessionKey, e.generation, e.plan.ObjectID,
			e.sourceTime+uint64(e.impactDeadline/time.Millisecond), binding,
			[]zoneability.AreaResult{{Snapshot: target, Damage: result}},
			[]campaignDamageTransition{transition}, nil, false,
		)
		if publishErr != nil {
			return nil, fmt.Errorf("fireTempestPetPublish: %w", publishErr)
		}
		packets = append(packets, resultPackets...)
	}
	repeatDelay := e.repeatDeadline - e.impactDeadline
	if repeatDelay <= 0 {
		repeatPackets, repeatErr := e.repeat()
		if repeatErr != nil {
			return nil, fmt.Errorf("fireTempestPetRepeatImmediate: %w", repeatErr)
		}
		return append(packets, repeatPackets...), nil
	}
	producers := e.runtime.registry.producerGuard.scheduledProducers(
		e.sessionKey, []raknet.ScheduledPacketProducer{{
			Delay: repeatDelay, Produce: e.repeat,
		}},
	)
	_, err = e.packet.ScheduleProducers(producers)
	if err != nil {
		return nil, fmt.Errorf("fireTempestPetRepeatSchedule: %w", err)
	}
	return packets, nil
}

func (e fireTempestPetAttackStep) repeat() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.fireTempestActive == e.ownerRun && e.ownerRun.attack == nil &&
		e.ownerRun.attackRevision == e.attackRevision
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packets, err := e.runtime.startFireTempestPetAttack(
		e.packet, e.sessionKey, e.generation,
		e.sourceTime+uint64(e.repeatDeadline/time.Millisecond),
	)
	if err != nil {
		return nil, fmt.Errorf("fireTempestPetRepeat: %w", err)
	}
	return packets, nil
}

func (e fireTempestPetAttackStep) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		e.ownerRun.attack = nil
		e.ownerRun.attackTargetObjectID = 0
		e.ownerRun.isAttackLaunched = false
		if peerSession.zone != nil && peerSession.zone.Companion() != nil {
			peerSession.zone.Companion().ReleaseAttack(
				e.plan.ObjectID, e.plan.TargetObjectID,
			)
		}
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return
	}
	e.run.Stop()
	e.runtime.logger.Printf(
		"RakNet Fire Tempest pet attack stopped for %s: %v",
		e.sessionKey, scheduleErr,
	)
}
