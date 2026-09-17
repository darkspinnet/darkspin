package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const integrityFailureHelp = "game files do not match the pristine build; verify integrity of installed files in Steam"
const integrityVerificationInterval = 48 * time.Hour

//go:embed integrity.json
var embeddedGameIntegrity []byte

type gameIntegrityAsset struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type gameIntegrityManifest struct {
	Version   string               `json:"version"`
	Algorithm string               `json:"algorithm"`
	Assets    []gameIntegrityAsset `json:"assets"`
}

type integrityScheduleDocument struct {
	Launcher struct {
		LastVerificationAt string `toml:"last_integrity_verification_at"`
	} `toml:"launcher"`
}

type integrityProgress struct {
	Message  string
	Progress int
}

type integrityProgressReporter interface {
	ReportIntegrityProgress(integrityProgress)
}

type launcherIntegrityReporter struct {
	app   *App
	state string
}

func (e launcherIntegrityReporter) ReportIntegrityProgress(progress integrityProgress) {
	if e.app == nil {
		return
	}
	e.app.setSubsystemProgress("Patch", e.state, progress.Message, progress.Progress)
}

func isGameIntegrityVerificationDue(configPath string, now time.Time) (bool, error) {
	r, err := os.Open(configPath)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("scheduleOpen: %w", err)
	}
	defer r.Close()
	document := integrityScheduleDocument{}
	_, err = toml.NewDecoder(r).Decode(&document)
	if err != nil {
		return false, fmt.Errorf("scheduleDecode: %w", err)
	}
	lastVerificationAt, err := time.Parse(time.RFC3339, document.Launcher.LastVerificationAt)
	if err != nil {
		return true, nil
	}
	return now.Sub(lastVerificationAt) >= integrityVerificationInterval || now.Before(lastVerificationAt), nil
}

func recordGameIntegrityVerification(configPath string, verifiedAt time.Time) error {
	contents, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("scheduleRead: %w", err)
	}
	lineEnding := "\n"
	if bytes.Contains(contents, []byte("\r\n")) {
		lineEnding = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n")
	sectionStart := -1
	sectionEnd := len(lines)
	keyIndex := -1
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if sectionStart >= 0 {
				sectionEnd = index
				break
			}
			if strings.EqualFold(trimmed, "[launcher]") {
				sectionStart = index
			}
			continue
		}
		if sectionStart >= 0 && strings.HasPrefix(strings.ToLower(trimmed), "last_integrity_verification_at") {
			keyIndex = index
		}
	}
	timestampLine := `last_integrity_verification_at = "` + verifiedAt.UTC().Format(time.RFC3339) + `"`
	switch {
	case keyIndex >= 0:
		lines[keyIndex] = timestampLine
	case sectionStart >= 0:
		lines = append(lines[:sectionEnd], append([]string{timestampLine}, lines[sectionEnd:]...)...)
	default:
		for len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		lines = append(lines, "", "[launcher]", timestampLine, "")
	}
	updated := []byte(strings.Join(lines, lineEnding))
	directory := filepath.Dir(configPath)
	r, err := os.CreateTemp(directory, ".darkspin-*.toml")
	if err != nil {
		return fmt.Errorf("scheduleTemp: %w", err)
	}
	tempPath := r.Name()
	defer os.Remove(tempPath)
	_, err = r.Write(updated)
	if err != nil {
		_ = r.Close()
		return fmt.Errorf("scheduleWrite: %w", err)
	}
	err = r.Sync()
	if err != nil {
		_ = r.Close()
		return fmt.Errorf("scheduleSync: %w", err)
	}
	err = r.Close()
	if err != nil {
		return fmt.Errorf("scheduleClose: %w", err)
	}
	err = os.Rename(tempPath, configPath)
	if err != nil {
		return fmt.Errorf("scheduleReplace: %w", err)
	}
	return nil
}

func validateGameIntegrity(ctx context.Context, gamePath string) error {
	return validateGameIntegrityWithProgress(ctx, gamePath, nil)
}

func (e *App) verifyGameIntegrity(ctx context.Context, gamePath, state string) error {
	reporter := launcherIntegrityReporter{app: e, state: state}
	err := validateGameIntegrityWithProgress(ctx, gamePath, reporter)
	if err != nil {
		return fmt.Errorf("integrityValidate: %w", err)
	}
	return nil
}

func validateGameIntegrityWithProgress(
	ctx context.Context,
	gamePath string,
	reporter integrityProgressReporter,
) error {
	return validateGameIntegrityManifestWithProgress(ctx, gamePath, embeddedGameIntegrity, reporter)
}

