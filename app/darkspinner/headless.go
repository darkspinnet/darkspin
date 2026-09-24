package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	server "github.com/darkspinnet/darkspin/server/runtime"
)

const launcherPath = "/launcher/"

type browserPresentation struct {
	cancel context.CancelFunc
}

func (browserPresentation) EmitStatus(context.Context, LauncherStatus) {}

func (browserPresentation) OpenURL(_ context.Context, address string) error {
	return openSystemBrowser(address)
}

func (e browserPresentation) Quit(context.Context) {
	e.cancel()
}

type browserCallRequest struct {
	Arguments []json.RawMessage `json:"arguments"`
}

type browserCallResponse struct {
	Result any    `json:"result"`
	Error  string `json:"error,omitempty"`
}

func runHeadless(app *App) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	app.isHeadless = true
	app.presentation = browserPresentation{cancel: cancel}
	handler, token, err := newLauncherHandler(app)
	if err != nil {
		return fmt.Errorf("handlerCreate: %w", err)
	}
	app.launcherHandler = handler
	address, bootstrapServer, bootstrapDone, err := startLauncherBootstrap(handler, token)
	if err != nil {
		return fmt.Errorf("bootstrapStart: %w", err)
	}
	var bootstrapStopOnce sync.Once
	var bootstrapStopErr error
	stopBootstrap := func() error {
		bootstrapStopOnce.Do(func() {
			closeErr := bootstrapServer.Close()
			serveErr := <-bootstrapDone
			if closeErr != nil {
				bootstrapStopErr = fmt.Errorf("bootstrapClose: %w", closeErr)
				return
			}
			if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
				bootstrapStopErr = fmt.Errorf("bootstrapServe: %w", serveErr)
			}
		})
		return bootstrapStopErr
	}
	app.beforeServerStart = stopBootstrap
	defer func() {
		_ = stopBootstrap()
	}()
	app.startup(ctx)
	defer app.shutdown(context.Background())
	fmt.Println("DarkSpinner browser launcher:", address)
	openErr := openSystemBrowser(address)
	if openErr != nil {
		fmt.Fprintln(os.Stderr, "DarkSpinner could not open the browser:", openErr)
	}
	<-ctx.Done()
	return nil
}

func startLauncherBootstrap(
	handler http.Handler, token string,
) (string, *http.Server, <-chan error, error) {
	basePath, err := executableDirectory()
	if err != nil {
		return "", nil, nil, fmt.Errorf("executableDirectory: %w", err)
	}
	pathSet, err := resolveSpinnerPaths(basePath)
	if err != nil {
		return "", nil, nil, fmt.Errorf("runtimePath: %w", err)
	}
	port := defaultRemoteServerPort
	_, err = os.Stat(pathSet.configPath)
	if err == nil {
		config, _, loadErr := game.LoadConfig(pathSet.configPath)
		if loadErr != nil {
			return "", nil, nil, fmt.Errorf("configLoad: %w", loadErr)
		}
		port, err = config.Uint16(game.ConfigServerPort)
		if err != nil {
			return "", nil, nil, fmt.Errorf("portLoad: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", nil, nil, fmt.Errorf("configStat: %w", err)
	}
	listenerAddress := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port))
	listener, err := net.Listen("tcp", listenerAddress)
	if err != nil {
		return "", nil, nil, fmt.Errorf("listenerOpen[%s]: %w", listenerAddress, err)
	}
	bootstrapServer := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	bootstrapDone := make(chan error, 1)
	go func() {
		bootstrapDone <- bootstrapServer.Serve(listener)
	}()
	address := "http://" + listenerAddress + launcherPath + "#token=" + token
	return address, bootstrapServer, bootstrapDone, nil
}

func (e *App) launcherHTTPRoutes() []server.HTTPRoute {
	if e.launcherHandler == nil {
		return nil
	}
	return []server.HTTPRoute{{
		Pattern: "/launcher(?:/.*)?",
		Methods: []string{http.MethodGet, http.MethodPost},
		Handler: e.launcherHandler,
	}}
}

