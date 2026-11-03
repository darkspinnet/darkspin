package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
	"github.com/darkspinnet/darkspin/server/admission"
	"github.com/darkspinnet/darkspin/server/bootstrap"
	"github.com/darkspinnet/darkspin/server/controlpanel"
	"github.com/darkspinnet/darkspin/server/maintenance"
)

type panelMaintenance struct {
	server            *Server
	accessService     *accountaccess.Service
	gate              *admission.Gate
	auditMu           sync.Mutex
	completions       map[string]maintenanceCompletion
	connectionControl *panelConnectionControl
	startupRecoveryID string
	readinessCount    int
	lastReadinessAt   time.Time
}

// ObserveReady is reachable only through the authenticated loopback maintenance
// channel, after the probe verifies the main endpoint against the running image.
// Only the operation restored at startup under a legacy supervisor is eligible.
func (e *panelMaintenance) ObserveReady(ctx context.Context, id string) (maintenance.State, error) {
	lease, err := e.gate.Change(ctx)
	if err != nil {
		return maintenance.State{}, fmt.Errorf("readyGate: %w", err)
	}
	defer e.closeLease(lease)
	current, err := e.control()
	if err != nil {
		return maintenance.State{}, fmt.Errorf("readyControl: %w", err)
	}
	state := current.control.Status()
	if id == "" || id != e.startupRecoveryID || id != state.ID ||
		(state.Phase != "stopping" && state.Phase != "failed") ||
		(state.Action != "restart" && state.Action != "update") {
		return state, nil
	}
	now := time.Now()
	if !e.lastReadinessAt.IsZero() && now.Sub(e.lastReadinessAt) < 4*time.Second {
		return state, nil
	}
	if !e.lastReadinessAt.IsZero() && now.Sub(e.lastReadinessAt) > 8*time.Second {
		e.readinessCount = 0
	}
	e.lastReadinessAt = now
	e.readinessCount++
	// The legacy supervisor validates startup after three successful probes.
	// A fourth proves it kept this child after first-boot validation/commit.
	if e.readinessCount < 4 {
		return state, nil
	}
	err = current.control.Resolve(id, "healthy")
	if err != nil {
		return state, fmt.Errorf("readyResolve: %w", err)
	}
	e.startupRecoveryID = ""
	e.server.logger.Printf("Legacy supervisor startup recovered; reopening player admission operation=%s action=%s; update installation outcome is unavailable", id, state.Action)
	err = e.complete(ctx, id, true, "legacy_server_ready")
	if err != nil {
		e.server.logger.Printf("Legacy maintenance recovery audit queued operation=%s: %v", id, err)
	}
	return current.control.Status(), nil
}

type maintenanceCompletion struct {
	operationID string
	isSuccess   bool
	outcome     string
}

const maximumMaintenanceCompletions = 256

type maintenanceRuntime struct {
	control   *maintenance.Control
	isManaged bool
}

func (e *panelMaintenance) control() (maintenanceRuntime, error) {
	if e == nil || e.server == nil {
		return maintenanceRuntime{}, errors.New("maintenance runtime unavailable")
	}
	e.server.lifecycleMu.Lock()
	control := e.server.maintenanceControl
	isManaged := e.server.maintenanceSchedule != nil
	isClosed := e.server.isClosed
	e.server.lifecycleMu.Unlock()
	if control == nil || isClosed {
		return maintenanceRuntime{}, errors.New("maintenance control unavailable")
	}
	return maintenanceRuntime{control: control, isManaged: isManaged}, nil
}

func (e *panelMaintenance) CurrentAdmission() admission.State {
	current, err := e.control()
	if err != nil {
		return admission.State{IsBlocked: true, Reason: "Server is starting or stopping."}
	}
	state := current.control.Status()
	if state.IsAdmissionBlocked && strings.TrimSpace(state.Reason) == "" {
		state.Reason = "Server maintenance is in progress."
	}
	return admission.State{IsBlocked: state.IsAdmissionBlocked, Reason: state.Reason, Deadline: state.Deadline}
}

