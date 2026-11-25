package maintenance

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const stateFilename = "maintenance-state.json"

type storedState struct {
	Version            int       `json:"version"`
	ID                 string    `json:"id"`
	Action             string    `json:"action"`
	Reason             string    `json:"reason"`
	Phase              string    `json:"phase"`
	Deadline           time.Time `json:"deadline"`
	IsActive           bool      `json:"is_active"`
	IsAdmissionBlocked bool      `json:"is_admission_blocked"`
	Delivered          int       `json:"delivered"`
	StartedAt          time.Time `json:"started_at"`
	WarningMinutes     int       `json:"warning_minutes"`
	LastReminderMinute int       `json:"last_reminder_minute"`
	IsDrainPending     bool      `json:"is_drain_pending,omitempty"`
}

func restoreState(path string) (countdown, error) {
	payload, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return countdown{}, nil
	}
	if err != nil {
		return countdown{}, fmt.Errorf("stateRead: %w", err)
	}
	if len(payload) > 16384 {
		return countdown{}, errors.New("maintenance state exceeds size limit")
	}
	var stored storedState
	err = json.Unmarshal(payload, &stored)
	if err != nil {
		return countdown{}, fmt.Errorf("stateDecode: %w", err)
	}
	req := Request{Action: stored.Action, Reason: stored.Reason, WarningMinutes: stored.WarningMinutes, OperationID: stored.ID}
	if req.Action == "announce" {
		req.Action = "hold"
	}
	err = validateRequest(req)
	if err != nil {
		return countdown{}, fmt.Errorf("stateValidate: %w", err)
	}
	if stored.Version != 1 || stored.ID == "" || stored.StartedAt.IsZero() || stored.Deadline.IsZero() || stored.Delivered < 0 || stored.LastReminderMinute < 0 || stored.LastReminderMinute > 1441 {
		return countdown{}, errors.New("invalid maintenance state")
	}
	state := countdown{State: State{ID: stored.ID, Action: stored.Action, Reason: stored.Reason,
		Phase: stored.Phase, Deadline: stored.Deadline, Delivered: stored.Delivered}, StartedAt: stored.StartedAt,
		WarningMinuteCount: stored.WarningMinutes, LastReminderMinute: stored.LastReminderMinute, IsDrainPending: stored.IsDrainPending}
	switch state.Phase {
	case "preparing":
		state.Phase = "cancelled"
	case "warning", "draining", "held", "stopping", "failed":
		state.IsActive = true
		state.IsAdmissionBlocked = state.Action != "announce"
	case "cancelled", "completed":
	default:
		return countdown{}, errors.New("unknown maintenance state phase")
	}
	if state.Phase == "held" && state.Action != "hold" || state.Phase == "stopping" && state.Action != "restart" && state.Action != "update" {
		return countdown{}, errors.New("maintenance state action and phase do not match")
	}
	if stored.Phase == "preparing" {
		err = saveState(path, state)
		if err != nil {
			return countdown{}, fmt.Errorf("stateAbort: %w", err)
		}
	}
	return state, nil
}

func saveState(path string, state countdown) error {
	if path == "" {
		return nil
	}
	stored := storedState{Version: 1, ID: state.ID, Action: state.Action, Reason: state.Reason,
		Phase: state.Phase, Deadline: state.Deadline, IsActive: state.IsActive,
		IsAdmissionBlocked: state.IsAdmissionBlocked, Delivered: state.Delivered, StartedAt: state.StartedAt,
		WarningMinutes: state.WarningMinuteCount, LastReminderMinute: state.LastReminderMinute, IsDrainPending: state.IsDrainPending}
	payload, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("stateEncode: %w", err)
	}
	w, err := os.CreateTemp(filepath.Dir(path), ".maintenance-state-*")
	if err != nil {
		return fmt.Errorf("stateCreate: %w", err)
	}
	temporaryPath := w.Name()
	defer removeStateTemp(temporaryPath)
	written, writeErr := w.Write(payload)
	if written != len(payload) && writeErr == nil {
		writeErr = io.ErrShortWrite
	}
	syncErr := w.Sync()
	closeErr := w.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return fmt.Errorf("stateWrite: %w", errors.Join(writeErr, syncErr, closeErr))
	}
	err = os.Rename(temporaryPath, path)
	if err != nil {
		return fmt.Errorf("stateReplace: %w", err)
	}
	if runtime.GOOS != "linux" {
		return nil
	}
	r, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("stateDirectory: %w", err)
	}
	syncErr = r.Sync()
	closeErr = r.Close()
	if syncErr != nil || closeErr != nil {
		return fmt.Errorf("stateDirectorySync: %w", errors.Join(syncErr, closeErr))
	}
	return nil
}

func removeStateTemp(path string) {
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("Maintenance temporary state cleanup failed", "error", err)
	}
}
