package bootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Storage owns stopped-server snapshots and the durable candidate transaction.
// Its methods must only be used while the supervised process is stopped, except
// ConfirmHealthy, which only commits metadata and prunes old snapshots.
type Storage struct {
	root          string
	retentionDays int
}

type snapshotManifest struct {
	Created        string `json:"created"`
	PreviousBuild  string `json:"previous_build"`
	PreviousHash   string `json:"previous_sha256"`
	CandidateBuild string `json:"candidate_build,omitempty"`
	CandidateHash  string `json:"candidate_sha256,omitempty"`
	IsComplete     bool   `json:"is_complete"`
}

type updateTransaction struct {
	Backup string `json:"backup"`
	Phase  string `json:"phase"`
}

func NewStorage(root string, retentionDays int) (*Storage, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("rootAbs: %w", err)
	}
	if retentionDays < 1 {
		return nil, errors.New("retention must be positive")
	}
	e := &Storage{root: filepath.Clean(abs), retentionDays: retentionDays}
	err = e.checkPath(e.root)
	if err != nil {
		return nil, fmt.Errorf("rootCheck: %w", err)
	}
	ownedPaths := []string{
		"bin/darkrun", "bin/darkrun.candidate", "backup", "bootstrap-lock/server.lock",
		"firstboot.json", "firstboot.json.tmp", "update-request.json",
		"runtime/darkspin.toml", "runtime/server.toml", "runtime/darkspin/server.lock", "runtime/darkspin/maintenance.json",
		"runtime/darkspin/saves/darkspin.db", "runtime/darkspin/saves/darkspin.db-wal",
		"runtime/darkspin/saves/darkspin.db-shm", "runtime/darkspin/cache/content.db",
	}
	for _, rel := range ownedPaths {
		err = e.checkPath(e.path(filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("layoutCheck: %w", err)
		}
	}
	return e, nil
}

func (e *Storage) path(parts ...string) string {
	return filepath.Join(append([]string{e.root}, parts...)...)
}

// checkPath rejects links in every existing path component, including ancestors
// of the configured root. All writes and recursive removal remain beneath root.
func (e *Storage) checkPath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("pathAbs: %w", err)
	}
	rel, err := filepath.Rel(e.root, abs)
	if err != nil {
		return fmt.Errorf("pathRel: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("path escapes bootstrap root")
	}
	for current := abs; ; current = filepath.Dir(current) {
		fi, statErr := os.Lstat(current)
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf("pathStat: %w", statErr)
		}
		if statErr == nil && fi.Mode()&os.ModeSymlink != 0 {
			return errors.New("bootstrap path contains symlink")
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return nil
}

func (e *Storage) removeTree(path string) error {
	err := e.checkPath(path)
	if err != nil {
		return fmt.Errorf("removeCheck: %w", err)
	}
	if filepath.Clean(path) == e.root {
		return errors.New("refusing to remove bootstrap root")
	}
	err = os.RemoveAll(path)
	if err != nil {
		return fmt.Errorf("removeTree: %w", err)
	}
	return nil
}

func syncDirectory(path string) error {
	r, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("syncOpen: %w", err)
	}
	err = r.Sync()
	closeErr := r.Close()
	if err != nil {
		return fmt.Errorf("directorySync: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("directoryClose: %w", closeErr)
	}
	return nil
}

func (e *Storage) syncParents(path string) error {
	for current := path; ; current = filepath.Dir(current) {
		err := e.checkPath(current)
		if err != nil {
			return fmt.Errorf("parentCheck: %w", err)
		}
		err = syncDirectory(current)
		if err != nil {
			return fmt.Errorf("parentSync: %w", err)
		}
		if current == e.root {
			return nil
		}
	}
}

func (e *Storage) writeJSON(path string, record any) error {
	payload, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("jsonMarshal: %w", err)
	}
	err = e.checkPath(path)
	if err != nil {
		return fmt.Errorf("jsonCheck: %w", err)
	}
	err = e.checkPath(path + ".tmp")
	if err != nil {
		return fmt.Errorf("jsonTempCheck: %w", err)
	}
	err = os.MkdirAll(filepath.Dir(path), 0700)
	if err != nil {
		return fmt.Errorf("jsonDirectory: %w", err)
	}
	err = writeSynced(path+".tmp", payload, 0600)
	if err != nil {
		return fmt.Errorf("jsonWrite: %w", err)
	}
	err = os.Rename(path+".tmp", path)
	if err != nil {
		return fmt.Errorf("jsonRename: %w", err)
	}
	err = e.syncParents(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("jsonSync: %w", err)
	}
	return nil
}

