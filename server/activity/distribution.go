package activity

import (
	"errors"
	"time"
)

// Distribution assigns every observed authenticated player one primary state.
// It describes a point observation, never session duration or peak concurrency.
type Distribution struct {
	PVECount         int64
	PVPCount         int64
	LobbyCount       int64
	MatchmakingCount int64
	OtherCount       int64
}

func (e Distribution) validate(playerCount int) error {
	remaining := int64(playerCount)
	counts := []int64{e.PVECount, e.PVPCount, e.LobbyCount, e.MatchmakingCount, e.OtherCount}
	for _, count := range counts {
		if count < 0 || count > remaining {
			return errors.New("invalid player state distribution")
		}
		remaining -= count
	}
	if remaining != 0 {
		return errors.New("incomplete player state distribution")
	}
	return nil
}

// DistributionSample averages coherent hourly observations within its bucket.
// ObservationCount distinguishes recorded zeroes from missing observations.
type DistributionSample struct {
	At               time.Time
	PVECount         float64
	PVPCount         float64
	LobbyCount       float64
	MatchmakingCount float64
	OtherCount       float64
	ObservationCount int64
}
