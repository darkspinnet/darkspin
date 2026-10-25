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
	"os"
	"path/filepath"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario/desktop"
)

type debugDiagnosticTrace struct {
	Name         string    `json:"name"`
	Path         string    `json:"path"`
	SHA256       string    `json:"sha256"`
	SourceSize   int64     `json:"source_size"`
	SourceOffset int64     `json:"source_offset"`
	Size         int       `json:"size"`
	IsTruncated  bool      `json:"is_truncated"`
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at"`
}

type debugDiagnosticTraceCapture struct {
	result debugDiagnosticTrace
	err    error
}

// Copy only complete newline-delimited records within the sampled file size.
// A live file can grow after Stat; the recorded interval is explicit, not frozen.
func captureDebugDiagnosticTrace(ctx context.Context, directory, name, source string) debugDiagnosticTraceCapture {
	result := debugDiagnosticTrace{Name: name, StartedAt: time.Now().UTC()}
	payload, size, offset, err := readDebugDiagnosticTail(ctx, source)
	result.FinishedAt = time.Now().UTC()
	result.SourceSize, result.SourceOffset = size, offset
	result.Size = len(payload)
	result.IsTruncated = offset != 0 || int64(len(payload)) != size
	if err != nil {
		return debugDiagnosticTraceCapture{result: result, err: fmt.Errorf("traceRead: %w", err)}
	}
	filename := name + ".jsonl"
	if name == "server-log" {
		filename = "server.log"
	}
	err = writeDebugDiagnosticFile(directory, filename, payload, 512*1024)
	if err != nil {
		return debugDiagnosticTraceCapture{result: result, err: fmt.Errorf("traceWrite: %w", err)}
	}
	result.Path = filepath.Join(directory, filename)
	digest := sha256.Sum256(payload)
	result.SHA256 = hex.EncodeToString(digest[:])
	return debugDiagnosticTraceCapture{result: result}
}

func readDebugDiagnosticTail(ctx context.Context, path string) ([]byte, int64, int64, error) {
	err := ctx.Err()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("tailContext: %w", err)
	}
	err = desktop.CheckAncestors(path)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("tailPath: %w", err)
	}
	r, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("tailOpen: %w", err)
	}
	fi, statErr := r.Stat()
	if statErr != nil {
		closeErr := r.Close()
		return nil, 0, 0, fmt.Errorf("tailStat: %w", errors.Join(statErr, closeErr))
	}
	if !fi.Mode().IsRegular() {
		closeErr := r.Close()
		return nil, 0, 0, fmt.Errorf("tailType: %w", errors.Join(errors.New("trace source is not a regular file"), closeErr))
	}
	size := fi.Size()
	offset := max(int64(0), size-512*1024)
	payload := make([]byte, int(size-offset))
	count, readErr := r.ReadAt(payload, offset)
	closeErr := r.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) || count != len(payload) || closeErr != nil {
		return nil, size, offset, fmt.Errorf("tailRead: %w", errors.Join(readErr, closeErr, errors.New("trace read did not finish at the sampled file size")))
	}
	err = ctx.Err()
	if err != nil {
		return nil, size, offset, fmt.Errorf("tailDeadline: %w", err)
	}
	if offset > 0 {
		firstEnd := bytes.IndexByte(payload, '\n')
		if firstEnd < 0 {
			return nil, size, offset, errors.New("bounded trace tail contains no complete record")
		}
		payload = payload[firstEnd+1:]
		offset += int64(firstEnd + 1)
	}
	lastEnd := bytes.LastIndexByte(payload, '\n')
	if lastEnd < 0 {
		payload = payload[:0]
	} else {
		payload = payload[:lastEnd+1]
	}
	return payload, size, offset, nil
}

func writeDebugDiagnosticFile(directory, name string, payload []byte, limit int) error {
	if len(payload) > limit {
		return errors.New("diagnostic artifact exceeds its byte limit")
	}
	err := desktop.CheckAncestors(directory)
	if err != nil {
		return fmt.Errorf("artifactParent: %w", err)
	}
	w, err := os.OpenFile(filepath.Join(directory, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("artifactCreate: %w", err)
	}
	count, writeErr := w.Write(payload)
	closeErr := w.Close()
	if writeErr != nil || closeErr != nil {
		return fmt.Errorf("artifactWrite: %w", errors.Join(writeErr, closeErr))
	}
	if count != len(payload) {
		return errors.New("diagnostic artifact write was incomplete")
	}
	return nil
}
