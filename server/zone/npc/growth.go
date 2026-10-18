package npc

import (
	"errors"
	"time"
)

const oozeGrowthMaximumStack = 4

func (e Snapshot) IsOozeGrowthReady(timestamp uint64) bool {
	return timestamp >= e.status.oozeGrowthReadyTimestamp
}

// CommitOozeGrowthAction keeps growth's cooldown independent of melee while
// retaining it across retargeting and changes of the action's party owner.
func (e *Session) CommitOozeGrowthAction(source Snapshot, timestamp uint64) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	npc, isFound := e.npcs[source.Plan.ObjectID]
	if !isFound || npc.IsDefeated || !npc.IsActionStarted ||
		npc.ActionGeneration != source.ActionGeneration ||
		!npc.IsOozeGrowthReady(timestamp) {
		return false
	}
	profile, isProfileFound := VerdanthBasicOozeGrowProfile(npc.Plan.NounName)
	if !isProfileFound {
		return false
	}
	npc.status.oozeGrowthReadyTimestamp = timestamp + uint64(profile.Cooldown/time.Millisecond)
	e.npcs[source.Plan.ObjectID] = npc
	return true
}

type OozeGrowthResult struct {
	Target       Snapshot
	StackCount   uint32
	HealedAmount float32
}

func (s *Session) OozeGrowthTarget(sourceObjectID uint32) (Snapshot, bool) {
	if s == nil || sourceObjectID == 0 {
		return Snapshot{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	target, isFound := s.npcs[sourceObjectID]
	if !isFound || target.IsDefeated || !target.IsPublished ||
		target.Plan.IsFixture || target.HitPoint <= 0 ||
		target.status.oozeGrowthStack >= oozeGrowthMaximumStack {
		return Snapshot{}, false
	}
	return target, true
}

func (s *Session) ApplyOozeGrowth(
	sourceObjectID uint32, targetObjectID uint32,
) (OozeGrowthResult, error) {
	if s == nil || sourceObjectID == 0 || targetObjectID == 0 {
		return OozeGrowthResult{}, errors.New("invalid ooze growth")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	source, isSourceFound := s.npcs[sourceObjectID]
	target, isTargetFound := s.npcs[targetObjectID]
	if !isSourceFound || source.IsDefeated || !source.IsPublished ||
		source.HitPoint <= 0 {
		return OozeGrowthResult{}, errors.New("ooze growth source unavailable")
	}
	if !isTargetFound || targetObjectID != sourceObjectID || target.IsDefeated ||
		!target.IsPublished || target.Plan.IsFixture || target.HitPoint <= 0 {
		return OozeGrowthResult{}, errors.New("ooze growth target unavailable")
	}
	if target.status.oozeGrowthStack >= oozeGrowthMaximumStack {
		return OozeGrowthResult{}, errors.New("ooze growth target capped")
	}
	if target.status.oozeBaseMaximumHitPoint <= 0 {
		target.status.oozeBaseMaximumHitPoint = target.Plan.NPCProfile.HitPoint
		target.status.oozeBaseGraphicsScale = target.Plan.NPCProfile.GraphicsScale
	}
	baseMaximumHitPoint := target.status.oozeBaseMaximumHitPoint
	if baseMaximumHitPoint <= 0 {
		return OozeGrowthResult{}, errors.New("ooze growth health unavailable")
	}
	previousMaximumHitPoint := target.Plan.NPCProfile.HitPoint
	target.status.oozeGrowthStack++
	stackScale := float32(target.status.oozeGrowthStack)
	target.status.oozeGrowthDamageIncrease = 0.25 * stackScale
	target.Plan.NPCProfile.HitPoint = baseMaximumHitPoint * (1 + 0.25*stackScale)
	if target.status.oozeBaseGraphicsScale > 0 {
		target.Plan.NPCProfile.GraphicsScale =
			target.status.oozeBaseGraphicsScale * (1 + 0.1*stackScale)
	}
	healedAmount := target.Plan.NPCProfile.HitPoint - previousMaximumHitPoint
	target.HitPoint += healedAmount
	s.npcs[targetObjectID] = target
	return OozeGrowthResult{
		Target: target, StackCount: target.status.oozeGrowthStack,
		HealedAmount: healedAmount,
	}, nil
}
