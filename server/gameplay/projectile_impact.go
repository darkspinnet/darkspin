package gameplay

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	geometryraknet "github.com/darkspinnet/darkspin/server/zone/geometry/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type campaignProjectileSchedule struct {
	runtime            campaignAbilityCommandRuntime
	packet             raknet.Packet
	sessionKey         string
	generation         uint64
	sourceObjectID     uint32
	targetObjectID     uint32
	projectileObjectID uint32
	impactDeadline     time.Duration
	maximumRange       float32
	sourcePosition     game.Vec3
	targetPosition     raknet.Vector3
	travelDistance     float32
	actorFootprint     float32
	geometry           zonenpc.ProjectileGeometry
	facing             raknet.Vector3
	creature           game.GameplayCreature
	definition         sim.AbilityDefinition
	projectile         sim.AbilityDefinition
	plan               zoneability.BasicPlan
	binding            game.GameplayBinding
	run                *abilityraknet.ProjectileRun
	isElectronSphere   bool
	flight             *sim.ProjectileFlight
	startedAt          time.Time
	retirement         *campaignProjectileRetirement
	wave               *campaignProjectileWave
}

type campaignProjectileStep struct {
	schedule campaignProjectileSchedule
	deadline time.Duration
}

func (e campaignProjectileSchedule) producer(
	deadline time.Duration,
) raknet.ScheduledPacketProducer {
	step := campaignProjectileStep{schedule: e, deadline: deadline}
	return raknet.ScheduledPacketProducer{
		Delay: deadline, Produce: step.produceTracked,
	}
}

