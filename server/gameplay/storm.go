package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sporenet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type campaignMerakStormRun struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	timestamp  uint64
	source     zonenpc.Snapshot
	profile    zonenpc.ActionProfile
	plan       zonenpc.AttackPlan
	center     game.Vec3
	cancel     raknet.CancelSchedule
	isReleased bool
	isStopped  bool
}

type campaignMerakStormHit struct {
	run   *campaignMerakStormRun
	delay time.Duration
}

func (e campaignNPCActionRuntime) produceMerakStorm(
	packet raknet.Packet, sessionKey string, generation uint64, objectID uint32, timestamp uint64,
) ([][]byte, bool, error) {
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || !member.isCampaignNPCSourceActive(generation, objectID) {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	source, isSourceFound := member.zone.NPCs().NPC(objectID)
	profile, isProfileFound := zonenpc.MerakStormProfile(source.Plan)
	target, isTargetFound := member.campaignNPCTarget(generation, source.TargetObjectID)
	if !isSourceFound || !isProfileFound || !isTargetFound || source.Plan.OwnerObjectID != 0 ||
		timestamp < member.campaignMerakStormReadiness[objectID] ||
		member.campaignMerakStormRuns[objectID] != nil ||
		member.zone.NPCs().SilenceRemaining(objectID, e.now()) > 0 {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	// The authored phase has no additional range or health condition. The
	// point-blank template's targeted flag fixes the area at this target pose.
	plan, err := zonenpc.PlanAreaAttackWithProfile(source, target.ObjectID, target.Position, profile)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, true, fmt.Errorf("stormSnapshot: %w", err)
	}
	run := &campaignMerakStormRun{runtime: e, packet: packet.Autonomous(),
		sessionKey: sessionKey, generation: generation, timestamp: timestamp,
		source: source, profile: profile, plan: plan, center: target.Position}
	if member.campaignMerakStormRuns == nil {
		member.campaignMerakStormRuns = make(map[uint32]*campaignMerakStormRun)
	}
	member.campaignMerakStormRuns[objectID] = run
	e.registry.sessions[sessionKey] = member
	e.registry.mutex.Unlock()
	packets, err := e.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		run.retire()
		return nil, true, fmt.Errorf("stormStart: %w", err)
	}
	producers := []raknet.ScheduledPacketProducer{{Delay: profile.ReleaseDelay, Produce: run.release}}
	for second := 8; second <= 15; second++ {
		hit := campaignMerakStormHit{run: run, delay: time.Duration(second) * time.Second}
		producers = append(producers, raknet.ScheduledPacketProducer{Delay: hit.delay, Produce: hit.produce})
	}
	// Retire after the final damage callback even on schedulers that order equal deadlines arbitrarily.
	producers = append(producers, raknet.ScheduledPacketProducer{Delay: 15*time.Second + time.Millisecond, Produce: run.finish})
	cancel, err := scheduleNPCProducers(e.registry, run.packet, producers)
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		run.retire()
		e.releaseActionGeneration(sessionKey, generation, objectID, source.ActionGeneration)
		return nil, true, fmt.Errorf("stormSchedule: %w", err)
	}
	e.registry.mutex.Lock()
	if run.isStopped {
		cancel()
	} else {
		run.cancel = cancel
	}
	e.registry.mutex.Unlock()
	return packets, true, nil
}

func (e *campaignMerakStormRun) stop() {
	if e.isStopped {
		return
	}
	e.isStopped = true
	if e.cancel != nil {
		e.cancel()
	}
}

func (e *campaignMerakStormRun) retire() {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	e.stop()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if isFound && member.generation == e.generation && member.campaignMerakStormRuns[e.source.Plan.ObjectID] == e {
		delete(member.campaignMerakStormRuns, e.source.Plan.ObjectID)
		e.runtime.registry.sessions[e.sessionKey] = member
	}
}

func (e *campaignMerakStormRun) finish() ([][]byte, error) {
	e.retire()
	return nil, nil
}

