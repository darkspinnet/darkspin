package main

import (
	"bytes"
	"context"
	"debug/pe"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	appwindow "github.com/darkspinnet/darkspin/window"
	"github.com/wailsapp/wails/v2"
	wailsoptions "github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	linuxoptions "github.com/wailsapp/wails/v2/pkg/options/linux"
)

// Version is replaced by Mage release builds and remains useful in direct Go builds.
var Version = "0.5.0"

// authServiceURL is replaced at link time for distributed DarkSpinner builds. It
// has no runtime override so one darkspinner.exe always targets its trusted service.
var authServiceURL = "http://127.0.0.1:42127"

// patchManifestURL is replaced at link time when a server distributes
// mod-owned file updates. An empty value leaves patch checking disabled.
var patchManifestURL string

// launcherUpdateManifestURL follows the latest stable GitHub release. Mage can
// override it at link time. Its manifest updates only the launcher executable.
var launcherUpdateManifestURL = "https://github.com/darkspinnet/darkspin/" +
	"releases/latest/download/darkspinner-update.json"

const (
	launchJWTEnvironment          = "DARKSPIN_LAUNCH_JWT"
	skipIntroEnvironment          = "DARKSPIN_SKIP_INTRO"
	skipCinematicEnvironment      = "DARKSPIN_SKIP_CINEMATIC"
	darkSpinnerVersionEnvironment = "DARKSPINNER_VERSION"
	serverAddressEnvironment      = "DARKSPIN_SERVER_ADDRESS"
	snapshotModeEnvironment       = "DARKSPIN_SNAPSHOT_MODE"
	snapshotControlEnvironment    = "DARKSPIN_SNAPSHOT_CONTROL"
	maximumJWTLength              = 16 * 1024
)

type options struct {
	gameExecutable            string
	fangDLL                   string
	isVersionRequested        bool
	clientTrace               string
	clientResult              string
	launchID                  string
	clientProfile             string
	isIntroSkipped            bool
	isCinematicSkipped        bool
	isMultipleInstanceAllowed bool
	userDataDirectory         string
	jwt                       string
	jwtFile                   string
	account                   string
	serverAddress             string
	legacyServerHost          string
	isAutoPlayRequested       bool
}

func main() {
	privilegeErr := ensureStandardUser()
	err := configureDarkSpinnerVersion()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "DarkSpinner version failed:", err)
		os.Exit(1)
	}
	if hasVersionArgument(os.Args[1:]) {
		fmt.Println(Version)
		return
	}
	if hasLaunchArgument(os.Args[1:], detachedGameArgument) {
		if privilegeErr != nil {
			_, _ = fmt.Fprintln(os.Stderr, privilegeLaunchMessage(privilegeErr))
			return
		}
		err = runDetachedGame(os.Args[1:])
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "DarkSpinner detached launch failed:", err)
		}
		return
	}
	releaseInstance, isExistingInstance, err := appwindow.Acquire(appwindow.InstanceOptions{
		MutexName: `Local\darkspin-darkspinner-single-instance-v1`,
	})
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "DarkSpinner instance check failed:", err)
		os.Exit(1)
	}
	if isExistingInstance {
		return
	}
	defer releaseInstance()
	var integrationErr error
	if privilegeErr == nil {
		integrationErr = completePendingInstallation(os.Args[1:])
		if integrationErr != nil {
			_, _ = fmt.Fprintln(os.Stderr, "DarkSpinner integration setup failed:", integrationErr)
		}
	}
	err = prepareRuntime()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "DarkSpinner startup failed:", err)
		os.Exit(1)
	}
	app := NewApp(removeInstallationArguments(os.Args[1:]))
	if privilegeErr != nil {
		app.startupError = privilegeLaunchMessage(privilegeErr)
	}
	if integrationErr != nil {
		app.integrationError = integrationErr.Error()
	}
	err = wails.Run(&wailsoptions.App{
		Title:            "DarkSpinner",
		Width:            800,
		Height:           560,
		MinWidth:         680,
		MinHeight:        500,
		Frameless:        false,
		BackgroundColour: &wailsoptions.RGBA{R: 8, G: 15, B: 22, A: 1},
		AssetServer:      &assetserver.Options{Assets: frontendAssets},
		Linux: &linuxoptions.Options{
			Icon: darkSpinnerIcon, ProgramName: "darkspinner",
		},
		OnStartup:     app.startup,
		OnBeforeClose: app.beforeClose,
		OnShutdown:    app.shutdown,
		Bind:          []interface{}{app},
	})
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "DarkSpinner failed:", err)
		os.Exit(1)
	}
}

