package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type RemoteControlActivitySample struct {
	At                string   `json:"at"`
	UniquePlayerCount int64    `json:"unique_player_count"`
	PeakOnlineCount   *int64   `json:"peak_online_count"`
	OnlineCount       *float64 `json:"online_count"`
}

type RemoteControlQueueSample struct {
	At                 string   `json:"at"`
	Kind               string   `json:"kind"`
	QueuedCount        float64  `json:"queued_count"`
	AverageWaitSeconds *float64 `json:"average_wait_seconds"`
	MaximumWaitSeconds *float64 `json:"maximum_wait_seconds"`
	ObservationCount   int64    `json:"observation_count"`
}

type RemoteControlDistributionSample struct {
	At               string  `json:"at"`
	PVECount         float64 `json:"pve_count"`
	PVPCount         float64 `json:"pvp_count"`
	LobbyCount       float64 `json:"lobby_count"`
	MatchmakingCount float64 `json:"matchmaking_count"`
	OtherCount       float64 `json:"other_count"`
	ObservationCount int64   `json:"observation_count"`
}

type RemoteControlLevelSample struct {
	At                     string   `json:"at"`
	ObservedAt             string   `json:"observed_at"`
	AverageCrogenitorLevel *float64 `json:"average_crogenitor_level"`
}

type RemoteControlDNASample struct {
	At     string `json:"at"`
	Earned uint64 `json:"earned"`
}

type RemoteControlProgressionSample struct {
	At                         string   `json:"at"`
	ObservedAt                 string   `json:"observed_at"`
	AverageCampaignProgression *float64 `json:"average_campaign_progression"`
	HighestCompletedCampaign   *uint32  `json:"highest_completed_campaign"`
}

type RemoteControlAccountSample struct {
	At           string `json:"at"`
	AccountCount int64  `json:"account_count"`
}

type RemoteControlCampaignSample struct {
	At              string `json:"at"`
	CompletionCount int64  `json:"completion_count"`
	ReplayCount     int64  `json:"replay_count"`
}

type RemoteControlMissionSample struct {
	At              string `json:"at"`
	StartedCount    int64  `json:"started_count"`
	CompletedCount  int64  `json:"completed_count"`
	UnresolvedCount int64  `json:"unresolved_count"`
}

type RemoteControlCampaignLevel struct {
	CampaignNumber  uint32 `json:"campaign_number"`
	LevelNumber     uint32 `json:"level_number"`
	CompletionCount int64  `json:"completion_count"`
	RepeatCount     int64  `json:"repeat_count"`
}

type RemoteControlActivity struct {
	MissionStartedAt           string                            `json:"mission_started_at"`
	MissionSamples             []RemoteControlMissionSample      `json:"mission_samples"`
	CampaignLevelStartedAt     string                            `json:"campaign_level_started_at"`
	CampaignLevels             []RemoteControlCampaignLevel      `json:"campaign_levels"`
	QueueStartedAt             string                            `json:"queue_started_at"`
	QueueSamples               []RemoteControlQueueSample        `json:"queue_samples"`
	DistributionStartedAt      string                            `json:"distribution_started_at"`
	DistributionSamples        []RemoteControlDistributionSample `json:"distribution_samples"`
	CampaignStartedAt          string                            `json:"campaign_started_at"`
	CampaignSamples            []RemoteControlCampaignSample     `json:"campaign_samples"`
	SignupStartedAt            string                            `json:"signup_started_at"`
	SignupSamples              []RemoteControlAccountSample      `json:"signup_samples"`
	FirstAdmissionStartedAt    string                            `json:"first_admission_started_at"`
	FirstAdmissionSamples      []RemoteControlAccountSample      `json:"first_admission_samples"`
	PopulationObservedAt       string                            `json:"population_observed_at"`
	PopulationSamples          []RemoteControlAccountSample      `json:"population_samples"`
	CreationSamples            []RemoteControlAccountSample      `json:"creation_samples"`
	Range                      string                            `json:"range"`
	ObservedAt                 string                            `json:"observed_at"`
	DailyObservedAt            string                            `json:"daily_observed_at"`
	NextRefreshAt              string                            `json:"next_refresh_at"`
	RangeStart                 string                            `json:"range_start"`
	CollectionStartedAt        string                            `json:"collection_started_at"`
	LastSampleAt               string                            `json:"last_sample_at"`
	IsPartial                  bool                              `json:"is_partial"`
	UniquePlayerCount          int64                             `json:"unique_player_count"`
	PeakOnlineCount            int64                             `json:"peak_online_count"`
	AverageCrogenitorLevel     *float64                          `json:"average_crogenitor_level"`
	LevelObservedAt            string                            `json:"level_observed_at"`
	LevelSamples               []RemoteControlLevelSample        `json:"level_samples"`
	DNAEarned                  uint64                            `json:"dna_earned"`
	DNACollectionStartedAt     string                            `json:"dna_collection_started_at"`
	IsDNAPartial               bool                              `json:"is_dna_partial"`
	DNASamples                 []RemoteControlDNASample          `json:"dna_samples"`
	AverageCampaignProgression *float64                          `json:"average_campaign_progression"`
	HighestCompletedCampaign   *uint32                           `json:"highest_completed_campaign"`
	ProgressionObservedAt      string                            `json:"progression_observed_at"`
	ProgressionSamples         []RemoteControlProgressionSample  `json:"progression_samples"`
	BucketSeconds              int64                             `json:"bucket_seconds"`
	Samples                    []RemoteControlActivitySample     `json:"samples"`
}

func (e *App) GetRemoteControlActivity(serverAddress, identity, rangeCode string) (RemoteControlActivity, error) {
	switch rangeCode {
	case "hour", "day", "week", "month", "year":
	default:
		return RemoteControlActivity{}, errors.New("unsupported activity range")
	}
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	address, err := normalizeRemoteServerAddress(serverAddress)
	if err != nil {
		return RemoteControlActivity{}, fmt.Errorf("activityAddress: %w", err)
	}
	identity = strings.TrimSpace(identity)
	if e.controlPanel.token == "" || e.controlPanel.address != address || e.controlPanel.identity != identity {
		return RemoteControlActivity{}, errors.New("panel session does not match the selected account")
	}
	if !time.Now().Before(e.controlPanel.expiresAt) {
		e.controlPanel = remoteControlSession{}
		return RemoteControlActivity{}, errors.New("controlPanel:unauthorized")
	}
	e.selectControlActivityOwnerLocked(address, identity)
	cached, isCached := e.controlActivityCache.ranges[rangeCode]
	if isCached {
		if time.Now().Before(cached.expiresAt) {
			return cached.activity.copy(), nil
		}
		delete(e.controlActivityCache.ranges, rangeCode)
	}
	result := RemoteControlActivity{}
	err = e.remoteControlActionLocked(address, identity, http.MethodPost, "/server/activity",
		map[string]string{"range": rangeCode}, &result)
	if err != nil {
		return RemoteControlActivity{}, fmt.Errorf("activityRead: %w", err)
	}
	e.cacheControlActivityLocked(rangeCode, result)
	return result, nil
}
