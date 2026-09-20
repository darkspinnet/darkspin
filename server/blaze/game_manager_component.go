package blaze

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/blaze/tdf"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/party"
	"github.com/darkspinnet/darkspin/server/pvp"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

const returnToShipReason = uint64(6)
const matchmakingResultCreatedGame = uint64(0)
const matchmakingResultJoinedNewGame = uint64(1)
const matchmakingResultFailed = uint64(3)
const matchmakingResultCancelled = uint64(4)
const gameSlotTypePublicParticipant = uint64(0)
const gamePlayerStateConnecting = uint64(2)
const gamePlayerStateConnected = uint64(4)
const gameManagerErrorPermissionDenied = uint16(0x001e)
const gameSetupContextCreate = uint64(0)
const gameSetupContextJoin = uint64(1)
const gameSetupContextIndirectJoin = uint64(2)
const gameSetupContextMatchmaking = uint64(3)
const gameSetupReasonDataless = uint8(0)
const gameSetupReasonIndirectJoin = uint8(2)
const gameSetupReasonMatchmaking = uint8(3)
const playgroupObjectType = uint64(1)

type tutorialCompleter interface {
	CompleteTutorial(context.Context, int64) (sporenet.TutorialCompletion, error)
}

type activeUserDirectory interface {
	UserByID(int64) *sporenet.User
}

type gameUserDirectory interface {
	tutorialCompleter
	activeUserDirectory
}

type partyDirectory interface {
	PartyForMember(int64) (party.Snapshot, bool)
}

// RegisterGameManagerComponent adds game creation and session lifecycle RPCs.
func RegisterGameManagerComponent(
	registry *Registry, gameManager *game.Manager, userDirectory gameUserDirectory,
	resumeSource game.ResumeSource, matchmaking *pvp.Matchmaking,
	partyDirectories ...partyDirectory,
) error {
	if gameManager == nil {
		return errors.New("register game manager component: nil game manager")
	}
	if userDirectory == nil {
		return errors.New("register game manager component: nil tutorial completer")
	}
	empty := func(context.Context, *Request) (*Response, error) { return &Response{}, nil }
	var partyDirectory partyDirectory
	if len(partyDirectories) != 0 {
		partyDirectory = partyDirectories[0]
	}
	if matchmaking == nil {
		return errors.New("register game manager component: nil matchmaking")
	}
	component := Component{ID: GameManagerComponentID, Name: "GameManager", Commands: map[uint16]Handler{
		0x01: createGameHandler(gameManager),
		0x02: destroyGameHandler(gameManager, resumeSource),
		0x03: advanceGameStateHandler(gameManager),
		0x04: empty,
		0x05: empty,
		0x07: empty,
		0x08: empty,
		0x09: joinGameHandler(gameManager),
		0x0b: removePlayerHandler(gameManager, userDirectory, resumeSource),
		0x0d: startMatchmakingHandler(
			matchmaking, gameManager, userDirectory, partyDirectory,
		),
		0x0e: cancelMatchmakingHandler(matchmaking),
		0x0f: finalizeGameHandler(gameManager),
		0x19: resetDedicatedServerHandler(gameManager, resumeSource, userDirectory, partyDirectory),
		0x1d: updateMeshHandler(gameManager),
	}}
	err := registry.Register(component)
	if err != nil {
		return fmt.Errorf("gameRegister: %w", err)
	}
	return nil
}

func createGameHandler(gameManager *game.Manager) Handler {
	return func(_ context.Context, request *Request) (*Response, error) {
		instance := gameManager.Create()
		applyGameRequest(instance, request.Fields)
		err := request.QueueNotification(GameManagerComponentID, 0x64, []tdf.Field{
			tdf.FieldNamed("GID", tdf.IntegerValue(uint64(instance.ID))),
			tdf.FieldNamed("GSTA", tdf.IntegerValue(uint64(game.StateInitializing))),
		})
		if err != nil {
			return nil, fmt.Errorf("createState: %w", err)
		}
		return &Response{Fields: gameIDFields(instance.ID)}, nil
	}
}

func resetDedicatedServerHandler(
	gameManager *game.Manager, resumeSource game.ResumeSource,
	userDirectory activeUserDirectory, partyDirectory partyDirectory,
) Handler {
	return func(ctx context.Context, request *Request) (*Response, error) {
		user := requestUser(request)
		if user == nil {
			return &Response{ErrorCode: authInvalidUser}, nil
		}
		hostNetwork, err := sessionGameHostNetwork(
			request.Session, gameManager.HostNetwork(),
		)
		if err != nil {
			return nil, fmt.Errorf("resetHostNetwork: %w", err)
		}
		interruptedTutorialID, isTutorialInterrupted :=
			gameManager.RemoveTutorialForPlayer(user)
		if isTutorialInterrupted {
			discardGameCheckpoint(resumeSource, interruptedTutorialID)
		}
		instance, isRestored, err := restoreGameCheckpoint(ctx, gameManager, resumeSource, user)
		if err != nil {
			return nil, fmt.Errorf("resetRestore: %w", err)
		}
		if !isRestored {
			instance = gameManager.Create()
			applyGameRequest(instance, request.Fields)
			if hostNetwork != (game.NetworkPair{}) {
				instance.Info.HostNetwork = hostNetwork
			}
		} else {
			applyRestoredGameRequest(instance, request.Fields, hostNetwork)
		}
		account := user.View().Account
		if !isRestored && account.IsTutorialPending() {
			instance.Info.Mode = game.ModeTutorial
			instance.Info.Level = game.TutorialLevel
			instance.Info.Attributes["SelectedDifficulty"] = "0"
		} else if !isRestored && instance.Info.Attributes["GameType"] == "3" {
			instance.Info.Mode = game.ModeArena
			instance.Info.Level = game.RandomArenaLevel()
			instance.Info.Name = "Darkspin Arena"
			instance.Info.TeamCapacity = 2
			instance.Info.Attributes["SelectedDifficulty"] = "0"
		} else if !isRestored && instance.Info.Attributes["GameType"] == "2" && instance.Info.Level == "" {
			instance.Info.Mode = game.ModeChain
			selection := account.ChainProgression + 1
			rawSelection := instance.Info.Attributes["SelectedDifficulty"]
			if rawSelection != "" {
				parsedSelection, parseErr := strconv.ParseUint(rawSelection, 10, 32)
				if parseErr != nil {
					gameManager.Remove(instance.ID)
					return &Response{ErrorCode: 0x0004}, nil
				}
				selection = uint32(parsedSelection)
			}
			if selection == 0 || selection > account.ChainProgression+1 {
				gameManager.Remove(instance.ID)
				return &Response{ErrorCode: 0x0004}, nil
			}
			level, isFound := gameManager.ChainLevelForSelection(selection)
			if !isFound {
				gameManager.Remove(instance.ID)
				return &Response{ErrorCode: 0x0004}, nil
			}
			instance.Info.Level = level
		}
		warpLevel := ""
		if !isRestored && instance.Info.Mode == game.ModeChain {
			queuedLevel, isWarpFound := gameManager.CampaignWarp(user.Account.ID)
			if isWarpFound {
				warpLevel = queuedLevel
				instance.Info.Level = queuedLevel
				instance.Info.IsWarped = true
			}
		}
		if !instance.AddPlayer(user) {
			rollbackResetGame(gameManager, instance, user, isRestored)
			logGameRequest(request,
				"game_create remote_ip=%q account=%q game_id=%d result=%q reason=%q",
				sessionRemoteIP(request.Session), user.LoginName, instance.ID,
				"rejected", "active_game",
			)
			return &Response{ErrorCode: 0x0004}, nil
		}
		if !isRestored {
			err = reservePartyGame(instance, user, userDirectory, partyDirectory)
			if err != nil {
				rollbackResetGame(gameManager, instance, user, false)
				return &Response{ErrorCode: 0x0004}, nil
			}
			if instance.Info.Mode == game.ModeArena {
				err = assignDirectArenaTeams(instance)
				if err != nil {
					rollbackResetGame(gameManager, instance, user, false)
					return &Response{ErrorCode: 0x0004}, nil
				}
			}
		}
		logGameRequest(request,
			"game_create remote_ip=%q account=%q game_id=%d mode=%d level=%q attributes=%v host_internal=%d:%d host_external=%d:%d topology=%d",
			sessionRemoteIP(request.Session), user.LoginName, instance.ID, instance.Info.Mode, instance.Info.Level,
			instance.Info.Attributes, instance.Info.HostNetwork.Internal.IP, instance.Info.HostNetwork.Internal.Port,
			instance.Info.HostNetwork.External.IP, instance.Info.HostNetwork.External.Port, instance.Info.NetworkTopology,
		)
		err = request.QueueNotification(GameManagerComponentID, 0x0f, gameIDFields(instance.ID))
		if err != nil {
			rollbackResetGame(gameManager, instance, user, isRestored)
			return nil, fmt.Errorf("resetNotify: %w", err)
		}
		err = queueGameSetupRoster(request, instance, gameSetupContextIndirectJoin, nil)
		if err != nil {
			rollbackResetGame(gameManager, instance, user, isRestored)
			return nil, fmt.Errorf("rosterNotify: %w", err)
		}
		if warpLevel != "" {
			gameManager.ConsumeCampaignWarp(user.Account.ID, warpLevel)
		}
		return &Response{Fields: gameIDFields(instance.ID)}, nil
	}
}

