package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// The authored suicide continues its fuse independently of target arrival.
// It waits .3s, adds MovementSpeedBuff +2, chases for 1.2s, then pops .04s later.
type campaignScarabChaseStep struct {
	schedule campaignExploderScarabSchedule
	movedAt  time.Time
	endsAt   time.Time
	isMoving bool
}

func (e campaignExploderScarabSchedule) resetMovement() ([][]byte, error) {
	r := e.request.runtime
	r.registry.mutex.RLock()
	member, isFound := r.registry.sessions[e.request.sessionKey]
	if !isFound || !member.isCampaignNPCSourceGenerationActive(
		e.request.generation, e.request.objectID, e.plan.ActionGeneration) {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	enemy, isEnemyFound := member.zone.NPCs().NPC(e.request.objectID)
	r.registry.mutex.RUnlock()
	if !isEnemyFound {
		return nil, nil
	}
	packets, err := npcraknet.StopAtPose(e.request.objectID, enemy.Plan.Position, enemy.Facing)
	if err != nil {
		return nil, fmt.Errorf("scarabResetStop: %w", err)
	}
	speed, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: e.request.objectID, Value: map[uint8]float32{48: enemy.Plan.MovementSpeedBuff},
	})
	if err != nil {
		return nil, fmt.Errorf("scarabResetSpeed: %w", err)
	}
	return append(packets, speed), nil
}

func (e campaignExploderScarabSchedule) arm() ([][]byte, error) {
	r := e.request.runtime
	r.registry.mutex.RLock()
	member, isFound := r.registry.sessions[e.request.sessionKey]
	isCurrent := isFound && member.isCampaignNPCSourceGenerationActive(
		e.request.generation, e.request.objectID, e.plan.ActionGeneration,
	)
	r.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	alert, err := npcraknet.DeathDetonation(e.request.objectID, e.plan.Profile.TargetEffectName)
	if err != nil {
		return e.fail("scarabAlert", err)
	}
	speed, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: e.request.objectID, Value: map[uint8]float32{48: 2},
	})
	if err != nil {
		return e.fail("scarabSpeed", err)
	}
	at := r.now()
	step := campaignScarabChaseStep{schedule: e, movedAt: at, endsAt: at.Add(1200 * time.Millisecond)}
	packets, err := step.produce()
	if err != nil {
		return nil, fmt.Errorf("scarabChase: %w", err)
	}
	return append([][]byte{alert, speed}, packets...), nil
}

