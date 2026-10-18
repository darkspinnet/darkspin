//go:build scenario

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/gameplay"
	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/scenario/desktop"
)

func validateScenarioHostPaths(req desktop.StartRequest) error {
	if req.RunID == "" || req.RunID != req.Paths.RunID || strings.ContainsAny(req.RunID, "/\\") || req.RunID == "." || req.RunID == ".." {
		return errors.New("scenario run identity is invalid")
	}
	for _, directory := range []string{req.Paths.CacheDirectory, req.Paths.ReportDirectory, req.Paths.TraceDirectory} {
		if !filepath.IsAbs(directory) || filepath.Base(directory) != req.RunID {
			return errors.New("scenario host directories do not identify the allocated run")
		}
		err := scenarioHostAncestors(directory)
		if err != nil {
			return fmt.Errorf("runAncestors: %w", err)
		}
		fi, err := os.Lstat(directory)
		if err != nil {
			return fmt.Errorf("runStat: %w", err)
		}
		if !fi.IsDir() {
			return errors.New("scenario run directory is not a directory")
		}
	}
	cacheRoot := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(req.Paths.CacheDirectory))))
	expectedPaths := []string{
		filepath.Join(cacheRoot, "bin", "cache", "scenario", req.RunID),
		filepath.Join(cacheRoot, "bin", "game", "logs", "scenarios", req.RunID),
		filepath.Join(cacheRoot, "bin", "server", "darkspin", "logs", "traces", req.RunID),
	}
	actualPaths := []string{req.Paths.CacheDirectory, req.Paths.ReportDirectory, req.Paths.TraceDirectory}
	for index, expectedPath := range expectedPaths {
		if !strings.EqualFold(filepath.Clean(actualPaths[index]), expectedPath) {
			return errors.New("scenario host paths do not share the prescribed workspace roots")
		}
	}
	return nil
}

func scenarioHostAncestors(path string) error {
	for {
		fi, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("pathStat: %w", err)
		}
		if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return errors.New("scenario path contains a link or reparse point")
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}

func snapshotScenarioContent(ctx context.Context, sourcePath, destinationPath string) (scenario.Artifact, error) {
	err := scenarioHostAncestors(sourcePath)
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("sourceAncestors: %w", err)
	}
	r, err := openScenarioContent(sourcePath)
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("sourceOpen: %w", err)
	}
	artifact, copyErr := copyScenarioContent(ctx, r, sourcePath, destinationPath)
	closeErr := r.Close()
	if copyErr != nil || closeErr != nil {
		return scenario.Artifact{}, fmt.Errorf("sourceCopy: %w", errors.Join(copyErr, closeErr))
	}
	return artifact, nil
}

