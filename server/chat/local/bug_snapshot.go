package local

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/darkspinnet/darkspin/server/chat"
)

// BugSnapshotProvider supplies completed diagnostics when automatic capture is
// enabled, or an empty directory when it is disabled.
type BugSnapshotProvider interface {
	CaptureBugSnapshot(context.Context, chat.BugCommand) (string, error)
}

// UseSnapshotProvider is configured before the reporter handles commands.
func (e *BugReporter) UseSnapshotProvider(provider BugSnapshotProvider) {
	e.snapshotProvider = provider
}

func addBugSnapshot(
	ctx context.Context, archive *zip.Writer, provider BugSnapshotProvider, req chat.BugCommand,
) error {
	captureContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	directory, err := provider.CaptureBugSnapshot(captureContext, req)
	cancel()
	if err != nil {
		return addBugSnapshotFailure(archive, err)
	}
	if directory == "" {
		return nil
	}
	err = addBugSnapshotFiles(ctx, archive, directory)
	if err != nil {
		return addBugSnapshotFailure(archive, err)
	}
	return nil
}

func addBugSnapshotFiles(ctx context.Context, archive *zip.Writer, directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("snapshotRead: %w", err)
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		err = ctx.Err()
		if err != nil {
			return fmt.Errorf("snapshotContext: %w", err)
		}
		r, openErr := os.Open(filepath.Join(directory, entry.Name()))
		if openErr != nil {
			return fmt.Errorf("snapshotOpen[%s]: %w", entry.Name(), openErr)
		}
		name := filepath.ToSlash(filepath.Join("snapshots", filepath.Base(directory), entry.Name()))
		w, createErr := archive.Create(name)
		if createErr != nil {
			closeErr := r.Close()
			if closeErr != nil {
				return fmt.Errorf("snapshotClose[%s]: %w", entry.Name(), closeErr)
			}
			return fmt.Errorf("snapshotEntry[%s]: %w", entry.Name(), createErr)
		}
		copiedByteCount, copyErr := io.Copy(w, r)
		closeErr := r.Close()
		if copyErr != nil {
			return fmt.Errorf("snapshotCopy[%s,%d]: %w", entry.Name(), copiedByteCount, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("snapshotClose[%s]: %w", entry.Name(), closeErr)
		}
	}
	return nil
}

func addBugSnapshotFailure(archive *zip.Writer, captureErr error) error {
	w, err := archive.Create("snapshots/capture-error.txt")
	if err != nil {
		return fmt.Errorf("snapshotErrorEntry: %w", err)
	}
	writtenByteCount, err := fmt.Fprintf(w, "Sync Snapshot diagnostics could not be included: %v\n", captureErr)
	if err != nil {
		return fmt.Errorf("snapshotErrorWrite[%d]: %w", writtenByteCount, err)
	}
	return nil
}
