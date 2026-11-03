package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
)

// launcherBrowserHost serves the launcher while neither gameplay server owns
// the configured port. The server taking ownership serves the same handler.
type launcherBrowserHost struct {
	ctx     context.Context
	handler http.Handler
	token   string
	mutex   sync.Mutex
	server  *http.Server
	done    <-chan error
}

func (e *launcherBrowserHost) start() (string, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	address, server, done, err := startLauncherBootstrap(e.handler, e.token)
	if err != nil {
		return "", fmt.Errorf("browserStart: %w", err)
	}
	e.server, e.done = server, done
	return address, nil
}

func (e *launcherBrowserHost) resume() error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.ctx.Err() != nil || e.server != nil {
		return nil
	}
	address, server, done, err := startLauncherBootstrap(e.handler, e.token)
	if err != nil {
		return fmt.Errorf("browserResume: %w", err)
	}
	if address == "" {
		return errors.New("restored browser address is empty")
	}
	e.server, e.done = server, done
	return nil
}

func (e *launcherBrowserHost) stop() error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.server == nil {
		return nil
	}
	closeErr := e.server.Close()
	serveErr := <-e.done
	e.server = nil
	if closeErr != nil {
		return fmt.Errorf("browserClose: %w", closeErr)
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return fmt.Errorf("browserServe: %w", serveErr)
	}
	return nil
}

func (e *launcherBrowserHost) close() {
	err := e.stop()
	if err != nil {
		log.Printf("Launcher browser close: %v", err)
	}
}
