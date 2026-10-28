//go:build scenario

package main

import "github.com/darkspinnet/darkspin/server/scenario"

func debugWorldHeroMotionDTO(hero *debugWorldHero, snapshot *scenario.WorldHero) {
	hero.FlakStackCount = snapshot.FlakStackCount
	hero.FlakAttackSpeed = snapshot.FlakAttackSpeed
	hero.IsStunned = snapshot.IsStunned
	hero.IsSilenced = snapshot.IsSilenced
	hero.HostileMovementModifier = snapshot.HostileMovementModifier
	hero.HostileAttackModifier = snapshot.HostileAttackModifier
	hero.SphereMembershipCount = snapshot.SphereMembershipCount
	hero.MovementPosition = debugWorldPositionDTO(snapshot.MovementPosition)
	hero.MovementSpeed = snapshot.MovementSpeed
	hero.IsMovementAvailable = snapshot.IsMovementAvailable
	hero.MovementUnavailableReason = snapshot.MovementUnavailableReason
}
