//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/scenario/desktop"
)

func writeScenarioWorkerEvidence(ctx context.Context, directory, name string, record any) (scenario.Artifact, error) {
	err := ctx.Err()
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("evidenceContext: %w", err)
	}
	err = desktop.CheckAncestors(directory)
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("evidenceDirectory: %w", err)
	}
	if filepath.Base(name) != name {
		return scenario.Artifact{}, errors.New("evidence name must be a single filename")
	}
	payload, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("evidenceMarshal: %w", err)
	}
	payload = append(payload, '\n')
	path := filepath.Join(directory, name)
	w, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("evidenceCreate: %w", err)
	}
	count, writeErr := w.Write(payload)
	if writeErr == nil && count != len(payload) {
		writeErr = errors.New("short worker evidence write")
	}
	syncErr := w.Sync()
	closeErr := w.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return scenario.Artifact{}, fmt.Errorf("evidenceWrite: %w", errors.Join(writeErr, syncErr, closeErr))
	}
	artifact, err := desktop.HashArtifact(ctx, "client_launch", path)
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("evidenceHash: %w", err)
	}
	return artifact, nil
}
