package game

import (
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

type leaderboardPlayer struct {
	view sporenet.UserView
}

func leaderboardResponse(
	currentUser *sporenet.User, activeUsers []*sporenet.User, query url.Values,
) string {
	players := make([]leaderboardPlayer, 0, len(activeUsers))
	if strings.EqualFold(query.Get("varient"), "friends") {
		if currentUser != nil {
			players = append(players, leaderboardPlayer{view: currentUser.View()})
		}
	} else {
		for _, candidate := range activeUsers {
			if candidate != nil {
				players = append(players, leaderboardPlayer{view: candidate.View()})
			}
		}
	}
	sortLeaderboardPlayers(players, query.Get("name"))
	start := leaderboardNonNegativeInt(query.Get("start"), 0)
	count := leaderboardNonNegativeInt(query.Get("count"), 15)
	if count == 0 {
		count = 15
	}
	if start > len(players) {
		start = len(players)
	}
	end := min(start+count, len(players))
	players = players[start:end]
	isPVP := strings.HasPrefix(strings.ToLower(query.Get("name")), "pvp_")
	categoryNames := []string{
		"progression", "xp", "totalKills", "deaths", "killDeathRatio", "damageMax", "healingMax",
	}
	if isPVP {
		categoryNames = []string{"wins", "playerKills", "damageMax", "healingMax"}
	}
	categoryNodes := make([]string, 0, len(categoryNames))
	for _, name := range categoryNames {
		categoryNodes = append(categoryNodes, xmlText("category", name))
	}
	playerNodes := make([]string, 0, len(players))
	for _, entry := range players {
		view := entry.view
		statNode := leaderboardPVEStats(view)
		if isPVP {
			statNode = leaderboardPVPStats(view)
		}
		playerNodes = append(playerNodes, xmlNode("player",
			xmlText("id", strconv.FormatInt(view.Account.ID, 10)),
			xmlText("name", view.DisplayName), xmlNode("stats", statNode...),
		))
	}
	return xmlResponse(true,
		xmlNode("stats", categoryNodes...),
		xmlNode("players", playerNodes...),
		xmlText("count", strconv.Itoa(len(players))),
	)
}

func leaderboardPVEStats(view sporenet.UserView) []string {
	stats := view.Stats
	return []string{
		xmlText("progression", number(view.Account.ChainProgression)),
		xmlText("xp", number(view.Account.XP)),
		xmlText("totalKills", number(stats.PVETotalKill)),
		xmlText("deaths", number(stats.PVEDeath)),
		xmlText("killDeathRatio", profileRatio(stats.PVETotalKill, stats.PVEDeath)),
		xmlText("damageMax", profileWholeStatNumber(stats.PVEDamageMaximum)),
		xmlText("healingMax", profileWholeStatNumber(stats.PVEHealingMaximum)),
	}
}

func leaderboardPVPStats(view sporenet.UserView) []string {
	stats := view.Stats
	return []string{
		xmlText("wins", number(stats.PVPWin)),
		xmlText("playerKills", number(stats.PVPPlayerKill)),
		xmlText("damageMax", profileStatNumber(stats.PVPDamageMaximum)),
		xmlText("healingMax", profileStatNumber(stats.PVPHealingMaximum)),
	}
}

func sortLeaderboardPlayers(players []leaderboardPlayer, name string) {
	sort.SliceStable(players, func(leftIndex, rightIndex int) bool {
		left := leaderboardSortNumber(players[leftIndex].view, name)
		right := leaderboardSortNumber(players[rightIndex].view, name)
		if left == right {
			return strings.ToLower(players[leftIndex].view.DisplayName) <
				strings.ToLower(players[rightIndex].view.DisplayName)
		}
		return left > right
	})
}

func leaderboardSortNumber(view sporenet.UserView, name string) float64 {
	stats := view.Stats
	switch name {
	case "progression":
		return float64(view.Account.ChainProgression)
	case "kills":
		return float64(stats.PVETotalKill)
	case "ratio":
		return leaderboardRatio(stats.PVETotalKill, stats.PVEDeath)
	case "damage":
		return stats.PVEDamageMaximum
	case "healing":
		return stats.PVEHealingMaximum
	case "pvp_wins":
		return float64(stats.PVPWin)
	case "pvp_kills":
		return float64(stats.PVPPlayerKill)
	case "pvp_damage":
		return stats.PVPDamageMaximum
	case "pvp_healing":
		return stats.PVPHealingMaximum
	default:
		return float64(view.Account.XP)
	}
}

func leaderboardRatio(numerator uint64, denominator uint64) float64 {
	if denominator == 0 {
		return float64(numerator)
	}
	return float64(numerator) / float64(denominator)
}

func leaderboardNonNegativeInt(text string, fallback int) int {
	parsed, err := strconv.Atoi(text)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}
