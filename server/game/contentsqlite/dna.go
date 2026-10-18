package contentsqlite

import (
	"context"
	"errors"
	"fmt"

	contentsqlite "github.com/darkspinnet/darkspin/content/sqlite"
	"github.com/darkspinnet/darkspin/server/game"
)

type dnaRewardStore interface {
	DNARewardTuning(context.Context) (contentsqlite.DNARewardTuning, error)
}

func loadDNADropTuning(ctx context.Context, store levelDirectorStore) (game.DNADropTuning, error) {
	dnaStore, isAvailable := store.(dnaRewardStore)
	if !isAvailable {
		return game.DNADropTuning{}, errors.New("DNA tuning source unavailable")
	}
	authored, err := dnaStore.DNARewardTuning(ctx)
	if err != nil {
		return game.DNADropTuning{}, fmt.Errorf("dnaRead: %w", err)
	}
	if authored.ContentSourceResourceID <= 0 || authored.Chance == nil {
		return game.DNADropTuning{}, errors.New("authored DNA chance missing")
	}
	// The native lookup defaults only the absent minimum-stage property to 1.
	tuning := game.DNADropTuning{Chance: *authored.Chance, MinimumStage: 1}
	if authored.MinimumStage != nil {
		tuning.MinimumStage = *authored.MinimumStage
	}
	err = tuning.Validate()
	if err != nil {
		return game.DNADropTuning{}, fmt.Errorf("dnaValidate: %w", err)
	}
	return tuning, nil
}
