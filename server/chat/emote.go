package chat

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/emote"
)

var ErrEmoteUnavailable = errors.New("Mod emote unavailable")

type EmoteRequest struct {
	Sender Participant
	GameID uint32
	Name   string
}

type EmoteRequester interface {
	RequestEmote(context.Context, EmoteRequest) error
}

func (e *Service) UseEmoteRequester(emoteRequester EmoteRequester) {
	e.emoteRequester = emoteRequester
}

func (e *Service) Emote(ctx context.Context, req EmoteRequest) error {
	if req.Sender.ID == 0 || req.Sender.Name == "" {
		return ErrSenderNotMember
	}
	_, isKnown := emote.SporeState(req.Name)
	if !isKnown || req.GameID == 0 || e.emoteRequester == nil {
		return ErrEmoteUnavailable
	}
	members, err := e.directory.Members(ctx, Target{Scope: ScopeGame, ID: uint64(req.GameID)})
	if err != nil {
		return fmt.Errorf("emoteAudience: %w", err)
	}
	for _, member := range members {
		if member.ID != req.Sender.ID {
			continue
		}
		err = e.emoteRequester.RequestEmote(ctx, req)
		if err != nil {
			return fmt.Errorf("emoteRequest: %w", err)
		}
		return nil
	}
	return ErrSenderNotMember
}
