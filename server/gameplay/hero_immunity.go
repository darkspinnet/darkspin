package gameplay

import "time"

type heroOwnImmunitySnapshot struct {
	silence         float32
	poisonOrDisease float32
	isDebuffImmune  bool
}

// These are the recipient hero's current own attributes, not the owner's
// contribution to a companion mirror or an earlier damage snapshot.
func (e gameplayPeerSession) heroOwnImmunity(now time.Time) (heroOwnImmunitySnapshot, bool) {
	if e.deployedObjectID == 0 || int(e.deployedCreatureIndex) >= len(e.binding.Creatures) {
		return heroOwnImmunitySnapshot{}, false
	}
	creature := e.binding.Creatures[e.deployedCreatureIndex]
	immunity := heroOwnImmunitySnapshot{
		silence: creature.PartAttribute[78], poisonOrDisease: creature.PartAttribute[80],
		isDebuffImmune: creature.PartAttribute[49] > 0 || e.heroModifierRun.IsDebuffImmune(),
	}
	if e.isOverdriveActiveAt(now) {
		// Build-103 OverdriveModifier561 Activate grants one to own78 and
		// own80. It does not grant the independent general-debuff attribute49.
		immunity.silence++
		immunity.poisonOrDisease++
	}
	return immunity, true
}
