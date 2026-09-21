// Package runtime owns the server application lifecycle and composition.
package runtime

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	contentsqlite "github.com/darkspinnet/darkspin/content/sqlite"
	serverauth "github.com/darkspinnet/darkspin/server/auth"
	"github.com/darkspinnet/darkspin/server/blaze"
	"github.com/darkspinnet/darkspin/server/buildinfo"
	"github.com/darkspinnet/darkspin/server/chat"
	chatlocal "github.com/darkspinnet/darkspin/server/chat/local"
	"github.com/darkspinnet/darkspin/server/chat/textlog"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/game/appearancefs"
	gamecontentsqlite "github.com/darkspinnet/darkspin/server/game/contentsqlite"
	"github.com/darkspinnet/darkspin/server/gameplay"
	recaphttp "github.com/darkspinnet/darkspin/server/http"
	navigationcontentsqlite "github.com/darkspinnet/darkspin/server/navigation/contentsqlite"
	"github.com/darkspinnet/darkspin/server/party"
	partylocal "github.com/darkspinnet/darkspin/server/party/local"
	partyraknet "github.com/darkspinnet/darkspin/server/party/raknet103"
	"github.com/darkspinnet/darkspin/server/protocoltrace"
	"github.com/darkspinnet/darkspin/server/pvp"
	"github.com/darkspinnet/darkspin/server/readiness"
	"github.com/darkspinnet/darkspin/server/scheduler"
	"github.com/darkspinnet/darkspin/server/snapshot"
	"github.com/darkspinnet/darkspin/server/sporenet"
	sporenetsqlite "github.com/darkspinnet/darkspin/server/sporenet/sqlite"
	sharedtcp "github.com/darkspinnet/darkspin/server/tcp"
	sharedudp "github.com/darkspinnet/darkspin/server/udp"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
	zonecheckpoint "github.com/darkspinnet/darkspin/server/zone/checkpoint"
	checkpointsqlite "github.com/darkspinnet/darkspin/server/zone/checkpoint/sqlite"
	zonecontentsqlite "github.com/darkspinnet/darkspin/server/zone/content/contentsqlite"
)

const (
	DarkspinDirectory       = "darkspin"
	LogDirectory            = "logs"
	TraceDirectory          = "traces"
	SaveDirectory           = "saves"
	CacheDirectory          = "cache"
	UserDatabaseFilename    = "darkspin.db"
	ContentDatabaseFilename = "content.db"
	advertisedServerHost    = "127.0.0.1"
	multiplayerBindHost     = "0.0.0.0"
	sqliteOpenLimit         = 8
	sqliteIdleLimit         = 4
	sqliteBusyTimeout       = 5 * time.Second
)

// TutorialProfileCompletion reports the durable profile produced by the
// server-owned tutorial completion operation.
type TutorialProfileCompletion struct {
	LoginName     string `json:"login_name"`
	CumulativeXP  int32  `json:"cumulative_xp"`
	CreatureCount int    `json:"creature_count"`
	IsChanged     bool   `json:"is_changed"`
}

// CopyUserProfile copies one configured local profile through the same
// repository used by the server runtime.
func CopyUserProfile(
	ctx context.Context, configFilename string,
	sourceLoginName string, destinationLoginName string,
) (sporenet.UserProfileCopy, error) {
	if ctx == nil {
		return sporenet.UserProfileCopy{}, errors.New("nil context")
	}
	if configFilename == "" || sourceLoginName == "" || destinationLoginName == "" {
		return sporenet.UserProfileCopy{}, errors.New("invalid user copy input")
	}
	configPath, err := filepath.Abs(configFilename)
	if err != nil {
		return sporenet.UserProfileCopy{}, fmt.Errorf("configPath: %w", err)
	}
	config, _, err := game.LoadConfig(configPath)
	if err != nil {
		return sporenet.UserProfileCopy{}, fmt.Errorf("configLoad: %w", err)
	}
	runtimePath := filepath.Join(filepath.Dir(configPath), DarkspinDirectory)
	userRepository, userStore, err := openUserRepository(
		config, filepath.Join(runtimePath, SaveDirectory, UserDatabaseFilename),
	)
	if err != nil {
		return sporenet.UserProfileCopy{}, fmt.Errorf("userStore: %w", err)
	}
	result, copyErr := sporenet.CopyUserProfile(
		ctx, userRepository, sourceLoginName, destinationLoginName,
	)
	closeErr := userStore.Close()
	if copyErr != nil {
		return sporenet.UserProfileCopy{}, fmt.Errorf(
			"userCopy: %w", errors.Join(copyErr, closeErr),
		)
	}
	if closeErr != nil {
		return sporenet.UserProfileCopy{}, fmt.Errorf("userStoreClose: %w", closeErr)
	}
	return result, nil
}

// CompleteTutorialProfile applies the same transactional completion operation
// used by live gameplay to one configured local profile.
func CompleteTutorialProfile(
	ctx context.Context, configFilename string, loginName string,
) (TutorialProfileCompletion, error) {
	if ctx == nil {
		return TutorialProfileCompletion{}, errors.New("nil context")
	}
	if configFilename == "" || loginName == "" {
		return TutorialProfileCompletion{}, errors.New("invalid tutorial profile input")
	}
	configPath, err := filepath.Abs(configFilename)
	if err != nil {
		return TutorialProfileCompletion{}, fmt.Errorf("configPath: %w", err)
	}
	config, _, err := game.LoadConfig(configPath)
	if err != nil {
		return TutorialProfileCompletion{}, fmt.Errorf("configLoad: %w", err)
	}
	runtimePath := filepath.Join(filepath.Dir(configPath), DarkspinDirectory)
	contentStore, err := openContentStore(
		filepath.Join(runtimePath, CacheDirectory, ContentDatabaseFilename),
	)
	if err != nil {
		return TutorialProfileCompletion{}, fmt.Errorf("contentStore: %w", err)
	}
	defer contentStore.Close()
	templateDatabase := sporenet.NewTemplateDatabase()
	err = loadCreatureTemplates(contentStore, templateDatabase)
	if err != nil {
		return TutorialProfileCompletion{}, fmt.Errorf("creatureTemplate: %w", err)
	}
	userRepository, userStore, err := openUserRepository(
		config, filepath.Join(runtimePath, SaveDirectory, UserDatabaseFilename),
	)
	if err != nil {
		return TutorialProfileCompletion{}, fmt.Errorf("userStore: %w", err)
	}
	defer userStore.Close()
	userManager, err := sporenet.NewUserManagerWithRepository(
		userRepository, templateDatabase,
	)
	if err != nil {
		return TutorialProfileCompletion{}, fmt.Errorf("userManager: %w", err)
	}
	login := userManager.LoginTrusted(ctx, loginName)
	if !login.IsSuccess || login.User == nil {
		return TutorialProfileCompletion{}, errors.New("profile login failed")
	}
	completion, err := userManager.CompleteTutorial(
		ctx, login.User.Account.ID,
	)
	if err != nil {
		return TutorialProfileCompletion{}, fmt.Errorf("tutorialComplete: %w", err)
	}
	record := login.User.Record()
	err = userManager.Logout(ctx, login.User)
	if err != nil {
		return TutorialProfileCompletion{}, fmt.Errorf("profileLogout: %w", err)
	}
	return TutorialProfileCompletion{
		LoginName: record.LoginName, CumulativeXP: completion.CumulativeXP,
		CreatureCount: len(record.Creatures), IsChanged: completion.IsChanged,
	}, nil
}

