//go:build scenario && !windows

package main

import (
	"context"
	"errors"
)

func (e *debugClientManager) InspectScreen(ctx context.Context, clientID string) (any, error) {
	return nil, errors.New("owned native screen observation requires Windows")
}

func (e *debugClientManager) SetScreen(ctx context.Context, clientID, screen string, ownerGeneration uint64) (any, error) {
	return nil, errors.New("owned native screen selection requires Windows")
}
