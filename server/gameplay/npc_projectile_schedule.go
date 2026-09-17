package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zone "github.com/darkspinnet/darkspin/server/zone"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type campaignNPCProjectileKind uint8

const (
	campaignNPCProjectileHaster campaignNPCProjectileKind = iota + 1
	campaignNPCProjectileZelem
)

type campaignNPCProjectileSchedule struct {
	runtime                     campaignNPCActionRuntime
	packet                      raknet.Packet
	sessionKey                  string
	generation                  uint64
	sourceObjectID              uint32
	projectileObjectID          uint32
	timestamp                   uint64
	impactDeadline              time.Duration
	lastDeadline                time.Duration
	nextDelay                   time.Duration
	hasteEndTimestamp           uint64
	kind                        campaignNPCProjectileKind
	source                      zonenpc.Snapshot
	target                      zone.NPCTarget
	plan                        zonenpc.AttackPlan
	result                      zonenpc.AttackResult
	geometry                    zoneability.ProjectileCollisionGeometry
	facing                      raknet.Vector3
	binding                     game.GameplayBinding
	run                         *abilityraknet.ProjectileRun
	flight                      *campaignNPCProjectileFlight
	volley                      campaignNPCProjectileVolley
	projectileSourcePosition    game.Vec3
	projectileTargetPosition    game.Vec3
	isProjectileTrajectoryFound bool
}

type campaignNPCRecoveryStep struct {
	schedule campaignNPCProjectileSchedule
	duration time.Duration
}

func (e campaignNPCRecoveryStep) produce() ([][]byte, error) {
	schedule := e.schedule
	timestamp := schedule.timestamp + uint64(e.duration/time.Millisecond)
	schedule.runtime.registry.mutex.RLock()
	current, isFound := schedule.runtime.registry.sessions[schedule.sessionKey]
	isCurrent := isFound && current.isCampaignNPCSourceGenerationActive(
		schedule.generation, schedule.sourceObjectID,
		schedule.plan.ActionGeneration,
	)
	schedule.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	resetPacket, err := npcraknet.ResetAnimation(
		schedule.sourceObjectID, timestamp,
	)
	if err != nil {
		return schedule.fail("enemyRecoveryReset", err)
	}
	packets, err := schedule.resume(timestamp)
	if err != nil {
		return schedule.fail("enemyRecoveryResume", err)
	}
	if schedule.runtime.logger != nil {
		schedule.runtime.logger.Printf(
			"RakNet NPC recovery completed source=%d ability=%q duration_ms=%d",
			schedule.sourceObjectID, schedule.plan.Profile.AbilityName,
			e.duration.Milliseconds(),
		)
	}
	return append([][]byte{resetPacket}, packets...), nil
}

func (e campaignNPCProjectileSchedule) recover(
	timestamp uint64,
) ([][]byte, bool, error) {
	profile := e.plan.Profile
	if profile.RecoveryEveryActionCount == 0 ||
		profile.RecoveryDuration <= 0 || profile.RecoveryAnimationName == "" {
		return nil, false, nil
	}
	e.runtime.registry.mutex.Lock()
	current, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && current.isCampaignNPCSourceGenerationActive(
		e.generation, e.sourceObjectID, e.plan.ActionGeneration,
	)
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, true, nil
	}
	if current.campaignNPCRecoveryActionCounts == nil {
		current.campaignNPCRecoveryActionCounts = make(map[uint32]uint32)
	}
	current.campaignNPCRecoveryActionCounts[e.sourceObjectID]++
	actionCount := current.campaignNPCRecoveryActionCounts[e.sourceObjectID]
	e.runtime.registry.sessions[e.sessionKey] = current
	e.runtime.registry.mutex.Unlock()
	if actionCount%profile.RecoveryEveryActionCount != 0 {
		return nil, false, nil
	}
	animationPacket, err := npcraknet.AnimationState(
		e.sourceObjectID, profile.RecoveryAnimationName, timestamp,
	)
	if err != nil {
		return nil, true, fmt.Errorf("enemyRecoveryAnimation: %w", err)
	}
	step := campaignNPCRecoveryStep{schedule: e, duration: profile.RecoveryDuration}
	step.schedule.timestamp = timestamp
	producer := raknet.ScheduledPacketProducer{
		Delay: profile.RecoveryDuration, Produce: step.produce,
	}
	cancel, err := e.packet.ScheduleProducers(
		[]raknet.ScheduledPacketProducer{producer},
	)
	if err != nil {
		return nil, true, fmt.Errorf("enemyRecoverySchedule: %w", err)
	}
	if cancel == nil {
		return nil, true, errors.New("enemy recovery cancellation unavailable")
	}
	if e.runtime.logger != nil {
		e.runtime.logger.Printf(
			"RakNet NPC recovery started source=%d ability=%q action=%d every=%d animation=%q duration_ms=%d",
			e.sourceObjectID, profile.AbilityName, actionCount,
			profile.RecoveryEveryActionCount, profile.RecoveryAnimationName,
			profile.RecoveryDuration.Milliseconds(),
		)
	}
	return [][]byte{animationPacket}, true, nil
}

type campaignNPCProjectileVolley struct {
	isActive               bool
	shotIndex              int
	startTimestamp         uint64
	retainedTargetPosition game.Vec3
	targetObjectIDs        []uint32
	plan                   zonenpc.AttackPlan
}

