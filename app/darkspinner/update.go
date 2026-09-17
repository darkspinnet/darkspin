package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	maximumUpdateManifestSize = 64 * 1024
	maximumLauncherUpdateSize = 256 * 1024 * 1024
)

type launcherUpdateManifest struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Format  string `json:"format,omitempty"`
}

func (a *App) prepareLauncherUpdate(ctx context.Context) (bool, error) {
	if goruntime.GOOS != "windows" {
		return false, nil
	}
	manifestURL := strings.TrimSpace(launcherUpdateManifestURL)
	if manifestURL == "" {
		return false, nil
	}
	client := &http.Client{Timeout: 30 * time.Second}
	release, err := checkLauncherUpdate(ctx, client, manifestURL, Version)
	if err != nil {
		return false, fmt.Errorf("updateCheck: %w", err)
	}
	if release == nil {
		return false, nil
	}
	executablePath, err := os.Executable()
	if err != nil {
		return false, fmt.Errorf("updateExecutable: %w", err)
	}
	a.setSubsystem("Patch", "Downloading launcher update", false)
	stagedPath, err := downloadLauncherUpdate(ctx, client, *release, executablePath)
	if err != nil {
		return false, fmt.Errorf("updateDownload: %w", err)
	}
	err = replaceAndRestart(stagedPath, executablePath)
	if err != nil {
		_ = os.Remove(stagedPath)
		return false, fmt.Errorf("updateReplace: %w", err)
	}
	a.mu.Lock()
	a.status.Patch = "Restarting with update"
	a.status.Message = "Restarting DarkSpinner"
	a.emitStatusLocked()
	a.mu.Unlock()
	a.beginShutdown()
	if a.ctx != nil {
		runtime.Quit(a.ctx)
	}
	return true, nil
}

