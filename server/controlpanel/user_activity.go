package controlpanel

import (
	"context"
	"net/http"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
	"github.com/darkspinnet/darkspin/server/activity"
)

type UserActivityReader interface {
	ReadUser(context.Context, int64, string) (activity.UserSummary, error)
}

type MetricsReviewer interface {
	Authorize(context.Context, int64, int64, bool) (accountaccess.Access, error)
	RecordMetricsReview(context.Context, accountaccess.Actor, int64, string, string) error
}

type userActivityDayDTO struct {
	At          time.Time `json:"at"`
	ObservedAt  time.Time `json:"observed_at"`
	Level       uint32    `json:"level"`
	Progression uint32    `json:"progression"`
}

type userActivityPresenceDTO struct {
	At                time.Time `json:"at"`
	ObservedHourCount float64   `json:"observed_hour_count"`
}

type userActivityPVPDTO struct {
	At              time.Time `json:"at"`
	MatchCount      uint64    `json:"match_count"`
	PlaytimeSeconds uint64    `json:"playtime_seconds"`
}

type userActivityDTO struct {
	MissionStartedAt       time.Time                   `json:"mission_started_at"`
	MissionSamples         []activityMissionSampleDTO  `json:"mission_samples"`
	CampaignLevelStartedAt time.Time                   `json:"campaign_level_started_at"`
	CampaignLevels         []activityCampaignLevelDTO  `json:"campaign_levels"`
	CampaignStartedAt      time.Time                   `json:"campaign_started_at"`
	CampaignSamples        []activityCampaignSampleDTO `json:"campaign_samples"`
	UserID                 int64                       `json:"user_id"`
	Range                  string                      `json:"range"`
	ObservedAt             time.Time                   `json:"observed_at"`
	RangeStart             time.Time                   `json:"range_start"`
	NextRefreshAt          time.Time                   `json:"next_refresh_at"`
	DailyObservedAt        time.Time                   `json:"daily_observed_at"`
	CollectionStartedAt    time.Time                   `json:"collection_started_at"`
	IsPartial              bool                        `json:"is_partial"`
	DNAEarned              uint64                      `json:"dna_earned"`
	DNASamples             []activityDNASampleDTO      `json:"dna_samples"`
	ObservedHourCount      uint64                      `json:"observed_hour_count"`
	PresenceSamples        []userActivityPresenceDTO   `json:"presence_samples"`
	PVPSamples             []userActivityPVPDTO        `json:"pvp_samples"`
	Days                   []userActivityDayDTO        `json:"days"`
}

func isActivityRange(rangeCode string) bool {
	switch rangeCode {
	case "hour", "day", "week", "month", "year":
		return true
	default:
		return false
	}
}

func (e *Service) accountActivity(writer http.ResponseWriter, req *http.Request, entry session) {
	if e.config.UserActivityReader == nil {
		writeError(writer, http.StatusNotFound, "Account activity is unavailable.")
		return
	}
	command := struct {
		Range string `json:"range"`
	}{}
	err := decodeControlRequest(writer, req, &command)
	if err != nil || !isActivityRange(command.Range) {
		writeError(writer, http.StatusBadRequest, "Invalid activity request.")
		return
	}
	ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
	defer cancel()
	// The authenticated session is the only source of the account ID.
	summary, err := e.config.UserActivityReader.ReadUser(ctx, entry.accountID, command.Range)
	if err != nil || summary.UserID != entry.accountID {
		writeError(writer, http.StatusServiceUnavailable, "Account activity is unavailable.")
		return
	}
	writeJSON(writer, http.StatusOK, userActivityResponse(summary))
}

