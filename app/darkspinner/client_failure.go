package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	clientResultEnvironment  = "DARKSPIN_CLIENT_RESULT"
	clientLaunchEnvironment  = "DARKSPIN_CLIENT_LAUNCH_ID"
	clientProfileEnvironment = "DARKSPIN_CLIENT_PROFILE"
	clientModeEnvironment    = "DARKSPIN_CLIENT_MODE"
	clientFailureDirectory   = "client-results"
)

var clientLaunchIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

type clientFailureRecord struct {
	PID      uint32 `json:"pid"`
	LaunchID string `json:"launch_id"`
	Stage    string `json:"stage"`
	Reason   string `json:"reason"`
	Message  string `json:"message"`
	TimeMS   uint64 `json:"time_ms"`
}

type clientProcessRecord struct {
	PID      uint32 `json:"pid"`
	LaunchID string `json:"launch_id"`
	Profile  string `json:"profile,omitempty"`
	Mode     string `json:"mode,omitempty"`
}

func newClientFailureTarget(basePath string) (string, string, error) {
	randomBytes := make([]byte, 16)
	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", "", fmt.Errorf("launchRandom: %w", err)
	}
	launchID := hex.EncodeToString(randomBytes)
	directoryPath := filepath.Join(basePath, "darkspin", "logs", clientFailureDirectory)
	err = os.MkdirAll(directoryPath, 0o755)
	if err != nil {
		return "", "", fmt.Errorf("resultMkdir: %w", err)
	}
	resultPath := filepath.Join(directoryPath, launchID+".failure.json")
	resultPath, err = filepath.Abs(resultPath)
	if err != nil {
		return "", "", fmt.Errorf("resultPath: %w", err)
	}
	return launchID, resultPath, nil
}

func configureClientFailure(
	resultPath, launchID, traceDirectory, profile string, isDetached bool,
) error {
	if resultPath == "" && launchID == "" {
		err := os.Unsetenv(clientResultEnvironment)
		if err != nil {
			return fmt.Errorf("resultUnset: %w", err)
		}
		err = os.Unsetenv(clientLaunchEnvironment)
		if err != nil {
			return fmt.Errorf("launchUnset: %w", err)
		}
		_ = os.Unsetenv(clientProfileEnvironment)
		_ = os.Unsetenv(clientModeEnvironment)
		return nil
	}
	if resultPath == "" || !clientLaunchIDPattern.MatchString(launchID) {
		return errors.New("client result requires a valid launch ID and result path")
	}
	if !filepath.IsAbs(resultPath) {
		resultPath = filepath.Join(traceDirectory, resultPath)
	}
	resultPath, err := filepath.Abs(resultPath)
	if err != nil {
		return fmt.Errorf("resultPath: %w", err)
	}
	err = os.MkdirAll(filepath.Dir(resultPath), 0o755)
	if err != nil {
		return fmt.Errorf("resultMkdir: %w", err)
	}
	err = os.Setenv(clientResultEnvironment, resultPath)
	if err != nil {
		return fmt.Errorf("resultSet: %w", err)
	}
	err = os.Setenv(clientLaunchEnvironment, launchID)
	if err != nil {
		return fmt.Errorf("launchSet: %w", err)
	}
	err = os.Setenv(clientProfileEnvironment, strings.TrimSpace(profile))
	if err != nil {
		return fmt.Errorf("profileSet: %w", err)
	}
	mode := "attached"
	if isDetached {
		mode = "detached"
	}
	err = os.Setenv(clientModeEnvironment, mode)
	if err != nil {
		return fmt.Errorf("modeSet: %w", err)
	}
	return nil
}

func writeClientProcessRegistration(processID uint32) error {
	resultPath := os.Getenv(clientResultEnvironment)
	launchID := os.Getenv(clientLaunchEnvironment)
	if resultPath == "" && launchID == "" {
		return nil
	}
	if processID == 0 || resultPath == "" || !clientLaunchIDPattern.MatchString(launchID) {
		return errors.New("managed game process registration is incomplete")
	}
	recordPath := filepath.Join(filepath.Dir(resultPath), launchID+".process.json")
	temporaryPath := fmt.Sprintf("%s.%d.tmp", recordPath, processID)
	contents, err := json.Marshal(clientProcessRecord{
		PID: processID, LaunchID: launchID,
		Profile: os.Getenv(clientProfileEnvironment), Mode: os.Getenv(clientModeEnvironment),
	})
	if err != nil {
		return fmt.Errorf("processMarshal: %w", err)
	}
	err = os.WriteFile(temporaryPath, append(contents, '\n'), 0o600)
	if err != nil {
		return fmt.Errorf("processWrite: %w", err)
	}
	err = os.Rename(temporaryPath, recordPath)
	if err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("processPublish: %w", err)
	}
	return nil
}