func copyScenarioContent(ctx context.Context, r *os.File, sourcePath, destinationPath string) (scenario.Artifact, error) {
	fi, err := r.Stat()
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("sourceStat: %w", err)
	}
	if !fi.Mode().IsRegular() || fi.Size() < 100 {
		return scenario.Artifact{}, errors.New("scenario content source is not a SQLite database file")
	}
	for _, suffix := range []string{"-wal", "-journal", "-shm"} {
		sidecarFI, sidecarErr := os.Lstat(sourcePath + suffix)
		if errors.Is(sidecarErr, os.ErrNotExist) {
			continue
		}
		if sidecarErr != nil {
			return scenario.Artifact{}, fmt.Errorf("sidecarStat: %w", sidecarErr)
		}
		return scenario.Artifact{}, fmt.Errorf("scenario content has SQLite sidecar %s; a coherent offline input is required", sidecarFI.Name())
	}
	header := make([]byte, 16)
	count, err := r.ReadAt(header, 0)
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("headerRead: %w", err)
	}
	if count != len(header) || !bytes.Equal(header, []byte("SQLite format 3\x00")) {
		return scenario.Artifact{}, errors.New("scenario content has an invalid SQLite header")
	}
	w, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("snapshotCreate: %w", err)
	}
	hash := sha256.New()
	count64, copyErr := copyScenarioBytes(ctx, io.MultiWriter(w, hash), r)
	if copyErr == nil && count64 != fi.Size() {
		copyErr = errors.New("scenario snapshot size differs from locked source")
	}
	syncErr := w.Sync()
	closeErr := w.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		return scenario.Artifact{}, fmt.Errorf("snapshotWrite: %w", errors.Join(copyErr, syncErr, closeErr))
	}
	return scenario.Artifact{Role: "content_input", Path: destinationPath,
		SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func copyScenarioBytes(ctx context.Context, w io.Writer, r io.Reader) (int64, error) {
	payload := make([]byte, 64*1024)
	var total int64
	for {
		err := ctx.Err()
		if err != nil {
			return total, fmt.Errorf("copyContext: %w", err)
		}
		count, readErr := r.Read(payload)
		if count != 0 {
			written, writeErr := w.Write(payload[:count])
			total += int64(written)
			if writeErr != nil {
				return total, fmt.Errorf("copyWrite: %w", writeErr)
			}
			if written != count {
				return total, io.ErrShortWrite
			}
		}
		if errors.Is(readErr, io.EOF) {
			return total, nil
		}
		if readErr != nil {
			return total, fmt.Errorf("copyRead: %w", readErr)
		}
	}
}

func (e *scenarioHost) waitReady(ctx context.Context, marker string) error {
	client := &http.Client{Timeout: 500 * time.Millisecond, Transport: &http.Transport{Proxy: nil},
		CheckRedirect: rejectScenarioReadyRedirect}
	defer client.CloseIdleConnections()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := e.checkReady(ctx, client, marker)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("readyContext: %w", ctx.Err())
		case <-e.done:
			if e.runErr != nil {
				return fmt.Errorf("readyServer: %w", e.runErr)
			}
			return errors.New("scenario server stopped before readiness")
		case <-ticker.C:
		}
	}
}

func rejectScenarioReadyRedirect(req *http.Request, priorRequests []*http.Request) error {
	return errors.New("scenario readiness redirects are not allowed")
}

func (e *scenarioHost) checkReady(ctx context.Context, client *http.Client, marker string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+e.Address+"/scenario/ready", nil)
	if err != nil {
		return fmt.Errorf("readyRequest: %w", err)
	}
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("readySend: %w", err)
	}
	payload, readErr := io.ReadAll(io.LimitReader(response.Body, 128))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		return fmt.Errorf("readyRead: %w", errors.Join(readErr, closeErr))
	}
	if response.StatusCode != http.StatusOK || string(payload) != marker {
		return errors.New("scenario readiness response did not identify the owned server")
	}
	return nil
}

