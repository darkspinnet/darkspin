//go:build scenario && windows

package main

import (
	"context"
	"fmt"
)

func (e *debugClientManager) InspectArena(ctx context.Context, clientID string) (any, error) {
	result, err := e.requestWindow(ctx, clientID, "mission.arena_lobby", controlCommand{})
	if err != nil {
		return result, fmt.Errorf("arenaInspect: %w", err)
	}
	return result, nil
}

func (e *debugClientManager) AcceptArena(ctx context.Context, clientID string) (any, error) {
	result, err := e.requestWindow(ctx, clientID, "mission.accept_arena", controlCommand{})
	if err != nil {
		return result, fmt.Errorf("arenaAccept: %w", err)
	}
	return result, nil
}
