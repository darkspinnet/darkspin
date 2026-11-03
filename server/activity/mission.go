package activity

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// MissionStart records accepted campaign participation, independently of its outcome.
type MissionStart struct {
	UserID         int64
	ResultID       uint64
	At             time.Time
	CampaignNumber uint32
	LevelNumber    uint32
	Map            string
	Difficulty     uint32
}

// MissionSample keeps unresolved starts separate from failures or abandonment.
type MissionSample struct {
	At              time.Time
	StartedCount    int64
	CompletedCount  int64
	UnresolvedCount int64
}

type MissionStore interface {
	BeginMission(context.Context, MissionStart) error
}

func ValidateMissionStart(req MissionStart) error {
	if req.UserID <= 0 || req.ResultID == 0 || req.ResultID > math.MaxInt64 ||
		req.CampaignNumber == 0 || req.LevelNumber == 0 || req.LevelNumber > 4 ||
		req.At.IsZero() || req.At.Unix() <= 0 || len(req.Map) > 256 || strings.TrimSpace(req.Map) == "" {
		return errors.New("invalid mission start")
	}
	return nil
}

func (e *Service) BeginMission(ctx context.Context, req MissionStart) error {
	if ctx == nil {
		return errors.New("nil mission context")
	}
	err := ValidateMissionStart(req)
	if err != nil {
		return fmt.Errorf("missionStart: %w", err)
	}
	store, isSupported := e.store.(MissionStore)
	if !isSupported {
		return errors.New("mission activity unavailable")
	}
	req.At = req.At.UTC()
	err = store.BeginMission(ctx, req)
	if err != nil {
		return fmt.Errorf("missionStore: %w", err)
	}
	return nil
}
