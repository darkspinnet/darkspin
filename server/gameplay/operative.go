package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e gameplayPeerSession) isOperativeCaged(at time.Time) bool {
	run := e.operativeCage
	if run == nil || !run.isActive() || e.zone == nil ||
		(run.operativeCage != nil && run.operativeCage.isComplete) ||
		run.record.TargetObjectID != e.deployedObjectID || e.deployedHitPoint() <= 0 ||
		e.isHeroDebuffImmune(e.deployedObjectID) || e.isOverdriveActiveAt(at) {
		return false
	}
	sourceID := run.record.SourceObjectID
	enemy, isFound := e.zone.NPCs().NPC(sourceID)
	return isFound && !enemy.IsDefeated && enemy.HitPoint > 0 &&
		enemy.IsActionStarted &&
		(run.operativeCage == nil || enemy.ActionGeneration == run.operativeCage.plan.ActionGeneration) &&
		enemy.TargetObjectID == e.deployedObjectID &&
		e.zone.NPCs().StunRemaining(sourceID, at) == 0 &&
		e.zone.NPCs().SleepRemaining(sourceID, at) == 0 &&
		e.zone.NPCs().FearRemaining(sourceID, at) == 0 &&
		e.zone.NPCs().SilenceRemaining(sourceID, at) == 0
}

func (e gameplayPeerSession) releaseOperativeCage(pool *modifierPool) error {
	run := e.operativeCage
	if run == nil {
		return nil
	}
	isCreated, err := run.release(pool)
	if err != nil {
		return fmt.Errorf("cageRelease: %w", err)
	}
	if isCreated && run.cancel != nil {
		run.cancel()
	}
	if e.zone != nil {
		e.zone.Effect().Remove(run.instanceID)
	}
	return nil
}

// The registry lock protects the rescue check and the target's cage ownership.
// Never trap the last free, living player, including after an ally disconnects.
func (e *gameplaySessionRegistry) hasOperativeRescuerLocked(target gameplayPeerSession) bool {
	for _, ally := range e.sessions {
		if ally.zone == target.zone && ally.binding.UserID != target.binding.UserID &&
			ally.stage.IsDungeon() && !ally.isRejoinPending && ally.deployedHitPoint() > 0 &&
			!ally.isOperativeCaged(e.now()) {
			return true
		}
	}
	return false
}

func (e campaignNPCActionRuntime) canApplyOperativeCage(
	sessionKey string, generation uint64, plan zonenpc.AttackPlan,
) bool {
	e.registry.mutex.RLock()
	defer e.registry.mutex.RUnlock()
	source, isFound := e.registry.sessions[sessionKey]
	if !isFound || source.generation != generation || source.zone == nil ||
		source.zone.NPCs().SilenceRemaining(plan.SourceObjectID, e.now()) > 0 {
		return false
	}
	for _, target := range e.registry.sessions {
		if target.zone == source.zone && target.deployedObjectID == plan.TargetObjectID &&
			!target.isRejoinPending && target.deployedHitPoint() > 0 && target.operativeCage == nil {
			return e.registry.hasOperativeRescuerLocked(target)
		}
	}
	return false
}

func (e campaignNPCActionRuntime) bindOperativeCage(
	source gameplayPeerSession, run *campaignNPCModifierRun,
) ([][]byte, bool, error) {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	state := source.operativeChannels[run.record.SourceObjectID]
	if state == nil || state.isComplete || state.run != nil {
		return nil, false, nil
	}
	enemy, isEnemyFound := source.zone.NPCs().NPC(run.record.SourceObjectID)
	if !isEnemyFound || enemy.ActionGeneration != state.plan.ActionGeneration ||
		enemy.TargetObjectID != state.plan.TargetObjectID {
		return nil, false, nil
	}
	for sessionKey, target := range e.registry.sessions {
		if target.zone != source.zone || target.deployedObjectID != run.record.TargetObjectID ||
			target.operativeCage != nil || target.isRejoinPending || target.deployedHitPoint() <= 0 ||
			!e.registry.hasOperativeRescuerLocked(target) {
			continue
		}
		packets, err := stopEnemyControlledHeroMovement(&target, e.now())
		if err != nil {
			return nil, false, fmt.Errorf("cageStop: %w", err)
		}
		target.basicSequenceSession().ReleaseHeld()
		target.campaignPlayerPursuitSession().Cancel()
		target.followTargetUserID = 0
		run.operativeCage = state
		state.run = run
		target.operativeCage = run
		e.registry.sessions[sessionKey] = target
		return packets, true, nil
	}
	return nil, false, nil
}

