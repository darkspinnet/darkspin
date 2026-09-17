//go:build windows

package main

import (
	"bytes"
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
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v2"
	wailsoptions "github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"golang.org/x/sys/windows"
)

// Version is replaced by Mage release builds and remains useful in direct Go builds.
var Version = "0.5.0"

// authServiceURL is replaced at link time for distributed launchers. It has no
// runtime override so one darkspin.exe always targets the service it was built for.
var authServiceURL = "http://127.0.0.1:8090"

// patchManifestURL is replaced at link time when a server distributes game
// updates. An empty value leaves patch checking disabled.
var patchManifestURL string

const (
	launchJWTEnvironment     = "DARKSPIN_LAUNCH_JWT"
	skipIntroEnvironment     = "DARKSPIN_SKIP_INTRO"
	skipCinematicEnvironment = "DARKSPIN_SKIP_CINEMATIC"
	maximumJWTLength         = 16 * 1024
)

var (
	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procVirtualAllocEx     = kernel32.NewProc("VirtualAllocEx")
	procVirtualFreeEx      = kernel32.NewProc("VirtualFreeEx")
	procCreateRemoteThread = kernel32.NewProc("CreateRemoteThread")
	procGetExitCodeThread  = kernel32.NewProc("GetExitCodeThread")
	procLoadLibraryW       = kernel32.NewProc("LoadLibraryW")
	launchEnvironmentMu    sync.Mutex
)

type gameLaunchEnvironment struct {
	jwt                string
	isIntroSkipped     bool
	isCinematicSkipped bool
}

type options struct {
	gameExecutable      string
	fangDLL             string
	isVersionRequested  bool
	clientTrace         string
	isIntroSkipped      bool
	isCinematicSkipped  bool
	jwt                 string
	jwtFile             string
	account             string
	isAutoPlayRequested bool
}

func main() {
	if hasVersionArgument(os.Args[1:]) {
		fmt.Println(Version)
		return
	}
	app := NewApp(os.Args[1:])
	err := wails.Run(&wailsoptions.App{
		Title:            "Dark Spin",
		Width:            960,
		Height:           620,
		MinWidth:         760,
		MinHeight:        520,
		Frameless:        false,
		BackgroundColour: &wailsoptions.RGBA{R: 8, G: 15, B: 22, A: 1},
		AssetServer:      &assetserver.Options{Assets: frontendAssets},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
	})
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "DarkSpin failed:", err)
		os.Exit(1)
	}
}

func hasVersionArgument(arguments []string) bool {
	for _, argument := range arguments {
		if argument == "--version" || argument == "-version" {
			return true
		}
	}
	return false
}

