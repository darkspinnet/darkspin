package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const operativeChannelStep = 250 * time.Millisecond

// The registry lock owns this state, shared by the source action and its
// target's modifier. Damage during the initial windup belongs to this cast.
type operativeCageState struct {
	plan               zonenpc.AttackPlan
	startedAt          time.Time
	elapsed            time.Duration
	damageElapsed      time.Duration
	totalDamage        float64
	criticalRating     float32
	isStartedAboveHalf bool
	isComplete         bool
	isRetryScheduled   bool
	run                *campaignNPCModifierRun
	cancel             raknet.CancelSchedule
	scheduleGeneration uint64
	retirementPackets  [][]byte
}

type operativeCageSchedule struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	timestamp  uint64
	state      *operativeCageState
}

func (e campaignNPCActionRuntime) produceOperativeCage(
	packet raknet.Packet, sessionKey string, generation uint64,
	plan zonenpc.AttackPlan, timestamp uint64,
) ([][]byte, error) {
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || !member.isCampaignNPCSourceGenerationActive(
		generation, plan.SourceObjectID, plan.ActionGeneration,
	) {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	var cleanupPackets [][]byte
	previous := member.operativeChannels[plan.SourceObjectID]
	if previous != nil {
		if previous.plan.ActionGeneration == plan.ActionGeneration {
			e.registry.mutex.Unlock()
			return nil, nil
		}
		var err error
		cleanupPackets, err = e.registry.retireOperativeCageLocked(previous.run, e.modifierPool)
		if err != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("cagePreviousRetire: %w", err)
		}
		previous.isComplete = true
		previous.cancelSchedule()
		cleanupPackets = append(previous.retirementPackets, cleanupPackets...)
		previous.retirementPackets = nil
		member = e.registry.sessions[sessionKey]
		delete(member.operativeChannels, plan.SourceObjectID)
	}
	enemy, isEnemyFound := member.zone.NPCs().NPC(plan.SourceObjectID)
	if !isEnemyFound {
		for sessionKey, observer := range e.registry.sessions {
			if observer.zone != member.zone || observer.isRejoinPending || !observer.stage.IsDungeon() {
				continue
			}
			observer.queuePackets(cleanupPackets)
			e.registry.sessions[sessionKey] = observer
		}
		e.registry.mutex.Unlock()
		return nil, nil
	}
	state := &operativeCageState{
		plan: plan, startedAt: e.now(),
		criticalRating:     enemy.EffectiveCriticalRating(),
		isStartedAboveHalf: enemy.HitPoint*2 > enemy.Plan.NPCProfile.HitPoint,
		retirementPackets:  cleanupPackets,
	}
	if member.operativeChannels == nil {
		member.operativeChannels = make(map[uint32]*operativeCageState)
	}
	member.operativeChannels[plan.SourceObjectID] = state
	e.registry.sessions[sessionKey] = member
	e.registry.mutex.Unlock()
	schedule := operativeCageSchedule{
		runtime: e, packet: packet, sessionKey: sessionKey,
		generation: generation, timestamp: timestamp, state: state,
	}
	err := schedule.admit(operativeCageStart, 0)
	if err != nil {
		return schedule.fail("cageStartSchedule", err, nil)
	}
	return nil, nil
}

// RakNet discards a producer's packets on error. Preserve committed cleanup in
// each member's pending queue before reporting a failed continuation.
func (e operativeCageSchedule) fail(step string, cause error, packets [][]byte) ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	source, isSourceFound := e.runtime.registry.sessions[e.sessionKey]
	sourceZone := source.zone
	if !isSourceFound || source.generation != e.generation {
		sourceZone = nil
	}
	if sourceZone == nil && e.state.run != nil {
		for _, target := range e.runtime.registry.sessions {
			if target.operativeCage == e.state.run {
				sourceZone = target.zone
				break
			}
		}
	}
	e.runtime.registry.mutex.RUnlock()
	cleanupPackets, cleanupErr := e.runtime.abortOperativeCage(e.sessionKey, e.generation, e.state)
	packets = append(packets, cleanupPackets...)
	if sourceZone != nil && len(packets) > 0 {
		e.runtime.registry.mutex.Lock()
		for sessionKey, observer := range e.runtime.registry.sessions {
			if observer.zone != sourceZone || observer.isRejoinPending || !observer.stage.IsDungeon() {
				continue
			}
			observer.queuePackets(packets)
			e.runtime.registry.sessions[sessionKey] = observer
		}
		e.runtime.registry.mutex.Unlock()
	}
	return nil, fmt.Errorf("%s: %w", step, errors.Join(cause, cleanupErr))
}