func newLauncherHandler(app *App) (http.Handler, string, error) {
	tokenBytes := make([]byte, 32)
	_, err := rand.Read(tokenBytes)
	if err != nil {
		return nil, "", fmt.Errorf("tokenCreate: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)
	assetFileSystem, err := fs.Sub(frontendAssets, "frontend/dist")
	if err != nil {
		assetFileSystem, err = fs.Sub(frontendAssets, "frontend")
		if err != nil {
			return nil, "", fmt.Errorf("assetsOpen: %w", err)
		}
	}
	assets := http.FileServer(http.FS(assetFileSystem))
	methods := launcherMethods(app)
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		if request.URL.Path == "/launcher" {
			http.Redirect(writer, request, launcherPath, http.StatusTemporaryRedirect)
			return
		}
		if strings.HasPrefix(request.URL.Path, launcherPath+"api/call/") {
			handleLauncherCall(writer, request, token, methods)
			return
		}
		if request.Method != http.MethodGet {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		staticRequest := request.Clone(request.Context())
		staticURL := *request.URL
		staticURL.Path = "/" + strings.TrimPrefix(request.URL.Path, launcherPath)
		staticRequest.URL = &staticURL
		assets.ServeHTTP(writer, staticRequest)
	})
	return handler, token, nil
}

func launcherMethods(app *App) map[string]any {
	return map[string]any{
		"Authorize":                    app.Authorize,
		"Cancel":                       app.Cancel,
		"CancelPatch":                  app.CancelPatch,
		"Check":                        app.Check,
		"CloseDetachedGameInstances":   app.CloseDetachedGameInstances,
		"CloseRunningGame":             app.CloseRunningGame,
		"CloseRunningProfile":          app.CloseRunningProfile,
		"CreateProfile":                app.CreateProfile,
		"DeleteProfile":                app.DeleteProfile,
		"DeleteRemoteProfile":          app.DeleteRemoteProfile,
		"DiscardInterruptedMission":    app.DiscardInterruptedMission,
		"GetInstallationStatus":        app.GetInstallationStatus,
		"GetInterruptedMission":        app.GetInterruptedMission,
		"GetLauncherIntegrationStatus": app.GetLauncherIntegrationStatus,
		"GetProfileAvatars":            app.GetProfileAvatars,
		"GetProfiles":                  app.GetProfiles,
		"GetRemoteProfiles":            app.GetRemoteProfiles,
		"GetServerConfiguration":       app.GetServerConfiguration,
		"GetStatus":                    app.GetStatus,
		"HasDetachedGameInstances":     app.HasDetachedGameInstances,
		"IsProfileRunning":             app.IsProfileRunning,
		"LaunchRemoteProfile":          app.LaunchRemoteProfile,
		"LoginRemoteProfile":           app.LoginRemoteProfile,
		"OpenFirewallSettings":         app.OpenFirewallSettings,
		"OpenReportFolder":             app.OpenReportFolder,
		"OpenSteamDemoInstall":         app.OpenSteamDemoInstall,
		"Patch":                        app.Patch,
		"Play":                         app.Play,
		"RefreshInstallationStatus":    app.RefreshInstallationStatus,
		"RefreshRemoteProfiles":        app.RefreshRemoteProfiles,
		"RegisterRemoteProfile":        app.RegisterRemoteProfile,
		"RelocateToGameRoot":           app.RelocateToGameRoot,
		"RemoveLauncherIntegration":    app.RemoveLauncherIntegration,
		"RepairLauncherIntegration":    app.RepairLauncherIntegration,
		"RestartLauncher":              app.RestartLauncher,
		"ScanRemoteServers":            app.ScanRemoteServers,
		"SendReport":                   app.SendReport,
		"SetIdentity":                  app.SetIdentity,
		"SetServerConfiguration":       app.SetServerConfiguration,
		"SetServerPort":                app.SetServerPort,
		"SetSkipCinematic":             app.SetSkipCinematic,
		"SignOut":                      app.SignOut,
		"StartDetachedGameInstance":    app.StartDetachedGameInstance,
		"UninstallDarkspinner":         app.UninstallDarkspinner,
		"Quit": func() {
			app.beginShutdown()
			app.quit()
		},
	}
}

func handleLauncherCall(
	writer http.ResponseWriter, request *http.Request, token string, methods map[string]any,
) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	providedToken := request.Header.Get("X-Darkspinner-Token")
	if subtle.ConstantTimeCompare([]byte(providedToken), []byte(token)) != 1 {
		http.Error(writer, "unauthorized", http.StatusUnauthorized)
		return
	}
	methodName := strings.TrimPrefix(request.URL.Path, launcherPath+"api/call/")
	method, isKnown := methods[methodName]
	if !isKnown {
		http.NotFound(writer, request)
		return
	}
	req := browserCallRequest{}
	request.Body = http.MaxBytesReader(writer, request.Body, 1<<20)
	decoder := json.NewDecoder(request.Body)
	err := decoder.Decode(&req)
	if err != nil {
		writeLauncherResponse(writer, browserCallResponse{Error: "invalid request"})
		return
	}
	result, err := invokeLauncherMethod(method, req.Arguments)
	if err != nil {
		writeLauncherResponse(writer, browserCallResponse{Error: err.Error()})
		return
	}
	writeLauncherResponse(writer, browserCallResponse{Result: result})
}

func invokeLauncherMethod(method any, arguments []json.RawMessage) (result any, err error) {
	defer func() {
		failure := recover()
		if failure != nil {
			result = nil
			err = fmt.Errorf("launcher method failed: %v", failure)
		}
	}()
	methodValue := reflect.ValueOf(method)
	methodType := methodValue.Type()
	if len(arguments) != methodType.NumIn() {
		return nil, fmt.Errorf("expected %d arguments", methodType.NumIn())
	}
	inputs := make([]reflect.Value, len(arguments))
	for index, argument := range arguments {
		input := reflect.New(methodType.In(index))
		err := json.Unmarshal(argument, input.Interface())
		if err != nil {
			return nil, fmt.Errorf("argumentDecode[%d]: %w", index, err)
		}
		inputs[index] = input.Elem()
	}
	outputs := methodValue.Call(inputs)
	if len(outputs) > 0 && methodType.Out(len(outputs)-1).Implements(reflect.TypeFor[error]()) {
		errOutput := outputs[len(outputs)-1]
		outputs = outputs[:len(outputs)-1]
		if !errOutput.IsNil() {
			return nil, errOutput.Interface().(error)
		}
	}
	if len(outputs) == 0 {
		return nil, nil
	}
	if len(outputs) == 1 {
		return outputs[0].Interface(), nil
	}
	return nil, errors.New("launcher method returned an unsupported result")
}

func writeLauncherResponse(writer http.ResponseWriter, response browserCallResponse) {
	writer.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(writer)
	err := encoder.Encode(response)
	if err != nil {
		fmt.Fprintln(os.Stderr, "DarkSpinner browser response failed:", err)
	}
}

func openSystemBrowser(address string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", address)
	case "darwin":
		command = exec.Command("open", address)
	default:
		command = exec.Command("xdg-open", address)
	}
	err := command.Start()
	if err != nil {
		return fmt.Errorf("browserStart: %w", err)
	}
	err = command.Process.Release()
	if err != nil {
		return fmt.Errorf("browserRelease: %w", err)
	}
	return nil
}
