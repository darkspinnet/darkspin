package gameplay

import (
	"context"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/sim"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
)

// produceFlight is entered with the peer registry locked, like the terminal
// impact producers. The shared flight owns collision; the run owns wire events.
func (e campaignProjectileStep) produceFlight(
	peerSession gameplayPeerSession,
) ([][]byte, error) {
	schedule := e.schedule
	isFinalScheduledStep := e.deadline >= schedule.run.LastDeadline()
	// A delayed scheduler must not replay old flight segments against a new
	// target pose. Catch up once to elapsed time; queued callbacks then observe
	// the same or a later sample, never a regressed simulator clock.
	now := schedule.runtime.now()
	e.deadline = max(e.deadline, schedule.run.Now(), now.Sub(schedule.startedAt))
	packets, err := schedule.run.Advance(context.Background(), e.deadline)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("flightAdvance: %w", err)
	}
	if e.deadline < schedule.projectile.HitDelay || schedule.flight.IsResolved() {
		e.finishFlightLocked(&peerSession)
		schedule.runtime.registry.mutex.Unlock()
		return packets, nil
	}
	if schedule.wave != nil {
		return e.produceWaveFlight(peerSession, now, isFinalScheduledStep, packets)
	}
	enemies := schedule.runtime.registry.abilityTargetsLocked(schedule.runtime, peerSession, schedule.definition, schedule.packet.SourceTime)
	targets := make([]sim.ProjectileCollisionTarget, 0)
	fallbackCount := 0
	for _, target := range enemies.LiveSnapshots() {
		if !target.IsPublished || target.IsDefeated || target.HitPoint <= 0 ||
			!isSupportHealerBasicImpactTarget(target) {
			continue
		}
		geometry, isFallback, geometryErr := abilityTargetProjectileGeometryLocked(schedule.runtime, peerSession, target)
		if geometryErr != nil {
			schedule.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("flightGeometry[%d]: %w", target.Plan.ObjectID, geometryErr)
		}
		if isFallback {
			fallbackCount++
		}
		targets = append(targets, sim.ProjectileCollisionTarget{
			ObjectID: target.Plan.ObjectID, Position: sim.Position(target.Plan.Position),
			Minimum: geometry.TargetMinimum, Maximum: geometry.TargetMaximum,
		})
	}
	if schedule.definition.Name == "MissileTempestBasic" {
		homingPackets, homingErr := schedule.trackMissileTarget(peerSession, now)
		if homingErr != nil {
			schedule.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("missileHoming: %w", homingErr)
		}
		packets = append(packets, homingPackets...)
	}
	snapshot, segments := schedule.run.SampleCollisionMotion(now)
	result, err := schedule.flight.AdvanceMotion(
		e.deadline-schedule.projectile.HitDelay, segments,
		snapshot.IsActive && snapshot.RemainingDistance <= 0, targets,
	)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("flightCollision: %w", err)
	}
	if !result.IsResolved {
		schedule.runtime.registry.mutex.Unlock()
		// Freeze and slow can extend flight beyond its original final callback.
		// Only the last nominal step starts this chain, even if callbacks arrive
		// late together and all catch up to the same elapsed time.
		if isFinalScheduledStep {
			if schedule.packet.ScheduleFunc == nil {
				return nil, fmt.Errorf("flightResume: scheduler unavailable")
			}
			next := campaignProjectileStep{schedule: schedule, deadline: e.deadline + abilityraknet.ProjectileCollisionTick}
			err = schedule.packet.ScheduleFunc(abilityraknet.ProjectileCollisionTick, next.produceContinuation)
			if err != nil {
				return nil, fmt.Errorf("flightResume: %w", err)
			}
		}
		return packets, nil
	}
	if result.TargetObjectID == 0 {
		if schedule.definition.Name == "MissileTempestBasic" {
			impactPackets, impactErr := e.produceMissileExplosion(peerSession, result.Position, 0)
			if impactErr != nil {
				return nil, fmt.Errorf("missileExpiry: %w", impactErr)
			}
			return append(packets, impactPackets...), nil
		}
		impactPackets, resolveErr := schedule.run.ResolveCollision(
			context.Background(), e.deadline, false, false, 0,
			schedule.plan.Damage.Maximum, false, result.Position, sim.Position(schedule.facing),
		)
		if resolveErr != nil {
			schedule.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("flightExpire: %w", resolveErr)
		}
		e.finishFlightLocked(&peerSession)
		schedule.runtime.registry.mutex.Unlock()
		schedule.runtime.logger.Printf(
			"RakNet projectile trajectory resolved kind=hero-basic projectile=%d source=%d target=%d outcome=range-expired endpoint=(%.3f,%.3f,%.3f) distance=%.3f flight_ms=%d",
			schedule.projectileObjectID, schedule.sourceObjectID, schedule.targetObjectID,
			result.Position.X, result.Position.Y, result.Position.Z, result.Distance,
			(e.deadline - schedule.projectile.HitDelay).Milliseconds(),
		)
		return append(packets, impactPackets...), nil
	}
	target, isFound := enemies.NPC(result.TargetObjectID)
	if !isFound {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("flightContact: target %d unavailable", result.TargetObjectID)
	}
	plan := schedule.plan
	plan.TargetObjectID = result.TargetObjectID
	if schedule.projectile.DamagePerSpeedUnit > 0 {
		bonusDamage := float32(math.Round(float64(result.Speed * schedule.projectile.DamagePerSpeedUnit)))
		plan.Damage.Minimum += bonusDamage
		plan.Damage.Maximum += bonusDamage
	}
	schedule.runtime.logger.Printf(
		"RakNet projectile flight contact projectile=%d selected_target=%d struck_target=%d distance=%.3f speed=%.3f flight_ms=%d candidate_count=%d geometry_fallback_count=%d",
		schedule.projectileObjectID, schedule.targetObjectID, result.TargetObjectID,
		result.Distance, result.Speed, (e.deadline - schedule.projectile.HitDelay).Milliseconds(),
		len(targets), fallbackCount,
	)
	impactPackets, err := e.produceContact(peerSession, target, plan, sim.ProjectileBoxCollision{
		Position: result.Position, TravelDistance: result.Distance, IsDirectHit: true,
	})
	if err != nil {
		return nil, fmt.Errorf("flightCommit: %w", err)
	}
	return append(packets, impactPackets...), nil
}

