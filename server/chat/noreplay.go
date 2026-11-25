package chat

import (
	"context"
	"errors"
	"fmt"
)

var ErrNoReplayUnavailable = errors.New("no replay unavailable")

type NoReplayRequest struct {
	Sender    Participant
	IsEnabled *bool
}
type NoReplaySetter interface {
	SetNoReplay(context.Context, NoReplayRequest) (bool, error)
	IsNoReplayEnabled(int64) bool
}

func (e *Service) UseNoReplaySetter(setter NoReplaySetter) { e.noReplaySetter = setter }
func (e *Service) SetNoReplay(ctx context.Context, req NoReplayRequest) (bool, error) {
	if req.Sender.ID == 0 || req.Sender.Name == "" {
		return false, ErrSenderNotMember
	}
	if e.noReplaySetter == nil {
		return false, ErrNoReplayUnavailable
	}
	isEnabled, err := e.noReplaySetter.SetNoReplay(ctx, req)
	if err != nil {
		return false, fmt.Errorf("noReplaySet: %w", err)
	}
	return isEnabled, nil
}
