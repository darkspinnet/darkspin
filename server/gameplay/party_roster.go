package gameplay

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
)

func (e campaignPreparation) bindPartyRoster(
	ctx context.Context, binding game.GameplayBinding,
) (game.GameplayBinding, error) {
	// Dark Spin enables this policy for ordinary chain runs. The recovered
	// client branch establishes how enabled mode chooses a roster, while the
	// original authoritative server's decision to enable it remains unproven.
	binding.IsFirstTimeDirectorEnabled = binding.Mode == game.ModeChain && !binding.IsWarped
	binding.PartyCompletedStages = nil
	if !binding.IsFirstTimeDirectorEnabled {
		return binding, nil
	}
	if e.gameplayJoin == nil {
		return game.GameplayBinding{}, errors.New("party roster gameplay unavailable")
	}
	completedStages, err := e.gameplayJoin.CampaignPartyCompletedStages(ctx, binding)
	if err != nil {
		return game.GameplayBinding{}, fmt.Errorf("partyProgress: %w", err)
	}
	binding.PartyCompletedStages = completedStages
	return binding, nil
}