func checkLauncherUpdate(
	ctx context.Context,
	client *http.Client,
	manifestURL string,
	currentVersion string,
) (*launcherUpdateManifest, error) {
	if ctx == nil {
		return nil, errors.New("update context is nil")
	}
	if client == nil {
		return nil, errors.New("update client is nil")
	}
	err := validateUpdateURL(manifestURL)
	if err != nil {
		return nil, fmt.Errorf("manifestURL: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("manifestRequest: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("manifestFetch: %w", err)
	}
	defer response.Body.Close()
	err = validateUpdateURL(response.Request.URL.String())
	if err != nil {
		return nil, fmt.Errorf("manifestRedirect: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("manifestStatus: %s", response.Status)
	}
	contents, err := io.ReadAll(io.LimitReader(response.Body, maximumUpdateManifestSize+1))
	if err != nil {
		return nil, fmt.Errorf("manifestRead: %w", err)
	}
	if len(contents) > maximumUpdateManifestSize {
		return nil, errors.New("manifest exceeds size limit")
	}
	contents, err = unwrapUpdateManifest(contents)
	if err != nil {
		return nil, fmt.Errorf("manifestUnwrap: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	decoder.DisallowUnknownFields()
	release := launcherUpdateManifest{}
	err = decoder.Decode(&release)
	if err != nil {
		return nil, fmt.Errorf("manifestDecode: %w", err)
	}
	trailing := struct{}{}
	err = decoder.Decode(&trailing)
	if !errors.Is(err, io.EOF) {
		return nil, errors.New("manifest has trailing content")
	}
	if !semanticVersionPattern.MatchString(release.Version) {
		return nil, fmt.Errorf("manifestVersion: %q", release.Version)
	}
	if release.Format != "" && release.Format != "zip" {
		return nil, fmt.Errorf("manifestFormat: unsupported %q", release.Format)
	}
	err = validateUpdateURL(release.URL)
	if err != nil {
		return nil, fmt.Errorf("artifactURL: %w", err)
	}
	digest, err := hex.DecodeString(strings.TrimSpace(release.SHA256))
	if err != nil || len(digest) != sha256.Size {
		return nil, errors.New("manifest digest is not SHA-256")
	}
	comparison, err := compareReleaseVersion(release.Version, currentVersion)
	if err != nil {
		return nil, fmt.Errorf("versionCompare: %w", err)
	}
	if comparison <= 0 {
		return nil, nil
	}
	return &release, nil
}

func downloadLauncherUpdate(
	ctx context.Context,
	client *http.Client,
	release launcherUpdateManifest,
	executablePath string,
) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, release.URL, nil)
	if err != nil {
		return "", fmt.Errorf("artifactRequest: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("artifactFetch: %w", err)
	}
	defer response.Body.Close()
	err = validateUpdateURL(response.Request.URL.String())
	if err != nil {
		return "", fmt.Errorf("artifactRedirect: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("artifactStatus: %s", response.Status)
	}
	stagedPath := executablePath + ".update-" + strconv.Itoa(os.Getpid())
	w, err := os.OpenFile(stagedPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return "", fmt.Errorf("artifactCreate: %w", err)
	}
	hasher := sha256.New()
	written, copyErr := copyLauncherUpdate(io.MultiWriter(w, hasher), response.Body, release.Format)
	closeErr := w.Close()
	if copyErr != nil {
		_ = os.Remove(stagedPath)
		return "", fmt.Errorf("artifactWrite: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(stagedPath)
		return "", fmt.Errorf("artifactClose: %w", closeErr)
	}
	if written < 2 || written > maximumLauncherUpdateSize {
		_ = os.Remove(stagedPath)
		return "", fmt.Errorf("artifactSize: %d", written)
	}
	expectedDigest, err := hex.DecodeString(strings.TrimSpace(release.SHA256))
	if err != nil {
		_ = os.Remove(stagedPath)
		return "", fmt.Errorf("artifactDigestDecode: %w", err)
	}
	if !equalDigest(hasher.Sum(nil), expectedDigest) {
		_ = os.Remove(stagedPath)
		return "", errors.New("artifact SHA-256 mismatch")
	}
	r, err := os.Open(stagedPath)
	if err != nil {
		_ = os.Remove(stagedPath)
		return "", fmt.Errorf("artifactOpen: %w", err)
	}
	header := make([]byte, 2)
	_, readErr := io.ReadFull(r, header)
	closeReadErr := r.Close()
	if readErr != nil {
		_ = os.Remove(stagedPath)
		return "", fmt.Errorf("artifactHeader: %w", readErr)
	}
	if closeReadErr != nil {
		_ = os.Remove(stagedPath)
		return "", fmt.Errorf("artifactReadClose: %w", closeReadErr)
	}
	if string(header) != "MZ" {
		_ = os.Remove(stagedPath)
		return "", errors.New("artifact is not a Windows executable")
	}
	return stagedPath, nil
}

func validateUpdateURL(address string) error {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	if parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return errors.New("URL authority is invalid")
	}
	if strings.EqualFold(parsed.Scheme, "https") {
		return nil
	}
	hostname := parsed.Hostname()
	ip := net.ParseIP(hostname)
	if strings.EqualFold(parsed.Scheme, "http") &&
		(strings.EqualFold(hostname, "localhost") || ip != nil && ip.IsLoopback()) {
		return nil
	}
	return errors.New("URL must use HTTPS")
}

func compareReleaseVersion(firstVersion, secondVersion string) (int, error) {
	first, err := parseReleaseVersion(firstVersion)
	if err != nil {
		return 0, fmt.Errorf("firstVersion: %w", err)
	}
	second, err := parseReleaseVersion(secondVersion)
	if err != nil {
		return 0, fmt.Errorf("secondVersion: %w", err)
	}
	for index := 0; index < 3; index++ {
		if first.number[index] < second.number[index] {
			return -1, nil
		}
		if first.number[index] > second.number[index] {
			return 1, nil
		}
	}
	return comparePrerelease(first.prerelease, second.prerelease), nil
}

type parsedReleaseVersion struct {
	number     [3]uint64
	prerelease string
}

func parseReleaseVersion(releaseVersion string) (parsedReleaseVersion, error) {
	withoutBuild := strings.SplitN(releaseVersion, "+", 2)[0]
	segments := strings.SplitN(withoutBuild, "-", 2)
	core := segments[0]
	fields := strings.Split(core, ".")
	if len(fields) != 3 {
		return parsedReleaseVersion{}, errors.New("semantic version requires three fields")
	}
	parsed := parsedReleaseVersion{}
	if len(segments) == 2 {
		parsed.prerelease = segments[1]
	}
	for index, field := range fields {
		part, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return parsedReleaseVersion{}, fmt.Errorf("field[%d]: %w", index, err)
		}
		parsed.number[index] = part
	}
	return parsed, nil
}

func comparePrerelease(firstPrerelease, secondPrerelease string) int {
	if firstPrerelease == secondPrerelease {
		return 0
	}
	if firstPrerelease == "" {
		return 1
	}
	if secondPrerelease == "" {
		return -1
	}
	firstFields := strings.Split(firstPrerelease, ".")
	secondFields := strings.Split(secondPrerelease, ".")
	fieldCount := len(firstFields)
	if len(secondFields) < fieldCount {
		fieldCount = len(secondFields)
	}
	for index := 0; index < fieldCount; index++ {
		firstNumber, isFirstNumeric := numericPrerelease(firstFields[index])
		secondNumber, isSecondNumeric := numericPrerelease(secondFields[index])
		if isFirstNumeric && isSecondNumeric {
			if firstNumber < secondNumber {
				return -1
			}
			if firstNumber > secondNumber {
				return 1
			}
			continue
		}
		if isFirstNumeric != isSecondNumeric {
			if isFirstNumeric {
				return -1
			}
			return 1
		}
		if firstFields[index] < secondFields[index] {
			return -1
		}
		if firstFields[index] > secondFields[index] {
			return 1
		}
	}
	if len(firstFields) < len(secondFields) {
		return -1
	}
	return 1
}

func numericPrerelease(field string) (uint64, bool) {
	number, err := strconv.ParseUint(field, 10, 64)
	return number, err == nil
}

func equalDigest(firstDigest, secondDigest []byte) bool {
	if len(firstDigest) != len(secondDigest) {
		return false
	}
	var difference byte
	for index := range firstDigest {
		difference |= firstDigest[index] ^ secondDigest[index]
	}
	return difference == 0
}