func privilegeLaunchMessage(err error) string {
	if errors.Is(err, errElevatedLaunch) {
		return "DarkSpinner cannot run as an administrator. Close this launcher and start it normally without Run as administrator."
	}
	return fmt.Sprintf("DarkSpinner could not verify standard-user mode: %v", err)
}

func hasVersionArgument(arguments []string) bool {
	for _, argument := range arguments {
		if argument == "--version" || argument == "-version" {
			return true
		}
	}
	return false
}

func run(ctx context.Context, arguments []string) error {
	if ctx == nil {
		return errors.New("launch context is nil")
	}
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("launchContext: %w", err)
	}
	err = configureDarkSpinnerVersion()
	if err != nil {
		return fmt.Errorf("versionConfigure: %w", err)
	}
	baseDirectory, err := executableDirectory()
	if err != nil {
		return fmt.Errorf("launcherPath: %w", err)
	}
	pathSet, err := resolveSpinnerPaths(baseDirectory)
	if err != nil {
		return fmt.Errorf("runtimePath: %w", err)
	}
	configuration := options{}
	flags := flag.NewFlagSet("launch", flag.ContinueOnError)
	flags.StringVar(&configuration.gameExecutable, "game-exe", defaultGameExecutable(baseDirectory), "game executable under this Darkspinner root")
	flags.StringVar(&configuration.fangDLL, "fang", "", "path to an external fang.dll override")
	flags.BoolVar(&configuration.isVersionRequested, "version", false, "print launch version")
	flags.StringVar(&configuration.clientTrace, "client-trace", "", "write redacted client socket events as JSONL")
	flags.StringVar(&configuration.clientTrace, "trace", "", "write redacted client socket events as JSONL (alias for --client-trace)")
	flags.StringVar(&configuration.clientResult, "client-result", "", "write a PID-bound client failure result")
	flags.StringVar(&configuration.launchID, "launch-id", "", "correlate one managed game process with its launcher")
	flags.StringVar(&configuration.clientProfile, "client-profile", "", "bind a managed game process to a launcher profile")
	flags.BoolVar(&configuration.isIntroSkipped, "skip-intro", false, "skip the EA and Maxis startup videos")
	flags.BoolVar(&configuration.isCinematicSkipped, "skip-cinematic", false, "skip startup videos and the post-login transition cinematic")
	flags.BoolVar(&configuration.isMultipleInstanceAllowed, "multiple-instances", false, "allow a detached client beside an existing game process")
	flags.StringVar(&configuration.userDataDirectory, "user-data-dir", "", "use a specific isolated writable client configuration directory")
	flags.StringVar(&configuration.jwt, "jwt", "", "short-lived login JWT (development use; prefer a .dsjwt file or game login URI)")
	flags.StringVar(&configuration.jwtFile, "jwt-file", "", "read a short-lived login JWT from a file")
	flags.StringVar(&configuration.account, "account", "", "account identity to authorize through the desktop auth service")
	flags.StringVar(&configuration.serverAddress, "server-address", "", "route this game process to a configured Darkspin IP address and port")
	flags.StringVar(&configuration.legacyServerHost, "server-host", "", "deprecated host-only alias for --server-address")
	flags.BoolVar(&configuration.isAutoPlayRequested, "auto-play", false, "authorize, patch, and launch when the patcher opens")
	err = flags.Parse(arguments)
	if err != nil {
		return fmt.Errorf("argsParse: %w", err)
	}
	if configuration.isVersionRequested {
		fmt.Println(Version)
		return nil
	}
	if configuration.serverAddress == "" && configuration.legacyServerHost != "" {
		configuration.serverAddress = net.JoinHostPort(
			configuration.legacyServerHost, fmt.Sprintf("%d", defaultRemoteServerPort),
		)
	}
	jwt, gameArguments, err := resolveLaunchJWT(configuration.jwt, configuration.jwtFile, flags.Args())
	if err != nil {
		return fmt.Errorf("jwtResolve: %w", err)
	}
	if configuration.account != "" && jwt != "" {
		return errors.New("--account cannot be combined with a JWT credential")
	}
	if configuration.account != "" {
		accountAuthURL := authServiceURL
		if configuration.serverAddress != "" && isLoopbackAuthURL(accountAuthURL) {
			accountAuthURL = "http://" + configuration.serverAddress
		}
		jwt, err = requestLaunchJWTContext(ctx, accountAuthURL, configuration.account)
		if err != nil {
			return fmt.Errorf("accountAuthorize: %w", err)
		}
	}
	gameArguments = append(gameArguments, "-nolauncher")
	if configuration.isMultipleInstanceAllowed {
		gameArguments = append(gameArguments, "-multipleInstances")
	}
	if configuration.userDataDirectory == "" {
		identity := configuration.clientProfile
		if identity == "" {
			identity = configuration.account
		}
		if identity == "" {
			identity = "default"
		}
		configuration.userDataDirectory, err = prepareClientConfig(baseDirectory, identity)
		if err != nil {
			return fmt.Errorf("clientConfig: %w", err)
		}
	}
	clientProfilePath, err := configureClientEnvironment(configuration.userDataDirectory)
	if err != nil {
		return fmt.Errorf("clientEnvironment: %w", err)
	}
	gameArguments = append(gameArguments, "-userDataDir:"+clientProfilePath)
	err = configureLaunchJWT(jwt)
	if err != nil {
		return fmt.Errorf("jwtConfigure: %w", err)
	}
	defer os.Unsetenv(launchJWTEnvironment)
	gamePath, gameWorkingDirectory, err := resolveGameExecutable(configuration.gameExecutable, baseDirectory)
	if err != nil {
		return fmt.Errorf("gameResolve: %w", err)
	}
	err = validateGameData(gameWorkingDirectory)
	if err != nil {
		return fmt.Errorf("gameData: %w", err)
	}
	gameConfig, _, err := game.LoadConfig(pathSet.configPath)
	if err != nil {
		return fmt.Errorf("localeConfig: %w", err)
	}
	err = configureSnapshotEnvironment(
		gameConfig.String(game.ConfigSnapshotMode),
		filepath.Join(pathSet.tracePath, "snapshot-control.txt"),
	)
	if err != nil {
		return fmt.Errorf("snapshotEnvironment: %w", err)
	}
	defer func() {
		unsetErr := os.Unsetenv(snapshotModeEnvironment)
		if unsetErr != nil {
			// The launched child already owns an independent environment block.
		}
		unsetErr = os.Unsetenv(snapshotControlEnvironment)
		if unsetErr != nil {
			// The launched child already owns an independent environment block.
		}
	}()
	clientLocales, err := detectClientLocales(pathSet.gamePath)
	if err != nil {
		return fmt.Errorf("localeDetect: %w", err)
	}
	clientLocale, err := resolveClientLocale(
		gameConfig.String(game.ConfigClientLocale), clientLocales,
	)
	if err != nil {
		return fmt.Errorf("localeResolve: %w", err)
	}
	gameArguments = setClientLocaleArgument(gameArguments, clientLocale)
	err = configureClientTrace(configuration.clientTrace, pathSet.tracePath)
	if err != nil {
		return fmt.Errorf("clientTrace: %w", err)
	}
	clientProfile := configuration.clientProfile
	if clientProfile == "" {
		clientProfile = configuration.account
	}
	err = configureClientFailure(
		configuration.clientResult, configuration.launchID, pathSet.tracePath,
		clientProfile, configuration.isMultipleInstanceAllowed,
	)
	if err != nil {
		return fmt.Errorf("clientFailure: %w", err)
	}
	err = configureCinematicOptions(configuration.isIntroSkipped, configuration.isCinematicSkipped)
	if err != nil {
		return fmt.Errorf("cinematicOption: %w", err)
	}
	fangPath, err := materializeFang(configuration.fangDLL, pathSet.fangPath, embeddedFang)
	if err != nil {
		return fmt.Errorf("fangPrepare: %w", err)
	}
	err = launchInjected(
		ctx, gamePath, installDirectory(gameWorkingDirectory), fangPath,
		gameArguments, configuration.serverAddress,
	)
	if err != nil {
		return fmt.Errorf("gameLaunch: %w", err)
	}
	return nil
}

