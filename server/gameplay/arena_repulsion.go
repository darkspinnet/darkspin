package gameplay

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type arenaRepulsionReset struct {
	registry       *gameplaySessionRegistry
	gameID         uint32
	targetKey      string
	generation     uint64
	objectID       uint32
	motionRevision uint64
	readyAt        time.Time
	timestamp      uint64
}

// Registry lock is held on entry and released before returning. The normal
// repulsion schedule retains its authored hit and release times.
func (e heroRepulsionSchedule) hitArenaLocked(peerSession gameplayPeerSession) ([][]byte, error) {
	match := e.runtime.registry.arenaMatches[peerSession.binding.GameID]
	if match == nil || !match.isCombatActive || match.navigation == nil || e.runtime.registry.timer == nil {
		e.runtime.registry.mutex.Unlock()
		return nil, errors.New("Arena repulsion world unavailable")
	}
	timestamp := e.packet.SourceTime + uint64(e.definition.HitDelay/time.Millisecond)
	targets := e.runtime.registry.abilityTargetsLocked(
		e.runtime, peerSession, e.definition, timestamp,
	).(*arenaAbilityTargets)
	packets := make([][]byte, 0)
	reflectionPackets, _, err := e.reflectArenaProjectilesLocked(&peerSession)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaRepulsionReflect: %w", err)
	}
	packets = append(packets, reflectionPackets...)
	snapshots := make([]zonenpc.Snapshot, 0)
	for _, target := range targets.LiveSnapshots() {
		delta := target.Plan.Position.Sub(e.center)
		distance := delta.Length()
		contactRadius := e.definition.Radius + max(float32(0), target.Plan.NPCProfile.FootprintRadius)
		if distance > contactRadius {
			continue
		}
		snapshots = append(snapshots, target)
		key, victim, isFound := targets.member(target.Plan.ObjectID)
		if !isFound || victim.isHeroDamageImmune(e.runtime.now()) {
			continue
		}
		if distance <= 0 {
			delta = game.Vec3{X: 1}
			distance = 1
		}
		desired := target.Plan.Position.Add(delta.Scale(repulsionKnockbackDistance / distance))
		err := zonenavigation.ValidateMovement(match.navigation, target.Plan.Position, desired,
			victim.deployedCampaignNavigationRadius())
		if err != nil {
			continue
		}
		plan := zonenpc.AttackPlan{
			SourceObjectID: e.sourceObjectID, TargetObjectID: target.Plan.ObjectID,
			SourcePosition: e.center, TargetPosition: target.Plan.Position,
			Profile: zonenpc.ActionProfile{ForcedMovementSpeed: repulsionKnockbackSpeed,
				ForcedMovementDistance:     repulsionKnockbackDistance,
				ForcedMovementReactionName: "react_knockback"},
		}
		movementPackets, err := npcraknet.ForcedMovement(plan, desired, timestamp)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaRepulsionMovePacket: %w", err)
		}
		travel := time.Duration(float64(desired.Sub(target.Plan.Position).Length()/repulsionKnockbackSpeed) * float64(time.Second))
		readyAt := e.runtime.now().Add(travel)
		err = victim.placeTeleportedPlayer(e.runtime.now(), raknet.Vector3(desired))
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaRepulsionPosition: %w", err)
		}
		interrupted := victim.resetInterruptibleActionAdmission()
		if interrupted != nil {
			interrupted.Stop()
		}
		victim.campaignPlayerPursuitSession().Cancel()
		victim.heroKnockbackLanding = heroKnockbackLanding{
			objectID: target.Plan.ObjectID, readyAt: readyAt,
			holdUntil: readyAt.Add(repulsionKnockbackOutro), isNativeJump: true,
		}
		reset := arenaRepulsionReset{registry: e.runtime.registry, gameID: peerSession.binding.GameID,
			targetKey: key, generation: victim.generation, objectID: target.Plan.ObjectID,
			motionRevision: victim.playerMotionRevision(), readyAt: readyAt,
			timestamp: timestamp + uint64((travel+repulsionKnockbackOutro)/time.Millisecond)}
		resetDelay := max(time.Millisecond, travel+repulsionKnockbackOutro)
		cancel, scheduleErr := e.runtime.registry.timer.Schedule(resetDelay, reset.finish)
		if scheduleErr == nil && cancel == nil {
			scheduleErr = errors.New("repulsion reset cancellation unavailable")
		}
		if scheduleErr != nil {
			if cancel != nil {
				cancel()
			}
			victim.heroKnockbackLanding = heroKnockbackLanding{}
			resetPacket, resetErr := npcraknet.ResetAnimation(target.Plan.ObjectID, timestamp)
			if resetErr != nil {
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("arenaRepulsionReset: %w", errors.Join(scheduleErr, resetErr))
			}
			movementPackets = append(movementPackets, resetPacket)
		}
		e.runtime.registry.sessions[key] = victim
		packets = append(packets, movementPackets...)
	}
	results := make([]zoneability.AreaResult, 0)
	if e.damage.Maximum > 0 && len(snapshots) > 0 {
		plan := zoneability.AreaPlan{SourceObjectID: e.sourceObjectID,
			AbilityID: util.HashID(e.definition.Name), Definition: e.definition,
			Damage: e.damage, Center: e.center, Target: snapshots}
		random, err := e.runtime.registry.abilityRandomLocked(peerSession)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaRepulsionRandom: %w", err)
		}
		results, err = zoneability.CommitArea(random, targets, plan, e.creature,
			peerSession.binding.Difficulty, e.runtime.program.Critical)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaRepulsionDamage: %w", err)
		}
	}
	if e.definition.StatusKind == sim.AbilityStatusKindSlow && len(snapshots) > 0 {
		objectIDs := make([]uint32, 0, len(snapshots))
		for _, snapshot := range snapshots {
			objectIDs = append(objectIDs, snapshot.Plan.ObjectID)
		}
		statusPlan := zoneability.AreaPlan{SourceObjectID: e.sourceObjectID,
			AbilityID: util.HashID(e.definition.Name), Definition: e.definition}
		statusPackets, err := e.runtime.applyArenaTargetStatusLocked(e.sessionKey,
			e.generation, e.sourceObjectID, timestamp, statusPlan, objectIDs)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaRepulsionSlow: %w", err)
		}
		packets = append(packets, statusPackets...)
	}
	e.runtime.registry.mutex.Unlock()
	damagePackets, err := targets.packets(results)
	if err != nil {
		targets.finish()
		return nil, fmt.Errorf("arenaRepulsionPublish: %w", err)
	}
	packets = append(packets, damagePackets...)
	targets.finish()
	return packets, nil
}

