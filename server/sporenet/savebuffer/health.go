package savebuffer

import "time"

// Health describes actual buffered profile persistence since this process began.
// A zero timestamp means no such outcome has been observed, not a successful save.
type Health struct {
	LastSuccessAt       time.Time
	LastFailureAt       time.Time
	PendingAccountCount int
	FailedAccountCount  int
}

func (e *Repository) Health() Health {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return Health{
		LastSuccessAt: e.lastSuccessAt, LastFailureAt: e.lastFailureAt,
		PendingAccountCount: len(e.snapshots), FailedAccountCount: len(e.failures),
	}
}

// Called under mutex only after the durable store returns. Recovery is tracked
// per account so another account's successful save cannot hide an unresolved error.
func (e *Repository) observeSaveLocked(userID int64, isSuccess bool) {
	now := time.Now().UTC()
	if isSuccess {
		e.lastSuccessAt = now
		delete(e.failures, userID)
		return
	}
	e.lastFailureAt = now
	if e.failures == nil {
		e.failures = make(map[int64]time.Time)
	}
	e.failures[userID] = now
}
