package gameplay

import (
	"errors"

	"github.com/darkspinnet/darkspin/server/game"
)

func (e gameplayPeerSession) beastPetDamageProfile() (game.DamageProfile, error) {
	if e.deployedCreatureIndex >= uint32(len(e.binding.Creatures)) {
		return game.DamageProfile{}, errors.New("beast owner unavailable")
	}
	creature := e.binding.Creatures[e.deployedCreatureIndex]
	// Build 103 SummonBeast mirrors the summoner's base attributes. The
	// non-player pet uses Mind for damage, with unit class coefficients.
	// Melee template chunk 832 separately multiplies pet damage by the
	// owner's PetDamage bonus; that percentage is not a primary stat.
	return game.DamageProfile{
		PrimaryAttribute: creature.Mind, IsPrimaryAttributeFound: true,
		PrimaryAttributeBaseline: game.PrimaryAttributeIgnoreAmount, IsPrimaryAttributeBaselineFound: true,
		DamageBuff: creature.PetDamage,
	}, nil
}