func validateGameIntegrityManifest(ctx context.Context, gamePath string, manifestJSON []byte) error {
	return validateGameIntegrityManifestWithProgress(ctx, gamePath, manifestJSON, nil)
}

func validateGameIntegrityManifestWithProgress(
	ctx context.Context,
	gamePath string,
	manifestJSON []byte,
	reporter integrityProgressReporter,
) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if strings.TrimSpace(gamePath) == "" {
		return errors.New("empty game path")
	}
	manifest := gameIntegrityManifest{}
	err := json.Unmarshal(manifestJSON, &manifest)
	if err != nil {
		return fmt.Errorf("manifestDecode: %w", err)
	}
	if !strings.EqualFold(manifest.Algorithm, "sha256") {
		return fmt.Errorf("manifestAlgorithm: got %q, want sha256", manifest.Algorithm)
	}
	if len(manifest.Assets) == 0 {
		return errors.New("manifest is empty")
	}
	reportIntegrityProgress(reporter, "Reading integrity manifest", 2)
	gamePath, err = filepath.Abs(gamePath)
	if err != nil {
		return fmt.Errorf("gameResolve: %w", err)
	}
	expected := make(map[string]gameIntegrityAsset, len(manifest.Assets))
	for index, asset := range manifest.Assets {
		cleanPath, cleanErr := cleanIntegrityPath(asset.Path)
		if cleanErr != nil {
			return fmt.Errorf("manifestPath[%d]: %w", index, cleanErr)
		}
		key := strings.ToLower(cleanPath)
		if _, isFound := expected[key]; isFound {
			return fmt.Errorf("manifestDuplicate[%d]: %s", index, cleanPath)
		}
		if asset.Size < 0 {
			return fmt.Errorf("manifestSize[%d]: %d", index, asset.Size)
		}
		digest, decodeErr := hex.DecodeString(asset.SHA256)
		if decodeErr != nil || len(digest) != sha256.Size {
			return fmt.Errorf("manifestDigest[%d]: invalid sha256", index)
		}
		asset.Path = cleanPath
		asset.SHA256 = strings.ToLower(asset.SHA256)
		expected[key] = asset
	}
	reportIntegrityProgress(reporter, "Scanning installed game files", 5)
	actualPath, err := integrityAssetPaths(gamePath)
	if err != nil {
		return fmt.Errorf("%s: assetInventory: %w", integrityFailureHelp, err)
	}
	reportIntegrityProgress(
		reporter,
		fmt.Sprintf("Found %d game files; checking metadata", len(actualPath)),
		10,
	)
	actual := make(map[string]string, len(actualPath))
	for _, relativePath := range actualPath {
		actual[strings.ToLower(relativePath)] = relativePath
	}
	type hashJob struct {
		asset gameIntegrityAsset
		path  string
	}
	type hashResult struct {
		asset  gameIntegrityAsset
		digest string
		err    error
	}
	hashJobSet := make([]hashJob, 0, len(expected))
	problem := make([]string, 0)
	checkedCount := 0
	for key, asset := range expected {
		checkedCount++
		metadataProgress := 10 + checkedCount*15/len(expected)
		reportIntegrityProgress(
			reporter,
			fmt.Sprintf("Checking file %d/%d: %s", checkedCount, len(expected), asset.Path),
			metadataProgress,
		)
		relativePath, isFound := actual[key]
		if !isFound {
			problem = append(problem, "missing "+asset.Path)
			continue
		}
		delete(actual, key)
		path := filepath.Join(gamePath, filepath.FromSlash(relativePath))
		fi, statErr := os.Stat(path)
		if statErr != nil {
			problem = append(problem, fmt.Sprintf("unreadable %s: %v", asset.Path, statErr))
			continue
		}
		if fi.Size() != asset.Size {
			problem = append(problem, fmt.Sprintf(
				"size mismatch %s: got %d, want %d", asset.Path, fi.Size(), asset.Size,
			))
			continue
		}
		hashJobSet = append(hashJobSet, hashJob{asset: asset, path: path})
	}
	for _, relativePath := range actual {
		problem = append(problem, "unexpected "+relativePath)
	}
	hashJobChannel := make(chan hashJob)
	hashResultChannel := make(chan hashResult, len(hashJobSet))
	workerCount := min(4, len(hashJobSet))
	reportIntegrityProgress(
		reporter,
		fmt.Sprintf("Hashing %d game files with %d workers", len(hashJobSet), workerCount),
		25,
	)
	for range workerCount {
		go func() {
			for job := range hashJobChannel {
				digest, hashErr := integrityFileSHA256(ctx, job.path)
				hashResultChannel <- hashResult{asset: job.asset, digest: digest, err: hashErr}
			}
		}()
	}
	go func() {
		for _, job := range hashJobSet {
			hashJobChannel <- job
		}
		close(hashJobChannel)
	}()
	hashedCount := 0
	for range hashJobSet {
		result := <-hashResultChannel
		hashedCount++
		hashProgress := 25
		if len(hashJobSet) > 0 {
			hashProgress += hashedCount * 75 / len(hashJobSet)
		}
		reportIntegrityProgress(
			reporter,
			fmt.Sprintf("Hashed file %d/%d: %s", hashedCount, len(hashJobSet), result.asset.Path),
			hashProgress,
		)
		if result.err != nil {
			if errors.Is(result.err, context.Canceled) || errors.Is(result.err, context.DeadlineExceeded) {
				return fmt.Errorf("assetHash[%s]: %w", result.asset.Path, result.err)
			}
			return fmt.Errorf("%s: assetHash[%s]: %w", integrityFailureHelp, result.asset.Path, result.err)
		}
		if result.digest != result.asset.SHA256 {
			problem = append(problem, "hash mismatch "+result.asset.Path)
		}
	}
	if len(problem) == 0 {
		reportIntegrityProgress(reporter, "Pristine game files verified", 100)
		return nil
	}
	sort.Strings(problem)
	return fmt.Errorf("%s: %s", integrityFailureHelp, strings.Join(problem, "; "))
}