// Options controls construction of a Server.
type Options struct {
	GamePath              string
	ConfigPath            string
	IsTimestampingEnabled bool
	Logger                *log.Logger
	BlazeCertificatePath  string
	BlazePrivateKeyPath   string
	TracePath             string
	RuntimePath           string
	workingDirectory      string
}

// Environment contains the normalized paths and detected game version used by
// server subsystems. More ported services can depend on this value without
// depending on Cobra or process-global state.
type Environment struct {
	WorkingDirectory string
	RuntimePath      string
	ConfigPath       string
	GamePath         string
	GameVersion      string
}

// Server coordinates the lifetime of all Game server subsystems.
type Server struct {
	environment           Environment
	config                *game.Config
	isConfigGenerated     bool
	scheduler             *scheduler.Scheduler
	blazeServers          []*blaze.Server
	sharedTCPServers      []*sharedtcp.SharedServer
	udpServer             *sharedudp.SharedServer
	gameplayCleanup       func()
	gameplayDiscard       func(uint32)
	gameplayDiscardMember func(uint32, uint64)
	bugContext            chat.BugContextProvider
	httpServers           []*recaphttp.Server
	httpRouter            *recaphttp.Router
	userManager           *sporenet.UserManager
	userStore             io.Closer
	contentStore          io.Closer
	checkpoint            *zonecheckpoint.Manager
	checkpointStore       io.Closer
	chatLogOutput         io.Closer
	roomManager           *sporenet.RoomManager
	gameManager           *game.Manager
	template              *sporenet.TemplateDatabase
	vendor                *sporenet.Vendor
	logger                *log.Logger
	traceRecorder         *protocoltrace.JSONRecorder
	snapshot              *snapshot.Service
	networkCloseOnce      sync.Once
	gameplayCloseOnce     sync.Once
	closeOnce             sync.Once
}

// InterruptedMission describes one durable zone that a local profile may
// continue after the previous client or server process ended.
type InterruptedMission struct {
	GameID     uint32
	Level      string
	Label      string
	Difficulty uint32
}

// BugContext freezes the selected local profile's current gameplay state for
// launcher-created diagnostic archives.
func (s *Server) BugContext(
	ctx context.Context, loginName string,
) (chat.BugContext, error) {
	if s == nil || s.userManager == nil || s.bugContext == nil {
		return chat.BugContext{}, nil
	}
	loginName = strings.TrimSpace(loginName)
	if loginName == "" {
		return chat.BugContext{}, nil
	}
	userID, err := s.userManager.FindUserID(ctx, loginName)
	if err != nil {
		return chat.BugContext{}, fmt.Errorf("bugUser: %w", err)
	}
	userName := loginName
	user := s.userManager.UserByID(userID)
	if user != nil {
		view := user.View()
		if strings.TrimSpace(view.DisplayName) != "" {
			userName = view.DisplayName
		}
	}
	bugContext, err := s.bugContext.BugContext(ctx, chat.BugContextRequest{
		Sender: chat.Participant{ID: userID, Name: userName},
	})
	if err != nil {
		return chat.BugContext{}, fmt.Errorf("bugGameplay: %w", err)
	}
	return bugContext, nil
}

// FindInterruptedMission resolves live membership or saved state without activating
// the selected profile.
func (s *Server) FindInterruptedMission(
	ctx context.Context, loginName string,
) (InterruptedMission, bool, error) {
	if s == nil || s.userManager == nil || s.checkpoint == nil {
		return InterruptedMission{}, false, nil
	}
	userID, err := s.userManager.FindUserID(ctx, loginName)
	if err != nil {
		return InterruptedMission{}, false, fmt.Errorf("missionUser: %w", err)
	}
	resume, isFound, err := s.gameManager.FindLiveResume(userID)
	if err != nil {
		return InterruptedMission{}, false, fmt.Errorf("missionLive: %w", err)
	}
	if !isFound {
		resume, isFound, err = s.checkpoint.FindResume(ctx, userID)
	}
	if err != nil {
		return InterruptedMission{}, false, fmt.Errorf("missionCheckpoint: %w", err)
	}
	if !isFound {
		return InterruptedMission{}, false, nil
	}
	label := "MISSION"
	if s.gameManager != nil {
		levelIndex, isIndexed := s.gameManager.ChainLevelIndex(resume.Level)
		if isIndexed && levelIndex > 0 {
			planet := (levelIndex-1)/4 + 1
			mission := (levelIndex-1)%4 + 1
			label = fmt.Sprintf("%d-%d", planet, mission)
		}
	}
	if strings.Contains(strings.ToLower(resume.Level), "tutorial") {
		label = "TUTORIAL"
	}
	return InterruptedMission{
		GameID: resume.GameID, Level: resume.Level, Label: label,
		Difficulty: resume.Difficulty,
	}, true, nil
}

