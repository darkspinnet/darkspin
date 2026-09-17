package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const preparationTimingFilename = "preparation-timing.jsonl"

type preparationTimingPhase struct {
	message      string
	startedAt    time.Time
	lastEventAt  time.Time
	lastProgress int
}

type preparationTimingRecorder struct {
	mu        sync.Mutex
	path      string
	startedAt time.Time
	phases    map[string]preparationTimingPhase
}

type preparationTimingEvent struct {
	Timestamp       string  `json:"timestamp"`
	ElapsedMS       float64 `json:"elapsedMs"`
	SincePreviousMS float64 `json:"sincePreviousMs,omitempty"`
	PhaseElapsedMS  float64 `json:"phaseElapsedMs,omitempty"`
	Subsystem       string  `json:"subsystem"`
	Event           string  `json:"event"`
	Phase           string  `json:"phase"`
	Progress        int     `json:"progress"`
}

func newPreparationTimingRecorder(logPath string) (*preparationTimingRecorder, error) {
	if strings.TrimSpace(logPath) == "" {
		return nil, fmt.Errorf("empty log path")
	}
	err := os.MkdirAll(logPath, 0o755)
	if err != nil {
		return nil, fmt.Errorf("logMkdir: %w", err)
	}
	now := time.Now()
	recorder := &preparationTimingRecorder{
		path:      filepath.Join(logPath, preparationTimingFilename),
		startedAt: now,
		phases: map[string]preparationTimingPhase{
			"Launcher": {
				message: "Initialization", startedAt: now, lastEventAt: now, lastProgress: 0,
			},
		},
	}
	event := recorder.event(now, "Launcher", "start", "Initialization", 0, 0, 0)
	contents, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("startMarshal: %w", err)
	}
	contents = append(contents, '\n')
	err = os.WriteFile(recorder.path, contents, 0o644)
	if err != nil {
		return nil, fmt.Errorf("startWrite: %w", err)
	}
	return recorder, nil
}

func (e *preparationTimingRecorder) progress(subsystem string, phase string, progress int) error {
	if e == nil {
		return nil
	}
	now := time.Now()
	e.mu.Lock()
	defer e.mu.Unlock()
	previous, exists := e.phases[subsystem]
	if exists && previous.message != phase {
		event := e.event(now, subsystem, "complete", previous.message, previous.lastProgress,
			now.Sub(previous.lastEventAt), now.Sub(previous.startedAt))
		err := e.append(event)
		if err != nil {
			return fmt.Errorf("phaseComplete: %w", err)
		}
		exists = false
	}
	if !exists {
		e.phases[subsystem] = preparationTimingPhase{
			message: phase, startedAt: now, lastEventAt: now, lastProgress: progress,
		}
		event := e.event(now, subsystem, "start", phase, progress, 0, 0)
		err := e.append(event)
		if err != nil {
			return fmt.Errorf("phaseStart: %w", err)
		}
		return nil
	}
	event := e.event(now, subsystem, "progress", phase, progress,
		now.Sub(previous.lastEventAt), now.Sub(previous.startedAt))
	previous.lastEventAt = now
	previous.lastProgress = progress
	e.phases[subsystem] = previous
	err := e.append(event)
	if err != nil {
		return fmt.Errorf("progressWrite: %w", err)
	}
	return nil
}

func (e *preparationTimingRecorder) finish(
	subsystem string,
	phase string,
	progress int,
	eventName string,
) error {
	if e == nil {
		return nil
	}
	now := time.Now()
	e.mu.Lock()
	defer e.mu.Unlock()
	previous, exists := e.phases[subsystem]
	if exists {
		phase = previous.message
		event := e.event(now, subsystem, eventName, phase, progress,
			now.Sub(previous.lastEventAt), now.Sub(previous.startedAt))
		delete(e.phases, subsystem)
		err := e.append(event)
		if err != nil {
			return fmt.Errorf("finishWrite: %w", err)
		}
		return nil
	}
	event := e.event(now, subsystem, eventName, phase, progress, 0, 0)
	err := e.append(event)
	if err != nil {
		return fmt.Errorf("finishWrite: %w", err)
	}
	return nil
}

func (e *preparationTimingRecorder) event(
	now time.Time,
	subsystem string,
	eventName string,
	phase string,
	progress int,
	sincePrevious time.Duration,
	phaseElapsed time.Duration,
) preparationTimingEvent {
	return preparationTimingEvent{
		Timestamp: now.Format(time.RFC3339Nano), ElapsedMS: durationMilliseconds(now.Sub(e.startedAt)),
		SincePreviousMS: durationMilliseconds(sincePrevious), PhaseElapsedMS: durationMilliseconds(phaseElapsed),
		Subsystem: subsystem, Event: eventName, Phase: phase, Progress: progress,
	}
}

func (e *preparationTimingRecorder) append(event preparationTimingEvent) error {
	contents, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("eventMarshal: %w", err)
	}
	contents = append(contents, '\n')
	output, err := os.OpenFile(e.path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("eventOpen: %w", err)
	}
	written, err := output.Write(contents)
	if err != nil {
		closeErr := output.Close()
		if closeErr != nil {
			return fmt.Errorf("eventWrite: %v; eventClose: %w", err, closeErr)
		}
		return fmt.Errorf("eventWrite: %w", err)
	}
	if written != len(contents) {
		closeErr := output.Close()
		if closeErr != nil {
			return fmt.Errorf("shortWrite: wrote %d of %d bytes; eventClose: %w", written, len(contents), closeErr)
		}
		return fmt.Errorf("shortWrite: wrote %d of %d bytes", written, len(contents))
	}
	err = output.Close()
	if err != nil {
		return fmt.Errorf("eventClose: %w", err)
	}
	return nil
}

func durationMilliseconds(duration time.Duration) float64 {
	return float64(duration.Microseconds()) / 1000
}

func (a *App) startPreparationTiming(logPath string) {
	recorder, err := newPreparationTimingRecorder(logPath)
	if err != nil {
		written, writeErr := fmt.Fprintf(os.Stderr, "darkspinner preparation timing: %v\n", err)
		if writeErr != nil || written == 0 {
			return
		}
		return
	}
	a.timingMu.Lock()
	a.preparationTiming = recorder
	a.timingMu.Unlock()
}

func (a *App) recordPreparationProgress(subsystem string, phase string, progress int) {
	a.timingMu.Lock()
	recorder := a.preparationTiming
	a.timingMu.Unlock()
	if recorder == nil {
		return
	}
	err := recorder.progress(subsystem, phase, progress)
	if err != nil {
		a.log("Preparation timing write failed: " + err.Error())
	}
}

func (a *App) finishPreparationTiming(subsystem string, phase string, progress int, eventName string) {
	a.timingMu.Lock()
	recorder := a.preparationTiming
	a.timingMu.Unlock()
	if recorder == nil {
		return
	}
	err := recorder.finish(subsystem, phase, progress, eventName)
	if err != nil {
		a.log("Preparation timing write failed: " + err.Error())
	}
}
