// Package activity records observed gameplay activity without inferring past sessions.
package activity

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const Retention = 370 * 24 * time.Hour

var ErrInvalidRange = errors.New("invalid activity range")

type Player struct {
	UserID int64
	Level  uint32
}

type Observation struct {
	Queues       []QueueObservation
	Distribution *Distribution
	At           time.Time
	Players      []Player
}

type Sample struct {
	At                time.Time
	OnlineCount       float64
	IsOnlineUnknown   bool
	UniquePlayerCount int64
	PeakOnlineCount   int64
}

// DNASample counts gameplay reward earnings, independently of balance changes.
type DNASample struct {
	At     time.Time
	Earned uint64
}

// LevelSample is a daily observation of saved, nondeleted accounts.
// AccountCount zero means AverageLevel is unknown.
type LevelSample struct {
	At           time.Time
	ObservedAt   time.Time
	AverageLevel float64
	AccountCount int64
}

// ProgressionSample observes saved, nondeleted accounts' highest completed
// campaign indexes. Zero is valid progression; AccountCount zero means unknown.
type ProgressionSample struct {
	At                       time.Time
	ObservedAt               time.Time
	AverageProgression       float64
	HighestCompletedCampaign uint32
	AccountCount             int64
}

// PopulationSample observes saved nondeleted accounts on one UTC day.
type PopulationSample struct {
	At           time.Time
	AccountCount int64
}

// SignupSample counts successful registrations atomically recorded on one UTC day.
// Its anonymous total survives account deletion and permanent purging.
type SignupSample struct {
	At           time.Time
	AccountCount int64
}

// CreationSample inventories remaining accounts by their recorded creation day.
type CreationSample struct {
	At           time.Time
	AccountCount int64
}

type Coverage struct {
	StartedAt    time.Time
	LastSampleAt time.Time
	IsPartial    bool
}

type Summary struct {
	MissionStartedAt           time.Time
	MissionSamples             []MissionSample
	CampaignLevelStartedAt     time.Time
	CampaignLevels             []CampaignLevel
	QueueStartedAt             time.Time
	QueueSamples               []QueueSample
	DistributionStartedAt      time.Time
	DistributionSamples        []DistributionSample
	CampaignStartedAt          time.Time
	CampaignSamples            []CampaignSample
	DNAEarned                  uint64
	DNAStartedAt               time.Time
	DNASamples                 []DNASample
	IsDNAPartial               bool
	Range                      string
	Since                      time.Time
	Until                      time.Time
	NextRefreshAt              time.Time
	DailyObservedAt            time.Time
	ObservedUniquePlayers      int64
	PeakOnline                 int64
	AverageCrogenitorLevel     float64
	LevelAccountCount          int64
	LevelObservedAt            time.Time
	LevelSamples               []LevelSample
	AverageCampaignProgression float64
	HighestCompletedCampaign   uint32
	ProgressionAccountCount    int64
	ProgressionObservedAt      time.Time
	ProgressionSamples         []ProgressionSample
	SignupStartedAt            time.Time
	SignupSamples              []SignupSample
	FirstAdmissionStartedAt    time.Time
	FirstAdmissionSamples      []CreationSample
	PopulationObservedAt       time.Time
	PopulationSamples          []PopulationSample
	CreationSamples            []CreationSample
	Samples                    []Sample
	Coverage                   Coverage
}

// Window is a bounded observation query. Bucket controls chart aggregation only.
type Window struct {
	Since  time.Time
	Until  time.Time
	Bucket time.Duration
}

type Snapshot struct {
	MissionStartedAt           time.Time
	MissionSamples             []MissionSample
	CampaignLevelStartedAt     time.Time
	CampaignLevels             []CampaignLevel
	QueueStartedAt             time.Time
	QueueSamples               []QueueSample
	DistributionStartedAt      time.Time
	DistributionSamples        []DistributionSample
	CampaignStartedAt          time.Time
	CampaignSamples            []CampaignSample
	DNAEarned                  uint64
	DNAStartedAt               time.Time
	DNASamples                 []DNASample
	ObservedUniquePlayers      int64
	PeakOnline                 int64
	AverageCrogenitorLevel     float64
	LevelAccountCount          int64
	LevelObservedAt            time.Time
	LevelSamples               []LevelSample
	AverageCampaignProgression float64
	HighestCompletedCampaign   uint32
	ProgressionAccountCount    int64
	ProgressionObservedAt      time.Time
	ProgressionSamples         []ProgressionSample
	SignupStartedAt            time.Time
	SignupSamples              []SignupSample
	FirstAdmissionStartedAt    time.Time
	FirstAdmissionSamples      []CreationSample
	PopulationObservedAt       time.Time
	PopulationSamples          []PopulationSample
	CreationSamples            []CreationSample
	Samples                    []Sample
	StartedAt                  time.Time
	LastSampleAt               time.Time
	SampleCount                int64
}

type Store interface {
	Record(context.Context, Observation) error
	RecordPlayer(context.Context, time.Time, Player) error
	ReadDaily(context.Context, Window) (Snapshot, error)
	ReadHourly(context.Context, Window) (Snapshot, error)
}

type Service struct {
	store       Store
	dailyCache  snapshotCache
	hourlyCache snapshotCache
	userCache   userCache
}

func NewService(store Store) *Service { return &Service{store: store} }

