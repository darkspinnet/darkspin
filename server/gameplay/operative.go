package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e gameplayPeerSession) isOperativeCaged(at time.Time) bool {
	run := e.operativeCage
	if run == nil || !run.isActive() || e.zone == nil ||
		run.record.TargetObjectID != e.deployedObjectID || e.deployedHitPoint() <= 0 ||
		e.isHeroDebuffImmune(e.deployedObjectID) {
		return false
	}
	sourceID := run.record.SourceObjectID
	enemy, isFound := e.zone.NPCs().NPC(sourceID)
	return isFound && !enemy.IsDefeated && enemy.HitPoint > 0 &&
		enemy.IsActionStarted &&
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
			target.isOperativeCaged(e.now()) {
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
	deletedPacket, err := effectraknet.ModifierDelete(run.record.TargetObjectID, run.instanceID)
	if err != nil {
		e.registry.mutex.Unlock()
		return fmt.Errorf("cageDelete: %w", err)
	}
	isCreated, err := run.release(e.modifierPool)
	if err != nil {
		e.registry.mutex.Unlock()
		return fmt.Errorf("cageRelease: %w", err)
	}
	// Expiry or source teardown may already have released the pool allocation;
	// the victim still needs its native cage presentation removed.
	if isCreated && run.cancel != nil {
		run.cancel()
	}
	if target.zone != nil {
		target.zone.Effect().Remove(run.instanceID)
	}
	for sessionKey, source := range e.registry.sessions {
		if source.zone == target.zone {
			source.untrackCampaignNPCModifier(run)
			e.registry.sessions[sessionKey] = source
		}
	}
	target.untrackCampaignNPCModifier(run)
	target.operativeCage = nil
	packets := [][]byte{deletedPacket}
	target.queuePackets(packets)
	e.registry.sessions[packet.Address.String()] = target
	identity := gameplayProducerIdentityFromSession(packet.Address.String(), target, true)
	e.registry.mutex.Unlock()
	e.registry.queuePeerPresentation(identity, packets)
	return nil
}
