//go:build scenario

package boss

import "github.com/darkspinnet/darkspin/server/scenario"

// ScenarioBoss copies terminal scalars without copying plans or actor lists.
func (e *Session) ScenarioBoss() *scenario.WorldBoss {
	if e == nil || !e.mu.TryRLock() {
		return nil
	}
	defer e.mu.RUnlock()
	return &scenario.WorldBoss{Phase: uint8(e.phase), LeaderObjectID: e.leaderObjectID, LeaderHitPoint: e.leaderHitPoint,
		IsComplete: e.phase == PhaseComplete, IsBeamOutCommitted: e.isBeamOutCommitted}
}
