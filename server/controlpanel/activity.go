package controlpanel

import (
	"context"
	"net/http"
	"time"

	"github.com/darkspinnet/darkspin/server/activity"
)

type ActivityReader interface {
	Read(context.Context, string) (activity.Summary, error)
}

type activitySampleDTO struct {
	At                time.Time `json:"at"`
	UniquePlayerCount int64     `json:"unique_player_count"`
	PeakOnlineCount   *int64    `json:"peak_online_count"`
	OnlineCount       *float64  `json:"online_count"`
}

type activityQueueSampleDTO struct {
	At                 time.Time `json:"at"`
	Kind               string    `json:"kind"`
	QueuedCount        float64   `json:"queued_count"`
	AverageWaitSeconds *float64  `json:"average_wait_seconds"`
	MaximumWaitSeconds *float64  `json:"maximum_wait_seconds"`
	ObservationCount   int64     `json:"observation_count"`
}

type activityDistributionSampleDTO struct {
	At               time.Time `json:"at"`
	PVECount         float64   `json:"pve_count"`
	PVPCount         float64   `json:"pvp_count"`
	LobbyCount       float64   `json:"lobby_count"`
	MatchmakingCount float64   `json:"matchmaking_count"`
	OtherCount       float64   `json:"other_count"`
	ObservationCount int64     `json:"observation_count"`
}

type activityLevelSampleDTO struct {
	At                     time.Time `json:"at"`
	ObservedAt             time.Time `json:"observed_at"`
	AverageCrogenitorLevel *float64  `json:"average_crogenitor_level"`
}

type activityDNASampleDTO struct {
	At     time.Time `json:"at"`
	Earned uint64    `json:"earned"`
}

type activityProgressionSampleDTO struct {
	At                         time.Time `json:"at"`
	ObservedAt                 time.Time `json:"observed_at"`
	AverageCampaignProgression *float64  `json:"average_campaign_progression"`
	HighestCompletedCampaign   *uint32   `json:"highest_completed_campaign"`
}

type activityAccountSampleDTO struct {
	At           time.Time `json:"at"`
	AccountCount int64     `json:"account_count"`
}

type activityCampaignSampleDTO struct {
	At              time.Time `json:"at"`
	CompletionCount int64     `json:"completion_count"`
	ReplayCount     int64     `json:"replay_count"`
}

type activityMissionSampleDTO struct {
	At              time.Time `json:"at"`
	StartedCount    int64     `json:"started_count"`
	CompletedCount  int64     `json:"completed_count"`
	UnresolvedCount int64     `json:"unresolved_count"`
}

func missionSampleDTOs(samples []activity.MissionSample) []activityMissionSampleDTO {
	responses := make([]activityMissionSampleDTO, 0, len(samples))
	for _, sample := range samples {
		responses = append(responses, activityMissionSampleDTO{At: sample.At.UTC(), StartedCount: sample.StartedCount,
			CompletedCount: sample.CompletedCount, UnresolvedCount: sample.UnresolvedCount})
	}
	return responses
}

type activityCampaignLevelDTO struct {
	CampaignNumber  uint32 `json:"campaign_number"`
	LevelNumber     uint32 `json:"level_number"`
	CompletionCount int64  `json:"completion_count"`
	RepeatCount     int64  `json:"repeat_count"`
}

func campaignLevelDTOs(levels []activity.CampaignLevel) []activityCampaignLevelDTO {
	responses := make([]activityCampaignLevelDTO, 0, len(levels))
	for _, level := range levels {
		responses = append(responses, activityCampaignLevelDTO{CampaignNumber: level.CampaignNumber, LevelNumber: level.LevelNumber,
			CompletionCount: level.CompletionCount, RepeatCount: level.RepeatCount})
	}
	return responses
}

func campaignSampleDTOs(samples []activity.CampaignSample) []activityCampaignSampleDTO {
	responses := make([]activityCampaignSampleDTO, 0, len(samples))
	for _, sample := range samples {
		responses = append(responses, activityCampaignSampleDTO{At: sample.At.UTC(), CompletionCount: sample.CompletionCount, ReplayCount: sample.RepeatCount})
	}
	return responses
}

