package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type campaignCitadelSpecialFourState struct {
	nextRepairTimestamp uint64
	nextSlamTimestamp   uint64
}

type campaignCitadelSpecialFourRequest struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
}

func (e campaignCitadelSpecialFourRequest) fail(
	step string, err error,
) ([][]byte, error) {
	e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
	return nil, fmt.Errorf("%s: %w", step, err)
}

func (e campaignCitadelSpecialFourRequest) resume(
	timestamp uint64,
) ([][]byte, error) {
	packets, _, err := e.runtime.produceCitadelSpecialFour(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
	if err != nil {
		return e.fail("citadelSpecialFourResume", err)
	}
	return packets, nil
}

type campaignCitadelRepairSchedule struct {
	request campaignCitadelSpecialFourRequest
	profile zonenpc.ActionProfile
	tick    uint32
}

func (e campaignCitadelRepairSchedule) produce() ([][]byte, error) {
	runtime := e.request.runtime
	isDeferred, err := runtime.pursuit.deferCommit(
		e.request.packet, e.request.sessionKey, e.request.generation,
		e.request.objectID, e.produce,
	)
	if err != nil {
		return e.request.fail("citadelRepairDefer", err)
	}
	if isDeferred {
		return nil, nil
	}
	runtime.registry.mutex.Lock()
	peerSession, isFound := runtime.registry.sessions[e.request.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		e.request.generation, e.request.objectID,
	)
	if !isCurrent {
		runtime.registry.mutex.Unlock()
		return nil, nil
	}
	npc, isNPCFound := peerSession.zone.NPCs().NPC(e.request.objectID)
	if !isNPCFound || npc.IsDefeated || npc.HitPoint <= 0 {
		runtime.registry.mutex.Unlock()
		runtime.releaseAction(
			e.request.sessionKey, e.request.generation, e.request.objectID,
		)
		return nil, nil
	}
	maximumHitPoint := npc.Plan.NPCProfile.HitPoint
	if maximumHitPoint <= 0 {
		runtime.registry.mutex.Unlock()
		return e.request.fail(
			"citadelRepairHealth",
			errors.New("maximum health unavailable"),
		)
	}
	timestamp := e.request.timestamp + uint64(e.profile.TickDuration/time.Millisecond)
	if npc.HitPoint/maximumHitPoint >= 0.9 {
		runtime.registry.mutex.Unlock()
		return e.finish(timestamp)
	}
	healed, healedAmount, err := peerSession.zone.NPCs().Heal(
		e.request.objectID, e.profile.MinimumHealing,
	)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return e.request.fail("citadelRepairHeal", err)
	}
	runtime.registry.sessions[e.request.sessionKey] = peerSession
	runtime.registry.mutex.Unlock()
	packets := make([][]byte, 0, 4)
	if healedAmount > 0 {
		objectiveErr := peerSession.zone.RecordNPCHeal(
			peerSession.zone.Context(), e.request.objectID,
			e.request.objectID, healedAmount,
		)
		if objectiveErr != nil {
			runtime.logger.Printf(
				"RakNet Citadel repair objective omitted object=%d: %v",
				e.request.objectID, objectiveErr,
			)
		}
		healPackets, marshalErr := npcraknet.HealDelta(
			e.request.objectID, healed, healedAmount,
		)
		if marshalErr != nil {
			runtime.logger.Printf(
				"RakNet Citadel repair presentation omitted object=%d: %v",
				e.request.objectID, marshalErr,
			)
		} else {
			packets = append(packets, healPackets...)
		}
	}
	if e.tick >= e.profile.NumberOfTicks || healed.HitPoint/maximumHitPoint >= 0.9 {
		finishPackets, finishErr := e.finish(timestamp)
		if finishErr != nil {
			return nil, finishErr
		}
		return append(packets, finishPackets...), nil
	}
	next := e
	next.tick++
	next.request.timestamp = timestamp
	cancel, scheduleErr := e.request.packet.ScheduleProducers(
		[]raknet.ScheduledPacketProducer{{
			Delay: e.profile.TickDuration, Produce: next.produce,
		}},
	)
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		return e.request.fail("citadelRepairTickSchedule", scheduleErr)
	}
	return packets, nil
}

func (e campaignCitadelRepairSchedule) finish(
	timestamp uint64,
) ([][]byte, error) {
	resetPacket, err := npcraknet.ResetAnimation(e.request.objectID, timestamp)
	if err != nil {
		e.request.runtime.logger.Printf(
			"RakNet Citadel repair reset presentation omitted object=%d: %v",
			e.request.objectID, err,
		)
	}
	nextPackets, err := e.request.resume(timestamp)
	if err != nil {
		return e.request.fail("citadelRepairNext", err)
	}
	if resetPacket == nil {
		return nextPackets, nil
	}
	return append([][]byte{resetPacket}, nextPackets...), nil
}

