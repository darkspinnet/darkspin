//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario/control"
	"github.com/darkspinnet/darkspin/server/scenario/desktop"
)

var debugDiagnosticLabelPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,47}$`)

type debugDiagnosticRequest struct {
	ClientID string `json:"client_id"`
	Label    string `json:"label"`
}

// Explicit local diagnostic DTOs; never marshal credentials, process owners or
// persisted user aggregates. Samples are sequential and are not an atomic dump.
type debugDiagnostic struct {
	Schema       int                    `json:"schema"`
	Label        string                 `json:"label"`
	IsComplete   bool                   `json:"is_complete"`
	IsAtomic     bool                   `json:"is_atomic"`
	StartedAt    time.Time              `json:"started_at"`
	FinishedAt   time.Time              `json:"finished_at"`
	ManifestPath string                 `json:"manifest_path"`
	Client       debugOwnedClientDTO    `json:"client"`
	Profile      *debugProfileState     `json:"profile"`
	World        *debugWorldState       `json:"world"`
	Window       any                    `json:"window"`
	Screenshot   *debugScreenshot       `json:"screenshot"`
	Traces       []debugDiagnosticTrace `json:"traces"`
	Failures     map[string]*debugError `json:"failures"`
}

type debugDiagnosticCapture struct {
	result   debugDiagnostic
	failures map[string]*control.ScenarioError
}

func (e *debugDiagnosticCapture) record(name string, err error) {
	if err == nil {
		return
	}
	failure := &control.ScenarioError{Context: name, Cause: err}
	e.failures[name] = failure
	e.result.Failures[name] = debugErrorDTO(failure, 0)
	e.result.IsComplete = false
}

func (e *debugSupervisor) captureDebugDiagnostic(ctx context.Context, host *debugHost, clientManager *debugClientManager, payload json.RawMessage) (debugDiagnostic, error) {
	capture := debugDiagnosticCapture{result: debugDiagnostic{Schema: 1,
		StartedAt: time.Now().UTC(), Traces: []debugDiagnosticTrace{}, Failures: make(map[string]*debugError)}, failures: make(map[string]*control.ScenarioError)}
	if clientManager == nil || ctx == nil {
		return capture.result, errors.New("diagnostic capture requires an owned client manager and context")
	}
	var req debugDiagnosticRequest
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return capture.result, fmt.Errorf("diagnosticRequest: %w", err)
	}
	if !debugDiagnosticLabelPattern.MatchString(req.Label) {
		return capture.result, &control.ScenarioError{Code: control.InvalidRequest, Context: "diagnosticLabel", Message: "capture label requires 1 to 48 lowercase letters, digits, underscores or hyphens, starting with a letter or digit"}
	}
	client, err := clientManager.State(req.ClientID)
	if err != nil {
		return capture.result, fmt.Errorf("diagnosticClient: %w", err)
	}
	capture.result.Label = req.Label
	capture.result.Client = debugOwnedClientResponse(client)
	err = e.reserveDebugDiagnostic(ctx)
	if err != nil {
		return capture.result, fmt.Errorf("diagnosticBudget: %w", err)
	}
	// Launch allocates a fresh suffixed directory for each process instance;
	// the logical client alias alone never identifies its artifact directory.
	parent := filepath.Dir(client.TracePath)
	relative, err := filepath.Rel(filepath.Join(e.reportDirectory, "clients"), parent)
	if err != nil {
		return capture.result, fmt.Errorf("diagnosticRelative: %w", err)
	}
	if !filepath.IsAbs(client.TracePath) || relative == "." || relative == ".." ||
		strings.ContainsAny(relative, `/\`) {
		return capture.result, errors.New("diagnostic client directory is outside its owned report root")
	}
	err = desktop.CheckAncestors(parent)
	if err != nil {
		return capture.result, fmt.Errorf("diagnosticParent: %w", err)
	}
	directory, err := os.MkdirTemp(parent, "capture-"+req.Label+"-")
	if err != nil {
		return capture.result, fmt.Errorf("diagnosticDirectory: %w", err)
	}
	err = desktop.CheckAncestors(directory)
	if err != nil {
		return capture.result, fmt.Errorf("diagnosticPath: %w", err)
	}
	captureContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if client.IsRunning {
		if host == nil {
			capture.record("profile", errors.New("owned server is unavailable"))
		} else {
			queryContext, queryCancel := context.WithTimeout(captureContext, 2*time.Second)
			profile, profileErr := host.ProfileState(queryContext, client.ProfileName)
			queryCancel()
			if profileErr == nil {
				capture.result.Profile = &profile
			}
			capture.record("profile", profileErr)
			world, worldErr := observeDebugWorld(captureContext, host, clientManager, json.RawMessage(fmt.Sprintf("{\"client_id\":%q}", req.ClientID)))
			if worldErr == nil {
				capture.result.World = &world
			}
			capture.record("world", worldErr)
		}
		capture.result.Window, err = clientManager.InspectWindow(captureContext, req.ClientID)
		capture.record("window", err)
		screenshot, screenshotErr := clientManager.Screenshot(captureContext, req.ClientID, req.Label)
		capture.result.Screenshot = &screenshot
		capture.record("screenshot", screenshotErr)
	} else {
		// A stopped handle still owns its retained traces. Do not attribute a
		// reset alias's new account or another process's pixels to this client.
		capture.record("client", errors.New("owned client has stopped; live profile, window and screenshot were not sampled"))
	}
	trace := captureDebugDiagnosticTrace(captureContext, directory, "client", client.TracePath)
	capture.result.Traces = append(capture.result.Traces, trace.result)
	capture.record("clientTrace", trace.err)
	serverTrace := filepath.Join(debugWorkspaceRoot(e.gameDirectory), "bin", "server", "darkspin", "logs", "traces", e.id, "server.jsonl")
	serverLog := filepath.Join(e.reportDirectory, "server.log")
	if host != nil {
		serverTrace = host.serverTracePath
		serverLog = host.serverLogPath
	}
	trace = captureDebugDiagnosticTrace(captureContext, directory, "server", serverTrace)
	capture.result.Traces = append(capture.result.Traces, trace.result)
	capture.record("serverTrace", trace.err)
	trace = captureDebugDiagnosticTrace(captureContext, directory, "server-log", serverLog)
	capture.result.Traces = append(capture.result.Traces, trace.result)
	capture.record("serverLog", trace.err)
	current, stateErr := clientManager.State(req.ClientID)
	capture.record("clientRecheck", stateErr)
	if stateErr == nil && (current.InstanceID != client.InstanceID || current.ProcessID != client.ProcessID || current.ProfileName != client.ProfileName || current.IsRunning != client.IsRunning) {
		capture.record("identity", &control.ScenarioError{Code: control.OwnershipLost, Context: "diagnosticIdentity", Message: "owned client changed or stopped during diagnostic capture"})
	}
	if host != nil && capture.result.Profile != nil {
		credential, credentialErr := host.ProfileCredential(client.ProfileName)
		capture.record("profileRecheck", credentialErr)
		if credentialErr == nil && credential.UserID != capture.result.Profile.UserID {
			capture.record("profileIdentity", errors.New("profile alias changed account during diagnostic capture"))
		}
	}
	capture.record("deadline", captureContext.Err())
	capture.result.IsComplete = len(capture.failures) == 0
	capture.result.FinishedAt = time.Now().UTC()
	capture.result.ManifestPath = filepath.Join(directory, "manifest.json")
	manifest, marshalErr := json.MarshalIndent(capture.result, "", "  ")
	if marshalErr != nil {
		capture.result.ManifestPath = ""
		capture.record("manifestMarshal", marshalErr)
		return capture.result, fmt.Errorf("diagnosticMarshal: %w", marshalErr)
	}
	// A small local manifest may finalize after cancellation; this authorizes
	// no additional native request, screenshot, or packet read.
	writeErr := writeDebugDiagnosticFile(directory, "manifest.json", append(manifest, '\n'), 128*1024)
	if writeErr != nil {
		capture.result.ManifestPath = ""
		capture.record("manifest", writeErr)
	}
	if !capture.result.IsComplete {
		return capture.result, &control.ScenarioError{Code: control.NotAvailable, Context: "diagnosticCapture", Message: "diagnostic capture is incomplete; retained artifacts and failures are available in the result", Failures: capture.failures}
	}
	return capture.result, nil
}

func (e *debugSupervisor) reserveDebugDiagnostic(ctx context.Context) error {
	err := e.lockDebugOperations(ctx)
	if err != nil {
		return fmt.Errorf("diagnosticLock: %w", err)
	}
	defer e.operationMutex.Unlock()
	if e.diagnosticCount >= 32 {
		return errors.New("session diagnostic capture limit of 32 is exhausted")
	}
	e.diagnosticCount++
	return nil
}
