//go:build scenario && windows

package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

func (e *debugClientManager) lockAbilityCast(ctx context.Context, clientID string) (*sync.Mutex, error) {
	e.mutex.Lock()
	client := e.clientsByID[clientID]
	e.mutex.Unlock()
	if client == nil {
		return nil, errors.New("ability cast client is not owned")
	}
	err := lockScenarioLiveness(ctx, &client.abilityCastMutex)
	if err != nil {
		return nil, fmt.Errorf("abilityCastLock: %w", err)
	}
	return &client.abilityCastMutex, nil
}

func (e *debugClientManager) CastActive(ctx context.Context, clientID string, point controlWorldPoint) (any, error) {
	result, err := e.requestWindow(ctx, clientID, "gameplay.cast_active", controlCommand{WorldPoint: &point})
	if err != nil {
		return result, fmt.Errorf("activeCast: %w", err)
	}
	return result, nil
}
