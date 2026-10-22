package npc

import (
	"math"
	"time"
)

const ShieldedModifierName = "Shielded_NPCAffixModifier"

// Shielded's activation generates absorption immediately. Its Lua uses
// major*10+minor difficulty and the modifier's rank, not the noun suffix.
func shieldedStatus(plan SpawnPlan, difficulty, playerCount uint32) status {
	difficulty = max(uint32(1), difficulty)
	major := (difficulty-1)/4 + 1
	minor := (difficulty-1)%4 + 1
	amount := float32(10 * math.Pow(1.037500023841858, float64(major*10+minor)) *
		float64(max(uint32(1), playerCount)))
	recharges := [...]time.Duration{12 * time.Second, 10 * time.Second, 8 * time.Second}
	return status{
		absorptionShieldAmount: amount, absorptionShieldMaximum: amount,
		absorptionShieldRecharge: recharges[plan.AffixRank(ShieldedModifierName)-1],
	}
}

func (e *Snapshot) renewAbsorptionShield(now time.Time) bool {
	if e.status.absorptionShieldMaximum <= 0 || e.status.absorptionShieldAmount > 0 ||
		e.status.absorptionShieldReadyAt.IsZero() || now.Before(e.status.absorptionShieldReadyAt) {
		return false
	}
	e.status.absorptionShieldAmount = e.status.absorptionShieldMaximum
	e.status.absorptionShieldReadyAt = time.Time{}
	return true
}

// RenewAbsorptionShields runs independently of incoming damage, matching the
// modifier coroutine's wait followed by shield generation.
func (e *Session) RenewAbsorptionShields(now time.Time) []uint32 {
	if e == nil || now.IsZero() {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	objectIDs := make([]uint32, 0)
	for _, objectID := range e.objectIDs {
		npc := e.npcs[objectID]
		if npc.IsDefeated || !npc.IsPublished || npc.Plan.IsIntroductionHidden ||
			!npc.renewAbsorptionShield(now) {
			continue
		}
		e.npcs[objectID] = npc
		objectIDs = append(objectIDs, objectID)
	}
	return objectIDs
}
