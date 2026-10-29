//go:build scenario

package death

func (e *Session) IsScenarioPending(objectID uint32) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	_, isFound := e.entries[objectID]
	return isFound
}
