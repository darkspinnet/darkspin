package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/darkspinnet/darkspin/content/spore"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/modprofile"
	server "github.com/darkspinnet/darkspin/server/runtime"
	appwindow "github.com/darkspinnet/darkspin/window"
)

func (a *App) requireModIdle() error {
	if a.modState.isRunning {
		return errors.New("wait for the modded game and server to finish closing")
	}
	a.mu.Lock()
	isBusy := a.gameDone != nil || a.cancel != nil || a.isShuttingDown
	a.mu.Unlock()
	if isBusy {
		return errors.New("close the current game and wait for launcher operations before editing mod profiles")
	}
	isRunning, err := appwindow.IsRunning(gameProcessName)
	if err != nil {
		return fmt.Errorf("modProcess: %w", err)
	}
	if isRunning {
		return errors.New("close Darkspore before editing or launching a mod profile")
	}
	return nil
}

// LaunchModProfile is the only entry point that supplies a native mod manifest.
// Its server, writable assets, client configuration and saves are isolated.
func (a *App) LaunchModProfile(identity string) error {
	a.modMu.Lock()
	defer a.modMu.Unlock()
	if runtime.GOOS != "windows" {
		return errors.New("the experimental Spore importer currently requires the Windows native client")
	}
	err := validateNewProfileIdentity(identity)
	if err != nil {
		return fmt.Errorf("modIdentity: %w", err)
	}
	err = a.requireModIdle()
	if err != nil {
		return fmt.Errorf("modIdle: %w", err)
	}
	a.mu.Lock()
	isReady := a.status.IsGameReady && a.status.IsContentReady && a.status.IsPatchComplete && a.status.IsAvatarReady
	a.mu.Unlock()
	if !isReady {
		return errors.New("wait for game, patch and content preparation to finish")
	}
	paths, err := a.modPaths()
	if err != nil {
		return fmt.Errorf("modPaths: %w", err)
	}
	configPath, err := a.prepareModConfig(paths)
	if err != nil {
		return fmt.Errorf("modConfig: %w", err)
	}
	// Standard tutorial-skipping is performed by the same feature operation as
	// normal profiles, before binding images to the resulting starter roster.
	preparationServer, err := a.newModServer(paths, configPath)
	if err != nil {
		return fmt.Errorf("modPrepare: %w", err)
	}
	completedCount, completeErr := preparationServer.CompletePendingTutorialProfiles(a.modContext())
	serverAddress, addressErr := localServerAddress(preparationServer)
	preparationServer.Close()
	if completeErr != nil {
		return fmt.Errorf("modTutorial: %w", completeErr)
	}
	if completedCount > 0 {
		a.log(fmt.Sprintf("Prepared %d modded starter loadouts", completedCount))
	}
	if addressErr != nil {
		return fmt.Errorf("prepareAddress: %w", addressErr)
	}
	manifestPath, err := a.prepareModAppearance(paths, identity, serverAddress)
	if err != nil {
		return fmt.Errorf("modAppearance: %w", err)
	}
	gameServer, err := a.newModServer(paths, configPath)
	if err != nil {
		return fmt.Errorf("modServer: %w", err)
	}
	serverAddress, err = localServerAddress(gameServer)
	if err != nil {
		gameServer.Close()
		return fmt.Errorf("modAddress: %w", err)
	}
	serverCtx, serverCancel := context.WithCancel(a.modContext())
	serverDone := make(chan error, 1)
	go a.runModServer(serverCtx, gameServer, serverDone)
	a.mu.Lock()
	gameCtx, gameDone, gameCancel := a.beginGameLaunchLocked()
	a.status.State = "launching"
	a.status.Message = "Launching modded Crogenitor"
	a.updatePlayReadyLocked()
	a.emitStatusLocked()
	arguments := append([]string(nil), a.arguments...)
	a.mu.Unlock()
	arguments = append(arguments, "--mod-manifest", manifestPath)
	a.modState.isRunning = true
	req := gameLaunchRequest{
		account: identity, clientProfile: modClientProfile(identity),
		serverAddress: serverAddress, arguments: arguments, done: gameDone, cancel: gameCancel,
		reportSession: launcherReportSession{
			client: reportClient{Mode: "modded", IsModded: true, Profile: identity, ModNames: modReportNames()},
			server: gameServer,
		},
	}
	go a.runModLaunch(gameCtx, req, gameServer, serverCancel, serverDone)
	return nil
}

