package main

import (
	"errors"
	"fmt"

	server "github.com/darkspinnet/darkspin/server/runtime"
)

func (e *App) GetServerRulesConfiguration() (server.ServerRulesConfiguration, error) {
	e.serverMu.Lock()
	defer e.serverMu.Unlock()
	e.mu.Lock()
	gameServer := e.serviceSet.gameServer
	isShuttingDown := e.isShuttingDown
	e.mu.Unlock()
	if gameServer == nil || isShuttingDown {
		return server.ServerRulesConfiguration{}, errors.New("local server is not available")
	}
	configuration, err := gameServer.ServerRulesConfiguration()
	if err != nil {
		return server.ServerRulesConfiguration{}, fmt.Errorf("rulesRead: %w", err)
	}
	return configuration, nil
}

func (e *App) SetServerRulesConfiguration(
	req server.ServerRulesConfiguration,
) (server.ServerRulesConfiguration, error) {
	e.serverMu.Lock()
	defer e.serverMu.Unlock()
	e.mu.Lock()
	gameServer := e.serviceSet.gameServer
	isShuttingDown := e.isShuttingDown
	ctx := e.ctx
	e.mu.Unlock()
	if gameServer == nil || isShuttingDown || ctx == nil {
		return server.ServerRulesConfiguration{}, errors.New("local server is not available")
	}
	configuration, err := gameServer.SetServerRulesConfiguration(ctx, req)
	if err != nil {
		return server.ServerRulesConfiguration{}, fmt.Errorf("rulesApply: %w", err)
	}
	return configuration, nil
}
