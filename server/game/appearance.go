package game

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

// AppearanceStore resolves an available appearance independently of stat revisions.
type AppearanceStore interface {
	Version(context.Context, uint32, string) (uint32, error)
}

type BotAppearanceStore interface {
	GenerateBot(context.Context, uint32, uint32, uint32, uint64) (uint32, uint32, error)
}

func (e *GameplayJoin) resolveBotAppearances(ctx context.Context, binding *GameplayBinding) error {
	store, isAvailable := e.appearanceStore.(BotAppearanceStore)
	if !isAvailable {
		return nil
	}
	for index := range binding.Creatures {
		creature := &binding.Creatures[index]
		if creature.Noun == 0 {
			continue
		}
		seed := binding.RunSeed ^ binding.UserID ^ uint64(index+1)*0x9e3779b97f4a7c15
		id, version, err := store.GenerateBot(ctx, creature.ID, creature.AppearanceVersion, creature.Noun, seed)
		if err != nil {
			return fmt.Errorf("botAppearance[%d]: %w", index, err)
		}
		creature.ID, creature.AppearanceVersion = id, version
	}
	return nil
}

func (e *GameplayJoin) SetAppearanceStore(store AppearanceStore) {
	e.appearanceStore = store
}

func (e *GameplayJoin) resolveAppearances(
	ctx context.Context, binding *GameplayBinding, view sporenet.UserView,
) error {
	versions := make(map[uint32]uint32, len(view.Creatures))
	for _, creature := range view.Creatures {
		if creature == nil {
			continue
		}
		version := uint32(1)
		if e.appearanceStore != nil {
			var err error
			version, err = e.appearanceStore.Version(ctx, creature.ID, creature.ThumbImageURL)
			if err != nil {
				return fmt.Errorf("appearanceVersion[%d]: %w", creature.ID, err)
			}
		}
		versions[creature.ID] = version
	}
	for index := range binding.Creatures {
		binding.Creatures[index].AppearanceVersion = versions[binding.Creatures[index].ID]
	}
	for index := range binding.ActivatedCreatures {
		binding.ActivatedCreatures[index].AppearanceVersion = versions[binding.ActivatedCreatures[index].ID]
	}
	return nil
}
