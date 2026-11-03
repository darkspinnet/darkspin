package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/diagnostic/dailylog"
	"github.com/darkspinnet/darkspin/server/diagnostic/retention"
)

// dedicatedDiagnostics belongs to one dedicated command. Its worker outlives
// individual supervised children and stops before the file writer is closed.
type dedicatedDiagnostics struct {
	writer             *dailylog.Writer
	runtimePath        string
	protectedPaths     []string
	isRetentionEnabled bool
	cancel             context.CancelFunc
	done               chan struct{}
	closeOnce          sync.Once
	closeErr           error
	failureMu          sync.Mutex
	lastFailure        error
}

func openDedicatedDiagnostics(ctx context.Context, runtimePath, tracePath string, isRetentionEnabled bool) (*dedicatedDiagnostics, error) {
	if ctx == nil {
		return nil, errors.New("nil diagnostic context")
	}
	absolutePath, err := filepath.Abs(runtimePath)
	if err != nil {
		return nil, fmt.Errorf("diagnosticPath: %w", err)
	}
	err = os.MkdirAll(absolutePath, 0o700)
	if err != nil {
		return nil, fmt.Errorf("diagnosticMkdir: %w", err)
	}
	writer, err := dailylog.New(absolutePath)
	if err != nil {
		return nil, fmt.Errorf("diagnosticOpen: %w", err)
	}
	workerCtx, cancel := context.WithCancel(ctx)
	e := &dedicatedDiagnostics{
		writer: writer, runtimePath: absolutePath, isRetentionEnabled: isRetentionEnabled,
		cancel: cancel, done: make(chan struct{}),
	}
	if tracePath != "" {
		if !filepath.IsAbs(tracePath) {
			tracePath = filepath.Join(absolutePath, "logs", "traces", tracePath)
		}
		e.protectedPaths = []string{filepath.Clean(tracePath)}
	}
	e.maintain(workerCtx)
	go e.run(workerCtx)
	return e, nil
}

func (e *dedicatedDiagnostics) Write(payload []byte) (int, error) {
	count, err := e.writer.Write(payload)
	if err == nil {
		return count, nil
	}
	e.recordFailure(err)
	// A disk failure must remain observable even when the file cannot be used.
	writtenCount, fallbackErr := fmt.Fprintf(os.Stderr, "Dedicated log write failed: %v\n%s", err, payload)
	if fallbackErr != nil {
		return count, fmt.Errorf("diagnosticFallback[%d]: %w", writtenCount, errors.Join(err, fallbackErr))
	}
	// The emergency sink accepted the full record. Keep draining child pipes
	// so a disk failure cannot block or terminate the supervised server.
	return len(payload), nil
}

func (e *dedicatedDiagnostics) run(ctx context.Context) {
	defer close(e.done)
	for {
		now := time.Now().UTC()
		midnight := now.Truncate(24 * time.Hour).Add(24 * time.Hour)
		timer := time.NewTimer(time.Until(midnight))
		select {
		case <-ctx.Done():
			isStopped := timer.Stop()
			if !isStopped {
				// An already fired timer has no further work after cancellation.
			}
			return
		case <-timer.C:
			e.maintain(ctx)
		}
	}
}

func (e *dedicatedDiagnostics) maintain(ctx context.Context) {
	err := e.writer.Rotate()
	if err != nil {
		e.report("Daily log rotation failed", err)
	}
	sweepCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if e.isRetentionEnabled {
		err = retention.Sweep(sweepCtx, e.runtimePath, e.protectedPaths)
	} else {
		// The supervisor cannot identify its child's active trace file. The
		// child owns artifact cleanup; the supervisor still expires daily logs.
		err = retention.SweepLogs(sweepCtx, e.runtimePath)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		e.report("Diagnostic retention cleanup failed", err)
	}
}

func (e *dedicatedDiagnostics) report(message string, err error) {
	payload := []byte(fmt.Sprintf("%s %s: %v\n", time.Now().UTC().Format(time.RFC3339), message, err))
	count, writeErr := e.Write(payload)
	if writeErr != nil {
		// Write already attempted stderr; no further output owner can recover it.
		e.recordFailure(fmt.Errorf("diagnosticReport[%d]: %w", count, writeErr))
	}
}

func (e *dedicatedDiagnostics) recordFailure(err error) {
	e.failureMu.Lock()
	e.lastFailure = err
	e.failureMu.Unlock()
}

func (e *dedicatedDiagnostics) Close() error {
	e.closeOnce.Do(e.close)
	if e.closeErr != nil {
		return fmt.Errorf("diagnosticClose: %w", e.closeErr)
	}
	return nil
}

func (e *dedicatedDiagnostics) close() {
	e.cancel()
	<-e.done
	writeCloseErr := e.writer.Close()
	e.failureMu.Lock()
	e.closeErr = errors.Join(writeCloseErr, e.lastFailure)
	e.failureMu.Unlock()
}