func sessionGameHostNetwork(
	session *Session, fallback game.NetworkPair,
) (game.NetworkPair, error) {
	if session == nil || session.conn == nil {
		return fallback, nil
	}
	localHost, _, err := net.SplitHostPort(session.conn.LocalAddr().String())
	if err != nil {
		return game.NetworkPair{}, fmt.Errorf("localAddressSplit: %w", err)
	}
	localIP := net.ParseIP(localHost).To4()
	if localIP == nil {
		return game.NetworkPair{}, fmt.Errorf("localAddressIPv4: invalid %q", localHost)
	}
	address := uint32(localIP[0])<<24 |
		uint32(localIP[1])<<16 |
		uint32(localIP[2])<<8 |
		uint32(localIP[3])
	fallback.Internal.IP = address
	fallback.External.IP = address
	return fallback, nil
}

// rollbackResetGame preserves an already-restored co-op shell when one
// reconnecting member cannot finish its Blaze setup. Fresh games have no
// prior participants to protect and are retired as one transaction.
func rollbackResetGame(
	gameManager *game.Manager, instance *game.Instance,
	user *sporenet.User, isRestored bool,
) {
	if gameManager == nil || instance == nil {
		return
	}
	if !isRestored {
		gameManager.Remove(instance.ID)
		return
	}
	if user != nil {
		instance.RemovePlayer(user.Account.ID)
	}
	if instance.ParticipantCount() == 0 {
		gameManager.Remove(instance.ID)
	}
}

func reservePartyGame(
	instance *game.Instance, actor *sporenet.User,
	userDirectory activeUserDirectory, partyDirectory partyDirectory,
) error {
	if instance == nil || actor == nil || partyDirectory == nil {
		return nil
	}
	partySnapshot, isFound := partyDirectory.PartyForMember(actor.Account.ID)
	if !isFound || len(partySnapshot.Members) <= 1 {
		return nil
	}
	if partySnapshot.LeaderID != actor.Account.ID {
		return errors.New("game party leader required")
	}
	if userDirectory == nil {
		return errors.New("game party directory unavailable")
	}
	instance.Info.PlaygroupID = strconv.FormatUint(uint64(partySnapshot.ID), 10)
	instance.Info.Attributes["PlaygroupKey"] = instance.Info.PlaygroupID
	instance.Info.Attributes["ExpectedPlayerCount"] = strconv.Itoa(len(partySnapshot.Members))
	instance.SetExpectedPlayerCount(uint16(len(partySnapshot.Members)))
	instance.SetMaxPlayers(uint16(len(partySnapshot.Members)))
	instance.ReserveAdmission()
	for memberIndex, member := range partySnapshot.Members {
		if member.ID == actor.Account.ID {
			continue
		}
		user := userDirectory.UserByID(member.ID)
		if user == nil {
			return fmt.Errorf("game party member %d unavailable", member.JoinOrdinal)
		}
		err := instance.AddPlayerAtSlot(user, uint16(memberIndex))
		if err != nil {
			return fmt.Errorf("game party member[%d]: %w", member.JoinOrdinal, err)
		}
	}
	return nil
}

func assignDirectArenaTeams(instance *game.Instance) error {
	if instance == nil {
		return errors.New("direct arena unavailable")
	}
	teamAttributes := [2][2]string{
		{"CustomTeam11", "CustomTeam12"},
		{"CustomTeam21", "CustomTeam22"},
	}
	assignedIDs := make(map[int64]struct{}, instance.ParticipantCount())
	for team, attributes := range teamAttributes {
		for _, attribute := range attributes {
			rawID := strings.TrimSpace(instance.Info.Attributes[attribute])
			if rawID == "" {
				continue
			}
			memberID, err := strconv.ParseInt(rawID, 10, 64)
			if err != nil || memberID <= 0 {
				return fmt.Errorf("direct arena %s invalid", attribute)
			}
			if _, isAssigned := assignedIDs[memberID]; isAssigned {
				return fmt.Errorf("direct arena member %d duplicated", memberID)
			}
			if !instance.AssignPlayerTeam(memberID, uint16(team+1)) {
				return fmt.Errorf("direct arena member %d unavailable", memberID)
			}
			assignedIDs[memberID] = struct{}{}
		}
	}
	if len(assignedIDs) != int(instance.ParticipantCount()) {
		return errors.New("direct arena roster incomplete")
	}
	return nil
}

// applyRestoredGameRequest refreshes connection-scoped Blaze metadata while
// preserving the mission identity and participant barrier owned by the
// checkpoint. A restarted client still supplies the ordinary capacity and
// topology envelope required by build 103.
func applyRestoredGameRequest(
	instance *game.Instance, fields []tdf.Field, hostNetwork game.NetworkPair,
) {
	if instance == nil {
		return
	}
	level := instance.Info.Level
	mode := instance.Info.Mode
	difficulty := instance.Info.Attributes["SelectedDifficulty"]
	expectedPlayerCount := instance.ParticipantCount()
	applyGameRequest(instance, fields)
	instance.Info.Level = level
	instance.Info.Mode = mode
	instance.Info.Attributes["SelectedDifficulty"] = difficulty
	instance.Info.Attributes["ExpectedPlayerCount"] = strconv.FormatUint(
		uint64(expectedPlayerCount), 10,
	)
	instance.SetExpectedPlayerCount(expectedPlayerCount)
	instance.SetMaxPlayers(max(instance.Info.MaxPlayers, expectedPlayerCount))
	if hostNetwork != (game.NetworkPair{}) {
		instance.Info.HostNetwork = hostNetwork
	}
}

func restoreGameCheckpoint(
	ctx context.Context, gameManager *game.Manager, resumeSource game.ResumeSource,
	user *sporenet.User,
) (*game.Instance, bool, error) {
	if resumeSource == nil || user == nil {
		return nil, false, nil
	}
	checkpoint, isFound, err := resumeSource.FindResume(ctx, user.Account.ID)
	if err != nil {
		return nil, false, fmt.Errorf("checkpointFind: %w", err)
	}
	if !isFound {
		return nil, false, nil
	}
	instance, err := gameManager.Restore(checkpoint, user)
	if err != nil {
		return nil, false, fmt.Errorf("checkpointGame: %w", err)
	}
	return instance, true, nil
}

func destroyGameHandler(
	gameManager *game.Manager, resumeSource game.ResumeSource,
) Handler {
	return func(_ context.Context, request *Request) (*Response, error) {
		gameID := uint32(fieldInteger(request.Fields, "GID"))
		instance := gameManager.Game(gameID)
		user := requestUser(request)
		if instance != nil && (user == nil || instance.HostUserID() != user.Account.ID) {
			account := ""
			if user != nil {
				account = user.LoginName
			}
			logGameRequest(request,
				"game_destroy remote_ip=%q account=%q game_id=%d result=%q reason=%q reason_code=%d",
				sessionRemoteIP(request.Session), account, gameID, "ignored", "not_host",
				fieldInteger(request.Fields, "REAS"),
			)
			return &Response{ErrorCode: gameManagerErrorPermissionDenied}, nil
		}
		var players []*sporenet.User
		if instance != nil {
			players = instance.Players()
		}
		account := ""
		if user != nil {
			account = user.LoginName
		}
		logGameRequest(request,
			"game_destroy remote_ip=%q account=%q game_id=%d result=%q reason_code=%d",
			sessionRemoteIP(request.Session), account, gameID, "accepted",
			fieldInteger(request.Fields, "REAS"),
		)
		gameManager.Remove(gameID)
		discardGameCheckpoint(resumeSource, gameID)
		err := queueGameNotification(request, players, 0x10, []tdf.Field{
			tdf.FieldNamed("GID", tdf.IntegerValue(uint64(gameID))),
			tdf.FieldNamed("REAS", tdf.IntegerValue(fieldInteger(request.Fields, "REAS"))),
		})
		if err != nil {
			return nil, fmt.Errorf("destroyNotify: %w", err)
		}
		return &Response{Fields: gameIDFields(gameID)}, nil
	}
}

