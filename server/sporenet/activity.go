package sporenet

import (
	"context"
	"log"
	"time"
)

// GameplayActivityObserver records only accepted game connections.
type GameplayActivityObserver interface {
	ObservePlayer(context.Context, int64, uint32) error
}

// SetGameplayActivityObserver installs optional observation during composition.
func (e *UserManager) SetGameplayActivityObserver(observer GameplayActivityObserver, logger *log.Logger) {
	e.mu.Lock()
	e.activityObserver = observer
	e.activityLogger = logger
	e.mu.Unlock()
}

// ActivityView reads only the scalar fields needed for aggregate observations.
func (e *User) ActivityView() (int64, uint32) {
	if e == nil {
		return 0, 0
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.Account.ID, e.Account.Level
}

// ObserveGameplayLogin is called by game transports only after acceptance.
// Recording is best effort and never changes the successful login outcome.
func (e *UserManager) ObserveGameplayLogin(ctx context.Context, user *User) {
	if e == nil || user == nil {
		return
	}
	e.mu.RLock()
	observer := e.activityObserver
	logger := e.activityLogger
	e.mu.RUnlock()
	if observer == nil {
		return
	}
	userID, level := user.ActivityView()
	observationCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err := observer.ObservePlayer(observationCtx, userID, level)
	if err != nil && logger != nil {
		logger.Printf("Gameplay activity observation failed: %v", err)
	}
}
