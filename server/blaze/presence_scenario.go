//go:build scenario

package blaze

// ScenarioPresence describes the last client-reported ship location. Revision
// identifies a changed presence envelope, not a frame or native scene boundary.
type ScenarioPresence struct {
	Location    string
	Revision    uint64
	IsAvailable bool
}

// ObserveScenarioPresence reads one retained envelope without changing it.
// It does not establish freshness, current authentication, or world readiness.
func (e *PresenceRegistry) ObserveScenarioPresence(userID int64) ScenarioPresence {
	if e == nil || userID <= 0 {
		return ScenarioPresence{}
	}
	record, isFound := e.lookup(userID)
	if !isFound {
		return ScenarioPresence{}
	}
	presence := ScenarioPresence{Revision: record.revision}
	switch fieldInteger(presenceVariableDataFields(record.variables), "STAT") {
	case 3:
		presence.Location = "arsenal"
	case 4:
		presence.Location = "navigation"
	case 7:
		presence.Location = "editor"
	default:
		return presence
	}
	presence.IsAvailable = true
	return presence
}
