package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

func (e campaignNPCActionRuntime) produceNomadCleave(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	request := campaignNPCAttackRequest{
		runtime: e, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
		kind: campaignNPCAttackMelee,
	}
	resume := campaignEnemyChargeNextStep{runtime: e, packet: packet, sessionKey: sessionKey, generation: generation, objectID: objectID}
	isDeferred, err := e.pursuit.deferAction(
		packet, sessionKey, generation, objectID, timestamp, resume.resume,
	)
	if err != nil {
		return nil, fmt.Errorf("nomadCleaveStun: %w", err)
	}
	if isDeferred {
		return nil, nil
	}
	e.registry.mutex.RLock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		e.registry.mutex.RUnlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, enemy.TargetObjectID,
	)
	e.registry.mutex.RUnlock()
	if !isEnemyFound || !isTargetFound {
		e.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	profile := zonenpc.NomadSpecialOneCleaveProfile(enemy.Plan.NounName)
	profile = zonenpc.ApplyAffixTiming(profile, enemy.Plan)
	plan, err := zonenpc.PlanAttackWithProfile(
		enemy, target.ObjectID, target.Position, profile, target.ActorFootprintRadius,
	)
	if err != nil {
		action, actionErr := campaignNPCActionWithProfile(
			enemy.Plan, target.ObjectID, target.Position, profile,
			target.ActorFootprintRadius,
		)
		if actionErr != nil {
			return nil, fmt.Errorf("nomadCleavePursuitPlan: %w", actionErr)
		}
		if !action.IsPursuitNeeded {
			return nil, nil
		}
		packets, marshalErr := npcraknet.Pursuit(action)
		if marshalErr != nil {
			return nil, fmt.Errorf("nomadCleavePursuitMarshal: %w", marshalErr)
		}
		scheduleErr := e.pursuit.schedule(
			packet, sessionKey, generation, objectID, timestamp,
			action.TargetPosition, profile, resume.resume,
		)
		if scheduleErr != nil {
			return nil, fmt.Errorf("nomadCleavePursuitSchedule: %w", scheduleErr)
		}
		return packets, nil
	}
	packets, err := e.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		return nil, fmt.Errorf("nomadCleaveStart: %w", err)
	}
	timeline, err := zonenpc.TimelineForAttack(plan)
	if err != nil {
		return nil, fmt.Errorf("nomadCleaveTimeline: %w", err)
	}
	nextDelay := timeline.NextDelay
	schedule := campaignNomadChargeAttackSchedule{
		attack:    campaignNPCAttackSchedule{request: request, plan: plan},
		nextDelay: nextDelay,
	}
	cancel, scheduleErr := scheduleNPCProducers(e.registry, packet, []raknet.ScheduledPacketProducer{
		{Delay: timeline.HitDelay, Produce: nomadCleaveHit{attack: schedule.attack}.produce},
		{Delay: nextDelay, Produce: schedule.next},
	})
	if scheduleErr == nil && cancel == nil {
		scheduleErr = fmt.Errorf("cleave continuation unavailable")
	}
	if scheduleErr != nil {
		e.releaseAction(sessionKey, generation, objectID)
		return nil, fmt.Errorf("nomadCleaveSchedule: %w", scheduleErr)
	}
	return packets, nil
}

type nomadCleaveHit struct{ attack campaignNPCAttackSchedule }

func (e nomadCleaveHit) produce() ([][]byte, error) {
	req := e.attack.request
	req.runtime.registry.mutex.RLock()
	member, isFound := req.runtime.registry.sessions[req.sessionKey]
	if !isFound || !member.isCampaignNPCSourceGenerationActive(req.generation, req.objectID, e.attack.plan.ActionGeneration) {
		req.runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	enemy, isEnemyFound := member.zone.NPCs().NPC(req.objectID)
	target, isTargetFound := member.campaignNPCTarget(req.generation, e.attack.plan.TargetObjectID)
	req.runtime.registry.mutex.RUnlock()
	if !isEnemyFound || !isTargetFound {
		return nil, nil
	}
	arc, err := zoneability.NewMeleeArc(enemy.Plan.Position, enemy.Facing, 2.75+enemy.Plan.ActorFootprintRadius(), 90)
	if err != nil {
		return nil, fmt.Errorf("cleaveArc: %w", err)
	}
	if !arc.Contains(target.Position, target.ActorFootprintRadius) {
		return nil, nil
	}
	packets, err := e.attack.hit()
	if err != nil {
		return nil, fmt.Errorf("cleaveHit: %w", err)
	}
	return packets, nil
}
