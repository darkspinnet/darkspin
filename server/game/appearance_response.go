package game

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game/appearancefs"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

// Native Arsenal loaders construct image filenames from the advertised
// revision. Older saves can have a newer stat revision than their saved PNG.
// Project the available appearance without rewriting the saved loadout.
func (e *API) appearanceCreature(ctx context.Context, creature *sporenet.Creature) (*sporenet.Creature, error) {
	if creature == nil {
		return nil, nil
	}
	store := appearancefs.Store{Root: e.storage}
	version, err := store.Version(ctx, creature.ID, creature.ThumbImageURL)
	if err != nil {
		return nil, fmt.Errorf("arsenalAppearance: %w", err)
	}
	presentation := *creature
	presentation.Version = version
	if version == 1 {
		// Use the existing noun-specific fallback URLs as well as its native
		// template revision, rather than retaining broken saved-image URLs.
		presentation.ThumbImageURL = ""
		presentation.LargeImageURL = ""
	}
	return &presentation, nil
}
