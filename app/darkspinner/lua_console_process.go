//go:build scenario

package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Retained only for compatibility with explicit standalone diagnostic workers.
const launcherLuaWorkerArgument = "--scenario-supervisor-worker"

// launcherLuaSession runs the supervisor and isolated server in the launcher.
// Each Run owns a fresh session; completed sessions remain available to inspect.
type launcherLuaSession struct {
	console    *launcherLua
	app        *App
	supervisor *debugSupervisor
	root       string
	scriptPath string
	done       chan struct{}
}

func (e *launcherLua) launch(ctx context.Context, app *App, scriptPath string) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("luaContext: %w", err)
	}
	supervisor, root, err := newDebugSupervisor(e.options)
	if err != nil {
		return fmt.Errorf("luaSupervisor: %w", err)
	}
	session := &launcherLuaSession{console: e, app: app, supervisor: supervisor,
		root: root, scriptPath: scriptPath, done: make(chan struct{})}
	supervisor.launcherHandler = app.launcherHandler
	supervisor.beforeHostStart = app.beforeServerStart
	supervisor.onLog = e.appendLine
	supervisor.onReady = session.ready
	e.mutex.Lock()
	e.child = session
	e.state.IsActive = true
	e.mutex.Unlock()
	go session.run(ctx)
	return nil
}

func (e *launcherLuaSession) ready(discovery debugDiscovery) {
	e.console.mutex.Lock()
	if e.console.child == e {
		e.console.state.RunID = discovery.ID
		e.console.state.Phase = "running"
	}
	e.console.mutex.Unlock()
}

func (e *launcherLuaSession) run(ctx context.Context) {
	stopContext := context.AfterFunc(ctx, e.requestStop)
	err := e.supervisor.serve(e.root, e.scriptPath)
	isContextStopped := stopContext()
	if !isContextStopped {
		// The context callback only requests shutdown; serve owns all cleanup.
	}
	if e.app.resumeLauncherHTTP != nil {
		resumeErr := e.app.resumeLauncherHTTP()
		if resumeErr != nil {
			e.console.appendLine("Launcher HTTP restore: " + resumeErr.Error())
		}
	}
	e.console.mutex.Lock()
	if e.console.child == e {
		e.console.state.IsActive = false
		if e.console.state.Phase == "running" || e.console.state.Phase == "starting" {
			e.console.state.Phase = "stopped"
		}
	}
	e.console.mutex.Unlock()
	if err != nil {
		e.console.fail(fmt.Errorf("luaSession: %w", err))
	}
	close(e.done)
}

func (e *launcherLuaSession) requestStop() {
	e.supervisor.stopOnce.Do(e.supervisor.requestStop)
}

func (e *launcherLuaSession) stop(ctx context.Context) error {
	e.requestStop()
	timer := time.NewTimer(45 * time.Second)
	defer timer.Stop()
	select {
	case <-e.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("sessionStop: %w", ctx.Err())
	case <-timer.C:
		return errors.New("scenario cleanup is still running; wait before starting another script")
	}
}

func (e *launcherLuaSession) stopScript(ctx context.Context) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("scriptStop: %w", err)
	}
	e.supervisor.scriptControl.stop()
	return nil
}

func (e *launcherLua) refresh(ctx context.Context) {
	if ctx == nil {
		return
	}
	e.mutex.Lock()
	session := e.child
	e.mutex.Unlock()
	if session == nil {
		return
	}
	select {
	case <-session.done:
		return
	default:
	}
	result := session.supervisor.registry.Snapshot().ScriptResult
	if result == nil {
		return
	}
	elapsed := result.FinishedAt.Sub(result.StartedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	e.mutex.Lock()
	if e.child == session {
		e.state.Phase = string(result.Outcome)
		e.state.ElapsedMS = elapsed.Milliseconds()
		e.state.Failure = ""
		if result.Failure != nil {
			e.state.Failure = result.Failure.Error()
		}
	}
	e.mutex.Unlock()
}