func run(arguments []string) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("launcherPath: %w", err)
	}
	baseDirectory := filepath.Dir(executable)
	configuration := options{}
	flags := flag.NewFlagSet("darkspin", flag.ContinueOnError)
	flags.StringVar(&configuration.gameExecutable, "game-exe", defaultGameExecutable(baseDirectory), "path to the game executable")
	flags.StringVar(&configuration.fangDLL, "fang", "", "path to fang.dll (default: beside darkspin.exe)")
	flags.BoolVar(&configuration.isVersionRequested, "version", false, "print launch version")
	flags.StringVar(&configuration.clientTrace, "client-trace", "", "write redacted client socket events as JSONL")
	flags.StringVar(&configuration.clientTrace, "trace", "", "write redacted client socket events as JSONL (alias for --client-trace)")
	flags.BoolVar(&configuration.isIntroSkipped, "skip-intro", false, "skip the EA and Maxis startup videos")
	flags.BoolVar(&configuration.isCinematicSkipped, "skip-cinematic", false, "skip startup videos and the post-login transition cinematic")
	flags.StringVar(&configuration.jwt, "jwt", "", "short-lived login JWT (development use; prefer a .dsjwt file or game URI)")
	flags.StringVar(&configuration.jwtFile, "jwt-file", "", "read a short-lived login JWT from a file")
	flags.StringVar(&configuration.account, "account", "", "account identity to authorize through the desktop auth service")
	flags.BoolVar(&configuration.isAutoPlayRequested, "auto-play", false, "authorize, patch, and launch when the patcher opens")
	err = flags.Parse(arguments)
	if err != nil {
		return fmt.Errorf("argsParse: %w", err)
	}
	if configuration.isVersionRequested {
		fmt.Println(Version)
		return nil
	}
	jwt, gameArguments, err := resolveLaunchJWT(configuration.jwt, configuration.jwtFile, flags.Args())
	if err != nil {
		return fmt.Errorf("jwtResolve: %w", err)
	}
	if configuration.account != "" && jwt != "" {
		return errors.New("--account cannot be combined with a JWT credential")
	}
	if configuration.account != "" {
		jwt, err = requestLaunchJWT(authServiceURL, configuration.account)
		if err != nil {
			return fmt.Errorf("accountAuthorize: %w", err)
		}
	}
	gameArguments = ensureGameArgument(gameArguments, "-multipleInstances")
	gamePath, gameWorkingDirectory, err := resolveGameExecutable(configuration.gameExecutable, baseDirectory)
	if err != nil {
		return fmt.Errorf("gameResolve: %w", err)
	}
	err = validateGameData(gameWorkingDirectory)
	if err != nil {
		return fmt.Errorf("gameData: %w", err)
	}
	fangPath, err := resolveFang(configuration.fangDLL, baseDirectory)
	if err != nil {
		return fmt.Errorf("fangResolve: %w", err)
	}
	err = configureClientTrace(configuration.clientTrace, baseDirectory)
	if err != nil {
		return fmt.Errorf("clientTrace: %w", err)
	}
	launchEnvironment := gameLaunchEnvironment{
		jwt: jwt, isIntroSkipped: configuration.isIntroSkipped,
		isCinematicSkipped: configuration.isCinematicSkipped,
	}
	err = launchInjected(
		gamePath, installDirectory(gameWorkingDirectory), fangPath, gameArguments, launchEnvironment,
	)
	if err != nil {
		return fmt.Errorf("gameLaunch: %w", err)
	}
	return nil
}

func requestLaunchJWT(authURL, account string) (string, error) {
	return requestLaunchJWTWithBrowser(authURL, account, nil)
}

func requestLaunchJWTWithBrowser(authURL, account string, openBrowser func(string)) (string, error) {
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
	err = postAuthJSON(client, baseURL+"/api/desktop/start", map[string]string{"account": account}, &startResponse)
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
	err = postAuthJSON(client, baseURL+"/api/desktop/exchange", map[string]string{"code": startResponse.Code}, &exchangeResponse)
	if err != nil {
		return "", fmt.Errorf("desktopExchange: %w", err)
	}
	token, err := normalizeJWT(exchangeResponse.LaunchToken)
	if err != nil {
		return "", fmt.Errorf("launchToken: %w", err)
	}
	return token, nil
}

func normalizeAuthURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("urlParse: %w", err)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Host == "" {
		return "", errors.New("authentication URL must be an origin without credentials, query, or fragment")
	}
	hostname := parsed.Hostname()
	ip := net.ParseIP(hostname)
	isLoopback := strings.EqualFold(hostname, "localhost") || ip != nil && ip.IsLoopback()
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopback) {
		return "", errors.New("authentication URL must use HTTPS unless it is loopback")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func postAuthJSON(client *http.Client, endpoint string, requestPayload, responsePayload any) error {
	contents, err := json.Marshal(requestPayload)
	if err != nil {
		return fmt.Errorf("requestMarshal: %w", err)
	}
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(contents))
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
		return "", errors.New("URI scheme is not game")
	}
	if parsed.Host != "" && !strings.EqualFold(parsed.Host, "login") {
		return "", fmt.Errorf("unsupported Game URI action %q", parsed.Host)
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

func normalizeJWT(value string) (string, error) {
	token := strings.TrimSpace(value)
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
	localPath := filepath.Join(baseDirectory, "DarksporeBin", "Darkspore.exe")
	info, err := os.Stat(localPath)
	if err == nil && !info.IsDir() {
		return localPath
	}
	legacyPath := filepath.Join(baseDirectory, "game", "DarksporeBin", "Darkspore.exe")
	info, err = os.Stat(legacyPath)
	if err == nil && !info.IsDir() {
		return legacyPath
	}
	environmentPath := os.Getenv("GAME_EXE")
	if environmentPath != "" {
		return environmentPath
	}
	return filepath.Join(baseDirectory, "Darkspore.exe")
}

func validateGameData(gameWorkingDirectory string) error {
	versionPath := filepath.Join(installDirectory(gameWorkingDirectory), "Data", "version_data.txt")
	info, err := os.Stat(versionPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("game data is missing at %s; select a complete installation with --game-exe or GAME_EXE", filepath.Dir(versionPath))
		}
		return fmt.Errorf("versionStat: %w", err)
	}
	if info.IsDir() {
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

func resolveGameExecutable(configuredPath, baseDirectory string) (string, string, error) {
	gamePath, err := existingAbsoluteFile(configuredPath)
	if err == nil {
		return gamePath, filepath.Dir(gamePath), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", "", fmt.Errorf("gameConfigured: %w", err)
	}

	defaultPath := filepath.Join(baseDirectory, "Darkspore.exe")
	configuredAbsolutePath, absoluteErr := filepath.Abs(configuredPath)
	if absoluteErr != nil {
		return "", "", fmt.Errorf("configuredPath: %w", absoluteErr)
	}
	defaultAbsolutePath, absoluteErr := filepath.Abs(defaultPath)
	if absoluteErr != nil {
		return "", "", fmt.Errorf("defaultPath: %w", absoluteErr)
	}
	if !strings.EqualFold(filepath.Clean(configuredAbsolutePath), filepath.Clean(defaultAbsolutePath)) {
		return "", "", fmt.Errorf("gameFallback: %w", err)
	}

	gameDirectory := filepath.Join(baseDirectory, "DarksporeBin")
	info, directoryErr := os.Stat(gameDirectory)
	if directoryErr != nil {
		return "", "", fmt.Errorf("binStat: %w", directoryErr)
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("%s is not a directory", gameDirectory)
	}

	gamePath, err = existingAbsoluteFile(filepath.Join(gameDirectory, "Darkspore.exe"))
	if err != nil {
		return "", "", fmt.Errorf("binGame: %w", err)
	}
	return gamePath, gameDirectory, nil
}

func configureClientTrace(path, baseDirectory string) error {
	if path == "" {
		err := os.Unsetenv("DARKSPIN_CLIENT_TRACE")
		if err != nil {
			return fmt.Errorf("traceUnset: %w", err)
		}
		return nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDirectory, path)
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

func launchInjected(
	gamePath, gameWorkingDirectory, fangPath string, gameArguments []string,
	launchEnvironment gameLaunchEnvironment,
) error {
	commandLine := windows.ComposeCommandLine(append([]string{gamePath}, gameArguments...))
	commandLinePointer, err := windows.UTF16PtrFromString(commandLine)
	if err != nil {
		return fmt.Errorf("commandLine: %w", err)
	}
	workingDirectory, err := windows.UTF16PtrFromString(gameWorkingDirectory)
	if err != nil {
		return fmt.Errorf("workingDir: %w", err)
	}
	startup := windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfo{}))}
	process := windows.ProcessInformation{}
	launchEnvironmentMu.Lock()
	err = configureLaunchJWT(launchEnvironment.jwt)
	if err != nil {
		launchEnvironmentMu.Unlock()
		return fmt.Errorf("jwtConfigure: %w", err)
	}
	err = configureCinematicOptions(
		launchEnvironment.isIntroSkipped, launchEnvironment.isCinematicSkipped,
	)
	if err != nil {
		_ = os.Unsetenv(launchJWTEnvironment)
		launchEnvironmentMu.Unlock()
		return fmt.Errorf("cinematicOption: %w", err)
	}
	err = windows.CreateProcess(nil, commandLinePointer, nil, nil, false, windows.CREATE_DEFAULT_ERROR_MODE|windows.CREATE_SUSPENDED, nil, workingDirectory, &startup, &process)
	jwtClearErr := os.Unsetenv(launchJWTEnvironment)
	launchEnvironmentMu.Unlock()
	if err != nil {
		return fmt.Errorf("processCreate: %w", err)
	}
	if jwtClearErr != nil {
		_ = windows.TerminateProcess(process.Process, 1)
		return fmt.Errorf("jwtParentClear: %w", jwtClearErr)
	}
	defer windows.CloseHandle(process.Process)
	defer windows.CloseHandle(process.Thread)

	moduleHandle, err := injectDLL(process.Process, fangPath)
	if err != nil {
		_ = windows.TerminateProcess(process.Process, 1)
		return fmt.Errorf("fangInject: %w", err)
	}
	err = callRemoteInitializer(process.Process, moduleHandle, fangPath)
	if err != nil {
		_ = windows.TerminateProcess(process.Process, 1)
		return fmt.Errorf("fangInit: %w", err)
	}
	_, err = windows.ResumeThread(process.Thread)
	if err != nil {
		_ = windows.TerminateProcess(process.Process, 1)
		return fmt.Errorf("gameResume: %w", err)
	}
	_, err = windows.WaitForSingleObject(process.Process, windows.INFINITE)
	if err != nil {
		return fmt.Errorf("gameWait: %w", err)
	}
	var exitCode uint32
	err = windows.GetExitCodeProcess(process.Process, &exitCode)
	if err != nil {
		return fmt.Errorf("exitCode: %w", err)
	}
	if exitCode != 0 {
		return gameExitError(exitCode)
	}
	return nil
}

