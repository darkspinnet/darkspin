package gameplay

import (
	"context"
	"errors"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e campaignNPCProjectileStep) produce() ([][]byte, error) {
	if e.schedule.flight != nil {
		return e.produceFlight()
	}
	schedule := e.schedule
	runtime := schedule.runtime
	if e.deadline == schedule.impactDeadline {
		runtime.registry.mutex.RLock()
		current, isFound := runtime.registry.sessions[schedule.sessionKey]
		// Released homing shots outlive the boss's current cast/phase.
		isSourceActive := schedule.isReleasedFlightCurrent(current, isFound)
		runtime.registry.mutex.RUnlock()
		if !isSourceActive {
			return schedule.deleteAfterSourceLoss(
				"enemyHomingProjectileSourceLoss",
			)
		}
		remainingFlight := schedule.run.RemainingFlightDelay(runtime.now())
		if remainingFlight > abilityraknet.ProjectileCollisionTick {
			if schedule.packet.ScheduleFunc == nil {
				return schedule.fail(
					"enemyProjectileResumeScheduler", errors.New("unavailable"),
				)
			}
			resumeDelay := min(remainingFlight, campaignProjectileMotionPollInterval)
			err := scheduleNPCProducer(runtime.registry, schedule.packet, resumeDelay, e.produce)
			if err != nil {
				return schedule.fail("enemyProjectileResume", err)
			}
			return nil, nil
		}
	}
	if e.deadline != schedule.impactDeadline {
		runtime.registry.mutex.RLock()
		current, isFound := runtime.registry.sessions[schedule.sessionKey]
		isCurrent := schedule.isReleasedFlightCurrent(current, isFound) &&
			(e.deadline > schedule.plan.Profile.HitDelay ||
				current.isCampaignNPCSourceGenerationActive(
					schedule.generation, schedule.sourceObjectID, schedule.plan.ActionGeneration,
				))
		runtime.registry.mutex.RUnlock()
		if !isCurrent {
			return schedule.deleteAfterSourceLoss("enemyProjectileSourceLoss")
		}
		packets, err := schedule.run.Advance(context.Background(), e.deadline)
		if err != nil {
			return schedule.fail("enemyProjectileAdvance", err)
		}
		if e.deadline == schedule.lastDeadline && !schedule.run.Snapshot(runtime.now()).IsActive {
			runtime.projectile.complete(
				schedule.sessionKey, schedule.generation,
				schedule.projectileObjectID, schedule.run,
			)
		}
		return packets, nil
	}
	runtime.registry.mutex.Lock()
	current, isFound := runtime.registry.sessions[schedule.sessionKey]
	isSourceActive := schedule.isReleasedFlightCurrent(current, isFound)
	if !isSourceActive {
		runtime.registry.mutex.Unlock()
		return schedule.deleteAfterSourceLoss("enemyProjectileImpactSourceLoss")
	}
	current, err := runtime.pursuit.advanceTargetPoseLocked(
		current, schedule.target.ObjectID,
	)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return schedule.fail("enemyProjectileTargetPose", err)
	}
	if schedule.plan.Profile.IsProjectilePiercing {
		packets, statDelta, err := schedule.producePiercingImpact(&current, e.deadline)
		if err != nil {
			runtime.registry.mutex.Unlock()
			return schedule.fail("enemyProjectilePiercing", err)
		}
		runtime.registry.sessions[schedule.sessionKey] = current
		runtime.registry.mutex.Unlock()
		if e.deadline == schedule.lastDeadline {
			runtime.projectile.complete(
				schedule.sessionKey, schedule.generation,
				schedule.projectileObjectID, schedule.run,
			)
		}
		err = runtime.stats.Record(context.Background(), schedule.binding, statDelta)
		if err != nil {
			runtime.logger.Printf(
				"RakNet campaign NPC piercing stats omitted object=%d: %v",
				schedule.sourceObjectID, err,
			)
		}
		return packets, nil
	}
	currentTarget, isTargetFound := current.campaignNPCTarget(
		schedule.generation, schedule.target.ObjectID,
	)
	if !isTargetFound || currentTarget.ObjectID != schedule.target.ObjectID {
		runtime.registry.mutex.Unlock()
		packets, err := schedule.run.ResolveCollision(
			context.Background(), e.deadline, false, false, 0,
			schedule.result.Damage, schedule.result.IsCritical,
			sim.Position{
				X: schedule.target.Position.X, Y: schedule.target.Position.Y,
				Z: schedule.target.Position.Z,
			},
			sim.Position{
				X: schedule.facing.X, Y: schedule.facing.Y, Z: schedule.facing.Z,
			},
		)
		if err != nil {
			return schedule.fail("enemyProjectileMiss", err)
		}
		if e.deadline == schedule.lastDeadline {
			runtime.projectile.complete(
				schedule.sessionKey, schedule.generation,
				schedule.projectileObjectID, schedule.run,
			)
		}
		return packets, nil
	}
	var collision sim.ProjectileBoxCollision
	isHomingActive := schedule.plan.Profile.HomingDelay > 0 &&
		e.deadline-schedule.plan.Profile.HitDelay >= schedule.plan.Profile.HomingDelay
	gravityTrajectory, isGravityDeflected :=
		schedule.run.GravityCollisionTrajectory()
	if isGravityDeflected {
		projectileSourcePosition := game.Vec3(gravityTrajectory.Position)
		projectileTargetPosition := game.Vec3{
			X: gravityTrajectory.Position.X +
				gravityTrajectory.Direction.X*gravityTrajectory.Distance,
			Y: gravityTrajectory.Position.Y +
				gravityTrajectory.Direction.Y*gravityTrajectory.Distance,
			Z: gravityTrajectory.Position.Z +
				gravityTrajectory.Direction.Z*gravityTrajectory.Distance,
		}
		collision, err = zonenpc.ResolveProjectileCollision(
			projectileSourcePosition, projectileTargetPosition,
			currentTarget.Position, gravityTrajectory.Distance,
			zonenpc.ProjectileGeometry{
				ProjectileHalfExtent: schedule.geometry.ProjectileHalfExtent,
				TargetMinimum:        schedule.geometry.TargetMinimum,
				TargetMaximum:        schedule.geometry.TargetMaximum,
			},
		)
	} else if isHomingActive {
		currentSourcePosition, currentAimPosition := campaignNPCProjectileEndpoints(
			schedule.source, currentTarget, schedule.geometry,
			runtime.program.NounPhysics[schedule.source.Plan.NounName],
		)
		currentLaunchPosition := campaignProjectileLaunchPosition(
			currentSourcePosition, currentAimPosition,
			schedule.source.Plan.NPCProfile.FootprintRadius,
		)
		travelDistance := zoneability.Distance(
			currentLaunchPosition, currentAimPosition,
		)
		collision = sim.ProjectileBoxCollision{
			Position:       sim.Position(currentAimPosition),
			TravelDistance: travelDistance,
			IsDirectHit:    travelDistance < schedule.plan.Profile.ProjectileDistance,
		}
	} else {
		projectileSourcePosition := schedule.source.Plan.Position
		projectileTargetPosition := schedule.target.Position
		if schedule.isProjectileTrajectoryFound {
			projectileSourcePosition = schedule.projectileSourcePosition
			projectileTargetPosition = schedule.projectileTargetPosition
		}
		projectileLaunchPosition := campaignProjectileLaunchPosition(
			projectileSourcePosition, projectileTargetPosition,
			schedule.source.Plan.NPCProfile.FootprintRadius,
		)
		collision, err = zonenpc.ResolveProjectileCollision(
			projectileLaunchPosition, projectileTargetPosition,
			currentTarget.Position,
			zoneability.Distance(projectileLaunchPosition, projectileTargetPosition),
			zonenpc.ProjectileGeometry{
				ProjectileHalfExtent: schedule.geometry.ProjectileHalfExtent,
				TargetMinimum:        schedule.geometry.TargetMinimum,
				TargetMaximum:        schedule.geometry.TargetMaximum,
			},
		)
	}
	if err != nil {
		runtime.registry.mutex.Unlock()
		return schedule.fail("enemyProjectileCollision", err)
	}
	_, currentAimPosition := campaignNPCProjectileEndpoints(
		schedule.source, currentTarget, schedule.geometry,
		runtime.program.NounPhysics[schedule.source.Plan.NounName],
	)
	runtime.logger.Printf(
		"RakNet projectile trajectory resolved kind=npc projectile=%d source=%d target=%d ability=%q direct=%t aim=(%.3f,%.3f,%.3f) target_now=(%.3f,%.3f,%.3f) contact=(%.3f,%.3f,%.3f) drift=%.3f",
		schedule.projectileObjectID, schedule.sourceObjectID,
		schedule.target.ObjectID, schedule.plan.Profile.AbilityName,
		collision.IsDirectHit, schedule.projectileTargetPosition.X,
		schedule.projectileTargetPosition.Y,
		schedule.projectileTargetPosition.Z, currentAimPosition.X,
		currentAimPosition.Y, currentAimPosition.Z, collision.Position.X,
		collision.Position.Y, collision.Position.Z, zonegeometry.Distance(
			schedule.projectileTargetPosition, currentAimPosition,
		),
	)
	if !collision.IsDirectHit {
		runtime.registry.mutex.Unlock()
		packets, resolveErr := schedule.run.ResolveCollision(
			context.Background(), e.deadline, false, false, 0,
			schedule.result.Damage, schedule.result.IsCritical,
			collision.Position,
			sim.Position{
				X: schedule.facing.X, Y: schedule.facing.Y, Z: schedule.facing.Z,
			},
		)
		if resolveErr != nil {
			return schedule.fail("enemyProjectileDodge", resolveErr)
		}
		if e.deadline == schedule.lastDeadline {
			runtime.projectile.complete(
				schedule.sessionKey, schedule.generation,
				schedule.projectileObjectID, schedule.run,
			)
		}
		return packets, nil
	}
	isControlProjectile := schedule.plan.Profile.AbilityName == "Puller"
	if isControlProjectile {
		packets, resolveErr := schedule.run.ResolveCollision(
			context.Background(), e.deadline, true, true,
			currentTarget.HitPoint, 0, false,
			collision.Position,
			sim.Position{
				X: schedule.facing.X, Y: schedule.facing.Y, Z: schedule.facing.Z,
			},
		)
		if resolveErr != nil {
			runtime.registry.mutex.Unlock()
			return schedule.fail("enemyControlProjectileImpact", resolveErr)
		}
		runtime.registry.sessions[schedule.sessionKey] = current
		runtime.registry.mutex.Unlock()
		controlPackets, controlErr := runtime.applyGrapplingPulsarPull(
			schedule.packet, schedule.sessionKey, schedule.generation,
			schedule.plan, currentTarget,
			schedule.timestamp+uint64(e.deadline/time.Millisecond),
		)
		if controlErr != nil {
			runtime.logger.Printf(
				"RakNet campaign NPC projectile pull omitted object=%d: %v",
				schedule.sourceObjectID, controlErr,
			)
			controlPackets = nil
		}
		if e.deadline == schedule.lastDeadline {
			runtime.projectile.complete(
				schedule.sessionKey, schedule.generation,
				schedule.projectileObjectID, schedule.run,
			)
		}
		return append(packets, controlPackets...), nil
	}
	packets, statDelta, isStatusEligible, err := runtime.applyEnemyProjectileDamage(
		&current, schedule.generation, schedule.plan, schedule.result,
		schedule.run, nil, e.deadline, currentTarget, collision.Position,
		sim.Position{
			X: schedule.facing.X, Y: schedule.facing.Y, Z: schedule.facing.Z,
		},
		schedule.timestamp+uint64(e.deadline/time.Millisecond),
	)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return schedule.fail("enemyProjectileDamage", err)
	}
	runtime.reserveSageCompanionRespawnLocked(
		&current, schedule.sessionKey, schedule.generation,
		schedule.plan.TargetObjectID,
	)
	isStunApplied := false
	if isStatusEligible && schedule.plan.Profile.ModifierName == "StalkerShock" {
		draw := current.zone.NPCRandom().Float64() * 100
		isStunApplied = draw < float64(schedule.plan.Profile.ModifierChance)
	}
	isDamageOverTimeApplied := isStatusEligible &&
		isCampaignNPCDamageOverTimeProfile(schedule.plan.Profile)
	if isDamageOverTimeApplied && schedule.plan.Profile.ModifierChance > 0 {
		draw := current.zone.NPCRandom().Float64() * 100
		isDamageOverTimeApplied = draw <
			float64(schedule.plan.Profile.ModifierChance)
	}
	runtime.registry.sessions[schedule.sessionKey] = current
	runtime.registry.mutex.Unlock()
	if isStunApplied {
		modifierPackets, modifierErr := runtime.applyCampaignNPCTimedModifier(
			schedule.packet, schedule.sessionKey, schedule.generation,
			schedule.plan, schedule.timestamp+uint64(e.deadline/time.Millisecond),
		)
		if modifierErr != nil {
			runtime.logger.Printf(
				"RakNet campaign NPC projectile stun omitted object=%d: %v",
				schedule.sourceObjectID, modifierErr,
			)
		} else {
			packets = append(packets, modifierPackets...)
		}
	}
	if isDamageOverTimeApplied {
		modifierPackets, modifierErr := runtime.applyCampaignNPCPoison(
			schedule.packet, schedule.sessionKey, schedule.generation,
			schedule.plan, schedule.timestamp+uint64(e.deadline/time.Millisecond),
		)
		if modifierErr != nil {
			runtime.logger.Printf(
				"RakNet campaign NPC projectile damage-over-time omitted object=%d: %v",
				schedule.sourceObjectID, modifierErr,
			)
		} else {
			packets = append(packets, modifierPackets...)
		}
	}
	if isStatusEligible && schedule.plan.Profile.ModifierName ==
		"NocturnaBasicRanged_SilenceModifier" {
		modifierPackets, modifierErr := runtime.applyCampaignNPCSilence(
			schedule.packet, schedule.sessionKey, schedule.generation,
			schedule.plan, schedule.timestamp+uint64(e.deadline/time.Millisecond),
		)
		if modifierErr != nil {
			runtime.logger.Printf(
				"RakNet campaign NPC projectile silence omitted object=%d: %v",
				schedule.sourceObjectID, modifierErr,
			)
		} else {
			packets = append(packets, modifierPackets...)
		}
	}
	if isStatusEligible && schedule.plan.Profile.ModifierName ==
		"NocturnaSpecialHomerFear" {
		modifierPackets, modifierErr := runtime.applyCampaignNPCFear(
			schedule.packet, schedule.sessionKey, schedule.generation,
			schedule.source, currentTarget, schedule.plan.Profile,
			schedule.timestamp+uint64(e.deadline/time.Millisecond),
		)
		if modifierErr != nil {
			runtime.logger.Printf(
				"RakNet campaign NPC projectile fear omitted object=%d: %v",
				schedule.sourceObjectID, modifierErr,
			)
		} else {
			packets = append(packets, modifierPackets...)
		}
	}
	if e.deadline == schedule.lastDeadline {
		runtime.projectile.complete(
			schedule.sessionKey, schedule.generation,
			schedule.projectileObjectID, schedule.run,
		)
	}
	err = runtime.stats.Record(context.Background(), schedule.binding, statDelta)
	if err != nil {
		runtime.logger.Printf(
			"RakNet campaign NPC projectile stats omitted object=%d: %v",
			schedule.sourceObjectID, err,
		)
	}
	return packets, nil
}
