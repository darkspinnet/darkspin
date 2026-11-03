package local

import (
	"context"
	"fmt"
	"github.com/darkspinnet/darkspin/server/chat"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

type noReplaySetter struct{ userManager *sporenet.UserManager }

func NewNoReplaySetter(userManager *sporenet.UserManager) chat.NoReplaySetter {
	return noReplaySetter{userManager: userManager}
}
func (e noReplaySetter) SetNoReplay(ctx context.Context, req chat.NoReplayRequest) (bool, error) {
	isEnabled, err := e.userManager.SetNoReplay(ctx, req.Sender.ID, req.IsEnabled)
	if err != nil {
		return false, fmt.Errorf("sessionNoReplay: %w", err)
	}
	return isEnabled, nil
}

func (e noReplaySetter) IsNoReplayEnabled(id int64) bool {
	return e.userManager.UserByID(id).IsNoReplayEnabled()
}