func (e campaignNPCActionRuntime) abortOperativeCage(
	sessionKey string, generation uint64, state *operativeCageState,
) ([][]byte, error) {
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	isActionReleaseAllowed := isFound && member.generation == generation &&
		(member.operativeChannels[state.plan.SourceObjectID] == state ||
			member.operativeChannels[state.plan.SourceObjectID] == nil)
	packets, err := e.registry.retireOperativeCageLocked(state.run, e.modifierPool)
	packets = append(state.retirementPackets, packets...)
	state.retirementPackets = nil
	state.isComplete = true
	state.cancelSchedule()
	if isFound && member.generation == generation &&
		member.operativeChannels[state.plan.SourceObjectID] == state {
		// Retirement may have updated this member's modifier references.
		member = e.registry.sessions[sessionKey]
		delete(member.operativeChannels, state.plan.SourceObjectID)
		e.registry.sessions[sessionKey] = member
	}
	e.registry.mutex.Unlock()
	if isActionReleaseAllowed {
		isReleased := e.releaseActionGeneration(sessionKey, generation,
			state.plan.SourceObjectID, state.plan.ActionGeneration)
		if !isReleased {
			// A replaced driver/action must never release its successor's lease.
		}
	}
	if err != nil {
		return packets, fmt.Errorf("cageAbortRetire: %w", err)
	}
	return packets, nil
}

// The InstantCast template deals its initial hit before OnDamageTarget starts
// the cage. Reuse that accepted damage/modifier path exactly once.
func (e operativeCageSchedule) begin(producer *operativeCageProducer) ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	member, isCurrent := e.currentLocked()
	isCurrent = isCurrent && !e.state.isComplete
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return e.finish(producer)
	}
	attack := campaignNPCAttackSchedule{
		request: campaignNPCAttackRequest{
			runtime: e.runtime, packet: e.packet, sessionKey: e.sessionKey,
			generation: e.generation, objectID: e.state.plan.SourceObjectID,
			timestamp: e.timestamp + uint64(e.state.plan.Profile.HitDelay/time.Millisecond),
			kind:      campaignNPCAttackMelee,
		},
		plan: e.state.plan,
	}
	packets, err := attack.hit()
	if err != nil {
		return e.fail("cageInitialHit", err, packets)
	}
	e.runtime.registry.mutex.RLock()
	member, isCurrent = e.currentLocked()
	isContinuing := isCurrent && e.state.run != nil &&
		e.state.run.isActive() && e.state.totalDamage < 5000
	if isContinuing {
		target, isTargetFound := member.campaignNPCTarget(e.generation, e.state.plan.TargetObjectID)
		isContinuing = isTargetFound && target.HitPoint > 0
	}
	e.runtime.registry.mutex.RUnlock()
	if !isContinuing {
		endPackets, endErr := e.finish(producer)
		if endErr != nil {
			return e.fail("cageInitialEnd", endErr, packets)
		}
		return append(packets, endPackets...), nil
	}
	producer.nextStep = operativeCageTick
	producer.nextDelay = operativeChannelStep
	return packets, nil
}

func (e operativeCageSchedule) currentLocked() (gameplayPeerSession, bool) {
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	return member, isFound && member.operativeChannels[e.state.plan.SourceObjectID] == e.state &&
		member.isCampaignNPCSourceGenerationActive(
			e.generation, e.state.plan.SourceObjectID, e.state.plan.ActionGeneration,
		)
}

func (e operativeCageSchedule) tick(producer *operativeCageProducer) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	member, isCurrent := e.currentLocked()
	isContinuing := isCurrent && !e.state.isComplete &&
		e.runtime.registry.isOperativeChannelLocked(
			member, e.state.plan.SourceObjectID, e.state.plan.TargetObjectID,
		)
	if !isContinuing {
		e.runtime.registry.mutex.Unlock()
		return e.finish(producer)
	}
	e.state.elapsed += operativeChannelStep
	e.state.damageElapsed += operativeChannelStep
	isPulse := e.state.plan.Profile.AbilityName == "NomadCyberOne_Cage" &&
		e.state.damageElapsed >= e.state.plan.Profile.TickDuration
	// Chunk 734 jumps to its timer reset even when the threshold is unmet.
	// Preserve that shipped branch: only the template's initial hit damages.
	if isPulse || e.state.plan.Profile.AbilityName == "NomadSpacetimeAgent_Cage" {
		e.state.damageElapsed = 0
	}
	e.runtime.registry.mutex.Unlock()
	var packets [][]byte
	if isPulse {
		var err error
		packets, err = e.damage()
		if err != nil {
			return e.fail("cagePulse", err, packets)
		}
	}
	e.runtime.registry.mutex.RLock()
	member, isCurrent = e.currentLocked()
	isContinuing = isCurrent && !e.state.isComplete &&
		e.state.totalDamage < 5000 && e.state.elapsed < e.state.plan.Profile.ModifierDuration
	if isContinuing && e.state.plan.Profile.AbilityName == "NomadSpacetimeAgent_Cage" &&
		e.state.isStartedAboveHalf {
		enemy, isEnemyFound := member.zone.NPCs().NPC(e.state.plan.SourceObjectID)
		isContinuing = isEnemyFound && enemy.HitPoint*2 >= enemy.Plan.NPCProfile.HitPoint
	}
	e.runtime.registry.mutex.RUnlock()
	if !isContinuing {
		endPackets, err := e.finish(producer)
		if err != nil {
			return e.fail("cageTickEnd", err, packets)
		}
		return append(packets, endPackets...), nil
	}
	producer.nextStep = operativeCageTick
	producer.nextDelay = operativeChannelStep
	return packets, nil
}

