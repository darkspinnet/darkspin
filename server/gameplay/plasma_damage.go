package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
)

func (e gameplayPeerSession) plasmaPetDamageRange(ability sim.AbilityDefinition) (game.DamageRange, error) {
	if e.deployedCreatureIndex >= uint32(len(e.binding.Creatures)) {
		return game.DamageRange{}, errors.New("Pain Hound owner unavailable")
	}
	creature := e.binding.Creatures[e.deployedCreatureIndex]
	// Build 103's pet modifier mirrors the owner's primary-stat selector and
	// base attributes; this pet's Strength/Dexterity/Mind coefficients are one.
	// The melee template applies the owner's PetDamage to base endpoints
	// separately, before primary-stat projection.
	petMultiplier := max(float32(0), 1+creature.PetDamage)
	damageRange, err := game.ResolveAbilityDamageRange(game.AbilityDamage{
		Minimum: ability.MinimumDamage * petMultiplier, Maximum: ability.MaximumDamage * petMultiplier,
		Coefficient: ability.DamageCoefficient,
	}, game.DamageProfile{
		PrimaryAttribute:                creature.DamageProfile.PrimaryAttribute,
		IsPrimaryAttributeFound:         creature.DamageProfile.IsPrimaryAttributeFound,
		PrimaryAttributeBaseline:        creature.DamageProfile.PrimaryAttributeBaseline,
		IsPrimaryAttributeBaselineFound: creature.DamageProfile.IsPrimaryAttributeBaselineFound,
	})
	if err != nil {
		return game.DamageRange{}, fmt.Errorf("houndDamage: %w", err)
	}
	return damageRange, nil
}