func (e *Service) Record(ctx context.Context, req Observation) error {
	if ctx == nil || req.At.IsZero() {
		return errors.New("invalid activity observation")
	}
	playersByID := make(map[int64]Player, len(req.Players))
	for _, player := range req.Players {
		if player.UserID <= 0 {
			return errors.New("invalid activity player")
		}
		playersByID[player.UserID] = player
	}
	req.At = req.At.UTC()
	req.Players = make([]Player, 0, len(playersByID))
	for _, player := range playersByID {
		req.Players = append(req.Players, player)
	}
	if req.Distribution != nil {
		err := req.Distribution.validate(len(req.Players))
		if err != nil {
			return fmt.Errorf("recordDistribution: %w", err)
		}
	}
	err := validateQueues(req.Queues)
	if err != nil {
		return fmt.Errorf("recordQueues: %w", err)
	}
	err = e.store.Record(ctx, req)
	if err != nil {
		return fmt.Errorf("recordStore: %w", err)
	}
	e.dailyCache.recorded(req.At, 24*time.Hour)
	e.hourlyCache.recorded(req.At, time.Hour)
	e.userCache.recorded(req.At)
	return nil
}

// ObservePlayer captures short sessions that may finish between periodic samples.
func (e *Service) ObservePlayer(ctx context.Context, userID int64, level uint32) error {
	if ctx == nil || userID <= 0 {
		return errors.New("invalid activity player")
	}
	err := e.store.RecordPlayer(ctx, time.Now().UTC(), Player{UserID: userID, Level: level})
	if err != nil {
		return fmt.Errorf("playerStore: %w", err)
	}
	return nil
}

func (e *Service) Read(ctx context.Context, rangeCode string) (Summary, error) {
	if ctx == nil {
		return Summary{}, errors.New("nil context")
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
		return Summary{}, ErrInvalidRange
	}
	now := time.Now().UTC()
	daily, err := e.readSnapshot(ctx, &e.dailyCache, rangeCode, duration, bucket, 24*time.Hour, now)
	if err != nil {
		return Summary{}, fmt.Errorf("readDaily: %w", err)
	}
	hourly, err := e.readSnapshot(ctx, &e.hourlyCache, rangeCode, duration, bucket, time.Hour, time.Now().UTC())
	if err != nil {
		return Summary{}, fmt.Errorf("readHourly: %w", err)
	}
	snapshot := hourly.snapshot
	since, until := hourly.window.Since, hourly.window.Until
	nextRefreshAt := hourly.expiresAt
	if daily.expiresAt.Before(nextRefreshAt) {
		nextRefreshAt = daily.expiresAt
	}
	expectedCount := int64(until.Truncate(time.Hour).Sub(since) / time.Hour)
	return Summary{
		MissionStartedAt: daily.snapshot.MissionStartedAt,
		MissionSamples:   daily.snapshot.MissionSamples,
		QueueStartedAt:   snapshot.QueueStartedAt, QueueSamples: snapshot.QueueSamples,
		DistributionStartedAt: snapshot.DistributionStartedAt,
		DistributionSamples:   snapshot.DistributionSamples,
		DNAEarned:             snapshot.DNAEarned, DNAStartedAt: snapshot.DNAStartedAt,
		DNASamples:   snapshot.DNASamples,
		IsDNAPartial: snapshot.DNAStartedAt.IsZero() || snapshot.DNAStartedAt.After(since) || snapshot.SampleCount < expectedCount,
		Range:        rangeCode, Since: since, Until: until,
		NextRefreshAt: nextRefreshAt, DailyObservedAt: daily.window.Until,
		ObservedUniquePlayers: snapshot.ObservedUniquePlayers, PeakOnline: snapshot.PeakOnline,
		AverageCrogenitorLevel:     daily.snapshot.AverageCrogenitorLevel,
		LevelAccountCount:          daily.snapshot.LevelAccountCount,
		LevelObservedAt:            daily.snapshot.LevelObservedAt,
		LevelSamples:               daily.snapshot.LevelSamples,
		AverageCampaignProgression: daily.snapshot.AverageCampaignProgression,
		HighestCompletedCampaign:   daily.snapshot.HighestCompletedCampaign,
		ProgressionAccountCount:    daily.snapshot.ProgressionAccountCount,
		ProgressionObservedAt:      daily.snapshot.ProgressionObservedAt,
		ProgressionSamples:         daily.snapshot.ProgressionSamples,
		CampaignLevelStartedAt:     daily.snapshot.CampaignLevelStartedAt,
		CampaignLevels:             daily.snapshot.CampaignLevels,
		CampaignStartedAt:          daily.snapshot.CampaignStartedAt,
		CampaignSamples:            daily.snapshot.CampaignSamples,
		SignupStartedAt:            snapshot.SignupStartedAt, SignupSamples: snapshot.SignupSamples,
		FirstAdmissionStartedAt: snapshot.FirstAdmissionStartedAt, FirstAdmissionSamples: snapshot.FirstAdmissionSamples,
		PopulationObservedAt: daily.snapshot.PopulationObservedAt,
		PopulationSamples:    daily.snapshot.PopulationSamples,
		CreationSamples:      daily.snapshot.CreationSamples,
		Samples:              snapshot.Samples,
		Coverage: Coverage{StartedAt: snapshot.StartedAt, LastSampleAt: snapshot.LastSampleAt,
			IsPartial: snapshot.StartedAt.IsZero() || snapshot.StartedAt.After(since) || snapshot.SampleCount < expectedCount},
	}, nil
}
