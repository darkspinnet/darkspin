package main

import (
	"fmt"
	"net/http"
)

type RemoteControlCapacity struct {
	WorldPlayerLimit uint32 `json:"world_player_limit"`
}

func (e *App) GetRemoteControlCapacity(serverAddress, identity string) (RemoteControlCapacity, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	result := RemoteControlCapacity{}
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodGet, "/admin/capacity", nil, &result)
	if err != nil {
		return RemoteControlCapacity{}, fmt.Errorf("capacityRead: %w", err)
	}
	return result, nil
}

func (e *App) SetRemoteControlCapacity(serverAddress, identity string, req RemoteControlCapacity) (RemoteControlCapacity, error) {
	e.controlPanelMu.Lock()
	defer e.controlPanelMu.Unlock()
	result := RemoteControlCapacity{}
	err := e.remoteControlActionLocked(serverAddress, identity, http.MethodPatch, "/admin/capacity", req, &result)
	if err != nil {
		return RemoteControlCapacity{}, fmt.Errorf("capacitySave: %w", err)
	}
	return result, nil
}
