//go:build scenario

package tcp

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/scenario"
)

func (e *SharedServer) DebugParty(ctx context.Context, req scenario.PartyRequest) (scenario.PartyResult, error) {
	if e == nil || e.blaze == nil {
		return scenario.PartyResult{}, scenario.ErrPartySessionMissing
	}
	result, err := e.blaze.DebugParty(ctx, req)
	if err != nil {
		return result, fmt.Errorf("sharedParty: %w", err)
	}
	return result, nil
}
