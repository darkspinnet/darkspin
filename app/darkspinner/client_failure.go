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
		if !a.clientSession.Register(record) {
			continue
		}
		a.consumeClientResult(path)
		resultPath := filepath.Join(directoryPath, record.LaunchID+".failure.json")
		observedPaths[path] = struct{}{}
		observedPaths[resultPath] = struct{}{}
		go a.awaitClientProcess(ctx, resultPath, record)
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

func (a *App) isClientProfileRunning(profile string) (bool, error) {
	records, err := a.runningClientProfileRecords(profile)
	if err != nil {
		return false, fmt.Errorf("profileRecords: %w", err)
	}
	return len(records) != 0, nil
}

func (a *App) runningClientProfileRecords(profile string) ([]clientProcessRecord, error) {
	records := make([]clientProcessRecord, 0, 1)
	for _, record := range a.clientSession.RunningRecords() {
		if !strings.EqualFold(strings.TrimSpace(record.Profile), strings.TrimSpace(profile)) {
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

func (a *App) runningUnownedAttachedClientRecords() ([]clientProcessRecord, error) {
	records := make([]clientProcessRecord, 0, 1)
	for _, record := range a.clientSession.RunningRecords() {
		if strings.TrimSpace(record.Profile) != "" ||
			!strings.EqualFold(strings.TrimSpace(record.Mode), "attached") {
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

func (a *App) runningAttachedClientRecords() ([]clientProcessRecord, error) {
	records := make([]clientProcessRecord, 0, 1)
	for _, record := range a.clientSession.RunningRecords() {
		if !strings.EqualFold(strings.TrimSpace(record.Mode), "attached") {
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

func (a *App) awaitClientProcess(
	ctx context.Context, resultPath string, record clientProcessRecord,
) {
	defer a.clientSession.Close(record.LaunchID)
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
	} else if !a.clientSession.IsFailed(record.LaunchID) {
		a.log(formatClientRun(record) + ": exited normally.")
		a.setClientLastRun("Exited normally.", false)
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
		if !a.clientSession.Owns(record.LaunchID) {
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
	if !a.clientSession.Fail(failureRecord) {
		return
	}
	message := fmt.Sprintf(
		"%s: %s [%s]", formatClientRun(processRecord), failureRecord.Message, failureRecord.Reason,
	)
	a.setClientLastRun(message, true)
	a.log(message)
	a.consumeClientResult(path)
}

func (a *App) consumeClientResult(path string) {
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		a.log("Client mailbox cleanup: " + err.Error())
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
