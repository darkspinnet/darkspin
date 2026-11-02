package game

import (
	"context"
	"fmt"
	"time"
)

// waitGameplayReady retains an admitted member's initial hello while Blaze
// finalizes the host setup. Guest setup notifications can arrive first. This
// does not cross the independent mesh or gameplay start barriers.
func (e *Instance) waitGameplayReady(ctx context.Context) error {
	waitContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	e.mu.Lock()
	switch e.Info.State {
	case StatePreGame, StateInGame:
		e.mu.Unlock()
		return nil
	case StateNew, StateInitializing:
		if e.gameplayReady == nil {
			e.gameplayReady = make(chan struct{})
		}
	default:
		e.mu.Unlock()
		return fmt.Errorf("readyState: %w", ErrGameplayNotReady)
	}
	ready := e.gameplayReady
	e.mu.Unlock()
	select {
	case <-waitContext.Done():
		return fmt.Errorf("readyWait: %w", waitContext.Err())
	case <-ready:
	}
	err := waitContext.Err()
	if err != nil {
		return fmt.Errorf("readyWake: %w", err)
	}
	if !e.IsGameplayJoinable() {
		return fmt.Errorf("readyRetired: %w", ErrGameplayNotReady)
	}
	return nil
}

func (e *Instance) notifyGameplayReadyLocked() {
	if e.gameplayReady == nil {
		return
	}
	close(e.gameplayReady)
	e.gameplayReady = nil
}

func (e *Instance) retireGameplayWait() {
	e.mu.Lock()
	e.Info.State = StateDestructing
	e.notifyGameplayReadyLocked()
	e.mu.Unlock()
}
