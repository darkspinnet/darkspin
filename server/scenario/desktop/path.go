//go:build scenario

package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/darkspinnet/darkspin/server/scenario"
)

// CheckRegular rejects reparse points in both the file and its ancestors.
func CheckRegular(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("input path must be absolute")
	}
	err := CheckAncestors(path)
	if err != nil {
		return fmt.Errorf("inputAncestors: %w", err)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inputStat: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return errors.New("input must be a regular file")
	}
	return nil
}

func CheckAncestors(path string) error {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		fi, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("ancestorStat: %w", err)
		}
		if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return errors.New("scenario paths cannot traverse links or reparse points")
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}

func ValidateStart(req StartRequest) error {
	err := ValidatePort(req.Port)
	if err != nil {
		return fmt.Errorf("startPort: %w", err)
	}
	err = req.Definition.Validate()
	if err != nil {
		return fmt.Errorf("startDefinition: %w", err)
	}
	if req.RunID == "" || req.RunID != req.Paths.RunID || filepath.Base(req.RunID) != req.RunID || strings.ContainsAny(req.RunID, "/\\:. ") {
		return errors.New("invalid worker run identity")
	}
	root := req.Paths.CacheDirectory
	for index := 0; index < 4; index++ {
		root = filepath.Dir(root)
	}
	expected := scenario.RunPaths{RunID: req.RunID,
		CacheDirectory:  filepath.Join(root, "bin", "cache", "scenario", req.RunID),
		ReportDirectory: filepath.Join(root, "bin", "game", "logs", "scenarios", req.RunID),
		TraceDirectory:  filepath.Join(root, "bin", "server", "darkspin", "logs", "traces", req.RunID)}
	expected.ManifestPath = filepath.Join(expected.ReportDirectory, "manifest.json")
	if req.Paths != expected || !filepath.IsAbs(root) {
		return errors.New("worker paths do not identify the allocated scenario roots")
	}
	for _, directory := range []string{req.Paths.CacheDirectory, req.Paths.ReportDirectory, req.Paths.TraceDirectory, req.GameDirectory} {
		err = CheckAncestors(directory)
		if err != nil {
			return fmt.Errorf("startDirectory: %w", err)
		}
		fi, statErr := os.Lstat(directory)
		if statErr != nil {
			return fmt.Errorf("startStat: %w", statErr)
		}
		if !fi.IsDir() {
			return errors.New("worker directory is not a directory")
		}
	}
	for _, path := range []string{req.ContentPath, req.FangPath} {
		err = CheckRegular(path)
		if err != nil {
			return fmt.Errorf("startInput: %w", err)
		}
	}
	return nil
}

func HashArtifact(ctx context.Context, role, path string) (scenario.Artifact, error) {
	err := CheckRegular(path)
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("hashInput: %w", err)
	}
	r, err := os.Open(path)
	if err != nil {
		return scenario.Artifact{}, fmt.Errorf("hashOpen: %w", err)
	}
	hash := sha256.New()
	buffer := make([]byte, 128*1024)
	for {
		err = ctx.Err()
		if err != nil {
			break
		}
		count, readErr := r.Read(buffer)
		if count > 0 {
			written, hashErr := hash.Write(buffer[:count])
			if hashErr != nil {
				err = fmt.Errorf("hashWrite: %w", hashErr)
				break
			}
			if written != count {
				err = errors.New("short artifact hash write")
				break
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			err = fmt.Errorf("hashRead: %w", readErr)
			break
		}
	}
	closeErr := r.Close()
	if err != nil || closeErr != nil {
		return scenario.Artifact{}, fmt.Errorf("hashFinish: %w", errors.Join(err, closeErr))
	}
	return scenario.Artifact{Role: role, Path: path, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}