func joinGameHandler(gameManager *game.Manager) Handler {
	return func(_ context.Context, request *Request) (*Response, error) {
		user := requestUser(request)
		if user == nil {
			return &Response{ErrorCode: authInvalidUser}, nil
		}
		gameID := uint32(fieldInteger(request.Fields, "GID"))
		instance := gameManager.Game(gameID)
		if instance == nil {
			return &Response{ErrorCode: 0x0002}, nil
		}
		isReserved := instance.HasPlayer(user.Account.ID)
		if instance.IsAdmissionReserved() && !isReserved {
			return &Response{ErrorCode: 0x0004}, nil
		}
		hostNetwork, err := sessionGameHostNetwork(
			request.Session, instance.Info.HostNetwork,
		)
		if err != nil {
			return nil, fmt.Errorf("joinHostNetwork: %w", err)
		}
		existingPlayers := instance.Players()
		if !instance.AddPlayer(user) {
			return &Response{ErrorCode: 0x0004}, nil
		}
		setupFields, err := gameSetupFields(
			instance, user, gameSetupContextJoin, 0, 0, hostNetwork, nil,
		)
		if err != nil {
			return nil, fmt.Errorf("joinSetupFields: %w", err)
		}
		err = request.QueueUserNotification(
			user.Account.ID, GameManagerComponentID, 0x14,
			setupFields,
		)
		if err != nil {
			return nil, fmt.Errorf("joinSetup: %w", err)
		}
		joiningFields := []tdf.Field{
			tdf.FieldNamed("GID", tdf.IntegerValue(uint64(gameID))),
			tdf.FieldNamed("PDAT", tdf.StructValue(gamePlayerFields(instance, user)...)),
		}
		if !isReserved {
			for _, existing := range existingPlayers {
				err = request.QueueUserNotification(existing.Account.ID, GameManagerComponentID, 0x15, joiningFields)
				if err != nil {
					return nil, fmt.Errorf("joinExisting: %w", err)
				}
			}
		}
		return &Response{Fields: gameIDFields(gameID)}, nil
	}
}

func removePlayerHandler(
	gameManager *game.Manager, completer tutorialCompleter,
	resumeSource game.ResumeSource,
) Handler {
	return func(ctx context.Context, request *Request) (*Response, error) {
		gameID := uint32(fieldInteger(request.Fields, "GID"))
		personaID := int64(fieldInteger(request.Fields, "PID"))
		reason := fieldInteger(request.Fields, "REAS")
		logGameRequest(request,
			"game_remove remote_ip=%q game_id=%d persona_id=%d reason=%d context=%d",
			sessionRemoteIP(request.Session), gameID, personaID, reason, fieldInteger(request.Fields, "CNTX"),
		)
		instance := gameManager.Game(gameID)
		var players []*sporenet.User
		isGameDestroyed := false
		if instance != nil {
			players = instance.Players()
			user := requestUser(request)
			isLaunchHandoffRemoval := reason == returnToShipReason && user != nil &&
				user.Account.ID == personaID &&
				instance.ConsumeLaunchHandoffRemoval(personaID)
			if isLaunchHandoffRemoval {
				logGameRequest(request,
					"game_remove_preserved account=%q game_id=%d persona_id=%d reason=%d context=%d transition=%q",
					user.LoginName, gameID, personaID, reason,
					fieldInteger(request.Fields, "CNTX"), "campaign_launch_handoff",
				)
				return &Response{}, nil
			}
			// Aborting leaves only this member while a co-op teammate remains.
			// RemoveMember transfers host ownership through the normal roster path.
			isGameAbort := reason == returnToShipReason && user != nil &&
				user.Account.ID == personaID && instance.HostUserID() == personaID &&
				(instance.Info.Mode != game.ModeChain || len(players) <= 1)
			if reason == returnToShipReason && user != nil &&
				user.Account.ID == personaID &&
				gameManager.CanResumeDefeatedMember(gameID, personaID) {
				logGameRequest(request,
					"game_remove_preserved account=%q game_id=%d persona_id=%d reason=%d transition=%q",
					user.LoginName, gameID, personaID, reason, "defeated_coop_resume",
				)
				return &Response{}, nil
			}
			isTutorialReturn := reason == returnToShipReason && instance.Info.Mode == game.ModeTutorial &&
				instance.Info.Level == game.TutorialLevel && instance.IsTutorialComplete(personaID) &&
				user != nil && user.Account.ID == personaID
			if isTutorialReturn {
				_, err := completer.CompleteTutorial(ctx, personaID)
				if err != nil {
					return nil, fmt.Errorf("removeTutorial: %w", err)
				}
			}
			if isGameAbort {
				gameManager.Remove(gameID)
				discardGameCheckpoint(resumeSource, gameID)
				isGameDestroyed = true
			} else {
				gameManager.RemoveMember(gameID, personaID)
			}
		}
		if isGameDestroyed {
			err := queueGameNotification(request, players, 0x28, []tdf.Field{
				tdf.FieldNamed("CNTX", tdf.IntegerValue(0)),
				tdf.FieldNamed("GID", tdf.IntegerValue(uint64(gameID))),
				tdf.FieldNamed("PID", tdf.IntegerValue(uint64(personaID))),
				tdf.FieldNamed("REAS", tdf.IntegerValue(reason)),
			})
			if err != nil {
				return nil, fmt.Errorf("removeAbortNotify: %w", err)
			}
			err = queueGameNotification(request, players, 0x10, []tdf.Field{
				tdf.FieldNamed("GID", tdf.IntegerValue(uint64(gameID))),
				tdf.FieldNamed("REAS", tdf.IntegerValue(reason)),
			})
			if err != nil {
				return nil, fmt.Errorf("removeDestroyNotify: %w", err)
			}
			return &Response{}, nil
		}
		err := queueGameNotification(request, players, 0x28, []tdf.Field{
			tdf.FieldNamed("CNTX", tdf.IntegerValue(0)),
			tdf.FieldNamed("GID", tdf.IntegerValue(uint64(gameID))),
			tdf.FieldNamed("PID", tdf.IntegerValue(uint64(personaID))),
			tdf.FieldNamed("REAS", tdf.IntegerValue(fieldInteger(request.Fields, "REAS"))),
		})
		if err != nil {
			return nil, fmt.Errorf("removeNotify: %w", err)
		}
		return &Response{}, nil
	}
}

func discardGameCheckpoint(resumeSource game.ResumeSource, gameID uint32) {
	repository, isSupported := resumeSource.(game.ResumeRepository)
	if !isSupported || gameID == 0 {
		return
	}
	repository.Discard(uint64(gameID))
}

func finalizeGameHandler(gameManager *game.Manager) Handler {
	return func(_ context.Context, request *Request) (*Response, error) {
		user := requestUser(request)
		if user == nil {
			return &Response{ErrorCode: authInvalidUser}, nil
		}
		instance := gameManager.Game(user.CurrentGameID())
		if instance == nil {
			return &Response{}, nil
		}
		if instance.HostUserID() != user.Account.ID {
			return &Response{ErrorCode: gameManagerErrorPermissionDenied}, nil
		}
		isFinalized := instance.FinalizePreGame()
		if !isFinalized {
			return &Response{Fields: gameIDFields(instance.ID)}, nil
		}
		players := instance.Players()
		err := queueGameNotification(request, players, 0x64, []tdf.Field{
			tdf.FieldNamed("GID", tdf.IntegerValue(uint64(instance.ID))),
			tdf.FieldNamed("GSTA", tdf.IntegerValue(uint64(game.StatePreGame))),
		})
		if err != nil {
			return nil, fmt.Errorf("finalizePreGame: %w", err)
		}
		_, isPublicationPending := instance.MarkPlayerReady(user.Account.ID)
		if !isPublicationPending {
			return &Response{Fields: gameIDFields(instance.ID)}, nil
		}
		err = publishGameStart(request, instance)
		if err != nil {
			return nil, fmt.Errorf("finalizeStart: %w", err)
		}
		return &Response{Fields: gameIDFields(instance.ID)}, nil
	}
}

