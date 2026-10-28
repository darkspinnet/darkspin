//go:build scenario

package gameplay

import "github.com/darkspinnet/darkspin/server/scenario"

// Called with the registry read lock held. Arena owns its deployed actor in
// the peer's squad session, rather than a campaign zone. Copy that producer
// without advancing movement or manufacturing a campaign zone generation.
func (e *gameplaySessionRegistry) scenarioArenaWorld(
	peer gameplayPeerSession, result scenario.WorldState,
) scenario.WorldState {
	match := e.arenaMatches[peer.binding.GameID]
	ability := e.scenarioAbilitySnapshot(peer, e.now())
	result.Ability = &ability
	result.Arena = &scenario.WorldArena{
		Team: peer.binding.Team, WinningTeam: peer.arenaWinningTeam,
		IsCombatActive: match != nil && match.isCombatActive,
		IsResultReady:  peer.isArenaResultReady,
	}
	// Read publication/retirement ownership only. Projectile Snapshot advances
	// retained motion and therefore cannot be called from this observer.
	for _, retirement := range e.projectileRetirements {
		if retirement.arenaGameID != peer.binding.GameID || !retirement.isAdmitted || !retirement.isPublished ||
			retirement.isRetired || retirement.isRetirementPending {
			continue
		}
		owner, isFound := e.sessions[retirement.sessionKey]
		if !isFound || owner.generation != retirement.generation || owner.binding.GameID != peer.binding.GameID {
			continue
		}
		if owner.binding.Team == peer.binding.Team {
			result.Arena.FriendlyProjectileCount++
		} else {
			result.Arena.HostileProjectileCount++
		}
	}
	for _, member := range e.sessions {
		if member.binding.GameID != peer.binding.GameID || member.squad == nil {
			continue
		}
		memberSS := scenario.WorldArenaMember{UserID: member.binding.UserID,
			Team: member.binding.Team, DeployedIndex: member.deployedCreatureIndex}
		if match != nil {
			statistics := match.result.players[member.binding.UserID]
			memberSS.DamageDone, memberSS.DamageTaken = statistics.DamageDealt, statistics.DamageTaken
			memberSS.HealingDone, memberSS.HealingReceived = statistics.HealingDealt, statistics.HealingReceived
		}
		remaining := member.squad.Snapshot().DeployCooldownEnd.Sub(e.now()).Milliseconds()
		if remaining > 0 {
			memberSS.SwapRemainingMS = uint32(remaining + 1)
		}
		for index := range member.binding.Creatures {
			character, isFound := member.squad.Character(uint32(index))
			if !isFound || !character.IsAvailable {
				continue
			}
			maximumHitPoint, maximumManaPoint := member.characterResourceMaximum(uint32(index))
			hero := scenario.WorldHero{
				Index: uint32(index), HitPoint: character.HitPoints, ManaPoint: character.ManaPoints,
				Noun:             member.binding.Creatures[index].Noun,
				PassiveKillStack: member.passiveKillStack[index],
				MaximumHitPoint:  maximumHitPoint, MaximumManaPoint: maximumManaPoint,
				IsDefeated: character.HitPoints <= 0,
			}
			member.fillScenarioHeroMotion(&hero, result.CapturedAt)
			memberSS.Characters = append(memberSS.Characters, hero)
		}
		result.Arena.Members = append(result.Arena.Members, memberSS)
	}
	if !peer.dungeonSetup.IsCommitted() || !peer.isArenaSquadAccepted ||
		match == nil || !match.isCombatActive || peer.squad == nil {
		result.UnavailableReason = "arena requires accepted squad and committed combat setup"
		return result
	}
	if peer.deployedObjectID == 0 || peer.deployedCreatureIndex >= uint32(len(peer.binding.Creatures)) {
		result.UnavailableReason = "arena deployed hero identity is unavailable"
		return result
	}
	character, isFound := peer.squad.Character(peer.deployedCreatureIndex)
	if !isFound || !character.IsAvailable {
		result.UnavailableReason = "arena deployed hero snapshot is unavailable"
		return result
	}
	maximumHitPoint, maximumManaPoint := peer.characterResourceMaximum(peer.deployedCreatureIndex)
	result.Hero = &scenario.WorldHero{
		InputLockRemainingMS: abilityDurationMS(peer.heroInputLockRemaining(e.now())),
		ObjectID:             peer.deployedObjectID, Index: peer.deployedCreatureIndex,
		Noun:             peer.binding.Creatures[peer.deployedCreatureIndex].Noun,
		PassiveKillStack: peer.passiveKillStack[peer.deployedCreatureIndex],
		Position: scenario.WorldPosition{
			X: peer.playerPosition.X, Y: peer.playerPosition.Y, Z: peer.playerPosition.Z,
		},
		HitPoint: character.HitPoints, ManaPoint: character.ManaPoints,
		MaximumHitPoint: maximumHitPoint, MaximumManaPoint: maximumManaPoint,
		IsDefeated: character.HitPoints <= 0,
	}
	result.IsAvailable = true
	peer.fillScenarioHeroMotion(result.Hero, result.CapturedAt)
	return result
}
