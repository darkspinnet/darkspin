//go:build scenario

package security

import "github.com/darkspinnet/darkspin/server/scenario"

// ScenarioRoutes includes the recovered initial-chain routes that the authored
// campaign route resolver deliberately excludes in favor of this producer.
func (e *Session) ScenarioRoutes(level string) []scenario.WorldTeleporter {
	if e == nil || !e.mutex.TryRLock() {
		return nil
	}
	defer e.mutex.RUnlock()
	teleporters := make([]scenario.WorldTeleporter, 0, RouteCount)
	for index, route := range Routes(level) {
		objectID := e.objectID[index]
		if objectID == 0 {
			continue
		}
		teleporters = append(teleporters, scenario.WorldTeleporter{RouteIndex: index, ObjectID: objectID,
			Source:      scenario.WorldPosition{X: route.Source.X, Y: route.Source.Y, Z: route.Source.Z},
			Destination: scenario.WorldPosition{X: route.Destination.X, Y: route.Destination.Y, Z: route.Destination.Z},
			IsSecurity:  true, IsBoss: route.IsBoss})
	}
	return teleporters
}

// ScenarioAnchor never waits for a producer lock during scoped observation.
func (e *Session) ScenarioAnchor(markerID uint32) (uint32, bool) {
	if e == nil || !e.mutex.TryRLock() {
		return 0, false
	}
	defer e.mutex.RUnlock()
	objectID, isFound := e.anchorsBySourceID[markerID]
	return objectID, isFound
}
