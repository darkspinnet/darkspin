//go:build scenario

package game

import (
	"errors"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

// ScenarioHero builds an unequipped installed template through the ordinary
// gameplay stat builder, without modifying the account or saved deck.
func ScenarioHero(template *sporenet.TemplateCreature, creatureID uint32) (GameplayCreature, error) {
	if template == nil || template.Noun == 0 || creatureID == 0 {
		return GameplayCreature{}, errors.New("scenario hero requires an installed template and creature identity")
	}
	creature := sporenet.NewCreature(template)
	creature.ID = creatureID
	return gameplayCreature(sporenet.UserView{}, creature, nil), nil
}
