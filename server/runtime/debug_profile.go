//go:build scenario

package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/accountaccess"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

// DebugCredentialRevision reads current durable access for an already owned
// profile selected by the scenario host; it never changes admission policy.
func (e *Server) DebugCredentialRevision(ctx context.Context, userID int64, loginName string) (int64, error) {
	if ctx == nil || e == nil || e.panelMaintenance == nil || e.panelMaintenance.accessService == nil {
		return 0, errors.New("scenario credential reader unavailable")
	}
	access, err := e.panelMaintenance.accessService.Read(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("credentialAccess: %w", err)
	}
	if access.UserID != userID || !strings.EqualFold(access.LoginName, loginName) || access.IsBlocked() || access.Revision <= 0 {
		return 0, errors.New("scenario credential identity is not active")
	}
	return access.Revision, nil
}

// DebugCreateProfile creates an independent supplied development profile. It
// deliberately omits legacy solo scenario-map and Continue-shell registration.
func (e *Server) DebugCreateProfile(ctx context.Context, req sporenet.DebugProfileRequest) (sporenet.DebugProfile, error) {
	if e == nil || e.userManager == nil {
		return sporenet.DebugProfile{}, errors.New("debug profile service unavailable")
	}
	if req.IsAdmin && (e.panelMaintenance == nil || e.panelMaintenance.accessService == nil) {
		return sporenet.DebugProfile{}, errors.New("isolated administrator provisioning unavailable")
	}
	profile, err := e.userManager.CreateDebugProfile(ctx, req)
	if err != nil {
		return sporenet.DebugProfile{}, fmt.Errorf("debugProfile: %w", err)
	}
	if req.IsAdmin {
		// This identity was freshly created above. No caller-supplied target or
		// source profile is elevated, and launch credentials are issued later.
		change, statusErr := e.panelMaintenance.accessService.SetStatus(accountaccess.WithTrustedLocal(ctx), accountaccess.SetStatusRequest{
			UserID: profile.UserID, Status: 251, Reason: "isolated scenario profile seed",
		})
		if statusErr != nil {
			return sporenet.DebugProfile{}, fmt.Errorf("debugAdmin: %w", statusErr)
		}
		if change.After.UserID != profile.UserID || !change.After.IsAdmin() {
			return sporenet.DebugProfile{}, errors.New("isolated administrator seed was not committed")
		}
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
