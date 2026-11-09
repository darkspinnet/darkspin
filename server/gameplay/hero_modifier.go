package gameplay

import "maps"

// Self modifiers replace only another instance of the same modifier on the
// same actor. Each run owns its grants, presentation, and expiry independently.
type heroModifierKey struct {
	objectID   uint32
	modifierID uint32
}

func (e *heroModifierRun) key() heroModifierKey {
	return heroModifierKey{objectID: e.objectID, modifierID: e.modifierID}
}

func (e gameplayPeerSession) heroModifier(modifierID uint32) *heroModifierRun {
	return e.heroModifierRuns[heroModifierKey{objectID: e.deployedObjectID, modifierID: modifierID}]
}

func (e gameplayPeerSession) isHeroModifierCurrent(run *heroModifierRun) bool {
	return run != nil && e.heroModifierRuns[run.key()] == run
}

func (e *gameplayPeerSession) putHeroModifier(run *heroModifierRun) {
	e.heroModifierRuns = maps.Clone(e.heroModifierRuns)
	if e.heroModifierRuns == nil {
		e.heroModifierRuns = make(map[heroModifierKey]*heroModifierRun)
	}
	e.heroModifierRuns[run.key()] = run
}

func (e *gameplayPeerSession) forgetHeroModifier(run *heroModifierRun) {
	if !e.isHeroModifierCurrent(run) {
		return
	}
	e.heroModifierRuns = maps.Clone(e.heroModifierRuns)
	delete(e.heroModifierRuns, run.key())
}

func (e gameplayPeerSession) shadowStealthModifier() *heroModifierRun {
	for _, run := range e.heroModifierRuns {
		if run.objectID == e.deployedObjectID && run.isShadowStealth {
			return run
		}
	}
	return nil
}

func (e gameplayPeerSession) quantumStateModifier() *heroModifierRun {
	for _, run := range e.heroModifierRuns {
		if run.objectID == e.deployedObjectID && run.isQuantumState {
			return run
		}
	}
	return nil
}

func (e gameplayPeerSession) arborealMightModifier() *heroModifierRun {
	for _, run := range e.heroModifierRuns {
		if run.objectID == e.deployedObjectID && run.isArborealMight {
			return run
		}
	}
	return nil
}

func (e gameplayPeerSession) hasSoulLinkModifier() bool {
	for _, run := range e.heroModifierRuns {
		if run.objectID == e.deployedObjectID && run.creatureIndex == e.deployedCreatureIndex && run.isSoulLink {
			return true
		}
	}
	return false
}

func (e gameplayPeerSession) hasHeroDamageImmunity() bool {
	for _, run := range e.heroModifierRuns {
		if run.objectID == e.deployedObjectID && run.IsDamageImmune() {
			return true
		}
	}
	return false
}

func (e gameplayPeerSession) hasHeroDebuffImmunity() bool {
	for _, run := range e.heroModifierRuns {
		if run.objectID == e.deployedObjectID && run.IsDebuffImmune() {
			return true
		}
	}
	return false
}