func (a *App) watchClientFailures(ctx context.Context, basePath string, done chan<- struct{}) {
	defer close(done)
	directoryPath := filepath.Join(basePath, "darkspin", "logs", clientFailureDirectory)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	observedPaths := make(map[string]struct{})
	for {
		a.scanClientProcesses(ctx, directoryPath, observedPaths)
		a.scanClientFailures(ctx, directoryPath, observedPaths)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *App) scanClientProcesses(ctx context.Context, directoryPath string, observedPaths map[string]struct{}) {
	paths, err := filepath.Glob(filepath.Join(directoryPath, "*.process.json"))
	if err != nil {
		return
	}
	for _, path := range paths {
		if _, isObserved := observedPaths[path]; isObserved {
			continue
		}
		record, err := readClientProcess(path)
		if err != nil {
			continue
		}
		resultPath := filepath.Join(directoryPath, record.LaunchID+".failure.json")
		observedPaths[path] = struct{}{}
		observedPaths[resultPath] = struct{}{}
		go a.awaitClientProcess(ctx, path, resultPath, record)
	}
}

func readClientProcess(path string) (clientProcessRecord, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return clientProcessRecord{}, fmt.Errorf("processRead: %w", err)
	}
	record := clientProcessRecord{}
	err = json.Unmarshal(contents, &record)
	if err != nil {
		return clientProcessRecord{}, fmt.Errorf("processDecode: %w", err)
	}
	if record.PID == 0 || !clientLaunchIDPattern.MatchString(record.LaunchID) {
		return clientProcessRecord{}, errors.New("client process registration is incomplete")
	}
	return record, nil
}

func isClientProfileRunning(basePath, profile string) (bool, error) {
	records, err := runningClientProfileRecords(basePath, profile)
	if err != nil {
		return false, fmt.Errorf("profileRecords: %w", err)
	}
	return len(records) != 0, nil
}

func runningClientProfileRecords(basePath, profile string) ([]clientProcessRecord, error) {
	directoryPath := filepath.Join(basePath, "darkspin", "logs", clientFailureDirectory)
	paths, err := filepath.Glob(filepath.Join(directoryPath, "*.process.json"))
	if err != nil {
		return nil, fmt.Errorf("profileGlob: %w", err)
	}
	records := make([]clientProcessRecord, 0, 1)
	for _, path := range paths {
		record, readErr := readClientProcess(path)
		if readErr != nil || !strings.EqualFold(strings.TrimSpace(record.Profile), strings.TrimSpace(profile)) {
			continue
		}
		if isProcessIDRunning(record.PID) {
			records = append(records, record)
		}
	}
	return records, nil
}

func runningUnownedAttachedClientRecords(basePath string) ([]clientProcessRecord, error) {
	directoryPath := filepath.Join(basePath, "darkspin", "logs", clientFailureDirectory)
	paths, err := filepath.Glob(filepath.Join(directoryPath, "*.process.json"))
	if err != nil {
		return nil, fmt.Errorf("attachedGlob: %w", err)
	}
	records := make([]clientProcessRecord, 0, 1)
	for _, path := range paths {
		record, readErr := readClientProcess(path)
		if readErr != nil || strings.TrimSpace(record.Profile) != "" ||
			!strings.EqualFold(strings.TrimSpace(record.Mode), "attached") {
			continue
		}
		if isProcessIDRunning(record.PID) {
			records = append(records, record)
		}
	}
	return records, nil
}

