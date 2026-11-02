package zone

// MarkNPCSpawnCommandUsed records a successful developer spawn for every
// member's diagnostics, even after that NPC has died or a member reconnects.
func (e *Zone) MarkNPCSpawnCommandUsed() {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.isNPCSpawnCommandUsed = true
	e.mu.Unlock()
}
