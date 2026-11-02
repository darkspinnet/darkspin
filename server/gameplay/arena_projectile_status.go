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
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	"github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type arenaProjectileStatusRun struct {
	runtime             campaignAbilityCommandRuntime
	packet              raknet.Packet
	sessionKey          string
	generation          uint64
	gameID              uint32
	sourceObjectID      uint32
	targetObjectID      uint32
	projectileObjectID  uint32
	startedAt           time.Time
	definition          sim.AbilityDefinition
	creature            game.GameplayCreature
	damage              game.DamageRange
	projectile          *abilityraknet.ProjectileRun
	wrapper             *heroProjectileStatusRun
	releasePacket       []byte
	sourcePosition      game.Vec3
	targetPosition      game.Vec3
	geometry            zonenpc.ProjectileGeometry
	facing              raknet.Vector3
	travelDistance      float32
	actorFootprint      float32
	impactDeadline      time.Duration
	lastScanPosition    game.Vec3
	isScanPositionSet   bool
	creatureIndex       uint32
	previousMana        float32
	cooldownReservation zoneability.CooldownReservation
	releaseReservation  zoneaction.ReleaseReservation
}

func (e *arenaProjectileStatusRun) isCurrentLocked() (gameplayPeerSession, bool) {
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	match := e.runtime.registry.arenaMatches[e.gameID]
	return member, isFound && member.generation == e.generation &&
		member.binding.Mode == game.ModeArena && member.binding.GameID == e.gameID &&
		member.isArenaCombatActive && member.deployedObjectID == e.sourceObjectID &&
		member.heroProjectileRuns[e.projectileObjectID] == e.wrapper &&
		match != nil && match.isCombatActive
}

func (e *arenaProjectileStatusRun) queueLocked(packets [][]byte) {
	e.runtime.registry.queueFieldMedicRealmPacketsLocked(nil, e.gameID, packets)
}

