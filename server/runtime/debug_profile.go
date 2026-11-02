//go:build scenario

package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

// DebugCreateProfile creates an independent supplied development profile. It
// deliberately omits legacy solo scenario-map and Continue-shell registration.
func (e *Server) DebugCreateProfile(ctx context.Context, req sporenet.DebugProfileRequest) (sporenet.DebugProfile, error) {
	if e == nil || e.userManager == nil {
		return sporenet.DebugProfile{}, errors.New("debug profile service unavailable")
	}
	profile, err := e.userManager.CreateDebugProfile(ctx, req)
	if err != nil {
		return sporenet.DebugProfile{}, fmt.Errorf("debugProfile: %w", err)
	}
	return profile, nil
}

// DebugLogoutProfile releases an owned audit account's authenticated slot after
// its client has stopped. It preserves the profile and normal reconnect rules.
func (e *Server) DebugLogoutProfile(ctx context.Context, userID int64) error {
	if e == nil || e.userManager == nil || ctx == nil || userID <= 0 {
		return errors.New("debug logout requires an owned profile and context")
	}
	user := e.userManager.UserByID(userID)
	if user == nil {
		return nil
	}
	err := e.userManager.Logout(ctx, user)
	if err != nil {
		return fmt.Errorf("profileLogout: %w", err)
	}
	return nil
}
