//go:build scenario && windows

package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Only this feature lock spans selection and its independent outcome wait.
// Manager, client lifecycle and bridge locks are not retained by this helper.
func (e *debugClientManager) lockHeroSelection(ctx context.Context, clientID string) (*sync.Mutex, error) {
	e.mutex.Lock()
	client := e.clientsByID[clientID]
	e.mutex.Unlock()
	if client == nil {
		return nil, errors.New("hero selection client is not owned")
	}
	err := lockScenarioLiveness(ctx, &client.heroSelectionMutex)
	if err != nil {
		return nil, fmt.Errorf("heroSelectionLock: %w", err)
	}
	return &client.heroSelectionMutex, nil
}

func (e *debugClientManager) SelectHero(ctx context.Context, clientID string, slot uint32) (any, error) {
	result, err := e.requestWindow(ctx, clientID, "gameplay.select_hero", controlCommand{HeroSlot: &slot})
	if err != nil {
		return result, fmt.Errorf("heroSelect: %w", err)
	}
	return result, nil
}
