//go:build !scenario

package gameplay

func (e *gameplaySessionRegistry) recordSpoffitBirthLocked(requestID string, objectID, ownerID uint32) {
}

func (e *gameplaySessionRegistry) recordSpoffitRemovalLocked(requestID string, objectID, ownerID uint32, isRetired bool) {
}

func (e *gameplaySessionRegistry) recordSpoffitFailureLocked(requestID, removalID string, cause error) {
}
