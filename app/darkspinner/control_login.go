package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type RemoteControlLoginEntry struct {
	IPAddress   string `json:"ip_address"`
	FirstSeenAt string `json:"first_seen_at"`
	LastSeenAt  string `json:"last_seen_at"`
	LoginCount  uint64 `json:"login_count"`
}

type RemoteControlLoginHistory struct {
	UserID     int64                     `json:"user_id"`
	Range      string                    `json:"range"`
	RangeStart string                    `json:"range_start"`
	Until      string                    `json:"until"`
	Entries    []RemoteControlLoginEntry `json:"entries"`
	NextIP     string                    `json:"next_ip"`
}

func (e *App) GetUserControlLoginHistory(serverAddress, identity string, userID int64, rangeCode, reason, afterIP, until string) (RemoteControlLoginHistory, error) {
	if userID <= 0 || !isPersonalActivityRange(rangeCode) || strings.TrimSpace(reason) == "" {
		return RemoteControlLoginHistory{}, errors.New("user, range, and review reason are required")
	}
	var end time.Time
	if until != "" {
		parsed, err := time.Parse(time.RFC3339Nano, until)
		if err != nil {
			return RemoteControlLoginHistory{}, fmt.Errorf("loginUntil: %w", err)
		}
		end = parsed
	}
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	result := RemoteControlLoginHistory{}
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodPost, "/admin/users/login-history", map[string]any{
		"user_id": userID, "range": rangeCode, "reason": reason, "after_ip": afterIP, "until": end, "limit": 100,
	}, &result)
	if err != nil {
		return RemoteControlLoginHistory{}, fmt.Errorf("loginReview: %w", err)
	}
	if result.UserID != userID {
		return RemoteControlLoginHistory{}, errors.New("login history does not match the selected account")
	}
	// No local cache: every delivered page must have its own committed audit.
	return result, nil
}