func publishGameStart(request *Request, instance *game.Instance) error {
	if request == nil || instance == nil {
		return errors.New("game start unavailable")
	}
	players := instance.Players()
	err := queueGameNotification(request, players, 0x64, []tdf.Field{
		tdf.FieldNamed("GID", tdf.IntegerValue(uint64(instance.ID))),
		tdf.FieldNamed("GSTA", tdf.IntegerValue(uint64(game.StateInGame))),
	})
	if err != nil {
		instance.CancelStartPublication()
		return fmt.Errorf("startState: %w", err)
	}
	for _, player := range players {
		isJoinPublicationReserved := instance.ReservePlayerJoinPublication(
			player.Account.ID,
		)
		if !isJoinPublicationReserved {
			continue
		}
		err = queueGameNotification(request, players, 0x74, []tdf.Field{
			tdf.FieldNamed("GID", tdf.IntegerValue(uint64(instance.ID))),
			tdf.FieldNamed("PID", tdf.IntegerValue(uint64(player.Account.ID))),
			tdf.FieldNamed("STAT", tdf.IntegerValue(gamePlayerStateConnected)),
		})
		if err != nil {
			instance.CancelPlayerJoinPublication(player.Account.ID)
			instance.CancelStartPublication()
			return fmt.Errorf("startStatus[%d]: %w", player.Account.ID, err)
		}
		err = queueGameNotification(request, players, 0x1e, []tdf.Field{
			tdf.FieldNamed("GID", tdf.IntegerValue(uint64(instance.ID))),
			tdf.FieldNamed("PID", tdf.IntegerValue(uint64(player.Account.ID))),
		})
		if err != nil {
			instance.CancelPlayerJoinPublication(player.Account.ID)
			instance.CancelStartPublication()
			return fmt.Errorf("startJoin[%d]: %w", player.Account.ID, err)
		}
	}
	instance.MarkStartPublished()
	return nil
}

func advanceGameStateHandler(gameManager *game.Manager) Handler {
	return func(_ context.Context, request *Request) (*Response, error) {
		gameID := uint32(fieldInteger(request.Fields, "GID"))
		state := game.SessionState(fieldInteger(request.Fields, "GSTA"))
		instance := gameManager.Game(gameID)
		var players []*sporenet.User
		if instance != nil {
			if state == game.StateInGame {
				user := requestUser(request)
				if user == nil {
					return &Response{ErrorCode: authInvalidUser}, nil
				}
				_, isPublicationPending := instance.MarkPlayerReady(user.Account.ID)
				if !isPublicationPending {
					return &Response{}, nil
				}
			} else {
				instance.SetState(state)
			}
			players = instance.Players()
		}
		if instance != nil && state == game.StateInGame {
			err := publishGameStart(request, instance)
			if err != nil {
				return nil, fmt.Errorf("stateStart: %w", err)
			}
			return &Response{}, nil
		}
		err := queueGameNotification(request, players, 0x64, []tdf.Field{
			tdf.FieldNamed("GID", tdf.IntegerValue(uint64(gameID))),
			tdf.FieldNamed("GSTA", tdf.IntegerValue(uint64(state))),
		})
		if err != nil {
			return nil, fmt.Errorf("stateNotify: %w", err)
		}
		return &Response{}, nil
	}
}

func logGameRequest(request *Request, format string, arguments ...any) {
	if request == nil {
		return
	}
	if request.Session == nil {
		return
	}
	if request.Session.server == nil {
		return
	}
	if request.Session.server.logger == nil {
		return
	}
	request.Session.server.logger.Printf(format, arguments...)
}

func queueGameNotification(request *Request, players []*sporenet.User, command uint16, fields []tdf.Field) error {
	for index, player := range players {
		if player == nil {
			continue
		}
		err := request.QueueUserNotification(player.Account.ID, GameManagerComponentID, command, fields)
		if err != nil {
			return fmt.Errorf("gameNotify[%d]: %w", index, err)
		}
	}
	return nil
}

// queueGameSetupRoster projects one complete, stable game roster to every
// initially admitted member. PROS already owns those player records; repeating
// them as player-joining notifications makes a non-host client tear down its
// freshly installed game.
func queueGameSetupRoster(
	request *Request, instance *game.Instance, setupContext uint64,
	matchmakingSessions []pvp.MatchmakingSession,
) error {
	if request == nil || instance == nil {
		return errors.New("game roster unavailable")
	}
	players := instance.Players()
	if len(players) == 0 {
		return errors.New("game roster empty")
	}
	if request.Session == nil || request.Session.server == nil {
		return errors.New("game roster session unavailable")
	}
	matchmakingSessionIDsByUser := make(map[int64]uint64, len(matchmakingSessions))
	matchmakingSessionsByUser := make(map[int64]pvp.MatchmakingSession, len(matchmakingSessions))
	playerSessionIDsByUser := make(map[int64]uint32, len(matchmakingSessions))
	for _, matchmakingSession := range matchmakingSessions {
		matchmakingSessionIDsByUser[matchmakingSession.UserID] = matchmakingSession.ID
		matchmakingSessionsByUser[matchmakingSession.UserID] = matchmakingSession
		if setupContext != gameSetupContextMatchmaking {
			continue
		}
		playerSessions := request.Session.server.sessionsForUser(matchmakingSession.UserID)
		if len(playerSessions) != 1 {
			return fmt.Errorf(
				"gamePlayerSession[%d]: got %d", matchmakingSession.UserID,
				len(playerSessions),
			)
		}
		playerSessionIDsByUser[matchmakingSession.UserID] = playerSessions[0].ID
	}
	for recipientIndex, recipient := range players {
		recipientContext := setupContext
		if setupContext == gameSetupContextIndirectJoin &&
			recipient.Account.ID == instance.HostUserID() {
			recipientContext = gameSetupContextCreate
		}
		matchmakingSessionID := uint64(0)
		setupPlaygroupID := uint32(0)
		if setupContext == gameSetupContextMatchmaking {
			var isMatchmakingSessionFound bool
			matchmakingSessionID, isMatchmakingSessionFound =
				matchmakingSessionIDsByUser[recipient.Account.ID]
			if !isMatchmakingSessionFound || matchmakingSessionID == 0 {
				return fmt.Errorf("gameSetupSession[%d]: missing", recipientIndex)
			}
			matchmakingSession := matchmakingSessionsByUser[recipient.Account.ID]
			setupPlaygroupID = matchmakingSession.PartyID
			if matchmakingSession.OwnerID != 0 &&
				matchmakingSession.OwnerID != recipient.Account.ID {
				recipientContext = gameSetupContextIndirectJoin
				matchmakingSessionID = 0
			}
		}
		recipientSessions := request.Session.server.sessionsForUser(recipient.Account.ID)
		for sessionIndex, recipientSession := range recipientSessions {
			hostNetwork, err := sessionGameHostNetwork(
				recipientSession, instance.Info.HostNetwork,
			)
			if err != nil {
				return fmt.Errorf(
					"gameHostNetwork[%d:%d]: %w", recipientIndex, sessionIndex, err,
				)
			}
			setupFields, err := gameSetupFields(
				instance, recipient, recipientContext, matchmakingSessionID,
				setupPlaygroupID, hostNetwork, playerSessionIDsByUser,
			)
			if err != nil {
				return fmt.Errorf(
					"gameSetupFields[%d:%d]: %w", recipientIndex, sessionIndex, err,
				)
			}
			err = request.QueueSessionNotification(
				recipientSession, GameManagerComponentID, 0x14, setupFields,
			)
			if err != nil {
				return fmt.Errorf(
					"gameSetup[%d:%d]: %w", recipientIndex, sessionIndex, err,
				)
			}
		}
	}
	return nil
}

func cancelMatchmakingHandler(matchmaking *pvp.Matchmaking) Handler {
	return func(_ context.Context, request *Request) (*Response, error) {
		sessionID := fieldInteger(request.Fields, "MSID")
		user := requestUser(request)
		if user == nil {
			logGameRequest(request,
				"matchmaking_cancel remote_ip=%q session_id=%d result=%q reason=%q",
				sessionRemoteIP(request.Session), sessionID, "rejected", "invalid_user",
			)
			return &Response{Fields: []tdf.Field{tdf.FieldNamed("MSID", tdf.IntegerValue(sessionID))}}, nil
		}
		isCancelled := matchmaking.Cancel(user.Account.ID, sessionID)
		err := request.QueueNotification(GameManagerComponentID, 0x0a, []tdf.Field{
			tdf.FieldNamed("MAXF", tdf.IntegerValue(0)),
			tdf.FieldNamed("MSID", tdf.IntegerValue(sessionID)),
			tdf.FieldNamed("RSLT", tdf.IntegerValue(matchmakingResultCancelled)),
			tdf.FieldNamed("USID", tdf.IntegerValue(uint64(user.Account.ID))),
		})
		if err != nil {
			return nil, fmt.Errorf("matchNotify: %w", err)
		}
		logGameRequest(request,
			"matchmaking_cancel remote_ip=%q account=%q session_id=%d result=%q removed=%t",
			sessionRemoteIP(request.Session), user.LoginName, sessionID, "cancelled", isCancelled,
		)
		return &Response{Fields: []tdf.Field{tdf.FieldNamed("MSID", tdf.IntegerValue(sessionID))}}, nil
	}
}

