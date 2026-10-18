package npc

import (
	"errors"
	"fmt"
	"math"
	"time"

	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

const RepairRadius = float32(20)
const RepairChance = 0.5
const repairFailedRollDuration = 2 * time.Second

type ShouldRepairRequest struct {
	SourceObjectID     uint32
	CandidateObjectIDs []uint32
	RandomRoll         float64
	At                 time.Time
}

func (e *Session) RepairBlockRemaining(objectID uint32, at time.Time) time.Duration {
	if e == nil || objectID == 0 || at.IsZero() {
		return 0
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return max(time.Duration(0), e.npcs[objectID].status.repairBlockedUntil.Sub(at))
}

// ShouldRepair ports client chunk241. Candidate IDs preserve the caller's
// radius-query ordering and carry the retained death run's eligibility check.
// No combat-state gate is authored by this condition.
func (e *Session) ShouldRepair(req ShouldRepairRequest) (Snapshot, bool, error) {
	if e == nil || req.SourceObjectID == 0 || req.At.IsZero() ||
		math.IsNaN(req.RandomRoll) || math.IsInf(req.RandomRoll, 0) ||
		req.RandomRoll < 0 || req.RandomRoll > 1 {
		return Snapshot{}, false, errors.New("invalid repair condition")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	source, isSourceFound := e.npcs[req.SourceObjectID]
	if !isSourceFound || source.IsDefeated || !source.IsPublished || source.HitPoint <= 0 {
		return Snapshot{}, false, nil
	}
	if req.At.Before(source.status.repairBlockedUntil) {
		return Snapshot{}, false, nil
	}
	if req.RandomRoll >= RepairChance {
		source.status.repairBlockedUntil = req.At.Add(repairFailedRollDuration)
		e.npcs[req.SourceObjectID] = source
		return Snapshot{}, false, nil
	}
	for _, objectID := range req.CandidateObjectIDs {
		target, isTargetFound := e.npcs[objectID]
		if !isTargetFound || objectID == req.SourceObjectID ||
			!target.IsPublished || !target.IsDefeated || target.Plan.IsFixture ||
			target.Faction != source.Faction || target.status.isCorpseFading ||
			target.Plan.NPCProfile.CreatureType != 0 ||
			zonegeometry.Distance(source.Plan.Position, target.Plan.Position) > RepairRadius {
			continue
		}
		return target, true, nil
	}
	return Snapshot{}, false, nil
}

// ApplyRepairStack mirrors BeingRepairedModifier's shared corpse stack count.
// NPC type zero uses minionStackCount; other robots use maxStackCount.
func (e *Session) ApplyRepairStack(objectID, minionThreshold, otherThreshold uint32) (bool, error) {
	if e == nil || objectID == 0 || minionThreshold == 0 || otherThreshold == 0 {
		return false, errors.New("invalid repair stack")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	target, isFound := e.npcs[objectID]
	if !isFound || !target.IsPublished || !target.IsDefeated || target.Plan.IsFixture {
		return false, fmt.Errorf("repairTarget: %d", objectID)
	}
	threshold := otherThreshold
	if target.Plan.NPCProfile.NPCType == 0 {
		threshold = minionThreshold
	}
	if target.repairStackCount < threshold {
		target.repairStackCount++
	}
	e.npcs[objectID] = target
	return target.repairStackCount >= threshold, nil
}
