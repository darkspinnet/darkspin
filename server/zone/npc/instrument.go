package npc

import "github.com/darkspinnet/darkspin/server/game"

// Build-103 chunks371/925 damage their own team, then retain the dead base.
// Chunk491 supplies the stabilizer's separate native knockback modifier.
func InstrumentDeathProfile(nounName string) (ActionProfile, bool) {
	profile := ActionProfile{Family: ActionRetainedArea, DamageType: 5,
		DamageSource: 1, IsDamageProfileKnown: true}
	if game.IsGraviticRegulatorNoun(nounName) {
		profile.AbilityName = "InstrumentDeath2"
		profile.MinimumDamage, profile.MaximumDamage = 1, 3
		profile.Radius = 6
		profile.DescriptorMask = 1<<3 | 1<<6
		profile.ImpactEffectName = "effect_environment_Zelems_instrument_minor_explosion.ServerEventDef"
		return profile, true
	}
	if !game.IsGraviticStabilizerNoun(nounName) {
		return ActionProfile{}, false
	}
	profile.AbilityName = "InstrumentDeath"
	profile.MinimumDamage, profile.MaximumDamage = 5, 15
	profile.Radius = 15
	profile.DescriptorMask = 1<<3 | 1<<7
	profile.ImpactEffectName = "zelem_gasCollectorUnit_destruction_effect.ServerEventDef"
	profile.ModifierName = "InstrumentKnockbackModifier"
	profile.ForcedMovementDistance, profile.ForcedMovementSpeed = 5, 10
	profile.ForcedMovementReactionName = "react_knockback"
	return profile, true
}