type campaignNPCProjectileStep struct {
	schedule     campaignNPCProjectileSchedule
	deadline     time.Duration
	isFlightPoll bool
}

type campaignNPCProjectileExpiryStep struct {
	schedule campaignNPCProjectileSchedule
}

func (e campaignNPCProjectileSchedule) deleteAfterSourceLoss(
	step string,
) ([][]byte, error) {
	packets, isDeleted, err := e.run.DeleteProjectile(context.Background())
	if err != nil {
		return e.fail(step, err)
	}
	if !isDeleted {
		e.runtime.projectile.retire(
			e.sessionKey, e.generation, e.projectileObjectID, e.run,
		)
		return nil, nil
	}
	e.runtime.projectile.complete(
		e.sessionKey, e.generation, e.projectileObjectID, e.run,
	)
	return packets, nil
}

func (e campaignNPCProjectileExpiryStep) produce() ([][]byte, error) {
	e.schedule.run.ClearCancel()
	packets, isDeleted, err := e.schedule.run.DeleteProjectile(context.Background())
	if err != nil {
		return e.schedule.fail("enemyProjectileExpire", err)
	}
	if !isDeleted {
		return nil, nil
	}
	e.schedule.runtime.projectile.complete(
		e.schedule.sessionKey, e.schedule.generation,
		e.schedule.projectileObjectID, e.schedule.run,
	)
	return packets, nil
}

func (e campaignNPCProjectileSchedule) producer(
	deadline time.Duration,
) raknet.ScheduledPacketProducer {
	step := campaignNPCProjectileStep{schedule: e, deadline: deadline}
	return raknet.ScheduledPacketProducer{Delay: deadline, Produce: step.produce}
}

func (e campaignNPCProjectileSchedule) fail(
	step string, err error,
) ([][]byte, error) {
	e.runtime.projectile.retire(
		e.sessionKey, e.generation, e.projectileObjectID, e.run,
	)
	e.runtime.releaseActionGeneration(
		e.sessionKey, e.generation, e.sourceObjectID,
		e.plan.ActionGeneration,
	)
	return nil, fmt.Errorf("%s: %w", step, err)
}

func (e campaignNPCProjectileSchedule) resume(
	timestamp uint64,
) ([][]byte, error) {
	if e.plan.Profile.AbilityName == "GhostlyBolt" {
		return e.runtime.produceResurrection(
			e.packet, e.sessionKey, e.generation, e.sourceObjectID, timestamp,
		)
	}
	switch e.kind {
	case campaignNPCProjectileHaster:
		return e.runtime.produceHasterProjectile(
			e.packet, e.sessionKey, e.generation, e.sourceObjectID,
			timestamp, e.hasteEndTimestamp,
		)
	case campaignNPCProjectileZelem:
		return e.runtime.produceZelemShot(
			e.packet, e.sessionKey, e.generation, e.sourceObjectID, timestamp,
		)
	default:
		return nil, errors.New("enemy projectile kind unsupported")
	}
}

func (e campaignNPCProjectileSchedule) next() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	current, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && current.isCampaignNPCSourceGenerationActive(
		e.generation, e.sourceObjectID, e.plan.ActionGeneration,
	)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	timestamp := e.timestamp + uint64(e.nextDelay/time.Millisecond)
	var packets [][]byte
	var err error
	if e.volley.isActive &&
		e.volley.shotIndex+1 < int(e.plan.Profile.ProjectileShotCount) {
		nextVolley := e.volley
		nextVolley.shotIndex++
		e.runtime.logger.Printf(
			"RakNet NPC projectile volley continuing source=%d ability=%q shot=%d/%d delay_ms=%d",
			e.sourceObjectID, e.plan.Profile.AbilityName,
			nextVolley.shotIndex+1, e.plan.Profile.ProjectileShotCount,
			e.nextDelay.Milliseconds(),
		)
		packets, err = e.runtime.produceZelemShotWithVolley(
			e.packet, e.sessionKey, e.generation, e.sourceObjectID,
			timestamp, nextVolley,
		)
	} else if e.plan.Profile.AbilityName == "ZelemMarkSeeker" {
		packets, err = e.runtime.producePolarisPhaseTransition(
			e.packet, e.sessionKey, e.generation, e.sourceObjectID, timestamp,
			polarisNextGravityOrb,
		)
	} else if e.plan.Profile.AbilityName == "Puller" {
		packets, err = e.runtime.produceEnemyMeleeWithPull(
			e.packet, e.sessionKey, e.generation, e.sourceObjectID, timestamp,
			false,
		)
	} else if e.volley.isActive {
		var isRecovering bool
		packets, isRecovering, err = e.recover(timestamp)
		if err == nil && !isRecovering {
			packets, err = e.resume(timestamp)
		}
	} else if !e.volley.isActive {
		packets, err = e.runtime.produceBoundedStrafeOrIdle(
			e.packet, e.sessionKey, e.generation, e.sourceObjectID,
			e.plan.ActionGeneration, timestamp, e.plan.Profile,
			campaignNPCStrafeModeOrdinary, e.resume,
		)
	} else {
		packets, err = e.resume(timestamp)
	}
	if err != nil {
		return e.fail("enemyProjectileNext", err)
	}
	return packets, nil
}