var semanticVersionPattern = regexp.MustCompile(
	`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`,
)

func configureDarkSpinnerVersion() error {
	if !semanticVersionPattern.MatchString(Version) {
		return fmt.Errorf("semanticVersion: %q", Version)
	}
	err := os.Setenv(darkSpinnerVersionEnvironment, Version)
	if err != nil {
		return fmt.Errorf("environmentSet: %w", err)
	}
	return nil
}

func requestLaunchJWT(authURL, account string) (string, error) {
	return requestLaunchJWTWithBrowserContext(context.Background(), authURL, account, nil)
}

func requestLaunchJWTWithBrowser(authURL, account string, openBrowser func(string)) (string, error) {
	return requestLaunchJWTWithBrowserContext(context.Background(), authURL, account, openBrowser)
}

func requestLaunchJWTContext(ctx context.Context, authURL, account string) (string, error) {
	return requestLaunchJWTWithBrowserContext(ctx, authURL, account, nil)
}

func requestLaunchJWTWithBrowserContext(ctx context.Context, authURL, account string, openBrowser func(string)) (string, error) {
	return requestLaunchJWTWithPasswordContext(ctx, authURL, account, "", openBrowser)
}

func requestLaunchJWTWithPasswordContext(
	ctx context.Context, authURL, account, password string, openBrowser func(string),
) (string, error) {
	if ctx == nil {
		return "", errors.New("authentication context is nil")
	}
	baseURL, err := normalizeAuthURL(authURL)
	if err != nil {
		return "", fmt.Errorf("authURL: %w", err)
	}
	account = strings.TrimSpace(account)
	if account == "" {
		return "", errors.New("account is empty")
	}
	client := &http.Client{Timeout: 15 * time.Second}
	startResponse := struct {
		Code             string `json:"code"`
		AuthorizationURL string `json:"authorization_url"`
	}{}
	err = postAuthJSON(ctx, client, baseURL+"/api/desktop/start", map[string]string{
		"account": account, "password": password,
	}, &startResponse)
	if err != nil {
		return "", fmt.Errorf("desktopStart: %w", err)
	}
	if startResponse.Code == "" {
		return "", errors.New("desktop start returned an empty code")
	}
	if startResponse.AuthorizationURL != "" && openBrowser != nil {
		openBrowser(startResponse.AuthorizationURL)
	}
	exchangeResponse := struct {
		LaunchToken string `json:"launch_token"`
	}{}
	err = postAuthJSON(ctx, client, baseURL+"/api/desktop/exchange", map[string]string{"code": startResponse.Code}, &exchangeResponse)
	if err != nil {
		return "", fmt.Errorf("desktopExchange: %w", err)
	}
	token, err := normalizeJWT(exchangeResponse.LaunchToken)
	if err != nil {
		return "", fmt.Errorf("launchToken: %w", err)
	}
	return token, nil
}

