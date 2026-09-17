package difficulty

import (
	"errors"
	"fmt"
	"slices"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
)

// ProjectDirector applies the client-proven shared
// non-player difficulty, party-size, and Star Mode stages. Ordinary build-103
// play initializes the Star Mode increment to zero.
func ProjectDirector(
	director game.CampaignDirector, difficulty uint32, participantCount uint16,
	tuning sim.DifficultyCombatTuning,
) (game.CampaignDirector, error) {
	if difficulty == 0 {
		return game.CampaignDirector{}, errors.New("campaign difficulty zero")
	}
	if participantCount == 0 || participantCount > game.MaxGamePlayers {
		return game.CampaignDirector{}, fmt.Errorf(
			"campaign participant count: %d", participantCount,
		)
	}
	partyCoefficient := float32(participantCount - 1)
	director.Pools = slices.Clone(director.Pools)
	director.StandaloneBossEntries = slices.Clone(director.StandaloneBossEntries)
	profilesByNoun := make(
		map[string]game.CampaignNPCProfile, len(director.NPCProfilesByNoun),
	)
	for nounName, profile := range director.NPCProfilesByNoun {
		profilesByNoun[nounName] = profile
	}
	director.NPCProfilesByNoun = profilesByNoun
	for poolIndex := range director.Pools {
		director.Pools[poolIndex].Entries = slices.Clone(director.Pools[poolIndex].Entries)
	}
	for poolIndex := range director.Pools {
		for entryIndex := range director.Pools[poolIndex].Entries {
			profile := director.Pools[poolIndex].Entries[entryIndex].NPCProfile
			if !profile.IsKnown {
				continue
			}
			hitPoint, err := sim.ProjectNonPlayerHealthForParty(
				profile.HitPoint, difficulty, 0, partyCoefficient,
				profile.PlayerCountHealthScale, tuning,
			)
			if err != nil {
				return game.CampaignDirector{}, fmt.Errorf(
					"difficultyHealth[%d:%d]: %w", poolIndex, entryIndex, err,
				)
			}
			damageMultiplier, err := sim.ProjectNonPlayerDamageForParty(
				1, difficulty, 0, uint32(participantCount), tuning,
			)
			if err != nil {
				return game.CampaignDirector{}, fmt.Errorf(
					"difficultyDamage[%d:%d]: %w", poolIndex, entryIndex, err,
				)
			}
			profile.HitPoint = hitPoint
			profile.DifficultyDamageMultiplier = damageMultiplier
			director.Pools[poolIndex].Entries[entryIndex].NPCProfile = profile
		}
	}
	for entryIndex := range director.StandaloneBossEntries {
		profile := director.StandaloneBossEntries[entryIndex].NPCProfile
		if !profile.IsKnown {
			continue
		}
		hitPoint, err := sim.ProjectNonPlayerHealthForParty(
			profile.HitPoint, difficulty, 0, partyCoefficient,
			profile.PlayerCountHealthScale, tuning,
		)
		if err != nil {
			return game.CampaignDirector{}, fmt.Errorf(
				"difficultyBossHealth[%d]: %w", entryIndex, err,
			)
		}
		damageMultiplier, err := sim.ProjectNonPlayerDamageForParty(
			1, difficulty, 0, uint32(participantCount), tuning,
		)
		if err != nil {
			return game.CampaignDirector{}, fmt.Errorf(
				"difficultyBossDamage[%d]: %w", entryIndex, err,
			)
		}
		profile.HitPoint = hitPoint
		profile.DifficultyDamageMultiplier = damageMultiplier
		director.StandaloneBossEntries[entryIndex].NPCProfile = profile
	}
	for nounName, profile := range director.NPCProfilesByNoun {
		if !profile.IsKnown || profile.HitPoint <= 0 {
			continue
		}
		hitPoint, err := sim.ProjectNonPlayerHealthForParty(
			profile.HitPoint, difficulty, 0, partyCoefficient,
			profile.PlayerCountHealthScale, tuning,
		)
		if err != nil {
			return game.CampaignDirector{}, fmt.Errorf(
				"difficultyProfileHealth[%s]: %w", nounName, err,
			)
		}
		damageMultiplier, err := sim.ProjectNonPlayerDamageForParty(
			1, difficulty, 0, uint32(participantCount), tuning,
		)
		if err != nil {
			return game.CampaignDirector{}, fmt.Errorf(
				"difficultyProfileDamage[%s]: %w", nounName, err,
			)
		}
		profile.HitPoint = hitPoint
		profile.DifficultyDamageMultiplier = damageMultiplier
		director.NPCProfilesByNoun[nounName] = profile
	}
	return director, nil
}

func HasTuning(tuning sim.DifficultyCombatTuning) bool {
	return len(tuning.HealthMultiplier) != 0 && len(tuning.DamageMultiplier) != 0
}
