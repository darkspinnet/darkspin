package local

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type bugLogCapture struct {
	RequestedWindowStartedAt time.Time      `json:"requested_window_started_at"`
	RequestedWindowEndedAt   time.Time      `json:"requested_window_ended_at"`
	MaximumSourceBytes       int            `json:"maximum_source_bytes"`
	Sources                  []bugLogSource `json:"sources"`
}

type bugLogSource struct {
	Name          string    `json:"name"`
	ReadAt        time.Time `json:"read_at"`
	ModifiedAt    time.Time `json:"modified_at"`
	OriginalBytes int64     `json:"original_bytes"`
	ReadOffset    int64     `json:"read_offset"`
	IncludedBytes int       `json:"included_bytes"`
	IsTruncated   bool      `json:"is_truncated"`
	IsStale       bool      `json:"is_stale"`
	Failure       string    `json:"failure,omitempty"`
}

func captureRecentBugLog(path string, createdAt time.Time) ([]byte, bugLogSource, error) {
	source := bugLogSource{Name: filepath.Base(path), ReadAt: time.Now().UTC()}
	r, err := os.Open(path)
	if err != nil {
		return nil, source, fmt.Errorf("sourceOpen: %w", err)
	}
	contents, source, readErr := readRecentBugLog(r, source, createdAt)
	closeErr := r.Close()
	if readErr != nil {
		return nil, source, fmt.Errorf("sourceRead: %w", errors.Join(readErr, closeErr))
	}
	if closeErr != nil {
		return nil, source, fmt.Errorf("sourceClose: %w", closeErr)
	}
	return contents, source, nil
}

func readRecentBugLog(
	r *os.File, source bugLogSource, createdAt time.Time,
) ([]byte, bugLogSource, error) {
	fi, err := r.Stat()
	if err != nil {
		return nil, source, fmt.Errorf("sourceStat: %w", err)
	}
	source.ModifiedAt = fi.ModTime()
	source.OriginalBytes = fi.Size()
	source.IsStale = fi.ModTime().Before(createdAt.Add(-bugLogWindow))
	if source.IsStale {
		return nil, source, nil
	}
	source.ReadOffset = max(int64(0), fi.Size()-maximumBugLogSize)
	source.IsTruncated = source.ReadOffset > 0
	contents, err := io.ReadAll(io.NewSectionReader(r, source.ReadOffset, fi.Size()-source.ReadOffset))
	if err != nil {
		return nil, source, fmt.Errorf("tailRead: %w", err)
	}
	if source.IsTruncated {
		separator := bytes.IndexByte(contents, '\n')
		if separator < 0 {
			return nil, source, nil
		}
		contents = contents[separator+1:]
	}
	contents = recentBugLines(contents, createdAt)
	source.IncludedBytes = len(contents)
	return contents, source, nil
}

func writeBugLogCapture(archive *zip.Writer, capture bugLogCapture) error {
	w, err := archive.Create("logs/capture.json")
	if err != nil {
		return fmt.Errorf("captureEntry: %w", err)
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(capture)
	if err != nil {
		return fmt.Errorf("captureEncode: %w", err)
	}
	return nil
}