// DiscardInterruptedMission removes one profile from a durable zone so its
// next launch starts fresh without discarding another co-op member's resume.
func (s *Server) DiscardInterruptedMission(
	ctx context.Context, loginName string,
) error {
	if s == nil || s.userManager == nil || s.checkpoint == nil {
		return nil
	}
	userID, err := s.userManager.FindUserID(ctx, loginName)
	if err != nil {
		return fmt.Errorf("missionUser: %w", err)
	}
	mission, isFound, err := s.FindInterruptedMission(ctx, loginName)
	if err != nil {
		return fmt.Errorf("missionFind: %w", err)
	}
	if !isFound {
		return nil
	}
	remainingMemberCount, err := s.checkpoint.RemoveMemberNow(
		ctx, uint64(mission.GameID), uint64(userID),
	)
	if err != nil {
		return fmt.Errorf("missionMember: %w", err)
	}
	// A pre-loading game can have members without a durable checkpoint.
	// Remove only this player before deciding whether the shared game is empty.
	if s.gameManager != nil {
		instance := s.gameManager.Game(mission.GameID)
		if instance != nil {
			instance.RemovePlayer(userID)
			remainingMemberCount = max(remainingMemberCount, len(instance.Players()))
		}
	}
	if remainingMemberCount == 0 {
		if s.gameManager != nil && s.gameManager.Game(mission.GameID) != nil {
			s.gameManager.Remove(mission.GameID)
			return nil
		}
		if s.gameplayDiscard != nil {
			s.gameplayDiscard(mission.GameID)
		}
		return nil
	}
	if s.gameplayDiscardMember != nil {
		s.gameplayDiscardMember(mission.GameID, uint64(userID))
	}
	if s.gameManager != nil {
		s.gameManager.ReserveGameID(mission.GameID)
		instance := s.gameManager.Game(mission.GameID)
		if instance != nil {
			instance.SetExpectedPlayerCount(uint16(remainingMemberCount))
		}
	}
	return nil
}

// DeleteProfile removes one inactive local account through the live user
// manager so shutdown cannot resurrect a deleted in-memory snapshot.
func (s *Server) DeleteProfile(ctx context.Context, loginName string) error {
	if s == nil || s.userManager == nil {
		return errors.New("user manager unavailable")
	}
	err := s.userManager.DeleteProfile(ctx, loginName)
	if err != nil {
		return fmt.Errorf("profileDelete: %w", err)
	}
	return nil
}

// CompletePendingTutorialProfile materializes one queued starter loadout
// through the live content-backed user manager.
func (s *Server) CompletePendingTutorialProfile(ctx context.Context, loginName string) error {
	if s == nil || s.userManager == nil {
		return errors.New("user manager unavailable")
	}
	login := s.userManager.LoginTrusted(ctx, loginName)
	if !login.IsSuccess || login.User == nil {
		return errors.New("profile login failed")
	}
	_, completeErr := s.userManager.CompleteTutorial(ctx, login.User.Account.ID)
	logoutErr := s.userManager.Logout(ctx, login.User)
	if completeErr != nil || logoutErr != nil {
		return fmt.Errorf("profileComplete: %w", errors.Join(completeErr, logoutErr))
	}
	return nil
}

// CompletePendingTutorialProfiles drains all durable starter-loadout work.
func (s *Server) CompletePendingTutorialProfiles(ctx context.Context) (int, error) {
	if s == nil || s.userManager == nil {
		return 0, errors.New("user manager unavailable")
	}
	completedProfileCount, err := s.userManager.CompletePendingTutorials(ctx)
	if err != nil {
		return completedProfileCount, fmt.Errorf("profileCompleteAll: %w", err)
	}
	return completedProfileCount, nil
}