func (e *campaignMerakStormRun) release() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := !e.isStopped && isFound && member.generation == e.generation &&
		member.isCampaignNPCSourceGenerationActive(e.generation, e.source.Plan.ObjectID, e.source.ActionGeneration)
	if !isCurrent {
		e.stop()
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	isInterrupted := !member.isCampaignNPCActionActiveAt(e.generation, e.source.Plan.ObjectID, e.runtime.now()) ||
		member.zone.NPCs().SilenceRemaining(e.source.Plan.ObjectID, e.runtime.now()) > 0
	if isInterrupted {
		e.stop()
		delete(member.campaignMerakStormRuns, e.source.Plan.ObjectID)
	} else {
		e.isReleased = true
		if member.campaignMerakStormReadiness == nil {
			member.campaignMerakStormReadiness = make(map[uint32]uint64)
		}
		member.campaignMerakStormReadiness[e.source.Plan.ObjectID] = e.timestamp +
			uint64((e.profile.ReleaseDelay+e.profile.Cooldown)/time.Millisecond)
	}
	e.runtime.registry.sessions[e.sessionKey] = member
	e.runtime.registry.mutex.Unlock()
	packets := make([][]byte, 0)
	if !isInterrupted {
		effectPacket, err := npcraknet.PositionedEffect(e.profile.GroundEffectName, e.center)
		if err != nil {
			e.retire()
			return nil, fmt.Errorf("stormEffect: %w", err)
		}
		packets = append(packets, effectPacket)
	}
	step := campaignNPCFirstActionStep{runtime: e.runtime, packet: e.packet,
		sessionKey: e.sessionKey, generation: e.generation, objectID: e.source.Plan.ObjectID,
		actionGeneration: e.source.ActionGeneration,
		timestamp:        e.timestamp + uint64(e.profile.ReleaseDelay/time.Millisecond)}
	nextPackets, err := step.produce()
	if err != nil {
		return packets, fmt.Errorf("stormNext: %w", err)
	}
	return append(packets, nextPackets...), nil
}

func (e campaignMerakStormHit) produce() ([][]byte, error) {
	run := e.run
	run.runtime.registry.mutex.Lock()
	member, isFound := run.runtime.registry.sessions[run.sessionKey]
	if run.isStopped || !run.isReleased || !isFound || member.generation != run.generation ||
		member.isZoneTerminal() || member.zone == nil || member.zone.NPCs() == nil {
		run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	source, isSourceFound := member.zone.NPCs().NPC(run.source.Plan.ObjectID)
	if !isSourceFound || source.IsDefeated || source.HitPoint <= 0 {
		run.stop()
		delete(member.campaignMerakStormRuns, run.source.Plan.ObjectID)
		run.runtime.registry.sessions[run.sessionKey] = member
		run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	packets := make([][]byte, 0)
	statDelta := sporenet.PlayerStatDelta{}
	for _, target := range member.zone.LiveNPCTargets() {
		if target.HitPoint <= 0 || target.Position.Sub(run.center).Length() > run.profile.Radius+target.ActorFootprintRadius {
			continue
		}
		// The template captures agent attributes once before its wait sequence.
		plan := run.plan
		plan.TargetObjectID = target.ObjectID
		plan.TargetPosition = target.Position
		result, err := zonenpc.CommitAttack(member.zone.NPCRandom(), plan,
			run.source.EffectiveCriticalRating(), run.runtime.program.Critical)
		if err != nil {
			run.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("stormCommit: %w", err)
		}
		hitPackets, targetStatDelta, isApplied, err := run.runtime.applyEnemyAreaAttackDamage(
			&member, run.generation, plan, result, run.timestamp+uint64(e.delay/time.Millisecond))
		if err != nil {
			run.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("stormDamage: %w", err)
		}
		if isApplied {
			statDelta.PVEDamageTaken += targetStatDelta.PVEDamageTaken
		}
		packets = append(packets, hitPackets...)
	}
	binding := member.binding
	run.runtime.registry.sessions[run.sessionKey] = member
	run.runtime.registry.mutex.Unlock()
	err := run.runtime.stats.Record(context.Background(), binding, statDelta)
	if err != nil {
		return nil, fmt.Errorf("stormStats: %w", err)
	}
	return packets, nil
}

// Called under the registry lock for source defeat and session teardown.
func (e *gameplayPeerSession) stopCampaignMerakStorms(sourceObjectID uint32) {
	for objectID, run := range e.campaignMerakStormRuns {
		if sourceObjectID != 0 && objectID != sourceObjectID {
			continue
		}
		run.stop()
		delete(e.campaignMerakStormRuns, objectID)
	}
}
