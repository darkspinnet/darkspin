//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type controlEditorArtifact struct {
	Path            string `json:"path"`
	SHA256          string `json:"sha256"`
	NodeCount       uint32 `json:"node_count"`
	OwnerGeneration uint32 `json:"owner_generation"`
	IsComplete      bool   `json:"is_complete"`
}

type debugEditorNode struct {
	ID          uint32     `json:"id"`
	Command     uint32     `json:"command"`
	Flags       uint32     `json:"flags"`
	ParentIndex uint32     `json:"parent_index"`
	Depth       uint32     `json:"depth"`
	Bounds      [4]float32 `json:"bounds"`
	IsVisible   bool       `json:"is_visible"`
	IsEnabled   bool       `json:"is_enabled"`
}

type debugEditorHierarchy struct {
	ClientID                string            `json:"client_id"`
	InstanceID              string            `json:"instance_id"`
	PID                     uint32            `json:"pid"`
	Path                    string            `json:"path"`
	SHA256                  string            `json:"sha256"`
	Source                  string            `json:"source"`
	ObservedAt              time.Time         `json:"observed_at"`
	OwnerGeneration         uint32            `json:"owner_generation"`
	IsComplete              bool              `json:"is_complete"`
	IsBodyMode              bool              `json:"is_body_mode"`
	EditorMode              uint32            `json:"editor_mode"`
	IsModEditor             bool              `json:"is_mod_editor"`
	IsImportEditor          bool              `json:"is_import_editor"`
	IsEnvironmentRegistered bool              `json:"is_environment_registered"`
	Nodes                   []debugEditorNode `json:"nodes"`
}

func openDebugCreatureEditor(ctx context.Context, host *debugHost, clientManager *debugClientManager, payload json.RawMessage) (any, error) {
	if host == nil || clientManager == nil || !host.IsModded {
		return nil, errors.New("Creature Editor requires an owned Mod server and client")
	}
	var req struct {
		ClientID       string `json:"client_id"`
		HeroNoun       uint32 `json:"hero_noun"`
		IsImportEditor bool   `json:"is_import_editor"`
		TimeoutMS      uint32 `json:"timeout_ms"`
	}
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return nil, fmt.Errorf("editorRequest: %w", err)
	}
	if req.HeroNoun == 0 || req.TimeoutMS == 0 || req.TimeoutMS > 120000 {
		return nil, errors.New("Creature Editor requires a hero noun and timeout between 1 and 120000 milliseconds")
	}
	waitContext, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()
	client, err := clientManager.State(req.ClientID)
	if err != nil {
		return nil, fmt.Errorf("editorOwner: %w", err)
	}
	if !client.IsModded || !client.IsRunning {
		return nil, errors.New("Creature Editor requires a running owned Mod client")
	}
	profile, err := host.ProfileState(waitContext, client.ProfileName)
	if err != nil {
		return nil, fmt.Errorf("editorProfile: %w", err)
	}
	if !profile.IsAuthenticated || profile.Presence.Location != "arsenal" || profile.Presence.Status != "available" {
		return nil, errors.New("Creature Editor requires an authenticated client in Arsenal")
	}
	action, err := clientManager.OpenCreatureEditor(waitContext, req.ClientID, req.HeroNoun, req.IsImportEditor)
	result := map[string]any{"action": action}
	if err != nil {
		return result, fmt.Errorf("editorOpen: %w", err)
	}
	waitPayload, err := json.Marshal(debugClientSceneRequest{ClientID: req.ClientID, Location: "editor", TimeoutMS: req.TimeoutMS, AfterRevision: profile.Presence.Revision})
	if err != nil {
		return result, fmt.Errorf("editorWaitPayload: %w", err)
	}
	observation, err := waitDebugClientReady(waitContext, host, clientManager, "client.wait_scene", waitPayload)
	result["observation"] = observation
	if err != nil {
		return result, fmt.Errorf("editorWait: %w", err)
	}
	hierarchy, err := clientManager.EditorHierarchy(waitContext, req.ClientID)
	result["hierarchy"] = hierarchy
	if err != nil {
		return result, fmt.Errorf("editorVerify: %w", err)
	}
	snapshot, isHierarchy := hierarchy.(debugEditorHierarchy)
	if !isHierarchy || !snapshot.IsModEditor {
		return result, errors.New("editor opened without the requested Mod creature-editor ownership")
	}
	if snapshot.IsImportEditor != req.IsImportEditor {
		return result, errors.New("editor opened through a different entry than requested")
	}
	return result, nil
}
