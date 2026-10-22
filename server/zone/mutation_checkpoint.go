package zone

// BeginMutationDamage excludes checkpoint capture from before target selection
// and random draws through the final authority receipt update. The caller must
// pair every accepted admission with EndMutationDamage, including error paths.
func (e *Zone) BeginMutationDamage() bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive || e.mutationDamageRunCount == ^uint32(0) {
		return false
	}
	e.mutationDamageRunCount++
	return true
}

func (e *Zone) EndMutationDamage() {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.mutationDamageRunCount > 0 {
		e.mutationDamageRunCount--
	}
}
