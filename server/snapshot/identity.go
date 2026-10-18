package snapshot

import "time"

// The monitor retains handle/network-ID associations longer than the cropped
// packet tail. Seed only identities belonging to the exact current world and
// still present in its authoritative keyframe; never match by noun or position.
func retainedMovementBindings(samples []movementSample, state StateFrame) map[uint32]uint32 {
	bindings := make(map[uint32]uint32)
	latestTimesByHandle := make(map[uint32]time.Time)
	for _, session := range state.Sessions {
		objectsByID := make(map[uint32]ObjectState, len(session.Objects))
		for _, object := range session.Objects {
			if object.IsDefeated || object.ObjectID == 0 {
				continue
			}
			objectsByID[object.ObjectID] = object
		}
		for _, sample := range samples {
			if sample.Remote != session.Remote || sample.SessionGeneration != session.SessionGeneration ||
				sample.TransportGeneration != session.TransportGeneration || sample.ZoneGeneration != session.ZoneGeneration ||
				sample.ClientObjectID == 0 || !sample.IsTimingValid {
				continue
			}
			object, isFound := objectsByID[sample.Object.ObjectID]
			if !isFound || object.Kind != sample.Object.Kind || object.NounName != sample.Object.NounName ||
				sample.CapturedAt.After(state.CapturedAt) || state.CapturedAt.Sub(sample.CapturedAt) > time.Minute {
				continue
			}
			if previous, isMapped := latestTimesByHandle[sample.ClientObjectID]; isMapped && !sample.CapturedAt.After(previous) {
				continue
			}
			bindings[sample.ClientObjectID] = sample.Object.ObjectID
			latestTimesByHandle[sample.ClientObjectID] = sample.CapturedAt
		}
	}
	return bindings
}
