package boss

import (
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
)

// NamedBossNounForDirector selects the static or seeded replay ending and its
// eligible noun from the destination map and planet.
func NamedBossNounForDirector(
	director game.CampaignDirector, chainLevelIndex uint32,
) (string, bool) {
	return campaignEndingLeader(director, chainLevelIndex)
}

func eligibleCaptain(director game.CampaignDirector) (string, bool) {
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

// LeaderPolicyResult records the ending rule and selected destination noun.
func LeaderPolicyResult(director game.CampaignDirector) string {
	mode := "static_campaign"
	if director.IsReplay {
		mode = "replay"
	}
	role := "captain"
	if isDestructorEnding(director, director.Difficulty) {
		role = "planet_boss"
	}
	nounName, isFound := campaignEndingLeader(director, director.Difficulty)
	if !isFound {
		return "darkspin:" + mode + ":" + role + ":required_leader_unavailable"
	}
	return "darkspin:" + mode + ":" + role + ":" + nounName
}
