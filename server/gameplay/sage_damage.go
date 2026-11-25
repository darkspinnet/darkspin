package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
)

func (e gameplayPeerSession) sagePetDamageRange(ability sim.AbilityDefinition) (game.DamageRange, error) {
	if e.deployedCreatureIndex >= uint32(len(e.binding.Creatures)) {
		return game.DamageRange{}, errors.New("Dendrone owner unavailable")
	}
	creature := e.binding.Creatures[e.deployedCreatureIndex]
	// Build 103's passive mirrors the owner's primary selector and base
	// attributes. HelperMelee's Strength/Dexterity/Mind mirror coefficients
	// are one. The melee template applies PetDamage to the authored endpoints
	// independently of that primary-stat projection.
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
		return game.DamageRange{}, fmt.Errorf("sageDamage: %w", err)
	}
	return damageRange, nil
}
