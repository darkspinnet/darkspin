//go:build scenario && !windows

package main

import (
	"context"
	"errors"
	"sync"
)

func (e *debugClientManager) lockAbilityCast(ctx context.Context, clientID string) (*sync.Mutex, error) {
	return nil, errors.New("native active ability requires Windows")
}

func (e *debugClientManager) CastActive(ctx context.Context, clientID string, point controlWorldPoint, abilityID uint32) (any, error) {
	return nil, errors.New("native active ability requires Windows")
}
