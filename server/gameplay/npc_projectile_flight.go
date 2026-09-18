package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/sporenet"
	zone "github.com/darkspinnet/darkspin/server/zone"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type campaignNPCProjectileFlight struct {
	collision    *sim.ProjectileFlight
	attack       *zone.NPCProjectileAttack
	startedAt    time.Time
	hitDelay     time.Duration
	releaseDelay time.Duration
	isPolling    bool
}

func newCampaignNPCProjectileFlight(
	profile zonenpc.ActionProfile, ability sim.AbilityDefinition,
	position game.Vec3, direction sim.Position, halfExtent sim.Position, startedAt time.Time,
) (*campaignNPCProjectileFlight, error) {
	// Only profiles whose contact operation supports retained launch authority
	// enter this path; homing, piercing, and control remain separate.
	if !isCampaignNPCProjectileSampled(profile) {
		return nil, nil
	}
	collision, err := sim.NewProjectileFlight(sim.ProjectileFlightInput{
		Position: sim.Position(position), Direction: direction, HalfExtent: halfExtent,
		Speed: ability.Speed, Acceleration: ability.Acceleration, MaximumDistance: ability.Distance,
	})
	if err != nil {
		return nil, fmt.Errorf("enemyFlightCreate: %w", err)
	}
	return &campaignNPCProjectileFlight{
		collision: collision, startedAt: startedAt, hitDelay: ability.HitDelay, releaseDelay: ability.ReleaseDelay,
	}, nil
}

func isCampaignNPCProjectileSampled(profile zonenpc.ActionProfile) bool {
	if profile.HomingDelay > 0 || profile.IsProjectilePiercing || profile.AbilityName == "Puller" {
		return false
	}
	return profile.ModifierName == "" || profile.ModifierName == "StalkerShock" ||
		(profile.ModifierName == "NocturnaBasicRanged_SilenceModifier" && profile.ModifierDuration == 2*time.Second) ||
		isCampaignNPCDamageOverTimeProfile(profile)
}

