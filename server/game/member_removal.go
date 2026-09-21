package game

// UseMemberRemovalObserver connects individual departures to live mission cleanup.
func (e *Manager) UseMemberRemovalObserver(observer func(uint32, uint64)) {
	e.mu.Lock()
	e.memberRemovalObserver = observer
	e.mu.Unlock()
}

// RemoveMember retires both the roster entry and its live gameplay presence.
// Invoke the observer without game locks because cleanup can consult the roster.
func (e *Manager) RemoveMember(gameID uint32, userID int64) {
	if e == nil || userID <= 0 {
		return
	}
	e.mu.RLock()
	instance := e.games[gameID]
	observer := e.memberRemovalObserver
	e.mu.RUnlock()
	if instance == nil {
		return
	}
	instance.RemovePlayer(userID)
	if observer != nil {
		observer(gameID, uint64(userID))
	}
}
