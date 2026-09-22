package gameplay

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zoneabilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type campaignNPCDrainRun struct {
	mu          sync.Mutex
	isEnded     bool
	effectLease *campaignNPCDrainEffectLease
}

type campaignNPCDrainEffectLease struct {
	pool           *attachedEffectPool
	sourceObjectID uint32
	sourceSlot     uint8
	targetObjectID uint32
	targetSlot     uint8
}

func (e *campaignNPCDrainRun) IsEnded() bool {
	if e == nil {
		return true
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.isEnded
}

func (e *campaignNPCDrainRun) End() bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.isEnded {
		return false
	}
	e.isEnded = true
	return true
}

func (e *campaignNPCDrainRun) BindEffects(
	pool *attachedEffectPool, sourceObjectID uint32, sourceSlot uint8,
	targetObjectID uint32, targetSlot uint8,
) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.effectLease = &campaignNPCDrainEffectLease{
		pool: pool, sourceObjectID: sourceObjectID, sourceSlot: sourceSlot,
		targetObjectID: targetObjectID, targetSlot: targetSlot,
	}
}

func (e *campaignNPCDrainRun) BindTargetEffect(
	pool *attachedEffectPool, sourceObjectID uint32,
	targetObjectID uint32, targetSlot uint8,
) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.effectLease = &campaignNPCDrainEffectLease{
		pool: pool, sourceObjectID: sourceObjectID,
		targetObjectID: targetObjectID, targetSlot: targetSlot,
	}
}

func (e *campaignNPCDrainRun) ReleaseEffects() (campaignNPCDrainEffectLease, bool) {
	if e == nil {
		return campaignNPCDrainEffectLease{}, false
	}
	e.mu.Lock()
	lease := e.effectLease
	e.effectLease = nil
	e.mu.Unlock()
	if lease == nil || lease.pool == nil {
		return campaignNPCDrainEffectLease{}, false
	}
	// The lease records presentation already sent to the client. Another cleanup
	// path can clear the shared slot pool first, but the client still needs the
	// matching removal for this lease.
	if lease.sourceObjectID != 0 {
		isSourceSlotReleased := lease.pool.Release(
			lease.sourceObjectID, lease.sourceSlot,
		)
		if !isSourceSlotReleased {
			// A prior actor cleanup already cleared the shared source slot.
		}
	}
	isTargetSlotReleased := lease.pool.Release(
		lease.targetObjectID, lease.targetSlot,
	)
	if !isTargetSlotReleased {
		// A prior actor cleanup already cleared the shared target slot.
	}
	return *lease, true
}

func (e *campaignNPCDrainRun) targets(objectID uint32) bool {
	if e == nil || objectID == 0 {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.effectLease != nil && e.effectLease.targetObjectID == objectID
}

func (s *gameplayPeerSession) stopCampaignNPCDrainsTargeting(
	targetObjectID uint32,
) ([][]byte, error) {
	if s == nil || targetObjectID == 0 {
		return nil, nil
	}
	packets := make([][]byte, 0)
	for sourceObjectID, run := range s.campaignNPCDrainRuns {
		if !run.targets(targetObjectID) {
			continue
		}
		delete(s.campaignNPCDrainRuns, sourceObjectID)
		run.End()
		lease, isLeaseFound := run.ReleaseEffects()
		if !isLeaseFound {
			continue
		}
		removalPackets, err := zoneabilityraknet.ChannelDrainTargetEffectRemoval(
			lease.targetObjectID, lease.targetSlot,
		)
		if err != nil {
			return nil, fmt.Errorf("enemyDrainTargetEffect: %w", err)
		}
		packets = append(packets, removalPackets...)
	}
	return packets, nil
}

type campaignNPCDrainRequest struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
}

