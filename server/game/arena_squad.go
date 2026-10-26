package game

import (
	"fmt"
	"strings"
)

// SelectArenaSquad authorizes the deck selected by build 103's Arena lobby.
// sub_4039A0 and sub_401C70 use the same native roster vector as the campaign
// selector, so an owned campaign deck is also a valid Arena selection.
func (e *GameplayJoin) SelectArenaSquad(
	binding GameplayBinding, squadID uint32,
) (GameplayBinding, error) {
	if e == nil || e.userFinder == nil || binding.Mode != ModeArena || squadID == 0 {
		return GameplayBinding{}, ErrGameplaySquadInvalid
	}
	user := e.userFinder.UserByID(int64(binding.UserID))
	if user == nil {
		return GameplayBinding{}, fmt.Errorf("arenaUser: %w", ErrGameplayUserNotFound)
	}
	view := user.View()
	for _, squad := range view.Squads {
		if squad.ID != squadID {
			continue
		}
		isKnownCategory := squad.Category == "" || strings.EqualFold(squad.Category, "pve") ||
			strings.EqualFold(squad.Category, "pvp")
		if view.Account.UnlockPVPDecks == 0 || squad.IsLockedFor(view.Account) || !isKnownCategory {
			return GameplayBinding{}, ErrGameplaySquadInvalid
		}
		creatures := selectedGameplayCreatures(view, e.partCatalog.forGameplayMode(binding.Mode), squadID)
		for index, creature := range creatures {
			if creature.ID == 0 || creature.Noun == 0 {
				return GameplayBinding{}, ErrGameplaySquadInvalid
			}
			for _, previous := range creatures[:index] {
				if previous.ID == creature.ID {
					return GameplayBinding{}, ErrGameplaySquadInvalid
				}
			}
		}
		binding.SquadID = squadID
		binding.Creatures = creatures
		binding.ActivatedCreatures = activatedGameplayCreatures(view, e.partCatalog.forGameplayMode(binding.Mode))
		return binding, nil
	}
	return GameplayBinding{}, ErrGameplaySquadInvalid
}