func (a *App) runModServer(ctx context.Context, gameServer *server.Server, done chan<- error) {
	err := gameServer.Run(ctx)
	done <- err
}

func (a *App) runModLaunch(ctx context.Context, req gameLaunchRequest, gameServer *server.Server, serverCancel context.CancelFunc, serverDone <-chan error) {
	defer a.finishModServer(gameServer, serverCancel)
	// Run establishes listeners asynchronously. Bounded auth retries also detect
	// bind failures without displaying a misleading ready Mod launch.
	var token string
	var err error
	for attempt := 0; attempt < 20; attempt++ {
		select {
		case serverErr := <-serverDone:
			if serverErr == nil {
				serverErr = errors.New("mod server stopped before authentication")
			}
			a.failModLaunch(req, fmt.Errorf("modServe: %w", serverErr))
			return
		case <-ctx.Done():
			a.failModLaunch(req, fmt.Errorf("modCanceled: %w", ctx.Err()))
			return
		default:
		}
		token, err = requestLaunchJWTContext(ctx, "http://"+req.serverAddress, req.account)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			a.failModLaunch(req, fmt.Errorf("modCanceled: %w", ctx.Err()))
			return
		case <-time.After(150 * time.Millisecond):
		}
	}
	if err != nil {
		a.failModLaunch(req, fmt.Errorf("modAuthorize: %w", err))
		return
	}
	req.token = token
	a.runGameLaunch(ctx, req)
}

func (a *App) finishModServer(gameServer *server.Server, cancel context.CancelFunc) {
	a.mu.Lock()
	if a.reportSession.server == gameServer {
		a.reportSession.server = nil
	}
	a.mu.Unlock()
	cancel()
	gameServer.Close()
	a.modMu.Lock()
	a.modState.isRunning = false
	a.modMu.Unlock()
}

func (a *App) failModLaunch(req gameLaunchRequest, err error) {
	req.cancel()
	a.finishGameLaunch(req.done)
	a.finishGameLaunchFailure(err)
}

func (a *App) newModServer(paths *spinnerPathSet, configPath string) (*server.Server, error) {
	gameServer, err := server.New(server.Options{
		GamePath: paths.gamePath, ConfigPath: configPath, RuntimePath: modRuntimePath(paths),
		UserDatabasePath: modDatabasePath(paths), ContentDatabasePath: paths.contentPath,
		StaticPath: paths.staticPath, TracePath: "server.jsonl", IsModded: true, ModNames: modReportNames(),
		Logger: log.New(&spinnerLogWriter{app: a, prefix: "Mod server"}, "", 0),
	})
	if err != nil {
		return nil, fmt.Errorf("serverCreate: %w", err)
	}
	return gameServer, nil
}

func (a *App) prepareModConfig(paths *spinnerPathSet) (string, error) {
	config, wasCreated, err := game.LoadConfig(paths.configPath)
	if err != nil {
		return "", fmt.Errorf("baseConfig: %w", err)
	}
	if wasCreated {
		a.log("Created launcher configuration before Mod server preparation")
	}
	// The dedicated loopback port is selected afresh; no normal config changes.
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("portSelect: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	err = listener.Close()
	if err != nil {
		return "", fmt.Errorf("portRelease: %w", err)
	}
	for key, setting := range map[game.ConfigKey]string{game.ConfigServerPort: strconv.Itoa(port), game.ConfigIsMultiplayerEnabled: "false", game.ConfigStorageDriver: "sqlite"} {
		err = config.Set(key, setting)
		if err != nil {
			return "", fmt.Errorf("configSet: %w", err)
		}
	}
	path := filepath.Join(modRuntimePath(paths), "darkspin.toml")
	err = os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		return "", fmt.Errorf("configMkdir: %w", err)
	}
	err = config.Save(path)
	if err != nil {
		return "", fmt.Errorf("configSave: %w", err)
	}
	return path, nil
}

