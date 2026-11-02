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
		// Squad selection rebuilds equipment-derived stats, but the appearance
		// revisions were resolved independently when this player joined.
		for index := range creatures {
			for _, previous := range binding.ActivatedCreatures {
				if creatures[index].ID == previous.ID {
					creatures[index].AppearanceVersion = previous.AppearanceVersion
					break
				}
			}
		}
		binding.SquadID = squadID
		binding.Creatures = creatures
		activatedCreatures := activatedGameplayCreatures(view, e.partCatalog.forGameplayMode(binding.Mode))
		for index := range activatedCreatures {
			for _, previous := range binding.ActivatedCreatures {
				if activatedCreatures[index].ID == previous.ID {
					activatedCreatures[index].AppearanceVersion = previous.AppearanceVersion
					break
				}
			}
		}
		binding.ActivatedCreatures = activatedCreatures
		return binding, nil
	}
	return GameplayBinding{}, ErrGameplaySquadInvalid
}
