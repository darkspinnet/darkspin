package boss

import (
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
)

// Static endings use the mission number; replay endings use a separate seeded
// forty-percent Destructor draw. The eligible variant follows the destination.
func campaignEndingLeader(director game.CampaignDirector, stage uint32) (string, bool) {
	if stage == 0 || stage > 72 || director.Difficulty == 0 || director.Difficulty > 72 {
		return "", false
	}
	if !isDestructorEnding(director, stage) {
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

func isDestructorEnding(director game.CampaignDirector, stage uint32) bool {
	if !director.IsReplay {
		return stage%4 == 0
	}
	// Recreate a private stream so preparation, admission and rejoin agree
	// without consuming map-layout, population or loot draws.
	seed := uint32(director.RunSeed) ^ uint32(director.RunSeed>>32) ^
		stage*0x9e3779b9 ^ util.HashID(strings.ToLower(director.Level)) ^ 0x424f5353
	return sim.NewSimulatorRandom(seed).Float64() < 0.4
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
