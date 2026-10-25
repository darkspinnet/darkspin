//go:build scenario

package tcp

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

func (e *SharedServer) DebugPVP(ctx context.Context, req scenario.PVPRequest, gameManager *game.Manager, userManager *sporenet.UserManager) (scenario.PVPResult, error) {
	if e == nil || e.blaze == nil {
		return scenario.PVPResult{}, scenario.ErrPVPSessionMissing
	}
	result, err := e.blaze.DebugPVP(ctx, req, gameManager, userManager)
	if err != nil {
		return result, fmt.Errorf("sharedPVP: %w", err)
	}
	return result, nil
}