func (e campaignScarabChaseStep) produce() ([][]byte, error) {
	req := e.schedule.request
	r := req.runtime
	at := r.now()
	r.registry.mutex.Lock()
	member, isFound := r.registry.sessions[req.sessionKey]
	isCurrent := isFound && member.isCampaignNPCSourceGenerationActive(
		req.generation, req.objectID, e.schedule.plan.ActionGeneration,
	)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	enemy, isEnemyFound := member.zone.NPCs().NPC(req.objectID)
	if !isEnemyFound || enemy.IsDefeated {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	var err error
	member, err = r.pursuit.advanceTargetPoseAtLocked(member, enemy.TargetObjectID, at)
	if err != nil {
		r.registry.mutex.Unlock()
		return e.schedule.fail("scarabTargetPose", err)
	}
	target, isTargetFound := member.campaignNPCTarget(req.generation, enemy.TargetObjectID)
	profile := e.schedule.plan.Profile
	profile.Range = 0.5
	baseSpeed := enemy.Plan.NPCProfile.BaseCombatSpeed
	if baseSpeed <= 0 {
		baseSpeed = profile.MovementSpeed / max(float32(0.1), 1+enemy.Plan.MovementSpeedBuff)
	}
	profile.MovementSpeed += 2 * baseSpeed
	profile.MovementSpeed *= member.zone.NPCs().SlowMovementScale(req.objectID, at)
	isPaused := member.zone.NPCs().StunRemaining(req.objectID, at) > 0 ||
		member.zone.NPCs().SleepRemaining(req.objectID, at) > 0 ||
		member.zone.NPCs().RootRemaining(req.objectID, at) > 0
	if isTargetFound && !isPaused {
		advancedAt := at
		if advancedAt.After(e.endsAt) {
			advancedAt = e.endsAt
		}
		elapsed := max(time.Duration(0), advancedAt.Sub(e.movedAt))
		// Arming can publish the chase in the same clock tick that sets
		// movedAt. There is no displacement to integrate yet; still publish
		// its movement goal and continue the authored timed fuse below.
		if elapsed > 0 {
			step, advanceErr := member.zone.NPCs().AdvancePursuit(member.zone.Navigation(), req.objectID,
				target.Position, profile.Range, profile.MovementSpeed,
				enemy.Plan.ActorFootprintRadius(), elapsed)
			if advanceErr != nil {
				r.registry.mutex.Unlock()
				return e.schedule.fail("scarabAdvance", advanceErr)
			}
			isPaused = step.IsBlocked || step.IsInRange
		}
	}
	enemy, isEnemyFound = member.zone.NPCs().NPC(req.objectID)
	r.registry.sessions[req.sessionKey] = member
	r.registry.mutex.Unlock()
	if !isEnemyFound {
		return nil, nil
	}
	if !at.Before(e.endsAt) {
		packets, stopErr := npcraknet.StopAtPose(req.objectID, enemy.Plan.Position, enemy.Facing)
		if stopErr != nil {
			return e.schedule.fail("scarabStop", stopErr)
		}
		animation, animationErr := npcraknet.AnimationState(req.objectID,
			profile.AnimationName, req.timestamp+1500)
		if animationErr != nil {
			return e.schedule.fail("scarabPopAnimation", animationErr)
		}
		cancel, scheduleErr := scheduleNPCProducers(r.registry, req.packet,
			[]raknet.ScheduledPacketProducer{{Delay: 40 * time.Millisecond, Produce: e.schedule.hit}})
		if scheduleErr != nil {
			return e.schedule.fail("scarabPopSchedule", scheduleErr)
		}
		if cancel == nil {
			return e.schedule.fail("scarabPopCancel", fmt.Errorf("cancellation unavailable"))
		}
		return append(packets, animation), nil
	}
	var packets [][]byte
	if isTargetFound && !isPaused {
		if e.isMoving {
			// Native 539810 overwrites both the destination and corridor
			// waypoint. Keep MoveToObject's path and stopping distance by
			// redirecting through the complete native movement command.
			packets, err = npcraknet.PursuitRedirect(req.objectID,
				enemy.Plan.Position, target.ObjectID, target.Position, profile.Range)
		} else {
			packets, err = npcraknet.Pursuit(zonenpc.FirstActionPlan{
				ObjectID: req.objectID, TargetObjectID: target.ObjectID,
				SourcePosition: enemy.Plan.Position, TargetPosition: target.Position,
				Profile: profile, IsPursuitNeeded: true,
			})
		}
		e.isMoving = true
	} else {
		packets, err = npcraknet.StopAtPose(req.objectID, enemy.Plan.Position, enemy.Facing)
		e.isMoving = false
	}
	if err != nil {
		return e.schedule.fail("scarabMovement", err)
	}
	e.movedAt = at
	cancel, scheduleErr := scheduleNPCProducers(r.registry, req.packet,
		[]raknet.ScheduledPacketProducer{{
			Delay: min(50*time.Millisecond, e.endsAt.Sub(at)), Produce: e.produce,
		}})
	if scheduleErr != nil {
		return e.schedule.fail("scarabTickSchedule", scheduleErr)
	}
	if cancel == nil {
		return e.schedule.fail("scarabTickCancel", fmt.Errorf("cancellation unavailable"))
	}
	return packets, nil
}