func runningAttachedClientRecords(basePath string) ([]clientProcessRecord, error) {
	directoryPath := filepath.Join(basePath, "darkspin", "logs", clientFailureDirectory)
	paths, err := filepath.Glob(filepath.Join(directoryPath, "*.process.json"))
	if err != nil {
		return nil, fmt.Errorf("attachedGlob: %w", err)
	}
	records := make([]clientProcessRecord, 0, 1)
	for _, path := range paths {
		record, readErr := readClientProcess(path)
		if readErr != nil ||
			!strings.EqualFold(strings.TrimSpace(record.Mode), "attached") {
			continue
		}
		if isProcessIDRunning(record.PID) {
			records = append(records, record)
		}
	}
	return records, nil
}

func isClientLaunchRunning(basePath, launchID string) bool {
	path := filepath.Join(basePath, "darkspin", "logs", clientFailureDirectory, launchID+".process.json")
	record, err := readClientProcess(path)
	return err == nil && record.LaunchID == launchID && isProcessIDRunning(record.PID)
}

func (a *App) awaitClientProcess(
	ctx context.Context, processPath, resultPath string, record clientProcessRecord,
) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for isProcessIDRunning(record.PID) {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	failureRecord, err := readClientFailure(resultPath)
	if err == nil && failureRecord.PID == record.PID && failureRecord.LaunchID == record.LaunchID {
		a.presentClientFailure(resultPath, failureRecord, record)
	} else {
		a.log(formatClientRun(record) + ": exited normally.")
		a.setClientLastRun("Exited normally.", false)
	}
	closedPath := strings.TrimSuffix(processPath, ".process.json") + ".closed.json"
	err = os.Rename(processPath, closedPath)
	if err != nil {
		a.log("Client process result could not be closed: " + err.Error())
	}
}

func (a *App) scanClientFailures(ctx context.Context, directoryPath string, observedPaths map[string]struct{}) {
	paths, err := filepath.Glob(filepath.Join(directoryPath, "*.failure.json"))
	if err != nil {
		return
	}
	for _, path := range paths {
		if _, isObserved := observedPaths[path]; isObserved {
			continue
		}
		record, err := readClientFailure(path)
		if err != nil {
			continue
		}
		observedPaths[path] = struct{}{}
		go a.awaitClientFailure(ctx, path, record)
	}
}

func readClientFailure(path string) (clientFailureRecord, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return clientFailureRecord{}, fmt.Errorf("resultRead: %w", err)
	}
	record := clientFailureRecord{}
	err = json.Unmarshal(contents, &record)
	if err != nil {
		return clientFailureRecord{}, fmt.Errorf("resultDecode: %w", err)
	}
	if record.PID == 0 || !clientLaunchIDPattern.MatchString(record.LaunchID) || strings.TrimSpace(record.Message) == "" {
		return clientFailureRecord{}, errors.New("client failure result is incomplete")
	}
	return record, nil
}

func (a *App) awaitClientFailure(ctx context.Context, path string, record clientFailureRecord) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for isProcessIDRunning(record.PID) {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	a.presentClientFailure(path, record, clientProcessRecord{PID: record.PID, LaunchID: record.LaunchID})
}

func (a *App) presentClientFailure(
	path string, failureRecord clientFailureRecord, processRecord clientProcessRecord,
) {
	message := fmt.Sprintf(
		"%s: %s [%s]", formatClientRun(processRecord), failureRecord.Message, failureRecord.Reason,
	)
	a.setClientLastRun(message, true)
	shownPath := strings.TrimSuffix(path, ".failure.json") + ".shown.json"
	err := os.Rename(path, shownPath)
	if err != nil {
		a.log("Client failure result could not be acknowledged: " + err.Error())
	}
}

func formatClientRun(record clientProcessRecord) string {
	mode := strings.TrimSpace(record.Mode)
	if mode == "" {
		mode = "client"
	}
	profile := strings.TrimSpace(record.Profile)
	if profile != "" {
		return fmt.Sprintf("%s Crogenitor %s (PID %d)", strings.Title(mode), profile, record.PID)
	}
	return fmt.Sprintf("%s (PID %d)", strings.Title(mode), record.PID)
}

func (a *App) setClientLastRun(message string, isFailure bool) {
	a.mu.Lock()
	a.status.LastRun = message
	a.status.IsLastRunFailure = isFailure
	a.emitStatusLocked()
	a.mu.Unlock()
}