func (r campaignAbilityCommandRuntime) handleArenaProjectileStatus(
	req campaignCharacterAbilityRequest, member gameplayPeerSession,
	creature game.GameplayCreature, definition sim.AbilityDefinition,
	abilityID uint32, targetObjectID uint32, sessionKey string, startedAt time.Time,
) ([][]byte, error) {
	if abilityID == 0 || definition.Kind != sim.AbilityKindProjectileStatus ||
		(definition.Name != "AfflictionBolt" && definition.Name != "Terrify") ||
		definition.Range <= 0 || definition.Speed <= 0 || definition.StatusDuration <= 0 ||
		definition.RootModifierID == 0 || definition.ProjectileNoun == "" {
		r.registry.mutex.Unlock()
		return req.reject("projectile status definition unavailable")
	}
	match := r.registry.arenaMatches[member.binding.GameID]
	targets := r.registry.abilityTargetsLocked(r, member, definition, req.packet.SourceTime).(*arenaAbilityTargets)
	if targetObjectID == 0 || match == nil || !match.isCombatActive || match.random == nil {
		r.registry.mutex.Unlock()
		return req.reject("projectile status target unavailable")
	}
	target, isTargetFound := targets.NPC(targetObjectID)
	if !isTargetFound || target.IsDefeated || target.HitPoint <= 0 ||
		!isInsideZoneTrigger(member.playerPosition, raknet.Vector3(target.Plan.Position),
			heroAbilityAdmissionRange(creature, definition)) {
		r.registry.mutex.Unlock()
		return req.reject("projectile status target unavailable")
	}
	if definition.Name == "AfflictionBolt" {
		_, victim, isVictimFound := targets.member(targetObjectID)
		if !isVictimFound || victim.isArenaStealthed() || victim.isArenaHitCurseActive(r.now()) {
			r.registry.mutex.Unlock()
			return req.reject("projectile status target unavailable")
		}
	}
	projected, err := zoneability.ProjectTiming(creature, definition)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaStatusTiming: %w", err)
	}
	projected.Speed, err = zoneability.ProjectProjectileSpeed(creature, projected)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaStatusSpeed: %w", err)
	}
	damage, err := zoneability.ProjectDamage(creature, projected,
		projected.MinimumDamagePerTick, projected.MaximumDamagePerTick)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaStatusDamage: %w", err)
	}
	manaCost, err := game.ResolveAbilityManaCost(projected.ManaCost,
		creature.DamageProfile.PrimaryAttribute, projected.ManaCoefficient,
		member.isOverdriveActiveAt(startedAt))
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaStatusMana: %w", err)
	}
	if member.deployedManaPoint() < manaCost {
		r.registry.mutex.Unlock()
		return req.reject("power unavailable")
	}
	previousProjectileID := member.nextProjectileObjectID
	projectileID, err := member.reserveCampaignProjectileIDs(1, 1000)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaStatusProjectileID: %w", err)
	}
	geometry, _, err := abilityTargetProjectileGeometryLocked(r, member, target)
	if err != nil {
		member.restoreCampaignProjectileID(previousProjectileID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaStatusGeometry: %w", err)
	}
	actorFootprint := member.deployedCampaignFootprintRadius()
	actorPosition := member.playerPosition
	actorHeight := actorFootprint
	if physics, isFound := r.program.NounPhysicsByID[creature.Noun]; isFound {
		actorHeight = max(actorHeight, (physics.BoundMinimum.Z+physics.BoundMaximum.Z)*0.5)
	}
	actorPosition.Z += max(float32(0), actorHeight)
	targetPosition := campaignProjectileAimPosition(target.Plan.Position,
		target.Plan.NPCProfile.FootprintRadius, geometry)
	facing := zoneability.ProjectileDirection(actorPosition, raknet.Vector3(targetPosition))
	launch := campaignProjectileLaunchPosition(game.Vec3(actorPosition), targetPosition, actorFootprint)
	travelDistance := zoneability.Distance(launch, targetPosition)
	travelDelay := max(abilityraknet.ProjectileCollisionTick,
		time.Duration(float64(travelDistance)/float64(projected.Speed)*float64(time.Second)))
	projectile, initialPackets, err := abilityraknet.NewProjectileRun(abilityraknet.ProjectileInput{
		Ability: projected, ActorObjectID: req.command.Common.ObjectID,
		TargetObjectID: targetObjectID, ProjectileObjectID: projectileID,
		ActorPosition: sim.Position(actorPosition), TargetPosition: sim.Position(targetPosition),
		ActorFacing: sim.Position(facing), ImpactPosition: sim.Position(targetPosition),
		FootprintRadius: actorFootprint, CollisionDelay: travelDelay,
		IsDirectHit: true, IsCollisionExternallyDriven: true,
		Damage: damage.Maximum, TargetHitPoint: target.HitPoint, ActorTeam: 1,
		SourceTime: req.packet.SourceTime})
	if err != nil {
		member.restoreCampaignProjectileID(previousProjectileID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaStatusProjectile: %w", err)
	}
	ack, err := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp: req.command.Common.Unknown[0], ResponseType: raknet.ActionResponseAccepted,
		ObjectID: abilityID, AbilityIndex: req.command.Ability.Index,
		SourceStartMilliseconds:  req.packet.SourceTime,
		SourceCommitMilliseconds: req.packet.SourceTime + uint64(projected.HitDelay/time.Millisecond),
		SourceEndMilliseconds:    req.packet.SourceTime + uint64(projected.ReleaseDelay/time.Millisecond)})
	if err != nil {
		projectile.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaStatusAck: %w", err)
	}
	release, err := abilityraknet.ReleaseResponse(req.command.Common.Unknown[0], abilityID,
		req.command.Ability.Index, req.packet.SourceTime, projected.HitDelay, projected.ReleaseDelay)
	if err != nil {
		projectile.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaStatusRelease: %w", err)
	}
	cooldown, err := abilityraknet.Cooldown(abilityraknet.CooldownRequest{
		ObjectID: req.command.Common.ObjectID, AbilityID: abilityID,
		Duration: projected.Cooldown, StartTime: req.packet.SourceTime})
	if err != nil {
		projectile.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaStatusCooldown: %w", err)
	}
	previousMana := member.deployedManaPoint()
	remainingMana := previousMana - manaCost
	mana, err := abilityraknet.Mana(req.command.Common.ObjectID, remainingMana)
	if err != nil {
		projectile.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaStatusManaPacket: %w", err)
	}
	cooldownReservation, isCooldownReserved := member.abilityCooldownSession().Reserve(
		zoneability.HeroAbilityCooldown(abilityID), startedAt, projected.Cooldown)
	if !isCooldownReserved {
		projectile.Stop()
		r.registry.mutex.Unlock()
		return req.reject("projectile cooldown unavailable")
	}
	releaseReservation, isReleaseReserved := member.abilityReleaseSession().Reserve(startedAt, projected.ReleaseDelay)
	if !isReleaseReserved {
		member.abilityCooldownSession().Rollback(cooldownReservation)
		projectile.Stop()
		r.registry.mutex.Unlock()
		return req.reject("projectile release unavailable")
	}
	err = member.stopPlayerMovement(startedAt)
	if err == nil {
		err = member.setDeployedManaPoints(remainingMana)
	}
	if err != nil {
		member.abilityCooldownSession().Rollback(cooldownReservation)
		member.abilityReleaseSession().Rollback(releaseReservation)
		projectile.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaStatusCommit: %w", err)
	}
	run := &arenaProjectileStatusRun{runtime: r, packet: req.packet, sessionKey: sessionKey,
		generation: member.generation, gameID: member.binding.GameID,
		sourceObjectID: req.command.Common.ObjectID, targetObjectID: targetObjectID,
		projectileObjectID: projectileID, startedAt: startedAt,
		definition: projected, creature: creature, damage: damage, projectile: projectile,
		releasePacket: release, sourcePosition: game.Vec3(actorPosition),
		targetPosition: targetPosition, geometry: geometry, facing: facing,
		travelDistance: travelDistance, actorFootprint: actorFootprint,
		impactDeadline: projected.HitDelay + travelDelay,
		creatureIndex:  member.deployedCreatureIndex, previousMana: previousMana,
		cooldownReservation: cooldownReservation, releaseReservation: releaseReservation}
	wrapper := &heroProjectileStatusRun{activationID: r.damage.activationID,
		projectile: projectile, arena: run, definition: projected,
		creature: creature, sourceObjectID: run.sourceObjectID}
	run.wrapper = wrapper
	if member.heroProjectileRuns == nil {
		member.heroProjectileRuns = make(map[uint32]*heroProjectileStatusRun)
	}
	member.heroProjectileRuns[projectileID] = wrapper
	r.registry.sessions[sessionKey] = member
	r.registry.mutex.Unlock()
	producers := []raknet.ScheduledPacketProducer{
		{Delay: projected.HitDelay, Produce: run.advance},
		{Delay: projected.ReleaseDelay, Produce: run.release},
	}
	if projected.Name == "AfflictionBolt" {
		flight := afflictionBoltFlightDuration(projected)
		if flight <= 0 {
			run.fail(errors.New("affliction flight unavailable"))
			return nil, errors.New("arenaStatusFlight: unavailable")
		}
		for deadline := projected.HitDelay; deadline <= projected.HitDelay+flight; deadline += afflictionBoltScanInterval {
			step := arenaAfflictionScanStep{run: run, deadline: deadline}
			producers = append(producers, raknet.ScheduledPacketProducer{Delay: deadline, Produce: step.scan})
		}
		producers = append(producers, raknet.ScheduledPacketProducer{Delay: projected.HitDelay + flight,
			Produce: run.finishFlight})
	} else {
		producers = append(producers, raknet.ScheduledPacketProducer{Delay: run.impactDeadline,
			Produce: run.impact})
	}
	sortScheduledPacketProducersByDelay(producers)
	producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
	var cancel raknet.CancelSchedule
	if req.packet.ScheduleGroupResult != nil {
		cancel, err = req.packet.ScheduleGroupResult(producers, run.fail)
	} else if req.packet.ScheduleGroup != nil {
		cancel, err = req.packet.ScheduleGroup(producers)
	} else {
		err = errors.New("projectile schedule unavailable")
	}
	if err != nil {
		run.fail(err)
		return nil, fmt.Errorf("arenaStatusSchedule: %w", err)
	}
	wrapper.mutex.Lock()
	wrapper.cancel = cancel
	wrapper.mutex.Unlock()
	packets := append([][]byte{ack, mana, cooldown}, initialPackets...)
	return packets, nil
}