type activityDTO struct {
	MissionStartedAt           time.Time                       `json:"mission_started_at"`
	MissionSamples             []activityMissionSampleDTO      `json:"mission_samples"`
	CampaignLevelStartedAt     time.Time                       `json:"campaign_level_started_at"`
	CampaignLevels             []activityCampaignLevelDTO      `json:"campaign_levels"`
	QueueStartedAt             time.Time                       `json:"queue_started_at"`
	QueueSamples               []activityQueueSampleDTO        `json:"queue_samples"`
	DistributionStartedAt      time.Time                       `json:"distribution_started_at"`
	DistributionSamples        []activityDistributionSampleDTO `json:"distribution_samples"`
	CampaignStartedAt          time.Time                       `json:"campaign_started_at"`
	CampaignSamples            []activityCampaignSampleDTO     `json:"campaign_samples"`
	SignupStartedAt            time.Time                       `json:"signup_started_at"`
	SignupSamples              []activityAccountSampleDTO      `json:"signup_samples"`
	FirstAdmissionStartedAt    time.Time                       `json:"first_admission_started_at"`
	FirstAdmissionSamples      []activityAccountSampleDTO      `json:"first_admission_samples"`
	PopulationObservedAt       time.Time                       `json:"population_observed_at"`
	PopulationSamples          []activityAccountSampleDTO      `json:"population_samples"`
	CreationSamples            []activityAccountSampleDTO      `json:"creation_samples"`
	DNAEarned                  uint64                          `json:"dna_earned"`
	DNACollectionStartedAt     time.Time                       `json:"dna_collection_started_at"`
	DNASamples                 []activityDNASampleDTO          `json:"dna_samples"`
	IsDNAPartial               bool                            `json:"is_dna_partial"`
	AverageCampaignProgression *float64                        `json:"average_campaign_progression"`
	HighestCompletedCampaign   *uint32                         `json:"highest_completed_campaign"`
	ProgressionObservedAt      time.Time                       `json:"progression_observed_at"`
	ProgressionSamples         []activityProgressionSampleDTO  `json:"progression_samples"`
	Range                      string                          `json:"range"`
	ObservedAt                 time.Time                       `json:"observed_at"`
	NextRefreshAt              time.Time                       `json:"next_refresh_at"`
	DailyObservedAt            time.Time                       `json:"daily_observed_at"`
	RangeStart                 time.Time                       `json:"range_start"`
	CollectionStartedAt        time.Time                       `json:"collection_started_at"`
	LastSampleAt               time.Time                       `json:"last_sample_at"`
	IsPartial                  bool                            `json:"is_partial"`
	UniquePlayerCount          int64                           `json:"unique_player_count"`
	PeakOnlineCount            int64                           `json:"peak_online_count"`
	AverageCrogenitorLevel     *float64                        `json:"average_crogenitor_level"`
	LevelObservedAt            time.Time                       `json:"level_observed_at"`
	LevelSamples               []activityLevelSampleDTO        `json:"level_samples"`
	BucketSeconds              int                             `json:"bucket_seconds"`
	Samples                    []activitySampleDTO             `json:"samples"`
}