func (a *App) prepareModAppearance(paths *spinnerPathSet, identity, serverAddress string) (string, error) {
	ctx := a.modContext()
	repository, err := a.openProfileRepositoryAt(ctx, modDatabasePath(paths))
	if err != nil {
		return "", fmt.Errorf("profileOpen: %w", err)
	}
	defer a.closeModRepository(repository)
	record, err := repository.LoadByLoginName(ctx, identity)
	if err != nil {
		return "", fmt.Errorf("profileLoad: %w", err)
	}
	bindings, err := modprofile.Bindings(record)
	if err != nil {
		return "", fmt.Errorf("bindingRead: %w", err)
	}
	settings, err := readLauncherSettings(paths.settingPath)
	if err != nil {
		return "", fmt.Errorf("sourceRead: %w", err)
	}
	isSceneAvailable := false
	if settings.Mod.SporePath != "" || len(bindings) != 0 {
		sourcePath, sourceErr := spore.Installation(settings.Mod.SporePath)
		if sourceErr != nil && len(bindings) != 0 {
			return "", fmt.Errorf("sourceValidate: %w", sourceErr)
		}
		if sourceErr != nil {
			a.log(fmt.Sprintf("Mod editor environment unavailable: %v", sourceErr))
		} else {
			settings.Mod.SporePath = sourcePath
			isSceneAvailable = true
		}
	}
	heroes, err := a.modHeroes(paths)
	if err != nil {
		return "", fmt.Errorf("heroRead: %w", err)
	}
	appearances := make([]modprofile.Appearance, 0, len(bindings))
	for _, binding := range bindings {
		cachePath, pathErr := modCreationPath(paths, binding.CreationID)
		if pathErr != nil {
			return "", fmt.Errorf("bindingPath: %w", pathErr)
		}
		thumb, readErr := os.ReadFile(cachePath + ".png")
		if readErr != nil {
			return "", fmt.Errorf("bindingThumb: %w", readErr)
		}
		large, readErr := os.ReadFile(cachePath + ".large.png")
		if readErr != nil {
			return "", fmt.Errorf("bindingLarge: %w", readErr)
		}
		heroName := ""
		category := ""
		for _, hero := range heroes {
			if hero.Noun == binding.HeroNoun {
				heroName = hero.Name
				category = hero.Category
				break
			}
		}
		if heroName == "" {
			return "", fmt.Errorf("bound character %d is unavailable", binding.HeroNoun)
		}
		if category != "Hero" {
			continue
		}
		for _, creature := range record.Creatures {
			if creature == nil || creature.TemplateName != heroName {
				continue
			}
			revision := modAppearanceRevision(append(append([]byte(nil), thumb...), large...), identity+binding.CreationID)
			appearance := modprofile.Appearance{HeroName: heroName, CreationID: binding.CreationID}
			appearance.SavedThumbPath, appearance.SavedLargePath, err = modSavedAppearance(
				modRuntimePath(paths), serverAddress, creature.ID, creature.Version, creature.ThumbImageURL, creature.LargeImageURL,
			)
			if err != nil {
				return "", fmt.Errorf("savedAppearance: %w", err)
			}
			for _, image := range []struct {
				kind    string
				payload []byte
			}{{"thumb", thumb}, {"large", large}} {
				name := fmt.Sprintf("%d_%d_%s.png", creature.ID, revision, image.kind)
				path := filepath.Join(modRuntimePath(paths), "creature_png", name)
				err = os.MkdirAll(filepath.Dir(path), 0o755)
				if err != nil {
					return "", fmt.Errorf("imageMkdir: %w", err)
				}
				err = replaceLauncherConfig(path, image.payload)
				if err != nil {
					return "", fmt.Errorf("imageWrite: %w", err)
				}
				if image.kind == "thumb" {
					appearance.ThumbPath = "http://" + serverAddress + "/creature_png/" + name
				} else {
					appearance.LargePath = "http://" + serverAddress + "/creature_png/" + name
				}
			}
			appearances = append(appearances, appearance)
		}
	}
	err = modprofile.ApplyAppearances(ctx, repository, identity, appearances)
	if err != nil {
		return "", fmt.Errorf("appearanceApply: %w", err)
	}
	// Approved Mod-only compatibility: bound imports supply their appearance
	// baseline. Source and parent-hero identities keep imported and edited saves
	// scoped to explicit bindings.
	manifest := "[mod]\nversion=1\nmodel_baseline=1\nmodel_material=1\nmodel_audio=1\nmodel_scale=1\nmodel_fit=1\nmodel_portrait=1\neditor_music=1\ncharacter_override=1\ncreature_editor=1\n"
	if isSceneAvailable {
		manifest += "editor_scene=1\n"
	}
	editorHeroCount := 0
	for _, hero := range heroes {
		if hero.Category != "Hero" || hero.Noun == 0 {
			continue
		}
		editorHeroCount++
		if editorHeroCount > 128 {
			return "", errors.New("a Mod launch supports at most 128 editor hero nouns")
		}
		manifest += fmt.Sprintf("editor_hero%d=%d\n", editorHeroCount, hero.Noun)
	}
	if len(bindings) > 128 {
		return "", errors.New("a Mod launch supports at most 128 creature bindings")
	}
	for index, binding := range bindings {
		creation, sourceErr := a.findModCreation(paths, binding.CreationID)
		if sourceErr != nil {
			return "", fmt.Errorf("baselineSource[%d]: %w", index, sourceErr)
		}
		if creation.Instance == 0 || creation.Instance > uint64(^uint32(0)) {
			return "", errors.New("a Mod creature requires a native 32-bit source identity")
		}
		manifest += fmt.Sprintf("source%d=%d\nhero%d=%d\n", index+1, creation.Instance, index+1, binding.HeroNoun)
		for _, character := range heroes {
			if character.Noun != binding.HeroNoun {
				continue
			}
			manifest += fmt.Sprintf("nominal%d=%.9g\nextent%d=%.9g\n", index+1, character.NominalScale, index+1, character.TargetExtent)
			if character.Category != "Hero" {
				pngPath, pathErr := modCreationPath(paths, binding.CreationID)
				if pathErr != nil {
					return "", fmt.Errorf("characterPNG: %w", pathErr)
				}
				manifest += fmt.Sprintf("is_npc%d=1\npng%d=%s.png\n", index+1, index+1, pngPath)
			}
			break
		}
	}
	if len(bindings) != 0 || isSceneAvailable {
		for index, name := range []string{"Spore_Game.package", "Spore_Graphics.package", "Spore_Content.package"} {
			path := filepath.Join(settings.Mod.SporePath, "Data", name)
			if strings.ContainsAny(path, "\r\n") {
				return "", errors.New("Spore path contains a line break")
			}
			manifest += fmt.Sprintf("package%d=%s\n", index+1, path)
		}
		for index, name := range []string{"Spore_Audio1.package", "Spore_Audio2.package"} {
			path := filepath.Join(settings.Mod.SporePath, "Data", name)
			if strings.ContainsAny(path, "\r\n") {
				return "", errors.New("Spore audio path contains a line break")
			}
			fi, statErr := os.Stat(path)
			if statErr != nil {
				return "", fmt.Errorf("audioStat[%d]: %w", index, statErr)
			}
			if !fi.Mode().IsRegular() || fi.Size() == 0 {
				return "", fmt.Errorf("Spore audio bank %s is empty or not a regular file", name)
			}
			manifest += fmt.Sprintf("package%d=%s\n", index+7, path)
		}
		if a.modState.userPath != "" {
			for index, name := range []string{"GraphicsCache.package", "Pollination.package", "EditorSaves.package"} {
				path := filepath.Join(a.modState.userPath, name)
				if strings.ContainsAny(path, "\r\n") {
					return "", errors.New("Spore user path contains a line break")
				}
				fi, statErr := os.Stat(path)
				if errors.Is(statErr, os.ErrNotExist) {
					continue
				}
				if statErr != nil {
					return "", fmt.Errorf("sourceCache: %w", statErr)
				}
				if fi.IsDir() {
					return "", errors.New("Spore cache package is a directory")
				}
				a.log(fmt.Sprintf("Mod package%d (direct read): %s", index+4, path))
				manifest += fmt.Sprintf("package%d=%s\n", index+4, path)
			}
		}
	}
	manifestPath := filepath.Join(modRuntimePath(paths), "cache", "profile", modClientProfile(identity)+".ini")
	err = os.MkdirAll(filepath.Dir(manifestPath), 0o755)
	if err != nil {
		return "", fmt.Errorf("manifestMkdir: %w", err)
	}
	units := utf16.Encode([]rune(manifest))
	manifestPayload := make([]byte, 2+len(units)*2)
	manifestPayload[0], manifestPayload[1] = 0xff, 0xfe
	for index, unit := range units {
		binary.LittleEndian.PutUint16(manifestPayload[2+index*2:], unit)
	}
	err = replaceLauncherConfig(manifestPath, manifestPayload)
	if err != nil {
		return "", fmt.Errorf("manifestWrite: %w", err)
	}
	return manifestPath, nil
}