func (e *arenaProjectileStatusRun) fail(cause error) {
	e.runtime.registry.mutex.Lock()
	member, isCurrent := e.isCurrentLocked()
	if isCurrent {
		delete(member.heroProjectileRuns, e.projectileObjectID)
		manaErr := member.setCampaignCharacterManaPoints(e.creatureIndex, e.previousMana)
		if manaErr != nil && e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena projectile status mana rollback failed source=%d: %v", e.sourceObjectID, manaErr)
		}
		member.abilityCooldownSession().Rollback(e.cooldownReservation)
		member.abilityReleaseSession().Rollback(e.releaseReservation)
		e.runtime.registry.sessions[e.sessionKey] = member
	}
	e.runtime.registry.mutex.Unlock()
	e.wrapper.Stop()
	if e.runtime.logger != nil {
		e.runtime.logger.Printf("Arena projectile status stopped source=%d: %v", e.sourceObjectID, cause)
	}
}

func (e *arenaProjectileStatusRun) advance() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	_, isCurrent := e.isCurrentLocked()
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packets, err := e.projectile.Advance(context.Background(), e.definition.HitDelay)
	if err != nil {
		return nil, fmt.Errorf("arenaStatusAdvance: %w", err)
	}
	e.runtime.registry.mutex.Lock()
	e.queueLocked(packets)
	e.runtime.registry.mutex.Unlock()
	return nil, nil
}