// New initializes the process environment required by the server.
func New(options Options) (*Server, error) {
	workingDirectory := options.workingDirectory
	if workingDirectory == "" {
		var err error
		workingDirectory, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("cwdGet: %w", err)
		}
	}
	workingDirectory, err := filepath.Abs(workingDirectory)
	if err != nil {
		return nil, fmt.Errorf("cwdResolve: %w", err)
	}
	runtimePath := options.RuntimePath
	if runtimePath == "" {
		runtimePath = filepath.Join(workingDirectory, DarkspinDirectory)
	}
	runtimePath, err = filepath.Abs(runtimePath)
	if err != nil {
		return nil, fmt.Errorf("runtimePath: %w", err)
	}
	logPath := filepath.Join(runtimePath, LogDirectory)
	traceDirectoryPath := filepath.Join(logPath, TraceDirectory)
	userDatabasePath := filepath.Join(runtimePath, SaveDirectory, UserDatabaseFilename)
	contentDatabasePath := filepath.Join(runtimePath, CacheDirectory, ContentDatabaseFilename)
	for index, directory := range []string{logPath, traceDirectoryPath, filepath.Dir(userDatabasePath), filepath.Dir(contentDatabasePath)} {
		err = os.MkdirAll(directory, 0o755)
		if err != nil {
			return nil, fmt.Errorf("runtimeMkdir[%d]: %w", index, err)
		}
	}

	configPath := options.ConfigPath
	if configPath == "" {
		configPath = game.DefaultConfigFilename
	}
	configPath, err = filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("configPath: %w", err)
	}

	gamePath, err := detectGame(options.GamePath)
	if err != nil {
		return nil, fmt.Errorf("gameDetect: %w", err)
	}
	gameVersion := contentsqlite.SourceVersion
	config, isConfigGenerated, err := game.LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("configLoad: %w", err)
	}

	logger := options.Logger
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	ports, err := loadServicePorts(config)
	if err != nil {
		return nil, fmt.Errorf("portsLoad: %w", err)
	}
	tlsConfig, err := loadBlazeTLS(options)
	if err != nil {
		return nil, fmt.Errorf("tlsLoad: %w", err)
	}
	host := advertisedServerHost
	bindHost := advertisedServerHost
	if config.Bool(game.ConfigIsMultiplayerEnabled) {
		bindHost = multiplayerBindHost
	}
	contentStore, err := openContentStore(contentDatabasePath)
	if err != nil {
		return nil, fmt.Errorf("contentStore: %w", err)
	}
	isContentStoreTransferred := false
	defer func() {
		if !isContentStoreTransferred && contentStore != nil {
			_ = contentStore.Close()
		}
	}()
	templateDatabase := sporenet.NewTemplateDatabase()
	err = loadCreatureTemplates(contentStore, templateDatabase)
	if err != nil {
		return nil, fmt.Errorf("creatureTemplate: %w", err)
	}
	simulationProgram, err := zonecontentsqlite.Load(contentStore)
	if err != nil {
		return nil, fmt.Errorf("simulationProgram: %w", err)
	}
	userRepository, userStore, err := openUserRepository(config, userDatabasePath)
	if err != nil {
		return nil, fmt.Errorf("userStore: %w", err)
	}
	isUserStoreTransferred := false
	defer func() {
		if !isUserStoreTransferred && userStore != nil {
			_ = userStore.Close()
		}
	}()
	userManager, err := sporenet.NewUserManagerWithRepository(userRepository, templateDatabase)
	if err != nil {
		return nil, fmt.Errorf("userInit: %w", err)
	}
	snapshotMode, err := snapshot.ParseMode(config.String(game.ConfigSnapshotMode))
	if err != nil {
		return nil, fmt.Errorf("snapshotMode: %w", err)
	}
	snapshotBufferSecond, err := config.Uint32(game.ConfigSnapshotBufferSecond)
	if err != nil {
		return nil, fmt.Errorf("snapshotBuffer: %w", err)
	}
	snapshotDelaySecond, err := config.Uint32(game.ConfigSnapshotDelaySecond)
	if err != nil {
		return nil, fmt.Errorf("snapshotDelay: %w", err)
	}
	snapshotService, err := snapshot.New(snapshot.Options{
		Directory:        filepath.Join(traceDirectoryPath, "snapshots"),
		ArchiveDirectory: filepath.Join(runtimePath, "bugs"),
		TraceDirectory:   traceDirectoryPath, Mode: snapshotMode,
		ControlPath:    filepath.Join(traceDirectoryPath, "snapshot-control.txt"),
		BufferDuration: time.Duration(snapshotBufferSecond) * time.Second,
		Delay:          time.Duration(snapshotDelaySecond) * time.Second,
		BuildVersion:   buildinfo.Version, Logger: logger,
		ResolveUserName: func(userID int64) string {
			user := userManager.UserByID(userID)
			if user == nil {
				return ""
			}
			return user.DisplayName
		},
	})
	if err != nil {
		return nil, fmt.Errorf("snapshotInit: %w", err)
	}
	isSnapshotTransferred := false
	defer func() {
		if !isSnapshotTransferred {
			snapshotService.Close()
		}
	}()
	reconcileCtx, reconcileCancel := context.WithTimeout(context.Background(), 30*time.Second)
	completedProfileCount, err := userManager.CompletePendingTutorials(reconcileCtx)
	reconcileCancel()
	if err != nil {
		logger.Printf("Deferred tutorial setup remains queued: %v", err)
	}
	if completedProfileCount != 0 {
		logger.Printf(
			"Completed deferred tutorial setup for %d local profiles",
			completedProfileCount,
		)
	}
	checkpointCtx, checkpointCancel := context.WithTimeout(context.Background(), 10*time.Second)
	checkpointStore, err := checkpointsqlite.New(
		checkpointCtx, userDatabasePath,
		checkpointsqlite.Options{BusyTimeout: sqliteBusyTimeout},
	)
	checkpointCancel()
	if err != nil {
		return nil, fmt.Errorf("checkpointStore: %w", err)
	}
	checkpointManager := zonecheckpoint.NewManager(checkpointStore, logger)
	isCheckpointStoreTransferred := false
	defer func() {
		if isCheckpointStoreTransferred {
			return
		}
		_ = checkpointManager.Close()
		_ = checkpointStore.Close()
	}()
	worldPlayerLimit, err := config.Uint32(game.ConfigWorldPlayerLimit)
	if err != nil {
		return nil, fmt.Errorf("worldPlayerLimit: %w", err)
	}
	userManager.SetWorldPlayerLimit(worldPlayerLimit)
	var loginTokenVerifier blaze.LoginTokenVerifier
	var localAuthHandler http.Handler
	jwtSecret := config.String(game.ConfigAuthJWTSecret)
	if jwtSecret != "" {
		jwtIssuer := config.String(game.ConfigAuthJWTIssuer)
		jwtAudience := config.String(game.ConfigAuthJWTAudience)
		loginTokenVerifier, err = serverauth.NewJWTVerifier([]byte(jwtSecret), jwtIssuer, jwtAudience)
		if err != nil {
			return nil, fmt.Errorf("jwtInit: %w", err)
		}
		logger.Printf("JWT launch login enabled for issuer %q and audience %q", jwtIssuer, jwtAudience)
		issuer, issuerErr := serverauth.NewJWTIssuer(
			[]byte(jwtSecret), jwtIssuer, jwtAudience,
		)
		if issuerErr != nil {
			return nil, fmt.Errorf("accountAuthIssuer: %w", issuerErr)
		}
		authAddress := net.JoinHostPort(host, fmt.Sprintf("%d", ports.http))
		broker, brokerErr := serverauth.NewAccountBroker(
			issuer, authAddress, userManager, logger,
		)
		if brokerErr != nil {
			return nil, fmt.Errorf("accountAuthBroker: %w", brokerErr)
		}
		localAuthHandler = broker.Handler()
	}
	roomManager := sporenet.NewRoomManager()
	gameManager := game.NewManager()
	partyService := party.NewService(partylocal.NewMembership(userManager))
	relayAddress, err := netip.ParseAddrPort(
		net.JoinHostPort(host, fmt.Sprintf("%d", ports.partyRelay)),
	)
	if err != nil {
		return nil, fmt.Errorf("partyRelayAddress: %w", err)
	}
	partyRelay := partyraknet.NewRelay(relayAddress, logger)
	gameManager.SetChainLevelReferences(simulationProgram.ChainLevel)
	hostNetwork, err := gameHostNetwork(host, ports.blaze)
	if err != nil {
		return nil, fmt.Errorf("gameHostNetwork: %w", err)
	}
	gameManager.SetHostNetwork(hostNetwork)
	chatRecorders, chatLogOutput, err := openChatRecorders(config, logPath, userRepository, logger)
	if err != nil {
		return nil, fmt.Errorf("chatLogInit: %w", err)
	}
	isChatLogOutputTransferred := false
	defer func() {
		if !isChatLogOutputTransferred && chatLogOutput != nil {
			_ = chatLogOutput.Close()
		}
	}()
	directory := chatlocal.NewDirectory(
		userManager, roomManager, gameManager, partyService,
	)
	chatService, err := chat.NewService(directory, chatRecorders...)
	if err != nil {
		return nil, fmt.Errorf("chatInit: %w", err)
	}
	chatService.UseEffectPreviewer(chatlocal.NewEffectPreviewer(gameManager))
	chatService.UseResourceMutator(chatlocal.NewResourceMutator(gameManager))
	chatService.UseEventTriggerer(chatlocal.NewEventTriggerer(gameManager))
	chatService.UseFollowRequester(chatlocal.NewFollowRequester(gameManager))
	chatService.UseItemSummoner(chatlocal.NewItemSummoner(userManager, gameManager))
	chatService.UseLevelSetter(chatlocal.NewLevelSetter(userManager, gameManager))
	chatService.UseWarpRequester(chatlocal.NewWarpRequester(gameManager))
	chatService.UseNPCSpawner(chatlocal.NewNPCSpawner(gameManager))
	chatService.UseDNAGranter(chatlocal.NewDNAGranter(userManager, gameManager))
	chatService.UseBugReporter(chatlocal.NewBugReporter(runtimePath))
	chatService.UseSnapshotManager(snapshotService)
	vendorContext, vendorCancel := context.WithTimeout(context.Background(), 10*time.Second)
	vendor, err := gamecontentsqlite.LoadVendor(vendorContext, contentStore)
	vendorCancel()
	if err != nil {
		return nil, fmt.Errorf("vendorLoad: %w", err)
	}
	registry := blaze.NewRegistry()
	err = blaze.RegisterCoreComponents(registry, blaze.Endpoints{
		Host:          host,
		BlazePort:     ports.blaze,
		PSSPort:       ports.pss,
		TickPort:      ports.tick,
		TelemetryPort: ports.telemetry,
		HTTPQoSPort:   ports.httpQoS,
		GameVersion:   gameVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("blazeRegister: %w", err)
	}
	presenceRegistry := blaze.NewPresenceRegistry()
	matchmaking := pvp.NewMatchmaking()
	err = blaze.RegisterSporeNetComponentsWithMembership(
		registry, userManager, loginTokenVerifier, partyService, presenceRegistry,
	)
	if err != nil {
		return nil, fmt.Errorf("sporenetRegister: %w", err)
	}
	err = blaze.RegisterSocialComponents(
		registry, roomManager, chatService, userManager, partyService, partyRelay,
		matchmaking, presenceRegistry,
	)
	if err != nil {
		return nil, fmt.Errorf("socialRegister: %w", err)
	}
	err = blaze.RegisterGameManagerComponent(
		registry, gameManager, userManager, checkpointManager, matchmaking,
		partyService,
	)
	if err != nil {
		return nil, fmt.Errorf("gamesRegister: %w", err)
	}
	router := recaphttp.NewRouter()
	if localAuthHandler != nil {
		for _, route := range []string{
			"/api/desktop/register", "/api/desktop/start", "/api/desktop/exchange",
			"/api/desktop/delete",
		} {
			err = router.Add(route, []string{http.MethodPost}, func(
				writer http.ResponseWriter, request *http.Request, _ *recaphttp.URI,
			) {
				localAuthHandler.ServeHTTP(writer, request)
			})
			if err != nil {
				return nil, fmt.Errorf("localAuthRoute[%q]: %w", route, err)
			}
		}
	}
	webFileSystem := os.DirFS(filepath.Join(runtimePath, CacheDirectory, "www", "static"))
	partCatalogContext, partCatalogCancel := context.WithTimeout(context.Background(), 10*time.Second)
	partCatalog, err := gamecontentsqlite.LoadPartCatalog(partCatalogContext, contentStore)
	partCatalogCancel()
	if err != nil {
		return nil, fmt.Errorf("partCatalogLoad: %w", err)
	}
	userManager.SetPartScorer(partCatalog)
	resumeCoordinator, err := game.NewResumeCoordinator(gameManager, checkpointManager)
	if err != nil {
		return nil, fmt.Errorf("resumeCoordinator: %w", err)
	}
	err = game.RegisterAPI(router, game.APIOptions{
		UserManager: userManager, Template: templateDatabase, PartCatalog: partCatalog, Vendor: vendor,
		ResumeActivator: resumeCoordinator,
		Storage:         runtimePath, StaticFileSystem: webFileSystem,
		Host: host, HTTPPort: ports.http, QoSPort: ports.qos,
		Logger: logger,
	})
	if err != nil {
		return nil, fmt.Errorf("apiRegister: %w", err)
	}
	err = readiness.Register(router, buildinfo.Version, gameVersion)
	if err != nil {
		return nil, fmt.Errorf("readinessRegister: %w", err)
	}
	var recorder protocoltrace.Recorder
	var traceRecorder *protocoltrace.JSONRecorder
	tracePath := options.TracePath
	if tracePath != "" {
		if !filepath.IsAbs(tracePath) {
			tracePath = filepath.Join(traceDirectoryPath, tracePath)
		}
		traceRecorder, err = protocoltrace.Open(tracePath)
		if err != nil {
			return nil, fmt.Errorf("traceInit: %w", err)
		}
		recorder = traceRecorder
		logger.Printf("Writing protocol trace to %s", tracePath)
	}
	blazePorts := uniquePorts(ports.redirector, ports.blaze, ports.pss, ports.tick, ports.telemetry)
	httpPorts := uniquePorts(ports.http, ports.httpTelemetry, ports.httpQoS)
	sharedPorts := commonPorts(blazePorts, httpPorts)
	blazeServers := make([]*blaze.Server, 0, len(blazePorts))
	allBlazeServers := make([]*blaze.Server, 0, len(blazePorts))
	sharedTCPServers := make([]*sharedtcp.SharedServer, 0, len(sharedPorts))
	httpHandler := recaphttp.TraceHandler(router, recorder)
	for _, port := range blazePorts {
		blazeServer := blaze.NewServer(bindHost, port, registry, cloneTLS(tlsConfig), logger, recorder)
		allBlazeServers = append(allBlazeServers, blazeServer)
		if containsPort(sharedPorts, port) {
			httpServer := recaphttp.NewServer(bindHost, port, httpHandler)
			address := net.JoinHostPort(bindHost, fmt.Sprintf("%d", port))
			sharedTCPServers = append(
				sharedTCPServers,
				sharedtcp.NewSharedServer(address, blazeServer, httpServer),
			)
			continue
		}
		blazeServers = append(blazeServers, blazeServer)
	}
	httpServers := make([]*recaphttp.Server, 0, len(httpPorts))
	for _, port := range httpPorts {
		if containsPort(sharedPorts, port) {
			continue
		}
		httpServers = append(httpServers, recaphttp.NewServer(bindHost, port, httpHandler))
	}
	gameplayJoin, err := game.NewGameplayJoin(userManager, gameManager, partCatalog)
	if err != nil {
		return nil, fmt.Errorf("gameplayJoin: %w", err)
	}
	gameplayJoin.UseTutorialEndPublisher(tutorialEndPublisher{servers: allBlazeServers})
	gameplayJoin.UseInventoryPublisher(inventoryPublisher{servers: allBlazeServers})
	gameplayJoin.UseSystemChatPublisher(systemChatPublisher{servers: allBlazeServers})
	gameplayJoin.SetAppearanceStore(appearancefs.Store{Root: runtimePath})
	directorSource, err := gamecontentsqlite.NewDirectorSource(contentStore)
	if err != nil {
		return nil, fmt.Errorf("directorSource: %w", err)
	}
	campaignSetup, err := game.NewCampaignSetup(directorSource)
	if err != nil {
		return nil, fmt.Errorf("campaignSetup: %w", err)
	}
	navigationSource, err := navigationcontentsqlite.NewSource(contentStore)
	if err != nil {
		return nil, fmt.Errorf("navigationSource: %w", err)
	}
	taskScheduler := scheduler.New()
	gameplayHandler, gameplayLifecycle := gameplay.NewHandler(
		gameplayJoin, userManager, simulationProgram, logger, campaignSetup, navigationSource,
		zone.NewSchedulerTimer(taskScheduler), checkpointManager,
	)
	chatService.UseBugContextProvider(gameplayLifecycle)
	chatService.UseHintProvider(gameplayLifecycle)
	chatService.UseLocationProvider(gameplayLifecycle)
	snapshotService.UseStateProvider(gameplayLifecycle)
	snapshotService.UseNotifier(snapshotNotifier{servers: allBlazeServers})
	gameManager.UseRemovalObserver(gameplayLifecycle.DiscardGame)
	gameManager.UseMemberRemovalObserver(gameplayLifecycle.DiscardMember)
	gameManager.UseMemberResumePolicy(gameplayLifecycle)
	udpServer := sharedudp.NewSharedServer(
		net.JoinHostPort(bindHost, fmt.Sprintf("%d", ports.qos)), logger, gameplayHandler,
	)
	udpServer.SetPollHandler(gameplayLifecycle.Poll)
	udpServer.SetRakNetObserver(snapshotService)
	udpServer.SetPeerReplacedHandler(func(address *net.UDPAddr, generation uint64) {
		if address != nil {
			gameplayLifecycle.DisconnectTransport(address.String(), generation)
		}
	})
	udpServer.SetPartyHandlers(
		partyRelay.HandleApplication,
		partyRelay.HandleControl,
		func(address *net.UDPAddr, _ uint64) {
			partyRelay.DisconnectTransport(address)
		},
	)

	application := &Server{
		environment: Environment{
			WorkingDirectory: workingDirectory,
			RuntimePath:      runtimePath,
			ConfigPath:       configPath,
			GamePath:         gamePath,
			GameVersion:      gameVersion,
		},
		config:                config,
		isConfigGenerated:     isConfigGenerated,
		scheduler:             taskScheduler,
		blazeServers:          blazeServers,
		sharedTCPServers:      sharedTCPServers,
		udpServer:             udpServer,
		gameplayCleanup:       gameplayLifecycle.Cleanup,
		gameplayDiscard:       gameplayLifecycle.DiscardGame,
		gameplayDiscardMember: gameplayLifecycle.DiscardMember,
		bugContext:            gameplayLifecycle,
		httpServers:           httpServers,
		httpRouter:            router,
		userManager:           userManager,
		userStore:             userStore,
		contentStore:          contentStore,
		checkpoint:            checkpointManager,
		checkpointStore:       checkpointStore,
		chatLogOutput:         chatLogOutput,
		roomManager:           roomManager,
		gameManager:           gameManager,
		template:              templateDatabase,
		vendor:                vendor,
		logger:                logger,
		traceRecorder:         traceRecorder,
		snapshot:              snapshotService,
	}
	isUserStoreTransferred = true
	isContentStoreTransferred = true
	isCheckpointStoreTransferred = true
	isChatLogOutputTransferred = true
	isSnapshotTransferred = true
	return application, nil
}

