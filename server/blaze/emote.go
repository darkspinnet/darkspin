package blaze

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/chat"
)

const modEmoteHelp = "Mod emotes: /emote happy | laugh | scared | swoon | roar | backflip | flex | yawn. Use a bound Spore creature in a mission."

func messagingEmote(ctx context.Context, messenger *chat.Service, req chat.EmoteRequest) (string, error) {
	req.Name = strings.ToLower(strings.TrimSpace(req.Name))
	if req.Name == "" || req.Name == "help" || req.Name == "list" {
		return modEmoteHelp, nil
	}
	err := messenger.Emote(ctx, req)
	if errors.Is(err, chat.ErrEmoteUnavailable) || errors.Is(err, chat.ErrSenderNotMember) {
		return modEmoteHelp, nil
	}
	if err != nil {
		return "", fmt.Errorf("emoteQueue: %w", err)
	}
	return "Requested " + req.Name + "; requires an idle, living bound Mod creature.", nil
}