func gameExitError(exitCode uint32) error {
	description, isKnown := gameExitDescription(exitCode)
	if !isKnown {
		description = "game exited without a known launcher diagnosis"
	}
	return fmt.Errorf("game exited with code %d (0x%08x): %s", int32(exitCode), exitCode, description)
}

func ensureGameArgument(arguments []string, requiredArgument string) []string {
	for _, argument := range arguments {
		if strings.EqualFold(argument, requiredArgument) {
			return arguments
		}
	}
	return append(arguments, requiredArgument)
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
	}[exitCode]
	return description, description != ""
}

func injectDLL(process windows.Handle, dllPath string) (uint32, error) {
	encodedPath, err := windows.UTF16FromString(dllPath)
	if err != nil {
		return 0, fmt.Errorf("dllPath: %w", err)
	}
	size := uintptr(len(encodedPath) * 2)
	remoteAddress, _, callErr := procVirtualAllocEx.Call(uintptr(process), 0, size, windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	if remoteAddress == 0 {
		return 0, fmt.Errorf("remoteAlloc: %w", callErr)
	}
	defer procVirtualFreeEx.Call(uintptr(process), remoteAddress, 0, windows.MEM_RELEASE)

	var written uintptr
	err = windows.WriteProcessMemory(process, remoteAddress, (*byte)(unsafe.Pointer(&encodedPath[0])), size, &written)
	if err != nil {
		return 0, fmt.Errorf("remoteWrite: %w", err)
	}
	if written != size {
		return 0, errors.New("hook path was only partially written into game")
	}
	loadLibraryAddress := procLoadLibraryW.Addr()
	var threadID uint32
	thread, _, callErr := procCreateRemoteThread.Call(uintptr(process), 0, 0, loadLibraryAddress, remoteAddress, 0, uintptr(unsafe.Pointer(&threadID)))
	if thread == 0 {
		return 0, fmt.Errorf("loaderStart: %w", callErr)
	}
	threadHandle := windows.Handle(thread)
	defer windows.CloseHandle(threadHandle)
	_, err = windows.WaitForSingleObject(threadHandle, windows.INFINITE)
	if err != nil {
		return 0, fmt.Errorf("loaderWait: %w", err)
	}
	var moduleHandle uint32
	result, _, callErr := procGetExitCodeThread.Call(thread, uintptr(unsafe.Pointer(&moduleHandle)))
	if result == 0 {
		return 0, fmt.Errorf("loaderResult: %w", callErr)
	}
	if moduleHandle == 0 {
		return 0, errors.New("fang.dll failed to load in the game process")
	}
	return moduleHandle, nil
}

func callRemoteInitializer(process windows.Handle, moduleHandle uint32, dllPath string) error {
	functionRVA, err := exportedFunctionRVA(dllPath, "RecapInitializeThread")
	if err != nil {
		functionRVA, err = exportedFunctionRVA(dllPath, "RecapInitializeThread@4")
	}
	if err != nil {
		return fmt.Errorf("initializerRVA: %w", err)
	}
	initializerAddress := uintptr(moduleHandle) + uintptr(functionRVA)
	var threadID uint32
	thread, _, callErr := procCreateRemoteThread.Call(uintptr(process), 0, 0, initializerAddress, 0, 0, uintptr(unsafe.Pointer(&threadID)))
	if thread == 0 {
		return fmt.Errorf("initializerStart: %w", callErr)
	}
	threadHandle := windows.Handle(thread)
	defer windows.CloseHandle(threadHandle)
	_, err = windows.WaitForSingleObject(threadHandle, windows.INFINITE)
	if err != nil {
		return fmt.Errorf("initializerWait: %w", err)
	}
	var exitCode uint32
	result, _, callErr := procGetExitCodeThread.Call(thread, uintptr(unsafe.Pointer(&exitCode)))
	if result == 0 {
		return fmt.Errorf("initializerResult: %w", callErr)
	}
	if exitCode != 0 {
		return fmt.Errorf("fang.dll hook initialization failed with code 0x%x", exitCode)
	}
	return nil
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
	file, err := pe.NewFile(bytes.NewReader(contents))
	if err != nil {
		return 0, fmt.Errorf("peParse[%q]: %w", path, err)
	}
	defer file.Close()
	optionalHeader, isValid := file.OptionalHeader.(*pe.OptionalHeader32)
	if !isValid {
		return 0, errors.New("fang.dll is not a 32-bit PE image")
	}
	exports := optionalHeader.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_EXPORT]
	if exports.VirtualAddress == 0 {
		return 0, errors.New("fang.dll has no export directory")
	}
	exportOffset, err := peFileOffset(file, optionalHeader, exports.VirtualAddress)
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
		nameRVA, readErr := peUint32(contents, file, optionalHeader, directory.AddressOfNames+index*4)
		if readErr != nil {
			return 0, fmt.Errorf("nameRVA[%d]: %w", index, readErr)
		}
		name, readErr := peString(contents, file, optionalHeader, nameRVA)
		if readErr != nil {
			return 0, fmt.Errorf("exportName[%d]: %w", index, readErr)
		}
		if name != functionName {
			continue
		}
		ordinal, readErr := peUint16(contents, file, optionalHeader, directory.AddressOfNameOrdinals+index*2)
		if readErr != nil {
			return 0, fmt.Errorf("ordinal[%d]: %w", index, readErr)
		}
		if uint32(ordinal) >= directory.NumberOfFunctions {
			return 0, errors.New("fang.dll export ordinal is out of range")
		}
		functionRVA, readErr := peUint32(contents, file, optionalHeader, directory.AddressOfFunctions+uint32(ordinal)*4)
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

func peFileOffset(file *pe.File, optionalHeader *pe.OptionalHeader32, rva uint32) (int, error) {
	if rva < optionalHeader.SizeOfHeaders {
		return int(rva), nil
	}
	for _, section := range file.Sections {
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

func peUint32(contents []byte, file *pe.File, optionalHeader *pe.OptionalHeader32, rva uint32) (uint32, error) {
	offset, err := peFileOffset(file, optionalHeader, rva)
	if err != nil {
		return 0, fmt.Errorf("u32Offset[%x]: %w", rva, err)
	}
	if offset < 0 || offset+4 > len(contents) {
		return 0, errors.New("PE uint32 lies outside the image")
	}
	return binary.LittleEndian.Uint32(contents[offset : offset+4]), nil
}

func peUint16(contents []byte, file *pe.File, optionalHeader *pe.OptionalHeader32, rva uint32) (uint16, error) {
	offset, err := peFileOffset(file, optionalHeader, rva)
	if err != nil {
		return 0, fmt.Errorf("u16Offset[%x]: %w", rva, err)
	}
	if offset < 0 || offset+2 > len(contents) {
		return 0, errors.New("PE uint16 lies outside the image")
	}
	return binary.LittleEndian.Uint16(contents[offset : offset+2]), nil
}

func peString(contents []byte, file *pe.File, optionalHeader *pe.OptionalHeader32, rva uint32) (string, error) {
	offset, err := peFileOffset(file, optionalHeader, rva)
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
	info, err := os.Stat(absolutePath)
	if err != nil {
		return "", fmt.Errorf("fileStat[%q]: %w", absolutePath, err)
	}
	if info.IsDir() {
		return "", errors.New("path is a directory")
	}
	return absolutePath, nil
}
