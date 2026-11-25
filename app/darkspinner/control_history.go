package main

import (
	"fmt"
	"net/http"
)

type RemoteControlHistoryFilter struct {
	BeforeEventID int64  `json:"before_event_id"`
	TargetUserID  int64  `json:"target_user_id"`
	ActorUserID   int64  `json:"actor_user_id"`
	ActorType     string `json:"actor_type"`
	AfterUnix     int64  `json:"after_unix"`
	BeforeUnix    int64  `json:"before_unix"`
	Action        string `json:"action"`
	Outcome       string `json:"outcome"`
	OperationID   string `json:"operation_id"`
}

func (e *App) FilterRemoteControlHistory(serverAddress, identity string, req RemoteControlHistoryFilter) (RemoteControlHistory, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	command := struct {
		RemoteControlHistoryFilter
		Limit int `json:"limit"`
	}{RemoteControlHistoryFilter: req, Limit: 25}
	result := RemoteControlHistory{}
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodPost, "/admin/history/list", command, &result)
	if err != nil {
		return RemoteControlHistory{}, fmt.Errorf("historyFilter: %w", err)
	}
	return result, nil
}