func startMatchmakingHandler(
	matchmaking *pvp.Matchmaking, gameManager *game.Manager,
	userDirectory activeUserDirectory, partyDirectory partyDirectory,
) Handler {
	return func(_ context.Context, request *Request) (*Response, error) {
		user := requestUser(request)
		if user == nil {
			return &Response{ErrorCode: authInvalidUser}, nil
		}
		if user.CurrentGameID() != 0 {
			return &Response{ErrorCode: 0x0004}, nil
		}
		criteria, err := matchmakingCriteria(request.Fields)
		if err != nil {
			logGameRequest(request,
				"matchmaking_start remote_ip=%q account=%q result=%q error=%q field_shape=%v",
				sessionRemoteIP(request.Session), user.LoginName, "invalid_criteria",
				err, traceFieldShapes(request.Fields),
			)
			return &Response{ErrorCode: ErrorSystem}, nil
		}
		userIDs, partyID, err := matchmakingUsers(
			user, criteria, userDirectory, partyDirectory, matchmaking,
		)
		if err != nil {
			logGameRequest(request,
				"matchmaking_start remote_ip=%q account=%q result=%q game_type=%d expected_players=%d team_size=%d ranked=%t error=%q",
				sessionRemoteIP(request.Session), user.LoginName, "rejected",
				criteria.GameType, criteria.ExpectedPlayerCount, criteria.TeamSize,
				criteria.IsRanked, err,
			)
			return &Response{ErrorCode: ErrorSystem}, nil
		}
		admission, err := matchmaking.Enter(pvp.MatchmakingRequest{
			UserID: user.Account.ID, UserIDs: userIDs, PartyID: partyID,
			Criteria: criteria,
		})
		if err != nil {
			logGameRequest(request,
				"matchmaking_start remote_ip=%q account=%q result=%q game_type=%d expected_players=%d team_size=%d ranked=%t field_shape=%v",
				sessionRemoteIP(request.Session), user.LoginName, "rejected",
				criteria.GameType, criteria.ExpectedPlayerCount, criteria.TeamSize,
				criteria.IsRanked, traceFieldShapes(request.Fields),
			)
			return &Response{ErrorCode: ErrorSystem}, nil
		}
		logGameRequest(request,
			"matchmaking_start remote_ip=%q account=%q session_id=%d result=%q game_type=%d expected_players=%d team_size=%d ranked=%t field_shape=%v",
			sessionRemoteIP(request.Session), user.LoginName, admission.Session.ID, "queued",
			criteria.GameType, criteria.ExpectedPlayerCount, criteria.TeamSize,
			criteria.IsRanked, traceFieldShapes(request.Fields),
		)
		if len(admission.Sessions) != 0 {
			instance, formErr := formMatchmakingGame(
				gameManager, userDirectory, admission.Sessions, criteria,
			)
			if formErr != nil {
				err = queueMatchmakingFailures(request, admission.Sessions)
				if err != nil {
					return nil, fmt.Errorf("matchFailureNotify: %w", err)
				}
				logGameRequest(request,
					"matchmaking_form session_id=%d result=%q error=%q",
					admission.Session.ID, "rejected", formErr,
				)
			} else {
				err = queueGameSetupRoster(
					request, instance, gameSetupContextMatchmaking, admission.Sessions,
				)
				if err != nil {
					gameManager.Remove(instance.ID)
					return nil, fmt.Errorf("matchRoster: %w", err)
				}
				if instance.Info.IsWarped {
					for _, session := range admission.Sessions {
						gameManager.ConsumeCampaignWarp(session.UserID, instance.Info.Level)
					}
				}
				logGameRequest(request,
					"matchmaking_form session_id=%d game_id=%d level=%q result=%q players=%d",
					admission.Session.ID, instance.ID, instance.Info.Level,
					"matched", len(admission.Sessions),
				)
			}
		}
		return &Response{Fields: []tdf.Field{
			tdf.FieldNamed("MSID", tdf.IntegerValue(admission.Session.ID)),
		}}, nil
	}
}

func matchmakingUsers(
	user *sporenet.User, criteria pvp.MatchmakingCriteria,
	userDirectory activeUserDirectory, partyDirectory partyDirectory,
	matchmaking *pvp.Matchmaking,
) ([]int64, uint32, error) {
	if user == nil {
		return nil, 0, errors.New("matchmaking user unavailable")
	}
	userIDs := []int64{user.Account.ID}
	if criteria.GameType != pvp.ArenaGameType || partyDirectory == nil {
		return userIDs, 0, nil
	}
	partySnapshot, isFound := partyDirectory.PartyForMember(user.Account.ID)
	if !isFound || len(partySnapshot.Members) <= 1 {
		return userIDs, 0, nil
	}
	if partySnapshot.LeaderID != user.Account.ID {
		if matchmaking != nil && matchmaking.IsQueued(user.Account.ID) {
			return userIDs, partySnapshot.ID, nil
		}
		return nil, 0, errors.New("arena party leader required")
	}
	if len(partySnapshot.Members) > int(criteria.TeamSize) {
		return nil, 0, errors.New("arena party exceeds team size")
	}
	userIDs = make([]int64, 0, len(partySnapshot.Members))
	for _, member := range partySnapshot.Members {
		memberUser := userDirectory.UserByID(member.ID)
		if memberUser == nil || memberUser.CurrentGameID() != 0 {
			return nil, 0, fmt.Errorf("arena party member %d unavailable", member.JoinOrdinal)
		}
		userIDs = append(userIDs, member.ID)
	}
	return userIDs, partySnapshot.ID, nil
}

func formMatchmakingGame(
	gameManager *game.Manager, userDirectory activeUserDirectory,
	sessions []pvp.MatchmakingSession, criteria pvp.MatchmakingCriteria,
) (*game.Instance, error) {
	switch criteria.GameType {
	case pvp.CampaignGameType:
		instance, err := formCampaignGame(gameManager, userDirectory, sessions, criteria)
		if err != nil {
			return nil, fmt.Errorf("campaignForm: %w", err)
		}
		return instance, nil
	case pvp.ArenaGameType:
		instance, err := formArenaGame(gameManager, userDirectory, sessions, criteria)
		if err != nil {
			return nil, fmt.Errorf("arenaForm: %w", err)
		}
		return instance, nil
	default:
		return nil, errors.New("matchmaking game type unsupported")
	}
}

func formCampaignGame(
	gameManager *game.Manager, userDirectory activeUserDirectory,
	sessions []pvp.MatchmakingSession, criteria pvp.MatchmakingCriteria,
) (*game.Instance, error) {
	if gameManager == nil || userDirectory == nil ||
		len(sessions) != int(pvp.CampaignExpectedPlayerCount) {
		return nil, errors.New("campaign formation invalid")
	}
	selection, err := strconv.ParseUint(criteria.SelectedDifficulty, 10, 32)
	if err != nil || selection == 0 {
		return nil, errors.New("campaign selection invalid")
	}
	level, isFound := gameManager.ChainLevelForSelection(uint32(selection))
	if !isFound {
		return nil, errors.New("campaign level unavailable")
	}
	players := make([]*sporenet.User, 0, len(sessions))
	for sessionIndex, session := range sessions {
		user := userDirectory.UserByID(session.UserID)
		if user == nil || user.CurrentGameID() != 0 {
			return nil, fmt.Errorf("campaign member[%d] unavailable", sessionIndex)
		}
		if uint32(selection) > user.View().Account.ChainProgression+1 {
			return nil, fmt.Errorf("campaign member[%d] progression locked", sessionIndex)
		}
		players = append(players, user)
	}
	instance := gameManager.Create()
	instance.Info.Mode = game.ModeChain
	instance.Info.Level = level
	instance.Info.Name = "Darkspin Campaign"
	instance.Info.Attributes["GameType"] = strconv.FormatUint(uint64(criteria.GameType), 10)
	instance.Info.Attributes["Ranked"] = "0"
	instance.Info.Attributes["SelectedDifficulty"] = criteria.SelectedDifficulty
	instance.Info.Attributes["ExpectedPlayerCount"] = strconv.Itoa(len(players))
	instance.Info.Attributes["TeamSize"] = "0"
	instance.Info.MaxPlayers = pvp.CampaignExpectedPlayerCount
	instance.Info.ExpectedPlayerCount = pvp.CampaignExpectedPlayerCount
	instance.Info.Capacity = []uint32{uint32(pvp.CampaignExpectedPlayerCount)}
	warpLevel := ""
	for _, player := range players {
		queuedLevel, isWarpFound := gameManager.CampaignWarp(player.Account.ID)
		if !isWarpFound {
			continue
		}
		if warpLevel != "" && !strings.EqualFold(warpLevel, queuedLevel) {
			gameManager.Remove(instance.ID)
			return nil, errors.New("campaign warp destinations conflict")
		}
		warpLevel = queuedLevel
	}
	if warpLevel != "" {
		instance.Info.Level = warpLevel
		instance.Info.IsWarped = true
	}
	instance.ReserveAdmission()
	for playerIndex, player := range players {
		err = instance.AddPlayerAtSlot(player, uint16(playerIndex))
		if err != nil {
			gameManager.Remove(instance.ID)
			return nil, fmt.Errorf("campaignMember[%d]: %w", playerIndex, err)
		}
	}
	return instance, nil
}

