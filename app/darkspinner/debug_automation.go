//go:build scenario

package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// This gate pauses Lua operation admission only. Network/game simulation and
// already admitted operations keep running, so the debugger remains responsive.
type debugAutomation struct {
	mutex       sync.Mutex
	isPaused    bool
	activeCount int
	changed     chan struct{}
}

type debugAutomationState struct {
	IsPaused             bool   `json:"is_paused"`
	IsSettled            bool   `json:"is_settled"`
	ActiveOperationCount int    `json:"active_operation_count"`
	IsSimulationPaused   bool   `json:"is_simulation_paused"`
	Scope                string `json:"scope"`
}

func (e *debugAutomation) stateLocked() debugAutomationState {
	return debugAutomationState{IsPaused: e.isPaused, IsSettled: e.isPaused && e.activeCount == 0,
		ActiveOperationCount: e.activeCount, Scope: "lua operation admission only; game simulation and clocks continue"}
}

func (e *debugAutomation) State() debugAutomationState {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return e.stateLocked()
}

func (e *debugAutomation) notifyLocked() {
	if e.changed != nil {
		close(e.changed)
	}
	e.changed = make(chan struct{})
}

func (e *debugAutomation) Enter(ctx context.Context) error {
	for {
		err := ctx.Err()
		if err != nil {
			return fmt.Errorf("automationContext: %w", err)
		}
		e.mutex.Lock()
		if !e.isPaused {
			e.activeCount++
			e.mutex.Unlock()
			return nil
		}
		changed := e.changed
		e.mutex.Unlock()
		select {
		case <-ctx.Done():
			return fmt.Errorf("automationWait: %w", ctx.Err())
		case <-changed:
		}
	}
}

func (e *debugAutomation) Leave() {
	e.mutex.Lock()
	e.activeCount--
	e.notifyLocked()
	e.mutex.Unlock()
}

func (e *debugAutomation) Pause(ctx context.Context) (debugAutomationState, error) {
	err := ctx.Err()
	if err != nil {
		return e.State(), fmt.Errorf("automationPauseContext: %w", err)
	}
	e.mutex.Lock()
	e.isPaused = true
	e.notifyLocked()
	for {
		state := e.stateLocked()
		err = ctx.Err()
		if err != nil {
			e.mutex.Unlock()
			return state, fmt.Errorf("automationPauseDeadline: %w", err)
		}
		if !e.isPaused {
			e.mutex.Unlock()
			return state, errors.New("automation pause was superseded by resume")
		}
		if e.activeCount == 0 {
			e.mutex.Unlock()
			return state, nil
		}
		changed := e.changed
		e.mutex.Unlock()
		select {
		case <-ctx.Done():
			return e.State(), fmt.Errorf("automationSettle: %w", ctx.Err())
		case <-changed:
		}
		e.mutex.Lock()
	}
}

func (e *debugAutomation) Resume() debugAutomationState {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.isPaused = false
	e.notifyLocked()
	return e.stateLocked()
}

func isDebugAutomationOperation(operation string) bool {
	return operation == "automation.state" || operation == "automation.pause" || operation == "automation.resume"
}

func (e *debugSupervisor) statusDTO() debugStatus {
	status := debugStatusDTO(e.registry.Snapshot())
	status.Automation = e.automation.State()
	return status
}

func (e *debugSupervisor) executeDebugAutomation(ctx context.Context, req debugOperationRequest) (any, error) {
	var empty struct{}
	err := decodeDebugPayload(req.Payload, &empty)
	if err != nil {
		return nil, fmt.Errorf("automationRequest: %w", err)
	}
	switch req.Operation {
	case "automation.state":
		return e.automation.State(), nil
	case "automation.pause":
		result, pauseErr := e.automation.Pause(ctx)
		if pauseErr != nil {
			return result, fmt.Errorf("automationPause: %w", pauseErr)
		}
		return result, nil
	case "automation.resume":
		return e.automation.Resume(), nil
	default:
		return nil, errors.New("unsupported automation control")
	}
}
