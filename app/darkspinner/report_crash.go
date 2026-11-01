package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

type AutomaticClientReport struct {
	LaunchID    string       `json:"launchId"`
	Title       string       `json:"title"`
	Message     string       `json:"message"`
	IsCrash     bool         `json:"isCrash"`
	IsPreparing bool         `json:"isPreparing"`
	Result      ReportResult `json:"result"`
	Error       string       `json:"error,omitempty"`
}

// The owning launch worker publishes abnormal exits through the same mailbox
// as Fang login failures, including for independently supervised clients.
func writeClientExitFailure(processID uint32, exitCode uint32) error {
	path := os.Getenv(clientResultEnvironment)
	launchID := os.Getenv(clientLaunchEnvironment)
	if path == "" || launchID == "" {
		return nil
	}
	if !clientLaunchIDPattern.MatchString(launchID) || processID == 0 {
		return errors.New("client exit identity invalid")
	}
	previous, err := readClientFailure(path)
	if err == nil && previous.PID == processID && previous.LaunchID == launchID {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("exitRead: %w", err)
	}
	record := clientFailureRecord{
		PID: processID, LaunchID: launchID, Stage: "client-exit",
		Reason: "abnormal-exit", Message: gameExitError(exitCode).Error(),
		ExitCode: exitCode, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("exitMarshal: %w", err)
	}
	err = replaceLauncherConfig(path, append(payload, '\n'))
	if err != nil {
		return fmt.Errorf("exitWrite: %w", err)
	}
	return nil
}

func (e *App) captureLaunchFailure(path, launchID string) {
	failure, err := readClientFailure(path)
	if err == nil && failure.LaunchID == launchID {
		e.presentClientFailure(path, failure, clientProcessRecord{
			PID: failure.PID, LaunchID: launchID,
		})
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		e.log(fmt.Sprintf("Automatic crash mailbox read failed launch=%s: %v", launchID, err))
	}
	for _, result := range e.clientSession.Results() {
		if result.Process.LaunchID == launchID && result.Failure != nil {
			e.captureClientFailure(*result.Failure)
			return
		}
	}
}

func (e *App) publishAutomaticClientReport(notice AutomaticClientReport) {
	e.mu.Lock()
	e.status.AutomaticClientReport = &notice
	e.emitStatusLocked()
	e.mu.Unlock()
}

// All failure observers converge here. A per-launch completion also lets the
// attached supervisor wait for capture before a retry can reset its trace.
func (e *App) captureClientFailure(failure clientFailureRecord) {
	session, done, isOwner := e.clientSession.beginReport(failure.LaunchID)
	if done == nil {
		return
	}
	if !isOwner {
		<-done
		return
	}
	defer close(done)
	if session.client.Mode == "" {
		e.log("Automatic crash report unavailable: launch context missing")
		return
	}
	title := fmt.Sprintf("Automatic client failure - %s - %s", failure.Reason, failure.Message)
	notice := AutomaticClientReport{
		LaunchID: failure.LaunchID, Title: title, Message: failure.Message,
		IsCrash: failure.Stage == "client-exit", IsPreparing: true,
	}
	e.publishAutomaticClientReport(notice)
	result, err := e.sendSessionReport(title, &session)
	notice.IsPreparing = false
	if err != nil {
		notice.Error = err.Error()
		e.publishAutomaticClientReport(notice)
		e.log(fmt.Sprintf("Automatic crash report failed launch=%s: %v", failure.LaunchID, err))
		return
	}
	notice.Result = result
	e.publishAutomaticClientReport(notice)
	e.log(fmt.Sprintf("Automatic crash report saved launch=%s: %s (%d ZIPs)",
		failure.LaunchID, result.Directory, len(result.Names)))
}