func (r campaignNPCActionRuntime) produceCitadelSpecialFour(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, bool, error) {
	request := campaignCitadelSpecialFourRequest{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil, false, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	repairProfile, isFamilyFound := zonenpc.CitadelSpecialFourRoboRepairProfile(
		enemy.Plan.NounName,
	)
	isSilenced := isEnemyFound &&
		peerSession.zone.NPCs().SilenceRemaining(objectID, r.now()) > 0
	r.registry.mutex.RUnlock()
	if !isEnemyFound || !isFamilyFound {
		return nil, false, nil
	}
	if isSilenced {
		return nil, false, nil
	}
	isDeferred, err := r.pursuit.deferAction(
		packet, sessionKey, generation, objectID, timestamp, request.resume,
	)
	if err != nil {
		packets, failErr := request.fail("citadelSpecialFourStun", err)
		return packets, true, failErr
	}
	if isDeferred {
		return nil, true, nil
	}
	r.registry.mutex.Lock()
	peerSession, isFound = r.registry.sessions[sessionKey]
	isCurrent = isFound && peerSession.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, true, nil
	}
	enemy, isEnemyFound = peerSession.zone.NPCs().NPC(objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, enemy.TargetObjectID,
	)
	if !isEnemyFound || enemy.IsDefeated || !isTargetFound {
		r.registry.mutex.Unlock()
		r.releaseAction(sessionKey, generation, objectID)
		return nil, true, nil
	}
	if peerSession.campaignNPCCitadelSpecialFourStates == nil {
		peerSession.campaignNPCCitadelSpecialFourStates = make(
			map[uint32]campaignCitadelSpecialFourState,
		)
	}
	state := peerSession.campaignNPCCitadelSpecialFourStates[objectID]
	maximumHitPoint := enemy.Plan.NPCProfile.HitPoint
	isRepairReady := maximumHitPoint > 0 && enemy.HitPoint/maximumHitPoint <= 0.5 &&
		timestamp >= state.nextRepairTimestamp
	if isRepairReady {
		previousState := state
		state.nextRepairTimestamp = timestamp + uint64(repairProfile.Cooldown/time.Millisecond)
		peerSession.campaignNPCCitadelSpecialFourStates[objectID] = state
		r.registry.sessions[sessionKey] = peerSession
		r.registry.mutex.Unlock()
		packets, startErr := r.startCitadelSpecialFourRepair(
			request, repairProfile,
		)
		if startErr != nil {
			r.restoreCitadelSpecialFourState(
				sessionKey, generation, objectID, state, previousState,
			)
			packets, failErr := request.fail("citadelRepairStart", startErr)
			return packets, true, failErr
		}
		return packets, true, nil
	}
	slamProfile, isSlamProfileFound := zonenpc.CitadelSpecialFourGroundSlamProfile(enemy.Plan.NounName)
	if !isSlamProfileFound {
		r.registry.mutex.Unlock()
		return nil, true, errors.New("citadel ground slam profile unavailable")
	}
	slamPlan, slamErr := zonenpc.PlanAttackWithProfile(
		enemy, target.ObjectID, target.Position, slamProfile, target.FootprintRadius,
	)
	if timestamp >= state.nextSlamTimestamp && slamErr == nil {
		previousState := state
		state.nextSlamTimestamp = timestamp + uint64(slamProfile.Cooldown/time.Millisecond)
		peerSession.campaignNPCCitadelSpecialFourStates[objectID] = state
		r.registry.sessions[sessionKey] = peerSession
		r.registry.mutex.Unlock()
		packets, startErr := r.startCitadelSpecialFourSlam(request, slamPlan)
		if startErr != nil {
			r.restoreCitadelSpecialFourState(
				sessionKey, generation, objectID, state, previousState,
			)
			packets, failErr := request.fail("citadelSlamStart", startErr)
			return packets, true, failErr
		}
		return packets, true, nil
	}
	punchProfile, isPunchProfileFound := zonenpc.CitadelSpecialFourPistonPunchProfile(enemy.Plan.NounName)
	if !isPunchProfileFound {
		r.registry.mutex.Unlock()
		return nil, true, errors.New("citadel piston punch profile unavailable")
	}
	punchPlan, punchErr := zonenpc.PlanAttackWithProfile(
		enemy, target.ObjectID, target.Position, punchProfile, target.FootprintRadius,
	)
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	if punchErr != nil {
		action, actionErr := campaignNPCActionWithProfile(
			enemy.Plan, target.ObjectID, target.Position,
			punchProfile, target.FootprintRadius,
		)
		if actionErr != nil || !action.IsPursuitNeeded {
			return nil, true, nil
		}
		pursuitPackets, marshalErr := npcraknet.Pursuit(action)
		if marshalErr != nil {
			packets, failErr := request.fail(
				"citadelPunchPursuitMarshal", marshalErr,
			)
			return packets, true, failErr
		}
		scheduleErr := r.pursuit.schedule(
			packet, sessionKey, generation, objectID, timestamp,
			action.TargetPosition, punchProfile, request.resume,
		)
		if scheduleErr != nil {
			packets, failErr := request.fail(
				"citadelPunchPursuitSchedule", scheduleErr,
			)
			return packets, true, failErr
		}
		return pursuitPackets, true, nil
	}
	packets, err := r.startCitadelSpecialFourPunch(request, punchPlan)
	if err != nil {
		failPackets, failErr := request.fail("citadelPunchStart", err)
		return failPackets, true, failErr
	}
	return packets, true, nil
}