func openUserRepository(config *game.Config, databasePath string) (sporenet.UserRepository, io.Closer, error) {
	driver := strings.ToLower(strings.TrimSpace(config.String(game.ConfigStorageDriver)))
	switch driver {
	case "", "sqlite":
		openCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		repository, err := sporenetsqlite.New(openCtx, databasePath, sporenetsqlite.Options{
			MaxOpenConnections: sqliteOpenLimit,
			MaxIdleConnections: sqliteIdleLimit,
			BusyTimeout:        sqliteBusyTimeout,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("sqliteOpen: %w", err)
		}
		return repository, repository, nil
	default:
		return nil, nil, fmt.Errorf("storageDriver: unsupported %q", driver)
	}
}

func openContentStore(databasePath string) (*contentsqlite.Store, error) {
	openCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := contentsqlite.New(openCtx, databasePath)
	if err != nil {
		return nil, fmt.Errorf("contentOpen: %w", err)
	}
	return store, nil
}

func loadCreatureTemplates(store *contentsqlite.Store, database *sporenet.TemplateDatabase) error {
	loadCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	records, err := store.CreatureTemplates(loadCtx)
	if err != nil {
		return fmt.Errorf("creatureRead: %w", err)
	}
	if len(records) == 0 {
		return errors.New("creatureRead: no creature templates")
	}
	templates := make([]*sporenet.TemplateCreature, 0, len(records))
	for _, record := range records {
		abilityPassive := runtimeAbilityID(record.AbilityPassiveAsset, record.AbilityPassive)
		abilityBasic := runtimeAbilityID(record.AbilityBasicAsset, record.AbilityBasic)
		abilityRandom := runtimeAbilityID(record.AbilityRandomAsset, record.AbilityRandom)
		abilitySpecial1 := runtimeAbilityID(record.AbilitySpecial1Asset, record.AbilitySpecial1)
		abilitySpecial2 := runtimeAbilityID(record.AbilitySpecial2Asset, record.AbilitySpecial2)
		abilityLocale := make(map[uint32]sporenet.AbilityLocale, len(record.AbilityLocale))
		for slot, locale := range record.AbilityLocale {
			abilityID := map[string]uint32{
				"passive": abilityPassive, "basic": abilityBasic, "random": abilityRandom,
				"special_1": abilitySpecial1, "special_2": abilitySpecial2,
			}[slot]
			if abilityID == 0 {
				continue
			}
			abilityLocale[abilityID] = sporenet.AbilityLocale{
				LocalizationTableID: locale.LocalizationTableID,
				NameLocaleID:        locale.NameLocaleKey,
				DescriptionLocaleID: locale.DescriptionLocaleKey,
			}
		}
		abilityProperty := make(map[uint32][]sporenet.AbilityProperty, len(record.AbilityProperty))
		for slot, properties := range record.AbilityProperty {
			abilityID := map[string]uint32{
				"passive": abilityPassive, "basic": abilityBasic, "random": abilityRandom,
				"special_1": abilitySpecial1, "special_2": abilitySpecial2,
			}[slot]
			if abilityID == 0 {
				continue
			}
			for _, property := range properties {
				abilityProperty[abilityID] = append(abilityProperty[abilityID], sporenet.AbilityProperty{
					Name: property.Name, SourceTableName: property.SourceTableName,
					SourceName: property.SourceName, Minimum: property.Minimum,
					Maximum: property.Maximum, AuthoredMinimum: property.AuthoredMinimum,
					AuthoredMaximum: property.AuthoredMaximum,
					Coefficient:     property.Coefficient, Evidence: property.Evidence,
				})
			}
		}
		templates = append(templates, &sporenet.TemplateCreature{
			Noun:                 record.ID,
			LocalizationTableID:  record.LocalizationTableID,
			NameLocaleID:         record.NameLocaleKey,
			DescriptionLocaleID:  record.DescriptionLocaleKey,
			Name:                 record.Name,
			ElementType:          record.ElementType,
			WeaponMinDamage:      record.WeaponMinDamage,
			WeaponMaxDamage:      record.WeaponMaxDamage,
			GearScore:            record.GearScore,
			ClassType:            record.ClassType,
			StatsTemplate:        record.StatTemplate,
			AbilityPassive:       abilityPassive,
			AbilityBasic:         abilityBasic,
			AbilityRandom:        abilityRandom,
			AbilitySpecial1:      abilitySpecial1,
			AbilitySpecial2:      abilitySpecial2,
			AbilityPassiveAsset:  record.AbilityPassiveAsset,
			AbilityBasicAsset:    record.AbilityBasicAsset,
			AbilityRandomAsset:   record.AbilityRandomAsset,
			AbilitySpecial1Asset: record.AbilitySpecial1Asset,
			AbilitySpecial2Asset: record.AbilitySpecial2Asset,
			AbilityLocale:        abilityLocale,
			AbilityProperty:      abilityProperty,
			AreHandsPresent:      record.IsHandPresent,
			AreFeetPresent:       record.IsFootPresent,
		})
	}
	err = database.Replace(templates)
	if err != nil {
		return fmt.Errorf("creatureReplace: %w", err)
	}
	return nil
}

func runtimeAbilityID(assetName string, fallback uint32) uint32 {
	if assetName == "" || assetName == "0" {
		return fallback
	}
	return util.HashID(assetName)
}

type bestEffortChatRecorder struct {
	recorder chat.Recorder
	logger   *log.Logger
	label    string
}

func (r bestEffortChatRecorder) Record(ctx context.Context, message chat.Message) error {
	err := r.recorder.Record(ctx, message)
	if err != nil {
		r.logger.Printf("Chat %s log failed: %v", r.label, err)
	}
	return nil
}

func openChatRecorders(config *game.Config, logPath string, repository sporenet.UserRepository, logger *log.Logger) ([]chat.Recorder, io.Closer, error) {
	recorders := make([]chat.Recorder, 0, 3)
	databaseRecorder, isSupported := repository.(chat.Recorder)
	if isSupported {
		recorders = append(recorders, databaseRecorder)
	}
	if config.Bool(game.ConfigIsChatStdoutEnabled) {
		stdoutRecorder, err := textlog.New(os.Stdout)
		if err != nil {
			return nil, nil, fmt.Errorf("stdoutCreate: %w", err)
		}
		recorders = append(recorders, bestEffortChatRecorder{recorder: stdoutRecorder, logger: logger, label: "stdout"})
	}
	filePath := strings.TrimSpace(config.String(game.ConfigChatFilePath))
	if filePath == "" {
		return recorders, nil, nil
	}
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(logPath, filepath.FromSlash(filePath))
	}
	err := os.MkdirAll(filepath.Dir(filePath), 0o755)
	if err != nil {
		return nil, nil, fmt.Errorf("fileDirectory: %w", err)
	}
	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("fileOpen: %w", err)
	}
	fileRecorder, err := textlog.New(file)
	if err != nil {
		_ = file.Close()
		return nil, nil, fmt.Errorf("fileCreate: %w", err)
	}
	recorders = append(recorders, bestEffortChatRecorder{recorder: fileRecorder, logger: logger, label: "file"})
	return recorders, file, nil
}