func (e *scenarioHost) Observe(ctx context.Context, kind scenario.StepKind) (scenario.Observation, error) {
	if ctx == nil {
		return scenario.Observation{}, errors.New("scenario observation requires context")
	}
	if kind != scenario.TerminalFailure {
		clientObservation, isStopped, err := e.checkClientObservation(ctx, kind)
		if err != nil {
			return clientObservation, fmt.Errorf("observeClient: %w", err)
		}
		if isStopped {
			return clientObservation, nil
		}
	}
	if kind == scenario.NativeLayout || kind == scenario.TerminalFailure || kind == scenario.FixtureComparison {
		return e.Capture(ctx, kind)
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	observation := scenario.Observation{Outcome: scenario.Inconclusive, Detail: "requested observation not yet reached"}
	var proof *gameplay.ScenarioPrepareEvidence
	var commitProof *gameplay.ScenarioDungeonCommitEvidence
	for {
		contextErr := ctx.Err()
		if contextErr != nil {
			if kind == scenario.DungeonCommitted {
				observation.Outcome = scenario.Inconclusive
				observation.SessionID = ""
				observation.ZoneGeneration = 0
			}
			observation.Detail = "observation deadline reached: " + observation.Detail
			return e.persistObserved(kind, observation, proof, commitProof)
		}
		clientObservation, isStopped, err := e.checkClientObservation(ctx, kind)
		if err != nil {
			return clientObservation, fmt.Errorf("pollClient: %w", err)
		}
		if isStopped {
			return clientObservation, nil
		}
		candidate, candidateProof, candidateCommitProof, err := e.observeCandidate(ctx, kind)
		if candidateCommitProof != nil && candidateCommitProof.UserID != 0 {
			retainedProof := *candidateCommitProof
			commitProof = &retainedProof
		}
		if err != nil {
			if kind == scenario.DungeonCommitted {
				observation.Outcome = scenario.Inconclusive
				observation.SessionID = ""
				observation.ZoneGeneration = 0
				observation.Detail = "dungeon observation interrupted: " + err.Error()
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				observation.Detail = "observation deadline reached: " + observation.Detail
				return e.persistObserved(kind, observation, proof, commitProof)
			}
			if kind == scenario.DungeonCommitted {
				persisted, persistErr := e.persistObserved(kind, observation, proof, commitProof)
				return persisted, fmt.Errorf("dungeonObserve: %w", errors.Join(err, persistErr))
			}
			return scenario.Observation{}, fmt.Errorf("serverObserve: %w", err)
		}
		observation = candidate
		proof = candidateProof
		if observation.Outcome == scenario.Passed {
			clientObservation, isStopped, err = e.checkClientObservation(ctx, kind)
			if err != nil {
				return clientObservation, fmt.Errorf("acceptedClient: %w", err)
			}
			if isStopped {
				return clientObservation, nil
			}
		}
		if kind == scenario.DungeonCommitted && ctx.Err() != nil {
			observation.Outcome = scenario.Inconclusive
			observation.SessionID = ""
			observation.ZoneGeneration = 0
			observation.Detail = "observation deadline reached: " + observation.Detail
			return e.persistObserved(kind, observation, proof, commitProof)
		}
		if kind != scenario.Authenticated && kind != scenario.PrepareAccepted && kind != scenario.DungeonCommitted || observation.Outcome == scenario.Passed || observation.Outcome == scenario.Failed {
			return e.persistObserved(kind, observation, proof, commitProof)
		}
		if kind == scenario.PrepareAccepted {
			e.collectEntryFrame(ctx)
		}
		select {
		case <-ctx.Done():
			if kind == scenario.DungeonCommitted {
				observation.Outcome = scenario.Inconclusive
				observation.SessionID = ""
				observation.ZoneGeneration = 0
			}
			observation.Detail = "observation deadline reached: " + observation.Detail
			return e.persistObserved(kind, observation, proof, commitProof)
		case <-e.done:
			observation.Outcome = scenario.Inconclusive
			observation.Detail = "owned server stopped before requested observation"
			return e.persistObserved(kind, observation, proof, commitProof)
		case <-ticker.C:
		}
	}
}

func (e *scenarioHost) observeCandidate(
	ctx context.Context, kind scenario.StepKind,
) (scenario.Observation, *gameplay.ScenarioPrepareEvidence, *gameplay.ScenarioDungeonCommitEvidence, error) {
	if kind == scenario.DungeonCommitted {
		observation, proof, err := e.server.ScenarioBoundDungeonCommitObservation(ctx, e.userID, e.authenticatedSessionID)
		if err != nil {
			return observation, nil, &proof, fmt.Errorf("commitObserve: %w", err)
		}
		return observation, nil, &proof, nil
	}
	if kind == scenario.PrepareAccepted {
		observation, proof, err := e.server.ScenarioBoundPrepareObservation(ctx, e.userID, e.authenticatedSessionID)
		if err != nil {
			return scenario.Observation{}, nil, nil, fmt.Errorf("prepareObserve: %w", err)
		}
		return observation, &proof, nil, nil
	}
	observation, err := e.server.ScenarioBoundObservation(ctx, e.userID, e.authenticatedSessionID, kind)
	if err != nil {
		return scenario.Observation{}, nil, nil, fmt.Errorf("boundObserve: %w", err)
	}
	return observation, nil, nil, nil
}