func normalizeAuthURL(rawURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", fmt.Errorf("urlParse: %w", err)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Host == "" {
		return "", errors.New("authentication URL must be an origin without credentials, query, or fragment")
	}
	hostname := parsed.Hostname()
	ip := net.ParseIP(hostname)
	isLocalNetwork := strings.EqualFold(hostname, "localhost") ||
		ip != nil && (ip.IsLoopback() || ip.IsPrivate())
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLocalNetwork) {
		return "", errors.New("authentication URL must use HTTPS unless it is on the local network")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func postAuthJSON(ctx context.Context, client *http.Client, endpoint string, requestPayload, responsePayload any) error {
	contents, err := json.Marshal(requestPayload)
	if err != nil {
		return fmt.Errorf("requestMarshal: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(contents))
	if err != nil {
		return fmt.Errorf("requestCreate: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("requestSend: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
		if readErr != nil {
			return fmt.Errorf("errorRead: %w", readErr)
		}
		return fmt.Errorf("authentication service returned %s: %s", response.Status, strings.TrimSpace(string(message)))
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 16*1024))
	err = decoder.Decode(responsePayload)
	if err != nil {
		return fmt.Errorf("responseDecode: %w", err)
	}
	return nil
}

func resolveLaunchJWT(configuredJWT, configuredFile string, arguments []string) (string, []string, error) {
	if configuredJWT != "" && configuredFile != "" {
		return "", nil, errors.New("--jwt and --jwt-file cannot be used together")
	}
	if configuredJWT != "" {
		token, err := normalizeJWT(configuredJWT)
		if err != nil {
			return "", nil, fmt.Errorf("argumentToken: %w", err)
		}
		return token, arguments, nil
	}
	if configuredFile != "" {
		token, err := readJWTFile(configuredFile)
		if err != nil {
			return "", nil, fmt.Errorf("configuredFile: %w", err)
		}
		return token, arguments, nil
	}
	if len(arguments) == 0 {
		return "", arguments, nil
	}
	credential := arguments[0]
	if strings.HasPrefix(strings.ToLower(credential), "game://") {
		token, err := jwtFromURI(credential)
		if err != nil {
			return "", nil, fmt.Errorf("uriToken: %w", err)
		}
		return token, arguments[1:], nil
	}
	if strings.EqualFold(filepath.Ext(credential), ".dsjwt") {
		token, err := readJWTFile(credential)
		if err != nil {
			return "", nil, fmt.Errorf("fileToken: %w", err)
		}
		return token, arguments[1:], nil
	}
	return "", arguments, nil
}

func jwtFromURI(rawURI string) (string, error) {
	parsed, err := url.Parse(rawURI)
	if err != nil {
		return "", fmt.Errorf("uriParse: %w", err)
	}
	if !strings.EqualFold(parsed.Scheme, "game") {
		return "", errors.New("unsupported game URI scheme")
	}
	if parsed.Host != "" && !strings.EqualFold(parsed.Host, "login") {
		return "", fmt.Errorf("unsupported game URI action %q", parsed.Host)
	}
	token, err := normalizeJWT(parsed.Query().Get("token"))
	if err != nil {
		return "", fmt.Errorf("queryToken: %w", err)
	}
	return token, nil
}

func readJWTFile(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("fileRead: %w", err)
	}
	token, err := normalizeJWT(string(contents))
	if err != nil {
		return "", fmt.Errorf("fileContents: %w", err)
	}
	return token, nil
}

func normalizeJWT(rawJWT string) (string, error) {
	token := strings.TrimSpace(rawJWT)
	if token == "" {
		return "", errors.New("JWT is empty")
	}
	if len(token) > maximumJWTLength {
		return "", fmt.Errorf("JWT exceeds %d bytes", maximumJWTLength)
	}
	if strings.Count(token, ".") != 2 {
		return "", errors.New("JWT must contain three segments")
	}
	return token, nil
}

func configureLaunchJWT(token string) error {
	if token == "" {
		err := os.Unsetenv(launchJWTEnvironment)
		if err != nil {
			return fmt.Errorf("jwtUnset: %w", err)
		}
		return nil
	}
	err := os.Setenv(launchJWTEnvironment, token)
	if err != nil {
		return fmt.Errorf("jwtSet: %w", err)
	}
	return nil
}

func defaultGameExecutable(baseDirectory string) string {
	return filepath.Join(baseDirectory, "DarksporeBin", "Darkspore.exe")
}

func validateGameData(gameWorkingDirectory string) error {
	versionPath := filepath.Join(installDirectory(gameWorkingDirectory), "Data", "version_data.txt")
	fi, err := os.Stat(versionPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("game data is missing at %s; place Darkspinner in a complete game root", filepath.Dir(versionPath))
		}
		return fmt.Errorf("versionStat: %w", err)
	}
	if fi.IsDir() {
		return fmt.Errorf("game data version path is a directory: %s", versionPath)
	}
	return nil
}

func installDirectory(gameExecutableDirectory string) string {
	if strings.EqualFold(filepath.Base(gameExecutableDirectory), "DarksporeBin") {
		return filepath.Dir(gameExecutableDirectory)
	}
	return gameExecutableDirectory
}

func resolveFang(configuredPath, launcherDirectory string) (string, error) {
	if configuredPath != "" {
		resolvedPath, err := existingAbsoluteFile(configuredPath)
		if err != nil {
			return "", fmt.Errorf("hookConfigured: %w", err)
		}
		return resolvedPath, nil
	}
	resolvedPath, err := existingAbsoluteFile(filepath.Join(launcherDirectory, "fang.dll"))
	if err != nil {
		return "", fmt.Errorf("hookLauncher: %w", err)
	}
	return resolvedPath, nil
}

func materializeFang(configuredPath, fangPath string, payload []byte) (string, error) {
	if configuredPath != "" {
		fangPath, err := resolveFang(configuredPath, filepath.Dir(fangPath))
		if err != nil {
			return "", fmt.Errorf("externalResolve: %w", err)
		}
		return fangPath, nil
	}
	fangPath, err := ensureFang(fangPath, payload)
	if err != nil {
		return "", fmt.Errorf("embeddedWrite: %w", err)
	}
	return fangPath, nil
}

func ensureFang(path string, payload []byte) (string, error) {
	resolvedPath, err := existingAbsoluteFile(path)
	if err == nil {
		if len(payload) == 0 {
			return resolvedPath, nil
		}
		contents, readErr := os.ReadFile(resolvedPath)
		if readErr != nil {
			return "", fmt.Errorf("fangRead: %w", readErr)
		}
		if bytes.Equal(contents, payload) {
			return resolvedPath, nil
		}
		err = os.WriteFile(resolvedPath, payload, 0o644)
		if err != nil {
			return "", fmt.Errorf("fangRefresh: %w", err)
		}
		return resolvedPath, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("fangStat: %w", err)
	}
	if len(payload) == 0 {
		return "", errors.New("fang.dll is missing and this development build has no embedded Fang payload")
	}
	err = os.WriteFile(path, payload, 0o644)
	if err != nil {
		return "", fmt.Errorf("fangWrite: %w", err)
	}
	resolvedPath, err = existingAbsoluteFile(path)
	if err != nil {
		return "", fmt.Errorf("fangVerify: %w", err)
	}
	return resolvedPath, nil
}

func resolveGameExecutable(configuredPath, baseDirectory string) (string, string, error) {
	defaultPath := defaultGameExecutable(baseDirectory)
	configuredAbsolutePath, absoluteErr := filepath.Abs(configuredPath)
	if absoluteErr != nil {
		return "", "", fmt.Errorf("configuredPath: %w", absoluteErr)
	}
	defaultAbsolutePath, absoluteErr := filepath.Abs(defaultPath)
	if absoluteErr != nil {
		return "", "", fmt.Errorf("defaultPath: %w", absoluteErr)
	}
	if !strings.EqualFold(filepath.Clean(configuredAbsolutePath), filepath.Clean(defaultAbsolutePath)) {
		return "", "", fmt.Errorf("gameRoot: Darkspinner requires %s", defaultAbsolutePath)
	}

	gameDirectory := filepath.Join(baseDirectory, "DarksporeBin")
	fi, directoryErr := os.Stat(gameDirectory)
	if directoryErr != nil {
		return "", "", fmt.Errorf("binStat: %w", directoryErr)
	}
	if !fi.IsDir() {
		return "", "", fmt.Errorf("%s is not a directory", gameDirectory)
	}

	gamePath, err := existingAbsoluteFile(filepath.Join(gameDirectory, "Darkspore.exe"))
	if err != nil {
		return "", "", fmt.Errorf("binGame: %w", err)
	}
	return gamePath, gameDirectory, nil
}

func configureClientTrace(path, traceDirectory string) error {
	if path == "" {
		err := os.Unsetenv("DARKSPIN_CLIENT_TRACE")
		if err != nil {
			return fmt.Errorf("traceUnset: %w", err)
		}
		return nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(traceDirectory, path)
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("tracePath: %w", err)
	}
	err = os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		return fmt.Errorf("traceMkdir: %w", err)
	}
	err = os.Setenv("DARKSPIN_CLIENT_TRACE", path)
	if err != nil {
		return fmt.Errorf("traceSet: %w", err)
	}
	return nil
}

