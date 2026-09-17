package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func hashFileInfos(directory string, files []fileInfo) ([]fileInfo, error) {
	hashedFiles := make([]fileInfo, len(files))
	for index, current := range files {
		if !isBundleFileName(current.Name) {
			return nil, fmt.Errorf("fileName[%d]: invalid %q", index, current.Name)
		}
		size, digest, err := hashFile(filepath.Join(directory, current.Name))
		if err != nil {
			return nil, fmt.Errorf("fileDigest[%s]: %w", current.Name, err)
		}
		current.Size = size
		current.SHA256 = digest
		hashedFiles[index] = current
	}
	return hashedFiles, nil
}

func hashFile(path string) (int64, string, error) {
	r, err := os.Open(path)
	if err != nil {
		return 0, "", fmt.Errorf("hashOpen: %w", err)
	}
	digest := sha256.New()
	size, copyErr := io.Copy(digest, r)
	closeErr := r.Close()
	if copyErr != nil {
		if closeErr != nil {
			return 0, "", fmt.Errorf("hashRead: %w; close: %v", copyErr, closeErr)
		}
		return 0, "", fmt.Errorf("hashRead: %w", copyErr)
	}
	if closeErr != nil {
		return 0, "", fmt.Errorf("hashClose: %w", closeErr)
	}
	return size, hex.EncodeToString(digest.Sum(nil)), nil
}

func isBundleFileName(name string) bool {
	if strings.TrimSpace(name) == "" || filepath.IsAbs(name) {
		return false
	}
	cleaned := filepath.Clean(name)
	return cleaned == name && filepath.Base(cleaned) == cleaned &&
		cleaned != "." && cleaned != ".."
}

func isSourceBundleFile(name string) bool {
	switch name {
	case "raknet.jsonl", "server-state.json", "client.jsonl", "client-memory.jsonl":
		return true
	default:
		return false
	}
}

func hasExpectedDigest(current fileInfo) bool {
	return current.Size >= 0 && len(current.SHA256) == sha256.Size*2
}

func isDigestMatch(current fileInfo, size int64, digest string) bool {
	return current.Size == size && strings.EqualFold(current.SHA256, digest)
}

func requireRegularDirectory(path string) (string, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("directoryAbs: %w", err)
	}
	fi, err := os.Stat(absolutePath)
	if err != nil {
		return "", fmt.Errorf("directoryStat: %w", err)
	}
	if !fi.IsDir() {
		return "", errors.New("snapshot bundle path is not a directory")
	}
	return absolutePath, nil
}
