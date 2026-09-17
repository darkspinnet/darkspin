package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	geometryraknet "github.com/darkspinnet/darkspin/server/zone/geometry/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

func (r campaignNPCActionRuntime) produceHasterProjectile(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64, hasteEndTimestamp uint64,
) ([][]byte, error) {
	resume := campaignNPCProjectileSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: objectID, timestamp: timestamp,
		hasteEndTimestamp: hasteEndTimestamp, kind: campaignNPCProjectileHaster,
	}
	isDeferred, err := r.pursuit.deferAction(
		packet, sessionKey, generation, objectID, timestamp,
		resume.resume,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyHasterProjectileStun: %w", err)
	}
	if isDeferred {
		return nil, nil
	}
	if hasteEndTimestamp != 0 && timestamp >= hasteEndTimestamp {
		return r.produceHasterBuff(packet, sessionKey, generation, objectID, timestamp)
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound &&
		peerSession.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	if !isEnemyFound || enemy.IsDefeated {
		r.registry.mutex.Unlock()
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	peerSession, err = r.pursuit.advanceTargetPoseLocked(
		peerSession, enemy.TargetObjectID,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyHasterTargetPose: %w", err)
	}
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, enemy.TargetObjectID,
	)
	if !isTargetFound {
		r.registry.mutex.Unlock()
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	hasteAttackSpeed, hasteCooldownReduction, hasteMovementSpeedBuff :=
		peerSession.zone.NPCs().HasteProfile(objectID, r.now())
	plan, planErr := planCampaignNPCHasterAttack(
		enemy, target.ObjectID, target.Position, target.FootprintRadius,
	)
	if planErr == nil {
		plan.Profile = applyNPCHaste(
			plan.Profile, hasteAttackSpeed,
			hasteCooldownReduction, hasteMovementSpeedBuff,
		)
	}
	if planErr != nil {
		profile, isProfileFound := zonenpc.ActionProfileForNoun(enemy.Plan.NounName)
		attackProfile := zonenpc.ZelemHasterAttackProfile()
		if isProfileFound && profile.Family == zonenpc.ActionZelemHaster {
			attackProfile.MovementSpeed = profile.MovementSpeed
			attackProfile.NonCombatMovementSpeed = profile.NonCombatMovementSpeed
			attackProfile = applyNPCHaste(
				attackProfile, hasteAttackSpeed,
				hasteCooldownReduction, hasteMovementSpeedBuff,
			)
		}
		action, actionErr := campaignNPCActionWithProfile(
			enemy.Plan, target.ObjectID, target.Position,
			attackProfile, target.FootprintRadius,
		)
		r.registry.mutex.Unlock()
		if actionErr != nil {
			return nil, fmt.Errorf("enemyHasterPursuitAction: %w", actionErr)
		}
		if !action.IsPursuitNeeded {
			return nil, nil
		}
		pursuitPackets, marshalErr := npcraknet.Pursuit(action)
		if marshalErr != nil {
			return nil, fmt.Errorf("enemyHasterPursuitMarshal: %w", marshalErr)
		}
		scheduleErr := r.pursuit.schedule(
			packet, sessionKey, generation, objectID, timestamp,
			action.TargetPosition, action.Profile, resume.resume,
		)
		if scheduleErr != nil {
			r.releaseAction(sessionKey, generation, objectID)
			r.logger.Printf("RakNet campaign Haster pursuit not scheduled object=%d: %v", objectID, scheduleErr)
		}
		return pursuitPackets, nil
	}
	if peerSession.zone.NPCRandom() == nil {
		r.registry.mutex.Unlock()
		return nil, errors.New("enemy random unavailable")
	}
	result, commitErr := zonenpc.CommitAttack(
		peerSession.zone.NPCRandom(), plan, enemy.Plan.NPCProfile.CriticalRating, r.program.Critical,
	)
	if commitErr != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyHasterCommit: %w", commitErr)
	}
	ability, abilityErr := campaignNPCHasterProjectileAbility(plan.Profile)
	if abilityErr != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyHasterAbility: %w", abilityErr)
	}
	geometry, geometryErr := r.program.ProjectileGeometry(0)
	if geometryErr != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyHasterGeometry: %w", geometryErr)
	}
	projectileObjectID, objectIDErr := peerSession.reserveCampaignProjectileIDs(1, 1000)
	if objectIDErr != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyHasterObjectID: %w", objectIDErr)
	}
	aimGeometry := geometry
	if isCampaignNPCProjectileSampled(plan.Profile) {
		var isAimGeometryFallback bool
		aimGeometry, isAimGeometryFallback = r.projectileTargetGeometryLocked(&peerSession, target, geometry)
		if isAimGeometryFallback {
			r.logger.Printf("RakNet enemy projectile aim geometry fallback source=%d target=%d", objectID, target.ObjectID)
		}
	}
	sourcePosition, targetPosition := campaignNPCProjectileEndpoints(
		enemy, target, aimGeometry, r.program.NounPhysics[enemy.Plan.NounName],
	)
	launchPosition := campaignProjectileLaunchPosition(
		sourcePosition, targetPosition, enemy.Plan.NPCProfile.FootprintRadius,
	)
	distance := zoneability.Distance(launchPosition, targetPosition)
	collisionDelay := max(
		time.Duration(float64(distance)/float64(ability.Speed)*float64(time.Second)),
		abilityraknet.ProjectileCollisionTick,
	)
	impactDeadline := ability.HitDelay + collisionDelay
	facing := geometryraknet.Direction(
		raknet.Vector3{
			X: sourcePosition.X, Y: sourcePosition.Y, Z: sourcePosition.Z,
		},
		raknet.Vector3{
			X: targetPosition.X, Y: targetPosition.Y, Z: targetPosition.Z,
		},
	)
	startedAt := r.now()
	flight, flightErr := newCampaignNPCProjectileFlight(plan.Profile, ability, launchPosition,
		sim.Position(facing), geometry.ProjectileHalfExtent, startedAt)
	if flightErr != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyHasterFlight: %w", flightErr)
	}
	if flight != nil {
		distance = ability.Distance
		collisionDelay = max(flight.collision.Duration(), abilityraknet.ProjectileCollisionTick)
		impactDeadline = ability.HitDelay + collisionDelay
	}
	projectileRun, immediatePackets, runErr := abilityraknet.NewProjectileRun(abilityraknet.ProjectileInput{
		StartedAt: startedAt, IsCollisionSampled: flight != nil,
		Ability: ability, ActorObjectID: objectID, TargetObjectID: target.ObjectID,
		ProjectileObjectID: projectileObjectID,
		ActorPosition: sim.Position{
			X: sourcePosition.X, Y: sourcePosition.Y, Z: sourcePosition.Z,
		},
		TargetPosition: sim.Position{
			X: targetPosition.X, Y: targetPosition.Y, Z: targetPosition.Z,
		},
		ActorFacing: sim.Position{X: facing.X, Y: facing.Y, Z: facing.Z},
		ImpactPosition: sim.Position{
			X: targetPosition.X, Y: targetPosition.Y, Z: targetPosition.Z,
		},
		FootprintRadius: enemy.Plan.NPCProfile.FootprintRadius,
		CollisionDelay:  collisionDelay, IsDirectHit: true, IsCollisionExternallyDriven: true,
		IsPiercing: plan.Profile.IsProjectilePiercing,
		Damage:     result.Damage, IsCritical: result.IsCritical,
		TargetHitPoint: target.HitPoint, TargetManaPoint: target.ManaPoint,
		ActorTeam: 0, SourceTime: timestamp,
	})
	if runErr != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyHasterProjectileRun: %w", runErr)
	}
	trackErr := peerSession.trackCampaignNPCProjectile(projectileObjectID, projectileRun)
	if trackErr != nil {
		projectileRun.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyHasterProjectileTrack: %w", trackErr)
	}
	if flight == nil {
		projectileRun.AddDeadline(impactDeadline)
	}
	lastProjectileDeadline := projectileRun.LastDeadline()
	binding := peerSession.binding
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	projectileDeadlines := projectileRun.Deadlines()
	projectileSchedule := campaignNPCProjectileSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: objectID,
		projectileObjectID: projectileObjectID, timestamp: timestamp,
		impactDeadline: impactDeadline, lastDeadline: lastProjectileDeadline,
		nextDelay: ability.Cooldown, hasteEndTimestamp: hasteEndTimestamp,
		kind: campaignNPCProjectileHaster, source: enemy, target: target,
		plan: plan, result: result, geometry: geometry, facing: facing,
		binding: binding, run: projectileRun, flight: flight,
		projectileSourcePosition:    sourcePosition,
		projectileTargetPosition:    targetPosition,
		isProjectileTrajectoryFound: true,
	}
	producers := make([]raknet.ScheduledPacketProducer, 0, len(projectileDeadlines)+1)
	for _, scheduledDeadline := range projectileDeadlines {
		producers = append(
			producers, projectileSchedule.producer(scheduledDeadline),
		)
	}
	producers = append(producers, raknet.ScheduledPacketProducer{
		Delay: ability.Cooldown, Produce: projectileSchedule.next,
	})
	cancel, scheduleErr := packet.ScheduleProducers(producers)
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		r.projectile.retire(
			sessionKey, generation, projectileObjectID, projectileRun,
		)
		return nil, fmt.Errorf("enemyHasterSchedule: %w", scheduleErr)
	}
	r.registry.mutex.Lock()
	currentSession, isCurrentFound := r.registry.sessions[sessionKey]
	isTracked := isCurrentFound && currentSession.generation == generation &&
		currentSession.campaignNPCProjectiles[projectileObjectID] == projectileRun
	if isTracked && flight == nil {
		projectileRun.SetCancel(cancel)
	}
	r.registry.mutex.Unlock()
	if !isTracked {
		cancel()
		projectileRun.Stop()
		return nil, nil
	}
	r.logger.Printf(
		"RakNet projectile trajectory launched kind=npc projectile=%d source=%d target=%d ability=%q origin=(%.3f,%.3f,%.3f) launch=(%.3f,%.3f,%.3f) aim=(%.3f,%.3f,%.3f) target_velocity=(%.3f,%.3f,%.3f) travel=%.3f delay_ms=%d homing=%t piercing=%t",
		projectileObjectID, objectID, target.ObjectID, plan.Profile.AbilityName,
		sourcePosition.X, sourcePosition.Y, sourcePosition.Z,
		launchPosition.X, launchPosition.Y, launchPosition.Z,
		targetPosition.X, targetPosition.Y, targetPosition.Z,
		target.LinearVelocity.X, target.LinearVelocity.Y,
		target.LinearVelocity.Z, distance, collisionDelay.Milliseconds(),
		plan.Profile.HomingDelay > 0, plan.Profile.IsProjectilePiercing,
	)
	return immediatePackets, nil
}
