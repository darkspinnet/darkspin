//go:build windows && cgo && scenario && fangdebug

package main

/*
#include "control.h"
*/
import "C"

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

type controlEditorArtifact struct {
	Path            string `json:"path"`
	SHA256          string `json:"sha256"`
	NodeCount       uint32 `json:"node_count"`
	OwnerGeneration uint32 `json:"owner_generation"`
	IsComplete      bool   `json:"is_complete"`
}

type controlEditorNode struct {
	ID          uint32     `json:"id"`
	Command     uint32     `json:"command"`
	Flags       uint32     `json:"flags"`
	ParentIndex uint32     `json:"parent_index"`
	Depth       uint32     `json:"depth"`
	Bounds      [4]float32 `json:"bounds"`
}

var editorArtifactCount atomic.Uint32

func saveControlEditorHierarchy(requestID uint32) (*controlEditorArtifact, error) {
	var snapshot C.fang_control_editor_tree
	isFound := C.fang_control_window_editor_tree_result(C.uint32_t(requestID), &snapshot)
	if isFound == 0 || snapshot.is_available == 0 {
		return nil, errors.New("native editor hierarchy is unavailable")
	}
	if editorArtifactCount.Add(1) > 128 {
		return nil, errors.New("editor hierarchy artifact limit reached")
	}
	nodes := make([]controlEditorNode, int(snapshot.node_count))
	for index := range nodes {
		node := snapshot.nodes[index]
		nodes[index] = controlEditorNode{ID: uint32(node.id), Command: uint32(node.command), Flags: uint32(node.flags),
			ParentIndex: uint32(node.parent_index), Depth: uint32(node.depth), Bounds: [4]float32{float32(node.left), float32(node.top), float32(node.right), float32(node.bottom)}}
	}
	payload, err := json.Marshal(struct {
		Source                  string              `json:"source"`
		PID                     int                 `json:"pid"`
		ObservedAt              time.Time           `json:"observed_at"`
		OwnerGeneration         uint32              `json:"owner_generation"`
		IsComplete              bool                `json:"is_complete"`
		IsBodyMode              bool                `json:"is_body_mode"`
		EditorMode              uint32              `json:"editor_mode"`
		IsModEditor             bool                `json:"is_mod_editor"`
		IsImportEditor          bool                `json:"is_import_editor"`
		IsEnvironmentRegistered bool                `json:"is_environment_registered"`
		Nodes                   []controlEditorNode `json:"nodes"`
	}{"native-editor-spui", os.Getpid(), time.Now().UTC(), uint32(snapshot.owner_generation), snapshot.is_complete != 0, snapshot.is_body_mode != 0, uint32(snapshot.editor_mode), snapshot.is_mod_editor != 0, snapshot.is_import_editor != 0, snapshot.is_environment_registered != 0, nodes})
	if err != nil {
		return nil, fmt.Errorf("treeMarshal: %w", err)
	}
	tracePath := os.Getenv("DARKSPIN_CLIENT_TRACE")
	if !filepath.IsAbs(tracePath) {
		return nil, errors.New("editor hierarchy requires an owned absolute trace directory")
	}
	path := filepath.Join(filepath.Dir(tracePath), fmt.Sprintf("editor-ui-%08x.json", requestID))
	w, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("treeCreate: %w", err)
	}
	writtenCount, writeErr := w.Write(payload)
	closeErr := w.Close()
	if writeErr != nil || closeErr != nil {
		return nil, fmt.Errorf("treeWrite: %w", errors.Join(writeErr, closeErr))
	}
	if writtenCount != len(payload) {
		return nil, errors.New("short editor hierarchy write")
	}
	digest := sha256.Sum256(payload)
	return &controlEditorArtifact{Path: path, SHA256: hex.EncodeToString(digest[:]), NodeCount: uint32(len(nodes)),
		OwnerGeneration: uint32(snapshot.owner_generation), IsComplete: snapshot.is_complete != 0}, nil
}