func (e *gameplaySessionRegistry) isOperativeChannelLocked(
	source gameplayPeerSession, sourceObjectID uint32, targetObjectID uint32,
) bool {
	for _, target := range e.sessions {
		if target.zone == source.zone && target.deployedObjectID == targetObjectID &&
			target.operativeCage != nil &&
			target.operativeCage.record.SourceObjectID == sourceObjectID &&
			target.isOperativeCaged(e.now()) && e.hasOperativeRescuerLocked(target) {
			return true
		}
	}
	return false
}

func (e gameplayPendingRuntime) pollOperativeCages(packet raknet.Packet) error {
	e.registry.mutex.Lock()
	target, isFound := e.registry.sessions[packet.Address.String()]
	run := target.operativeCage
	if !isFound || run == nil ||
		(target.isOperativeCaged(e.now()) && e.registry.hasOperativeRescuerLocked(target)) {
		e.registry.mutex.Unlock()
		return nil
	}
	packets, err := e.registry.retireOperativeCageLocked(run, e.modifierPool)
	if err != nil {
		e.registry.mutex.Unlock()
		return fmt.Errorf("cagePollRetire: %w", err)
	}
	target = e.registry.sessions[packet.Address.String()]
	target.queuePackets(packets)
	e.registry.sessions[packet.Address.String()] = target
	identity := gameplayProducerIdentityFromSession(packet.Address.String(), target, true)
	e.registry.mutex.Unlock()
	e.registry.queuePeerPresentation(identity, packets)
	return nil
}

// Called once at the modifier's target boundary, never once per observer.
// Clear every shared reference before another poll can publish a second delete.
func (e *gameplaySessionRegistry) retireOperativeCageLocked(
	run *campaignNPCModifierRun, pool *modifierPool,
) ([][]byte, error) {
	if run == nil {
		return nil, nil
	}
	isTracked := false
	for _, member := range e.sessions {
		if member.operativeCage == run || member.campaignNPCModifiers[run.instanceID] == run {
			isTracked = true
			break
		}
	}
	if !isTracked {
		return nil, nil
	}
	deletedPacket, err := effectraknet.ModifierDelete(run.record.TargetObjectID, run.instanceID)
	if err != nil {
		return nil, fmt.Errorf("cageRetireDelete: %w", err)
	}
	isCreated, err := run.release(pool)
	if err != nil {
		return nil, fmt.Errorf("cageRetireRelease: %w", err)
	}
	if isCreated && run.cancel != nil {
		run.cancel()
	}
	if run.operativeCage != nil {
		run.operativeCage.isComplete = true
	}
	for sessionKey, member := range e.sessions {
		if member.operativeCage == run {
			member.operativeCage = nil
		}
		if member.campaignNPCModifiers[run.instanceID] == run && member.zone != nil {
			member.zone.Effect().Remove(run.instanceID)
		}
		member.untrackCampaignNPCModifier(run)
		e.sessions[sessionKey] = member
	}
	return [][]byte{deletedPacket}, nil
}

// Retire before a transport reset or member removal discards its action maps.
// Queue deletes directly to each remaining zone member because the departing
// connection's guarded scheduler can no longer commit a peer publication.
func (e *gameplaySessionRegistry) retireOperativeChannelsLocked(
	member *gameplayPeerSession, pool *modifierPool,
) error {
	if member == nil {
		return nil
	}
	runs := make(map[uint32]*campaignNPCModifierRun)
	var packets [][]byte
	if member.operativeCage != nil {
		runs[member.operativeCage.instanceID] = member.operativeCage
	}
	for _, state := range member.operativeChannels {
		state.isComplete = true
		state.cancelSchedule()
		packets = append(packets, state.retirementPackets...)
		state.retirementPackets = nil
		if state.run != nil {
			runs[state.run.instanceID] = state.run
		}
	}
	var retireErr error
	for _, run := range runs {
		deletePackets, err := e.retireOperativeCageLocked(run, pool)
		if err != nil {
			retireErr = errors.Join(retireErr, err)
			continue
		}
		packets = append(packets, deletePackets...)
		member.untrackCampaignNPCModifier(run)
		if member.operativeCage == run {
			member.operativeCage = nil
		}
	}
	member.operativeChannels = nil
	for sessionKey, observer := range e.sessions {
		if observer.zone != member.zone || observer.binding.UserID == member.binding.UserID ||
			observer.isRejoinPending || !observer.stage.IsDungeon() {
			continue
		}
		observer.queuePackets(packets)
		e.sessions[sessionKey] = observer
	}
	if retireErr != nil {
		return fmt.Errorf("cageTeardown: %w", retireErr)
	}
	return nil
}