func configureSnapshotEnvironment(mode string, controlPath string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "manual" && mode != "auto" && mode != "off" {
		return errors.New("snapshot mode must be manual, auto, or off")
	}
	err := os.Setenv(snapshotModeEnvironment, mode)
	if err != nil {
		return fmt.Errorf("snapshotSet: %w", err)
	}
	err = os.Setenv(snapshotControlEnvironment, controlPath)
	if err != nil {
		return fmt.Errorf("snapshotControlSet: %w", err)
	}
	return nil
}

func configureCinematicOptions(isIntroSkipped, isCinematicSkipped bool) error {
	if isCinematicSkipped {
		isIntroSkipped = true
	}
	err := configureBooleanEnvironment(skipIntroEnvironment, isIntroSkipped)
	if err != nil {
		return fmt.Errorf("introEnvironment: %w", err)
	}
	err = configureBooleanEnvironment(skipCinematicEnvironment, isCinematicSkipped)
	if err != nil {
		return fmt.Errorf("cinematicEnvironment: %w", err)
	}
	return nil
}

func configureBooleanEnvironment(name string, isEnabled bool) error {
	if isEnabled {
		err := os.Setenv(name, "1")
		if err != nil {
			return fmt.Errorf("set: %w", err)
		}
		return nil
	}
	err := os.Unsetenv(name)
	if err != nil {
		return fmt.Errorf("unset: %w", err)
	}
	return nil
}

