//go:build scenario && !windows

package main

import (
	"context"
	"errors"
)

func (e *debugClientManager) InspectArena(ctx context.Context, clientID string) (any, error) {
	return nil, errors.New("native arena controls require Windows")
}

func (e *debugClientManager) AcceptArena(ctx context.Context, clientID string) (any, error) {
	return nil, errors.New("native arena controls require Windows")
}
