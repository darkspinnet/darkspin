//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario/desktop"
)

type debugObserverSignal struct {
	Schema     int    `json:"schema"`
	Name       string `json:"name"`
	PID        uint32 `json:"pid"`
	InstanceID string `json:"instance_id"`
}

type debugObserverReceipt struct {
	ClientID   string              `json:"client_id"`
	Signal     debugObserverSignal `json:"signal"`
	Path       string              `json:"path"`
	ObservedAt time.Time           `json:"observed_at"`
}

func waitDebugObserverSignal(ctx context.Context, clientManager *debugClientManager, payload json.RawMessage) (debugObserverReceipt, error) {
	if clientManager == nil {
		return debugObserverReceipt{}, errors.New("observer signal requires an owned client")
	}
	var req struct {
		ClientID string `json:"client_id"`
		Name     string `json:"name"`
	}
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return debugObserverReceipt{}, fmt.Errorf("signalRequest: %w", err)
	}
	if !debugDiagnosticLabelPattern.MatchString(req.Name) {
		return debugObserverReceipt{}, errors.New("observer signal name must be a diagnostic label")
	}
	initial, err := clientManager.State(req.ClientID)
	if err != nil {
		return debugObserverReceipt{}, fmt.Errorf("signalClient: %w", err)
	}
	if !initial.IsRunning || initial.TracePath == "" {
		return debugObserverReceipt{}, errors.New("observer signal requires a running owned client")
	}
	path := filepath.Join(filepath.Dir(initial.TracePath), "signal-"+req.Name+".json")
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, stateErr := clientManager.State(req.ClientID)
		if stateErr != nil {
			return debugObserverReceipt{}, fmt.Errorf("signalState: %w", stateErr)
		}
		if !current.IsRunning || current.ProcessID != initial.ProcessID || current.InstanceID != initial.InstanceID {
			return debugObserverReceipt{}, errors.New("observer signal client identity changed or stopped")
		}
		signal, isReady, readErr := readDebugObserverSignal(path)
		if readErr != nil {
			return debugObserverReceipt{}, fmt.Errorf("signalRead: %w", readErr)
		}
		if isReady {
			if signal.Schema != 1 || signal.Name != req.Name || signal.PID != initial.ProcessID || signal.InstanceID != initial.InstanceID {
				return debugObserverReceipt{}, errors.New("observer signal does not match this owned process instance")
			}
			return debugObserverReceipt{ClientID: req.ClientID, Signal: signal, Path: path, ObservedAt: time.Now().UTC()}, nil
		}
		select {
		case <-ctx.Done():
			return debugObserverReceipt{}, fmt.Errorf("signalDeadline: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func readDebugObserverSignal(path string) (debugObserverSignal, bool, error) {
	err := desktop.CheckAncestors(filepath.Dir(path))
	if err != nil {
		return debugObserverSignal{}, false, fmt.Errorf("signalPath: %w", err)
	}
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return debugObserverSignal{}, false, nil
	}
	if err != nil {
		return debugObserverSignal{}, false, fmt.Errorf("signalStat: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return debugObserverSignal{}, false, errors.New("regular observer signal file required")
	}
	r, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return debugObserverSignal{}, false, nil
	}
	if err != nil {
		return debugObserverSignal{}, false, fmt.Errorf("signalOpen: %w", err)
	}
	opened, statErr := r.Stat()
	if statErr != nil || !os.SameFile(fi, opened) {
		closeErr := r.Close()
		return debugObserverSignal{}, false, fmt.Errorf("signalIdentity: %w", errors.Join(statErr, closeErr, errors.New("observer signal file changed")))
	}
	payload, readErr := io.ReadAll(io.LimitReader(r, 4097))
	closeErr := r.Close()
	if readErr != nil || closeErr != nil {
		return debugObserverSignal{}, false, fmt.Errorf("signalPayload: %w", errors.Join(readErr, closeErr))
	}
	if len(payload) > 4096 {
		return debugObserverSignal{}, false, errors.New("observer signal exceeds 4096 bytes")
	}
	var signal debugObserverSignal
	err = json.Unmarshal(payload, &signal)
	if err != nil {
		// The external observer may still be completing its first write.
		return debugObserverSignal{}, false, nil
	}
	return signal, true, nil
}