func (e campaignProjectileStep) produce() ([][]byte, error) {
	schedule := e.schedule
	schedule.runtime.registry.mutex.Lock()
	peerSession, isFound :=
		schedule.runtime.registry.sessions[schedule.sessionKey]
	if schedule.retirement != nil && (schedule.retirement.isRetired || schedule.retirement.isRetirementPending) {
		schedule.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	isCurrent := isFound && peerSession.generation == schedule.generation &&
		peerSession.sageAttacks[schedule.projectileObjectID] == schedule.run
	if !isCurrent {
		schedule.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if schedule.flight != nil {
		return e.produceFlight(peerSession)
	}
	if e.deadline != schedule.impactDeadline {
		packets, err := schedule.run.Advance(
			context.Background(), e.deadline,
		)
		if err == nil && e.deadline == schedule.run.LastDeadline() &&
			!schedule.run.Snapshot(schedule.runtime.now()).IsActive {
			delete(peerSession.sageAttacks, schedule.projectileObjectID)
			schedule.runtime.registry.sessions[schedule.sessionKey] =
				peerSession
		}
		schedule.runtime.registry.mutex.Unlock()
		if err != nil {
			return nil, fmt.Errorf("campaignProjectileAdvance: %w", err)
		}
		return packets, nil
	}
	remaining := schedule.run.RemainingFlightDelay(schedule.runtime.now())
	if remaining > abilityraknet.ProjectileCollisionTick {
		schedule.runtime.registry.mutex.Unlock()
		err := scheduleNPCProducer(schedule.runtime.registry, schedule.packet,
			min(remaining, campaignProjectileMotionPollInterval), e.produceTracked)
		if err != nil {
			return nil, fmt.Errorf("basicSlowResume: %w", err)
		}
		return nil, nil
	}
	e.deadline = max(e.deadline, schedule.run.Now())
	defer e.retireResolved()
	if schedule.isElectronSphere {
		return e.produceElectronImpact(peerSession)
	}
	return e.produceDirectImpact(peerSession)
}

func (e campaignProjectileStep) retireResolved() {
	schedule := e.schedule
	schedule.runtime.registry.mutex.Lock()
	defer schedule.runtime.registry.mutex.Unlock()
	member, isFound := schedule.runtime.registry.sessions[schedule.sessionKey]
	if !isFound || member.generation != schedule.generation ||
		member.sageAttacks[schedule.projectileObjectID] != schedule.run ||
		schedule.run.Now() < schedule.run.LastDeadline() ||
		schedule.run.Snapshot(schedule.runtime.now()).IsActive {
		return
	}
	delete(member.sageAttacks, schedule.projectileObjectID)
	schedule.runtime.registry.sessions[schedule.sessionKey] = member
}

func (e campaignProjectileStep) produceElectronImpact(
	peerSession gameplayPeerSession,
) ([][]byte, error) {
	schedule := e.schedule
	enemies := schedule.runtime.registry.abilityTargetsLocked(schedule.runtime, peerSession, schedule.definition,
		schedule.packet.SourceTime+uint64(e.deadline/time.Millisecond))
	liveNPC, isLiveFound := enemies.NPC(schedule.targetObjectID)
	if schedule.targetObjectID == 0 || !isLiveFound {
		launchPosition := campaignProjectileLaunchPosition(
			schedule.sourcePosition, game.Vec3(schedule.targetPosition),
			schedule.actorFootprint,
		)
		packets, err := schedule.run.ResolveCollision(
			context.Background(), e.deadline, false, false, 0,
			schedule.plan.Damage.Maximum, false,
			sim.Position(schedule.targetPosition), sim.Position(schedule.facing),
		)
		schedule.runtime.registry.mutex.Unlock()
		if err != nil {
			return nil, fmt.Errorf("campaignElectronMiss: %w", err)
		}
		schedule.runtime.logger.Printf(
			"RakNet Electron Sphere terminal projectile=%d source=%d target=%d outcome=target-unavailable launch=(%.3f,%.3f,%.3f) aim=(%.3f,%.3f,%.3f) endpoint=(%.3f,%.3f,%.3f)",
			schedule.projectileObjectID, schedule.sourceObjectID,
			schedule.targetObjectID, launchPosition.X, launchPosition.Y,
			launchPosition.Z, schedule.targetPosition.X,
			schedule.targetPosition.Y, schedule.targetPosition.Z,
			schedule.targetPosition.X, schedule.targetPosition.Y,
			schedule.targetPosition.Z,
		)
		return packets, nil
	}
	launchPosition := campaignProjectileLaunchPosition(
		schedule.sourcePosition, game.Vec3(schedule.targetPosition),
		schedule.actorFootprint,
	)
	collision, err := zonenpc.ResolveProjectileCollision(
		launchPosition, game.Vec3(schedule.targetPosition),
		liveNPC.Plan.Position, schedule.travelDistance, schedule.geometry,
	)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignElectronCollision: %w", err)
	}
	if !collision.IsDirectHit {
		packets, resolveErr := schedule.run.ResolveCollision(
			context.Background(), e.deadline, false, false, 0,
			schedule.plan.Damage.Maximum, false, collision.Position,
			sim.Position(schedule.facing),
		)
		schedule.runtime.registry.mutex.Unlock()
		if resolveErr != nil {
			return nil, fmt.Errorf("campaignElectronMiss: %w", resolveErr)
		}
		schedule.runtime.logger.Printf(
			"RakNet Electron Sphere terminal projectile=%d source=%d target=%d outcome=path-miss launch=(%.3f,%.3f,%.3f) aim=(%.3f,%.3f,%.3f) target_now=(%.3f,%.3f,%.3f) endpoint=(%.3f,%.3f,%.3f)",
			schedule.projectileObjectID, schedule.sourceObjectID,
			schedule.targetObjectID, launchPosition.X, launchPosition.Y,
			launchPosition.Z, schedule.targetPosition.X,
			schedule.targetPosition.Y, schedule.targetPosition.Z,
			liveNPC.Plan.Position.X, liveNPC.Plan.Position.Y,
			liveNPC.Plan.Position.Z, collision.Position.X,
			collision.Position.Y, collision.Position.Z,
		)
		return packets, nil
	}
	impactPosition := game.Vec3(collision.Position)
	return e.produceElectronContact(peerSession, liveNPC, impactPosition)
}

func (e campaignProjectileStep) produceElectronContact(
	peerSession gameplayPeerSession, liveNPC zonenpc.Snapshot, impactPosition game.Vec3,
) ([][]byte, error) {
	schedule := e.schedule
	enemies := schedule.runtime.registry.abilityTargetsLocked(schedule.runtime, peerSession, schedule.definition,
		schedule.packet.SourceTime+uint64(e.deadline/time.Millisecond))
	launchPosition := campaignProjectileLaunchPosition(schedule.sourcePosition, game.Vec3(schedule.targetPosition), schedule.actorFootprint)
	impactPlan, err := zoneability.PlanElectronSphereImpact(
		enemies, schedule.sourceObjectID,
		schedule.targetObjectID, impactPosition,
		schedule.creature, schedule.plan,
	)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignElectronImpactPlan: %w", err)
	}
	random, err := schedule.runtime.registry.abilityRandomLocked(peerSession)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("electronRandom: %w", err)
	}
	results, err := zoneability.CommitArea(
		random, enemies, impactPlan, schedule.creature,
		peerSession.binding.Difficulty, schedule.runtime.program.Critical,
	)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignElectronImpactCommit: %w", err)
	}
	transitions := make([]campaignDamageTransition, 0, len(results))
	for _, result := range results {
		if peerSession.binding.Mode == game.ModeArena {
			continue
		}
		transition, transitionErr :=
			peerSession.applyCampaignDamageTransition(result.Damage)
		if transitionErr != nil {
			schedule.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf(
				"campaignElectronImpactTransition: %w", transitionErr,
			)
		}
		transitions = append(transitions, transition)
	}
	isAccepted := len(results) > 0
	previousHealth := float32(0)
	damage := schedule.plan.Damage.Maximum
	isCritical := false
	if isAccepted {
		previousHealth = results[0].Damage.PreviousHealth
		damage = results[0].Damage.Damage
		isCritical = results[0].IsCritical
	}
	packets, err := schedule.run.ResolveCollision(
		context.Background(), e.deadline, isAccepted, isAccepted,
		previousHealth, damage, isCritical,
		sim.Position(impactPosition), sim.Position(schedule.facing),
	)
	if schedule.flight != nil {
		if peerSession.binding.Mode == game.ModeArena {
			peerSession = schedule.runtime.registry.sessions[schedule.sessionKey]
		}
		e.finishFlightLocked(&peerSession)
	}
	if peerSession.binding.Mode != game.ModeArena {
		schedule.runtime.registry.sessions[schedule.sessionKey] = peerSession
	}
	schedule.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("campaignElectronImpactCollision: %w", err)
	}
	schedule.runtime.logger.Printf(
		"RakNet Electron Sphere terminal projectile=%d source=%d target=%d outcome=impact launch=(%.3f,%.3f,%.3f) aim=(%.3f,%.3f,%.3f) target_now=(%.3f,%.3f,%.3f) endpoint=(%.3f,%.3f,%.3f) targets_hit=%d",
		schedule.projectileObjectID, schedule.sourceObjectID,
		schedule.targetObjectID, launchPosition.X, launchPosition.Y,
		launchPosition.Z, schedule.targetPosition.X,
		schedule.targetPosition.Y, schedule.targetPosition.Z,
		liveNPC.Plan.Position.X, liveNPC.Plan.Position.Y,
		liveNPC.Plan.Position.Z, impactPosition.X, impactPosition.Y,
		impactPosition.Z, len(results),
	)
	filteredPackets := filterProjectileCombatPackets(packets)
	timestamp := schedule.packet.SourceTime +
		uint64(e.deadline/time.Millisecond)
	effect := areaSpecialEffect{
		assetID: util.HashID(impactPlan.Definition.HitEffectName),
	}
	if arenaTargets, isArena := enemies.(*arenaAbilityTargets); isArena {
		arenaTargets.finish()
		hitPackets, hitErr := arenaTargets.packetsWithEffects(results, effect.produce, true)
		if hitErr != nil {
			return nil, fmt.Errorf("electronPublish: %w", hitErr)
		}
		return append(filteredPackets, hitPackets...), nil
	}
	resultPackets, err := schedule.runtime.damage.publishAreaResults(
		schedule.packet, schedule.sessionKey, schedule.generation,
		schedule.sourceObjectID, timestamp, schedule.binding,
		results, transitions, effect.produce, true,
	)
	if err != nil {
		return nil, fmt.Errorf("campaignElectronImpactPublish: %w", err)
	}
	return append(filteredPackets, resultPackets...), nil
}

