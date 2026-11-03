package game

// PlayerActivity reads one member's lifecycle and mode under the instance lock.
// Bot roster slots are separate from the authenticated player directory.
func (e *Instance) PlayerActivity(userID int64) (Mode, SessionState, bool) {
	if e == nil || userID <= 0 {
		return 0, 0, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, isMember := e.players[userID]
	return e.Info.Mode, e.Info.State, isMember
}