// Environment returns a copy of the initialized runtime environment.
func (s *Server) Environment() Environment {
	return s.environment
}

// Config returns the loaded game configuration.
func (s *Server) Config() *game.Config {
	return s.config
}

// Scheduler returns the delayed-task scheduler shared by server subsystems.
func (s *Server) Scheduler() *scheduler.Scheduler {
	return s.scheduler
}

// Run starts the application lifecycle and blocks until cancellation. Protocol
// services will be attached here as each recap_server subsystem is ported.
func (s *Server) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("run server: nil context")
	}
	if ctx.Err() != nil {
		s.Close()
		return nil
	}

	s.logger.Printf("Starting Dark Spin server v%s", buildinfo.Version)
	s.logger.Printf("Game version: %s", s.environment.GameVersion)
	if s.environment.GamePath != "" {
		s.logger.Printf("Game install path: %s", s.environment.GamePath)
	}
	if s.isConfigGenerated {
		s.logger.Printf("Generated default config: %s", s.environment.ConfigPath)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	serviceCount := len(s.blazeServers) + len(s.sharedTCPServers) + len(s.httpServers) + 1
	results := make(chan error, serviceCount)
	for _, service := range s.blazeServers {
		go func(current *blaze.Server) {
			results <- current.ListenAndServe(runCtx)
		}(service)
	}
	for _, service := range s.sharedTCPServers {
		go func(current *sharedtcp.SharedServer) {
			results <- current.ListenAndServe(runCtx)
		}(service)
	}
	go func() {
		results <- s.udpServer.ListenAndServe(runCtx)
	}()
	for _, service := range s.httpServers {
		go func(current *recaphttp.Server) {
			results <- current.ListenAndServe(runCtx)
		}(service)
	}

	var runErr error
	for completed := 0; completed < serviceCount; completed++ {
		err := <-results
		if err != nil && runErr == nil {
			runErr = err
			cancel()
			s.closeNetworkServices()
		}
	}
	s.Close()
	s.logger.Print("Stopping recap server")
	if runErr != nil {
		return fmt.Errorf("servicesRun: %w", runErr)
	}
	return nil
}

