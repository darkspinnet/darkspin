package local

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/chat"
	"github.com/darkspinnet/darkspin/server/emote"
	"github.com/darkspinnet/darkspin/server/game"
)

type modEmoteRequester struct {
	gameManager *game.Manager
	isModded    bool
}

func NewModEmoteRequester(gameManager *game.Manager, isModded bool) chat.EmoteRequester {
	return modEmoteRequester{gameManager: gameManager, isModded: isModded}
}

func (e modEmoteRequester) RequestEmote(ctx context.Context, req chat.EmoteRequest) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("emoteContext: %w", err)
	}
	_, isKnown := emote.SporeState(req.Name)
	if !e.isModded || !isKnown || e.gameManager == nil {
		return chat.ErrEmoteUnavailable
	}
	instance := e.gameManager.Game(req.GameID)
	if instance == nil || !instance.RequestPlayerEventCommand(req.Sender.ID, game.PlayerEventCommand{
		Name: "spore-emote", Category: req.Name,
	}) {
		return chat.ErrEmoteUnavailable
	}
	return nil
}