func formArenaGame(
	gameManager *game.Manager, userDirectory activeUserDirectory,
	sessions []pvp.MatchmakingSession, criteria pvp.MatchmakingCriteria,
) (*game.Instance, error) {
	if gameManager == nil || userDirectory == nil ||
		!pvp.IsArenaCriteria(criteria) ||
		len(sessions) != int(criteria.ExpectedPlayerCount) {
		return nil, errors.New("arena formation invalid")
	}
	orderedSessions := make([]pvp.MatchmakingSession, 0, len(sessions))
	for _, session := range sessions {
		if session.PartyID != 0 {
			orderedSessions = append(orderedSessions, session)
		}
	}
	for _, session := range sessions {
		if session.PartyID == 0 {
			orderedSessions = append(orderedSessions, session)
		}
	}
	sessions = orderedSessions
	players := make([]*sporenet.User, 0, len(sessions))
	partyID := uint32(0)
	for sessionIndex, session := range sessions {
		user := userDirectory.UserByID(session.UserID)
		if user == nil || user.CurrentGameID() != 0 {
			return nil, fmt.Errorf("arena member[%d] unavailable", sessionIndex)
		}
		players = append(players, user)
		if session.PartyID == 0 {
			continue
		}
		if partyID == 0 {
			partyID = session.PartyID
		}
	}
	instance := gameManager.Create()
	instance.Info.Mode = game.ModeArena
	instance.Info.Level = game.RandomArenaLevel()
	instance.Info.Name = "Darkspin Arena"
	instance.Info.Attributes["GameType"] = strconv.FormatUint(uint64(criteria.GameType), 10)
	instance.Info.Attributes["Ranked"] = strconv.FormatUint(boolInteger(criteria.IsRanked), 10)
	instance.Info.Attributes["SelectedDifficulty"] = criteria.SelectedDifficulty
	instance.Info.Attributes["ExpectedPlayerCount"] = strconv.Itoa(len(players))
	instance.Info.Attributes["TeamSize"] = strconv.FormatUint(uint64(criteria.TeamSize), 10)
	if partyID != 0 {
		instance.Info.PlaygroupID = strconv.FormatUint(uint64(partyID), 10)
		instance.Info.Attributes["PlaygroupKey"] = instance.Info.PlaygroupID
	}
	instance.Info.MaxPlayers = criteria.ExpectedPlayerCount
	instance.Info.ExpectedPlayerCount = criteria.ExpectedPlayerCount
	instance.Info.TeamCapacity = pvp.ArenaTeamCount
	instance.Info.Capacity = []uint32{uint32(criteria.ExpectedPlayerCount)}
	instance.ReserveAdmission()
	for playerIndex, player := range players {
		err := instance.AddPlayerAtSlot(player, uint16(playerIndex))
		if err != nil {
			gameManager.Remove(instance.ID)
			return nil, fmt.Errorf("arenaMember[%d]: %w", playerIndex, err)
		}
		team := uint16(playerIndex)/criteria.TeamSize + 1
		if !instance.AssignPlayerTeam(player.Account.ID, team) {
			gameManager.Remove(instance.ID)
			return nil, fmt.Errorf("arena team[%d] unavailable", playerIndex)
		}
	}
	return instance, nil
}

func queueMatchmakingFailures(request *Request, sessions []pvp.MatchmakingSession) error {
	for sessionIndex, session := range sessions {
		if session.OwnerID != 0 && session.OwnerID != session.UserID {
			continue
		}
		fields := []tdf.Field{
			tdf.FieldNamed("MAXF", tdf.IntegerValue(0)),
			tdf.FieldNamed("MSID", tdf.IntegerValue(session.ID)),
			tdf.FieldNamed("RSLT", tdf.IntegerValue(matchmakingResultFailed)),
			tdf.FieldNamed("USID", tdf.IntegerValue(uint64(session.UserID))),
		}
		err := request.QueueUserNotification(
			session.UserID, GameManagerComponentID, 0x0a, fields,
		)
		if err != nil {
			return fmt.Errorf("matchResult[%d]: %w", sessionIndex, err)
		}
	}
	return nil
}

func matchmakingCriteria(fields []tdf.Field) (pvp.MatchmakingCriteria, error) {
	attributes := make(map[string]string)
	collectMatchmakingStrings(fields, attributes)
	gameType := uint64(0)
	if attributes["GameType"] != "" {
		parsedGameType, err := strconv.ParseUint(attributes["GameType"], 10, 32)
		if err != nil {
			return pvp.MatchmakingCriteria{}, fmt.Errorf("criteriaGameType: %w", err)
		}
		gameType = parsedGameType
	}
	expectedPlayerCount := uint64(0)
	if attributes["ExpectedPlayerCount"] != "" {
		parsedPlayerCount, err := strconv.ParseUint(attributes["ExpectedPlayerCount"], 10, 16)
		if err != nil {
			return pvp.MatchmakingCriteria{}, fmt.Errorf("criteriaPlayerCount: %w", err)
		}
		expectedPlayerCount = parsedPlayerCount
	}
	teamSize := uint64(0)
	if attributes["TeamSize"] != "" {
		parsedTeamSize, err := strconv.ParseUint(attributes["TeamSize"], 10, 16)
		if err != nil {
			return pvp.MatchmakingCriteria{}, fmt.Errorf("criteriaTeamSize: %w", err)
		}
		teamSize = parsedTeamSize
	}
	return pvp.MatchmakingCriteria{
		GameType: uint32(gameType), ExpectedPlayerCount: uint16(expectedPlayerCount),
		TeamSize: uint16(teamSize), SelectedDifficulty: attributes["SelectedDifficulty"],
		IsRanked: attributes["Ranked"] == "1",
	}, nil
}

func collectMatchmakingStrings(fields []tdf.Field, attributes map[string]string) {
	for _, field := range fields {
		collectMatchmakingValue(field.Value, attributes)
	}
}

func collectMatchmakingValue(matchValue tdf.Value, attributes map[string]string) {
	switch matchValue.Type {
	case tdf.Struct, tdf.Union:
		collectMatchmakingStrings(matchValue.Fields, attributes)
	case tdf.List:
		for _, item := range matchValue.Items {
			collectMatchmakingValue(item, attributes)
		}
	case tdf.Map:
		for _, entry := range matchValue.Entries {
			if entry.Key.Type == tdf.String && entry.Value.Type == tdf.String {
				attributes[entry.Key.String] = entry.Value.String
			}
			collectMatchmakingValue(entry.Value, attributes)
		}
	}
}

func updateMeshHandler(gameManager *game.Manager) Handler {
	return func(_ context.Context, request *Request) (*Response, error) {
		user := requestUser(request)
		if user == nil {
			return &Response{}, nil
		}
		gameID := uint32(fieldInteger(request.Fields, "GID"))
		instance := gameManager.Game(gameID)
		if instance == nil || !instance.HasPlayer(user.Account.ID) {
			return &Response{ErrorCode: 0x0002}, nil
		}
		logGameRequest(request,
			"game_mesh remote_ip=%q account=%q game_id=%d targets=%s",
			sessionRemoteIP(request.Session), user.LoginName, gameID,
			gameMeshValueSummary(fieldValue(request.Fields, "TARG")),
		)
		_, isPublicationPending := instance.MarkPlayerReady(user.Account.ID)
		if !isPublicationPending {
			return &Response{}, nil
		}
		err := publishGameStart(request, instance)
		if err != nil {
			return nil, fmt.Errorf("meshStart: %w", err)
		}
		return &Response{}, nil
	}
}