// Close releases initialized server subsystems. It is safe to call repeatedly.
func (s *Server) Close() {
	s.closeOnce.Do(func() {
		s.closeNetworkServices()
		if s.snapshot != nil {
			s.snapshot.Close()
		}
		if s.scheduler != nil {
			s.scheduler.Shutdown()
		}
		s.closeGameplayServices()
		if s.checkpoint != nil {
			err := s.checkpoint.Close()
			if err != nil {
				s.logger.Printf("Zone checkpoint flush failed: %v", err)
			}
		}
		if s.checkpointStore != nil {
			err := s.checkpointStore.Close()
			if err != nil {
				s.logger.Printf("Zone checkpoint storage close failed: %v", err)
			}
		}
		flushCtx, flushCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer flushCancel()
		err := s.userManager.Flush(flushCtx)
		if err != nil {
			s.logger.Printf("Active user flush failed: %v", err)
		}
		if s.userStore != nil {
			err = s.userStore.Close()
			if err != nil {
				s.logger.Printf("User storage close failed: %v", err)
			}
		}
		if s.contentStore != nil {
			err = s.contentStore.Close()
			if err != nil {
				s.logger.Printf("Content storage close failed: %v", err)
			}
		}
		if s.chatLogOutput != nil {
			err = s.chatLogOutput.Close()
			if err != nil {
				s.logger.Printf("Chat log close failed: %v", err)
			}
		}
		if s.traceRecorder != nil {
			err := s.traceRecorder.Close()
			if err != nil {
				s.logger.Printf("Protocol trace close failed: %v", err)
			}
		}
	})
}

