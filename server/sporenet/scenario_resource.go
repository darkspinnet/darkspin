//go:build scenario

package sporenet

type DebugProfileResources struct {
	Level     uint32
	DNA       uint32
	PartCount uint32
}

// DebugResources observes only the three scalar command outcomes. It does not
// copy the user's inventory, credentials or persistence aggregate.
func (e *User) DebugResources() DebugProfileResources {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := DebugProfileResources{Level: e.Account.Level, DNA: e.Account.DNA}
	for _, part := range e.Parts {
		if part.OccupiesInventorySlot() {
			result.PartCount++
		}
	}
	return result
}
