package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/matchmaking"
	server "github.com/darkspinnet/darkspin/server/runtime"
)

type MatchmakingConfiguration struct {
	CampaignMatchmakingBotSize         uint32 `json:"campaignMatchmakingBotSize"`
	IsCampaignBotSquadMirroringEnabled bool   `json:"isCampaignBotSquadMirroringEnabled"`
}

// GetMatchmakingConfiguration reads settings owned by the local server database.
func (e *App) GetMatchmakingConfiguration() (MatchmakingConfiguration, error) {
	e.serverMu.Lock()
	defer e.serverMu.Unlock()
	e.mu.Lock()
	gameServer := e.serviceSet.gameServer
	ctx := e.ctx
	isShuttingDown := e.isShuttingDown
	e.mu.Unlock()
	if isShuttingDown {
		return MatchmakingConfiguration{}, errors.New("Darkspinner is shutting down")
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	var settings matchmaking.Settings
	var err error
	if gameServer != nil {
		settings, err = gameServer.ReadMatchmakingConfiguration(ctx)
	} else {
		pathSet, pathErr := e.spinnerPaths()
		if pathErr != nil {
			return MatchmakingConfiguration{}, fmt.Errorf("matchmakingPath: %w", pathErr)
		}
		settings, err = server.ReadMatchmakingConfiguration(ctx, pathSet.configPath)
	}
	if err != nil {
		return MatchmakingConfiguration{}, fmt.Errorf("matchmakingRead: %w", err)
	}
	return matchmakingConfiguration(settings), nil
}

// SetMatchmakingConfiguration changes matchmaking without restarting the server.
func (e *App) SetMatchmakingConfiguration(req MatchmakingConfiguration) (MatchmakingConfiguration, error) {
	e.serverMu.Lock()
	defer e.serverMu.Unlock()
	e.mu.Lock()
	gameServer := e.serviceSet.gameServer
	ctx := e.ctx
	isShuttingDown := e.isShuttingDown
	e.mu.Unlock()
	if isShuttingDown {
		return MatchmakingConfiguration{}, errors.New("Darkspinner is shutting down")
	}
	if ctx == nil {
		ctx = context.TODO()
	}
	settings := matchmaking.Settings{
		CampaignMatchmakingBotSize:         req.CampaignMatchmakingBotSize,
		IsCampaignBotSquadMirroringEnabled: req.IsCampaignBotSquadMirroringEnabled,
	}
	var err error
	if gameServer != nil {
		err = gameServer.SetMatchmakingConfiguration(ctx, settings)
	} else {
		pathSet, pathErr := e.spinnerPaths()
		if pathErr != nil {
			return MatchmakingConfiguration{}, fmt.Errorf("matchmakingPath: %w", pathErr)
		}
		err = server.SetMatchmakingConfiguration(ctx, pathSet.configPath, settings)
	}
	if err != nil {
		return MatchmakingConfiguration{}, fmt.Errorf("matchmakingSave: %w", err)
	}
	return req, nil
}

func matchmakingConfiguration(settings matchmaking.Settings) MatchmakingConfiguration {
	return MatchmakingConfiguration{
		CampaignMatchmakingBotSize:         settings.CampaignMatchmakingBotSize,
		IsCampaignBotSquadMirroringEnabled: settings.IsCampaignBotSquadMirroringEnabled,
	}
}
