package zone

import zonecheckpoint "github.com/darkspinnet/darkspin/server/zone/checkpoint"

// SaveDeploymentCheckpoint establishes resume state before the first pickup
// or defeated group. Wait for each known member's squad initialization so a
// partially loaded co-op roster cannot replace a valid checkpoint.
func (e *Zone) SaveDeploymentCheckpoint() {
	if e == nil {
		return
	}
	e.mu.RLock()
	for userID := range e.members {
		if _, isFound := e.checkpointSquads[userID]; !isFound {
			e.mu.RUnlock()
			return
		}
	}
	e.mu.RUnlock()
	e.SaveCheckpointIfSafe(zonecheckpoint.ReasonDeployment)
}
