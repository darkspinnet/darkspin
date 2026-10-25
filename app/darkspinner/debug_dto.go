//go:build scenario

package main

import (
	"errors"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario/control"
)

type debugStatus struct {
	ID                  string               `json:"id"`
	State               string               `json:"state"`
	LeaseOwner          string               `json:"lease_owner"`
	LeaseGeneration     uint64               `json:"lease_generation"`
	CreatedAt           time.Time            `json:"created_at"`
	InspectionExpiresAt time.Time            `json:"inspection_expires_at"`
	Clients             []debugClient        `json:"clients"`
	ScriptResult        *debugScriptResult   `json:"script_result"`
	Automation          debugAutomationState `json:"automation"`
}

type debugScriptResult struct {
	RelativePath string      `json:"relative_path"`
	SHA256       string      `json:"sha256"`
	Outcome      string      `json:"outcome"`
	StartedAt    time.Time   `json:"started_at"`
	FinishedAt   time.Time   `json:"finished_at"`
	Error        string      `json:"error,omitempty"`
	Failure      *debugError `json:"failure,omitempty"`
	Traceback    string      `json:"traceback,omitempty"`
}

type debugError struct {
	Code             string                 `json:"code,omitempty"`
	Context          string                 `json:"context,omitempty"`
	Message          string                 `json:"message"`
	ClientInstanceID string                 `json:"client_instance_id,omitempty"`
	ActionID         string                 `json:"action_id,omitempty"`
	Cause            *debugError            `json:"cause,omitempty"`
	Failures         map[string]*debugError `json:"failures,omitempty"`
	Details          any                    `json:"details,omitempty"`
}

func debugErrorDTO(failure error, depth int) *debugError {
	if failure == nil {
		return nil
	}
	if depth >= 16 {
		return &debugError{Message: "error chain exceeds response depth limit"}
	}
	typedFailure, isTyped := failure.(*control.ScenarioError)
	if isTyped {
		var failures map[string]*debugError
		if len(typedFailure.Failures) > 0 {
			failures = make(map[string]*debugError, len(typedFailure.Failures))
			for name, branchFailure := range typedFailure.Failures {
				failures[name] = debugErrorDTO(branchFailure, depth+1)
			}
		}
		return &debugError{Code: string(typedFailure.Code), Context: typedFailure.Context,
			Message: typedFailure.Message, ClientInstanceID: typedFailure.ClientInstanceID,
			ActionID: typedFailure.ActionID, Cause: debugErrorDTO(typedFailure.Cause, depth+1), Failures: failures}
	}
	message := failure.Error()
	cause := errors.Unwrap(failure)
	if cause == nil {
		// In particular, errors.Join has multiple causes and no single Unwrap
		// result. Preserve its complete authored message as one terminal detail.
		return &debugError{Message: message}
	}
	causeMessage := cause.Error()
	suffix := ": " + causeMessage
	if strings.HasSuffix(message, suffix) {
		// fmt.Errorf("step: %w", err) already includes the complete cause text.
		// Keep only this layer's local breadcrumb so reconstructing/tostring
		// appends each cause exactly once instead of expanding the whole chain.
		return &debugError{Context: strings.TrimSuffix(message, suffix), Cause: debugErrorDTO(cause, depth+1)}
	}
	if message == causeMessage {
		return &debugError{Cause: debugErrorDTO(cause, depth+1)}
	}
	// A custom wrapper may add details in a different format. Preserve those
	// details and the cause rather than guessing how to remove part of them.
	return &debugError{Message: message, Cause: debugErrorDTO(cause, depth+1)}
}

type debugClient struct {
	Name                 string           `json:"name"`
	InstanceID           string           `json:"instance_id"`
	PID                  uint32           `json:"pid"`
	ConnectionGeneration uint64           `json:"connection_generation"`
	ExecutableSHA256     string           `json:"executable_sha256"`
	Architecture         string           `json:"architecture"`
	BuildID              string           `json:"build_id"`
	ObservedAt           time.Time        `json:"observed_at"`
	Sequence             uint64           `json:"sequence"`
	Bridge               debugObservation `json:"bridge"`
	FrameSequence        debugObservation `json:"frame_sequence"`
	SceneAssetID         debugObservation `json:"scene_asset_id"`
	SceneName            debugObservation `json:"scene_name"`
	ControlledHeroID     debugObservation `json:"controlled_hero_id"`
	SelectedHeroSlot     debugObservation `json:"selected_hero_slot"`
}

type debugObservation struct {
	Status     string    `json:"status"`
	Reason     string    `json:"reason,omitempty"`
	ObservedAt time.Time `json:"observed_at"`
	Sequence   uint64    `json:"sequence"`
	Data       any       `json:"data"`
}

func debugObservationDTO(availability control.Availability, data any) debugObservation {
	if availability.Status == control.Available && !availability.ObservedAt.IsZero() && time.Since(availability.ObservedAt) > 2*time.Second {
		availability.Status = control.Stale
		availability.Reason = "observation_heartbeat_expired"
	}
	if availability.Status != control.Available {
		data = nil
	}
	return debugObservation{Status: string(availability.Status), Reason: availability.Reason,
		ObservedAt: availability.ObservedAt, Sequence: availability.Sequence, Data: data}
}

func debugClientDTO(client control.ClientState) debugClient {
	return debugClient{Name: client.Identity.Name, InstanceID: client.Identity.InstanceID,
		PID: client.Identity.PID, ConnectionGeneration: client.Identity.ConnectionGeneration,
		ExecutableSHA256: client.Identity.ExecutableSHA256, Architecture: client.Identity.Architecture,
		BuildID: client.Identity.BuildID, ObservedAt: client.ObservedAt, Sequence: client.Sequence,
		Bridge:           debugObservationDTO(client.Bridge, nil),
		FrameSequence:    debugObservationDTO(client.FrameSequence.Availability, client.FrameSequence.Data),
		SceneAssetID:     debugObservationDTO(client.SceneAssetID.Availability, client.SceneAssetID.Data),
		SceneName:        debugObservationDTO(client.SceneName.Availability, client.SceneName.Data),
		ControlledHeroID: debugObservationDTO(client.ControlledHeroID.Availability, client.ControlledHeroID.Data),
		SelectedHeroSlot: debugObservationDTO(client.SelectedHeroSlot.Availability, client.SelectedHeroSlot.Data)}
}

func debugStatusDTO(session control.Session) debugStatus {
	status := debugStatus{ID: session.ID, State: string(session.State), LeaseOwner: string(session.Lease.Owner),
		LeaseGeneration: session.Lease.Generation, CreatedAt: session.CreatedAt,
		InspectionExpiresAt: session.InspectionExpiresAt, Clients: make([]debugClient, 0, len(session.Clients))}
	for _, client := range session.Clients {
		status.Clients = append(status.Clients, debugClientDTO(client))
	}
	if session.ScriptResult != nil {
		result := session.ScriptResult
		status.ScriptResult = &debugScriptResult{RelativePath: result.Identity.RelativePath,
			SHA256: result.Identity.SHA256, Outcome: string(result.Outcome),
			StartedAt: result.StartedAt, FinishedAt: result.FinishedAt, Traceback: result.Traceback}
		if result.Failure != nil {
			status.ScriptResult.Error = result.Failure.Error()
			status.ScriptResult.Failure = debugErrorDTO(result.Failure, 0)
		}
	}
	return status
}