func setClientLocaleArgument(arguments []string, locale string) []string {
	updatedArguments := make([]string, 0, len(arguments)+1)
	for _, argument := range arguments {
		lowerArgument := strings.ToLower(argument)
		if strings.HasPrefix(lowerArgument, "-locale:") ||
			strings.HasPrefix(lowerArgument, "--locale:") {
			continue
		}
		updatedArguments = append(updatedArguments, argument)
	}
	return append(updatedArguments, "-locale:"+locale)
}

func gameExitError(exitCode uint32) error {
	description, isKnown := gameExitDescription(exitCode)
	if !isKnown {
		description = "game exited without a known launcher diagnosis"
	}
	return fmt.Errorf("game exited with code %d (0x%08x): %s", int32(exitCode), exitCode, description)
}

func gameExitDescription(exitCode uint32) (string, bool) {
	description := map[uint32]string{
		0xffffffff: "generic game failure; known causes include rejected login or lost server connection, missing game data, and an incompatible fang.dll hook",
		0x40000015: "game requested a fatal application exit",
		0x80000003: "breakpoint exception, usually from a failed assertion or debugger trap",
		0xc0000005: "access violation caused by invalid memory access",
		0xc000001d: "illegal CPU instruction",
		0xc0000094: "integer division by zero",
		0xc00000fd: "thread stack overflow",
		0xc0000135: "a required DLL could not be found",
		0xc0000139: "a required DLL entry point could not be found",
		0xc0000142: "DLL initialization failed",
		0xc0000374: "heap corruption detected",
		0xc0000409: "stack buffer overrun or fail-fast termination",
		0xd15c0001: "Darkspinner closed the client after its login watchdog expired",
	}[exitCode]
	return description, description != ""
}