// Build-103 Repulsion Wave rewrites the projectile's team and mover, then
// stops its old server flight. The client owns the reflected flight thereafter.
func (e heroRepulsionSchedule) reflectArenaProjectilesLocked(source *gameplayPeerSession) ([][]byte, uint32, error) {
	if source == nil || e.definition.Radius <= repulsionProjectileInnerRadius {
		return nil, 0, nil
	}
	now := e.runtime.now()
	owners := e.runtime.registry.hostileProjectileOwnersLocked(source, e.sessionKey)
	packets := make([][]byte, 0, len(owners)*2)
	reflectedCount := uint32(0)
	for _, owner := range owners {
		snapshot := owner.run.Snapshot(now)
		if !snapshot.IsActive {
			continue
		}
		position := game.Vec3(snapshot.Position)
		delta := position.Sub(e.center)
		delta.Z = 0
		distance := delta.Length()
		if distance > e.definition.Radius {
			continue
		}
		if distance <= 0 {
			delta = game.Vec3{X: 1}
			distance = 1
		}
		direction := delta.Scale(1 / distance)
		falloff := min(float32(1), max(float32(0),
			(distance-repulsionProjectileInnerRadius)/(e.definition.Radius-repulsionProjectileInnerRadius)))
		speed := repulsionProjectileMaximumSpeed -
			(repulsionProjectileMaximumSpeed-repulsionProjectileMinimumSpeed)*falloff
		if math.IsNaN(float64(speed)) || math.IsInf(float64(speed), 0) {
			return nil, reflectedCount, fmt.Errorf("arenaReflectionSpeed[%d]: invalid", owner.objectID)
		}
		team := abilityActorTeam(*source)
		linearVelocity := raknet.Vector3{X: direction.X * speed, Y: direction.Y * speed}
		projectilePosition := raknet.Vector3{X: position.X, Y: position.Y, Z: position.Z}
		objectPacket, err := raknet.MarshalApplication(raknet.ObjectUpdateContractMessage{
			ObjectID: owner.objectID,
			Object: raknet.ObjectReflection{Team: &team, Position: &projectilePosition,
				LinearVelocity: &linearVelocity},
		})
		if err != nil {
			return nil, reflectedCount, fmt.Errorf("arenaReflectionObject[%d]: %w", owner.objectID, err)
		}
		targetObjectID := uint32(0)
		initialDirection := raknet.Vector3{X: direction.X, Y: direction.Y}
		reflectedLastUpdate := int32(1000)
		locomotionPacket, err := raknet.MarshalApplication(raknet.LocomotionUpdateContractMessage{
			ObjectID: owner.objectID,
			Locomotion: raknet.LocomotionReflection{
				Projectile: &raknet.ProjectileParameter{Speed: speed, Range: owner.run.Definition().Distance,
					Direction: initialDirection},
				TargetObjectID: &targetObjectID, TargetPosition: &projectilePosition,
				InitialDirection: &initialDirection, ReflectedLastUpdate: &reflectedLastUpdate,
			},
		})
		if err != nil {
			return nil, reflectedCount, fmt.Errorf("arenaReflectionLocomotion[%d]: %w", owner.objectID, err)
		}
		retirement := e.runtime.registry.projectileRetirements[owner.run]
		if retirement == nil || retirement.isRetired || retirement.isRetirementPending {
			continue
		}
		err = e.runtime.registry.retireProjectileFreezesLocked(retirement)
		if err != nil {
			return nil, reflectedCount, fmt.Errorf("arenaReflectionFreeze[%d]: %w", owner.objectID, err)
		}
		e.runtime.registry.retireTimeBubbleProjectileLocked(nil, owner.objectID, owner.run, now)
		retirement.isRetired = true
		delete(e.runtime.registry.projectileRetirements, owner.run)
		e.runtime.registry.untrackRetiredProjectileLocked(retirement)
		owner.run.Stop()
		packets = append(packets, objectPacket, locomotionPacket)
		reflectedCount++
	}
	*source = e.runtime.registry.sessions[e.sessionKey]
	return packets, reflectedCount, nil
}

func (e arenaRepulsionReset) finish() {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	member, isFound := e.registry.sessions[e.targetKey]
	if !isFound || member.binding.Mode != game.ModeArena || member.binding.GameID != e.gameID ||
		member.generation != e.generation || member.deployedObjectID != e.objectID ||
		member.playerMotionRevision() != e.motionRevision || member.heroKnockbackLanding.readyAt != e.readyAt {
		return
	}
	member.heroKnockbackLanding = heroKnockbackLanding{}
	e.registry.sessions[e.targetKey] = member
	packet, err := npcraknet.ResetAnimation(e.objectID, e.timestamp)
	if err != nil {
		if e.registry.logger != nil {
			e.registry.logger.Printf("Arena Repulsion Wave reset failed target=%d: %v", e.objectID, err)
		}
		return
	}
	e.registry.queueArenaForcePulsePacketsLocked(e.gameID, [][]byte{packet})
}
