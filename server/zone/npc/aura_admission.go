package npc

// AuraImmunitySnapshot contains the current immunity contributions represented
// by the server's NPC authority. It does not claim to recover unknown inherent
// or class attributes from installed content.
type AuraImmunitySnapshot struct {
	ImmuneToSleep   float32
	ImmuneToSilence float32
}

// AuraAdmission evaluates a bounded, read-only modifier predicate while the
// NPC lock is held. Implementations consume only this copied snapshot and must
// not query or mutate the NPC session or world. Game mode belongs to the caller.
type AuraAdmission interface {
	CanUseAura(req AuraImmunitySnapshot) (bool, error)
}

func auraImmunitySnapshot(actor Snapshot) AuraImmunitySnapshot {
	immunity := AuraImmunitySnapshot{}
	// Build-103 Unstoppable Tick adds one to ImmuneToSleep (75) and
	// ImmuneToSilence (78). HasAffixModifier includes active aura grants.
	if actor.HasAffixModifier(unstoppableModifierName) {
		immunity.ImmuneToSleep = 1
		immunity.ImmuneToSilence = 1
	}
	return immunity
}