func gameMeshValueSummary(meshValue tdf.Value) string {
	switch meshValue.Type {
	case tdf.Integer:
		return strconv.FormatUint(meshValue.Integer, 10)
	case tdf.String:
		return strconv.Quote(meshValue.String)
	case tdf.ObjectID:
		return fmt.Sprintf(
			"object(%d:%d:%d)", meshValue.ObjectID[0], meshValue.ObjectID[1], meshValue.ObjectID[2],
		)
	case tdf.Struct, tdf.Union:
		fields := make([]string, 0, len(meshValue.Fields))
		for _, field := range meshValue.Fields {
			fields = append(fields, field.Label+"="+gameMeshValueSummary(field.Value))
		}
		return "{" + strings.Join(fields, ",") + "}"
	case tdf.List:
		items := make([]string, 0, len(meshValue.Items))
		for _, item := range meshValue.Items {
			items = append(items, gameMeshValueSummary(item))
		}
		return "[" + strings.Join(items, ",") + "]"
	default:
		return traceTypeName(meshValue.Type)
	}
}

func applyGameRequest(instance *game.Instance, fields []tdf.Field) {
	instance.Info.Name = fieldString(fields, "GNAM")
	instance.Info.Type = fieldString(fields, "GTYP")
	instance.Info.Version = valueOrDefault(fieldString(fields, "VSTR"), instance.Info.Version)
	instance.Info.Settings = fieldInteger(fields, "GSET")
	instance.Info.NetworkTopology = uint32(fieldInteger(fields, "NTOP"))
	instance.Info.PresenceMode = uint32(fieldInteger(fields, "PRES"))
	instance.Info.VoIPTopology = uint32(fieldInteger(fields, "VOIP"))
	instance.SetMaxPlayers(uint16(fieldInteger(fields, "PMAX")))
	instance.Info.QueueCapacity = uint16(fieldInteger(fields, "QCAP"))
	instance.Info.TeamCapacity = uint16(fieldInteger(fields, "TCAP"))
	instance.Info.TeamIndex = uint16(fieldInteger(fields, "TIDX"))
	instance.Info.PlaygroupID = fieldString(fields, "PGID")
	instance.Info.IsResettable = fieldInteger(fields, "NRES") != 0
	instance.Info.IsIgnored = fieldInteger(fields, "IGNO") != 0
	attributes, isFound := tdf.Find(fields, "ATTR")
	if isFound && attributes.Type == tdf.Map {
		for _, entry := range attributes.Entries {
			instance.Info.Attributes[entry.Key.String] = entry.Value.String
		}
	}
	expectedPlayerCount, err := strconv.ParseUint(
		instance.Info.Attributes["ExpectedPlayerCount"], 10, 16,
	)
	if err == nil {
		instance.SetExpectedPlayerCount(uint16(expectedPlayerCount))
	}
	instance.Info.Capacity = integerList(fieldValue(fields, "PCAP"))
	instance.Info.HostNetwork = requestNetworkPair(fieldValue(fields, "HNET"))
}

func gameIDFields(gameID uint32) []tdf.Field {
	return []tdf.Field{tdf.FieldNamed("GID", tdf.IntegerValue(uint64(gameID)))}
}

func gamePlayerFields(instance *game.Instance, player *sporenet.User) []tdf.Field {
	slot, isSlotAssigned := instance.PlayerSlot(player.Account.ID)
	if !isSlotAssigned {
		slot = 0xffff
	}
	team, isTeamAssigned := instance.PlayerTeam(player.Account.ID)
	if !isTeamAssigned {
		team = 0xffff
	} else if instance.Info.Mode == game.ModeArena {
		team--
	}
	playerNetwork := game.NetworkPair{
		Internal: game.NetworkEndpoint{Port: gameplayPort(instance.Info.HostNetwork)},
	}
	return []tdf.Field{
		tdf.FieldNamed("BLOB", tdf.BinaryValue(nil)),
		tdf.FieldNamed("EXID", tdf.IntegerValue(0)),
		tdf.FieldNamed("GID", tdf.IntegerValue(uint64(instance.ID))),
		tdf.FieldNamed("LOC", tdf.IntegerValue(0x656e5553)),
		tdf.FieldNamed("NAME", tdf.StringValue(player.DisplayName)),
		tdf.FieldNamed("PATT", tdf.MapValue(tdf.String, tdf.String)),
		tdf.FieldNamed("PID", tdf.IntegerValue(uint64(player.Account.ID))),
		tdf.FieldNamed("PNET", tdf.UnionValue(2,
			tdf.FieldNamed("VALU", tdf.StructValue(networkPairFields(playerNetwork)...)),
		)),
		tdf.FieldNamed("SID", tdf.IntegerValue(uint64(slot))),
		tdf.FieldNamed("SLOT", tdf.IntegerValue(gameSlotTypePublicParticipant)),
		tdf.FieldNamed("STAT", tdf.IntegerValue(gamePlayerStateConnecting)),
		tdf.FieldNamed("TIDX", tdf.IntegerValue(uint64(team))),
		tdf.FieldNamed("TIME", tdf.IntegerValue(uint64(time.Now().Unix()))),
		tdf.FieldNamed("UGID", tdf.Value{Type: tdf.ObjectID}),
		tdf.FieldNamed("UID", tdf.IntegerValue(uint64(player.Account.ID))),
	}
}

func gameSetupFields(
	instance *game.Instance, user *sporenet.User, setupContext uint64,
	matchmakingSessionID uint64, setupPlaygroupID uint32,
	hostNetworkPair game.NetworkPair,
	playerSessionIDsByUser map[int64]uint32,
) ([]tdf.Field, error) {
	userSessionID := uint32(user.Account.ID)
	if playerSessionID, isFound := playerSessionIDsByUser[user.Account.ID]; isFound {
		userSessionID = playerSessionID
	}
	setupReason, err := gameSetupReason(
		instance, user, setupContext, matchmakingSessionID, userSessionID,
		setupPlaygroupID,
	)
	if err != nil {
		return nil, fmt.Errorf("setupReason: %w", err)
	}
	capacity := make([]tdf.Value, 0, len(instance.Info.Capacity))
	for _, value := range instance.Info.Capacity {
		capacity = append(capacity, tdf.IntegerValue(uint64(value)))
	}
	hostNetwork := tdf.Value{
		Type: tdf.List, ListType: tdf.Struct, IsStub: true,
		Items: []tdf.Value{tdf.StructValue(networkPairFields(hostNetworkPair)...)},
	}
	hostUserID := instance.HostUserID()
	if hostUserID == 0 {
		hostUserID = user.Account.ID
	}
	hostSlot, isHostSlotFound := instance.PlayerSlot(hostUserID)
	if !isHostSlotFound {
		hostSlot = 0
	}
	playerItems := make([]tdf.Value, 0)
	for _, player := range instance.Players() {
		playerSessionID := uint32(player.Account.ID)
		if matchedSessionID, isFound := playerSessionIDsByUser[player.Account.ID]; isFound {
			playerSessionID = matchedSessionID
		}
		playerFields := gamePlayerFields(instance, player)
		replaceField(
			playerFields, "UID", tdf.IntegerValue(uint64(playerSessionID)),
		)
		playerItems = append(playerItems, tdf.StructValue(playerFields...))
	}
	hostSessionID := uint32(hostUserID)
	if matchedSessionID, isFound := playerSessionIDsByUser[hostUserID]; isFound {
		hostSessionID = matchedSessionID
	}
	gameData := []tdf.Field{
		tdf.FieldNamed("ADMN", tdf.ListValue(tdf.Integer, tdf.IntegerValue(uint64(hostUserID)))),
		tdf.FieldNamed("ATTR", stringMapValue(instance.Info.Attributes)),
		tdf.FieldNamed("CAP", tdf.ListValue(tdf.Integer, capacity...)),
		tdf.FieldNamed("CRIT", tdf.MapValue(tdf.String, tdf.String)),
		tdf.FieldNamed("GID", tdf.IntegerValue(uint64(instance.ID))),
		tdf.FieldNamed("GNAM", tdf.StringValue(instance.Info.Name)),
		tdf.FieldNamed("GPVH", tdf.IntegerValue(1)),
		tdf.FieldNamed("GSET", tdf.IntegerValue(instance.Info.Settings)),
		tdf.FieldNamed("GSID", tdf.IntegerValue(1)),
		tdf.FieldNamed("GSTA", tdf.IntegerValue(uint64(instance.Info.State))),
		tdf.FieldNamed("GTYP", tdf.StringValue(instance.Info.Type)),
		tdf.FieldNamed("HNET", hostNetwork),
		tdf.FieldNamed("HSES", tdf.IntegerValue(uint64(hostSessionID))),
		tdf.FieldNamed("IGNO", tdf.IntegerValue(boolInteger(instance.Info.IsIgnored))),
		tdf.FieldNamed("MCAP", tdf.IntegerValue(uint64(instance.Info.MaxPlayers))),
		tdf.FieldNamed("NQOS", tdf.StructValue(
			tdf.FieldNamed("DBPS", tdf.IntegerValue(100)),
			tdf.FieldNamed("NATT", tdf.IntegerValue(0)),
			tdf.FieldNamed("UBPS", tdf.IntegerValue(100)),
		)),
		tdf.FieldNamed("NRES", tdf.IntegerValue(boolInteger(!instance.Info.IsResettable))),
		tdf.FieldNamed("NTOP", tdf.IntegerValue(uint64(instance.Info.NetworkTopology))),
		tdf.FieldNamed("PGID", tdf.StringValue(instance.Info.PlaygroupID)),
		tdf.FieldNamed("PGSR", tdf.BinaryValue(nil)),
		tdf.FieldNamed("PHST", tdf.StructValue(
			tdf.FieldNamed("HPID", tdf.IntegerValue(uint64(hostUserID))),
			tdf.FieldNamed("HSLT", tdf.IntegerValue(uint64(hostSlot))),
		)),
		tdf.FieldNamed("PRES", tdf.IntegerValue(uint64(instance.Info.PresenceMode))),
		tdf.FieldNamed("PSAS", tdf.StringValue("ams")),
		tdf.FieldNamed("QCAP", tdf.IntegerValue(uint64(instance.Info.QueueCapacity))),
		tdf.FieldNamed("SEED", tdf.IntegerValue(0)),
		tdf.FieldNamed("TCAP", tdf.IntegerValue(uint64(instance.Info.TeamCapacity))),
		tdf.FieldNamed("THST", tdf.StructValue(
			tdf.FieldNamed("HPID", tdf.IntegerValue(uint64(hostUserID))),
			tdf.FieldNamed("HSLT", tdf.IntegerValue(uint64(hostSlot))),
		)),
		tdf.FieldNamed("TIDS", gameTeamIDs(instance)),
		tdf.FieldNamed("UUID", tdf.StringValue(instance.Info.UUID)),
		tdf.FieldNamed("VOIP", tdf.IntegerValue(uint64(instance.Info.VoIPTopology))),
		tdf.FieldNamed("VSTR", tdf.StringValue(instance.Info.Version)),
		tdf.FieldNamed("XNNC", tdf.BinaryValue(nil)),
		tdf.FieldNamed("XSES", tdf.BinaryValue(nil)),
	}
	return []tdf.Field{
		tdf.FieldNamed("GAME", tdf.StructValue(gameData...)),
		tdf.FieldNamed("PROS", tdf.ListValue(tdf.Struct, playerItems...)),
		tdf.FieldNamed("REAS", setupReason),
	}, nil
}

