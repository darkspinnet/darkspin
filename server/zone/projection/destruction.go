package projection

// IsNPCDestroyed distinguishes OnAgentDestroyed from an actor's zero health.
func (e *Session) IsNPCDestroyed(objectID uint32) bool {
	if e == nil || objectID == 0 {
		return true
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	_, isDestroyed := e.deletedNPCObjectIDs[objectID]
	return isDestroyed
}
