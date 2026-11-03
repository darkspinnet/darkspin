package sporenet

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/loginhistory"
)

// GameplayLoginIPRecorder observes an accepted connection without changing login.
type GameplayLoginIPRecorder interface {
	Record(context.Context, loginhistory.RecordRequest) error
}

// AccountID reads the immutable identity without copying profile collections.
func (e *User) AccountID() int64 {
	if e == nil {
		return 0
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.Account.ID
}

// SetGameplayLoginIPRecorder installs dedicated-server history during composition.
func (e *UserManager) SetGameplayLoginIPRecorder(recorder GameplayLoginIPRecorder) {
	e.mu.Lock()
	e.loginIPRecorder = recorder
	e.mu.Unlock()
}

// ObserveGameplayLoginIP records only after a game transport accepts the account.
// Callers own connection deduplication and handle failures without rejecting login.
func (e *UserManager) ObserveGameplayLoginIP(ctx context.Context, user *User, ipAddress string) (bool, error) {
	if e == nil || user == nil {
		return false, nil
	}
	e.mu.RLock()
	recorder := e.loginIPRecorder
	e.mu.RUnlock()
	if recorder == nil {
		return false, nil
	}
	userID := user.AccountID()
	observationCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err := recorder.Record(observationCtx, loginhistory.RecordRequest{
		UserID: userID, IPAddress: ipAddress, At: time.Now().UTC(),
	})
	if err != nil {
		return false, fmt.Errorf("loginIPRecord: %w", err)
	}
	return true, nil
}
