package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	stdruntime "runtime"
	"time"

	"github.com/darkspinnet/darkspin/server/blaze"
	"github.com/darkspinnet/darkspin/server/bootstrap"
	"github.com/darkspinnet/darkspin/server/buildinfo"
	"github.com/darkspinnet/darkspin/server/maintenance"
)

func (e *Server) runMaintenanceSchedule(ctx, runCtx context.Context, cancel context.CancelFunc) {
	defer close(e.maintenanceDone)
	req := maintenance.ScheduleOptions{WarningMinutes: 30,
		UpdateRequestPath: filepath.Join(filepath.Dir(e.environment.ConfigPath), "update-request.json")}
	if e.maintenanceSchedule != nil {
		req = *e.maintenanceSchedule
	}
	for runCtx.Err() == nil {
		isDue, err := e.maintenanceControl.RunSchedule(runCtx, req)
		if err != nil {
			e.logger.Printf("Scheduled maintenance failed; retaining server: %v", err)
			timer := time.NewTimer(time.Second)
			select {
			case <-runCtx.Done():
			case <-timer.C:
			}
			timer.Stop()
			continue
		}
		if !isDue {
			return
		}
		state := e.maintenanceControl.Status()
		e.lifecycleMu.Lock()
		if !e.isClosed && ctx.Err() == nil && state.Phase == "stopping" &&
			(state.Action == "restart" || state.Action == "update") {
			e.isMaintenanceStop = true
			e.maintenanceOperationID = state.ID
			e.maintenanceAction = state.Action
			cancel()
		}
		e.lifecycleMu.Unlock()
		return
	}
}

func (e *Server) writeMaintenanceRequest(ctx context.Context) error {
	if !e.isMaintenanceStop {
		return nil
	}
	err := e.writeCleanMaintenanceRequest(ctx)
	if err == nil && ctx.Err() == nil {
		return nil
	}
	resolveErr := e.maintenanceControl.Resolve(e.maintenanceOperationID, "failed")
	if err != nil || resolveErr != nil {
		return fmt.Errorf("maintenanceRecovery: %w", errors.Join(err, resolveErr))
	}
	// An external stop can win the shutdown race. Keep its normal cancellation
	// result while making this unmarked maintenance operation recoverable.
	return nil
}

func (e *Server) writeCleanMaintenanceRequest(ctx context.Context) error {
	if !e.isMaintenanceStop || ctx.Err() != nil {
		return nil
	}
	if e.closeErr != nil {
		return fmt.Errorf("maintenanceClose: %w", e.closeErr)
	}
	if !e.maintenanceControl.IsScheduledUpdatePending() {
		return nil
	}
	if e.maintenanceSchedule == nil || e.maintenanceOperationID == "" ||
		(e.maintenanceAction != "restart" && e.maintenanceAction != "update") {
		return errors.New("managed maintenance shutdown binding unavailable")
	}
	req := struct {
		Version         int       `json:"version"`
		RequestedAt     time.Time `json:"requested_at"`
		BuildID         string    `json:"build_id"`
		IsCleanShutdown bool      `json:"is_clean_shutdown"`
		Action          string    `json:"action"`
		OperationID     string    `json:"operation_id"`
	}{Version: 1, RequestedAt: time.Now().UTC(), BuildID: buildinfo.ID, IsCleanShutdown: true,
		Action: e.maintenanceAction, OperationID: e.maintenanceOperationID}
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("requestEncode: %w", err)
	}
	path := e.maintenanceSchedule.UpdateRequestPath
	w, err := os.CreateTemp(filepath.Dir(path), ".update-request-*")
	if err != nil {
		return fmt.Errorf("requestCreate: %w", err)
	}
	defer e.removeMaintenanceTemp(w.Name())
	written, writeErr := w.Write(payload)
	if written != len(payload) && writeErr == nil {
		writeErr = io.ErrShortWrite
	}
	syncErr := w.Sync()
	closeErr := w.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return fmt.Errorf("requestWrite: %w", errors.Join(writeErr, syncErr, closeErr))
	}
	if ctx.Err() != nil {
		return nil
	}
	err = os.Rename(w.Name(), path)
	if err != nil {
		return fmt.Errorf("requestInstall: %w", err)
	}
	err = syncMaintenanceDirectory(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("requestDirectory: %w", err)
	}
	return nil
}

