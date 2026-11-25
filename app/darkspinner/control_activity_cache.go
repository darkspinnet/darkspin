package main

import (
	"slices"
	"time"
)

// The panel mutex owns this memory-only cache. Only the five supported ranges
// for one server/account are retained, including across panel close/reopen.
type remoteControlActivityCache struct {
	address           string
	identity          string
	ranges            map[string]remoteControlActivityEntry
	personalRanges    map[string]remoteControlPersonalEntry
	personalAccountID int64
}

type remoteControlPersonalEntry struct {
	activity  RemoteControlPersonalActivity
	expiresAt time.Time
}

type remoteControlActivityEntry struct {
	activity  RemoteControlActivity
	expiresAt time.Time
}

func (e *App) selectControlActivityOwnerLocked(address, identity string) {
	if e.controlActivityCache.address == address && e.controlActivityCache.identity == identity {
		return
	}
	e.controlActivityCache = remoteControlActivityCache{
		address: address, identity: identity,
		ranges:         make(map[string]remoteControlActivityEntry),
		personalRanges: make(map[string]remoteControlPersonalEntry),
	}
}

func (e *App) cacheControlActivityLocked(rangeCode string, activity RemoteControlActivity) {
	expiresAt, err := time.Parse(time.RFC3339Nano, activity.NextRefreshAt)
	if err != nil {
		// Older servers have no scheduled expiry and remain uncached.
		return
	}
	now := time.Now()
	if !expiresAt.After(now) || expiresAt.After(now.Add(time.Hour)) {
		// The earliest scheduled refresh is hourly. Reject stale or implausible
		// deadlines rather than retaining an invalid server response indefinitely.
		return
	}
	e.controlActivityCache.ranges[rangeCode] = remoteControlActivityEntry{
		activity: activity.copy(), expiresAt: expiresAt,
	}
}

func (e RemoteControlActivity) copy() RemoteControlActivity {
	e.MissionSamples = slices.Clone(e.MissionSamples)
	e.CampaignLevels = slices.Clone(e.CampaignLevels)
	e.QueueSamples = slices.Clone(e.QueueSamples)
	for index := range e.QueueSamples {
		sample := &e.QueueSamples[index]
		if sample.AverageWaitSeconds != nil {
			averageWait := *sample.AverageWaitSeconds
			sample.AverageWaitSeconds = &averageWait
		}
		if sample.MaximumWaitSeconds != nil {
			maximumWait := *sample.MaximumWaitSeconds
			sample.MaximumWaitSeconds = &maximumWait
		}
	}
	e.DistributionSamples = slices.Clone(e.DistributionSamples)
	e.CampaignSamples = slices.Clone(e.CampaignSamples)
	e.Samples = slices.Clone(e.Samples)
	for index := range e.Samples {
		sample := &e.Samples[index]
		if sample.OnlineCount != nil {
			count := *sample.OnlineCount
			sample.OnlineCount = &count
		}
		if sample.PeakOnlineCount != nil {
			count := *sample.PeakOnlineCount
			sample.PeakOnlineCount = &count
		}
	}
	e.PopulationSamples = slices.Clone(e.PopulationSamples)
	e.CreationSamples = slices.Clone(e.CreationSamples)
	e.SignupSamples = slices.Clone(e.SignupSamples)
	e.FirstAdmissionSamples = slices.Clone(e.FirstAdmissionSamples)
	e.LevelSamples = slices.Clone(e.LevelSamples)
	e.ProgressionSamples = slices.Clone(e.ProgressionSamples)
	e.DNASamples = slices.Clone(e.DNASamples)
	return e
}
