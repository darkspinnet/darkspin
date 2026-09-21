package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

const detachedGameArgument = "detached-game"

// StartDetachedGameInstance starts an independently supervised client while
// the current launcher and its local services remain online.
func (e *App) StartDetachedGameInstance(identity string) error {
	identity = strings.TrimSpace(identity)
	err := validateProfileIdentity(identity)
	if err != nil {
		return fmt.Errorf("detachedIdentity: %w", err)
	}
	e.mu.Lock()
	isReady := e.status.IsAuthOnline && e.status.IsServerOnline &&
		e.status.IsPatchComplete && e.status.IsGameReady
	arguments := append([]string(nil), e.arguments...)
	gameServer := e.serviceSet.gameServer
	e.mu.Unlock()
	if !isReady {
		return errors.New("authentication, server, patch, and game must be ready")
	}
	serverAddress, err := localServerAddress(gameServer)
	if err != nil {
		return fmt.Errorf("detachedServer: %w", err)
	}
	profiles, err := e.GetProfiles()
	if err != nil {
		return fmt.Errorf("detachedProfiles: %w", err)
	}
	isFound := false
	for _, profile := range profiles {
		if profile.LoginName == identity {
			isFound = true
			break
		}
	}
	if !isFound {
		return errors.New("selected Crogenitor does not exist")
	}
	err = e.recordProfileConnection(identity)
	if err != nil {
		return fmt.Errorf("detachedConnection: %w", err)
	}
	err = e.startDetachedGame(detachedGameRequest{
		account: identity, clientProfile: identity, serverAddress: serverAddress,
		arguments: arguments,
	})
	if err != nil {
		return fmt.Errorf("detachedLaunch: %w", err)
	}
	e.log("Detached game instance requested for " + identity)
	return nil
}

type detachedGameRequest struct {
	account       string
	clientProfile string
	serverAddress string
	token         string
	arguments     []string
}

func (e *App) startDetachedGame(req detachedGameRequest) error {
	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("executable: %w", err)
	}
	basePath := filepath.Dir(executablePath)
	isProfileRunning, err := isClientProfileRunning(basePath, req.clientProfile)
	if err != nil {
		return fmt.Errorf("profileCheck: %w", err)
	}
	if isProfileRunning {
		return fmt.Errorf("Crogenitor %s already has a managed game client running", req.account)
	}
	userDataPath, err := prepareClientConfig(basePath, req.clientProfile)
	if err != nil {
		return fmt.Errorf("userData: %w", err)
	}
	arguments := append([]string(nil), req.arguments...)
	arguments = append(
		arguments,
		"--client-trace", detachedTraceName(clientProfilePathName(req.clientProfile), time.Now().UTC()),
	)
	launchID, resultPath, err := newClientFailureTarget(basePath)
	if err != nil {
		return fmt.Errorf("failureTarget: %w", err)
	}
	launchArguments := gameLaunchArguments(arguments, req.token)
	prefixArguments := []string{
		"--" + detachedGameArgument, "--multiple-instances",
		"--user-data-dir", userDataPath,
		"--client-profile", req.clientProfile,
		"--server-address", req.serverAddress,
		"--launch-id", launchID, "--client-result", resultPath,
	}
	if req.token == "" {
		prefixArguments = append(prefixArguments, "--account", req.account)
	}
	launchArguments = append(prefixArguments, launchArguments...)
	err = startDetachedProcess(executablePath, launchArguments)
	if err != nil {
		return fmt.Errorf("processStart: %w", err)
	}
	return nil
}

func prepareClientConfig(basePath, identity string) (string, error) {
	identityHash := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(identity))))
	profileName := fmt.Sprintf("%s-%x", clientProfilePathName(identity), identityHash[:8])
	path := filepath.Join(
		basePath, "darkspin", "config", "profiles", profileName,
	)
	legacyPath := filepath.Join(basePath, "darkspin", "client-data", "detached", profileName)
	err := migrateClientConfig(legacyPath, path)
	if err != nil {
		return "", fmt.Errorf("configMigrate: %w", err)
	}
	err = os.MkdirAll(path, 0o755)
	if err != nil {
		return "", fmt.Errorf("configMkdir: %w", err)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("configPath: %w", err)
	}
	return path, nil
}

func ensureWindowedClientPreference(rootPath string) error {
	preferencePath := filepath.Join(
		rootPath, "AppData", "Roaming", "DarksporeData", "Preferences", "Preferences.prop",
	)
	err := os.MkdirAll(filepath.Dir(preferencePath), 0o755)
	if err != nil {
		return fmt.Errorf("windowedMkdir: %w", err)
	}
	contents, err := os.ReadFile(preferencePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("windowedRead: %w", err)
	}
	// Both windowed and borderless use native windowed rendering. Fang remembers
	// borderless separately in DarkspinDisplay.ini; every other native option is
	// preserved. Build 103 needs OptionVersion 5 when seeding a new profile.
	if errors.Is(err, os.ErrNotExist) {
		err = writeClientPreference(preferencePath, "OptionVersion 5\r\nOptionFullScreen 0\r\n")
		if err != nil {
			return fmt.Errorf("windowedSeed: %w", err)
		}
		return nil
	}
	preference := string(contents)
	lines := strings.SplitAfter(preference, "\n")
	isFullscreenPresent := false
	for index, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.EqualFold(fields[0], "OptionFullScreen") {
			continue
		}
		isFullscreenPresent = true
		if len(fields) >= 2 && fields[1] == "0" {
			continue
		}
		ending := ""
		if strings.HasSuffix(line, "\r\n") {
			ending = "\r\n"
		} else if strings.HasSuffix(line, "\n") {
			ending = "\n"
		}
		lines[index] = "OptionFullScreen 0" + ending
	}
	updated := strings.Join(lines, "")
	if !isFullscreenPresent {
		if updated != "" && !strings.HasSuffix(updated, "\n") {
			updated += "\r\n"
		}
		updated += "OptionFullScreen 0\r\n"
	}
	if updated == preference {
		return nil
	}
	err = writeClientPreference(preferencePath, updated)
	if err != nil {
		return fmt.Errorf("windowedUpdate: %w", err)
	}
	return nil
}

