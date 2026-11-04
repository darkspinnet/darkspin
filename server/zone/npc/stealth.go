package npc

import (
	"fmt"
	"time"
)

// IsStealthReady keeps the special cooldown independent of ordinary melee.
func (e Snapshot) IsStealthReady(now time.Time) bool {
	return !now.Before(e.status.stealthReadyAt)
}

func (e *Session) BeginStealthCooldown(objectID uint32, actionGeneration uint64, readyAt time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	enemy, isFound := e.npcs[objectID]
	if !isFound || enemy.IsDefeated || enemy.ActionGeneration != actionGeneration {
		return fmt.Errorf("stealthCooldownActor: %d", objectID)
	}
	enemy.status.stealthReadyAt = readyAt
	e.npcs[objectID] = enemy
	return nil
}