func (e *panelMaintenance) ActiveMatchCount() int {
	if e == nil || e.server == nil || e.server.gameManager == nil {
		return 0
	}
	matches := 0
	games := e.server.gameManager.Games()
	for _, instance := range games {
		if instance == nil || !instance.IsMaintenanceActive() {
			continue
		}
		matches++
	}
	return matches
}

func (e *panelMaintenance) Snapshot(ctx context.Context) (controlpanel.MaintenanceSnapshot, error) {
	if ctx == nil {
		return controlpanel.MaintenanceSnapshot{}, errors.New("nil maintenance context")
	}
	err := ctx.Err()
	if err != nil {
		return controlpanel.MaintenanceSnapshot{}, fmt.Errorf("maintenanceContext: %w", err)
	}
	current, err := e.control()
	if err != nil {
		return controlpanel.MaintenanceSnapshot{}, fmt.Errorf("maintenanceControl: %w", err)
	}
	snapshot := controlpanel.MaintenanceSnapshot{State: current.control.Status(), IsRestartSupported: current.isManaged, IsUpdateSupported: current.isManaged}
	result, err := bootstrap.ReadMaintenanceResult(filepath.Dir(e.server.environment.ConfigPath))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			e.server.logger.Printf("Maintenance result observation unavailable: %v", err)
		}
		return snapshot, nil
	}
	if result.OperationID == snapshot.State.ID && result.Action == snapshot.State.Action {
		snapshot.Result = &controlpanel.MaintenanceOutcome{OperationID: result.OperationID, Action: result.Action, Phase: result.Phase,
			UpdatedAt: result.UpdatedAt, InstalledVersion: result.InstalledVersion, CandidateVersion: result.CandidateVersion,
			IsSuccess: result.IsSuccess, FailureCode: result.FailureCode}
	}
	return snapshot, nil
}

func (e *panelMaintenance) authorize(ctx context.Context, actor accountaccess.Actor) error {
	if e.accessService == nil {
		return errors.New("maintenance authorization unavailable")
	}
	access, err := e.accessService.Authorize(ctx, actor.UserID, actor.Revision, true)
	if err != nil {
		return fmt.Errorf("maintenanceAuthorize: %w", err)
	}
	if access.UserID != actor.UserID {
		return accountaccess.ErrAccessDenied
	}
	return nil
}