func (e operativeCageSchedule) damage() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	member, isCurrent := e.currentLocked()
	if !isCurrent || e.state.isComplete || !e.runtime.registry.isOperativeChannelLocked(
		member, e.state.plan.SourceObjectID, e.state.plan.TargetObjectID,
	) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	enemy, isEnemyFound := member.zone.NPCs().NPC(e.state.plan.SourceObjectID)
	target, isTargetFound := member.campaignNPCTarget(e.generation, e.state.plan.TargetObjectID)
	if !isEnemyFound || !isTargetFound {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	// The channel captured its source attribute snapshot at activation. Keep
	// its resolved damage and critical rating; only world positions are live.
	plan := e.state.plan
	plan.SourcePosition = enemy.Plan.Position
	plan.TargetPosition = target.Position
	result, err := zonenpc.CommitAttack(member.zone.NPCRandom(), plan,
		e.state.criticalRating, e.runtime.program.Critical)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("cagePulseCommit: %w", err)
	}
	timestamp := e.timestamp + uint64((e.state.plan.Profile.HitDelay+e.state.elapsed)/time.Millisecond)
	packets, statDelta, reflection, isApplied, err := e.runtime.applyEnemyAttackDamage(
		&member, e.generation, plan, result, timestamp,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("cagePulseDamage: %w", err)
	}
	// The shared damage owner handles defenses, co-op target ownership and
	// passive reactions. A blocked pulse still returns its combat feedback.
	if !isApplied {
		e.runtime.registry.mutex.Unlock()
		return packets, nil
	}
	e.runtime.registry.sessions[e.sessionKey] = member
	binding := member.binding
	e.runtime.registry.mutex.Unlock()
	reflectionPackets, err := e.runtime.publishThornBarkReflection(
		e.packet, e.sessionKey, e.generation, timestamp, binding, reflection,
	)
	if err != nil {
		return packets, fmt.Errorf("cagePulseReflection: %w", err)
	}
	err = e.runtime.stats.Record(member.zone.Context(), binding, statDelta)
	if err != nil {
		return append(packets, reflectionPackets...), fmt.Errorf("cagePulseStats: %w", err)
	}
	return append(packets, reflectionPackets...), nil
}

func (e operativeCageSchedule) finish(producer *operativeCageProducer) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	member, isCurrent := e.currentLocked()
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return e.runtime.abortOperativeCage(e.sessionKey, e.generation, e.state)
	}
	if e.state.isRetryScheduled {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	packets, err := e.runtime.registry.retireOperativeCageLocked(e.state.run, e.runtime.modifierPool)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return e.fail("cageEndModifier", err, nil)
	}
	e.state.isComplete = true
	e.state.isRetryScheduled = true
	e.runtime.registry.mutex.Unlock()
	endPacket, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
		ObjectID: e.state.plan.SourceObjectID, State: util.HashID(e.state.plan.Profile.EndAnimationName),
		Timestamp: e.timestamp + uint64(max(e.runtime.now().Sub(e.state.startedAt), 0)/time.Millisecond), Scale: 1,
	})
	if err != nil {
		return e.fail("cageEndAnimation", err, packets)
	}
	packets = append(packets, endPacket)
	if member.isZoneTerminal() {
		return packets, nil
	}
	producer.nextStep = operativeCageRetry
	producer.nextDelay = e.state.plan.Profile.Cooldown
	return packets, nil
}

func (e operativeCageSchedule) retry() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	member, isCurrent := e.currentLocked()
	if !isCurrent || !e.state.isComplete {
		e.runtime.registry.mutex.Unlock()
		return e.runtime.abortOperativeCage(e.sessionKey, e.generation, e.state)
	}
	delete(member.operativeChannels, e.state.plan.SourceObjectID)
	e.runtime.registry.sessions[e.sessionKey] = member
	e.runtime.registry.mutex.Unlock()
	timestamp := e.timestamp + uint64(max(e.runtime.now().Sub(e.state.startedAt), 0)/time.Millisecond)
	packets, err := e.runtime.produceEnemyMelee(e.packet, e.sessionKey,
		e.generation, e.state.plan.SourceObjectID, timestamp)
	if err != nil {
		return e.fail("cageRetry", err, nil)
	}
	return packets, nil
}

// This consumes one accepted source damage event, before observer fanout.
// The damage-event amount remains the existing server semantic boundary.
func (e gameplayProjectionRuntime) recordOperativeCageDamage(
	sessionKey string, generation uint64, damage zonenpc.DamageEvent,
) {
	if damage.IsDamageImmune || damage.Damage <= 0 {
		return
	}
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	source, isFound := e.registry.sessions[sessionKey]
	if !isFound || source.generation != generation || source.zone == nil {
		return
	}
	for _, member := range e.registry.sessions {
		if member.zone != source.zone {
			continue
		}
		state := member.operativeChannels[damage.TargetObjectID]
		if state != nil && !state.isComplete && member.isCampaignNPCSourceGenerationActive(
			member.generation, damage.TargetObjectID, state.plan.ActionGeneration,
		) {
			state.totalDamage += float64(damage.Damage)
		}
	}
}
