package gameplay

import "github.com/darkspinnet/darkspin/server/sim"

// isGenericHeroActiveKind reports the ability shapes whose complete active
// admission, resource, cooldown, damage, release, and rollback paths are shared.
func isGenericHeroActiveKind(kind sim.AbilityKind) bool {
	return kind == sim.AbilityKindProjectile ||
		kind == sim.AbilityKindProjectileBurst ||
		kind == sim.AbilityKindMelee ||
		kind == sim.AbilityKindCursorArea ||
		kind == sim.AbilityKindPointBlank ||
		kind == sim.AbilityKindCone ||
		kind == sim.AbilityKindModifier ||
		kind == sim.AbilityKindChannelArea ||
		kind == sim.AbilityKindTrap ||
		kind == sim.AbilityKindChannelDrain ||
		kind == sim.AbilityKindTimedArea ||
		kind == sim.AbilityKindStatusArea ||
		kind == sim.AbilityKindInfection ||
		kind == sim.AbilityKindHealingTicks ||
		kind == sim.AbilityKindAuraArea ||
		kind == sim.AbilityKindProjectileStatus ||
		kind == sim.AbilityKindCharge ||
		kind == sim.AbilityKindTeleportArea ||
		kind == sim.AbilityKindRepulsion ||
		kind == sim.AbilityKindModifierArea ||
		kind == sim.AbilityKindQuantumBlink ||
		kind == sim.AbilityKindReactiveSummon ||
		kind == sim.AbilityKindSummonBuff
}
