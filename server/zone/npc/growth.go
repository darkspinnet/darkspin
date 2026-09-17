package npc

import (
	"errors"
	"math"

	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

const oozeGrowthMaximumStack = 4

type OozeGrowthResult struct {
	Target       Snapshot
	StackCount   uint32
	HealedAmount float32
}

func (s *Session) FirstOozeGrowthTarget(
	sourceObjectID uint32, maximumRange float32,
) (Snapshot, bool) {
	if s == nil || sourceObjectID == 0 || maximumRange <= 0 ||
		math.IsNaN(float64(maximumRange)) || math.IsInf(float64(maximumRange), 0) {
		return Snapshot{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	source, isFound := s.npcs[sourceObjectID]
	if !isFound || source.IsDefeated || !source.IsPublished || source.HitPoint <= 0 {
		return Snapshot{}, false
	}
	for _, objectID := range s.objectIDs {
		candidate := s.npcs[objectID]
		if candidate.Faction != source.Faction || candidate.IsDefeated ||
			!candidate.IsPublished || candidate.Plan.IsFixture ||
			candidate.HitPoint <= 0 ||
			candidate.status.oozeGrowthStack >= oozeGrowthMaximumStack ||
			zonegeometry.Distance(source.Plan.Position, candidate.Plan.Position) > maximumRange {
			continue
		}
		return candidate, true
	}
	return Snapshot{}, false
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
	if !isTargetFound || target.Faction != source.Faction || target.IsDefeated ||
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
