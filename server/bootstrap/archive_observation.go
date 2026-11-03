package bootstrap

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ArchiveObservation covers the required files of one recorded snapshot. It
// does not scan copied save trees, rehash archive contents, or exercise restore.
type ArchiveObservation struct {
	Status          string
	IntegrityStatus string
	RestoreStatus   string
}

// BackupDirectory accepts only the supervisor-owned backup directory, with no
// symlink components. Callers must independently bind root to their deployment.
func BackupDirectory(root string) (string, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return "", errors.New("invalid bootstrap root")
	}
	storage := &Storage{root: root}
	path := storage.path("backup")
	err := storage.checkPath(path)
	if err != nil {
		return "", fmt.Errorf("backupPath: %w", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("backupStat: %w", err)
	}
	if !fi.IsDir() {
		return "", errors.New("backup mount is not a directory")
	}
	return path, nil
}

func ObserveBackupArchive(root string, success BackupSuccess) ArchiveObservation {
	observation := ArchiveObservation{Status: "unknown", IntegrityStatus: "unknown", RestoreStatus: "not_verified"}
	if !backupObservationIDPattern.MatchString(success.ID) || !backupObservationBuildPattern.MatchString(success.BuildID) {
		return observation
	}
	backupDirectory, err := BackupDirectory(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			observation.Status = "missing"
		}
		return observation
	}
	storage := &Storage{root: root}
	archivePath := filepath.Join(backupDirectory, filepath.FromSlash(success.ID))
	manifestPath := filepath.Join(archivePath, "snapshot.json")
	err = storage.checkPath(manifestPath)
	if err != nil {
		return observation
	}
	manifestFI, err := os.Stat(manifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			observation.Status = "missing"
		}
		return observation
	}
	if !manifestFI.Mode().IsRegular() || manifestFI.Size() <= 0 || manifestFI.Size() > 16384 {
		return observation
	}
	r, err := os.Open(manifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			observation.Status = "missing"
		}
		return observation
	}
	payload, readErr := io.ReadAll(io.LimitReader(r, 16385))
	closeErr := r.Close()
	if readErr != nil || closeErr != nil || len(payload) > 16384 {
		return observation
	}
	var manifest snapshotManifest
	err = json.Unmarshal(payload, &manifest)
	if err != nil || !manifest.IsComplete || manifest.PreviousBuild != success.BuildID ||
		len(manifest.PreviousHash) != 64 || !strings.HasSuffix(success.ID, "-"+manifest.PreviousHash[:12]) ||
		len(manifest.DatabaseHash) != 64 || manifest.DatabaseBytes < 0 {
		return observation
	}
	createdAt, err := time.Parse(time.RFC3339Nano, manifest.Created)
	if err != nil || createdAt.UTC().Format("2006-01-02/15-04-05.000000000") != success.ID[:29] {
		return observation
	}
	previousHash, hashErr := hex.DecodeString(manifest.PreviousHash)
	databaseHash, databaseErr := hex.DecodeString(manifest.DatabaseHash)
	if hashErr != nil || databaseErr != nil || len(previousHash) != 32 || len(databaseHash) != 32 {
		return observation
	}
	paths := []string{filepath.Join("bin", installedBinaryName()), "runtime/darkspin.toml", "postgres.dump"}
	for _, rel := range paths {
		path := filepath.Join(archivePath, filepath.FromSlash(rel))
		err = storage.checkPath(path)
		if err != nil {
			return observation
		}
		fi, statErr := os.Stat(path)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				observation.Status = "missing"
			}
			return observation
		}
		if !fi.Mode().IsRegular() || fi.Size() == 0 {
			observation.IntegrityStatus = "mismatch"
			return observation
		}
		if rel == "postgres.dump" && manifest.DatabaseBytes > 0 && fi.Size() != manifest.DatabaseBytes {
			observation.IntegrityStatus = "mismatch"
			return observation
		}
	}
	observation.Status = "available"
	// Snapshot creation hashes the dump. Matching size now cannot prove its
	// contents remain unchanged, so never describe this as current verification.
	observation.IntegrityStatus = "recorded_at_creation"
	return observation
}
