//go:build scenario && windows

package main

import (
	"context"
	"fmt"
)

func (e *debugClientManager) InspectScreen(ctx context.Context, clientID string) (any, error) {
	result, err := e.requestWindow(ctx, clientID, "ui.screen_state", controlCommand{})
	if err != nil {
		return result, fmt.Errorf("screenInspect: %w", err)
	}
	return result, nil
}

func (e *debugClientManager) SetScreen(ctx context.Context, clientID, screen string, ownerGeneration uint64) (any, error) {
	result, err := e.requestWindow(ctx, clientID, "ui.screen_set", controlCommand{Screen: screen, ExpectedOwnerGeneration: ownerGeneration})
	if err != nil {
		return result, fmt.Errorf("screenSet: %w", err)
	}
	return result, nil
}
