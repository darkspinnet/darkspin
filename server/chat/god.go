package chat

import (
	"context"
	"errors"
	"fmt"
)

var ErrGodModeUnavailable = errors.New("god mode unavailable")

type GodModeRequest struct {
	Sender Participant
	GameID uint32
}

type GodModeToggler interface {
	ToggleGodMode(context.Context, GodModeRequest) (bool, error)
}

func (e *Service) UseGodModeToggler(toggler GodModeToggler) {
	e.godModeToggler = toggler
}

func (e *Service) ToggleGodMode(ctx context.Context, req GodModeRequest) (bool, error) {
	if req.Sender.ID == 0 || req.Sender.Name == "" {
		return false, ErrSenderNotMember
	}
	if req.GameID == 0 || e.godModeToggler == nil {
		return false, ErrGodModeUnavailable
	}
	members, err := e.directory.Members(ctx, Target{Scope: ScopeGame, ID: uint64(req.GameID)})
	if err != nil {
		return false, fmt.Errorf("godAudience: %w", err)
	}
	for _, member := range members {
		if member.ID != req.Sender.ID {
			continue
		}
		isEnabled, err := e.godModeToggler.ToggleGodMode(ctx, req)
		if err != nil {
			return false, fmt.Errorf("godToggle: %w", err)
		}
		return isEnabled, nil
	}
	return false, ErrSenderNotMember
}
