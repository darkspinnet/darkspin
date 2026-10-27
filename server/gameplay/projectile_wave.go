package gameplay

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// The schedule copies share this projectile-owned attempted-target set. The
// registry lock serializes it with collision, damage and projectile retirement.
type campaignProjectileWave struct {
	halfExtent       sim.Position
	attemptedTargets map[uint32]struct{}
}

type campaignWaveContact struct {
	target   zonenpc.Snapshot
	position sim.Position
	distance float32
}

// produceWaveFlight is entered with the registry locked. Motion, range expiry
// and cancellation retain the shared projectile owners; contacts do not resolve
// that owner, because the native piercing flag keeps its motion alive.
func (e campaignProjectileStep) produceWaveFlight(
	peerSession gameplayPeerSession, now time.Time, isFinalScheduledStep bool, packets [][]byte,
) ([][]byte, error) {
	schedule := e.schedule
	snapshot, segments := schedule.run.SampleCollisionMotion(now)
	flight, err := schedule.flight.AdvanceMotion(
		e.deadline-schedule.projectile.HitDelay, segments,
		snapshot.IsActive && snapshot.RemainingDistance <= 0, nil,
	)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("waveMotion: %w", err)
	}
	// WaitForProjectile checks exhausted range before exposing collision flags.
	if flight.IsResolved {
		terminalPackets, resolveErr := schedule.run.ResolveCollision(
			context.Background(), e.deadline, false, false, 0,
			schedule.plan.Damage.Maximum, false, flight.Position, sim.Position(schedule.facing),
		)
		if resolveErr != nil {
			schedule.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("waveExpire: %w", resolveErr)
		}
		e.finishFlightLocked(&peerSession)
		schedule.runtime.registry.mutex.Unlock()
		return append(packets, terminalPackets...), nil
	}
	contacts, err := schedule.wave.contacts(schedule, peerSession, snapshot, segments)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("waveContacts: %w", err)
	}
	results := make([]zoneability.AreaResult, 0, len(contacts))
	transitions := make([]campaignDamageTransition, 0, len(contacts))
	width := schedule.projectile.ProjectileWave.Width(now.Sub(schedule.startedAt.Add(schedule.projectile.HitDelay)))
	enemies := schedule.runtime.registry.abilityTargetsLocked(schedule.runtime, peerSession, schedule.definition,
		schedule.packet.SourceTime+uint64(e.deadline/time.Millisecond))
	random, randomErr := schedule.runtime.registry.abilityRandomLocked(peerSession)
	if randomErr != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("waveRandom: %w", randomErr)
	}
	if peerSession.binding.Mode == game.ModeArena {
		schedule.runtime.registry.sessions[schedule.sessionKey] = peerSession
	}
	for _, contact := range contacts {
		// Template chunk38 records this BEFORE the child hit callback, including
		// a callback returning false because the wave is still too narrow.
		schedule.wave.attemptedTargets[contact.target.Plan.ObjectID] = struct{}{}
		contactPackets, effectErr := schedule.wave.contactPackets(schedule, snapshot, contact)
		if effectErr != nil {
			schedule.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("waveEffects: %w", effectErr)
		}
		packets = append(packets, contactPackets...)
		// Piercing leaves the projectile at the physics step endpoint. The Lua
		// callback reads that object's position, not the swept contact position.
		if zoneability.Distance(game.Vec3(snapshot.Position), contact.target.Plan.Position) >= width {
			continue
		}
		plan := schedule.plan
		plan.TargetObjectID = contact.target.Plan.ObjectID
		result, commitErr := zoneability.CommitBasic(
			random, enemies, plan,
			schedule.creature, schedule.binding.Difficulty, schedule.runtime.program.Critical,
		)
		if commitErr != nil {
			schedule.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("waveCommit: %w", commitErr)
		}
		if peerSession.binding.Mode != game.ModeArena {
			transition, transitionErr := peerSession.applyCampaignDamageTransition(result.Damage)
			if transitionErr != nil {
				schedule.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("waveTransition: %w", transitionErr)
			}
			transitions = append(transitions, transition)
		}
		results = append(results, zoneability.AreaResult{
			SourceRank: plan.SourceRank, SourceCreature: schedule.creature,
			Snapshot: contact.target, Damage: result.Damage, IsCritical: result.IsCritical,
			Definition: plan.Definition,
		})
	}
	if peerSession.binding.Mode != game.ModeArena {
		schedule.runtime.registry.sessions[schedule.sessionKey] = peerSession
	}
	schedule.runtime.registry.mutex.Unlock()
	if arenaTargets, isArena := enemies.(*arenaAbilityTargets); isArena {
		arenaTargets.finish()
		resultPackets, publishErr := arenaTargets.packets(results)
		if publishErr != nil {
			return nil, fmt.Errorf("arenaWavePublish: %w", publishErr)
		}
		packets = append(packets, resultPackets...)
	} else if len(results) > 0 {
		resultPackets, publishErr := schedule.runtime.damage.publishAreaResults(
			schedule.packet, schedule.sessionKey, schedule.generation, schedule.sourceObjectID,
			schedule.packet.SourceTime+uint64(e.deadline/time.Millisecond), schedule.binding,
			results, transitions, nil, false,
		)
		if publishErr != nil {
			return nil, fmt.Errorf("wavePublish: %w", publishErr)
		}
		packets = append(packets, resultPackets...)
	}
	if isFinalScheduledStep {
		// Slow and freeze extend the same guarded producer chain as other
		// projectiles. Width continues to use elapsed game time while frozen.
		if schedule.packet.ScheduleFunc == nil {
			return nil, fmt.Errorf("waveResume: scheduler unavailable")
		}
		next := campaignProjectileStep{schedule: schedule, deadline: e.deadline + abilityraknet.ProjectileCollisionTick}
		err = schedule.packet.ScheduleFunc(abilityraknet.ProjectileCollisionTick, next.produceContinuation)
		if err != nil {
			return nil, fmt.Errorf("waveResume: %w", err)
		}
	}
	return packets, nil
}

