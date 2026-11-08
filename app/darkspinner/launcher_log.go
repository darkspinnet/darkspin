package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

const launcherLogQueueByteLimit = 16 * 1024 * 1024
const launcherLogQueueEntryLimit = 32768

type launcherLogEntry struct {
	path       string
	text       string
	completion chan error
}

// The server can log while holding a gameplay/transport lock. Only this worker
// performs filesystem operations; a slow disk must not stall that caller.
type launcherLogSession struct {
	mutex           sync.Mutex
	entries         []launcherLogEntry
	queuedByteCount int
	droppedCount    uint64
	droppedPath     string
	wake            chan struct{}
	done            chan struct{}
	isClosing       bool
}

func (e *launcherLogSession) enqueue(path, text string) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isClosing {
		return
	}
	if e.wake == nil {
		e.wake = make(chan struct{}, 1)
		e.done = make(chan struct{})
		go e.run()
	}
	if len(e.entries) >= launcherLogQueueEntryLimit || len(text) > launcherLogQueueByteLimit-e.queuedByteCount {
		e.droppedCount++
		e.droppedPath = path
	} else {
		e.entries = append(e.entries, launcherLogEntry{path: path, text: text})
		e.queuedByteCount += len(text)
	}
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

func (e *launcherLogSession) take() ([]launcherLogEntry, bool) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	entries := e.entries
	if e.droppedCount != 0 {
		omission := launcherLogEntry{path: e.droppedPath,
			text: fmt.Sprintf("%s Launcher log backlog exceeded %d bytes or %d entries; omitted %d log messages while disk output was delayed\n",
				time.Now().Format(time.RFC3339Nano), launcherLogQueueByteLimit, launcherLogQueueEntryLimit, e.droppedCount)}
		// A report barrier must not overtake the loss notice.
		entries = append([]launcherLogEntry{omission}, entries...)
	}
	e.entries = nil
	e.queuedByteCount = 0
	e.droppedCount = 0
	e.droppedPath = ""
	return entries, e.isClosing
}

func (e *launcherLogSession) run() {
	defer close(e.done)
	isEarlierWriteFailed := false
	for {
		entries, isClosing := e.take()
		if len(entries) != 0 {
			err := writeLauncherLogs(entries, isEarlierWriteFailed)
			if err != nil {
				isEarlierWriteFailed = true
				launcherLogFailure(err)
			}
		}
		if isClosing {
			return
		}
		<-e.wake
	}
}

func writeLauncherLogs(entries []launcherLogEntry, isEarlierWriteFailed bool) error {
	var writeErr error
	for start := 0; start < len(entries); {
		if entries[start].completion != nil {
			var flushErr error
			if writeErr != nil {
				flushErr = fmt.Errorf("logFlush: %w", writeErr)
			} else if isEarlierWriteFailed {
				flushErr = errors.New("earlier launcher log output failed; diagnostic log may be incomplete")
			}
			entries[start].completion <- flushErr
			start++
			continue
		}
		end := start + 1
		for end < len(entries) && entries[end].completion == nil && entries[end].path == entries[start].path {
			end++
		}
		err := appendLauncherLogs(entries[start:end])
		if err != nil {
			writeErr = errors.Join(writeErr, err)
		}
		start = end
	}
	if writeErr != nil {
		return fmt.Errorf("logBatch: %w", writeErr)
	}
	return nil
}

// A report waits outside gameplay locks until all earlier accepted messages
// are appended and flushed. A timed-out caller never blocks the worker's reply.
func (e *launcherLogSession) flush(ctx context.Context) error {
	if ctx == nil {
		return errors.New("launcher log flush context unavailable")
	}
	contextErr := ctx.Err()
	if contextErr != nil {
		return fmt.Errorf("logContext: %w", contextErr)
	}
	completion := make(chan error, 1)
	e.mutex.Lock()
	if e.wake == nil {
		e.mutex.Unlock()
		return nil
	}
	if e.isClosing {
		e.mutex.Unlock()
		return errors.New("launcher log is already closing")
	}
	if len(e.entries) >= launcherLogQueueEntryLimit {
		e.mutex.Unlock()
		return errors.New("launcher log flush queue is full")
	}
	e.entries = append(e.entries, launcherLogEntry{completion: completion})
	select {
	case e.wake <- struct{}{}:
	default:
	}
	e.mutex.Unlock()
	select {
	case err := <-completion:
		if err != nil {
			return fmt.Errorf("logBarrier: %w", err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("logDeadline: %w", ctx.Err())
	}
}

func appendLauncherLogs(entries []launcherLogEntry) error {
	output, err := os.OpenFile(entries[0].path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("logOpen: %w", err)
	}
	writer := bufio.NewWriterSize(output, 64*1024)
	var appendErr error
	for _, entry := range entries {
		writtenCount, err := writer.WriteString(entry.text)
		if err != nil {
			appendErr = fmt.Errorf("logWrite: %w", err)
			break
		}
		if writtenCount != len(entry.text) {
			appendErr = fmt.Errorf("logShortWrite: %w", io.ErrShortWrite)
			break
		}
	}
	flushErr := writer.Flush()
	if flushErr != nil {
		flushErr = fmt.Errorf("logBufferFlush: %w", flushErr)
	}
	closeErr := output.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("logClose: %w", closeErr)
	}
	if appendErr != nil || flushErr != nil || closeErr != nil {
		return fmt.Errorf("logAppend: %w", errors.Join(appendErr, flushErr, closeErr))
	}
	return nil
}

func (e *launcherLogSession) close() error {
	e.mutex.Lock()
	e.isClosing = true
	done := e.done
	if done != nil {
		select {
		case e.wake <- struct{}{}:
		default:
		}
	}
	e.mutex.Unlock()
	if done == nil {
		return nil
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		return errors.New("launcher log shutdown timed out with disk output still pending")
	}
}

func launcherLogFailure(err error) {
	writtenCount, stderrErr := fmt.Fprintf(os.Stderr, "Launcher log output failed: %v\n", err)
	if stderrErr != nil || writtenCount == 0 {
		// No independent diagnostic sink remains; never recurse into App.log.
		return
	}
}