func (e *Server) recoverRestoredMaintenance(control *maintenance.Control) error {
	state := control.Status()
	isLegacySupervisor := e.maintenanceSchedule != nil && os.Getenv("DARKSPIN_BOOTSTRAP_VERSION") == ""
	if state.Phase != "stopping" && !(state.Phase == "failed" && isLegacySupervisor) {
		return nil
	}
	result, err := bootstrap.ReadMaintenanceResult(filepath.Dir(e.environment.ConfigPath))
	if err == nil && result.OperationID == state.ID && result.Action == state.Action {
		// The supervisor publishes starting/validating before starting its child.
		// Retain the closed state until the matching final result is observed.
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		e.logger.Printf("Restored maintenance supervisor result unavailable operation=%s: %v", state.ID, err)
	}
	if isLegacySupervisor && (err == nil || errors.Is(err, os.ErrNotExist)) && e.panelMaintenance != nil &&
		(state.Action == "restart" || state.Action == "update") {
		e.panelMaintenance.startupRecoveryID = state.ID
		e.logger.Printf("Legacy supervisor maintenance recovery awaiting four stable image-verified readiness probes operation=%s action=%s", state.ID, state.Action)
		return nil
	}
	resolveErr := control.Resolve(state.ID, "failed")
	if resolveErr != nil {
		return fmt.Errorf("restoreResolve: %w", resolveErr)
	}
	e.logger.Printf("Restored maintenance has no matching supervisor result; admission remains closed until recovery operation=%s", state.ID)
	return nil
}

func syncMaintenanceDirectory(path string) error {
	// Bootstrap scheduling is Linux-only. Windows directory handles do not
	// support os.File.Sync; preserve ordinary server compilation there.
	if stdruntime.GOOS != "linux" {
		return nil
	}
	r, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("directoryOpen: %w", err)
	}
	syncErr := r.Sync()
	closeErr := r.Close()
	if syncErr != nil || closeErr != nil {
		return fmt.Errorf("directorySync: %w", errors.Join(syncErr, closeErr))
	}
	return nil
}

func (e *Server) removeMaintenanceTemp(path string) {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		e.logger.Printf("Maintenance temporary request cleanup failed: %v", err)
	}
}

func (e *Server) broadcastMaintenance(ctx context.Context, message string) (int, error) {
	delivered, err := blaze.BroadcastSystemChat(ctx, e.maintenanceServers, message)
	if err != nil {
		return delivered, fmt.Errorf("maintenanceBroadcast: %w", err)
	}
	return delivered, nil
}

func (e *panelMaintenance) RequestLocal(ctx context.Context, req maintenance.Request) (maintenance.State, error) {
	err := maintenance.ValidateRequest(req)
	if err != nil {
		return maintenance.State{}, fmt.Errorf("localRequestValidate: %w", err)
	}
	lease, err := e.gate.Change(ctx)
	if err != nil {
		return maintenance.State{}, fmt.Errorf("localRequestGate: %w", err)
	}
	defer e.closeLease(lease)
	current, err := e.control()
	if err != nil {
		return maintenance.State{}, fmt.Errorf("localRequestControl: %w", err)
	}
	state := current.control.Status()
	if req.Action != "hold" && !current.isManaged {
		return state, maintenance.ErrUnsupported
	}
	if state.IsActive {
		if state.Action == req.Action {
			return state, nil
		}
		return state, maintenance.ErrConflict
	}
	actorType := req.ActorType
	if actorType == "" {
		actorType = "local_operator"
	}
	if req.Reason == "" {
		req.Reason = "Local operator requested server maintenance."
		if actorType == "system" {
			req.Reason = "Scheduled server maintenance."
		}
	}
	operationID, err := e.beginTrusted(ctx, actorType, "maintenance."+req.Action, req.Reason, req.OperationID)
	if err != nil {
		return state, fmt.Errorf("localRequestAudit: %w", err)
	}
	req.OperationID = operationID
	state, err = current.control.Request(ctx, req)
	if err != nil {
		auditErr := e.complete(ctx, operationID, false, "prepare_failed")
		return state, fmt.Errorf("localRequestState: %w", errors.Join(err, auditErr))
	}
	return state, nil
}

func (e *panelMaintenance) ActivateLocal(ctx context.Context, id string) error {
	err := e.Activate(ctx, id)
	if err != nil {
		return fmt.Errorf("localActivate: %w", err)
	}
	return nil
}

