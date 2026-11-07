package boss

import (
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
)

// Campaign endings choose the encounter class from the mission number and the
// eligible variant from the destination planet, including randomized replays.
func campaignEndingLeader(director game.CampaignDirector, stage uint32) (string, bool) {
	if stage == 0 || stage > 72 || director.Difficulty == 0 || director.Difficulty > 72 {
		return "", false
	}
	if stage%4 != 0 {
		nounName, isFound := eligibleCaptain(director)
		return nounName, isFound && !IsFinalBossNoun(nounName)
	}
	family := ""
	switch strings.ToLower(director.PlanetConfigName) {
	case "zelem.levelconfig":
		family = "zelemboss"
	case "nocturna.levelconfig":
		family = "shadowboss"
	case "verdanth.levelconfig":
		family = "verdanthboss"
	case "cryos.levelconfig":
		family = "cryosboss"
	case "sentios.levelconfig":
		family = "citadelboss"
	case "scaldron.levelconfig":
		family = "scaldronboss"
	default:
		return "", false
	}
	return eligiblePlanetLeader(director, family)
}

func eligiblePlanetLeader(director game.CampaignDirector, family string) (string, bool) {
	nouns := make(map[string]string)
	for _, pool := range director.ExternalPools {
		if !strings.EqualFold(pool.ConfigurationName, director.PlanetConfigName) ||
			!strings.EqualFold(pool.ConfigKind, "boss") {
			continue
		}
		for _, entry := range pool.Entries {
			if director.Difficulty < entry.MinimumDifficulty || director.Difficulty > entry.MaximumDifficulty {
				continue
			}
			name := strings.TrimSuffix(strings.ToLower(entry.NounName), ".noun")
			name = strings.TrimSuffix(strings.TrimSuffix(name, "_2"), "_3")
			if name != family || !entry.NPCProfile.IsKnown || entry.NPCProfile.HitPoint <= 0 {
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