func (e *campaignProjectileWave) contacts(
	schedule campaignProjectileSchedule, peerSession gameplayPeerSession,
	snapshot abilityraknet.ProjectileSnapshot, segments []sim.ProjectileMotionSegment,
) ([]campaignWaveContact, error) {
	if len(segments) == 0 {
		segments = []sim.ProjectileMotionSegment{{Position: snapshot.Position, Direction: snapshot.Direction}}
	}
	contacts := make([]campaignWaveContact, 0)
	enemies := schedule.runtime.registry.abilityTargetsLocked(schedule.runtime, peerSession, schedule.definition,
		schedule.packet.SourceTime)
	for _, target := range enemies.LiveSnapshots() {
		if _, isAttempted := e.attemptedTargets[target.Plan.ObjectID]; isAttempted {
			continue
		}
		if !target.IsPublished || target.IsDefeated || target.HitPoint <= 0 || !isSupportHealerBasicImpactTarget(target) {
			continue
		}
		geometry, isFallback, err := abilityTargetProjectileGeometryLocked(schedule.runtime, peerSession, target)
		if err != nil {
			return nil, fmt.Errorf("targetGeometry: %w", err)
		}
		if isFallback && schedule.runtime.logger != nil {
			schedule.runtime.logger.Printf("RakNet wave target geometry fallback projectile=%d target=%d", schedule.projectileObjectID, target.Plan.ObjectID)
		}
		distance := float32(0)
		for _, segment := range segments {
			collision, collisionErr := sim.ResolveProjectileOrientedBoxCollision(sim.ProjectileBoxCollisionInput{
				ProjectilePosition: segment.Position, ProjectileDirection: segment.Direction,
				ProjectileHalfExtent: e.halfExtent, MaximumDistance: segment.Distance,
				TargetPosition: sim.Position(target.Plan.Position), TargetMinimum: geometry.TargetMinimum, TargetMaximum: geometry.TargetMaximum,
			})
			if collisionErr != nil {
				return nil, fmt.Errorf("targetCollision: %w", collisionErr)
			}
			if collision.IsDirectHit && collision.TravelDistance <= segment.Distance {
				contacts = append(contacts, campaignWaveContact{target: target, position: collision.Position, distance: distance + collision.TravelDistance})
				break
			}
			distance += segment.Distance
		}
	}
	sort.Slice(contacts, func(first, second int) bool {
		if contacts[first].distance == contacts[second].distance {
			return contacts[first].target.Plan.ObjectID < contacts[second].target.Plan.ObjectID
		}
		return contacts[first].distance < contacts[second].distance
	})
	return contacts, nil
}

func (e *campaignProjectileWave) contactPackets(
	schedule campaignProjectileSchedule, snapshot abilityraknet.ProjectileSnapshot, contact campaignWaveContact,
) ([][]byte, error) {
	packets := make([][]byte, 0, 2)
	impactPacket, err := raknet.MarshalApplication(raknet.PositionedEffectMessage{
		Asset: util.HashID(schedule.projectile.ImpactEffectName), Position: raknet.Vector3(contact.position), Facing: raknet.Vector3(snapshot.Direction),
	})
	if err != nil {
		return nil, fmt.Errorf("impactMarshal: %w", err)
	}
	packets = append(packets, impactPacket)
	exitPosition := game.Vec3(contact.position)
	exitDistance := 2 * contact.target.Plan.NPCProfile.FootprintRadius
	exitPosition.X += snapshot.Direction.X * exitDistance
	exitPosition.Y += snapshot.Direction.Y * exitDistance
	exitPosition.Z += snapshot.Direction.Z * exitDistance
	exitPacket, err := raknet.MarshalApplication(raknet.PositionedEffectMessage{
		Asset: util.HashID(schedule.projectile.ProjectileWave.ExitEffectName), Position: raknet.Vector3(exitPosition), Facing: raknet.Vector3(snapshot.Direction),
	})
	if err != nil {
		return nil, fmt.Errorf("exitMarshal: %w", err)
	}
	return append(packets, exitPacket), nil
}
