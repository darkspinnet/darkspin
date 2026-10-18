package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type snapshotArchiveSource struct {
	path string
	fi   os.FileInfo
}

type SnapshotCompactResult struct {
	Names         []string `json:"names"`
	Directory     string   `json:"directory"`
	SnapshotCount int      `json:"snapshotCount"`
	RemovedCount  int      `json:"removedCount"`
	Warning       string   `json:"warning,omitempty"`
}

// CompactSnapshots moves completed snapshot ZIPs into size-limited bundles.
// Original ZIPs are removed only after every output part has been closed.
func (a *App) CompactSnapshots() (SnapshotCompactResult, error) {
	if !a.snapshotCompactMu.TryLock() {
		return SnapshotCompactResult{}, errors.New("snapshot compaction is already running")
	}
	defer a.snapshotCompactMu.Unlock()
	_, directory, err := a.reportPaths()
	if err != nil {
		return SnapshotCompactResult{}, fmt.Errorf("compactPaths: %w", err)
	}
	result := SnapshotCompactResult{Directory: directory, Names: []string{}}
	sources, err := snapshotArchiveSources(directory)
	if err != nil {
		return result, fmt.Errorf("compactSources: %w", err)
	}
	if len(sources) == 0 {
		return result, nil
	}
	stem := filepath.Join(directory, "snapshots-"+time.Now().UTC().Format("20060102T150405.000000000Z"))
	paths, err := compactSnapshotArchives(stem, sources)
	if err != nil {
		return result, fmt.Errorf("compactArchives: %w", err)
	}
	result.SnapshotCount = len(sources)
	for _, path := range paths {
		result.Names = append(result.Names, filepath.Base(path))
	}
	warnings := make([]string, 0)
	for _, source := range sources {
		removeErr := removeCompactedSnapshot(source)
		if removeErr != nil {
			warnings = append(warnings, filepath.Base(source.path)+": "+removeErr.Error())
			continue
		}
		result.RemovedCount++
	}
	result.Warning = strings.Join(warnings, "; ")
	a.log(fmt.Sprintf("Compacted %d snapshot ZIPs into %d parts; removed %d originals", result.SnapshotCount, len(paths), result.RemovedCount))
	return result, nil
}

func snapshotArchiveSources(directory string) ([]snapshotArchiveSource, error) {
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("snapshotReadDir: %w", err)
	}
	sources := make([]snapshotArchiveSource, 0)
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !strings.HasPrefix(name, "SS-") ||
			!strings.Contains(name, "-darkspin-snapshot-") ||
			!strings.EqualFold(filepath.Ext(name), ".zip") {
			continue
		}
		fi, statErr := entry.Info()
		if statErr != nil {
			return nil, fmt.Errorf("snapshotStat[%s]: %w", name, statErr)
		}
		sources = append(sources, snapshotArchiveSource{path: filepath.Join(directory, name), fi: fi})
	}
	return sources, nil
}

func compactSnapshotArchives(stem string, sources []snapshotArchiveSource) (paths []string, err error) {
	w, err := os.CreateTemp(filepath.Dir(stem), ".compact-snapshots-*.zip")
	if err != nil {
		return nil, fmt.Errorf("compactTemp: %w", err)
	}
	archivePath := w.Name()
	defer func() {
		if archivePath == "" {
			return
		}
		removeErr := os.Remove(archivePath)
		if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("compactTempRemove: %w", removeErr))
		}
	}()
	archive := zip.NewWriter(w)
	for _, source := range sources {
		err = addSnapshotArchive(archive, source)
		if err != nil {
			break
		}
	}
	archiveErr := archive.Close()
	closeErr := w.Close()
	if err != nil || archiveErr != nil || closeErr != nil {
		return nil, fmt.Errorf("compactWrite: %w", errors.Join(err, archiveErr, closeErr))
	}
	fi, err := os.Stat(archivePath)
	if err != nil {
		return nil, fmt.Errorf("compactStat: %w", err)
	}
	// Number even a single part, so a later compaction has the same layout.
	outputPath := stem + ".zip"
	if fi.Size() <= maximumReportPartSize {
		outputPath = stem + "-part-001.zip"
	}
	err = os.Rename(archivePath, outputPath)
	if err != nil {
		return nil, fmt.Errorf("compactRename: %w", err)
	}
	archivePath = outputPath
	paths, err = segmentReportArchive(outputPath)
	if err != nil {
		return nil, fmt.Errorf("compactSegment: %w", err)
	}
	// segmentReportArchive removes the large staging archive itself. A single
	// final part must survive the temporary-file cleanup above as well.
	if len(paths) == 1 && paths[0] == archivePath {
		archivePath = ""
	}
	return paths, nil
}

func addSnapshotArchive(archive *zip.Writer, source snapshotArchiveSource) (err error) {
	r, err := os.Open(source.path)
	if err != nil {
		return fmt.Errorf("snapshotOpen: %w", err)
	}
	defer func() {
		closeErr := r.Close()
		if closeErr != nil {
			err = errors.Join(err, fmt.Errorf("snapshotClose: %w", closeErr))
		}
	}()
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("snapshotReadStat: %w", err)
	}
	if !isSameSnapshotArchive(source.fi, fi) {
		return errors.New("snapshot changed before compaction; original retained")
	}
	header, err := zip.FileInfoHeader(fi)
	if err != nil {
		return fmt.Errorf("snapshotHeader: %w", err)
	}
	header.Name = filepath.Base(source.path)
	// Snapshot ZIPs are already compressed; preserve their bytes unchanged.
	header.Method = zip.Store
	w, err := archive.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("snapshotEntry: %w", err)
	}
	count, err := io.Copy(w, r)
	if err != nil {
		return fmt.Errorf("snapshotCopy: %w", err)
	}
	current, err := r.Stat()
	if err != nil {
		return fmt.Errorf("snapshotCopiedStat: %w", err)
	}
	if count != fi.Size() || !isSameSnapshotArchive(fi, current) {
		return errors.New("snapshot changed during compaction; original retained")
	}
	return nil
}

func isSameSnapshotArchive(first os.FileInfo, second os.FileInfo) bool {
	return second.Mode().IsRegular() && os.SameFile(first, second) &&
		first.Size() == second.Size() && first.ModTime().Equal(second.ModTime())
}

func removeCompactedSnapshot(source snapshotArchiveSource) error {
	fi, err := os.Lstat(source.path)
	if err != nil {
		return fmt.Errorf("snapshotRemoveStat: %w", err)
	}
	if !isSameSnapshotArchive(source.fi, fi) {
		return errors.New("snapshot changed after compaction; original retained")
	}
	err = os.Remove(source.path)
	if err != nil {
		return fmt.Errorf("snapshotRemove: %w", err)
	}
	return nil
}