func writeSynced(path string, payload []byte, mode fs.FileMode) error {
	w, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("writeOpen: %w", err)
	}
	writtenSize, err := w.Write(payload)
	if err == nil && writtenSize != len(payload) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = w.Chmod(mode)
	}
	if err == nil {
		err = w.Sync()
	}
	closeErr := w.Close()
	if err != nil {
		return fmt.Errorf("writeSync: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("writeClose: %w", closeErr)
	}
	return nil
}

func readJSON(path string, record any) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("jsonRead: %w", err)
	}
	err = json.Unmarshal(payload, record)
	if err != nil {
		return fmt.Errorf("jsonDecode: %w", err)
	}
	return nil
}

func hashFile(path string) (string, error) {
	r, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("hashOpen: %w", err)
	}
	fi, err := r.Stat()
	if err != nil {
		closeErr := r.Close()
		return "", fmt.Errorf("hashStat: %w", errors.Join(err, closeErr))
	}
	if !fi.Mode().IsRegular() {
		closeErr := r.Close()
		return "", fmt.Errorf("hashMode: %w", errors.Join(errors.New("hash source is not a regular file"), closeErr))
	}
	hash := sha256.New()
	hashedSize, err := io.Copy(hash, r)
	if err == nil && hashedSize != fi.Size() {
		err = errors.New("hash source size changed")
	}
	closeErr := r.Close()
	if err != nil {
		return "", fmt.Errorf("hashRead: %w", err)
	}
	if closeErr != nil {
		return "", fmt.Errorf("hashClose: %w", closeErr)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (e *Storage) copyFile(source, destination string, mode fs.FileMode) error {
	for _, path := range []string{source, destination} {
		err := e.checkPath(path)
		if err != nil {
			return fmt.Errorf("copyCheck: %w", err)
		}
	}
	fi, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("copyStat: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return errors.New("snapshot source is not a regular file")
	}
	err = os.MkdirAll(filepath.Dir(destination), 0700)
	if err != nil {
		return fmt.Errorf("copyDirectory: %w", err)
	}
	r, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("copyOpen: %w", err)
	}
	w, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		closeErr := r.Close()
		return fmt.Errorf("copyCreate: %w", errors.Join(err, closeErr))
	}
	copiedSize, err := io.Copy(w, r)
	if err == nil && copiedSize != fi.Size() {
		err = errors.New("snapshot source size changed")
	}
	if err == nil {
		err = w.Sync()
	}
	readCloseErr := r.Close()
	writeCloseErr := w.Close()
	if err != nil {
		return fmt.Errorf("copyWrite: %w", err)
	}
	if readCloseErr != nil {
		return fmt.Errorf("copyReadClose: %w", readCloseErr)
	}
	if writeCloseErr != nil {
		return fmt.Errorf("copyWriteClose: %w", writeCloseErr)
	}
	err = e.syncParents(filepath.Dir(destination))
	if err != nil {
		return fmt.Errorf("copySync: %w", err)
	}
	return nil
}

func (e *Storage) copyTree(source, destination string) error {
	err := e.checkPath(source)
	if err != nil {
		return fmt.Errorf("treeCheck: %w", err)
	}
	err = e.checkPath(destination)
	if err != nil {
		return fmt.Errorf("treeTarget: %w", err)
	}
	err = os.MkdirAll(destination, 0700)
	if err != nil {
		return fmt.Errorf("treeDirectory: %w", err)
	}
	err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walkEntry: %w", walkErr)
		}
		rel, relErr := filepath.Rel(source, path)
		if relErr != nil {
			return fmt.Errorf("walkRel: %w", relErr)
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			mkdirErr := os.MkdirAll(target, 0700)
			if mkdirErr != nil {
				return fmt.Errorf("walkDirectory: %w", mkdirErr)
			}
			syncErr := e.syncParents(target)
			if syncErr != nil {
				return fmt.Errorf("walkSync: %w", syncErr)
			}
			return nil
		}
		copyErr := e.copyFile(path, target, 0600)
		if copyErr != nil {
			return fmt.Errorf("walkCopy: %w", copyErr)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("treeWalk: %w", err)
	}
	err = syncDirectory(destination)
	if err != nil {
		return fmt.Errorf("treeSync: %w", err)
	}
	return nil
}
