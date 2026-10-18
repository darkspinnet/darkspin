//go:build scenario

package jsonstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/darkspinnet/darkspin/server/scenario"
)

func (e *Store) PreserveInput(ctx context.Context, paths scenario.RunPaths, input scenario.Artifact) (scenario.Artifact, error) {
	err := e.validatePaths(paths)
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("inputOwner: %w", err)
	}
	err = ctx.Err()
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("inputContext: %w", err)
	}
	if input.Role != "scenario_definition" || input.Path == "" || len(input.SHA256) != 64 {
		return scenario.Artifact{}, errors.New("parsed scenario input provenance unavailable")
	}
	r, err := os.Open(input.Path)
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("inputOpen: %w", err)
	}
	fi, statErr := r.Stat()
	if statErr != nil || !fi.Mode().IsRegular() {
		closeErr := r.Close()
		if statErr == nil {
			statErr = errors.New("scenario input must be a regular file")
		}
		return scenario.Artifact{}, fmt.Errorf("inputStat: %w", errors.Join(statErr, closeErr))
	}
	payload, readErr := io.ReadAll(io.LimitReader(r, 1024*1024+1))
	closeErr := r.Close()
	if readErr != nil || closeErr != nil {
		return scenario.Artifact{}, fmt.Errorf("inputRead: %w", errors.Join(readErr, closeErr))
	}
	digest := sha256.Sum256(payload)
	if len(payload) > 1024*1024 || hex.EncodeToString(digest[:]) != input.SHA256 {
		return scenario.Artifact{}, errors.New("scenario input changed since strict parsing")
	}
	err = checkAncestors(paths.ReportDirectory)
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("inputAncestors: %w", err)
	}
	input.Path = filepath.Join(paths.ReportDirectory, "scenario-definition.json")
	w, err := os.OpenFile(input.Path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0400)
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("inputExclusive: %w", err)
	}
	count, writeErr := w.Write(payload)
	if writeErr == nil && count != len(payload) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = w.Sync()
	}
	closeErr = w.Close()
	if writeErr != nil || closeErr != nil {
		removeErr := os.Remove(input.Path)
		return scenario.Artifact{}, fmt.Errorf("inputWrite: %w", errors.Join(writeErr, closeErr, removeErr))
	}
	return input, nil
}

func (e *Store) VerifyEvidence(ctx context.Context, paths scenario.RunPaths, artifacts []scenario.Artifact) error {
	err := e.validatePaths(paths)
	if err != nil {
		return fmt.Errorf("evidenceOwner: %w", err)
	}
	if len(artifacts) == 0 {
		return errors.New("no run evidence supplied")
	}
	for index, artifact := range artifacts {
		err = e.verifyArtifact(ctx, paths, artifact)
		if err != nil {
			return fmt.Errorf("evidenceArtifact[%d]: %w", index, err)
		}
	}
	return nil
}

func (e *Store) validatePaths(paths scenario.RunPaths) error {
	e.mutex.Lock()
	allocatedPaths, isOwned := e.allocations[paths.RunID]
	e.mutex.Unlock()
	if !isOwned || allocatedPaths != paths {
		return errors.New("evidence paths were not allocated by this store")
	}
	return nil
}

func (e *Store) verifyArtifact(ctx context.Context, paths scenario.RunPaths, artifact scenario.Artifact) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("evidenceContext: %w", err)
	}
	if artifact.Role == "" || !filepath.IsAbs(artifact.Path) {
		return errors.New("evidence requires a role and absolute run-owned path")
	}
	digestBytes, err := hex.DecodeString(artifact.SHA256)
	if err != nil {
		return fmt.Errorf("evidenceDigest: %w", err)
	}
	if len(digestBytes) != sha256.Size {
		return errors.New("evidence SHA-256 digest must contain 32 bytes")
	}
	isContained := false
	for _, directory := range []string{paths.CacheDirectory, paths.ReportDirectory, paths.TraceDirectory} {
		relativePath, relativeErr := filepath.Rel(directory, artifact.Path)
		if relativeErr != nil {
			continue // A different volume cannot contain this run's evidence.
		}
		if relativePath != "." && relativePath != ".." && !filepath.IsAbs(relativePath) &&
			!strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
			isContained = true
			break
		}
	}
	if !isContained {
		return errors.New("evidence must reside beneath this run's isolated directories")
	}
	err = checkAncestors(artifact.Path)
	if err != nil {
		return fmt.Errorf("evidenceAncestors: %w", err)
	}
	r, err := os.Open(artifact.Path)
	if err != nil {
		return fmt.Errorf("evidenceOpen: %w", err)
	}
	fi, statErr := r.Stat()
	if statErr != nil || !fi.Mode().IsRegular() {
		closeErr := r.Close()
		if statErr == nil {
			statErr = errors.New("evidence must be a regular file")
		}
		return fmt.Errorf("evidenceStat: %w", errors.Join(statErr, closeErr))
	}
	hasher := sha256.New()
	count, copyErr := io.Copy(hasher, r)
	closeErr := r.Close()
	if copyErr != nil || closeErr != nil {
		return fmt.Errorf("evidenceRead: %w", errors.Join(copyErr, closeErr))
	}
	if count != fi.Size() || hex.EncodeToString(hasher.Sum(nil)) != strings.ToLower(artifact.SHA256) {
		return errors.New("evidence is incomplete or differs from its immutable digest")
	}
	err = ctx.Err()
	if err != nil {
		return fmt.Errorf("evidenceFinished: %w", err)
	}
	return nil
}
