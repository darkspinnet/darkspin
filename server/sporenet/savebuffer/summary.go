package savebuffer

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

// LoadProfileSummary reads scalar overlays without cloning inventory snapshots.
func (e *Repository) LoadProfileSummary(ctx context.Context, userID int64) (sporenet.ProfileSummary, error) {
	err := checkContext(ctx)
	if err != nil {
		return sporenet.ProfileSummary{}, fmt.Errorf("summaryContext: %w", err)
	}
	if userID <= 0 {
		return sporenet.ProfileSummary{}, fmt.Errorf("summaryID: %w", sporenet.ErrUserNotFound)
	}
	summary, isCached := e.cachedSummary(userID)
	if isCached {
		return summary, nil
	}
	err = e.acquire(ctx)
	if err != nil {
		return sporenet.ProfileSummary{}, fmt.Errorf("summaryGate: %w", err)
	}
	defer e.release()
	summary, isCached = e.cachedSummary(userID)
	if isCached {
		return summary, nil
	}
	repository, isSupported := e.store.(sporenet.ProfileSummaryRepository)
	if !isSupported {
		return sporenet.ProfileSummary{}, errors.New("profile summary repository unavailable")
	}
	summary, err = repository.LoadProfileSummary(ctx, userID)
	if err != nil {
		return sporenet.ProfileSummary{}, fmt.Errorf("summaryRead: %w", err)
	}
	e.mutex.Lock()
	pending, isPending := e.snapshots[userID]
	if isPending {
		if pending.record != nil {
			summary = sporenet.SummaryFromRecord(*pending.record)
		}
		summary.Stats = pending.statSnapshot
	}
	e.mutex.Unlock()
	return summary, nil
}

func (e *Repository) cachedSummary(userID int64) (sporenet.ProfileSummary, bool) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	pending := e.snapshots[userID]
	if pending.record == nil {
		return sporenet.ProfileSummary{}, false
	}
	summary := sporenet.SummaryFromRecord(*pending.record)
	summary.Stats = pending.statSnapshot
	return summary, true
}
