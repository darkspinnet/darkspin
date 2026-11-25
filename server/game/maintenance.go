package game

// IsMaintenanceActive avoids treating an Arena inter-round transition as a
// finished best-of-three match. Retain that match until players leave or the
// maintenance deadline expires; ordinary lobby-only accounts never count.
func (e *Instance) IsMaintenanceActive() bool {
	if e == nil {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if len(e.players) == 0 {
		return false
	}
	if e.Info.State == StateInGame {
		return true
	}
	return e.Info.Mode == ModeArena && !e.StartedAt.IsZero() && e.Info.State != StateDestructing
}
