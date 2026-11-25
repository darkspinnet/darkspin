package snapshot

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

const automaticArchiveLimit int64 = 512 << 20

var (
	errAutomaticStorageLow      = errors.New("insufficient free space for automatic snapshot")
	errAutomaticArchiveTooLarge = errors.New("automatic snapshot exceeds ZIP budget")
	automaticArchivePattern     = regexp.MustCompile(`^SS-[0-9]{6,}-darkspin-snapshot-.+-[0-9]{8}T[0-9]{6}\.[0-9]{9}Z\.zip$`)
	automaticBundlePattern      = regexp.MustCompile(`^SS-[0-9]{6,}_[0-9]{8}T[0-9]{6}\.[0-9]{9}Z$`)
)

type automaticArchiveWriter struct {
	destination io.Writer
	remaining   int64
}

func (e *automaticArchiveWriter) Write(payload []byte) (int, error) {
	if int64(len(payload)) > e.remaining {
		return 0, errAutomaticArchiveTooLarge
	}
	count, err := e.destination.Write(payload)
	e.remaining -= int64(count)
	if err != nil {
		return count, fmt.Errorf("zipWrite: %w", err)
	}
	return count, nil
}

type automaticArchive struct {
	path     string
	size     int64
	manifest manifest
}

func (e *Service) pruneAutomaticArchives(incomingSize int64, isStartup bool) error {
	if e.archiveDirectory == "" {
		return nil
	}
	entries, err := os.ReadDir(e.archiveDirectory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("archiveRead: %w", err)
	}
	archives := make([]automaticArchive, 0)
	totalSize := incomingSize
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !automaticArchivePattern.MatchString(entry.Name()) {
			continue
		}
		path := filepath.Join(e.archiveDirectory, entry.Name())
		metadata, manifestErr := readAutomaticArchiveManifest(path)
		if manifestErr != nil {
			e.logger.Printf("Sync Snapshot archive classification failed for %s: %v", entry.Name(), manifestErr)
			continue
		}
		if metadata.Trigger != "automatic" || metadata.CreatedAt.IsZero() || metadata.ArchiveName != entry.Name() {
			continue
		}
		fi, statErr := entry.Info()
		if statErr != nil {
			return fmt.Errorf("archiveStat: %w", statErr)
		}
		archives = append(archives, automaticArchive{path: path, size: fi.Size(), manifest: metadata})
		totalSize += fi.Size()
	}
	sort.Slice(archives, func(i, j int) bool {
		left, right := archives[i], archives[j]
		if left.manifest.CreatedAt.Equal(right.manifest.CreatedAt) {
			return left.path < right.path
		}
		return left.manifest.CreatedAt.Before(right.manifest.CreatedAt)
	})
	for _, archive := range archives {
		if isStartup {
			e.removeArchivedAutomaticBundle(archive)
		}
		if totalSize <= automaticArchiveLimit {
			if isStartup {
				continue
			}
			break
		}
		err = os.Remove(archive.path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("archiveEvict: %w", err)
		}
		totalSize -= archive.size
	}
	return nil
}

func readAutomaticArchiveManifest(path string) (manifest, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return manifest{}, fmt.Errorf("manifestArchive: %w", err)
	}
	defer closeAutomaticArchive(archive)
	for _, entry := range archive.File {
		if filepath.Base(entry.Name) != "manifest.json" {
			continue
		}
		r, openErr := entry.Open()
		if openErr != nil {
			return manifest{}, fmt.Errorf("manifestOpen: %w", openErr)
		}
		var metadata manifest
		decodeErr := json.NewDecoder(io.LimitReader(r, 2<<20)).Decode(&metadata)
		closeErr := r.Close()
		if decodeErr != nil || closeErr != nil {
			return manifest{}, fmt.Errorf("manifestRead: %w", errors.Join(decodeErr, closeErr))
		}
		return metadata, nil
	}
	return manifest{}, errors.New("snapshot manifest missing")
}

func verifyAutomaticArchive(path, bundleName string) (int64, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return 0, fmt.Errorf("verifyOpen: %w", err)
	}
	defer closeAutomaticArchive(archive)
	isManifestFound := false
	for _, entry := range archive.File {
		if filepath.ToSlash(filepath.Dir(entry.Name)) != bundleName {
			return 0, errors.New("snapshot archive contains an unexpected path")
		}
		isManifestFound = isManifestFound || filepath.Base(entry.Name) == "manifest.json"
		r, openErr := entry.Open()
		if openErr != nil {
			return 0, fmt.Errorf("verifyEntry: %w", openErr)
		}
		_, readErr := io.Copy(io.Discard, r)
		closeErr := r.Close()
		if readErr != nil || closeErr != nil {
			return 0, fmt.Errorf("verifyRead: %w", errors.Join(readErr, closeErr))
		}
	}
	if !isManifestFound {
		return 0, errors.New("snapshot archive has no manifest")
	}
	fi, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("verifySize: %w", err)
	}
	return fi.Size(), nil
}

func (e *Service) removeArchivedAutomaticBundle(archive automaticArchive) {
	metadata := archive.manifest
	name := metadata.ID + "_" + metadata.CreatedAt.UTC().Format("20060102T150405.000000000Z")
	if !automaticBundlePattern.MatchString(name) {
		return
	}
	directory := filepath.Join(e.directory, name)
	fi, err := os.Lstat(directory)
	if err != nil || !fi.IsDir() {
		return
	}
	rawManifest, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		return
	}
	var raw manifest
	err = json.Unmarshal(rawManifest, &raw)
	if err != nil || raw.Trigger != "automatic" || raw.ID != metadata.ID ||
		raw.ArchiveName != metadata.ArchiveName || !raw.CreatedAt.Equal(metadata.CreatedAt) {
		return
	}
	_, err = verifyAutomaticArchive(archive.path, name)
	if err != nil {
		e.logger.Printf("Sync Snapshot legacy archive verification failed for %s: %v", metadata.ArchiveName, err)
		return
	}
	e.removeAutomaticBundle(directory)
}

func (e *Service) removeAutomaticBundle(directory string) {
	if filepath.Dir(directory) != filepath.Clean(e.directory) || !automaticBundlePattern.MatchString(filepath.Base(directory)) {
		e.logger.Printf("Sync Snapshot skipped cleanup outside the automatic bundle directory")
		return
	}
	err := os.RemoveAll(directory)
	if err != nil {
		e.logger.Printf("Sync Snapshot automatic bundle cleanup failed: %v", err)
	}
}

func closeAutomaticArchive(archive *zip.ReadCloser) {
	err := archive.Close()
	if err != nil {
		// Archive readers do not own pending writes and cannot retry Close.
		return
	}
}
