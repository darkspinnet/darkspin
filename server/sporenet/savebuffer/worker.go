package savebuffer

import (
	"context"
	"fmt"
	"sort"
	"time"
)

type candidate struct {
	userID     int64
	firstDirty time.Time
}

func (e *Repository) run(ctx context.Context) {
	defer close(e.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			e.drain()
			return
		}
		select {
		case <-ctx.Done():
			e.drain()
			return
		case <-ticker.C:
			e.flush(ctx, false)
		case <-e.wake:
			e.flush(ctx, false)
		}
	}
}

func (e snapshot) isDue(now time.Time, isClosing bool) bool {
	if isClosing {
		return true
	}
	if now.Before(e.retryAfter) {
		return false
	}
	return e.isReady || now.Sub(e.lastChange) >= idleDelay || now.Sub(e.firstDirty) >= maximumDelay
}

func (e *Repository) flush(parent context.Context, isClosing bool) {
	now := time.Now()
	e.mutex.Lock()
	candidates := make([]candidate, 0, len(e.snapshots))
	for userID, pending := range e.snapshots {
		if pending.isDue(now, isClosing) {
			candidates = append(candidates, candidate{userID: userID, firstDirty: pending.firstDirty})
		}
	}
	e.mutex.Unlock()
	if len(candidates) == 0 {
		return
	}
	sort.Slice(candidates, func(first, second int) bool {
		return candidates[first].firstDirty.Before(candidates[second].firstDirty)
	})
	ctx, cancel := context.WithTimeout(parent, writeTimeout)
	defer cancel()
	savedCount, failedCount := 0, 0
	var storageWait time.Duration
	var writeDuration time.Duration
	for _, current := range candidates {
		if ctx.Err() != nil {
			break
		}
		startedAt := time.Now()
		err := e.acquire(ctx)
		storageWait += time.Since(startedAt)
		if err != nil {
			break
		}
		writeStartedAt := time.Now()
		isSaved, err := e.save(ctx, current.userID, isClosing)
		writeDuration += time.Since(writeStartedAt)
		e.release()
		if err != nil {
			failedCount++
			if e.logger != nil {
				e.logger.Printf("Profile save retained for retry user=%d: %v", current.userID, err)
			}
			continue
		}
		if isSaved {
			savedCount++
		}
	}
	e.mutex.Lock()
	pendingCount := len(e.snapshots)
	e.mutex.Unlock()
	if e.logger != nil {
		e.logger.Printf(
			"Profile save buffer duration=%s storage_wait=%s write_duration=%s queued=%d saved=%d failed=%d pending=%d",
			time.Since(now), storageWait, writeDuration, len(candidates), savedCount, failedCount, pendingCount,
		)
	}
}

// save runs with the storage gate held, but never holds mutex during I/O.
func (e *Repository) save(ctx context.Context, userID int64, isClosing bool) (bool, error) {
	e.mutex.Lock()
	pending, isFound := e.snapshots[userID]
	if !isFound || !pending.isDue(time.Now(), isClosing) {
		e.mutex.Unlock()
		return false, nil
	}
	current := pending
	current.isReady = false
	e.snapshots[userID] = current
	e.mutex.Unlock()
	var err error
	if pending.record != nil {
		record := *pending.record
		record.Stats = pending.statSnapshot
		err = e.store.Save(ctx, record)
	} else {
		err = e.store.SavePlayerStats(ctx, userID, pending.statSnapshot)
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	current = e.snapshots[userID]
	if err != nil {
		current.retryAfter = time.Now().Add(retryDelay)
		current.isReady = true
		e.snapshots[userID] = current
		return false, fmt.Errorf("snapshotWrite: %w", err)
	}
	if current.revision == pending.revision {
		delete(e.snapshots, userID)
		return true, nil
	}
	// A newer snapshot arrived during I/O. Only retire the exact version saved.
	// If only stats changed, the next write can use the focused stat-row update.
	if current.profileRevision == pending.profileRevision {
		current.record = nil
	}
	current.firstDirty = time.Now()
	current.retryAfter = time.Time{}
	e.snapshots[userID] = current
	return true, nil
}

// Close is called after gameplay stops and active profiles enter the buffer.
// It cancels an outstanding write and gives the final drain five seconds.
func (e *Repository) Close() error {
	if e == nil {
		return nil
	}
	e.mutex.Lock()
	e.isClosed = true
	e.cancel()
	e.mutex.Unlock()
	<-e.done
	if e.closeErr != nil {
		return fmt.Errorf("bufferClose: %w", e.closeErr)
	}
	return nil
}

func (e *Repository) drain() {
	e.flush(context.Background(), true)
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if len(e.snapshots) != 0 {
		e.closeErr = fmt.Errorf("%d profile snapshots remain unsaved", len(e.snapshots))
	}
}