func (e campaignNPCDrainRequest) resume(timestamp uint64) ([][]byte, error) {
	return e.runtime.produceHealthDrain(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
}

type campaignNPCDrainSchedule struct {
	request campaignNPCDrainRequest
	plan    zonenpc.AttackPlan
	run     *campaignNPCDrainRun
}

type campaignNPCDrainTickStep struct {
	schedule campaignNPCDrainSchedule
	deadline time.Duration
	isLast   bool
}

func (e campaignNPCDrainTickStep) produce() ([][]byte, error) {
	return e.schedule.tick(e.deadline, e.isLast)
}

func (e campaignNPCDrainSchedule) endPacket(
	deadline time.Duration,
) ([][]byte, error) {
	isFirstEnd := e.run.End()
	e.request.runtime.untrackHealthDrain(
		e.request.sessionKey, e.request.generation,
		e.request.objectID, e.run,
	)
	packets, err := e.effectRemovals()
	if err != nil {
		return nil, fmt.Errorf("enemyDrainEndEffect: %w", err)
	}
	if !isFirstEnd {
		return packets, nil
	}
	packet, err := npcraknet.AnimationState(
		e.plan.SourceObjectID, e.plan.Profile.EndAnimationName,
		e.request.timestamp+uint64(deadline/time.Millisecond),
	)
	if err != nil {
		return nil, fmt.Errorf("enemyDrainEnd: %w", err)
	}
	return append(packets, packet), nil
}

func (e campaignNPCDrainSchedule) effectRemovals() ([][]byte, error) {
	lease, isLeaseFound := e.run.ReleaseEffects()
	if !isLeaseFound {
		return nil, nil
	}
	packets, err := zoneabilityraknet.ChannelDrainTargetEffectRemoval(
		lease.targetObjectID, lease.targetSlot,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyDrainEffectRemoval: %w", err)
	}
	return packets, nil
}

func (e campaignNPCDrainSchedule) tick(
	deadline time.Duration, isLast bool,
) ([][]byte, error) {
	if e.run.IsEnded() {
		return nil, nil
	}
	req := e.request
	runtime := req.runtime
	runtime.registry.mutex.Lock()
	current, isFound := runtime.registry.sessions[req.sessionKey]
	isCurrent := isFound && current.isCampaignNPCAttackActiveAt(
		req.generation, req.objectID, e.plan.TargetObjectID, runtime.now(),
	)
	if !isCurrent {
		runtime.registry.mutex.Unlock()
		return e.endPacket(deadline)
	}
	currentNPC, isNPCFound := current.zone.NPCs().NPC(req.objectID)
	currentTarget, isTargetFound := current.campaignNPCTarget(
		req.generation, e.plan.TargetObjectID,
	)
	if !isNPCFound || !isTargetFound {
		runtime.registry.mutex.Unlock()
		return e.endPacket(deadline)
	}
	currentPlan, err := zonenpc.PlanAttackWithProfile(
		currentNPC, currentTarget.ObjectID, currentTarget.Position,
		e.plan.Profile, currentTarget.FootprintRadius,
	)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return e.endPacket(deadline)
	}
	if current.zone.NPCRandom() == nil {
		runtime.registry.mutex.Unlock()
		return nil, errors.New("enemy drain random unavailable")
	}
	result, err := zonenpc.CommitAttack(
		current.zone.NPCRandom(), currentPlan,
		currentNPC.Plan.NPCProfile.CriticalRating, runtime.program.Critical,
	)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyDrainCommit: %w", err)
	}
	packets, statDelta, reflection, isApplied, err := runtime.applyEnemyAttackDamage(
		&current, req.generation, currentPlan, result,
		req.timestamp+uint64(deadline/time.Millisecond),
	)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyDrainDamage: %w", err)
	}
	remainingHitPoint := currentTarget.HitPoint
	updatedTarget, isUpdatedTargetFound := current.campaignNPCTarget(
		req.generation, currentTarget.ObjectID,
	)
	if isUpdatedTargetFound {
		remainingHitPoint = updatedTarget.HitPoint
	} else if isApplied {
		remainingHitPoint = 0
	}
	appliedDamage := max(float32(0), currentTarget.HitPoint-remainingHitPoint)
	// A damage reaction can kill the draining enemy before its healing stage.
	healingSource, isHealingSourceFound := current.zone.NPCs().NPC(req.objectID)
	if appliedDamage > 0 && isHealingSourceFound &&
		!healingSource.IsDefeated && healingSource.HitPoint > 0 {
		healedNPC, healedAmount, healErr := current.zone.NPCs().Heal(
			req.objectID, appliedDamage*e.plan.Profile.LifeSteal,
		)
		if healErr != nil {
			runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemyDrainHealing: %w", healErr)
		}
		if healedAmount > 0 {
			objectiveErr := current.zone.RecordNPCHeal(
				current.zone.Context(), req.objectID, req.objectID,
				healedAmount,
			)
			if objectiveErr != nil {
				runtime.logger.Printf(
					"RakNet campaign drain-heal objective omitted source=%d: %v",
					req.objectID, objectiveErr,
				)
			}
			healingPackets, marshalErr := zoneabilityraknet.ChannelDrainHealing(
				req.objectID, healedNPC.HitPoint, healedAmount,
			)
			if marshalErr != nil {
				runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("enemyDrainHealingMarshal: %w", marshalErr)
			}
			packets = append(packets, healingPackets...)
		}
	}
	animationName := e.plan.Profile.LoopAnimationName
	if isLast {
		animationName = e.plan.Profile.EndAnimationName
		e.run.End()
		if current.campaignNPCDrainRuns[req.objectID] == e.run {
			delete(current.campaignNPCDrainRuns, req.objectID)
		}
		removalPackets, removalErr := e.effectRemovals()
		if removalErr != nil {
			runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemyDrainFinalEffect: %w", removalErr)
		}
		packets = append(packets, removalPackets...)
	}
	animationPacket, err := npcraknet.AnimationState(
		req.objectID, animationName,
		req.timestamp+uint64(deadline/time.Millisecond),
	)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyDrainAnimation: %w", err)
	}
	packets = append(packets, animationPacket)
	binding := current.binding
	runtime.registry.sessions[req.sessionKey] = current
	runtime.registry.mutex.Unlock()
	reflectionPackets, reflectionErr := runtime.publishThornBarkReflection(
		req.packet, req.sessionKey, req.generation,
		req.timestamp+uint64(deadline/time.Millisecond), binding, reflection,
	)
	if reflectionErr != nil {
		return nil, fmt.Errorf("enemyDrainThornBark: %w", reflectionErr)
	}
	packets = append(packets, reflectionPackets...)
	if isApplied {
		err = runtime.stats.Record(context.Background(), binding, statDelta)
		if err != nil {
			return nil, fmt.Errorf("enemyDrainStats: %w", err)
		}
	}
	return packets, nil
}

