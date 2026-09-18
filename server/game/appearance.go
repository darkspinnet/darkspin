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