func (e campaignProjectileStep) produceContinuation() ([][]byte, error) {
	packets, err := e.produceTracked()
	if err == nil {
		return packets, nil
	}
	return nil, fmt.Errorf("flightContinuation: %w", err)
}

func (e campaignProjectileStep) produceTracked() ([][]byte, error) {
	retirement := e.schedule.retirement
	if retirement == nil {
		packets, err := e.produce()
		if err != nil {
			return nil, fmt.Errorf("flightProduce: %w", err)
		}
		return packets, nil
	}
	e.schedule.runtime.registry.mutex.Lock()
	member, isFound := e.schedule.runtime.registry.sessions[e.schedule.sessionKey]
	isCurrent := isFound && member.generation == e.schedule.generation && member.zone == retirement.originalZone &&
		member.sageAttacks[e.schedule.projectileObjectID] == e.schedule.run
	isStarted := isCurrent && e.schedule.runtime.registry.beginCampaignProjectilePublicationLocked(retirement)
	e.schedule.runtime.registry.mutex.Unlock()
	if !isStarted {
		return nil, nil
	}
	packets, err := e.produce()
	e.schedule.runtime.registry.mutex.Lock()
	retireErr := e.schedule.runtime.registry.finishCampaignProjectilePublicationLocked(retirement, e.schedule.sessionKey, packets, err)
	if err != nil {
		member, isFound = e.schedule.runtime.registry.sessions[e.schedule.sessionKey]
		if isFound && member.generation == e.schedule.generation {
			member.basicSequenceSession().ReleaseHeld()
			e.schedule.runtime.registry.sessions[e.schedule.sessionKey] = member
		}
	}
	e.schedule.runtime.registry.mutex.Unlock()
	if retireErr != nil && e.schedule.runtime.logger != nil {
		e.schedule.runtime.logger.Printf("RakNet projectile producer cleanup failed projectile=%d: %v", e.schedule.projectileObjectID, retireErr)
	}
	if err != nil {
		return nil, fmt.Errorf("flightProduce: %w", err)
	}
	return nil, nil
}

func (e campaignProjectileStep) finishFlightLocked(peerSession *gameplayPeerSession) {
	schedule := e.schedule
	if !schedule.flight.IsResolved() || e.deadline < schedule.projectile.ReleaseDelay {
		return
	}
	delete(peerSession.sageAttacks, schedule.projectileObjectID)
	schedule.runtime.registry.sessions[schedule.sessionKey] = *peerSession
}
