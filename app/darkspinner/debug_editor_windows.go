//go:build scenario && windows

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/darkspinnet/darkspin/server/scenario/desktop"
)

var debugEditorArtifactPattern = regexp.MustCompile(`^editor-ui-[0-9a-f]{8}\.json$`)

func (e *debugClientManager) OpenCreatureEditor(ctx context.Context, clientID string, heroNoun uint32) (any, error) {
	result, err := e.requestWindow(ctx, clientID, "editor.open_creature", controlCommand{HeroNoun: heroNoun})
	if err != nil {
		return result, fmt.Errorf("creatureOpen: %w", err)
	}
	return result, nil
}

func (e *debugClientManager) EditorHierarchy(ctx context.Context, clientID string) (any, error) {
	state, err := e.State(clientID)
	if err != nil {
		return nil, fmt.Errorf("editorClient: %w", err)
	}
	result, err := e.requestWindow(ctx, clientID, "editor.hierarchy", controlCommand{})
	if err != nil {
		return result, fmt.Errorf("editorObserve: %w", err)
	}
	fields, isObject := result.(map[string]any)
	if !isObject {
		return result, errors.New("editor hierarchy receipt is not an object")
	}
	artifact, isArtifact := fields["editor_hierarchy"].(*controlEditorArtifact)
	if !isArtifact || artifact == nil || artifact.NodeCount == 0 || artifact.NodeCount > 4096 || artifact.OwnerGeneration == 0 {
		return result, errors.New("editor hierarchy artifact metadata is missing or invalid")
	}
	if !strings.EqualFold(filepath.Clean(filepath.Dir(artifact.Path)), filepath.Clean(filepath.Dir(state.TracePath))) ||
		!debugEditorArtifactPattern.MatchString(filepath.Base(artifact.Path)) {
		return result, errors.New("editor hierarchy path is outside this client's owned report directory")
	}
	err = desktop.CheckRegular(artifact.Path)
	if err != nil {
		return result, fmt.Errorf("editorFile: %w", err)
	}
	r, err := os.Open(artifact.Path)
	if err != nil {
		return result, fmt.Errorf("editorOpen: %w", err)
	}
	payload, readErr := io.ReadAll(io.LimitReader(r, 1024*1024+1))
	closeErr := r.Close()
	if readErr != nil || closeErr != nil {
		return result, fmt.Errorf("editorRead: %w", errors.Join(readErr, closeErr))
	}
	if len(payload) > 1024*1024 {
		return result, errors.New("editor hierarchy exceeds the artifact size limit")
	}
	digest := sha256.Sum256(payload)
	if hex.EncodeToString(digest[:]) != artifact.SHA256 {
		return result, errors.New("editor hierarchy artifact digest differs from the receipt")
	}
	var hierarchy debugEditorHierarchy
	err = json.Unmarshal(payload, &hierarchy)
	if err != nil {
		return result, fmt.Errorf("editorDecode: %w", err)
	}
	if hierarchy.PID != state.ProcessID || hierarchy.OwnerGeneration != artifact.OwnerGeneration ||
		len(hierarchy.Nodes) != int(artifact.NodeCount) || hierarchy.IsComplete != artifact.IsComplete || hierarchy.Source != "native-editor-spui" {
		return result, errors.New("editor hierarchy identity differs from its owned receipt")
	}
	for index := range hierarchy.Nodes {
		node := &hierarchy.Nodes[index]
		if (index == 0 && node.ParentIndex != 0) || (index > 0 && (node.ParentIndex == 0 || node.ParentIndex > uint32(index))) {
			return result, errors.New("editor hierarchy has an invalid parent index")
		}
		node.IsVisible = node.Flags&1 != 0
		node.IsEnabled = node.Flags&2 != 0
		if node.ParentIndex != 0 {
			parent := hierarchy.Nodes[node.ParentIndex-1]
			node.IsVisible = node.IsVisible && parent.IsVisible
			node.IsEnabled = node.IsEnabled && parent.IsEnabled
		}
	}
	hierarchy.ClientID, hierarchy.InstanceID = state.ClientID, state.InstanceID
	hierarchy.Path, hierarchy.SHA256 = artifact.Path, artifact.SHA256
	return hierarchy, nil
}
