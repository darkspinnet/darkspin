package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	creationreader "github.com/darkspinnet/darkspin/content/creation"
	contentsqlite "github.com/darkspinnet/darkspin/content/sqlite"
	"github.com/darkspinnet/darkspin/server/modprofile"
	sporenetsqlite "github.com/darkspinnet/darkspin/server/sporenet/sqlite"
)

type ModCreature struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	IsPersonal bool   `json:"isPersonal"`
}

type modState struct {
	userPath  string
	creations []creationreader.Creation
	isRunning bool
}

type ModHero struct {
	Noun         uint32  `json:"noun"`
	Name         string  `json:"name"`
	PortraitURL  string  `json:"portraitUrl"`
	Category     string  `json:"category"`
	NounName     string  `json:"nounName"`
	NominalScale float64 `json:"nominalScale"`
	TargetExtent float64 `json:"targetExtent"`
}

type ModBinding struct {
	HeroNoun   uint32 `json:"heroNoun"`
	CreationID string `json:"creationId"`
}

type ModProfile struct {
	LoginName                   string       `json:"loginName"`
	DisplayName                 string       `json:"displayName"`
	CreateDT                    string       `json:"createDt"`
	LastConnectionDT            string       `json:"lastConnectionDt"`
	CrogenitorLevel             uint32       `json:"crogenitorLevel"`
	CumulativeXP                uint32       `json:"cumulativeXp"`
	HighestCampaignUnlocked     uint32       `json:"highestCampaignUnlocked"`
	IsTutorialCompleted         bool         `json:"isTutorialCompleted"`
	IsTutorialCompletionPending bool         `json:"isTutorialCompletionPending"`
	AvatarID                    uint32       `json:"avatarId"`
	AvatarURL                   string       `json:"avatarUrl"`
	Bindings                    []ModBinding `json:"bindings"`
}

type ModConfiguration struct {
	SporePath   string        `json:"sporePath"`
	Creatures   []ModCreature `json:"creatures"`
	Heroes      []ModHero     `json:"heroes"`
	Profiles    []ModProfile  `json:"profiles"`
	SourceError string        `json:"sourceError"`
}

type ModProfileRequest struct {
	LoginName         string       `json:"loginName"`
	AvatarID          uint32       `json:"avatarId"`
	IsTutorialSkipped bool         `json:"isTutorialSkipped"`
	IsNew             bool         `json:"isNew"`
	Bindings          []ModBinding `json:"bindings"`
}

func modRuntimePath(paths *spinnerPathSet) string { return filepath.Join(paths.runtimePath, "mod") }
func modDatabasePath(paths *spinnerPathSet) string {
	return filepath.Join(modRuntimePath(paths), "saves", "mod.db")
}

func (a *App) modPaths() (*spinnerPathSet, error) {
	base, err := executableDirectory()
	if err != nil {
		return nil, fmt.Errorf("modBase: %w", err)
	}
	paths, err := resolveSpinnerPaths(base)
	if err != nil {
		return nil, fmt.Errorf("modPaths: %w", err)
	}
	return paths, nil
}

func (a *App) modContext() context.Context {
	a.mu.Lock()
	ctx := a.lifecycleCtx
	a.mu.Unlock()
	if ctx == nil {
		return context.TODO()
	}
	return ctx
}

func detectSporeInstallation() string {
	libraries := discoverSteamRoots()
	for _, root := range append([]string(nil), libraries...) {
		payload, err := os.ReadFile(filepath.Join(root, "steamapps", "libraryfolders.vdf"))
		if err != nil {
			continue
		}
		libraries = append(libraries, parseSteamLibraryRoots(payload)...)
	}
	for _, library := range libraries {
		root, err := creationreader.Installation(filepath.Join(library, "steamapps", "common", "Spore"))
		if err == nil {
			return root
		}
	}
	return ""
}