func (e *panelMaintenance) Request(ctx context.Context, actor accountaccess.Actor, req maintenance.Request) (maintenance.State, error) {
	err := e.authorize(ctx, actor)
	if err != nil {
		return maintenance.State{}, fmt.Errorf("requestActor: %w", err)
	}
	err = maintenance.ValidateRequest(req)
	if err != nil {
		return maintenance.State{}, fmt.Errorf("requestValidate: %w", err)
	}
	lease, err := e.gate.Change(ctx)
	if err != nil {
		return maintenance.State{}, fmt.Errorf("requestGate: %w", err)
	}
	defer e.closeLease(lease)
	current, err := e.control()
	if err != nil {
		return maintenance.State{}, fmt.Errorf("requestControl: %w", err)
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
	if strings.TrimSpace(req.Reason) == "" {
		req.Reason = "Administrator requested server maintenance."
	}
	if e.isAuditBacklogged() {
		return state, errors.New("maintenance audit recovery is pending")
	}
	operationID, err := e.accessService.BeginServerOperation(ctx, actor, "maintenance."+req.Action, req.Reason)
	if err != nil {
		return state, fmt.Errorf("requestAudit: %w", err)
	}
	req.OperationID = operationID
	state, err = current.control.Request(ctx, req)
	if err != nil {
		auditErr := e.complete(ctx, operationID, false, "prepare_failed")
		return state, fmt.Errorf("requestPrepare: %w", errors.Join(err, auditErr))
	}
	return state, nil
}

func (e *panelMaintenance) Activate(ctx context.Context, id string) error {
	lease, err := e.gate.Change(ctx)
	if err != nil {
		return fmt.Errorf("activateGate: %w", err)
	}
	defer e.closeLease(lease)
	current, err := e.control()
	if err != nil {
		return fmt.Errorf("activateControl: %w", err)
	}
	state := current.control.Status()
	if !current.isManaged && state.Action != "hold" {
		return maintenance.ErrUnsupported
	}
	err = current.control.Activate(id)
	if err != nil {
		return fmt.Errorf("activateState: %w", err)
	}
	return nil
}

func (e *panelMaintenance) Abort(ctx context.Context, id string) error {
	lease, err := e.gate.Change(ctx)
	if err != nil {
		return fmt.Errorf("abortGate: %w", err)
	}
	defer e.closeLease(lease)
	current, err := e.control()
	if err != nil {
		return fmt.Errorf("abortControl: %w", err)
	}
	state := current.control.Status()
	if state.ID != id || state.Phase != "preparing" {
		return nil
	}
	err = current.control.Abort(id)
	if err != nil {
		return fmt.Errorf("abortState: %w", err)
	}
	err = e.complete(ctx, id, false, "acceptance_not_flushed")
	if err != nil {
		return fmt.Errorf("abortAudit: %w", err)
	}
	return nil
}

func (e *panelMaintenance) Cancel(ctx context.Context, actor accountaccess.Actor, id string) (maintenance.State, error) {
	err := e.authorize(ctx, actor)
	if err != nil {
		return maintenance.State{}, fmt.Errorf("cancelActor: %w", err)
	}
	lease, err := e.gate.Change(ctx)
	if err != nil {
		return maintenance.State{}, fmt.Errorf("cancelGate: %w", err)
	}
	defer e.closeLease(lease)
	current, err := e.control()
	if err != nil {
		return maintenance.State{}, fmt.Errorf("cancelControl: %w", err)
	}
	state := current.control.Status()
	if id == "" || id != state.ID {
		return state, maintenance.ErrConflict
	}
	if state.Phase == "stopping" {
		return state, maintenance.ErrStopping
	}
	if state.Action == "hold" && state.Phase == "draining" && !time.Now().Before(state.Deadline) {
		return state, maintenance.ErrStopping
	}
	if current.isManaged && state.Phase == "failed" && e.isSupervisorPending(state) {
		return state, maintenance.ErrStopping
	}
	if !state.IsActive {
		return state, nil
	}
	if e.isAuditBacklogged() {
		return state, errors.New("maintenance audit recovery is pending")
	}
	operationID, err := e.accessService.BeginServerOperation(ctx, actor, "maintenance.cancel", "Administrator cancelled server maintenance.")
	if err != nil {
		return state, fmt.Errorf("cancelAudit: %w", err)
	}
	previousPhase := state.Phase
	state, err = current.control.Cancel(ctx, id)
	if state.Phase != "cancelled" {
		auditErr := e.complete(ctx, operationID, false, "cancel_failed")
		return state, fmt.Errorf("cancelState: %w", errors.Join(err, auditErr))
	}
	outcome := "cancelled"
	if err != nil {
		outcome = "cancellation_delivery_partial"
		e.server.logger.Printf("Maintenance cancelled with partial notification delivery operation=%s: %v", id, err)
	}
	cancelAuditErr := e.complete(ctx, operationID, true, outcome)
	isOriginalSuccess := previousPhase == "held"
	originalOutcome := "cancelled"
	if isOriginalSuccess {
		originalOutcome = "held"
	}
	originalAuditErr := e.complete(ctx, id, isOriginalSuccess, originalOutcome)
	if cancelAuditErr != nil || originalAuditErr != nil {
		return state, fmt.Errorf("cancelComplete: %w", errors.Join(cancelAuditErr, originalAuditErr))
	}
	return state, nil
}

func (e *panelMaintenance) complete(ctx context.Context, id string, isSuccess bool, outcome string) error {
	if e.accessService == nil {
		return nil
	}
	e.auditMu.Lock()
	pending, isPending := e.completions[id]
	e.auditMu.Unlock()
	if isPending {
		// A later cancellation cannot replace an already observed successful
		// hold while its terminal journal write is waiting for recovery.
		isSuccess, outcome = pending.isSuccess, pending.outcome
	}
	err := e.accessService.CompleteServerOperation(ctx, id, isSuccess, outcome)
	if errors.Is(err, accountaccess.ErrNotFound) || errors.Is(err, accountaccess.ErrInvalidRequest) {
		// Restored countdowns from versions before trusted-command auditing may
		// have no accepted row or use the older non-journal ID representation.
		err = nil
	}
	e.auditMu.Lock()
	if err == nil {
		delete(e.completions, id)
	} else {
		if e.completions == nil {
			e.completions = make(map[string]maintenanceCompletion)
		}
		if len(e.completions) < maximumMaintenanceCompletions {
			e.completions[id] = maintenanceCompletion{operationID: id, isSuccess: isSuccess, outcome: outcome}
		}
	}
	e.auditMu.Unlock()
	if err != nil {
		return fmt.Errorf("maintenanceComplete: %w", err)
	}
	return nil
}

func (e *panelMaintenance) isAuditBacklogged() bool {
	e.auditMu.Lock()
	defer e.auditMu.Unlock()
	// Cancellation can complete its own journal row and the original request.
	return len(e.completions) >= maximumMaintenanceCompletions-2
}

func (e *panelMaintenance) retryCompletions(ctx context.Context) {
	e.auditMu.Lock()
	completions := make([]maintenanceCompletion, 0, len(e.completions))
	for _, completion := range e.completions {
		completions = append(completions, completion)
	}
	e.auditMu.Unlock()
	for _, completion := range completions {
		err := e.complete(ctx, completion.operationID, completion.isSuccess, completion.outcome)
		if err != nil && ctx.Err() == nil {
			e.server.logger.Printf("Maintenance pending outcome audit failed operation=%s: %v", completion.operationID, err)
		}
	}
}

func (e *panelMaintenance) closeLease(lease *admission.Lease) {
	err := lease.Close()
	if err != nil {
		e.server.logger.Printf("Maintenance admission lease close failed: %v", err)
	}
}

func (e *panelMaintenance) isSupervisorPending(state maintenance.State) bool {
	result, err := bootstrap.ReadMaintenanceResult(filepath.Dir(e.server.environment.ConfigPath))
	if err != nil {
		// A failed orphan can be recovered explicitly without a readable result.
		// Only a matching supervisor's active operation reserves cancellation.
		return false
	}
	if result.OperationID != state.ID || result.Action != state.Action {
		return false
	}
	return result.Phase != "healthy" && result.Phase != "rolled_back" && result.Phase != "failed"
}

func (e *Server) runMaintenanceReconciliation(ctx context.Context) {
	defer close(e.maintenanceReconciliationDone)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	completedID := ""
	completedPhase := ""
	for ctx.Err() == nil {
		e.panelMaintenance.retryCompletions(ctx)
		id, phase, err := e.panelMaintenance.reconcile(ctx, completedID, completedPhase)
		if err != nil {
			if ctx.Err() == nil {
				e.logger.Printf("Maintenance outcome reconciliation failed: %v", err)
			}
		} else {
			completedID, completedPhase = id, phase
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
}

func (e *panelMaintenance) reconcile(ctx context.Context, completedID, completedPhase string) (string, string, error) {
	current, err := e.control()
	if err != nil {
		return completedID, completedPhase, fmt.Errorf("reconcileControl: %w", err)
	}
	state := current.control.Status()
	if state.ID == "" {
		return completedID, completedPhase, nil
	}
	if state.ID == completedID && state.Phase == completedPhase && state.Phase != "failed" {
		return completedID, completedPhase, nil
	}
	isSuccess := false
	outcome := ""
	switch state.Phase {
	case "held":
		isSuccess, outcome = true, "held"
	case "cancelled":
		outcome = "cancelled"
	case "failed":
		outcome = "maintenance_failed"
	}
	if current.isManaged && (state.Phase == "stopping" || state.Phase == "failed" || state.Phase == "completed") {
		result, readErr := bootstrap.ReadMaintenanceResult(filepath.Dir(e.server.environment.ConfigPath))
		if errors.Is(readErr, os.ErrNotExist) {
			id, phase, finishErr := e.finishReconciliation(ctx, state, isSuccess, outcome, completedID, completedPhase)
			if finishErr != nil {
				return completedID, completedPhase, fmt.Errorf("reconcileMissing: %w", finishErr)
			}
			return id, phase, nil
		}
		if readErr != nil {
			if state.Phase == "failed" {
				if state.ID != completedID || state.Phase != completedPhase {
					e.server.logger.Printf("Failed maintenance has no readable supervisor outcome operation=%s: %v", state.ID, readErr)
				}
				id, phase, finishErr := e.finishReconciliation(ctx, state, false, outcome, completedID, completedPhase)
				if finishErr != nil {
					return completedID, completedPhase, fmt.Errorf("reconcileUnavailable: %w", finishErr)
				}
				return id, phase, nil
			}
			return completedID, completedPhase, fmt.Errorf("reconcileResult: %w", readErr)
		}
		if result.OperationID != state.ID || result.Action != state.Action {
			id, phase, finishErr := e.finishReconciliation(ctx, state, isSuccess, outcome, completedID, completedPhase)
			if finishErr != nil {
				return completedID, completedPhase, fmt.Errorf("reconcileBinding: %w", finishErr)
			}
			return id, phase, nil
		}
		if state.Phase == "failed" && result.Phase == "failed" && state.ID == completedID && state.Phase == completedPhase {
			return completedID, completedPhase, nil
		}
		if result.Phase != "healthy" && result.Phase != "rolled_back" && result.Phase != "failed" {
			return completedID, completedPhase, nil
		}
		if result.IsSuccess == nil {
			return completedID, completedPhase, errors.New("final maintenance result has no outcome")
		}
		lease, gateErr := e.gate.Change(ctx)
		if gateErr != nil {
			return completedID, completedPhase, fmt.Errorf("reconcileGate: %w", gateErr)
		}
		resolveErr := current.control.Resolve(state.ID, result.Phase)
		resolvedState := current.control.Status()
		e.closeLease(lease)
		if resolveErr != nil {
			return completedID, completedPhase, fmt.Errorf("reconcileResolve: %w", resolveErr)
		}
		if resolvedState.ID == state.ID {
			state = resolvedState
		}
		isSuccess = result.Phase == "healthy" && result.IsSuccess != nil && *result.IsSuccess
		outcome = result.Phase
		if !isSuccess && result.Phase == "healthy" {
			outcome = "update_failed"
		}
	}
	id, phase, finishErr := e.finishReconciliation(ctx, state, isSuccess, outcome, completedID, completedPhase)
	if finishErr != nil {
		return completedID, completedPhase, fmt.Errorf("reconcileFinish: %w", finishErr)
	}
	return id, phase, nil
}

func (e *panelMaintenance) finishReconciliation(ctx context.Context, state maintenance.State, isSuccess bool, outcome, completedID, completedPhase string) (string, string, error) {
	if outcome == "" || state.ID == completedID && state.Phase == completedPhase {
		return completedID, completedPhase, nil
	}
	err := e.complete(ctx, state.ID, isSuccess, outcome)
	if err != nil {
		return completedID, completedPhase, fmt.Errorf("reconcileAudit: %w", err)
	}
	return state.ID, state.Phase, nil
}