func (e *arenaProjectileStatusRun) release() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	_, isCurrent := e.isCurrentLocked()
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packets, err := e.projectile.Advance(context.Background(), e.definition.ReleaseDelay)
	if err != nil {
		return nil, fmt.Errorf("arenaStatusRelease: %w", err)
	}
	e.runtime.registry.mutex.Lock()
	e.queueLocked(gameplayPeerPresentationPackets(packets))
	e.runtime.registry.mutex.Unlock()
	return [][]byte{e.releasePacket}, nil
}

type arenaAfflictionScanStep struct {
	run      *arenaProjectileStatusRun
	deadline time.Duration
}

func (e arenaAfflictionScanStep) scan() ([][]byte, error) {
	return e.run.scanAffliction(e.deadline)
}

func (e *arenaProjectileStatusRun) impact() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	source, isCurrent := e.isCurrentLocked()
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	remaining := e.projectile.RemainingFlightDelay(e.runtime.now())
	if remaining > abilityraknet.ProjectileCollisionTick {
		e.runtime.registry.mutex.Unlock()
		err := scheduleNPCProducer(e.runtime.registry, e.packet,
			min(remaining, campaignProjectileMotionPollInterval), e.impact)
		if err != nil {
			return nil, fmt.Errorf("arenaTerrifySlowResume: %w", err)
		}
		return nil, nil
	}
	e.impactDeadline = max(e.impactDeadline, e.projectile.Now())
	targets := e.runtime.registry.abilityTargetsLocked(e.runtime, source, e.definition,
		e.packet.SourceTime+uint64(e.impactDeadline/time.Millisecond)).(*arenaAbilityTargets)
	target, isFound := targets.NPC(e.targetObjectID)
	if !isFound || target.IsDefeated || target.HitPoint <= 0 {
		packets, err := e.projectile.ResolveCollision(context.Background(), e.impactDeadline,
			false, false, 0, e.damage.Maximum, false,
			sim.Position(e.targetPosition), sim.Position(e.facing))
		if err == nil {
			e.queueLocked(packets)
		}
		e.runtime.registry.mutex.Unlock()
		if err == nil {
			_, finishErr := e.finishTerrify()
			if finishErr != nil {
				return nil, fmt.Errorf("arenaTerrifyMissFinish: %w", finishErr)
			}
		}
		return nil, err
	}
	launch := campaignProjectileLaunchPosition(e.sourcePosition, e.targetPosition, e.actorFootprint)
	collision, err := zonenpc.ResolveProjectileCollision(launch, e.targetPosition,
		target.Plan.Position, e.travelDistance, e.geometry)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaTerrifyCollision: %w", err)
	}
	if !collision.IsDirectHit {
		packets, resolveErr := e.projectile.ResolveCollision(context.Background(), e.impactDeadline,
			false, false, 0, e.damage.Maximum, false, collision.Position, sim.Position(e.facing))
		if resolveErr == nil {
			e.queueLocked(packets)
		}
		e.runtime.registry.mutex.Unlock()
		if resolveErr != nil {
			return nil, fmt.Errorf("arenaTerrifyDodge: %w", resolveErr)
		}
		_, finishErr := e.finishTerrify()
		if finishErr != nil {
			return nil, fmt.Errorf("arenaTerrifyDodgeFinish: %w", finishErr)
		}
		return nil, nil
	}
	facing := zoneability.ProjectileDirection(raknet.Vector3(e.sourcePosition), raknet.Vector3(collision.Position))
	packets, err := e.projectile.ResolveCollision(context.Background(), e.impactDeadline,
		true, true, target.HitPoint, e.damage.Maximum, false,
		collision.Position, sim.Position(facing))
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaTerrifyImpact: %w", err)
	}
	e.queueLocked(filterProjectileCombatPackets(packets))
	timestamp := e.packet.SourceTime + uint64(e.impactDeadline/time.Millisecond)
	var status *arenaHitStatus
	// The final authored DOT tick can coincide with status expiry. Keep the
	// recipient lease one timer quantum longer so the tick executes first;
	// millisecond wire duration remains exactly build 103's value.
	statusDuration := e.definition.StatusDuration + time.Nanosecond
	if e.definition.StatusKind == sim.AbilityStatusKindFear {
		status, err = e.runtime.applyArenaFearStatusLocked(source, e.targetObjectID,
			e.definition.RootModifierID, statusDuration, timestamp)
	} else if e.definition.StatusKind == sim.AbilityStatusKindStun {
		plan := zoneability.AreaPlan{SourceObjectID: e.sourceObjectID,
			Definition: e.definition, Center: target.Plan.Position}
		plan.Definition.StatusDuration = statusDuration
		_, err = e.runtime.applyArenaTargetStatusLocked(e.sessionKey, e.generation,
			e.sourceObjectID, timestamp, plan, []uint32{e.targetObjectID})
		if err == nil {
			_, victim, isVictimFound := targets.member(e.targetObjectID)
			if isVictimFound {
				status = victim.arenaHitStatuses[arenaHitStatusKey{modifierID: e.definition.RootModifierID}]
			}
		}
	}
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaTerrifyStatus: %w", err)
	}
	if status != nil {
		run := &arenaAfflictionRun{status: status, source: source, creature: e.creature, damage: e.damage,
			definition: e.definition, timestamp: timestamp}
		status.affliction = run
		err = run.attachTicksLocked()
		if err != nil {
			_, victim, isVictimFound := targets.member(e.targetObjectID)
			if isVictimFound {
				status.retire(&victim)
				e.runtime.registry.sessions[status.targetKey] = victim
			}
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaTerrifyTicks: %w", err)
		}
	}
	if e.definition.SecondaryModifierID != 0 && e.definition.Radius > 0 {
		for _, candidate := range targets.LiveSnapshots() {
			if candidate.Plan.ObjectID == e.targetObjectID ||
				geometry.Distance(game.Vec3(collision.Position), candidate.Plan.Position) > e.definition.Radius {
				continue
			}
			fearErr := e.runtime.applyArenaFearLocked(source, candidate.Plan.ObjectID,
				e.definition.SecondaryModifierID, e.definition.StatusDuration, timestamp)
			if fearErr != nil && e.runtime.logger != nil {
				e.runtime.logger.Printf("Arena Terrify splash fear failed target=%d: %v",
					candidate.Plan.ObjectID, fearErr)
			}
		}
	}
	e.runtime.registry.mutex.Unlock()
	err = scheduleNPCProducer(e.runtime.registry, e.packet, e.definition.StatusDuration, e.finishTerrify)
	if err != nil {
		return nil, fmt.Errorf("arenaTerrifyFinish: %w", err)
	}
	return nil, nil
}

func (e *arenaProjectileStatusRun) finishTerrify() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	member, isCurrent := e.isCurrentLocked()
	if isCurrent {
		delete(member.heroProjectileRuns, e.projectileObjectID)
		e.runtime.registry.sessions[e.sessionKey] = member
	}
	e.runtime.registry.mutex.Unlock()
	if isCurrent {
		e.wrapper.Stop()
	}
	return nil, nil
}
