//go:build scenario && windows

package main

import (
	"context"
	"fmt"
)

func (e *debugClientManager) InspectGameplay(ctx context.Context, clientID string) (any, error) {
	result, err := e.requestWindow(ctx, clientID, "gameplay.inspect", controlCommand{})
	if err != nil {
		return result, fmt.Errorf("gameplayInspect: %w", err)
	}
	return result, nil
}

func (e *debugClientManager) ProjectWorld(ctx context.Context, clientID string, point controlWorldPoint) (any, error) {
	result, err := e.requestWindow(ctx, clientID, "gameplay.project", controlCommand{WorldPoint: &point})
	if err != nil {
		return result, fmt.Errorf("worldProject: %w", err)
	}
	return result, nil
}

func (e *debugClientManager) ClickWorld(ctx context.Context, clientID string, point controlWorldPoint) (any, error) {
	result, err := e.requestWindow(ctx, clientID, "gameplay.click", controlCommand{WorldPoint: &point})
	if err != nil {
		return result, fmt.Errorf("worldClick: %w", err)
	}
	return result, nil
}

func (e *debugClientManager) HoldWorld(ctx context.Context, clientID string, point controlWorldPoint, holdMS uint32) (any, error) {
	result, err := e.requestWindow(ctx, clientID, "gameplay.hold", controlCommand{WorldPoint: &point, HoldMS: holdMS})
	if err != nil {
		return result, fmt.Errorf("worldHold: %w", err)
	}
	return result, nil
}