func reportIntegrityProgress(
	reporter integrityProgressReporter,
	message string,
	progress int,
) {
	if reporter == nil {
		return
	}
	reporter.ReportIntegrityProgress(integrityProgress{Message: message, Progress: progress})
}

func cleanIntegrityPath(relativePath string) (string, error) {
	relativePath = strings.TrimSpace(strings.ReplaceAll(relativePath, "\\", "/"))
	if relativePath == "" {
		return "", errors.New("empty path")
	}
	cleanPath := filepath.ToSlash(filepath.Clean(filepath.FromSlash(relativePath)))
	if cleanPath == "." || filepath.IsAbs(filepath.FromSlash(relativePath)) ||
		cleanPath == ".." || strings.HasPrefix(cleanPath, "../") {
		return "", fmt.Errorf("unsafe path %q", relativePath)
	}
	return cleanPath, nil
}

func integrityAssetPaths(gamePath string) ([]string, error) {
	binaryPath := filepath.Join(gamePath, "DarksporeBin")
	executablePath := filepath.Join(binaryPath, "Darkspore.exe")
	fi, err := os.Stat(executablePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("executableStat: %w", err)
	}
	assets := make([]string, 0)
	if err == nil && fi.IsDir() {
		return nil, errors.New("Darkspore.exe is a directory")
	}
	if err == nil {
		assets = append(assets, "DarksporeBin/Darkspore.exe")
	}
	err = filepath.WalkDir(binaryPath, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walkEntry: %w", walkErr)
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".dll") ||
			strings.EqualFold(entry.Name(), "VERSION.dll") {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("assetSymlink: %s", path)
		}
		relativePath, relativeErr := filepath.Rel(gamePath, path)
		if relativeErr != nil {
			return fmt.Errorf("relativePath: %w", relativeErr)
		}
		assets = append(assets, filepath.ToSlash(relativePath))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("binaryWalk: %w", err)
	}
	dataPath := filepath.Join(gamePath, "Data")
	err = filepath.WalkDir(dataPath, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walkEntry: %w", walkErr)
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".package") {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("assetSymlink: %s", path)
		}
		relativePath, relativeErr := filepath.Rel(gamePath, path)
		if relativeErr != nil {
			return fmt.Errorf("relativePath: %w", relativeErr)
		}
		assets = append(assets, filepath.ToSlash(relativePath))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("dataWalk: %w", err)
	}
	sort.Strings(assets)
	return assets, nil
}

func integrityFileSHA256(ctx context.Context, path string) (string, error) {
	r, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("fileOpen: %w", err)
	}
	defer r.Close()
	digest := sha256.New()
	buffer := make([]byte, 1024*1024)
	for {
		err = ctx.Err()
		if err != nil {
			return "", fmt.Errorf("hashContext: %w", err)
		}
		readByte, readErr := r.Read(buffer)
		if readByte > 0 {
			_, writeErr := digest.Write(buffer[:readByte])
			if writeErr != nil {
				return "", fmt.Errorf("hashWrite: %w", writeErr)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return "", fmt.Errorf("fileRead: %w", readErr)
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
