package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	contentcache "github.com/darkspinnet/darkspin/content"
	"github.com/darkspinnet/darkspin/server/sporenet"
	sporenetsqlite "github.com/darkspinnet/darkspin/server/sporenet/sqlite"
	appwindow "github.com/darkspinnet/darkspin/window"
)

const maximumProfileIdentityLength = 20

// Profile is one locally managed player identity.
type Profile struct {
	LoginName                   string `json:"loginName"`
	DisplayName                 string `json:"displayName"`
	AvatarID                    uint32 `json:"avatarId"`
	AvatarURL                   string `json:"avatarUrl"`
	CrogenitorLevel             uint32 `json:"crogenitorLevel"`
	CumulativeXP                uint32 `json:"cumulativeXp"`
	HighestCampaignUnlocked     uint32 `json:"highestCampaignUnlocked"`
	IsTutorialCompleted         bool   `json:"isTutorialCompleted"`
	IsTutorialCompletionPending bool   `json:"isTutorialCompletionPending"`
}

// ProfileAvatar is one locally cached retail account portrait.
type ProfileAvatar struct {
	ID  uint32 `json:"id"`
	URL string `json:"url"`
}

// InterruptedMission is the launcher-safe projection of a persisted zone.
type InterruptedMission struct {
	GameID      uint32 `json:"gameId"`
	Level       string `json:"level"`
	Label       string `json:"label"`
	Difficulty  uint32 `json:"difficulty"`
	IsAvailable bool   `json:"isAvailable"`
}

// GetInterruptedMission reports whether a profile can continue a deployment.
func (a *App) GetInterruptedMission(identity string) (InterruptedMission, error) {
	identity = strings.TrimSpace(identity)
	err := validateProfileIdentity(identity)
	if err != nil {
		return InterruptedMission{}, fmt.Errorf("identityValidate: %w", err)
	}
	a.mu.Lock()
	gameServer := a.serviceSet.gameServer
	ctx := a.lifecycleCtx
	a.mu.Unlock()
	if gameServer == nil {
		return InterruptedMission{}, errors.New("server is not online")
	}
	mission, isFound, err := gameServer.FindInterruptedMission(ctx, identity)
	if err != nil {
		return InterruptedMission{}, fmt.Errorf("missionFind: %w", err)
	}
	return InterruptedMission{
		GameID: mission.GameID, Level: mission.Level, Label: mission.Label,
		Difficulty: mission.Difficulty, IsAvailable: isFound,
	}, nil
}

// DiscardInterruptedMission removes the selected profile from its interrupted
// mission while retaining the shared checkpoint for other co-op members.
func (a *App) DiscardInterruptedMission(identity string) error {
	identity = strings.TrimSpace(identity)
	err := validateProfileIdentity(identity)
	if err != nil {
		return fmt.Errorf("identityValidate: %w", err)
	}
	a.mu.Lock()
	gameServer := a.serviceSet.gameServer
	ctx := a.lifecycleCtx
	a.mu.Unlock()
	if gameServer == nil {
		return errors.New("server is not online")
	}
	err = gameServer.DiscardInterruptedMission(ctx, identity)
	if err != nil {
		return fmt.Errorf("missionDiscard: %w", err)
	}
	return nil
}

// GetProfiles lists identities retained by DarkSpinner's user database.
func (a *App) GetProfiles() ([]Profile, error) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.TODO()
	}
	repository, err := a.openProfileRepository(ctx)
	if err != nil {
		return nil, fmt.Errorf("profileOpen: %w", err)
	}
	defer repository.Close()
	identities, err := repository.ListIdentities(ctx)
	if err != nil {
		return nil, fmt.Errorf("profileList: %w", err)
	}
	profiles := make([]Profile, 0, len(identities))
	for _, identity := range identities {
		avatarURL, avatarErr := a.profileAvatarURL(identity.AvatarID)
		if avatarErr != nil && !errors.Is(avatarErr, os.ErrNotExist) {
			return nil, fmt.Errorf("profileAvatar[%d]: %w", identity.AvatarID, avatarErr)
		}
		profiles = append(profiles, Profile{
			LoginName: identity.LoginName, DisplayName: identity.DisplayName,
			AvatarID: identity.AvatarID, AvatarURL: avatarURL,
			CrogenitorLevel: identity.Level, CumulativeXP: identity.XP,
			HighestCampaignUnlocked:     identity.ChainProgression + 1,
			IsTutorialCompleted:         identity.IsTutorialCompleted,
			IsTutorialCompletionPending: identity.IsTutorialCompletionPending,
		})
	}
	return profiles, nil
}

