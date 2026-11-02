package main

import (
	"errors"
	"fmt"
	"time"

	appwindow "github.com/darkspinnet/darkspin/window"
)

// SetLocalServicesRequired follows the selected Crogenitor. Selecting Remote
// defers shutdown until its launch so browsing never interrupts existing clients.
func (e *App) SetLocalServicesRequired(isRequired bool) error {
	e.mu.Lock()
	e.areLocalServicesRequired = isRequired || e.isHeadless
	e.mu.Unlock()
	err := e.syncLocalServices()
	if err != nil {
		return fmt.Errorf("localSync: %w", err)
	}
	return nil
}

func (e *App) syncLocalServices() error {
	e.serverMu.Lock()
	defer e.serverMu.Unlock()
	e.mu.Lock()
	isRequired := e.areLocalServicesRequired || e.isHeadless
	isContentReady := e.status.IsContentReady
	isShuttingDown := e.isShuttingDown
	gameServer := e.serviceSet.gameServer
	ctx := e.lifecycleCtx
	e.mu.Unlock()
	if isShuttingDown || gameServer != nil {
		return nil
	}
	if !isRequired {
		e.setSubsystem("Auth", "Not needed for this Crogenitor", false)
		e.setSubsystem("Server", "Not needed for this Crogenitor", false)
		return nil
	}
	if !isContentReady || ctx == nil {
		return nil
	}
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("localContext: %w", err)
	}
	paths, err := e.spinnerPaths()
	if err != nil {
		return fmt.Errorf("localPaths: %w", err)
	}
	err = e.startGameServer(ctx, paths)
	if err != nil {
		e.mu.Lock()
		failedServer := e.serviceSet.gameServer
		serverDone := e.serviceSet.serverDone
		e.mu.Unlock()
		if failedServer != nil {
			failedServer.Close()
			e.waitForShutdown("Failed local server", serverDone)
		}
		e.mu.Lock()
		e.serviceSet.gameServer = nil
		e.serviceSet.serverDone = nil
		e.mu.Unlock()
		e.setCriticalFailure("Server", err)
		return fmt.Errorf("localStart: %w", err)
	}
	e.setSubsystem("Auth", "Online", true)
	return nil
}

// Keep local services whenever any client or launch may still depend on them.
func (e *App) stopIdleLocalServices() error {
	e.serverMu.Lock()
	defer e.serverMu.Unlock()
	e.mu.Lock()
	isRequired := e.areLocalServicesRequired || e.isHeadless
	isLaunching := e.cancel != nil || e.gameDone != nil
	gameServer := e.serviceSet.gameServer
	serverDone := e.serviceSet.serverDone
	e.mu.Unlock()
	if isRequired || isLaunching || gameServer == nil || e.clientSession.HasOpenClient() || gameServer.HasActiveProfiles() {
		return nil
	}
	isRunning, err := appwindow.IsRunning(gameProcessName)
	if err != nil {
		return fmt.Errorf("localProcess: %w", err)
	}
	if isRunning {
		return nil
	}
	e.mu.Lock()
	isRequired = e.areLocalServicesRequired || e.isHeadless
	e.mu.Unlock()
	if isRequired {
		return nil
	}
	gameServer.Close()
	if serverDone != nil {
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-serverDone:
		case <-timer.C:
			return errors.New("local server did not finish shutting down")
		}
	}
	e.mu.Lock()
	e.serviceSet.gameServer = nil
	e.serviceSet.serverDone = nil
	if e.authURL == e.embeddedAuthURL {
		e.authURL = authServiceURL
	}
	e.embeddedAuthURL = ""
	e.token = ""
	e.status.IsAuthenticated = false
	e.status.AuthError = ""
	e.status.ServerError = ""
	e.status.Auth = "Not needed for this Crogenitor"
	e.status.Server = "Not needed for this Crogenitor"
	e.status.IsAuthOnline = false
	e.status.IsServerOnline = false
	e.updatePlayReadyLocked()
	e.emitStatusLocked()
	e.mu.Unlock()
	e.log("Stopped idle local login and server for remote launch")
	return nil
}