func (e *Service) reviewUserActivity(writer http.ResponseWriter, req *http.Request, actor accountaccess.Actor) {
	if e.config.UserActivityReader == nil || e.config.MetricsReviewer == nil {
		writeError(writer, http.StatusNotFound, "Account activity is unavailable.")
		return
	}
	command := struct {
		UserID int64  `json:"user_id"`
		Range  string `json:"range"`
		Reason string `json:"reason"`
	}{}
	err := decodeControlRequest(writer, req, &command)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "Invalid activity request.")
		return
	}
	err = accountaccess.ValidateMetricsReview(actor, command.UserID, command.Range, command.Reason)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "Invalid activity request.")
		return
	}
	ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
	defer cancel()
	access, err := e.config.MetricsReviewer.Authorize(ctx, actor.UserID, actor.Revision, true)
	if err != nil || access.UserID != actor.UserID {
		writeError(writer, http.StatusForbidden, "Administrator access is required.")
		return
	}
	target, err := e.config.AccessReader.Read(ctx, command.UserID)
	if err != nil || target.UserID != command.UserID {
		writeError(writer, http.StatusServiceUnavailable, "Account activity is unavailable.")
		return
	}
	summary, err := e.config.UserActivityReader.ReadUser(ctx, command.UserID, command.Range)
	if err != nil || summary.UserID != command.UserID {
		writeError(writer, http.StatusServiceUnavailable, "Account activity is unavailable.")
		return
	}
	// This happens for every HTTP read, including cached metric summaries. Never
	// deliver data until the completed review event has committed successfully.
	err = e.config.MetricsReviewer.RecordMetricsReview(ctx, actor, command.UserID, command.Range, command.Reason)
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "Account activity is unavailable.")
		return
	}
	writeJSON(writer, http.StatusOK, userActivityResponse(summary))
}

func userActivityResponse(summary activity.UserSummary) userActivityDTO {
	response := userActivityDTO{
		MissionStartedAt: summary.MissionStartedAt.UTC(), MissionSamples: missionSampleDTOs(summary.MissionSamples),
		CampaignLevelStartedAt: summary.CampaignLevelStartedAt.UTC(),
		CampaignLevels:         campaignLevelDTOs(summary.CampaignLevels),
		CampaignStartedAt:      summary.CampaignStartedAt.UTC(),
		CampaignSamples:        campaignSampleDTOs(summary.CampaignSamples),
		UserID:                 summary.UserID, Range: summary.Range, ObservedAt: summary.Until.UTC(),
		RangeStart: summary.Since.UTC(), NextRefreshAt: summary.NextRefreshAt.UTC(),
		DailyObservedAt: summary.DailyObservedAt.UTC(), CollectionStartedAt: summary.CollectionStartedAt.UTC(),
		IsPartial: summary.IsPartial, DNAEarned: summary.DNAEarned,
		DNASamples:        make([]activityDNASampleDTO, 0, len(summary.DNASamples)),
		PresenceSamples:   make([]userActivityPresenceDTO, 0, len(summary.PresenceSamples)),
		ObservedHourCount: summary.ObservedHourCount, Days: make([]userActivityDayDTO, 0, len(summary.Days)),
	}
	response.PVPSamples = make([]userActivityPVPDTO, 0, len(summary.PVPSamples))
	for _, sample := range summary.PVPSamples {
		response.PVPSamples = append(response.PVPSamples, userActivityPVPDTO{
			At: sample.At.UTC(), MatchCount: sample.MatchCount, PlaytimeSeconds: sample.PlaytimeSeconds,
		})
	}
	for _, sample := range summary.DNASamples {
		response.DNASamples = append(response.DNASamples, activityDNASampleDTO{At: sample.At.UTC(), Earned: sample.Earned})
	}
	for _, sample := range summary.PresenceSamples {
		response.PresenceSamples = append(response.PresenceSamples, userActivityPresenceDTO{
			At: sample.At.UTC(), ObservedHourCount: sample.OnlineCount,
		})
	}
	for _, day := range summary.Days {
		response.Days = append(response.Days, userActivityDayResponse(day))
	}
	return response
}

func userActivityDayResponse(day activity.UserDay) userActivityDayDTO {
	return userActivityDayDTO{
		At: day.At.UTC(), ObservedAt: day.ObservedAt.UTC(), Level: day.Level, Progression: day.Progression,
	}
}