func (e campaignNPCDrainSchedule) next() ([][]byte, error) {
	profile := e.plan.Profile
	finalTickDeadline := profile.HitDelay +
		time.Duration(profile.NumberOfTicks-1)*profile.TickDuration
	timestamp := e.request.timestamp +
		uint64((finalTickDeadline+profile.EndAnimationDelay)/time.Millisecond)
	return e.request.resume(timestamp)
}

func (r campaignNPCActionRuntime) produceHealthDrain(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	request := campaignNPCDrainRequest{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
	}
	isDeferred, err := r.pursuit.deferAction(
		packet, sessionKey, generation, objectID, timestamp, request.resume,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyDrainStun: %w", err)
	}
	if isDeferred {
		return nil, nil
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound &&
		peerSession.isCampaignNPCSourceActive(generation, objectID) &&
		peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, enemy.TargetObjectID,
	)
	r.registry.mutex.RUnlock()
	if !isEnemyFound || enemy.IsDefeated || !isTargetFound {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	profile, isProfileFound := zonenpc.ActionProfileForPlan(enemy.Plan)
	if !isProfileFound || profile.Family != zonenpc.ActionChannelDrain ||
		profile.NumberOfTicks == 0 || profile.TickDuration <= 0 ||
		profile.EndAnimationDelay <= 0 || profile.LifeSteal <= 0 ||
		profile.LoopAnimationName == "" || profile.EndAnimationName == "" ||
		profile.TargetEffectName == "" {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, errors.New("enemy drain profile unavailable")
	}
	plan, err := zonenpc.PlanAttackWithProfile(
		enemy, target.ObjectID, target.Position, profile,
		target.FootprintRadius,
	)
	if err != nil {
		action, actionErr := campaignNPCActionWithProfile(
			enemy.Plan, target.ObjectID, target.Position,
			profile, target.FootprintRadius,
		)
		if actionErr != nil {
			return nil, fmt.Errorf("enemyDrainPursuitAction: %w", actionErr)
		}
		if !action.IsPursuitNeeded {
			r.releaseAction(sessionKey, generation, objectID)
			return nil, nil
		}
		pursuitPackets, marshalErr := npcraknet.Pursuit(action)
		if marshalErr != nil {
			return nil, fmt.Errorf("enemyDrainPursuitMarshal: %w", marshalErr)
		}
		scheduleErr := r.pursuit.schedule(
			packet, sessionKey, generation, objectID, timestamp,
			action.TargetPosition, profile, request.resume,
		)
		if scheduleErr != nil {
			r.releaseAction(sessionKey, generation, objectID)
			r.logger.Printf(
				"RakNet campaign health-drain pursuit not scheduled object=%d: %v",
				objectID, scheduleErr,
			)
		}
		return pursuitPackets, nil
	}
	startPackets, err := r.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		return nil, fmt.Errorf("enemyDrainStart: %w", err)
	}
	if r.effectPool == nil {
		return nil, errors.New("enemy drain effect pool unavailable")
	}
	targetSlot, isTargetAllocated := r.effectPool.Allocate(target.ObjectID)
	if !isTargetAllocated {
		return nil, errors.New("enemy drain target effect slot unavailable")
	}
	effectPackets, err := zoneabilityraknet.ChannelDrainTargetEffect(
		profile.TargetEffectName, objectID, target.ObjectID, targetSlot,
	)
	if err != nil {
		r.effectPool.Release(target.ObjectID, targetSlot)
		return nil, fmt.Errorf("enemyDrainEffect: %w", err)
	}
	run := &campaignNPCDrainRun{}
	run.BindTargetEffect(r.effectPool, objectID, target.ObjectID, targetSlot)
	if !r.trackHealthDrain(sessionKey, generation, objectID, run) {
		run.End()
		run.ReleaseEffects()
		r.releaseAction(sessionKey, generation, objectID)
		return nil, errors.New("enemy drain run unavailable")
	}
	schedule := campaignNPCDrainSchedule{
		request: request, plan: plan, run: run,
	}
	producers := make([]raknet.ScheduledPacketProducer, 0, profile.NumberOfTicks+1)
	for tick := uint32(0); tick < profile.NumberOfTicks; tick++ {
		deadline := profile.HitDelay + time.Duration(tick)*profile.TickDuration
		step := campaignNPCDrainTickStep{
			schedule: schedule, deadline: deadline,
			isLast: tick+1 == profile.NumberOfTicks,
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: deadline, Produce: step.produce,
		})
	}
	finalTickDeadline := profile.HitDelay +
		time.Duration(profile.NumberOfTicks-1)*profile.TickDuration
	producers = append(producers, raknet.ScheduledPacketProducer{
		Delay:   finalTickDeadline + profile.EndAnimationDelay,
		Produce: schedule.next,
	})
	cancel, scheduleErr := scheduleNPCProducers(r.registry, packet, producers)
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		run.End()
		run.ReleaseEffects()
		r.untrackHealthDrain(sessionKey, generation, objectID, run)
		r.releaseAction(sessionKey, generation, objectID)
		return nil, fmt.Errorf("enemyDrainSchedule: %w", scheduleErr)
	}
	return append(startPackets, effectPackets...), nil
}

