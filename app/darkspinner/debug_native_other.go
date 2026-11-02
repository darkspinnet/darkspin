//go:build scenario && !windows

package main

import (
	"context"
	"errors"
)

func (e *debugClientManager) InspectGameplay(ctx context.Context, clientID string) (any, error) {
	return nil, errors.New("native gameplay observation requires Windows")
}

func (e *debugClientManager) ProjectWorld(ctx context.Context, clientID string, point controlWorldPoint) (any, error) {
	return nil, errors.New("native world projection requires Windows")
}

func (e *debugClientManager) ClickWorld(ctx context.Context, clientID string, point controlWorldPoint) (any, error) {
	return nil, errors.New("native world click requires Windows")
}

func (e *debugClientManager) HoldWorld(ctx context.Context, clientID string, point controlWorldPoint, holdMS uint32) (any, error) {
	return nil, errors.New("native world hold requires Windows")
}
