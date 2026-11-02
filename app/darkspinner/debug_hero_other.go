//go:build scenario && !windows

package main

import (
	"context"
	"errors"
	"sync"
)

func (e *debugClientManager) lockHeroSelection(ctx context.Context, clientID string) (*sync.Mutex, error) {
	return nil, errors.New("native hero selection requires Windows")
}

func (e *debugClientManager) SelectHero(ctx context.Context, clientID string, slot uint32) (any, error) {
	return nil, errors.New("native hero selection requires Windows")
}
