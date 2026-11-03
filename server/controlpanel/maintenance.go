package controlpanel

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
	"github.com/darkspinnet/darkspin/server/maintenance"
)

type MaintenanceOutcome struct {
	OperationID      string
	Action           string
	Phase            string
	UpdatedAt        time.Time
	InstalledVersion string
	CandidateVersion string
	IsSuccess        *bool
	FailureCode      string
}

type MaintenanceSnapshot struct {
	State              maintenance.State
	IsRestartSupported bool
	IsUpdateSupported  bool
	Result             *MaintenanceOutcome
}

type MaintenanceController interface {
	Snapshot(context.Context) (MaintenanceSnapshot, error)
	Request(context.Context, accountaccess.Actor, maintenance.Request) (maintenance.State, error)
	Activate(context.Context, string) error
	Abort(context.Context, string) error
	Cancel(context.Context, accountaccess.Actor, string) (maintenance.State, error)
}

type publicMaintenanceDTO struct {
	ID                 string    `json:"id"`
	Action             string    `json:"action"`
	Reason             string    `json:"reason"`
	Phase              string    `json:"phase"`
	Deadline           time.Time `json:"deadline"`
	IsActive           bool      `json:"is_active"`
	IsAdmissionBlocked bool      `json:"is_admission_blocked"`
}

type maintenanceOutcomeDTO struct {
	OperationID      string    `json:"operation_id"`
	Action           string    `json:"action"`
	Phase            string    `json:"phase"`
	UpdatedAt        time.Time `json:"updated_at"`
	InstalledVersion string    `json:"installed_version"`
	CandidateVersion string    `json:"candidate_version"`
	IsSuccess        *bool     `json:"is_success"`
	FailureCode      string    `json:"failure_code"`
}

type maintenanceDTO struct {
	publicMaintenanceDTO
	Delivered          int                    `json:"delivered"`
	IsRestartSupported bool                   `json:"is_restart_supported"`
	IsUpdateSupported  bool                   `json:"is_update_supported"`
	Result             *maintenanceOutcomeDTO `json:"result,omitempty"`
}

func publicMaintenanceResponse(state maintenance.State) publicMaintenanceDTO {
	phase := state.Phase
	if phase == "" {
		phase = "idle"
	}
	return publicMaintenanceDTO{ID: state.ID, Action: state.Action, Reason: state.Reason,
		Phase: phase, Deadline: state.Deadline, IsActive: state.IsActive, IsAdmissionBlocked: state.IsAdmissionBlocked}
}

func maintenanceResponse(snapshot MaintenanceSnapshot) maintenanceDTO {
	result := maintenanceDTO{publicMaintenanceDTO: publicMaintenanceResponse(snapshot.State),
		Delivered: snapshot.State.Delivered, IsRestartSupported: snapshot.IsRestartSupported,
		IsUpdateSupported: snapshot.IsUpdateSupported}
	if snapshot.Result == nil || snapshot.Result.OperationID != snapshot.State.ID {
		return result
	}
	result.Result = &maintenanceOutcomeDTO{
		OperationID: snapshot.Result.OperationID, Action: snapshot.Result.Action, Phase: snapshot.Result.Phase,
		UpdatedAt: snapshot.Result.UpdatedAt, InstalledVersion: snapshot.Result.InstalledVersion,
		CandidateVersion: snapshot.Result.CandidateVersion, IsSuccess: snapshot.Result.IsSuccess,
		FailureCode: snapshot.Result.FailureCode,
	}
	if snapshot.State.Phase == "completed" {
		result.Phase = snapshot.Result.Phase
	}
	return result
}

