package preview

import (
	"errors"
	"fmt"
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
	// Build 103 sub_527DD0 renders the six wire slots in their supplied order.
	// Keep the composed minion slots followed by the special slots: imported
	// ordinals belong to separate source pools, not to the selected roster.
	// Selection owns per-role duplicate rules; presentation must also preserve
	// repeated nouns instead of imposing uniqueness across both roles.
	nounNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		nounName := strings.TrimSpace(entry.NounName)
		if nounName == "" {
			continue
		}
		nounNames = append(nounNames, nounName)
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
