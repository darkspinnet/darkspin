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