func (e *Service) serverActivity(writer http.ResponseWriter, req *http.Request) {
	if e.config.ActivityReader == nil {
		writeError(writer, http.StatusNotFound, "Player activity is unavailable.")
		return
	}
	command := struct {
		Range string `json:"range"`
	}{}
	err := decodeControlRequest(writer, req, &command)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "Invalid activity request.")
		return
	}
	bucketSeconds := 0
	switch command.Range {
	case "hour":
		bucketSeconds = 3600
	case "day":
		bucketSeconds = 3600
	case "week":
		bucketSeconds = 3600
	case "month":
		bucketSeconds = 86400
	case "year":
		bucketSeconds = 86400
	default:
		writeError(writer, http.StatusBadRequest, "Choose hour, day, week, month, or year.")
		return
	}
	ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
	defer cancel()
	summary, err := e.config.ActivityReader.Read(ctx, command.Range)
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "Player activity is unavailable.")
		return
	}
	response := activityDTO{
		MissionStartedAt: summary.MissionStartedAt.UTC(), MissionSamples: missionSampleDTOs(summary.MissionSamples),
		CampaignLevelStartedAt:  summary.CampaignLevelStartedAt.UTC(),
		CampaignLevels:          campaignLevelDTOs(summary.CampaignLevels),
		QueueStartedAt:          summary.QueueStartedAt.UTC(),
		QueueSamples:            make([]activityQueueSampleDTO, 0, len(summary.QueueSamples)),
		DistributionStartedAt:   summary.DistributionStartedAt.UTC(),
		DistributionSamples:     make([]activityDistributionSampleDTO, 0, len(summary.DistributionSamples)),
		CampaignStartedAt:       summary.CampaignStartedAt.UTC(),
		CampaignSamples:         campaignSampleDTOs(summary.CampaignSamples),
		SignupStartedAt:         summary.SignupStartedAt.UTC(),
		SignupSamples:           make([]activityAccountSampleDTO, 0, len(summary.SignupSamples)),
		FirstAdmissionStartedAt: summary.FirstAdmissionStartedAt.UTC(),
		FirstAdmissionSamples:   make([]activityAccountSampleDTO, 0, len(summary.FirstAdmissionSamples)),
		PopulationObservedAt:    summary.PopulationObservedAt.UTC(),
		PopulationSamples:       make([]activityAccountSampleDTO, 0, len(summary.PopulationSamples)),
		CreationSamples:         make([]activityAccountSampleDTO, 0, len(summary.CreationSamples)),
		ProgressionObservedAt:   summary.ProgressionObservedAt.UTC(),
		ProgressionSamples:      make([]activityProgressionSampleDTO, 0, len(summary.ProgressionSamples)),
		DNAEarned:               summary.DNAEarned, DNACollectionStartedAt: summary.DNAStartedAt.UTC(),
		DNASamples: make([]activityDNASampleDTO, 0, len(summary.DNASamples)), IsDNAPartial: summary.IsDNAPartial,
		Range: summary.Range, ObservedAt: summary.Until.UTC(), RangeStart: summary.Since.UTC(),
		NextRefreshAt: summary.NextRefreshAt.UTC(), DailyObservedAt: summary.DailyObservedAt.UTC(),
		CollectionStartedAt: summary.Coverage.StartedAt.UTC(), LastSampleAt: summary.Coverage.LastSampleAt.UTC(),
		IsPartial: summary.Coverage.IsPartial, UniquePlayerCount: summary.ObservedUniquePlayers,
		PeakOnlineCount: summary.PeakOnline,
		BucketSeconds:   bucketSeconds,
		Samples:         make([]activitySampleDTO, 0, len(summary.Samples)),
		LevelObservedAt: summary.LevelObservedAt.UTC(),
		LevelSamples:    make([]activityLevelSampleDTO, 0, len(summary.LevelSamples)),
	}
	for _, sample := range summary.QueueSamples {
		observation := activityQueueSampleDTO{At: sample.At.UTC(), Kind: string(sample.Kind), QueuedCount: sample.QueuedCount, ObservationCount: sample.ObservationCount}
		if sample.QueuedCount > 0 {
			averageWait, maximumWait := sample.AverageWaitSeconds, sample.MaximumWaitSeconds
			observation.AverageWaitSeconds, observation.MaximumWaitSeconds = &averageWait, &maximumWait
		}
		response.QueueSamples = append(response.QueueSamples, observation)
	}
	for _, sample := range summary.SignupSamples {
		response.SignupSamples = append(response.SignupSamples, activityAccountSampleDTO{At: sample.At.UTC(), AccountCount: sample.AccountCount})
	}
	for _, sample := range summary.DistributionSamples {
		response.DistributionSamples = append(response.DistributionSamples, activityDistributionSampleDTO{
			At: sample.At.UTC(), PVECount: sample.PVECount, PVPCount: sample.PVPCount, LobbyCount: sample.LobbyCount,
			MatchmakingCount: sample.MatchmakingCount, OtherCount: sample.OtherCount, ObservationCount: sample.ObservationCount,
		})
	}
	for _, sample := range summary.FirstAdmissionSamples {
		response.FirstAdmissionSamples = append(response.FirstAdmissionSamples, activityAccountSampleDTO{At: sample.At.UTC(), AccountCount: sample.AccountCount})
	}
	for _, sample := range summary.PopulationSamples {
		response.PopulationSamples = append(response.PopulationSamples, activityAccountSampleDTO{At: sample.At.UTC(), AccountCount: sample.AccountCount})
	}
	for _, sample := range summary.CreationSamples {
		response.CreationSamples = append(response.CreationSamples, activityAccountSampleDTO{At: sample.At.UTC(), AccountCount: sample.AccountCount})
	}
	for _, sample := range summary.DNASamples {
		response.DNASamples = append(response.DNASamples, activityDNASampleDTO{At: sample.At.UTC(), Earned: sample.Earned})
	}
	if summary.LevelAccountCount > 0 {
		response.AverageCrogenitorLevel = &summary.AverageCrogenitorLevel
	}
	if summary.ProgressionAccountCount > 0 {
		response.AverageCampaignProgression = &summary.AverageCampaignProgression
		response.HighestCompletedCampaign = &summary.HighestCompletedCampaign
	}
	for _, sample := range summary.ProgressionSamples {
		progression := activityProgressionSampleDTO{At: sample.At.UTC(), ObservedAt: sample.ObservedAt.UTC()}
		if sample.AccountCount > 0 {
			average := sample.AverageProgression
			highest := sample.HighestCompletedCampaign
			progression.AverageCampaignProgression = &average
			progression.HighestCompletedCampaign = &highest
		}
		response.ProgressionSamples = append(response.ProgressionSamples, progression)
	}
	for _, sample := range summary.LevelSamples {
		level := activityLevelSampleDTO{At: sample.At.UTC(), ObservedAt: sample.ObservedAt.UTC()}
		if sample.AccountCount > 0 {
			average := sample.AverageLevel
			level.AverageCrogenitorLevel = &average
		}
		response.LevelSamples = append(response.LevelSamples, level)
	}
	for _, sample := range summary.Samples {
		observation := activitySampleDTO{At: sample.At.UTC(), UniquePlayerCount: sample.UniquePlayerCount}
		if !sample.IsOnlineUnknown {
			onlineCount, peakOnlineCount := sample.OnlineCount, sample.PeakOnlineCount
			observation.OnlineCount = &onlineCount
			observation.PeakOnlineCount = &peakOnlineCount
		}
		response.Samples = append(response.Samples, observation)
	}
	writeJSON(writer, http.StatusOK, response)
}