func (e campaignProjectileStep) produceDirectImpact(
	peerSession gameplayPeerSession,
) ([][]byte, error) {
	schedule := e.schedule
	// Range was validated at acceptance. Moving the caster after launch
	// must not invalidate a projectile already in flight.
	targets := schedule.runtime.registry.abilityTargetsLocked(schedule.runtime, peerSession, schedule.definition, schedule.packet.SourceTime)
	livePlan := schedule.plan
	liveNPC, isLiveFound :=
		targets.NPC(schedule.targetObjectID)
	if !isLiveFound || liveNPC.IsDefeated || liveNPC.HitPoint <= 0 ||
		schedule.definition.Name == "SupportHealerBasic" &&
			!isSupportHealerBasicImpactTarget(liveNPC) {
		packets, err := schedule.run.ResolveCollision(
			context.Background(), e.deadline, false, false, 0,
			schedule.plan.Damage.Maximum, false,
			sim.Position(schedule.targetPosition),
			sim.Position(schedule.facing),
		)
		schedule.runtime.registry.mutex.Unlock()
		if err != nil {
			return nil, fmt.Errorf("campaignProjectileMiss: %w", err)
		}
		return packets, nil
	}
	launchPosition := campaignProjectileLaunchPosition(
		schedule.sourcePosition, game.Vec3(schedule.targetPosition),
		schedule.actorFootprint,
	)
	collision, err := zonenpc.ResolveProjectileCollision(
		launchPosition, game.Vec3(schedule.targetPosition),
		liveNPC.Plan.Position, schedule.travelDistance, schedule.geometry,
	)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignProjectileCollision: %w", err)
	}
	if !collision.IsDirectHit {
		currentAimPosition := campaignProjectileAimPosition(
			liveNPC.Plan.Position, liveNPC.Plan.NPCProfile.FootprintRadius,
			schedule.geometry,
		)
		packets, resolveErr := schedule.run.ResolveCollision(
			context.Background(), e.deadline, false, false, 0,
			schedule.plan.Damage.Maximum, false, collision.Position,
			sim.Position(schedule.facing),
		)
		schedule.runtime.registry.mutex.Unlock()
		if resolveErr != nil {
			return nil, fmt.Errorf("campaignProjectileDodge: %w", resolveErr)
		}
		schedule.runtime.logger.Printf(
			"RakNet projectile trajectory resolved kind=hero-basic projectile=%d source=%d target=%d outcome=miss aim=(%.3f,%.3f,%.3f) target_now=(%.3f,%.3f,%.3f) endpoint=(%.3f,%.3f,%.3f) drift=%.3f",
			schedule.projectileObjectID, schedule.sourceObjectID,
			schedule.targetObjectID, schedule.targetPosition.X,
			schedule.targetPosition.Y, schedule.targetPosition.Z,
			currentAimPosition.X, currentAimPosition.Y,
			currentAimPosition.Z, collision.Position.X,
			collision.Position.Y, collision.Position.Z,
			zonegeometry.Distance(
				game.Vec3(schedule.targetPosition), currentAimPosition,
			),
		)
		return packets, nil
	}
	return e.produceContact(peerSession, liveNPC, livePlan, collision)
}

