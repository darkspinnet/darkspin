package preview

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/util"
)

var initialNounNames = [6]string{
	"VerdanthBasicMelee.Noun",
	"ZelemBasicHybrid.Noun",
	"ZelemBasicPackMelee.Noun",
	"ZelemBasicRangedHoming.Noun",
	"ZelemBasicFlyingMelee.Noun",
	"NomadSnipe.Noun",
}

func Nouns(
	director game.CampaignDirector, level string, difficulty uint32,
) ([6]uint32, error) {
	if level == "" || difficulty == 0 {
		return [6]uint32{}, errors.New("campaign preview: invalid selection")
	}
	if strings.EqualFold(level, game.TutorialLevel) {
		return HashNouns(initialNounNames), nil
	}
	if !strings.EqualFold(director.Level, level) {
		return [6]uint32{}, fmt.Errorf("campaign preview: got level %q, want %q",
			director.Level, level)
	}
	entries := make([]game.CampaignDirectorEntry, 0)
	// Preview the same composed minion/special pools used by population.
	// Unselected first-time entries and captain equivalents are not additional
	// mission archetypes and must not displace the six selected enemy portraits.
	for _, poolKind := range []string{"minion", "special"} {
		poolEntries, err := director.EligibleEntries(poolKind, difficulty)
		if err != nil {
			return [6]uint32{}, fmt.Errorf("previewPool[%s]: %w", poolKind, err)
		}
		entries = append(entries, poolEntries...)
	}
	slices.SortFunc(entries, func(left game.CampaignDirectorEntry, right game.CampaignDirectorEntry) int {
		return left.Ordinal - right.Ordinal
	})
	nounNames := make([]string, 0, len(entries))
	seenNounNames := make(map[string]bool, len(entries))
	for _, entry := range entries {
		nounName := strings.TrimSpace(entry.NounName)
		normalizedNounName := strings.ToLower(nounName)
		if nounName == "" || seenNounNames[normalizedNounName] {
			continue
		}
		nounNames = append(nounNames, nounName)
		seenNounNames[normalizedNounName] = true
	}
	if len(nounNames) < 6 {
		return [6]uint32{}, fmt.Errorf("campaign preview: level %q has %d eligible nouns",
			level, len(nounNames))
	}
	selected := [6]string{}
	copy(selected[:], nounNames[:6])
	return HashNouns(selected), nil
}

func HashNouns(nounNames [6]string) [6]uint32 {
	nouns := [6]uint32{}
	for index, nounName := range nounNames {
		nouns[index] = util.HashID(nounName)
	}
	return nouns
}

func InitialNounNames() [6]string {
	return initialNounNames
}