func (e *panelMaintenance) AbortLocal(ctx context.Context, id string) error {
	err := e.Abort(ctx, id)
	if err != nil {
		return fmt.Errorf("localAbortState: %w", err)
	}
	return nil
}

func (e *panelMaintenance) CancelLocal(ctx context.Context, id string) (maintenance.State, error) {
	lease, err := e.gate.Change(ctx)
	if err != nil {
		return maintenance.State{}, fmt.Errorf("localCancelGate: %w", err)
	}
	defer e.closeLease(lease)
	current, err := e.control()
	if err != nil {
		return maintenance.State{}, fmt.Errorf("localCancelControl: %w", err)
	}
	previous := current.control.Status()
	if id == "" || id != previous.ID {
		return previous, maintenance.ErrConflict
	}
	if previous.Phase == "stopping" || previous.Action == "hold" && previous.Phase == "draining" && !time.Now().Before(previous.Deadline) {
		return previous, maintenance.ErrStopping
	}
	if current.isManaged && previous.Phase == "failed" && e.isSupervisorPending(previous) {
		return previous, maintenance.ErrStopping
	}
	if !previous.IsActive {
		return previous, nil
	}
	operationID, err := e.beginTrusted(ctx, "local_operator", "maintenance.cancel", "Local operator cancelled server maintenance.", "")
	if err != nil {
		return previous, fmt.Errorf("localCancelAudit: %w", err)
	}
	state, err := current.control.Cancel(ctx, id)
	if state.Phase != "cancelled" {
		auditErr := e.complete(ctx, operationID, false, "cancel_failed")
		return state, fmt.Errorf("localCancelState: %w", errors.Join(err, auditErr))
	}
	outcome := "cancelled"
	if err != nil {
		outcome = "cancellation_delivery_partial"
		e.server.logger.Printf("Local maintenance cancellation notification partial operation=%s: %v", id, err)
	}
	cancelAuditErr := e.complete(ctx, operationID, true, outcome)
	isOriginalSuccess := previous.Phase == "held"
	originalOutcome := "cancelled"
	if isOriginalSuccess {
		originalOutcome = "held"
	}
	originalAuditErr := e.complete(ctx, id, isOriginalSuccess, originalOutcome)
	if cancelAuditErr != nil || originalAuditErr != nil {
		return state, fmt.Errorf("localCancelComplete: %w", errors.Join(cancelAuditErr, originalAuditErr))
	}
	return state, nil
}

func (e *panelMaintenance) beginTrusted(ctx context.Context, actorType, action, reason, operationID string) (string, error) {
	if e.accessService == nil {
		return "", errors.New("maintenance audit unavailable")
	}
	if e.isAuditBacklogged() {
		return "", errors.New("maintenance audit recovery is pending")
	}
	id, err := e.accessService.BeginTrustedServerOperation(ctx, actorType, action, reason, operationID)
	if err != nil {
		return "", fmt.Errorf("trustedAudit: %w", err)
	}
	return id, nil
}

func (e *panelMaintenance) AnnounceLocal(ctx context.Context, minutes int) (maintenance.State, error) {
	if minutes < 1 || minutes > 1440 {
		return maintenance.State{}, maintenance.ErrInvalidRequest
	}
	lease, err := e.gate.Change(ctx)
	if err != nil {
		return maintenance.State{}, fmt.Errorf("localAnnounceGate: %w", err)
	}
	defer e.closeLease(lease)
	current, err := e.control()
	if err != nil {
		return maintenance.State{}, fmt.Errorf("localAnnounceControl: %w", err)
	}
	state := current.control.Status()
	if state.IsActive {
		if state.Action == "announce" && state.Phase != "failed" {
			return state, nil
		}
		return state, maintenance.ErrConflict
	}
	operationID, err := e.beginTrusted(ctx, "local_operator", "server.announcement", "Local operator requested a maintenance warning.", "")
	if err != nil {
		return state, fmt.Errorf("localAnnounceAudit: %w", err)
	}
	state, err = current.control.Announce(ctx, minutes, operationID)
	outcome := "announced"
	if err != nil {
		outcome = "announcement_failed"
	}
	auditErr := e.complete(ctx, operationID, err == nil, outcome)
	if err != nil || auditErr != nil {
		return state, fmt.Errorf("localAnnounceComplete: %w", errors.Join(err, auditErr))
	}
	return state, nil
}