func (e campaignProjectileStep) produceContact(
	peerSession gameplayPeerSession, liveNPC zonenpc.Snapshot,
	livePlan zoneability.BasicPlan, collision sim.ProjectileBoxCollision,
) ([][]byte, error) {
	schedule := e.schedule
	if schedule.definition.Name == "MissileTempestBasic" {
		return e.produceMissileExplosion(peerSession, collision.Position, liveNPC.Plan.ObjectID)
	}
	err := schedule.run.SetImpactTarget(liveNPC.Plan.ObjectID, sim.Position(liveNPC.Plan.Position))
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("projectileTarget: %w", err)
	}
	targets := schedule.runtime.registry.abilityTargetsLocked(schedule.runtime, peerSession, schedule.definition, schedule.packet.SourceTime+uint64(e.deadline/time.Millisecond))
	random, randomErr := schedule.runtime.registry.abilityRandomLocked(peerSession)
	if randomErr != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("projectileRandom: %w", randomErr)
	}
	if peerSession.binding.Mode == game.ModeArena {
		schedule.runtime.registry.sessions[schedule.sessionKey] = peerSession
	}
	result, err := zoneability.CommitBasic(
		random, targets, livePlan, schedule.creature,
		peerSession.binding.Difficulty, schedule.runtime.program.Critical,
	)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignProjectileCommit: %w", err)
	}
	if arenaTargets, isArena := targets.(*arenaAbilityTargets); isArena {
		// Hit cleanup may have queued reliable removals to the shooter. Carry
		// that updated session through flight retirement instead of overwriting it.
		peerSession = schedule.runtime.registry.sessions[schedule.sessionKey]
		packets, publishErr := e.commitArenaProjectileLocked(&peerSession, arenaTargets, liveNPC, result, collision)
		schedule.runtime.registry.mutex.Unlock()
		arenaTargets.finish()
		if publishErr != nil {
			return nil, fmt.Errorf("projectileArenaPublish: %w", publishErr)
		}
		return packets, nil
	}
	transition, err :=
		peerSession.applyCampaignDamageTransition(result.Damage)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignProjectileTransition: %w", err)
	}
	impactPosition := raknet.Vector3(collision.Position)
	currentAimPosition := campaignProjectileAimPosition(
		liveNPC.Plan.Position, liveNPC.Plan.NPCProfile.FootprintRadius,
		schedule.geometry,
	)
	facing := geometryraknet.Direction(
		raknet.Vector3(schedule.sourcePosition), impactPosition,
	)
	packets, err := schedule.run.ResolveCollision(
		context.Background(), e.deadline, true, true,
		result.Damage.PreviousHealth, result.Damage.Damage,
		result.IsCritical, sim.Position(impactPosition),
		sim.Position(facing),
	)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignProjectileHit: %w", err)
	}
	if schedule.flight != nil {
		e.finishFlightLocked(&peerSession)
	}
	schedule.runtime.registry.sessions[schedule.sessionKey] = peerSession
	healingPackets, healingErr := schedule.applyDamageHealingLocked(
		peerSession.zone, game.Vec3(impactPosition), result.Damage.Damage,
	)
	cooldownPackets, cooldownErr := schedule.applyLightspeedCooldownsLocked(game.Vec3(impactPosition))
	schedule.runtime.registry.mutex.Unlock()
	if healingErr != nil {
		return nil, fmt.Errorf("campaignProjectileHealing: %w", healingErr)
	}
	if cooldownErr != nil {
		return nil, fmt.Errorf("projectileCooldown: %w", cooldownErr)
	}
	schedule.runtime.logger.Printf(
		"RakNet projectile trajectory resolved kind=hero-basic projectile=%d source=%d target=%d outcome=hit aim=(%.3f,%.3f,%.3f) target_now=(%.3f,%.3f,%.3f) contact=(%.3f,%.3f,%.3f) drift=%.3f ability=%q weapon=(%.3f,%.3f) projected=(%.3f,%.3f) selected=%.3f applied=%.3f previous_health=%.3f critical=%t",
		schedule.projectileObjectID, schedule.sourceObjectID,
		schedule.targetObjectID, schedule.targetPosition.X,
		schedule.targetPosition.Y, schedule.targetPosition.Z,
		currentAimPosition.X, currentAimPosition.Y,
		currentAimPosition.Z, impactPosition.X, impactPosition.Y,
		impactPosition.Z, zonegeometry.Distance(
			game.Vec3(schedule.targetPosition), currentAimPosition,
		),
		schedule.definition.Name,
		schedule.creature.MinimumWeaponDamage, schedule.creature.MaximumWeaponDamage,
		livePlan.Damage.Minimum, livePlan.Damage.Maximum,
		result.SelectedDamage, result.Damage.Damage,
		result.Damage.PreviousHealth, result.IsCritical,
	)
	filteredPackets := filterProjectileCombatPackets(packets)
	timestamp := schedule.packet.SourceTime +
		uint64(e.deadline/time.Millisecond)
	resultPackets, err := schedule.runtime.damage.publishAreaResults(
		schedule.packet, schedule.sessionKey, schedule.generation,
		schedule.sourceObjectID, timestamp, schedule.binding,
		[]zoneability.AreaResult{{
			SourceRank: schedule.plan.SourceRank, SourceCreature: schedule.creature,
			Snapshot: liveNPC, Damage: result.Damage,
			IsCritical: result.IsCritical, Definition: schedule.plan.Definition,
		}},
		[]campaignDamageTransition{transition}, nil, false,
	)
	if err != nil {
		return nil, fmt.Errorf("campaignProjectilePublish: %w", err)
	}
	packets = append(filteredPackets, resultPackets...)
	packets = append(packets, healingPackets...)
	return append(packets, cooldownPackets...), nil
}

func filterProjectileCombatPackets(packets [][]byte) [][]byte {
	filteredPackets := packets[:0]
	for _, packet := range packets {
		if len(packet) == 0 ||
			packet[0] != byte(raknet.CombatEvent) &&
				packet[0] != byte(raknet.CombatantDataUpdate) {
			filteredPackets = append(filteredPackets, packet)
		}
	}
	return filteredPackets
}
