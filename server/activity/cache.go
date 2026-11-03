package activity

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"
)

// Each cadence keeps at most one entry for each of the five supported ranges.
// An entry also owns its in-flight read, so clients share database work.
type snapshotCache struct {
	mu             sync.Mutex
	snapshots      map[string]*snapshotEntry
	lastRecordedAt time.Time
}

type snapshotEntry struct {
	ready         chan struct{}
	isLoading     bool
	isInvalidated bool
	snapshot      Snapshot
	window        Window
	expiresAt     time.Time
	err           error
}

func (e *snapshotCache) recorded(at time.Time, cadence time.Duration) {
	period := at.UTC().Truncate(cadence)
	e.mu.Lock()
	defer e.mu.Unlock()
	if !period.After(e.lastRecordedAt) {
		return
	}
	e.lastRecordedAt = period
	// The first successful collection in a cadence may supply observations that
	// an earlier metrics request could not yet observe.
	for rangeCode, entry := range e.snapshots {
		entry.isInvalidated = true
		if entry.isLoading {
			continue
		}
		delete(e.snapshots, rangeCode)
	}
}

func (e *Service) readSnapshot(ctx context.Context, cache *snapshotCache, rangeCode string,
	duration, bucket, cadence time.Duration, now time.Time) (snapshotEntry, error) {
	for {
		err := ctx.Err()
		if err != nil {
			return snapshotEntry{}, fmt.Errorf("cacheContext: %w", err)
		}
		cache.mu.Lock()
		entry := cache.snapshots[rangeCode]
		if entry != nil && (entry.isLoading || now.Before(entry.expiresAt)) {
			cache.mu.Unlock()
			select {
			case <-ctx.Done():
				return snapshotEntry{}, fmt.Errorf("cacheWait: %w", ctx.Err())
			case <-entry.ready:
			}
			cache.mu.Lock()
			if entry.isInvalidated {
				cache.mu.Unlock()
				now = time.Now().UTC()
				continue
			}
			result := entry.copy()
			err = entry.err
			cache.mu.Unlock()
			if err != nil {
				return snapshotEntry{}, fmt.Errorf("cacheRead: %w", err)
			}
			return result, nil
		}
		entry = &snapshotEntry{
			ready: make(chan struct{}), isLoading: true,
			window:    Window{Since: now.Add(-duration).Truncate(bucket), Until: now, Bucket: bucket},
			expiresAt: now.Truncate(cadence).Add(cadence),
		}
		if cache.snapshots == nil {
			cache.snapshots = make(map[string]*snapshotEntry, 5)
		}
		cache.snapshots[rangeCode] = entry
		cache.mu.Unlock()
		var snapshot Snapshot
		if cadence == time.Hour {
			snapshot, err = e.store.ReadHourly(ctx, entry.window)
		} else {
			snapshot, err = e.store.ReadDaily(ctx, entry.window)
		}
		cache.mu.Lock()
		entry.snapshot, entry.err = snapshot.copy(), err
		entry.isLoading = false
		if err != nil || entry.isInvalidated {
			delete(cache.snapshots, rangeCode)
		}
		close(entry.ready)
		isInvalidated := entry.isInvalidated
		result := entry.copy()
		cache.mu.Unlock()
		if isInvalidated {
			now = time.Now().UTC()
			continue
		}
		if err != nil {
			return snapshotEntry{}, fmt.Errorf("cacheLoad: %w", err)
		}
		return result, nil
	}
}

func (e *snapshotEntry) copy() snapshotEntry {
	return snapshotEntry{
		window: e.window, expiresAt: e.expiresAt,
		snapshot: e.snapshot.copy(),
	}
}

func (e Snapshot) copy() Snapshot {
	e.MissionSamples = slices.Clone(e.MissionSamples)
	e.CampaignLevels = slices.Clone(e.CampaignLevels)
	e.QueueSamples = slices.Clone(e.QueueSamples)
	e.DistributionSamples = slices.Clone(e.DistributionSamples)
	e.CampaignSamples = slices.Clone(e.CampaignSamples)
	e.SignupSamples = slices.Clone(e.SignupSamples)
	e.FirstAdmissionSamples = slices.Clone(e.FirstAdmissionSamples)
	e.PopulationSamples = slices.Clone(e.PopulationSamples)
	e.CreationSamples = slices.Clone(e.CreationSamples)
	e.Samples = slices.Clone(e.Samples)
	e.DNASamples = slices.Clone(e.DNASamples)
	e.LevelSamples = slices.Clone(e.LevelSamples)
	e.ProgressionSamples = slices.Clone(e.ProgressionSamples)
	return e
}
