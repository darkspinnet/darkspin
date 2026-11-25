package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type RemoteControlPersonalDay struct {
	At          string `json:"at"`
	ObservedAt  string `json:"observed_at"`
	Level       uint32 `json:"level"`
	Progression uint32 `json:"progression"`
}

type RemoteControlPersonalActivity struct {
	MissionStartedAt       string                          `json:"mission_started_at"`
	MissionSamples         []RemoteControlMissionSample    `json:"mission_samples"`
	CampaignLevelStartedAt string                          `json:"campaign_level_started_at"`
	CampaignLevels         []RemoteControlCampaignLevel    `json:"campaign_levels"`
	CampaignStartedAt      string                          `json:"campaign_started_at"`
	CampaignSamples        []RemoteControlCampaignSample   `json:"campaign_samples"`
	UserID                 int64                           `json:"user_id"`
	Range                  string                          `json:"range"`
	ObservedAt             string                          `json:"observed_at"`
	RangeStart             string                          `json:"range_start"`
	NextRefreshAt          string                          `json:"next_refresh_at"`
	DailyObservedAt        string                          `json:"daily_observed_at"`
	CollectionStartedAt    string                          `json:"collection_started_at"`
	IsPartial              bool                            `json:"is_partial"`
	DNAEarned              uint64                          `json:"dna_earned"`
	DNASamples             []RemoteControlDNASample        `json:"dna_samples"`
	ObservedHourCount      uint64                          `json:"observed_hour_count"`
	PresenceSamples        []RemoteControlPersonalPresence `json:"presence_samples"`
	PVPSamples             []RemoteControlPersonalPVP      `json:"pvp_samples"`
	Days                   []RemoteControlPersonalDay      `json:"days"`
}

type RemoteControlPersonalPVP struct {
	At              string `json:"at"`
	MatchCount      uint64 `json:"match_count"`
	PlaytimeSeconds uint64 `json:"playtime_seconds"`
}

type RemoteControlPersonalPresence struct {
	At                string  `json:"at"`
	ObservedHourCount float64 `json:"observed_hour_count"`
}

func (e *App) GetMyControlActivity(serverAddress, identity, rangeCode string) (RemoteControlPersonalActivity, error) {
	if !isPersonalActivityRange(rangeCode) {
		return RemoteControlPersonalActivity{}, errors.New("unsupported activity range")
	}
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	address, err := normalizeRemoteServerAddress(serverAddress)
	if err != nil {
		return RemoteControlPersonalActivity{}, fmt.Errorf("personalAddress: %w", err)
	}
	identity = strings.TrimSpace(identity)
	if e.controlPanel.token == "" || e.controlPanel.address != address || e.controlPanel.identity != identity {
		return RemoteControlPersonalActivity{}, errors.New("panel session does not match the selected account")
	}
	if !time.Now().Before(e.controlPanel.expiresAt) {
		e.controlPanel = remoteControlSession{}
		return RemoteControlPersonalActivity{}, errors.New("controlPanel:unauthorized")
	}
	e.selectControlActivityOwnerLocked(address, identity)
	if e.controlPanel.accountID <= 0 {
		return RemoteControlPersonalActivity{}, errors.New("personal activity account identity is unavailable")
	}
	if e.controlActivityCache.personalAccountID != e.controlPanel.accountID {
		e.controlActivityCache.personalAccountID = e.controlPanel.accountID
		e.controlActivityCache.personalRanges = make(map[string]remoteControlPersonalEntry)
	}
	cached, isCached := e.controlActivityCache.personalRanges[rangeCode]
	if isCached && time.Now().Before(cached.expiresAt) {
		return cached.activity, nil
	}
	delete(e.controlActivityCache.personalRanges, rangeCode)
	result := RemoteControlPersonalActivity{}
	err = e.remoteControlActionLocked(address, identity, http.MethodPost, "/account/activity", map[string]string{"range": rangeCode}, &result)
	if err != nil {
		return RemoteControlPersonalActivity{}, fmt.Errorf("personalRead: %w", err)
	}
	if result.UserID != e.controlPanel.accountID {
		return RemoteControlPersonalActivity{}, errors.New("personal activity does not match the authenticated account")
	}
	expiresAt, parseErr := time.Parse(time.RFC3339Nano, result.NextRefreshAt)
	if parseErr != nil {
		// Responses without a valid scheduled expiry remain uncached.
		return result, nil
	}
	now := time.Now()
	if expiresAt.After(now) && !expiresAt.After(now.Add(time.Hour)) {
		e.controlActivityCache.personalRanges[rangeCode] = remoteControlPersonalEntry{activity: result, expiresAt: expiresAt}
	}
	return result, nil
}

func (e *App) GetUserControlActivity(serverAddress, identity string, userID int64, rangeCode, reason string) (RemoteControlPersonalActivity, error) {
	if userID <= 0 || !isPersonalActivityRange(rangeCode) || strings.TrimSpace(reason) == "" {
		return RemoteControlPersonalActivity{}, errors.New("user, activity range, and review reason are required")
	}
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	result := RemoteControlPersonalActivity{}
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodPost, "/admin/users/activity",
		map[string]any{"user_id": userID, "range": rangeCode, "reason": reason}, &result)
	if err != nil {
		return RemoteControlPersonalActivity{}, fmt.Errorf("userActivityRead: %w", err)
	}
	if result.UserID != userID {
		return RemoteControlPersonalActivity{}, errors.New("user activity does not match the selected account")
	}
	// Admin reviews always reach the server for authorization and audit.
	return result, nil
}

func isPersonalActivityRange(rangeCode string) bool {
	switch rangeCode {
	case "hour", "day", "week", "month", "year":
		return true
	default:
		return false
	}
}
