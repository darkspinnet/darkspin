package npc

import (
	"errors"
	"strings"
)

// ActivateEnemyPortal retains the stable passive for subsequent snapshots.
// A portal is a fixture, so changing its presentation must not start an action.
func (e *Session) ActivateEnemyPortal(objectID uint32) (Snapshot, error) {
	if e == nil || objectID == 0 {
		return Snapshot{}, errors.New("portal session unavailable")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	portal, isFound := e.npcs[objectID]
	if !isFound || !portal.IsPublished || portal.IsDefeated || !portal.Plan.IsFixture ||
		!strings.EqualFold(portal.Plan.NounName, "EnemyPortal.Noun") {
		return Snapshot{}, errors.New("portal activation unavailable")
	}
	portal.Plan.ActionProfile.PassiveEffectName = "scaldron_boss_portal_effect.ServerEventDef"
	e.npcs[objectID] = portal
	return portal, nil
}