// GetModConfiguration performs the first automatic detection once. Invalid
// sources remain visible for correction without silently replacing user choices.
func (a *App) GetModConfiguration() (ModConfiguration, error) {
	a.modMu.Lock()
	defer a.modMu.Unlock()
	paths, err := a.modPaths()
	if err != nil {
		return ModConfiguration{}, fmt.Errorf("modResolve: %w", err)
	}
	settings, err := readLauncherSettings(paths.configPath)
	if err != nil {
		return ModConfiguration{}, fmt.Errorf("modSettings: %w", err)
	}
	if !settings.Mod.IsDetectionAttempted && settings.Mod.SporePath == "" {
		settings.Mod.SporePath = detectSporeInstallation()
		settings.Mod.IsDetectionAttempted = true
		err = writeLauncherSettings(paths.configPath, settings)
		if err != nil {
			return ModConfiguration{}, fmt.Errorf("detectSave: %w", err)
		}
	}
	configuration := ModConfiguration{SporePath: settings.Mod.SporePath, Creatures: []ModCreature{}, Heroes: []ModHero{}, Profiles: []ModProfile{}}
	if configuration.SporePath != "" {
		creations, scanErr := a.scanModCreations(configuration.SporePath)
		if scanErr != nil {
			configuration.SourceError = scanErr.Error()
		}
		for _, creation := range creations {
			configuration.Creatures = append(configuration.Creatures, ModCreature{ID: creation.ID, Name: creation.Name, IsPersonal: creation.IsPersonal})
		}
	}
	configuration.Heroes, err = a.modHeroes(paths)
	if err != nil {
		return configuration, fmt.Errorf("heroList: %w", err)
	}
	configuration.Profiles, err = a.modProfiles(paths)
	if err != nil {
		return configuration, fmt.Errorf("modProfiles: %w", err)
	}
	return configuration, nil
}

func (a *App) SetModSporePath(path string) error {
	a.modMu.Lock()
	defer a.modMu.Unlock()
	root, err := creationreader.Installation(path)
	if err != nil {
		return fmt.Errorf("sporeValidate: %w", err)
	}
	paths, err := a.modPaths()
	if err != nil {
		return fmt.Errorf("modResolve: %w", err)
	}
	settings, err := readLauncherSettings(paths.configPath)
	if err != nil {
		return fmt.Errorf("modSettings: %w", err)
	}
	settings.Mod.SporePath = root
	a.modState.creations = nil
	settings.Mod.IsDetectionAttempted = true
	err = writeLauncherSettings(paths.configPath, settings)
	if err != nil {
		return fmt.Errorf("sourceSave: %w", err)
	}
	return nil
}

func (a *App) DetectModSporePath() (string, error) {
	root := detectSporeInstallation()
	if root == "" {
		return "", errors.New("Spore was not found in the detected Steam libraries; select SporeApp.exe manually")
	}
	err := a.SetModSporePath(root)
	if err != nil {
		return "", fmt.Errorf("detectSet: %w", err)
	}
	return root, nil
}

func (a *App) scanModCreations(root string) ([]creationreader.Creation, error) {
	validated, err := creationreader.Installation(root)
	if err != nil {
		return nil, fmt.Errorf("sporeSource: %w", err)
	}
	creations, err := creationreader.Scan(a.modContext(), validated, a.modState.userPath)
	if err != nil {
		return nil, fmt.Errorf("sporeScan: %w", err)
	}
	a.modState.creations = creations
	return creations, nil
}

