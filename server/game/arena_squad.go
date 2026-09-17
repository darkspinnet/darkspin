package game

import (
	"fmt"
	"strings"
)

// SelectArenaSquad replaces the provisional deck with an owned, unlocked PVP deck.
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
		if squad.IsLocked || squad.Slot == 0 || squad.Slot > view.Account.UnlockPVPDecks ||
			!strings.EqualFold(squad.Category, "pvp") {
			return GameplayBinding{}, ErrGameplaySquadInvalid
		}
		creatures := selectedGameplayCreatures(view, e.partCatalog, squadID)
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
		binding.ActivatedCreatures = activatedGameplayCreatures(view, e.partCatalog)
		return binding, nil
	}
	return GameplayBinding{}, ErrGameplaySquadInvalid
}
