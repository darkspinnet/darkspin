package activity

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

// UserDay holds the observed account level and progression for a UTC day.
type UserDay struct {
	At, ObservedAt     time.Time
	Level, Progression uint32
}

type UserSummary struct {
	MissionStartedAt                                                  time.Time
	MissionSamples                                                    []MissionSample
	CampaignLevelStartedAt                                            time.Time
	CampaignLevels                                                    []CampaignLevel
	CampaignStartedAt                                                 time.Time
	CampaignSamples                                                   []CampaignSample
	UserID                                                            int64
	Range                                                             string
	Since, Until, NextRefreshAt, DailyObservedAt, CollectionStartedAt time.Time
	DNAEarned                                                         uint64
	DNASamples                                                        []DNASample
	IsPartial                                                         bool
	Days                                                              []UserDay
	ObservedHourCount                                                 uint64
	PresenceSamples                                                   []Sample
	PVPSamples                                                        []PVPOutcomeSample
}

// UserStore is separate from the public aggregate metrics port.
type UserStore interface {
	ReadUserDaily(context.Context, int64, Window) (UserSummary, error)
	ReadUserHourly(context.Context, int64, Window) (UserSummary, error)
}

type userCacheKey struct {
	userID    int64
	rangeCode string
	cadence   time.Duration
}
type userCacheEntry struct {
	ready         chan struct{}
	isLoading     bool
	isInvalidated bool
	summary       UserSummary
	err           error
}
type userCache struct {
	mu                        sync.Mutex
	entries                   map[userCacheKey]*userCacheEntry
	lastHourlyAt, lastDailyAt time.Time
}

// reserve keeps at most 32 accounts (five ranges and two cadences each).
// An uncached read remains available when every retained account is loading.
func (e *userCache) reserve(userID int64) bool {
	userIDs := make(map[int64]struct{})
	loadingUserIDs := make(map[int64]struct{})
	for key, entry := range e.entries {
		userIDs[key.userID] = struct{}{}
		if entry.isLoading {
			loadingUserIDs[key.userID] = struct{}{}
		}
	}
	_, isPresent := userIDs[userID]
	if isPresent || len(userIDs) < 32 {
		return true
	}
	for cachedUserID := range userIDs {
		_, isLoading := loadingUserIDs[cachedUserID]
		if isLoading {
			continue
		}
		for key := range e.entries {
			if key.userID == cachedUserID {
				delete(e.entries, key)
			}
		}
		return true
	}
	return false
}

func (e *userCache) recorded(at time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	hour, day := at.UTC().Truncate(time.Hour), at.UTC().Truncate(24*time.Hour)
	isHourlyNew, isDailyNew := hour.After(e.lastHourlyAt), day.After(e.lastDailyAt)
	if isHourlyNew {
		e.lastHourlyAt = hour
	}
	if isDailyNew {
		e.lastDailyAt = day
	}
	for key, entry := range e.entries {
		if key.cadence == time.Hour && !isHourlyNew || key.cadence == 24*time.Hour && !isDailyNew {
			continue
		}
		entry.isInvalidated = true
		if !entry.isLoading {
			delete(e.entries, key)
		}
	}
}