func (r campaignNPCActionRuntime) trackHealthDrain(
	sessionKey string, generation uint64, objectID uint32,
	run *campaignNPCDrainRun,
) bool {
	if sessionKey == "" || generation == 0 || objectID == 0 || run == nil {
		return false
	}
	r.registry.mutex.Lock()
	defer r.registry.mutex.Unlock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation {
		return false
	}
	if peerSession.campaignNPCDrainRuns == nil {
		peerSession.campaignNPCDrainRuns = make(map[uint32]*campaignNPCDrainRun)
	}
	if peerSession.campaignNPCDrainRuns[objectID] != nil {
		return false
	}
	peerSession.campaignNPCDrainRuns[objectID] = run
	r.registry.sessions[sessionKey] = peerSession
	return true
}

func (r campaignNPCActionRuntime) untrackHealthDrain(
	sessionKey string, generation uint64, objectID uint32,
	run *campaignNPCDrainRun,
) {
	if sessionKey == "" || generation == 0 || objectID == 0 || run == nil {
		return
	}
	r.registry.mutex.Lock()
	defer r.registry.mutex.Unlock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation ||
		peerSession.campaignNPCDrainRuns[objectID] != run {
		return
	}
	delete(peerSession.campaignNPCDrainRuns, objectID)
	r.registry.sessions[sessionKey] = peerSession
}

func (r campaignNPCActionRuntime) stopHealthDrain(
	sessionKey string, generation uint64, objectID uint32,
) ([][]byte, error) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	run := peerSession.campaignNPCDrainRuns[objectID]
	delete(peerSession.campaignNPCDrainRuns, objectID)
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	if run == nil {
		return nil, nil
	}
	run.End()
	lease, isLeaseFound := run.ReleaseEffects()
	if !isLeaseFound {
		return nil, nil
	}
	packets, err := zoneabilityraknet.ChannelDrainTargetEffectRemoval(
		lease.targetObjectID, lease.targetSlot,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyDrainStopEffect: %w", err)
	}
	return packets, nil
}
