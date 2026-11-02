package game

import (
	"math/rand/v2"
	"strings"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

// BotSquadChoice is a historical player name and three recognizable heroes.
// The source is notes/squad.md; ability annotations are omitted, while named
// Alpha/Beta/Gamma/Delta variants must match the authored template variant.
type BotSquadChoice struct {
	DisplayName string
	Heroes      [3]string
}

var botSquadChoices = [...]BotSquadChoice{
	{"atimzarr", [3]string{"Skar", "Viper", "Zrin"}},
	{"XxThaonexX", [3]string{"Arakna", "Krel", "Tork"}},
	{"newbie11", [3]string{"Blitz", "Sage", "Wraith"}},
	{"quasar23", [3]string{"Tork", "Maldri", "Zrin"}},
	{"quasar23", [3]string{"Sage", "Vex", "Zrin"}},
	{"Horrorman_01", [3]string{"Vex", "Tork", "Krel"}},
	{"Horrorman_01", [3]string{"Maldri", "Tork", "Blitz"}},
	{"JAYmaster6", [3]string{"Sage", "Seraph", "Magnos"}},
	{"quasar23", [3]string{"Meditron Alpha", "Skar Gamma", "Magnos Beta"}},
	{"Raven1975", [3]string{"Revenant", "Jinx", "Meditron"}},
	{"Raven1975", [3]string{"Sage", "Savage", "Arborus"}},
	{"DVDMaster68", [3]string{"Zrin", "Savage", "Meditron"}},
	{"Inquisitor_Laine", [3]string{"Sage", "Krel", "Jinx"}},
	{"CrogenitorSeims", [3]string{"Arakna Alpha", "Arborus Alpha", "Vex Alpha"}},
	{"Malmatik", [3]string{"Wraith", "Zrin", "Titan"}},
	{"TrookLue", [3]string{"Viper", "SRS-42", "Meditron"}},
	{"CyberSentry", [3]string{"Magnos", "Sage", "Viper"}},
	{"karmenera", [3]string{"Viper", "Krel", "Arakna"}},
	{"Duly_noted", [3]string{"Savage", "Meditron", "Andromeda"}},
	{"Megavore", [3]string{"Sage", "Titan", "Blitz"}},
	{"Alopecian", [3]string{"Magnos", "Savage", "Meditron"}},
	{"Duly_noted", [3]string{"Viper", "Magnos", "Char"}},
	{"NickBourgade1992", [3]string{"Andromeda", "Skar", "Arborus"}},
	{"CatalystKO", [3]string{"Lumin", "Krel", "Arakna"}},
	{"CatalystKO", [3]string{"Magnos", "Blitz", "Meditron"}},
	{"CatalystKO", [3]string{"Jinx", "Skar", "Zrin"}},
	{"Crow149", [3]string{"Vex Gamma", "Blitz Gamma", "Arakna Delta"}},
	{"iamawseome14", [3]string{"Magnos", "Revenant", "Viper"}},
}

func botHeroMatches(availableName, requestedName string) bool {
	requested := strings.Fields(strings.ToLower(requestedName))
	available := strings.Fields(strings.ToLower(availableName))
	if len(requested) == 0 || len(available) == 0 || requested[0] != available[0] {
		return false
	}
	if len(requested) == 1 {
		return true
	}
	return len(available) > 1 && requested[1] == available[1]
}

// AvailableBotSquads keeps complete trios whose templates can be unlocked at
// the highest real player's Crogenitor level; ownership is irrelevant.
func AvailableBotSquads(templates []*sporenet.TemplateCreature, level uint32) []BotSquadChoice {
	choices := make([]BotSquadChoice, 0, len(botSquadChoices))
	for _, choice := range botSquadChoices {
		isEligible := true
		for _, hero := range choice.Heroes {
			if len(botHeroTemplates(templates, hero, level)) == 0 {
				isEligible = false
				break
			}
		}
		if isEligible {
			choices = append(choices, choice)
		}
	}
	return choices
}

func botHeroTemplates(templates []*sporenet.TemplateCreature, name string, level uint32) []*sporenet.TemplateCreature {
	eligible := make([]*sporenet.TemplateCreature, 0, 4)
	for _, template := range templates {
		if template == nil || template.Noun == 0 || template.UnlockLevel == 0 ||
			template.UnlockLevel > level || !botHeroMatches(template.Name, name) {
			continue
		}
		eligible = append(eligible, template)
	}
	return eligible
}

// BotSquadTemplates resolves each requested hero to one eligible installed variant.
func BotSquadTemplates(templates []*sporenet.TemplateCreature, choice BotSquadChoice, level uint32) ([3]*sporenet.TemplateCreature, bool) {
	selected := [3]*sporenet.TemplateCreature{}
	for index, name := range choice.Heroes {
		eligible := botHeroTemplates(templates, name, level)
		if len(eligible) == 0 {
			return [3]*sporenet.TemplateCreature{}, false
		}
		selected[index] = eligible[rand.IntN(len(eligible))]
	}
	return selected, true
}