// GetProfileAvatars returns every retail portrait already available in cache.
func (a *App) GetProfileAvatars() ([]ProfileAvatar, error) {
	avatars := contentcache.SelectableProfileAvatars()
	options := make([]ProfileAvatar, 0, len(avatars))
	for _, avatar := range avatars {
		avatarURL, err := a.profileAvatarURL(avatar.ID)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("avatarRead[%d]: %w", avatar.ID, err)
		}
		options = append(options, ProfileAvatar{ID: avatar.ID, URL: avatarURL})
	}
	return options, nil
}

// CreateProfile creates a local player whose login and display names match.
func (a *App) CreateProfile(identity string, avatarID uint32, isTutorialSkipped bool) error {
	a.mu.Lock()
	isInstallationReady := a.installation.IsLocalReady
	isAvatarReady := a.status.IsAvatarReady
	a.mu.Unlock()
	if !isInstallationReady {
		return errors.New("install Darkspinner beside the game before creating a Crogenitor")
	}
	if !isAvatarReady {
		return errors.New("wait for Crogenitor photos to finish preparing")
	}
	err := validateNewProfileIdentity(identity)
	if err != nil {
		criticalErr := fmt.Errorf("identityValidate: %w", err)
		a.setCriticalFailure("Identity", criticalErr)
		return criticalErr
	}
	err = validateProfileAvatarID(avatarID)
	if err != nil {
		criticalErr := fmt.Errorf("avatarValidate: %w", err)
		a.setCriticalFailure("Identity", criticalErr)
		return criticalErr
	}
	err = a.ensureLocalProfileWithAvatar(identity, avatarID, isTutorialSkipped)
	if err != nil {
		criticalErr := fmt.Errorf("profileEnsure: %w", err)
		a.setCriticalFailure("Identity", criticalErr)
		return criticalErr
	}
	if isTutorialSkipped {
		a.completePendingTutorialProfile(identity)
	}
	a.mu.Lock()
	a.status.IdentityError = ""
	a.emitStatusLocked()
	a.mu.Unlock()
	return nil
}

func (a *App) completePendingTutorialProfile(identity string) {
	a.mu.Lock()
	gameServer := a.serviceSet.gameServer
	ctx := a.lifecycleCtx
	a.mu.Unlock()
	if gameServer == nil {
		return
	}
	err := gameServer.CompletePendingTutorialProfile(ctx, identity)
	if err != nil {
		a.log("Starter loadout remains queued for " + identity + ": " + err.Error())
	}
}

// DeleteProfile permanently removes one inactive local player account.
func (a *App) DeleteProfile(identity string) error {
	identity = strings.TrimSpace(identity)
	err := validateProfileIdentity(identity)
	if err != nil {
		return fmt.Errorf("identityValidate: %w", err)
	}
	isRunning, err := appwindow.IsRunning(gameProcessName)
	if err != nil {
		return fmt.Errorf("gameProcess: %w", err)
	}
	if isRunning {
		return errors.New("close the game before deleting this Crogenitor")
	}
	a.mu.Lock()
	gameServer := a.serviceSet.gameServer
	ctx := a.lifecycleCtx
	a.mu.Unlock()
	if gameServer == nil {
		return errors.New("server is not online")
	}
	err = gameServer.DeleteProfile(ctx, identity)
	if errors.Is(err, sporenet.ErrUserActive) {
		return errors.New("sign out of this Crogenitor before deleting it")
	}
	if err != nil {
		return fmt.Errorf("profileDelete: %w", err)
	}
	a.mu.Lock()
	if strings.EqualFold(a.status.Identity, identity) {
		a.status.Identity = ""
		a.status.IsAuthenticated = false
		a.status.IdentityError = ""
		a.token = ""
		a.updatePlayReadyLocked()
		a.emitStatusLocked()
	}
	a.mu.Unlock()
	return nil
}

func (a *App) ensureLocalProfile(identity string) error {
	return a.ensureLocalProfileWithAvatar(identity, 0, false)
}

