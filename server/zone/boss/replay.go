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
	if !director.IsReplay {
		return NamedBossNoun(chainLevelIndex, director.Level)
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
