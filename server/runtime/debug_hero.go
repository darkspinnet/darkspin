//go:build scenario

package runtime

import (
	"context"
	"errors"
	"fmt"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/scenario"
)

type debugHeroResetProvider interface {
	ScenarioResetHero(context.Context, scenario.HeroResetRequest, game.GameplayCreature) (scenario.HeroResetReceipt, error)
}

func (e *Server) DebugResetHero(ctx context.Context, req scenario.HeroResetRequest) (scenario.HeroResetReceipt, error) {
	if e == nil || ctx == nil || e.template == nil {
		return scenario.HeroResetReceipt{}, errors.New("scenario hero service unavailable")
	}
	provider, isSupported := e.bugContext.(debugHeroResetProvider)
	if !isSupported {
		return scenario.HeroResetReceipt{}, errors.New("scenario hero provider unavailable")
	}
	var creature game.GameplayCreature
	if req.Noun != 0 {
		var err error
		creature, err = game.ScenarioHero(e.template.ByNoun(req.Noun), req.ObjectID)
		if err != nil {
			return scenario.HeroResetReceipt{}, fmt.Errorf("heroTemplate: %w", err)
		}
	}
	receipt, err := provider.ScenarioResetHero(ctx, req, creature)
	if err != nil {
		return receipt, fmt.Errorf("heroReset: %w", err)
	}
	return receipt, nil
}
