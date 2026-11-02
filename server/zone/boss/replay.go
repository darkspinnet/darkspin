package boss

import (
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
)

// NamedBossNounForDirector keeps first-clear occurrences on their named boss
// while replay destinations use the selected map's authored captain tier.
func NamedBossNounForDirector(
	director game.CampaignDirector, chainLevelIndex uint32,
) (string, bool) {
	rules := director.BossSpawnRules.Resolved()
	if !director.IsReplay || rules.ReplayLeaderSelection == "same_as_first_clear" {
		nounName, isFound := firstClearLeader(director, chainLevelIndex)
		if rules.LeaderSelection == "authored_planet_boss" {
			candidate, isCandidateFound := authoredPlanetLeader(director)
			if isCandidateFound {
				return candidate, true
			}
		}
		return nounName, isFound
	}
	if director.Level == "" || director.Difficulty == 0 || director.Difficulty > 72 {
		return "", false
	}
	nounName := ""
	for _, pool := range director.Pools {
		if !strings.EqualFold(pool.ConfigurationName, "levelConfig") ||
			!strings.EqualFold(pool.ConfigKind, "captain") {
			continue
		}
		for _, entry := range pool.Entries {
			if director.Difficulty < entry.MinimumDifficulty ||
				director.Difficulty > entry.MaximumDifficulty {
				continue
			}
			if nounName != "" || strings.TrimSpace(entry.NounName) == "" {
				return "", false
			}
			nounName = entry.NounName
		}
	}
	return nounName, nounName != ""
}

func firstClearLeader(director game.CampaignDirector, stage uint32) (string, bool) {
	if !director.IsReplay {
		return NamedBossNoun(stage, director.Level)
	}
	// A replay destination can differ from the stage's campaign destination.
	// Apply that map's first-clear catalog entry for the selected threat tier.
	rank := min((director.Difficulty-1)/24, uint32(2))
	for _, definition := range namedBossCatalog {
		if strings.EqualFold(definition.level, director.Level) {
			noun := definition.nounName[rank]
			return noun, noun != ""
		}
	}
	return "", false
}

func authoredPlanetLeader(director game.CampaignDirector) (string, bool) {
	expectedNouns := map[string]string{"zelems_4": "zelemboss", "verdanth_4": "verdanthboss", "nocturna_1": "shadowboss", "cryos_2": "cryosboss", "infinity_4": "citadelboss", "scaldron_4": "scaldronboss"}
	expected, isOccurrence := expectedNouns[strings.ToLower(director.Level)]
	if !isOccurrence {
		return "", false
	}
	nouns := make(map[string]string)
	for _, pool := range director.ExternalPools {
		if !strings.EqualFold(pool.ConfigurationName, director.PlanetConfigName) || !strings.EqualFold(pool.ConfigKind, "boss") {
			continue
		}
		for _, entry := range pool.Entries {
			if director.Difficulty < entry.MinimumDifficulty || director.Difficulty > entry.MaximumDifficulty {
				continue
			}
			name := strings.TrimSuffix(strings.ToLower(entry.NounName), ".noun")
			name = strings.TrimSuffix(strings.TrimSuffix(name, "_2"), "_3")
			if name != expected || !entry.NPCProfile.IsKnown || entry.NPCProfile.HitPoint <= 0 {
				continue
			}
			identity, isIdentityFound := namedBossIdentity(director, entry.NounName)
			if !isIdentityFound || !identity.IsKnown {
				continue
			}
			nouns[strings.ToLower(entry.NounName)] = entry.NounName
		}
	}
	if len(nouns) != 1 {
		return "", false
	}
	for _, noun := range nouns {
		return noun, true
	}
	return "", false
}

// LeaderPolicyResult identifies reconstructed policy and ambiguous/missing
// authored candidates in the existing mission diagnostics.
func LeaderPolicyResult(director game.CampaignDirector) string {
	rules := director.BossSpawnRules.Resolved()
	if director.IsReplay && rules.ReplayLeaderSelection == "eligible_captain" {
		return "darkspin:eligible_captain"
	}
	if rules.LeaderSelection != "authored_planet_boss" {
		return "darkspin:current_campaign"
	}
	noun, isFound := authoredPlanetLeader(director)
	if isFound {
		return "darkspin:authored_planet_boss:" + noun
	}
	return "darkspin:campaign_fallback:no_unique_matching_eligible_planet_boss"
}