func (a *App) modProfiles(paths *spinnerPathSet) ([]ModProfile, error) {
	ctx := a.modContext()
	repository, err := a.openProfileRepositoryAt(ctx, modDatabasePath(paths))
	if err != nil {
		return nil, fmt.Errorf("modOpen: %w", err)
	}
	defer a.closeModRepository(repository)
	identities, err := repository.ListIdentities(ctx)
	if err != nil {
		return nil, fmt.Errorf("modList: %w", err)
	}
	profiles := make([]ModProfile, 0, len(identities))
	for _, identity := range identities {
		record, loadErr := repository.LoadByLoginName(ctx, identity.LoginName)
		if loadErr != nil {
			return nil, fmt.Errorf("modLoad: %w", loadErr)
		}
		bindings, bindingErr := modprofile.Bindings(record)
		if bindingErr != nil {
			return nil, fmt.Errorf("modBindings: %w", bindingErr)
		}
		avatarURL, avatarErr := a.profileAvatarURL(identity.AvatarID)
		if avatarErr != nil && !errors.Is(avatarErr, os.ErrNotExist) {
			return nil, fmt.Errorf("modAvatar: %w", avatarErr)
		}
		lastConnectionDT := ""
		if !identity.LastConnectionDT.IsZero() {
			lastConnectionDT = identity.LastConnectionDT.UTC().Format(time.RFC3339Nano)
		}
		profile := ModProfile{
			LoginName: identity.LoginName, DisplayName: identity.DisplayName,
			CreateDT:         identity.CreateDT.UTC().Format(time.RFC3339Nano),
			LastConnectionDT: lastConnectionDT, CrogenitorLevel: identity.Level,
			CumulativeXP: identity.XP, HighestCampaignUnlocked: identity.ChainProgression + 1,
			IsTutorialCompleted:         identity.IsTutorialCompleted,
			IsTutorialCompletionPending: identity.IsTutorialCompletionPending,
			AvatarID:                    identity.AvatarID, AvatarURL: avatarURL, Bindings: []ModBinding{},
		}
		for _, binding := range bindings {
			profile.Bindings = append(profile.Bindings, ModBinding{HeroNoun: binding.HeroNoun, CreationID: binding.CreationID})
		}
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

// SaveModProfile uses the ordinary account defaults in a separate repository.
// Creation bytes are cached before accepting a binding, preserving the PNG's
// native payload rather than re-encoding it as an ordinary thumbnail.
func (a *App) SaveModProfile(req ModProfileRequest) error {
	a.modMu.Lock()
	defer a.modMu.Unlock()
	err := validateNewProfileIdentity(req.LoginName)
	if err != nil {
		return fmt.Errorf("modIdentity: %w", err)
	}
	if req.IsNew {
		err = validateProfileAvatarID(req.AvatarID)
		if err != nil {
			return fmt.Errorf("modAvatar: %w", err)
		}
	}
	err = a.requireModIdle()
	if err != nil {
		return fmt.Errorf("modIdle: %w", err)
	}
	paths, err := a.modPaths()
	if err != nil {
		return fmt.Errorf("modResolve: %w", err)
	}
	heroes, err := a.modHeroes(paths)
	if err != nil {
		return fmt.Errorf("heroList: %w", err)
	}
	bindings := make([]modprofile.Binding, 0, len(req.Bindings))
	for _, binding := range req.Bindings {
		isKnown := false
		for _, hero := range heroes {
			if hero.Noun == binding.HeroNoun {
				isKnown = true
				break
			}
		}
		if !isKnown {
			return fmt.Errorf("unknown character %d", binding.HeroNoun)
		}
		err = a.cacheModCreation(paths, binding.CreationID)
		if err != nil {
			return fmt.Errorf("bindingCache: %w", err)
		}
		bindings = append(bindings, modprofile.Binding{HeroNoun: binding.HeroNoun, CreationID: binding.CreationID})
	}
	repository, err := a.openProfileRepositoryAt(a.modContext(), modDatabasePath(paths))
	if err != nil {
		return fmt.Errorf("modOpen: %w", err)
	}
	defer a.closeModRepository(repository)
	err = modprofile.Save(a.modContext(), repository, modprofile.SaveRequest{LoginName: req.LoginName, AvatarID: req.AvatarID, IsTutorialSkipped: req.IsTutorialSkipped, IsNew: req.IsNew, Bindings: bindings})
	if err != nil {
		return fmt.Errorf("modSave: %w", err)
	}
	return nil
}

type ModDirectory struct {
	Path        string   `json:"path"`
	Parent      string   `json:"parent"`
	Directories []string `json:"directories"`
	Executables []string `json:"executables"`
}

func (a *App) closeModContent(store *contentsqlite.Store) {
	err := store.Close()
	if err != nil {
		a.log("Mod content close: " + err.Error())
	}
}

func (a *App) closeModRepository(repository *sporenetsqlite.Repository) {
	err := repository.Close()
	if err != nil {
		a.log("Mod profile close: " + err.Error())
	}
}

// BrowseModSporeDirectory backs the themed in-window executable picker.
func (a *App) BrowseModSporeDirectory(path string) (ModDirectory, error) {
	if strings.TrimSpace(path) == "" {
		path = filepath.VolumeName(os.TempDir()) + string(os.PathSeparator)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return ModDirectory{}, fmt.Errorf("browsePath: %w", err)
	}
	fi, statErr := os.Stat(absolute)
	if statErr == nil && !fi.IsDir() {
		absolute = filepath.Dir(absolute)
	}
	entries, err := os.ReadDir(absolute)
	if err != nil {
		return ModDirectory{}, fmt.Errorf("browseRead: %w", err)
	}
	directory := ModDirectory{Path: absolute, Parent: filepath.Dir(absolute), Directories: []string{}, Executables: []string{}}
	for _, entry := range entries {
		if entry.IsDir() {
			directory.Directories = append(directory.Directories, filepath.Join(absolute, entry.Name()))
			continue
		}
		if strings.EqualFold(entry.Name(), "SporeApp.exe") || strings.EqualFold(entry.Name(), "SporeBin.exe") {
			directory.Executables = append(directory.Executables, filepath.Join(absolute, entry.Name()))
		}
	}
	return directory, nil
}
