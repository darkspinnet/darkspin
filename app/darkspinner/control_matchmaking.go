package main

import (
	"fmt"
	"net/http"
)

type RemoteControlMatchmaking struct {
	CampaignMatchmakingBotSize         uint32 `json:"campaign_matchmaking_bot_size"`
	IsCampaignBotSquadMirroringEnabled bool   `json:"is_campaign_bot_squad_mirroring_enabled"`
}

func (e *App) GetRemoteControlMatchmaking(serverAddress, identity string) (RemoteControlMatchmaking, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	result := RemoteControlMatchmaking{}
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodGet, "/admin/matchmaking", nil, &result)
	if err != nil {
		return RemoteControlMatchmaking{}, fmt.Errorf("matchmakingRead: %w", err)
	}
	return result, nil
}

func (e *App) SetRemoteControlMatchmaking(serverAddress, identity string, req RemoteControlMatchmaking) (RemoteControlMatchmaking, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	result := RemoteControlMatchmaking{}
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodPatch, "/admin/matchmaking", req, &result)
	if err != nil {
		return RemoteControlMatchmaking{}, fmt.Errorf("matchmakingSave: %w", err)
	}
	return result, nil
}
