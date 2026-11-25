package gameplay

// The registry lock is held. Generic death/dispel retirement also removes the
// exact feature owner so a later hit cannot refresh a released modifier slot.
func (e *gameplayPeerSession) forgetHeroTimeRavagerSlow(modifier *campaignNPCModifierRun) {
	for targetObjectID, run := range e.heroTimeRavagerSlows {
		if run == nil || run.modifier != modifier {
			continue
		}
		if run.cancel != nil {
			run.cancel()
			run.cancel = nil
		}
		if e.zone != nil && e.zone.NPCs() != nil {
			e.zone.NPCs().ClearSlowModifier(targetObjectID, modifier.instanceID)
		}
		delete(e.heroTimeRavagerSlows, targetObjectID)
	}
}

// An accepted modifier can be reserved but not yet published. Only a released
// allocation is retired; an unpublished live reservation remains reusable.
func (e *campaignNPCModifierRun) isRetired() bool {
	if e == nil {
		return true
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.isReleased
}
