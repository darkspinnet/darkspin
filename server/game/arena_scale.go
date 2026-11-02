package game

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

// NormalizeUnrankedArenaBindings caps every participant's arena gear to the
// lowest functional item level represented in the accepted match rosters.
// It changes only detached gameplay bindings, never persisted inventory.
func (e *GameplayJoin) NormalizeUnrankedArenaBindings(
	req []GameplayBinding,
) ([]GameplayBinding, uint32, error) {
	if e == nil || e.userFinder == nil || e.partCatalog == nil || len(req) == 0 {
		return nil, 0, errors.New("arena normalization unavailable")
	}
	for _, binding := range req {
		if binding.Mode != ModeArena || binding.IsRanked {
			return append([]GameplayBinding(nil), req...), 0, nil
		}
	}
	minimumItemLevel := uint32(0)
	for _, binding := range req {
		if binding.IsServerBot {
			continue
		}
		for _, creature := range binding.Creatures {
			itemLevel := creature.MinimumFunctionalItemLevel
			if itemLevel == 0 {
				continue
			}
			if minimumItemLevel == 0 || itemLevel < minimumItemLevel {
				minimumItemLevel = itemLevel
			}
		}
	}
	if minimumItemLevel == 0 {
		return append([]GameplayBinding(nil), req...), 0, nil
	}

	normalizedBindings := append([]GameplayBinding(nil), req...)
	ownerBindings := make(map[uint64]GameplayBinding, len(req))
	for bindingIndex, binding := range req {
		if binding.IsServerBot {
			continue
		}
		normalizedBinding, err := e.normalizeArenaPlayerBinding(binding, minimumItemLevel)
		if err != nil {
			return nil, 0, fmt.Errorf("arenaPlayer[%d]: %w", bindingIndex, err)
		}
		normalizedBindings[bindingIndex] = normalizedBinding
		ownerBindings[binding.UserID] = normalizedBinding
	}
	for bindingIndex, binding := range req {
		if !binding.IsServerBot {
			continue
		}
		ownerBinding, isOwnerFound := ownerBindings[binding.BotOwnerUserID]
		if !isOwnerFound {
			return nil, 0, fmt.Errorf("arenaBot[%d]: owner binding unavailable", bindingIndex)
		}
		ownerBinding.UserID = binding.UserID
		ownerBinding.AvatarID = binding.AvatarID
		ownerBinding.Slot = binding.Slot
		ownerBinding.Team = binding.Team
		ownerBinding.PlayerMask = binding.PlayerMask
		ownerBinding.ParticipantCount = binding.ParticipantCount
		ownerBinding.IsServerBot = true
		ownerBinding.BotOwnerUserID = binding.BotOwnerUserID
		ownerBinding.ActivatedCreatures = nil
		for index := range ownerBinding.Creatures {
			hero := binding.Creatures[index]
			if hero.Noun == 0 {
				continue
			}
			ownerBinding.Creatures[index].ID = hero.ID
			ownerBinding.Creatures[index].AppearanceVersion = hero.AppearanceVersion
			ownerBinding.Creatures[index].Name = hero.Name
			ownerBinding.Creatures[index].Noun = hero.Noun
			ownerBinding.Creatures[index].PassiveAbility = hero.PassiveAbility
			ownerBinding.Creatures[index].ElementType = hero.ElementType
			ownerBinding.Creatures[index].ClassType = hero.ClassType
			ownerBinding.Creatures[index].IsModBound = hero.IsModBound
		}
		normalizedBindings[bindingIndex] = ownerBinding
	}
	return normalizedBindings, minimumItemLevel, nil
}

func (e *GameplayJoin) normalizeArenaPlayerBinding(
	binding GameplayBinding, itemLevel uint32,
) (GameplayBinding, error) {
	user := e.userFinder.UserByID(int64(binding.UserID))
	if user == nil {
		return GameplayBinding{}, fmt.Errorf("arenaUser: %w", ErrGameplayUserNotFound)
	}
	view := user.View()
	for partIndex := range view.Parts {
		part := &view.Parts[partIndex]
		if part.MarketStatus != sporenet.PartMarketOwned || part.IsFlair ||
			part.EquippedToCreatureID == 0 || uint32(part.Level) <= itemLevel {
			continue
		}
		part.Level = uint16(itemLevel)
	}
	partCatalog := e.partCatalog.forGameplayMode(ModeArena)
	creatures := selectedGameplayCreatures(view, partCatalog, binding.SquadID)
	for index := range creatures {
		for _, previous := range binding.Creatures {
			if creatures[index].ID == previous.ID {
				creatures[index].AppearanceVersion = previous.AppearanceVersion
				break
			}
		}
	}
	activatedCreatures := activatedGameplayCreatures(view, partCatalog)
	for index := range activatedCreatures {
		for _, previous := range binding.ActivatedCreatures {
			if activatedCreatures[index].ID == previous.ID {
				activatedCreatures[index].AppearanceVersion = previous.AppearanceVersion
				break
			}
		}
	}
	binding.Creatures = creatures
	binding.ActivatedCreatures = activatedCreatures
	return binding, nil
}
