//go:build scenario && windows

package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// The fence is installed only after the last native receipt settled. It also
// lengthens the current read, not just the next heartbeat read.
func (e *controlPipe) beginDebugPause(ctx context.Context, token string, until time.Time) (controlEnvelope, string, error) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		e.mutex.Lock()
		if e.pauseID != "" || e.isPauseUnsafe || !e.isConnected || e.r == nil {
			e.mutex.Unlock()
			return controlEnvelope{}, "", errors.New("debugger pause requires an unfenced connected bridge with no unresolved dispatched command")
		}
		if e.pending == nil {
			e.pauseID, e.pauseUntil = token, until
			err := e.r.SetReadDeadline(until)
			if err != nil {
				e.pauseID = ""
				e.pauseUntil = time.Time{}
				e.mutex.Unlock()
				return controlEnvelope{}, "", fmt.Errorf("pauseReadDeadline: %w", err)
			}
			envelope, generation := e.last, e.generation
			e.mutex.Unlock()
			return envelope, generation, nil
		}
		e.mutex.Unlock()
		select {
		case <-ctx.Done():
			return controlEnvelope{}, "", fmt.Errorf("pauseSettle: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
func (e *controlPipe) endDebugPause(token string) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.pauseID != token {
		return errors.New("debugger pause fence token changed")
	}
	e.pauseID = ""
	e.pauseUntil = time.Time{}
	if e.r == nil {
		return errors.New("debugger bridge closed during pause")
	}
	err := e.r.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err != nil {
		return fmt.Errorf("resumeReadDeadline: %w", err)
	}
	return nil
}
func (e *controlPipe) debugPauseActive() bool {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return e.pauseID != ""
}