func (e *Service) maintenanceOperation(writer http.ResponseWriter, req *http.Request, actor accountaccess.Actor) {
	if e.config.MaintenanceController == nil {
		writeErrorCode(writer, http.StatusNotFound, "maintenance_unsupported", "Maintenance controls are unavailable.")
		return
	}
	if req.Method == http.MethodGet {
		snapshot, err := e.config.MaintenanceController.Snapshot(req.Context())
		if err != nil {
			writeMaintenanceError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, maintenanceResponse(snapshot))
		return
	}
	if req.URL.Path == "/api/control/admin/maintenance/cancel" {
		e.cancelMaintenance(writer, req, actor)
		return
	}
	command := struct {
		Action         string `json:"action"`
		Reason         string `json:"reason"`
		WarningMinutes *int   `json:"warning_minutes"`
	}{}
	err := decodeControlRequest(writer, req, &command)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "Invalid maintenance request.")
		return
	}
	switch req.URL.Path {
	case "/api/control/admin/maintenance/restart":
		command.Action = "restart"
	case "/api/control/admin/maintenance/update":
		command.Action = "update"
	}
	minutes := 30
	if command.WarningMinutes != nil {
		minutes = *command.WarningMinutes
	}
	state, err := e.config.MaintenanceController.Request(req.Context(), actor, maintenance.Request{
		Action: command.Action, Reason: command.Reason, WarningMinutes: minutes,
	})
	if err != nil {
		writeMaintenanceError(writer, err)
		return
	}
	snapshot, err := e.config.MaintenanceController.Snapshot(req.Context())
	if err != nil {
		e.abortMaintenance(req.Context(), state.ID)
		writeMaintenanceError(writer, err)
		return
	}
	// Use the exact accepted operation even if another observer changes its phase.
	snapshot.State = state
	e.acceptMaintenance(writer, req, maintenanceResponse(snapshot))
}

func (e *Service) acceptMaintenance(writer http.ResponseWriter, req *http.Request, response maintenanceDTO) {
	payload, err := json.Marshal(response)
	if err != nil {
		e.abortMaintenance(req.Context(), response.ID)
		writeError(writer, http.StatusServiceUnavailable, "Maintenance acceptance could not be encoded.")
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	writer.WriteHeader(http.StatusAccepted)
	written, err := writer.Write(payload)
	if err == nil && written != len(payload) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = http.NewResponseController(writer).Flush()
	}
	if err != nil {
		e.abortMaintenance(req.Context(), response.ID)
		return
	}
	activationCtx, cancel := context.WithTimeout(context.WithoutCancel(req.Context()), 5*time.Second)
	defer cancel()
	err = e.config.MaintenanceController.Activate(activationCtx, response.ID)
	if err != nil {
		// Acceptance has already reached the caller. Fresh status, not this
		// response, reports the final operation; never claim a completed restart.
		log.Printf("Maintenance activation failed id=%s: %v", response.ID, err)
		e.abortMaintenance(activationCtx, response.ID)
	}
}

func (e *Service) abortMaintenance(ctx context.Context, id string) {
	abortCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := e.config.MaintenanceController.Abort(abortCtx, id)
	if err != nil {
		log.Printf("Maintenance acceptance abort failed id=%s: %v", id, err)
	}
}

func (e *Service) cancelMaintenance(writer http.ResponseWriter, req *http.Request, actor accountaccess.Actor) {
	command := struct {
		ID string `json:"id"`
	}{}
	err := decodeControlRequest(writer, req, &command)
	if err != nil || command.ID == "" {
		writeError(writer, http.StatusBadRequest, "The exact maintenance operation ID is required.")
		return
	}
	state, err := e.config.MaintenanceController.Cancel(req.Context(), actor, command.ID)
	if err != nil {
		writeMaintenanceError(writer, err)
		return
	}
	snapshot, err := e.config.MaintenanceController.Snapshot(req.Context())
	if err != nil {
		writeErrorCode(writer, http.StatusServiceUnavailable, "outcome_unknown", "Cancellation was accepted. Refresh maintenance status before trying again.")
		return
	}
	snapshot.State = state
	writeJSON(writer, http.StatusOK, maintenanceResponse(snapshot))
}

func writeMaintenanceError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, maintenance.ErrStopping):
		writeErrorCode(writer, http.StatusConflict, "maintenance_stopping", "Session cleanup or shutdown has begun. Cancellation is unavailable while it is in progress.")
	case errors.Is(err, maintenance.ErrUnsupported):
		writeErrorCode(writer, http.StatusBadRequest, "maintenance_unsupported", "Restart and update require a bootstrap-managed server.")
	case errors.Is(err, maintenance.ErrInvalidRequest):
		writeError(writer, http.StatusBadRequest, "Check the maintenance action, reason, and warning period.")
	case errors.Is(err, maintenance.ErrConflict):
		writeErrorCode(writer, http.StatusConflict, "maintenance_conflict", "Another maintenance operation is active or the operation changed. Refresh its status.")
	default:
		writeOperationError(writer, err)
	}
}