type peExportDirectory struct {
	Characteristics       uint32
	TimeDateStamp         uint32
	MajorVersion          uint16
	MinorVersion          uint16
	Name                  uint32
	Base                  uint32
	NumberOfFunctions     uint32
	NumberOfNames         uint32
	AddressOfFunctions    uint32
	AddressOfNames        uint32
	AddressOfNameOrdinals uint32
}

func exportedFunctionRVA(path, functionName string) (uint32, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("peRead[%q]: %w", path, err)
	}
	executable, err := pe.NewFile(bytes.NewReader(contents))
	if err != nil {
		return 0, fmt.Errorf("peParse[%q]: %w", path, err)
	}
	defer executable.Close()
	optionalHeader, isValid := executable.OptionalHeader.(*pe.OptionalHeader32)
	if !isValid {
		return 0, errors.New("fang.dll is not a 32-bit PE image")
	}
	exports := optionalHeader.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_EXPORT]
	if exports.VirtualAddress == 0 {
		return 0, errors.New("fang.dll has no export directory")
	}
	exportOffset, err := peFileOffset(executable, optionalHeader, exports.VirtualAddress)
	if err != nil {
		return 0, fmt.Errorf("exportOffset: %w", err)
	}
	reader := bytes.NewReader(contents[exportOffset:])
	directory := peExportDirectory{}
	err = binary.Read(reader, binary.LittleEndian, &directory)
	if err != nil {
		return 0, fmt.Errorf("exportDecode: %w", err)
	}
	for index := uint32(0); index < directory.NumberOfNames; index++ {
		nameRVA, readErr := peUint32(contents, executable, optionalHeader, directory.AddressOfNames+index*4)
		if readErr != nil {
			return 0, fmt.Errorf("nameRVA[%d]: %w", index, readErr)
		}
		name, readErr := peString(contents, executable, optionalHeader, nameRVA)
		if readErr != nil {
			return 0, fmt.Errorf("exportName[%d]: %w", index, readErr)
		}
		if name != functionName {
			continue
		}
		ordinal, readErr := peUint16(contents, executable, optionalHeader, directory.AddressOfNameOrdinals+index*2)
		if readErr != nil {
			return 0, fmt.Errorf("ordinal[%d]: %w", index, readErr)
		}
		if uint32(ordinal) >= directory.NumberOfFunctions {
			return 0, errors.New("fang.dll export ordinal is out of range")
		}
		functionRVA, readErr := peUint32(contents, executable, optionalHeader, directory.AddressOfFunctions+uint32(ordinal)*4)
		if readErr != nil {
			return 0, fmt.Errorf("functionRVA[%d]: %w", ordinal, readErr)
		}
		if functionRVA >= exports.VirtualAddress && functionRVA < exports.VirtualAddress+exports.Size {
			return 0, errors.New("fang.dll initializer is a forwarded export")
		}
		return functionRVA, nil
	}
	return 0, fmt.Errorf("fang.dll export %s was not found", functionName)
}