func uniquePorts(ports ...uint16) []uint16 {
	seen := make(map[uint16]struct{}, len(ports))
	unique := make([]uint16, 0, len(ports))
	for _, port := range ports {
		if _, isFound := seen[port]; isFound {
			continue
		}
		seen[port] = struct{}{}
		unique = append(unique, port)
	}
	return unique
}

func commonPorts(first, second []uint16) []uint16 {
	common := make([]uint16, 0)
	for _, port := range first {
		if containsPort(second, port) {
			common = append(common, port)
		}
	}
	return common
}

func containsPort(ports []uint16, expected uint16) bool {
	for _, port := range ports {
		if port == expected {
			return true
		}
	}
	return false
}

type servicePorts struct {
	redirector    uint16
	blaze         uint16
	pss           uint16
	tick          uint16
	telemetry     uint16
	qos           uint16
	partyRelay    uint16
	http          uint16
	httpTelemetry uint16
	httpQoS       uint16
}

func gameHostNetwork(host string, port uint16) (game.NetworkPair, error) {
	ip := net.ParseIP(host).To4()
	if ip == nil {
		return game.NetworkPair{}, fmt.Errorf("host %q is not an IPv4 address", host)
	}
	address := uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
	endpoint := game.NetworkEndpoint{IP: address, Port: port}
	return game.NetworkPair{Internal: endpoint, External: endpoint}, nil
}

func loadServicePorts(config *game.Config) (servicePorts, error) {
	port, err := config.Uint16(game.ConfigServerPort)
	if err != nil {
		return servicePorts{}, fmt.Errorf("portLoad: %w", err)
	}
	if port == 0 || port == ^uint16(0) {
		return servicePorts{}, errors.New("server port must be between 1 and 65534")
	}
	partyRelayPort := port + 1
	return servicePorts{
		redirector: port, blaze: port, pss: port, tick: port,
		telemetry: port, qos: port, partyRelay: partyRelayPort, http: port,
		httpTelemetry: port, httpQoS: port,
	}, nil
}

func loadBlazeTLS(options Options) (*tls.Config, error) {
	if options.BlazeCertificatePath == "" && options.BlazePrivateKeyPath == "" {
		return nil, nil
	}
	if options.BlazeCertificatePath == "" || options.BlazePrivateKeyPath == "" {
		return nil, errors.New("both --blaze-cert and --blaze-key are required")
	}
	config, err := blaze.LoadTLSConfig(options.BlazeCertificatePath, options.BlazePrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("keypairLoad: %w", err)
	}
	return config, nil
}

func cloneTLS(config *tls.Config) *tls.Config {
	if config == nil {
		return nil
	}
	return config.Clone()
}

func (s *Server) closeNetworkServices() {
	s.networkCloseOnce.Do(func() {
		for _, service := range s.blazeServers {
			_ = service.Close()
		}
		for _, service := range s.sharedTCPServers {
			service.Close()
		}
		if s.udpServer != nil {
			_ = s.udpServer.Close()
		}
		for _, service := range s.httpServers {
			_ = service.Close()
		}
	})
}

func (s *Server) closeGameplayServices() {
	s.gameplayCloseOnce.Do(func() {
		if s.gameplayCleanup != nil {
			s.gameplayCleanup()
		}
	})
}

func detectGame(path string) (string, error) {
	if path == "" {
		return "", nil
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("gamePath: %w", err)
	}
	return filepath.Clean(absolutePath), nil
}