// ReadUser must be called only after the adapter authorizes the target account.
func (e *Service) ReadUser(ctx context.Context, userID int64, rangeCode string) (UserSummary, error) {
	if ctx == nil || userID <= 0 {
		return UserSummary{}, errors.New("invalid activity user")
	}
	var duration, bucket time.Duration
	switch rangeCode {
	case "hour":
		duration, bucket = time.Hour, time.Hour
	case "day":
		duration, bucket = 24*time.Hour, time.Hour
	case "week":
		duration, bucket = 7*24*time.Hour, time.Hour
	case "month":
		duration, bucket = 30*24*time.Hour, 24*time.Hour
	case "year":
		duration, bucket = 365*24*time.Hour, 24*time.Hour
	default:
		return UserSummary{}, ErrInvalidRange
	}
	store, isSupported := e.store.(UserStore)
	if !isSupported {
		return UserSummary{}, errors.New("user activity unavailable")
	}
	hourly, err := e.readUserCached(ctx, store, userCacheKey{userID, rangeCode, time.Hour}, duration, bucket)
	if err != nil {
		return UserSummary{}, fmt.Errorf("userHourly: %w", err)
	}
	daily, err := e.readUserCached(ctx, store, userCacheKey{userID, rangeCode, 24 * time.Hour}, duration, bucket)
	if err != nil {
		return UserSummary{}, fmt.Errorf("userDaily: %w", err)
	}
	hourly.Days = daily.Days
	hourly.MissionStartedAt, hourly.MissionSamples = daily.MissionStartedAt, daily.MissionSamples
	hourly.PVPSamples = daily.PVPSamples
	hourly.CampaignLevelStartedAt, hourly.CampaignLevels = daily.CampaignLevelStartedAt, daily.CampaignLevels
	hourly.CampaignStartedAt, hourly.CampaignSamples = daily.CampaignStartedAt, daily.CampaignSamples
	hourly.DailyObservedAt = daily.Until
	if daily.NextRefreshAt.Before(hourly.NextRefreshAt) {
		hourly.NextRefreshAt = daily.NextRefreshAt
	}
	return hourly, nil
}

func (e *Service) readUserCached(ctx context.Context, store UserStore, key userCacheKey, duration, bucket time.Duration) (UserSummary, error) {
	contextErr := ctx.Err()
	if contextErr != nil {
		return UserSummary{}, fmt.Errorf("userCacheContext: %w", contextErr)
	}
	now := time.Now().UTC()
	e.userCache.mu.Lock()
	entry := e.userCache.entries[key]
	if entry != nil && (entry.isLoading || now.Before(entry.summary.NextRefreshAt)) {
		e.userCache.mu.Unlock()
		select {
		case <-ctx.Done():
			return UserSummary{}, fmt.Errorf("userWait: %w", ctx.Err())
		case <-entry.ready:
		}
		e.userCache.mu.Lock()
		isInvalidated, err, summary := entry.isInvalidated, entry.err, entry.summary.copy()
		e.userCache.mu.Unlock()
		if isInvalidated {
			return e.readUserCached(ctx, store, key, duration, bucket)
		}
		if err != nil {
			return UserSummary{}, fmt.Errorf("userShared: %w", err)
		}
		return summary, nil
	}
	if e.userCache.entries == nil {
		e.userCache.entries = make(map[userCacheKey]*userCacheEntry)
	}
	isCacheable := e.userCache.reserve(key.userID)
	entry = &userCacheEntry{ready: make(chan struct{}), isLoading: true}
	if isCacheable {
		e.userCache.entries[key] = entry
	}
	e.userCache.mu.Unlock()
	req := Window{Since: now.Add(-duration).Truncate(bucket), Until: now, Bucket: bucket}
	var summary UserSummary
	var err error
	if key.cadence == time.Hour {
		summary, err = store.ReadUserHourly(ctx, key.userID, req)
	} else {
		summary, err = store.ReadUserDaily(ctx, key.userID, req)
	}
	summary.UserID, summary.Range, summary.Since, summary.Until = key.userID, key.rangeCode, req.Since, req.Until
	summary.NextRefreshAt = now.Truncate(key.cadence).Add(key.cadence)
	e.userCache.mu.Lock()
	entry.summary, entry.err, entry.isLoading = summary.copy(), err, false
	if (err != nil || entry.isInvalidated) && isCacheable {
		delete(e.userCache.entries, key)
	}
	close(entry.ready)
	isInvalidated := entry.isInvalidated
	e.userCache.mu.Unlock()
	if isInvalidated {
		return e.readUserCached(ctx, store, key, duration, bucket)
	}
	if err != nil {
		return UserSummary{}, fmt.Errorf("userLoad: %w", err)
	}
	return summary, nil
}

func (e UserSummary) copy() UserSummary {
	e.MissionSamples = slices.Clone(e.MissionSamples)
	e.CampaignLevels = slices.Clone(e.CampaignLevels)
	e.CampaignSamples = slices.Clone(e.CampaignSamples)
	e.Days = slices.Clone(e.Days)
	e.DNASamples = slices.Clone(e.DNASamples)
	e.PresenceSamples = slices.Clone(e.PresenceSamples)
	e.PVPSamples = slices.Clone(e.PVPSamples)
	return e
}