func writeClientPreference(path, preference string) (resultErr error) {
	w, err := os.CreateTemp(filepath.Dir(path), ".preferences-*")
	if err != nil {
		return fmt.Errorf("preferenceCreate: %w", err)
	}
	temporaryPath := w.Name()
	defer func() {
		removeErr := os.Remove(temporaryPath)
		if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, fmt.Errorf("preferenceCleanup: %w", removeErr))
		}
	}()
	written, writeErr := w.WriteString(preference)
	closeErr := w.Close()
	if writeErr != nil {
		return fmt.Errorf("preferenceWrite: %w", errors.Join(writeErr, closeErr))
	}
	if closeErr != nil {
		return fmt.Errorf("preferenceClose: %w", closeErr)
	}
	if written != len(preference) {
		return fmt.Errorf("preferenceSize: wrote %d of %d bytes", written, len(preference))
	}
	err = os.Rename(temporaryPath, path)
	if err != nil {
		return fmt.Errorf("preferenceReplace: %w", err)
	}
	return nil
}

func migrateClientConfig(legacyPath, path string) error {
	_, err := os.Stat(path)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("targetStat: %w", err)
	}
	_, err = os.Stat(legacyPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("legacyStat: %w", err)
	}
	err = os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		return fmt.Errorf("targetMkdir: %w", err)
	}
	err = os.Rename(legacyPath, path)
	if err != nil {
		return fmt.Errorf("legacyMove: %w", err)
	}
	return nil
}

func clientProfilePathName(identity string) string {
	var name strings.Builder
	for _, character := range strings.TrimSpace(identity) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || character == '-' || character == '_' {
			name.WriteRune(character)
		} else {
			name.WriteByte('_')
		}
		if name.Len() >= 32 {
			break
		}
	}
	if name.Len() == 0 {
		return "profile"
	}
	return name.String()
}

func configureClientEnvironment(rootPath string) (string, error) {
	profilePath := rootPath
	roamingPath := filepath.Join(profilePath, "AppData", "Roaming")
	localPath := filepath.Join(profilePath, "AppData", "Local")
	directoryPaths := []string{
		profilePath,
		roamingPath,
		localPath,
		filepath.Join(profilePath, "Documents"),
	}
	for _, directoryPath := range directoryPaths {
		err := os.MkdirAll(directoryPath, 0o755)
		if err != nil {
			return "", fmt.Errorf("profileMkdir: %w", err)
		}
	}
	environmentPaths := map[string]string{
		"APPDATA":      roamingPath,
		"LOCALAPPDATA": localPath,
		"USERPROFILE":  profilePath,
	}
	volumeName := filepath.VolumeName(profilePath)
	if volumeName != "" {
		environmentPaths["HOMEDRIVE"] = volumeName
		environmentPaths["HOMEPATH"] = strings.TrimPrefix(profilePath, volumeName)
	}
	for environmentName, environmentPath := range environmentPaths {
		err := os.Setenv(environmentName, environmentPath)
		if err != nil {
			return "", fmt.Errorf("profileEnvironment[%s]: %w", environmentName, err)
		}
	}
	return profilePath, nil
}

func runDetachedGame(arguments []string) error {
	filteredArguments := make([]string, 0, len(arguments))
	for _, argument := range arguments {
		if argument == "--"+detachedGameArgument || argument == "-"+detachedGameArgument {
			continue
		}
		filteredArguments = append(filteredArguments, argument)
	}
	err := prepareRuntime()
	if err != nil {
		return fmt.Errorf("detachedRuntime: %w", err)
	}
	err = run(context.Background(), filteredArguments)
	if err != nil {
		writeDetachedLaunchError(err)
		return fmt.Errorf("detachedLaunch: %w", err)
	}
	return nil
}

func writeDetachedLaunchError(launchErr error) {
	executablePath, err := os.Executable()
	if err != nil {
		return
	}
	logPath := filepath.Join(filepath.Dir(executablePath), "darkspin", "logs")
	err = os.MkdirAll(logPath, 0o755)
	if err != nil {
		return
	}
	writer, err := os.OpenFile(
		filepath.Join(logPath, fmt.Sprintf(
			"detached-launch-%d-%s.log",
			os.Getpid(), time.Now().UTC().Format("20060102T150405.000000000Z"),
		)),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600,
	)
	if err != nil {
		return
	}
	defer writer.Close()
	_, _ = fmt.Fprintf(writer, "%s %v\n", time.Now().UTC().Format(time.RFC3339), launchErr)
}

func detachedTraceName(identity string, launchTime time.Time) string {
	return fmt.Sprintf(
		"game-detached-%s-%s.jsonl",
		identity, launchTime.Format("20060102T150405.000000000Z"),
	)
}
