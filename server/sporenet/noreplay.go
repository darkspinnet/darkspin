package sporenet

import (
	"context"
	"fmt"
)

// SetNoReplay changes only this login session, never the saved profile.
func (e *UserManager) SetNoReplay(ctx context.Context, id int64, isEnabled *bool) (bool, error) {
	err := ctx.Err()
	if err != nil {
		return false, fmt.Errorf("noReplayContext: %w", err)
	}
	user := e.UserByID(id)
	if user == nil {
		return false, fmt.Errorf("noReplayUser: %w", ErrUserNotFound)
	}
	user.mu.Lock()
	defer user.mu.Unlock()
	if isEnabled == nil {
		user.isNoReplay = !user.isNoReplay
	} else {
		user.isNoReplay = *isEnabled
	}
	return user.isNoReplay, nil
}
func (e *User) IsNoReplayEnabled() bool {
	if e == nil {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.isNoReplay
}