func peFileOffset(executable *pe.File, optionalHeader *pe.OptionalHeader32, rva uint32) (int, error) {
	if rva < optionalHeader.SizeOfHeaders {
		return int(rva), nil
	}
	for _, section := range executable.Sections {
		size := section.VirtualSize
		if section.Size > size {
			size = section.Size
		}
		if rva >= section.VirtualAddress && rva < section.VirtualAddress+size {
			return int(section.Offset + rva - section.VirtualAddress), nil
		}
	}
	return 0, fmt.Errorf("PE RVA 0x%x is outside every section", rva)
}

func peUint32(contents []byte, executable *pe.File, optionalHeader *pe.OptionalHeader32, rva uint32) (uint32, error) {
	offset, err := peFileOffset(executable, optionalHeader, rva)
	if err != nil {
		return 0, fmt.Errorf("u32Offset[%x]: %w", rva, err)
	}
	if offset < 0 || offset+4 > len(contents) {
		return 0, errors.New("PE uint32 lies outside the image")
	}
	return binary.LittleEndian.Uint32(contents[offset : offset+4]), nil
}

func peUint16(contents []byte, executable *pe.File, optionalHeader *pe.OptionalHeader32, rva uint32) (uint16, error) {
	offset, err := peFileOffset(executable, optionalHeader, rva)
	if err != nil {
		return 0, fmt.Errorf("u16Offset[%x]: %w", rva, err)
	}
	if offset < 0 || offset+2 > len(contents) {
		return 0, errors.New("PE uint16 lies outside the image")
	}
	return binary.LittleEndian.Uint16(contents[offset : offset+2]), nil
}

func peString(contents []byte, executable *pe.File, optionalHeader *pe.OptionalHeader32, rva uint32) (string, error) {
	offset, err := peFileOffset(executable, optionalHeader, rva)
	if err != nil {
		return "", fmt.Errorf("stringOffset[%x]: %w", rva, err)
	}
	if offset < 0 || offset >= len(contents) {
		return "", errors.New("PE string lies outside the image")
	}
	end := bytes.IndexByte(contents[offset:], 0)
	if end < 0 {
		return "", errors.New("PE string is not terminated")
	}
	return string(contents[offset : offset+end]), nil
}

func existingAbsoluteFile(path string) (string, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("filePath[%q]: %w", path, err)
	}
	fi, err := os.Stat(absolutePath)
	if err != nil {
		return "", fmt.Errorf("fileStat[%q]: %w", absolutePath, err)
	}
	if fi.IsDir() {
		return "", errors.New("path is a directory")
	}
	return absolutePath, nil
}
