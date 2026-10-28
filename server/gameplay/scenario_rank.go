//go:build scenario

package gameplay

import (
	"errors"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/squad"
)

// This fixture affects server-authored rank projection only. Native commands,
// native UI state, saved profiles, resources and cooldowns are not changed.
type scenarioHeroRankOverride struct {
	rank                int32
	userID              uint64
	gameID              uint32
	objectID            uint32
	noun                uint32
	index               uint32
	peerGeneration      uint64
	transportGeneration uint64
	squad               *squad.Session
}

func (e *scenarioHeroRankOverride) matches(member gameplayPeerSession) bool {
	return e != nil && member.binding.Mode == game.ModeArena && member.isArenaCombatActive &&
		member.binding.UserID == e.userID && member.binding.GameID == e.gameID &&
		member.deployedObjectID == e.objectID && member.deployedCreatureIndex == e.index &&
		member.generation == e.peerGeneration && member.transportGeneration == e.transportGeneration &&
		member.squad == e.squad && member.deployedHitPoint() > 0 &&
		e.index < uint32(len(member.binding.Creatures)) && member.binding.Creatures[e.index].Noun == e.noun &&
		member.scenarioAbility.retirement == nil
}

func (e gameplayPeerSession) scenarioHeroAbilityRank(baseRank int32) int32 {
	override := e.scenarioAbility.rankOverride
	if override.matches(e) {
		return override.rank
	}
	return baseRank
}

func (e *gameplayPeerSession) clearScenarioHeroAbilityRank() { e.scenarioAbility.rankOverride = nil }

func (e *gameplaySessionRegistry) setScenarioHeroAbilityRankLocked(member gameplayPeerSession, req scenario.HeroResetRequest, recipientCount int) (scenario.HeroResetReceipt, error) {
	if req.AbilityRank < 0 || req.AbilityRank > 8 || req.Noun != 0 || req.IsPlacement || req.IsCooldownReset ||
		req.IsRetirement || req.HitPoint != nil || req.ManaPoint != nil || req.Position != nil {
		return scenario.HeroResetReceipt{}, errors.New("ability rank fixture requires only rank0 through8")
	}
	index := member.deployedCreatureIndex
	if req.ObservedNoun == 0 || req.ObservedNoun != member.binding.Creatures[index].Noun || req.ObservedIndex != index {
		return scenario.HeroResetReceipt{}, errors.New("ability rank fixture actor observation changed")
	}
	member.clearScenarioHeroAbilityRank()
	if req.AbilityRank != 0 {
		member.scenarioAbility.rankOverride = &scenarioHeroRankOverride{rank: req.AbilityRank,
			userID: member.binding.UserID, gameID: member.binding.GameID, objectID: member.deployedObjectID,
			index: index, noun: member.binding.Creatures[index].Noun, peerGeneration: member.generation,
			transportGeneration: member.transportGeneration, squad: member.squad}
	}
	for key, candidate := range e.sessions {
		if candidate.binding.UserID == member.binding.UserID && candidate.generation == member.generation {
			e.sessions[key] = member
			break
		}
	}
	if e.logger != nil {
		e.logger.Printf("Scenario server ability-rank fixture user=%d game=%d object=%d noun=%d index=%d rank=%d", member.binding.UserID,
			member.binding.GameID, member.deployedObjectID, member.binding.Creatures[index].Noun, index, req.AbilityRank)
	}
	return scenario.HeroResetReceipt{GameID: member.binding.GameID, ObjectID: member.deployedObjectID,
		Noun: member.binding.Creatures[index].Noun, Index: index, HitPoint: member.deployedHitPoint(), ManaPoint: member.deployedManaPoint(),
		RecipientCount: recipientCount, IsAbilityRankSet: true, AbilityRank: req.AbilityRank}, nil
}