func (e campaignNPCProjectileStep) produceFlight() ([][]byte, error) {
	schedule := e.schedule
	schedule.runtime.registry.mutex.Lock()
	current, isFound := schedule.runtime.registry.sessions[schedule.sessionKey]
	if !isFound || current.generation != schedule.generation ||
		current.campaignNPCProjectiles[schedule.projectileObjectID] != schedule.run {
		schedule.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	source, isSourceFound := current.zone.NPCs().NPC(schedule.sourceObjectID)
	isLaunchPending := schedule.flight.attack == nil
	if current.isZoneTerminal() ||
		(isLaunchPending && (!isSourceFound || source.IsDefeated || source.HitPoint <= 0)) {
		schedule.runtime.registry.mutex.Unlock()
		return schedule.deleteAfterSourceLoss("enemyFlightSourceLoss")
	}
	now := schedule.runtime.now()
	e.deadline = max(e.deadline, schedule.run.Now(), now.Sub(schedule.flight.startedAt))
	if schedule.flight.attack == nil && e.deadline >= schedule.flight.hitDelay {
		attack, isAccepted, err := current.zone.BeginNPCProjectile(zonenpc.ActionOwner{
			UserID: current.binding.UserID, PeerGeneration: schedule.generation,
		}, schedule.sourceObjectID, schedule.plan.ActionGeneration, now)
		if err != nil {
			schedule.runtime.registry.mutex.Unlock()
			return schedule.fail("enemyFlightLaunch", err)
		}
		if !isAccepted {
			schedule.runtime.registry.mutex.Unlock()
			return schedule.deleteAfterSourceLoss("enemyFlightCancelled")
		}
		schedule.flight.attack = attack
	}
	packets, statDelta, status, err := e.sampleFlightLocked(&current, now)
	if err == nil {
		if schedule.flight.collision.IsResolved() && e.deadline >= schedule.flight.releaseDelay {
			current.untrackCampaignNPCProjectile(schedule.projectileObjectID, schedule.run)
			schedule.run.Finish()
		}
		schedule.runtime.registry.sessions[schedule.sessionKey] = current
	}
	isResolved := schedule.flight.collision.IsResolved()
	isNextPollRequired := !isResolved && e.deadline >= schedule.flight.hitDelay &&
		(!schedule.flight.isPolling || e.isFlightPoll)
	if isNextPollRequired {
		schedule.flight.isPolling = true
	}
	schedule.runtime.registry.mutex.Unlock()
	if err != nil {
		return schedule.fail("enemyFlightSample", err)
	}
	if status != nil {
		statusPackets, statusErr := status.apply(schedule)
		if statusErr != nil {
			schedule.runtime.logger.Printf("RakNet projectile status omitted projectile=%d target=%d modifier=%q: %v",
				schedule.projectileObjectID, status.plan.TargetObjectID, status.plan.Profile.ModifierName, statusErr)
		} else {
			packets = append(packets, statusPackets...)
		}
	}
	if statDelta != (sporenet.PlayerStatDelta{}) {
		err = schedule.runtime.stats.Record(context.Background(), schedule.binding, statDelta)
		if err != nil {
			schedule.runtime.logger.Printf("RakNet enemy projectile flight stats omitted source=%d: %v", schedule.sourceObjectID, err)
		}
	}
	if isNextPollRequired {
		if schedule.packet.ScheduleFunc == nil {
			return schedule.fail("enemyFlightResume", errors.New("scheduler unavailable"))
		}
		next := campaignNPCProjectileStep{schedule: schedule, deadline: e.deadline + abilityraknet.ProjectileCollisionTick, isFlightPoll: true}
		err = scheduleNPCProducer(schedule.runtime.registry, schedule.packet, abilityraknet.ProjectileCollisionTick, next.produce)
		if err != nil {
			return schedule.fail("enemyFlightResume", err)
		}
	}
	return packets, nil
}

// sampleFlightLocked uses one time for projectile travel and every candidate
// hero pose, then commits damage to the first object actually intersected.
func (e campaignNPCProjectileStep) sampleFlightLocked(current *gameplayPeerSession, now time.Time) ([][]byte, sporenet.PlayerStatDelta, *campaignNPCProjectileStatus, error) {
	schedule := e.schedule
	packets, err := schedule.run.Advance(context.Background(), e.deadline)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, nil, fmt.Errorf("enemyFlightAdvance: %w", err)
	}
	if e.deadline < schedule.flight.hitDelay || schedule.flight.collision.IsResolved() {
		return packets, sporenet.PlayerStatDelta{}, nil, nil
	}
	for _, target := range current.zone.LiveNPCTargets() {
		updated, poseErr := schedule.runtime.pursuit.advanceTargetPoseAtLocked(*current, target.ObjectID, now)
		if poseErr != nil {
			return nil, sporenet.PlayerStatDelta{}, nil, fmt.Errorf("enemyFlightPose: %w", poseErr)
		}
		*current = updated
	}
	targets := make([]sim.ProjectileCollisionTarget, 0)
	fallbackCount := 0
	for _, target := range current.zone.LiveNPCTargets() {
		geometry, isFallback := schedule.runtime.projectileTargetGeometryLocked(current, target, schedule.geometry)
		if isFallback {
			fallbackCount++
		}
		targets = append(targets, sim.ProjectileCollisionTarget{
			ObjectID: target.ObjectID, Position: sim.Position(target.Position),
			Minimum: geometry.TargetMinimum, Maximum: geometry.TargetMaximum,
		})
	}
	snapshot, segments := schedule.run.SampleCollisionMotion(now)
	result, err := schedule.flight.collision.AdvanceMotion(e.deadline-schedule.flight.hitDelay,
		segments, snapshot.IsActive && snapshot.RemainingDistance <= 0, targets)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, nil, fmt.Errorf("enemyFlightCollision: %w", err)
	}
	if !result.IsResolved {
		return packets, sporenet.PlayerStatDelta{}, nil, nil
	}
	if result.TargetObjectID == 0 {
		impactPackets, resolveErr := schedule.run.ResolveCollision(context.Background(), e.deadline,
			false, false, 0, schedule.result.Damage, schedule.result.IsCritical, result.Position, snapshot.Direction)
		if resolveErr != nil {
			return nil, sporenet.PlayerStatDelta{}, nil, fmt.Errorf("enemyFlightExpire: %w", resolveErr)
		}
		return append(packets, impactPackets...), sporenet.PlayerStatDelta{}, nil, nil
	}
	target, isFound := current.zone.NPCTarget(result.TargetObjectID)
	if !isFound {
		return nil, sporenet.PlayerStatDelta{}, nil, errors.New("enemy projectile contact unavailable")
	}
	err = schedule.run.SetImpactTarget(target.ObjectID, sim.Position(target.Position))
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, nil, fmt.Errorf("enemyFlightTarget: %w", err)
	}
	plan := schedule.plan
	plan.TargetObjectID = target.ObjectID
	impactPackets, statDelta, isStatusEligible, err := schedule.runtime.applyEnemyProjectileDamage(
		current, schedule.generation, plan, schedule.result, schedule.run, schedule.flight.attack,
		e.deadline, target, result.Position, snapshot.Direction,
		schedule.timestamp+uint64(e.deadline/time.Millisecond),
	)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, nil, fmt.Errorf("enemyFlightDamage: %w", err)
	}
	status := e.selectStatusLocked(current, plan, isStatusEligible)
	schedule.runtime.reserveSageCompanionRespawnLocked(current, schedule.sessionKey, schedule.generation, target.ObjectID)
	schedule.runtime.logger.Printf("RakNet projectile flight contact kind=npc projectile=%d selected_target=%d struck_target=%d distance=%.3f flight_ms=%d candidates=%d geometry_fallback_count=%d",
		schedule.projectileObjectID, schedule.target.ObjectID, target.ObjectID, result.Distance,
		(e.deadline - schedule.flight.hitDelay).Milliseconds(), len(targets), fallbackCount)
	return append(packets, impactPackets...), statDelta, status, nil
}

func (e campaignNPCActionRuntime) projectileTargetGeometryLocked(
	current *gameplayPeerSession, target zone.NPCTarget, fallback zoneability.ProjectileCollisionGeometry,
) (zoneability.ProjectileCollisionGeometry, bool) {
	noun := uint32(0)
	if target.IsHero {
		for _, candidate := range e.registry.sessions {
			if candidate.zone != current.zone || candidate.deployedObjectID != target.ObjectID ||
				int(candidate.deployedCreatureIndex) >= len(candidate.binding.Creatures) {
				continue
			}
			noun = candidate.binding.Creatures[candidate.deployedCreatureIndex].Noun
			break
		}
	} else {
		actor, isFound := current.zone.Companion().Snapshot(target.ObjectID)
		if isFound {
			noun = actor.Noun
		}
	}
	physics, isFound := e.program.NounPhysicsByID[noun]
	if !isFound {
		return fallback, true
	}
	return zoneability.ProjectileCollisionGeometry{
		ProjectileHalfExtent: fallback.ProjectileHalfExtent,
		TargetMinimum:        physics.BoundMinimum, TargetMaximum: physics.BoundMaximum,
	}, false
}