func (a *App) ensureLocalProfileWithAvatar(
	identity string, avatarID uint32, isTutorialCompletionPending bool,
) error {
	err := validateProfileIdentity(identity)
	if err != nil {
		return fmt.Errorf("identityValidate: %w", err)
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.TODO()
	}
	repository, err := a.openProfileRepository(ctx)
	if err != nil {
		return fmt.Errorf("repositoryOpen: %w", err)
	}
	defer repository.Close()
	record, err := repository.LoadByLoginName(ctx, identity)
	if err == nil {
		if isTutorialCompletionPending && !record.IsTutorialCompletionPending {
			record.IsTutorialCompletionPending = true
			err = repository.Save(ctx, record)
			if err != nil {
				return fmt.Errorf("profileQueue: %w", err)
			}
		}
		return nil
	}
	if !errors.Is(err, sporenet.ErrUserNotFound) {
		return fmt.Errorf("profileLoad: %w", err)
	}
	user := sporenet.NewUser(identity, identity, "")
	user.Account.AvatarID = avatarID
	user.IsTutorialCompletionPending = isTutorialCompletionPending
	_, err = repository.Create(ctx, user.Record())
	if errors.Is(err, sporenet.ErrUserExists) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("profileCreate: %w", err)
	}
	return nil
}

func validateProfileAvatarID(avatarID uint32) error {
	if contentcache.IsSelectableProfileAvatar(avatarID) {
		return nil
	}
	return fmt.Errorf("unknown Crogenitor photo %d", avatarID)
}

func (a *App) profileAvatarURL(avatarID uint32) (string, error) {
	avatar, isFound := contentcache.ProfileAvatarByID(avatarID)
	if !isFound {
		return "", fmt.Errorf("unknown Crogenitor photo %d", avatarID)
	}
	basePath, err := executableDirectory()
	if err != nil {
		return "", fmt.Errorf("baseResolve: %w", err)
	}
	pathSet, err := resolveSpinnerPaths(basePath)
	if err != nil {
		return "", fmt.Errorf("pathResolve: %w", err)
	}
	contents, err := os.ReadFile(filepath.Join(pathSet.staticPath, filepath.FromSlash(avatar.Target)))
	if err != nil {
		return "", fmt.Errorf("avatarRead: %w", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(contents), nil
}

func validateProfileIdentity(identity string) error {
	if identity == "" {
		return errors.New("identity is required")
	}
	if strings.IndexFunc(identity, unicode.IsSpace) >= 0 {
		return errors.New("login and display names cannot contain spaces")
	}
	return nil
}

func validateNewProfileIdentity(identity string) error {
	err := validateProfileIdentity(identity)
	if err != nil {
		return fmt.Errorf("identityBase: %w", err)
	}
	if len(identity) > maximumProfileIdentityLength {
		return fmt.Errorf(
			"Crogenitor name cannot exceed %d characters",
			maximumProfileIdentityLength,
		)
	}
	for _, character := range identity {
		isLowercase := character >= 'a' && character <= 'z'
		isUppercase := character >= 'A' && character <= 'Z'
		isDigit := character >= '0' && character <= '9'
		if !isLowercase && !isUppercase && !isDigit {
			return errors.New("Crogenitor name can contain only letters and numbers")
		}
	}
	return nil
}

func (a *App) openProfileRepository(ctx context.Context) (*sporenetsqlite.Repository, error) {
	basePath, err := executableDirectory()
	if err != nil {
		return nil, fmt.Errorf("baseResolve: %w", err)
	}
	pathSet, err := resolveSpinnerPaths(basePath)
	if err != nil {
		return nil, fmt.Errorf("pathResolve: %w", err)
	}
	return a.openProfileRepositoryAt(ctx, pathSet.userPath)
}

func (a *App) openProfileRepositoryAt(
	ctx context.Context, databasePath string,
) (*sporenetsqlite.Repository, error) {
	err := os.MkdirAll(filepath.Dir(databasePath), 0o755)
	if err != nil {
		return nil, fmt.Errorf("repositoryPath: %w", err)
	}
	repository, err := sporenetsqlite.New(ctx, databasePath, sporenetsqlite.Options{})
	if err != nil {
		return nil, fmt.Errorf("repositoryCreate: %w", err)
	}
	return repository, nil
}

func isLoopbackAuthURL(address string) bool {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil {
		return false
	}
	hostname := parsed.Hostname()
	ip := net.ParseIP(hostname)
	return strings.EqualFold(hostname, "localhost") || ip != nil && ip.IsLoopback()
}