func gameSetupReason(
	instance *game.Instance, user *sporenet.User, setupContext uint64,
	matchmakingSessionID uint64, userSessionID uint32, setupPlaygroupID uint32,
) (tdf.Value, error) {
	if setupContext == gameSetupContextMatchmaking {
		if instance == nil || user == nil || matchmakingSessionID == 0 ||
			userSessionID == 0 {
			return tdf.Value{}, errors.New("matchmaking setup unavailable")
		}
		matchmakingResult := matchmakingResultJoinedNewGame
		if user.Account.ID == instance.HostUserID() {
			matchmakingResult = matchmakingResultCreatedGame
		}
		return tdf.UnionValue(gameSetupReasonMatchmaking,
			tdf.FieldNamed("VALU", tdf.StructValue(
				tdf.FieldNamed("FIT", tdf.IntegerValue(0)),
				tdf.FieldNamed("MAXF", tdf.IntegerValue(0)),
				tdf.FieldNamed("MSID", tdf.IntegerValue(matchmakingSessionID)),
				tdf.FieldNamed("RSLT", tdf.IntegerValue(matchmakingResult)),
				tdf.FieldNamed("USID", tdf.IntegerValue(uint64(userSessionID))),
			)),
		), nil
	}
	if setupContext != gameSetupContextIndirectJoin {
		return tdf.UnionValue(gameSetupReasonDataless,
			tdf.FieldNamed("VALU", tdf.StructValue(
				tdf.FieldNamed("DCTX", tdf.IntegerValue(setupContext)),
			)),
		), nil
	}
	if instance == nil {
		return tdf.Value{}, errors.New("indirect playgroup unavailable")
	}
	playgroupID := uint64(setupPlaygroupID)
	if playgroupID == 0 && instance.Info.PlaygroupID == "" {
		return tdf.Value{}, errors.New("indirect playgroup unavailable")
	}
	if playgroupID == 0 {
		var err error
		playgroupID, err = strconv.ParseUint(instance.Info.PlaygroupID, 10, 64)
		if err != nil {
			return tdf.Value{}, fmt.Errorf("indirectPlaygroupID: %w", err)
		}
	}
	if playgroupID == 0 {
		return tdf.Value{}, errors.New("indirect playgroup invalid")
	}
	return tdf.UnionValue(gameSetupReasonIndirectJoin,
		tdf.FieldNamed("VALU", tdf.StructValue(
			tdf.FieldNamed("GRID", tdf.Value{
				Type: tdf.ObjectID,
				ObjectID: [3]uint64{
					uint64(PlaygroupsComponentID), playgroupObjectType, playgroupID,
				},
			}),
			tdf.FieldNamed("RPVC", tdf.IntegerValue(0)),
		)),
	), nil
}

func gameTeamIDs(instance *game.Instance) tdf.Value {
	if instance == nil || instance.Info.TeamCapacity == 0 {
		return tdf.ListValue(tdf.Integer, tdf.IntegerValue(0))
	}
	teams := make([]tdf.Value, 0, instance.Info.TeamCapacity)
	for team := uint16(0); team < instance.Info.TeamCapacity; team++ {
		teams = append(teams, tdf.IntegerValue(uint64(team)))
	}
	return tdf.ListValue(tdf.Integer, teams...)
}

func networkPairFields(pair game.NetworkPair) []tdf.Field {
	return []tdf.Field{
		tdf.FieldNamed("EXIP", tdf.StructValue(
			tdf.FieldNamed("IP", tdf.IntegerValue(uint64(pair.External.IP))),
			tdf.FieldNamed("PORT", tdf.IntegerValue(uint64(pair.External.Port))),
		)),
		tdf.FieldNamed("INIP", tdf.StructValue(
			tdf.FieldNamed("IP", tdf.IntegerValue(uint64(pair.Internal.IP))),
			tdf.FieldNamed("PORT", tdf.IntegerValue(uint64(pair.Internal.Port))),
		)),
	}
}

func gameplayPort(pair game.NetworkPair) uint16 {
	if pair.Internal.Port != 0 {
		return pair.Internal.Port
	}
	return pair.External.Port
}

func fieldValue(fields []tdf.Field, label string) tdf.Value {
	value, isFound := tdf.Find(fields, label)
	if !isFound {
		return tdf.Value{}
	}
	return value
}

func integerList(value tdf.Value) []uint32 {
	if value.Type != tdf.List {
		return nil
	}
	values := make([]uint32, 0, len(value.Items))
	for _, item := range value.Items {
		if item.Type == tdf.Integer {
			values = append(values, uint32(item.Integer))
		}
	}
	return values
}

func requestNetworkPair(value tdf.Value) game.NetworkPair {
	if value.Type != tdf.List || len(value.Items) == 0 {
		return game.NetworkPair{}
	}
	fields := value.Items[0].Fields
	pair := game.NetworkPair{
		External: endpointFromStruct(fieldValue(fields, "EXIP")),
		Internal: endpointFromStruct(fieldValue(fields, "INIP")),
	}
	if pair.External != (game.NetworkEndpoint{}) || pair.Internal != (game.NetworkEndpoint{}) {
		return pair
	}
	pair.Internal = endpointFromFields(value.Items[0].Fields)
	if len(value.Items) > 1 {
		pair.External = endpointFromFields(value.Items[1].Fields)
	}
	return pair
}

func endpointFromStruct(value tdf.Value) game.NetworkEndpoint {
	if value.Type != tdf.Struct {
		return game.NetworkEndpoint{}
	}
	return endpointFromFields(value.Fields)
}

func endpointFromFields(fields []tdf.Field) game.NetworkEndpoint {
	return game.NetworkEndpoint{
		IP: uint32(fieldInteger(fields, "IP")), Port: uint16(fieldInteger(fields, "PORT")),
	}
}

func stringMapValue(values map[string]string) tdf.Value {
	entries := make([]tdf.MapEntry, 0, len(values))
	for key, value := range values {
		entries = append(entries, tdf.MapEntry{Key: tdf.StringValue(key), Value: tdf.StringValue(value)})
	}
	return tdf.MapValue(tdf.String, tdf.String, entries...)
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