func (r campaignNPCActionRuntime) startCitadelSpecialFourRepair(
	request campaignCitadelSpecialFourRequest,
	profile zonenpc.ActionProfile,
) ([][]byte, error) {
	animationPacket, err := npcraknet.AnimationState(
		request.objectID, profile.AnimationName, request.timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("citadelRepairAnimation: %w", err)
	}
	schedule := campaignCitadelRepairSchedule{
		request: request, profile: profile, tick: 1,
	}
	cancel, err := request.packet.ScheduleProducers(
		[]raknet.ScheduledPacketProducer{{
			Delay: profile.TickDuration, Produce: schedule.produce,
		}},
	)
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		return nil, fmt.Errorf("citadelRepairSchedule: %w", err)
	}
	return [][]byte{animationPacket}, nil
}

func (r campaignNPCActionRuntime) startCitadelSpecialFourSlam(
	request campaignCitadelSpecialFourRequest,
	plan zonenpc.AttackPlan,
) ([][]byte, error) {
	startPackets, err := r.startNPCAttack(request.sessionKey, request.generation, plan, request.timestamp)
	if err != nil {
		return nil, fmt.Errorf("citadelSlamStart: %w", err)
	}
	schedule := campaignConeSchedule{
		runtime: r, packet: request.packet, sessionKey: request.sessionKey,
		generation: request.generation, objectID: request.objectID,
		timestamp: request.timestamp, nextDelay: plan.Profile.ReleaseDelay,
		plan: plan,
	}
	cancel, err := request.packet.ScheduleProducers([]raknet.ScheduledPacketProducer{
		{Delay: plan.Profile.HitDelay, Produce: schedule.hit},
		{Delay: plan.Profile.ReleaseDelay, Produce: schedule.next},
	})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		return nil, fmt.Errorf("citadelSlamSchedule: %w", err)
	}
	return startPackets, nil
}

func (r campaignNPCActionRuntime) startCitadelSpecialFourPunch(
	request campaignCitadelSpecialFourRequest,
	plan zonenpc.AttackPlan,
) ([][]byte, error) {
	startPackets, err := r.startNPCAttack(request.sessionKey, request.generation, plan, request.timestamp)
	if err != nil {
		return nil, fmt.Errorf("citadelPunchStart: %w", err)
	}
	timeline, err := zonenpc.TimelineForAttack(plan)
	if err != nil {
		return nil, fmt.Errorf("citadelPunchTimeline: %w", err)
	}
	schedule := campaignNPCAttackSchedule{
		request: campaignNPCAttackRequest{
			runtime: r, packet: request.packet, sessionKey: request.sessionKey,
			generation: request.generation, objectID: request.objectID,
			timestamp: request.timestamp, kind: campaignNPCAttackMelee,
		},
		plan: plan,
	}
	cancel, err := request.packet.ScheduleProducers([]raknet.ScheduledPacketProducer{
		{Delay: timeline.HitDelay, Produce: schedule.hit},
		{Delay: timeline.NextDelay, Produce: schedule.next},
	})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		return nil, fmt.Errorf("citadelPunchSchedule: %w", err)
	}
	return startPackets, nil
}

func (r campaignNPCActionRuntime) restoreCitadelSpecialFourState(
	sessionKey string, generation uint64, objectID uint32,
	expected campaignCitadelSpecialFourState,
	previous campaignCitadelSpecialFourState,
) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if isFound && peerSession.generation == generation &&
		peerSession.campaignNPCCitadelSpecialFourStates[objectID] == expected {
		peerSession.campaignNPCCitadelSpecialFourStates[objectID] = previous
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
}
