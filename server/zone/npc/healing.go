package npc

import (
	"errors"
	"math"
	"time"
)

// PreviewHealing lets a transport prepare feedback before accepting the
// resource change. RestoreHealing rejects a changed actor or healing profile.
func (e *Session) PreviewHealing(objectID uint32, amount float32, at time.Time) (Snapshot, float32, float32, error) {
	if e == nil || amount <= 0 || math.IsNaN(float64(amount)) || math.IsInf(float64(amount), 0) {
		return Snapshot{}, 0, 0, errors.New("invalid healing preview")
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	target, isFound := e.npcs[objectID]
	if !isFound || !target.IsPublished || target.IsDefeated || target.HitPoint <= 0 {
		return Snapshot{}, 0, 0, errors.New("healing target unavailable")
	}
	if at.Before(target.status.healingReductionEnd) {
		amount *= 1 - target.status.healingReduction
	}
	return target, amount, min(target.Plan.NPCProfile.HitPoint, target.HitPoint+amount), nil
}

func (e *Session) RestoreHealing(expected Snapshot, hitPoint float32) (bool, error) {
	if e == nil || math.IsNaN(float64(hitPoint)) || math.IsInf(float64(hitPoint), 0) || hitPoint < expected.HitPoint || hitPoint > expected.Plan.NPCProfile.HitPoint {
		return false, errors.New("invalid healing restoration")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	target, isFound := e.npcs[expected.Plan.ObjectID]
	if !isFound || target.IsDefeated || !target.IsPublished || target.HitPoint != expected.HitPoint ||
		target.Faction != expected.Faction || target.Plan.NPCProfile.HitPoint != expected.Plan.NPCProfile.HitPoint ||
		target.ActionGeneration != expected.ActionGeneration || target.Plan.NounName != expected.Plan.NounName ||
		target.status.healingReductionEnd != expected.status.healingReductionEnd || target.status.healingReduction != expected.status.healingReduction {
		return false, nil
	}
	target.HitPoint = hitPoint
	e.npcs[expected.Plan.ObjectID] = target
	return true, nil
}
