// Package gameplay adapts zone operations to the build-103 RakNet gameplay
// protocol and owns connection-local presentation state.
package gameplay

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/chat"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/navigation"
	"github.com/darkspinnet/darkspin/server/playerstat"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/snapshot"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/squad"
	"github.com/darkspinnet/darkspin/server/util"
	zone "github.com/darkspinnet/darkspin/server/zone"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
	zonebarrier "github.com/darkspinnet/darkspin/server/zone/barrier"
	zoneboss "github.com/darkspinnet/darkspin/server/zone/boss"
	bossraknet "github.com/darkspinnet/darkspin/server/zone/boss/raknet103"
	zonecheckpoint "github.com/darkspinnet/darkspin/server/zone/checkpoint"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	zonecontent "github.com/darkspinnet/darkspin/server/zone/content"
	zonedeath "github.com/darkspinnet/darkspin/server/zone/death"
	zonedifficulty "github.com/darkspinnet/darkspin/server/zone/difficulty"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	zoneencounter "github.com/darkspinnet/darkspin/server/zone/encounter"
	zonehero "github.com/darkspinnet/darkspin/server/zone/hero"
	heroraknet "github.com/darkspinnet/darkspin/server/zone/hero/raknet103"
	zonehorde "github.com/darkspinnet/darkspin/server/zone/horde"
	zoneinteract "github.com/darkspinnet/darkspin/server/zone/interact"
	zoneloot "github.com/darkspinnet/darkspin/server/zone/loot"
	lootraknet "github.com/darkspinnet/darkspin/server/zone/loot/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	zoneobject "github.com/darkspinnet/darkspin/server/zone/object"
	objectraknet "github.com/darkspinnet/darkspin/server/zone/object/raknet103"
	zoneobjectid "github.com/darkspinnet/darkspin/server/zone/objectid"
	zoneobjective "github.com/darkspinnet/darkspin/server/zone/objective"
	zoneoutcome "github.com/darkspinnet/darkspin/server/zone/outcome"
	zonepopulation "github.com/darkspinnet/darkspin/server/zone/population"
	zonepreview "github.com/darkspinnet/darkspin/server/zone/preview"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
	zoneresult "github.com/darkspinnet/darkspin/server/zone/result"
	zonesecurity "github.com/darkspinnet/darkspin/server/zone/security"
	securityraknet "github.com/darkspinnet/darkspin/server/zone/security/raknet103"
	zonetimeline "github.com/darkspinnet/darkspin/server/zone/timeline"
	tutorialprogress "github.com/darkspinnet/darkspin/server/zone/tutorial/progression"
	zoneunlock "github.com/darkspinnet/darkspin/server/zone/unlock"
)

const tutorialStartingDNA = uint32(100)

type Programs = zonecontent.Programs
type zoneNounPhysics = zonecontent.NounPhysics
type attachedEffectPool = zoneeffect.AttachmentPool
type modifierPool = zoneeffect.ModifierPool

const attachedEffectSlotCount = zoneeffect.AttachmentSlotCount

func newAttachedEffectPool() *attachedEffectPool {
	return zoneeffect.NewAttachmentPool()
}

func newModifierPool() *modifierPool {
	return zoneeffect.NewModifierPool()
}

const campaignInteractableCompatibilityRange = float32(12)
const campaignPrepareFallbackDelay = zoneresult.VoteDuration

var initialChainSpawnPosition = raknet.Vector3{X: -123.8707, Y: -151.64705, Z: 10.037109}
var tutorialPlayerSpawnPosition = raknet.Vector3{
	X: 176.288162, Y: -239.948944, Z: -0.058266,
}

func campaignEntryPosition(director game.CampaignDirector, playerSlot uint16) raknet.Vector3 {
	if strings.EqualFold(director.Level, "zelems_1") {
		position := initialChainSpawnPosition
		switch playerSlot {
		case 1:
			position.X += 3
		case 2:
			position.Y += 3
		case 3:
			position.X -= 3
		}
		return position
	}
	index := int(playerSlot)
	if index < 0 || index >= len(director.EntryPositions) {
		return initialChainSpawnPosition
	}
	position := director.EntryPositions[index]
	if !isFiniteCampaignPopulationPosition(position) {
		return initialChainSpawnPosition
	}
	return raknet.Vector3{X: position.X, Y: position.Y, Z: position.Z}
}

func normalizeCampaignPickupCommand(command raknet.ActionCommandData) (raknet.ActionCommandData, bool) {
	switch command.Common.Type {
	case raknet.ActionUseInteractable:
		return command, false
	case raknet.ActionCatalystPickup:
		if command.Catalyst == nil || command.Catalyst.TargetObjectID == 0 {
			return command, false
		}
		command.Common.Type = raknet.ActionUseInteractable
		command.Value = command.Catalyst.TargetObjectID
		return command, true
	default:
		return command, false
	}
}

type gameplayMovementRuntime struct {
	campaign campaignMovementCommandRuntime
	logger   *log.Logger
}

func (r gameplayMovementRuntime) handle(
	ctx context.Context, packet raknet.Packet, command raknet.ActionCommandData,
	commandSession gameplayPeerSession, isSessionFound bool,
) ([][]byte, error) {
	if !isSessionFound {
		return nil, errors.New("movement session unavailable")
	}
	if commandSession.binding.Mode == game.ModeArena {
		return r.handleArena(packet, command, commandSession)
	}
	return r.campaign.handle(ctx, packet, command, commandSession)
}

func (r gameplayMovementRuntime) stop(
	ctx context.Context, packet raknet.Packet, command raknet.ActionCommandData,
	commandSession gameplayPeerSession, isSessionFound bool,
) ([][]byte, error) {
	if !isFiniteZonePosition(command.Common.Position) {
		return nil, errors.New("stop position invalid")
	}
	if isSessionFound {
		r.logger.Printf(
			"RakNet campaign stop received source=%d deployed=%d position=(%g,%g,%g)",
			command.Common.ObjectID, commandSession.deployedObjectID,
			command.Common.Position.X, command.Common.Position.Y,
			command.Common.Position.Z,
		)
	}
	movementCommand := command
	movementCommand.Common.Type = raknet.ActionMovement
	movementCommand.Movement = &raknet.ActionMovementData{
		GoalPosition: command.Common.Position,
		GoalFlags:    0x20,
	}
	response, err := r.handle(
		ctx, packet, movementCommand, commandSession, isSessionFound,
	)
	if err != nil {
		return nil, fmt.Errorf("stopPose: %w", err)
	}
	if len(response) < 2 || len(response[0]) == 0 || len(response[1]) == 0 ||
		response[0][0] != byte(raknet.ObjectPlayerMove) ||
		response[1][0] != byte(raknet.LocomotionUnreliable) {
		return response, nil
	}
	movePacket, err := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{
		ObjectID: command.Common.ObjectID, GoalFlags: 0x20,
		GoalPosition: command.Common.Position,
	})
	if err != nil {
		return nil, fmt.Errorf("stopMarshal: %w", err)
	}
	r.logger.Printf(
		"RakNet stop object=%d position=(%g,%g,%g)",
		command.Common.ObjectID, command.Common.Position.X,
		command.Common.Position.Y, command.Common.Position.Z,
	)
	return append([][]byte{movePacket}, response[2:]...), nil
}

type gameplaySimpleActionRuntime struct {
	registry *gameplaySessionRegistry
	action   campaignActionAuthority
	program  Programs
	now      func() time.Time
	logger   *log.Logger
}

type gameplayActionRuntime struct {
	registry    *gameplaySessionRegistry
	interaction campaignInteractionRuntime
	switcher    gameplaySwitchRuntime
	ability     campaignAbilityCommandRuntime
	movement    gameplayMovementRuntime
	simple      gameplaySimpleActionRuntime
	overdrive   campaignOverdriveRuntime
	now         func() time.Time
	logger      *log.Logger
}

func (r gameplayActionRuntime) handle(
	ctx context.Context, packet raknet.Packet,
) ([][]byte, error) {
	startedAt := r.now()
	command, err := raknet.DecodeActionCommand(packet.Payload)
	if err != nil {
		return nil, fmt.Errorf("actionDecode: %w", err)
	}
	r.registry.mutex.RLock()
	peerSession, isSessionFound := r.registry.sessions[packet.Address.String()]
	r.registry.mutex.RUnlock()
	inputLockRemaining := peerSession.heroInputLockRemaining(startedAt)
	if isSessionFound && inputLockRemaining > 0 &&
		command.Common.ObjectID == peerSession.heroInputLockedObjectID {
		r.logger.Printf(
			"RakNet campaign action quarantined during hero switch type=%d source=%d outgoing=%d remaining=%s",
			command.Common.Type, command.Common.ObjectID,
			peerSession.heroInputLockedObjectID, inputLockRemaining,
		)
		return nil, nil
	}
	key := gameplayActionLeaseKey{}
	var scheduleSet *gameplayActionScheduleSet
	if isSessionFound {
		memberKey := gameplaySessionMemberKey(peerSession)
		memberMutex, lockErr := r.registry.lockMember(ctx, memberKey)
		if lockErr != nil {
			return r.reject(packet, command, r.now().Sub(startedAt), lockErr)
		}
		defer memberMutex.Unlock()
		r.registry.mutex.RLock()
		currentSession, isCurrentFound :=
			r.registry.sessions[packet.Address.String()]
		isCurrent := isCurrentFound &&
			gameplaySessionMemberKey(currentSession) == memberKey &&
			currentSession.generation == peerSession.generation &&
			currentSession.transportGeneration == peerSession.transportGeneration
		r.registry.mutex.RUnlock()
		if !isCurrent {
			return r.reject(
				packet, command, r.now().Sub(startedAt),
				errors.New("action session replaced during admission"),
			)
		}
		peerSession = currentSession
		key = gameplayActionLeaseKey{
			sessionKey:          packet.Address.String(),
			memberKey:           memberKey,
			traceID:             packet.TraceID,
			zoneGeneration:      peerSession.generation,
			transportGeneration: peerSession.transportGeneration,
			actionGeneration:    r.registry.lifecycle.nextActionGeneration(),
			syncStamp:           command.Common.Unknown[0],
		}
		scheduleSet = &gameplayActionScheduleSet{}
		packet = r.protectScheduledResponses(packet, key, command, scheduleSet)
		r.registry.mutex.Lock()
		r.registry.actionAdmissions[key] = struct{}{}
		r.registry.mutex.Unlock()
		defer func() {
			r.registry.mutex.Lock()
			delete(r.registry.actionAdmissions, key)
			delete(r.registry.actionTerminals, key)
			r.registry.mutex.Unlock()
		}()
	}
	response, err := r.dispatch(ctx, packet, command)
	duration := r.now().Sub(startedAt)
	if err != nil {
		scheduleSet.cancelAll()
		if isSessionFound {
			return r.rejectRecovered(packet, command, duration, key, err)
		}
		return r.reject(packet, command, duration, err)
	}
	if isActionTerminalResponseRequired(command.Common.Type) &&
		!hasActionResponseForSync(response, command.Common.Unknown[0]) {
		scheduleSet.cancelAll()
		cause := errors.New("terminal response missing")
		if isSessionFound {
			return r.rejectRecovered(
				packet, command, duration, key, cause,
			)
		}
		return r.reject(
			packet, command, duration, cause,
		)
	}
	if isSessionFound && (command.Common.Type == raknet.ActionCancel ||
		(command.Common.Type == raknet.ActionSwitchCharacter &&
			!hasRejectedActionResponse(response))) {
		r.registry.mutex.Lock()
		r.registry.clearActionLeasesLocked(
			packet.Address.String(), peerSession.transportGeneration,
		)
		r.registry.mutex.Unlock()
	}
	if duration >= 250*time.Millisecond {
		r.logger.Printf(
			"RakNet slow action completed trace=%d remote=%s type=%d object=%d sync=%d duration=%s packets=%d",
			packet.TraceID, packet.Address, command.Common.Type, command.Common.ObjectID,
			command.Common.Unknown[0], duration, len(response),
		)
	}
	if !isSessionFound {
		return response, nil
	}
	isOriginallyRejected := hasRejectedActionResponse(response)
	protected, err := r.protectResponse(packet, command, key, response)
	isProtectedRejected := hasRejectedActionResponse(protected)
	if err != nil || isProtectedRejected {
		isScheduled := scheduleSet.cancelAll()
		if err != nil || !isOriginallyRejected || isScheduled {
			cause := err
			if cause == nil {
				cause = errors.New("accepted action protection rejected")
			}
			recoveryPackets := r.recoverRejectedActionAdmission(key, cause)
			protected = append(protected, recoveryPackets...)
		}
		return protected, err
	}
	scheduleSet.seal()
	isArenaPresentation := peerSession.binding.Mode == game.ModeArena
	if isArenaPresentation ||
		(command.Common.Type != raknet.ActionMovement &&
			command.Common.Type != raknet.ActionStopMovement) {
		peerPackets := gameplayPeerPresentationPackets(protected)
		recipients := r.registry.queuePeerPresentation(
			gameplayProducerIdentityFromSession(
				packet.Address.String(), peerSession, true,
			),
			protected,
		)
		if recipients != 0 {
			r.logger.Printf(
				"RakNet co-op action presentation fanned out game=%d source=%d type=%d packets=%d peers=%d",
				peerSession.binding.GameID, command.Common.ObjectID,
				command.Common.Type, len(peerPackets), recipients,
			)
		}
	}
	return protected, nil
}

func (r gameplayActionRuntime) rejectRecovered(
	request raknet.Packet, command raknet.ActionCommandData,
	duration time.Duration, key gameplayActionLeaseKey, cause error,
) ([][]byte, error) {
	recoveryPackets := r.recoverRejectedActionAdmission(key, cause)
	packets, err := r.reject(request, command, duration, cause)
	if err != nil {
		return nil, fmt.Errorf("recoveredReject: %w", err)
	}
	return append(packets, recoveryPackets...), nil
}

func hasRejectedActionResponse(packets [][]byte) bool {
	for _, packet := range packets {
		_, responseType, isFound := actionResponse(packet)
		if isFound && responseType == raknet.ActionResponseRejected {
			return true
		}
	}
	return false
}

func hasActionResponseForSync(packets [][]byte, syncStamp uint8) bool {
	for _, packet := range packets {
		responseSyncStamp, _, isFound := actionResponse(packet)
		if isFound && responseSyncStamp == syncStamp {
			return true
		}
	}
	return false
}

func (r gameplayActionRuntime) dispatch(
	ctx context.Context, packet raknet.Packet, command raknet.ActionCommandData,
) ([][]byte, error) {
	r.registry.mutex.RLock()
	commandSession, isSessionFound := r.registry.sessions[packet.Address.String()]
	isRejoinPending := isSessionFound && commandSession.isRejoinPending
	isTerminal := isSessionFound && (commandSession.isZoneTerminal() ||
		(commandSession.zone != nil && commandSession.zone.Boss() != nil &&
			commandSession.zone.Boss().IsBeamOutCommitted()))
	r.registry.mutex.RUnlock()
	if isRejoinPending {
		return nil, errors.New("rejoin baseline pending")
	}
	if isTerminal {
		return nil, errors.New("zone terminal")
	}
	if command.Common.Type == raknet.ActionDance {
		return r.simple.dance(packet, command)
	}
	if command.Common.Type == raknet.ActionCancel {
		return r.simple.cancel(
			packet, command, isSessionFound,
		)
	}
	if command.Common.Type == raknet.ActionMovement && command.Movement != nil {
		return r.movement.handle(
			ctx, packet, command, commandSession, isSessionFound,
		)
	}
	if command.Common.Type == raknet.ActionStopMovement {
		return r.movement.stop(
			ctx, packet, command, commandSession, isSessionFound,
		)
	}
	pickupCommand, isCatalystPickupCommand := normalizeCampaignPickupCommand(command)
	if isCatalystPickupCommand {
		command = pickupCommand
	}
	if command.Common.Type == raknet.ActionUseInteractable {
		sessionKey := packet.Address.String()
		pickupResponse, pickupErr := r.interaction.handlePickup(
			ctx, packet, command, sessionKey,
		)
		if !errors.Is(pickupErr, errCampaignPickupNotFound) {
			return pickupResponse, pickupErr
		}
		return r.interaction.handleScriptUse(
			ctx, packet, command, sessionKey, isCatalystPickupCommand,
		)
	}
	if command.Common.Type == raknet.ActionSwitchCharacter {
		return r.switcher.handle(
			packet, command, commandSession, isSessionFound,
		)
	}
	if command.Common.Type == raknet.ActionOverdrive {
		return r.overdrive.handle(packet, command)
	}
	if isSessionFound {
		return r.ability.handle(ctx, packet, command)
	}
	return nil, errors.New("session unavailable")
}

func (r gameplayActionRuntime) reject(
	request raknet.Packet, command raknet.ActionCommandData,
	duration time.Duration, cause error,
) ([][]byte, error) {
	if command.Common.Type == raknet.ActionMovement ||
		command.Common.Type == raknet.ActionStopMovement {
		r.registry.mutex.Lock()
		peerSession, isFound := r.registry.sessions[request.Address.String()]
		if isFound {
			stopErr := peerSession.stopPlayerMovement(r.now())
			if stopErr != nil {
				r.logger.Printf(
					"RakNet rejected movement state cleanup skipped remote=%s: %v",
					request.Address, stopErr,
				)
			}
			r.registry.sessions[request.Address.String()] = peerSession
		}
		r.registry.mutex.Unlock()
		if isFound && peerSession.deployedObjectID != 0 {
			packets, err := marshalZonePlayerStop(
				peerSession.deployedObjectID, peerSession.playerPosition,
			)
			if err != nil {
				return nil, fmt.Errorf("movementCorrection: %w", err)
			}
			r.logger.Printf(
				"RakNet movement corrected object=%d duration=%s error=%v",
				command.Common.ObjectID, duration, cause,
			)
			return packets, nil
		}
	}
	if command.Common.ObjectID == 0 {
		return nil, fmt.Errorf("actionRejectUnavailable: %w", cause)
	}
	packet, err := actionraknet.Reject(command)
	if err != nil {
		return nil, fmt.Errorf("actionReject: %w", err)
	}
	r.logger.Printf(
		"RakNet action safely rejected type=%d object=%d sync=%d duration=%s error=%v",
		command.Common.Type, command.Common.ObjectID, command.Common.Unknown[0],
		duration, cause,
	)
	return [][]byte{packet}, nil
}

func isActionTerminalResponseRequired(command raknet.ActionCommand) bool {
	switch command {
	case raknet.ActionMovement, raknet.ActionStopMovement,
		raknet.ActionSwitchCharacter, raknet.ActionCancel, raknet.ActionDance:
		return false
	default:
		return true
	}
}

func (r gameplaySimpleActionRuntime) dance(
	packet raknet.Packet, command raknet.ActionCommandData,
) ([][]byte, error) {
	danceNow := r.now()
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[packet.Address.String()]
	isAccepted := isFound && peerSession.stage.IsDungeon() &&
		command.Common.ObjectID == peerSession.deployedObjectID &&
		isFiniteZonePosition(command.Common.Position) &&
		peerSession.isAbilityReleaseReady(danceNow) &&
		peerSession.basicAttack == nil && peerSession.deployedHitPoint() > 0
	danceAnimation := ""
	if isAccepted &&
		peerSession.deployedCreatureIndex < uint32(len(peerSession.binding.Creatures)) {
		creature := peerSession.binding.Creatures[peerSession.deployedCreatureIndex]
		noun, isNounFound := r.program.NounPhysicsByID[creature.Noun]
		danceAnimation = noun.DanceAnimation
		isAccepted = isNounFound && danceAnimation != ""
	} else {
		isAccepted = false
	}
	var err error
	if isAccepted {
		err = peerSession.advancePlayerPosition(danceNow, command.Common.Position)
		if err == nil {
			peerSession.isDancing = true
			r.registry.sessions[packet.Address.String()] = peerSession
		}
	}
	r.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("dancePosition: %w", err)
	}
	if !isAccepted {
		r.logger.Printf("RakNet dance rejected object=%d", command.Common.ObjectID)
		return nil, nil
	}
	dancePacket, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
		ObjectID: command.Common.ObjectID, State: util.HashID(danceAnimation),
		Timestamp: packet.SourceTime, Scale: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("danceMarshal: %w", err)
	}
	r.logger.Printf(
		"RakNet dance accepted object=%d animation=%s",
		command.Common.ObjectID, danceAnimation,
	)
	return [][]byte{dancePacket}, nil
}

func (r gameplaySimpleActionRuntime) cancel(
	packet raknet.Packet, command raknet.ActionCommandData,
	isSessionFound bool,
) ([][]byte, error) {
	if isSessionFound {
		pursuit, interruptedBasic, heroDrain, isCanceled := r.action.cancel(
			packet.Address.String(), command.Common.ObjectID,
		)
		if !isCanceled {
			return nil, nil
		}
		if interruptedBasic != nil {
			interruptedBasic.Stop()
		}
		if heroDrain != nil {
			packets, err := heroDrain.interruptionPacketsAt(packet.SourceTime)
			heroDrain.Stop()
			if err != nil {
				return nil, fmt.Errorf("cancelHeroDrain: %w", err)
			}
			return packets, nil
		}
		if pursuit.IsActive {
			r.logger.Printf(
				"RakNet campaign cancel replaces pursuit source=%d target=%d ability=%d pursuit_sync=%d cancel_sync=%d",
				command.Common.ObjectID, pursuit.TargetObjectID, pursuit.AbilityIndex,
				pursuit.SyncStamp, command.Common.Unknown[0],
			)
		}
		return nil, nil
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[packet.Address.String()]
	if isFound && command.Common.ObjectID == peerSession.deployedObjectID {
		peerSession.basicSequenceSession().ReleaseHeld()
		r.registry.sessions[packet.Address.String()] = peerSession
	}
	r.registry.mutex.Unlock()
	return nil, nil
}

type campaignCloudLobRun struct {
	cancel raknet.CancelSchedule
}

type basicHeldContextKey struct{}

type Progression interface {
	GrantPartOnce(context.Context, int64, sporenet.Part) (bool, error)
	DropPartTo(context.Context, int64, uint64, sporenet.PartDropTarget) (bool, error)
	tutorialprogress.AwardStore
	tutorialprogress.RestartStore
	tutorialprogress.CompletionStore
	RecordPlayerStats(context.Context, int64, sporenet.PlayerStatDelta) error
	GrantAccountExperience(context.Context, int64, uint32) (sporenet.TutorialExperience, error)
	GrantCampaignExperience(context.Context, int64, uint64, uint32) (sporenet.TutorialExperience, error)
}

type campaignChainProgression interface {
	AdvanceChainProgression(context.Context, int64, uint32) (uint32, bool, error)
}

type campaignCashOutProgression interface {
	CampaignCashOutBonusStatus(
		context.Context, int64,
	) (sporenet.CampaignCashOutBonus, error)
	CommitCampaignCashOut(
		context.Context, int64, sporenet.CampaignCashOutReward,
	) (sporenet.CampaignCashOutReceipt, error)
}

type campaignOverdriveProgression interface {
	UnlockOverdrive(context.Context, int64) (bool, error)
}

type NavigationSource interface {
	LoadCampaignNavigation(context.Context, string) (*navigation.Mesh, error)
}

type gameplayHandlerDependencies struct {
	campaignSetup         *game.CampaignSetup
	campaignNavigation    NavigationSource
	publishCampaignEvent  func(game.CampaignDirectorPublication)
	registerCleanup       func(func())
	registerDisconnect    func(func(string, uint64))
	registerDiscard       func(func(uint32))
	registerDiscardMember func(func(uint32, uint64))
	registerPoll          func(raknet.PollHandler)
	registerBugContext    func(chat.BugContextProvider)
	registerHint          func(chat.HintProvider)
	registerLocation      func(chat.LocationProvider)
	registerSyncSnapshot  func(snapshot.StateProvider)
	zoneTimer             zone.Timer
	checkpoint            zonecheckpoint.Repository
	now                   func() time.Time
}

type Lifecycle struct {
	cleanup           func()
	disconnectAddress func(string, uint64)
	discardGame       func(uint32)
	discardMember     func(uint32, uint64)
	poll              raknet.PollHandler
	bugContext        chat.BugContextProvider
	hintProvider      chat.HintProvider
	locationProvider  chat.LocationProvider
	syncSnapshot      snapshot.StateProvider
}

type gameplayPacketHandler struct {
	registry   *gameplaySessionRegistry
	join       gameplayJoinRuntime
	setup      gameplaySetupRuntime
	inventory  gameplayInventoryRuntime
	result     campaignResultRuntime
	arena      gameplayArenaRuntime
	action     gameplayActionRuntime
	status     gameplayStatusRuntime
	pending    gameplayPendingRuntime
	projection gameplayProjectionRuntime
	crystal    gameplayCrystalRuntime
	logger     *log.Logger
}

type gameplayCrystalRuntime struct {
	registry *gameplaySessionRegistry
	logger   *log.Logger
}

func (r gameplayCrystalRuntime) handle(packet raknet.Packet) ([][]byte, error) {
	command, err := raknet.DecodeCrystalDrag(packet.Payload)
	if err != nil {
		return r.result(command, false)
	}
	sessionKey := packet.Address.String()
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || peerSession.zone == nil || peerSession.isZoneTerminal() {
		r.registry.mutex.Unlock()
		return r.result(command, false)
	}
	inventory, isInventoryFound := peerSession.zone.CrystalInventory(
		peerSession.binding.UserID, peerSession.generation,
	)
	if !isInventoryFound {
		r.registry.mutex.Unlock()
		return r.result(command, false)
	}
	slotCount := int(peerSession.binding.CatalystSlotCount)
	if command.Operation == raknet.CrystalDragIntoSlot {
		isMoved := inventory.Move(int(command.SourceSlot), int(command.DestinationSlot), slotCount)
		if isMoved {
			err = peerSession.zone.SetCrystalInventory(
				peerSession.binding.UserID, peerSession.generation, inventory,
			)
			if err == nil {
				peerSession.setCrystalInventory(inventory)
				r.registry.sessions[sessionKey] = peerSession
			}
		}
		r.registry.mutex.Unlock()
		if err != nil {
			return nil, fmt.Errorf("crystalMoveStore: %w", err)
		}
		if !isMoved {
			return r.result(command, false)
		}
		resultPackets, resultErr := r.acceptedResult(command, peerSession)
		if resultErr != nil {
			return nil, fmt.Errorf("crystalMoveResult: %w", resultErr)
		}
		return resultPackets, nil
	}
	slot, isRemoved := inventory.Remove(int(command.SourceSlot), slotCount)
	if !isRemoved || slot.NounName == "" {
		r.registry.mutex.Unlock()
		return r.result(command, false)
	}
	source := sim.Position{
		X: peerSession.playerPosition.X, Y: peerSession.playerPosition.Y,
		Z: peerSession.playerPosition.Z,
	}
	destination := source
	if command.Operation == raknet.CrystalDragIntoWorldAtPoint {
		destination = sim.Position{
			X: command.WorldPosition.X, Y: command.WorldPosition.Y, Z: command.WorldPosition.Z,
		}
	} else {
		destination.X++
	}
	lob, err := sim.BuildDropLob(time.Duration(packet.SourceTime)*time.Millisecond, source, destination)
	if err != nil {
		inventory.Restore(int(command.SourceSlot), slot)
		r.registry.mutex.Unlock()
		return r.result(command, false)
	}
	objectID, err := peerSession.reserveCampaignObjectID()
	if err != nil {
		inventory.Restore(int(command.SourceSlot), slot)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("crystalDropObject: %w", err)
	}
	request := sim.CrystalPickupRequest{
		Role: sim.Role(fmt.Sprintf("crystalTrade.%d", objectID)), NounName: slot.NounName,
		CrystalType: slot.CrystalType, CrystalLevel: slot.CrystalLevel, Rarity: slot.Rarity,
		Position: source, Destination: destination, Lob: lob,
	}
	worldPackets, err := lootraknet.MarshalCrystalDrop(request, objectID)
	if err == nil {
		err = peerSession.registerCampaignPickup(
			zoneinteract.PickupCrystal, objectID, source, destination,
		)
	}
	if err == nil {
		err = peerSession.zone.PickupPayload().AddCrystal(zoneinteract.CrystalPickup{
			ObjectID: objectID, Request: request,
			Object: sim.CrystalPickupObject{
				Role: request.Role, NounName: request.NounName, NounAsset: slot.NounAsset,
				CrystalType: slot.CrystalType, CrystalLevel: slot.CrystalLevel,
				Rarity: slot.Rarity, Position: destination,
				IsLive: true, IsPhaseOwned: true, IsLootDataPresent: true,
			},
		})
	}
	if err == nil {
		err = peerSession.zone.SetCrystalInventory(
			peerSession.binding.UserID, peerSession.generation, inventory,
		)
	}
	if err != nil {
		peerSession.zone.Pickups().Remove(objectID)
		peerSession.zone.PickupPayload().RemoveCrystal(objectID)
		inventory.Restore(int(command.SourceSlot), slot)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("crystalDropCommit: %w", err)
	}
	peerSession.setCrystalInventory(inventory)
	for candidateKey, candidate := range r.registry.sessions {
		if candidateKey == sessionKey || candidate.zone != peerSession.zone {
			continue
		}
		publishErr := candidate.publishPackets(worldPackets)
		if publishErr != nil && r.logger != nil {
			r.logger.Printf(
				"RakNet campaign crystal drop peer delivery queued game=%d user=%d object=%d: %v",
				candidate.binding.GameID, candidate.binding.UserID,
				objectID, publishErr,
			)
		}
		r.registry.sessions[candidateKey] = candidate
	}
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	resultPackets, err := r.acceptedResult(command, peerSession)
	if err != nil {
		return nil, fmt.Errorf("crystalDropResult: %w", err)
	}
	return append(resultPackets, worldPackets...), nil
}

func (r gameplayCrystalRuntime) acceptedResult(
	command raknet.CrystalDragCommand, peerSession gameplayPeerSession,
) ([][]byte, error) {
	statePackets, err := marshalGameplayCrystalState(peerSession)
	if err != nil {
		return nil, fmt.Errorf("crystalBonusMarshal: %w", err)
	}
	resultPackets, err := r.result(command, true)
	if err != nil {
		return nil, fmt.Errorf("crystalAcceptedResult: %w", err)
	}
	// A drag response changes the HUD, but does not refresh the player-owned
	// crystal records used by subsequent tooltips and drag admission.
	return append(resultPackets, statePackets...), nil
}

func (r gameplayCrystalRuntime) result(
	command raknet.CrystalDragCommand, isAccepted bool,
) ([][]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.CrystalMoveResultMessage{
		IsAccepted: isAccepted, SourceSlot: command.SourceSlot,
		DestinationSlot: command.DestinationSlot,
	})
	if err != nil {
		return nil, fmt.Errorf("crystalMoveMarshal: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e Lifecycle) Cleanup() {
	e.cleanup()
}

func (e Lifecycle) DisconnectTransport(address string, generation uint64) {
	e.disconnectAddress(address, generation)
}

func (e Lifecycle) DiscardGame(gameID uint32) {
	e.discardGame(gameID)
}

func (e Lifecycle) DiscardMember(gameID uint32, userID uint64) {
	e.discardMember(gameID, userID)
}

func (e Lifecycle) Poll(
	ctx context.Context, packet raknet.Packet,
) ([][]byte, error) {
	return e.poll(ctx, packet)
}

func (e Lifecycle) BugContext(
	ctx context.Context, req chat.BugContextRequest,
) (chat.BugContext, error) {
	if e.bugContext == nil {
		return chat.BugContext{
			CapturedAt: time.Now().UTC(), UserID: req.Sender.ID,
			UserName: req.Sender.Name, GameID: req.GameID,
		}, nil
	}
	return e.bugContext.BugContext(ctx, req)
}

// Hint returns the nearest living hostile from authoritative gameplay state.
func (e Lifecycle) Hint(
	ctx context.Context, req chat.HintRequest,
) (chat.HintResult, error) {
	if e.hintProvider == nil {
		return chat.HintResult{}, chat.ErrHintUnavailable
	}
	result, err := e.hintProvider.Hint(ctx, req)
	if err != nil {
		return chat.HintResult{}, fmt.Errorf("hintGameplay: %w", err)
	}
	return result, nil
}

// Location returns the authoritative position of the player's deployed hero.
func (e Lifecycle) Location(
	ctx context.Context, req chat.LocationRequest,
) (chat.LocationResult, error) {
	if e.locationProvider == nil {
		return chat.LocationResult{}, chat.ErrLocationUnavailable
	}
	result, err := e.locationProvider.Location(ctx, req)
	if err != nil {
		return chat.LocationResult{}, fmt.Errorf("locationGameplay: %w", err)
	}
	return result, nil
}

// SyncSnapshot returns a high-fidelity authoritative gameplay keyframe.
func (e Lifecycle) SyncSnapshot(
	ctx context.Context, req snapshot.StateRequest,
) (snapshot.StateFrame, error) {
	if e.syncSnapshot == nil {
		return snapshot.StateFrame{
			CapturedAt: time.Now().UTC(), Actor: req.Actor,
		}, nil
	}
	return e.syncSnapshot.SyncSnapshot(ctx, req)
}

func isIncompleteClientPacket(id raknet.PacketID) bool {
	return false
}

func logIncompleteClientPacket(logger *log.Logger, packet raknet.Packet) {
	if !isIncompleteClientPacket(packet.ID) {
		return
	}
	logger.Printf(
		"RakNet incomplete client packet from %s id=%#x length=%d source_time=%d client_time=%d payload=%x",
		packet.Address, packet.ID, len(packet.Payload), packet.SourceTime, packet.ClientTime, packet.Payload,
	)
}

func defaultGameplayHandlerDependencies() gameplayHandlerDependencies {
	return gameplayHandlerDependencies{}
}

func NewHandler(
	gameplayJoin *game.GameplayJoin, progression Progression,
	program Programs, logger *log.Logger, campaignSetup *game.CampaignSetup,
	campaignNavigation NavigationSource, timer zone.Timer,
	checkpoint zonecheckpoint.Repository,
) (raknet.Handler, Lifecycle) {
	lifecycle := Lifecycle{}
	dependency := defaultGameplayHandlerDependencies()
	dependency.campaignSetup = campaignSetup
	dependency.campaignNavigation = campaignNavigation
	dependency.zoneTimer = timer
	dependency.checkpoint = checkpoint
	dependency.registerCleanup = func(registered func()) {
		lifecycle.cleanup = registered
	}
	dependency.registerDisconnect = func(registered func(string, uint64)) {
		lifecycle.disconnectAddress = registered
	}
	dependency.registerDiscard = func(registered func(uint32)) {
		lifecycle.discardGame = registered
	}
	dependency.registerDiscardMember = func(registered func(uint32, uint64)) {
		lifecycle.discardMember = registered
	}
	dependency.registerPoll = func(registered raknet.PollHandler) {
		lifecycle.poll = registered
	}
	dependency.registerBugContext = func(registered chat.BugContextProvider) {
		lifecycle.bugContext = registered
	}
	dependency.registerHint = func(registered chat.HintProvider) {
		lifecycle.hintProvider = registered
	}
	dependency.registerLocation = func(registered chat.LocationProvider) {
		lifecycle.locationProvider = registered
	}
	dependency.registerSyncSnapshot = func(registered snapshot.StateProvider) {
		lifecycle.syncSnapshot = registered
	}
	handler := newGameplayHandlerWithDependencies(
		gameplayJoin, progression, program, logger, dependency,
	)
	if lifecycle.cleanup == nil {
		lifecycle.cleanup = func() {}
	}
	if lifecycle.disconnectAddress == nil {
		lifecycle.disconnectAddress = func(string, uint64) {}
	}
	if lifecycle.discardGame == nil {
		lifecycle.discardGame = func(uint32) {}
	}
	if lifecycle.discardMember == nil {
		lifecycle.discardMember = func(uint32, uint64) {}
	}
	if lifecycle.poll == nil {
		lifecycle.poll = func(context.Context, raknet.Packet) ([][]byte, error) { return nil, nil }
	}
	return handler, lifecycle
}

func newGameplayHandlerWithDependencies(
	gameplayJoin *game.GameplayJoin, progression Progression,
	program Programs, logger *log.Logger, dependency gameplayHandlerDependencies,
) raknet.Handler {
	if dependency.now == nil {
		dependency.now = time.Now
	}
	statsRecorder := playerstat.NewRecorder(progression)
	sessionRegistry := newGameplaySessionRegistry(
		dependency.zoneTimer, dependency.now, logger,
	)
	if dependency.registerBugContext != nil {
		dependency.registerBugContext(sessionRegistry)
	}
	if dependency.registerHint != nil {
		dependency.registerHint(sessionRegistry)
	}
	if dependency.registerLocation != nil {
		dependency.registerLocation(sessionRegistry)
	}
	if dependency.registerSyncSnapshot != nil {
		dependency.registerSyncSnapshot(sessionRegistry)
	}
	zoneRegistry := zone.NewRegistry()
	modifierInstancePool := newModifierPool()
	attachedEffectPool := newAttachedEffectPool()
	lootProgression, isLootProgression := progression.(campaignLootProgression)
	if !isLootProgression {
		lootProgression = nil
	}
	dnaProgression, isDNAProgression := progression.(zoneloot.DNAGranter)
	if !isDNAProgression {
		dnaProgression = nil
	}
	chainProgression, isChainProgression := progression.(campaignChainProgression)
	if !isChainProgression {
		chainProgression = nil
	}
	cashOutProgression, isCashOutProgression := progression.(campaignCashOutProgression)
	if !isCashOutProgression {
		cashOutProgression = nil
	}
	overdriveProgression, isOverdriveProgression := progression.(campaignOverdriveProgression)
	if !isOverdriveProgression {
		overdriveProgression = nil
	}
	if dependency.registerCleanup != nil {
		dependency.registerCleanup(func() {
			sessionRegistry.lifecycle.cleanup(
				modifierInstancePool, attachedEffectPool,
			)
		})
	}
	if dependency.registerDisconnect != nil {
		dependency.registerDisconnect(func(sessionKey string, generation uint64) {
			sessionRegistry.lifecycle.disconnect(
				sessionKey, generation, modifierInstancePool, attachedEffectPool,
			)
		})
	}
	campaignPrepare := campaignPreparation{
		setup: dependency.campaignSetup, difficulty: program.Difficulty, session: sessionRegistry,
		zone:       zoneRegistry,
		navigation: dependency.campaignNavigation, program: program,
		timer:        dependency.zoneTimer,
		checkpoint:   dependency.checkpoint,
		gameplayJoin: gameplayJoin,
		logger:       logger,
	}
	interactionRuntime := campaignInteractionRuntime{
		registry: sessionRegistry, progression: lootProgression,
		crystalPickup: program.CrystalPickup, logger: logger,
		gameplayJoin: gameplayJoin, now: dependency.now,
		interactWithObelisk:   program.InteractWithObelisk,
		interactHealthObelisk: program.InteractHealthObelisk,
		crystalDefinitions:    program.CrystalDefinitions,
		crystalLevelOffsets:   program.CrystalLevelOffsets,
	}
	enemyPursuit := campaignNPCPursuitRuntime{
		registry: sessionRegistry, logger: logger, now: dependency.now,
	}
	projectionRuntime := gameplayProjectionRuntime{
		registry: sessionRegistry, logger: logger,
	}
	npcAction := campaignNPCActionRuntime{
		registry:   sessionRegistry,
		projectile: campaignNPCProjectileAuthority{registry: sessionRegistry},
		pursuit:    enemyPursuit, program: program,
		logger: logger, stats: statsRecorder, modifierPool: modifierInstancePool,
		now: dependency.now, gameplayJoin: gameplayJoin,
		timer:      dependency.zoneTimer,
		projection: projectionRuntime, effectPool: attachedEffectPool,
		death: campaignDeathRuntime{
			timer: dependency.zoneTimer, logger: logger,
		},
	}
	populationRuntime := campaignPopulationRuntime{
		registry: sessionRegistry, npc: npcAction, logger: logger,
	}
	setupRuntime := gameplaySetupRuntime{
		registry: sessionRegistry, population: populationRuntime,
		preparation: campaignPrepare, program: program,
		modifierPool: modifierInstancePool,
		now:          dependency.now, logger: logger,
	}
	encounterRuntime := campaignEncounterRuntime{
		supportUnlock: program.SupportUnlock, progression: dnaProgression, logger: logger,
		registry: sessionRegistry, npc: npcAction, timer: dependency.zoneTimer,
		program: program, modifierPool: modifierInstancePool,
	}
	securityRuntime := securityraknet.NewTransferRuntime(
		campaignSecurityTransferAuthority{registry: sessionRegistry},
		dependency.now,
		logger,
	)
	damageRuntime := campaignDamageRuntime{
		registry: sessionRegistry, npc: npcAction,
		projection: projectionRuntime, effectPool: attachedEffectPool, logger: logger,
		gameplayJoin: gameplayJoin,
	}
	encounterRuntime.damage = damageRuntime
	setupRuntime.damage = damageRuntime
	passiveRuntime := fireTempestPassiveRuntime{
		registry: sessionRegistry, damage: damageRuntime,
		effectPool: attachedEffectPool, program: program, now: dependency.now,
	}
	setupRuntime.passive = passiveRuntime
	energyPassiveRuntime := energySentinelPassiveRuntime{
		registry: sessionRegistry, modifierPool: modifierInstancePool,
		now: dependency.now,
	}
	setupRuntime.energyPassive = energyPassiveRuntime
	campaignMovement := campaignMovementCommandRuntime{
		registry: sessionRegistry, action: campaignActionAuthority{registry: sessionRegistry},
		program: program, encounter: encounterRuntime,
		security: securityRuntime, npc: npcAction,
		damage: damageRuntime, modifierPool: modifierInstancePool,
		now: dependency.now, logger: logger,
		publishEvent: dependency.publishCampaignEvent,
	}
	resultRuntime := campaignResultRuntime{
		registry: sessionRegistry, chain: chainProgression,
		cashOutProgression: cashOutProgression, gameplayJoin: gameplayJoin,
		modifierPool: modifierInstancePool, effectPool: attachedEffectPool,
		preparation: campaignPrepare,
		logger:      logger,
	}
	pendingRuntime := gameplayPendingRuntime{
		registry: sessionRegistry, gameplayJoin: gameplayJoin,
		stats: statsRecorder, damage: damageRuntime, modifierPool: modifierInstancePool,
		effectPool: attachedEffectPool,
		projection: projectionRuntime, result: resultRuntime, setup: setupRuntime,
		overdriveProgression: overdriveProgression,
		now:                  dependency.now, logger: logger,
	}
	joinRuntime := gameplayJoinRuntime{
		registry: sessionRegistry, gameplayJoin: gameplayJoin, progression: progression,
		modifierPool: modifierInstancePool, effectPool: attachedEffectPool,
		logger: logger,
	}
	inventoryRuntime := gameplayInventoryRuntime{
		registry: sessionRegistry, progression: progression, logger: logger,
	}
	statusRuntime := gameplayStatusRuntime{
		registry: sessionRegistry, gameplayJoin: gameplayJoin,
		progression:  progression,
		modifierPool: modifierInstancePool, effectPool: attachedEffectPool,
		preparation: campaignPrepare, setup: setupRuntime,
		chainLevels: program.ChainLevel, logger: logger,
	}
	simpleActionRuntime := gameplaySimpleActionRuntime{
		registry: sessionRegistry, action: campaignActionAuthority{registry: sessionRegistry},
		program: program, now: dependency.now, logger: logger,
	}
	switchRuntime := gameplaySwitchRuntime{
		registry: sessionRegistry, program: program, npc: npcAction,
		damage:       damageRuntime,
		modifierPool: modifierInstancePool, effectPool: attachedEffectPool,
		passive: passiveRuntime, energyPassive: energyPassiveRuntime,
		now: dependency.now, logger: logger,
	}
	movementRuntime := gameplayMovementRuntime{
		campaign: campaignMovement, logger: logger,
	}
	campaignAbility := campaignAbilityCommandRuntime{
		registry: sessionRegistry, action: campaignActionAuthority{registry: sessionRegistry},
		program:      program,
		modifierPool: modifierInstancePool, effectPool: attachedEffectPool,
		npc: npcAction, damage: damageRuntime, stats: statsRecorder,
		now: dependency.now, logger: logger,
	}
	actionRuntime := gameplayActionRuntime{
		registry: sessionRegistry, interaction: interactionRuntime,
		switcher: switchRuntime, ability: campaignAbility,
		movement: movementRuntime, simple: simpleActionRuntime,
		overdrive: campaignOverdriveRuntime{
			registry: sessionRegistry, now: dependency.now, logger: logger,
		},
		now: dependency.now, logger: logger,
	}
	if dependency.registerPoll != nil {
		dependency.registerPoll(pendingRuntime.poll)
	}
	if dependency.registerDiscard != nil {
		dependency.registerDiscard(func(gameID uint32) {
			sessionRegistry.lifecycle.discardGame(
				gameID, modifierInstancePool, attachedEffectPool,
			)
			zoneRegistry.Discard(uint64(gameID))
		})
	}
	if dependency.registerDiscardMember != nil {
		dependency.registerDiscardMember(func(gameID uint32, userID uint64) {
			sessionRegistry.lifecycle.discardMember(
				gameID, userID, modifierInstancePool, attachedEffectPool,
			)
		})
	}
	handler := gameplayPacketHandler{
		registry: sessionRegistry, join: joinRuntime, setup: setupRuntime,
		inventory: inventoryRuntime, result: resultRuntime,
		arena: gameplayArenaRuntime{
			registry: sessionRegistry, logger: logger, gameplayJoin: gameplayJoin,
		}, action: actionRuntime,
		status: statusRuntime, pending: pendingRuntime,
		projection: projectionRuntime,
		crystal:    gameplayCrystalRuntime{registry: sessionRegistry, logger: logger},
		logger:     logger,
	}
	return handler.handle
}

func (e gameplayPacketHandler) handle(
	ctx context.Context, packet raknet.Packet,
) ([][]byte, error) {
	if !e.registry.beginRequest() {
		return nil, errors.New("gameplay registry closed")
	}
	defer e.registry.endRequest()
	contextErr := ctx.Err()
	if contextErr != nil {
		e.pending.retireCanceledTransport(packet)
		return nil, fmt.Errorf("requestContext: %w", contextErr)
	}
	response, err := e.dispatch(ctx, packet)
	contextErr = ctx.Err()
	if contextErr != nil {
		e.pending.retireCanceledTransport(packet)
		return nil, fmt.Errorf("dispatchContext: %w", contextErr)
	}
	if err != nil {
		return nil, fmt.Errorf("packetDispatch: %w", err)
	}
	recovered, recoveryErr := e.pending.recoverNPCs(packet)
	if recoveryErr != nil {
		e.logger.Printf(
			"RakNet NPC recovery deferred for %s: %v",
			packet.Address, recoveryErr,
		)
	}
	response = append(response, recovered...)
	contextErr = ctx.Err()
	if contextErr != nil {
		e.pending.retireCanceledTransport(packet)
		return nil, fmt.Errorf("recoveryContext: %w", contextErr)
	}
	projected, err := e.projection.drain(
		packet, packet.Address.String(), packet.SourceTime,
	)
	if err != nil {
		e.logger.Printf(
			"RakNet campaign projection quarantined for %s: %v",
			packet.Address, err,
		)
	}
	return append(response, projected...), nil
}

func (e gameplayPacketHandler) dispatch(
	ctx context.Context, packet raknet.Packet,
) ([][]byte, error) {
	logIncompleteClientPacket(e.logger, packet)
	switch packet.ID {
	case raknet.HelloPlayerRequest:
		return e.join.handle(ctx, packet)
	case raknet.DebugPing:
		return e.setup.handle(ctx, packet)
	case raknet.LootDropMessage:
		return e.inventory.drop(ctx, packet)
	case raknet.ChainPlayerMsgs:
		return e.result.handle(ctx, packet)
	case raknet.ArenaPlayerMsgs:
		return e.arena.handle(packet)
	case raknet.ActionCommandMsgs:
		return e.action.handle(ctx, packet)
	case raknet.CrystalDragMessage:
		return e.crystal.handle(packet)
	case raknet.PlayerStatusUpdate:
		return e.status.handle(ctx, packet)
	default:
		e.logIgnored(packet)
		return nil, nil
	}
}

func (e gameplayPacketHandler) logIgnored(packet raknet.Packet) {
	packetName := "unknown"
	contract, isFound := raknet.LookupApplicationPacketContract(packet.ID)
	if isFound {
		packetName = contract.Name
	}
	e.logger.Printf(
		"RakNet gameplay packet from %s ignored id=%#x name=%s length=%d source_time=%d client_time=%d payload=%x",
		packet.Address, packet.ID, packetName, len(packet.Payload),
		packet.SourceTime, packet.ClientTime, packet.Payload,
	)
}

func tutorialGameplayBinding(binding game.GameplayBinding) game.GameplayBinding {
	if binding.Mode == game.ModeTutorial {
		binding.DNA = tutorialStartingDNA
		if binding.Creatures[0].Noun == 0 {
			binding.Creatures[0].Noun = util.HashID("PC_EL_Rogue.Noun")
			binding.Creatures[0].Version = 1
		}
		if binding.Creatures[0].MinimumWeaponDamage <= 0 ||
			binding.Creatures[0].MaximumWeaponDamage < binding.Creatures[0].MinimumWeaponDamage {
			binding.Creatures[0].MinimumWeaponDamage = 4
			binding.Creatures[0].MaximumWeaponDamage = 12
		}
	}
	return binding
}

func tutorialSageBinding(binding game.GameplayBinding) game.GameplayBinding {
	if binding.Mode != game.ModeTutorial {
		return binding
	}
	if binding.Creatures[1].Noun == 0 {
		binding.Creatures[1] = game.GameplayCreature{
			Noun: util.HashID("PC_LF_Mage.Noun"), Version: 1,
			HitPoint:   campaignHeroResourceFallback,
			PowerPoint: campaignHeroResourceFallback,
		}
	}
	if binding.Creatures[1].MinimumWeaponDamage <= 0 ||
		binding.Creatures[1].MaximumWeaponDamage < binding.Creatures[1].MinimumWeaponDamage {
		binding.Creatures[1].MinimumWeaponDamage = 4
		binding.Creatures[1].MaximumWeaponDamage = 12
	}
	binding.Creatures[1].PassiveAbility = util.HashID("SupportHealerPassiveModifier")
	return binding
}

func marshalCampaignDungeonSetup(
	binding game.GameplayBinding, scriptObjectPlans []zoneobject.ScriptPlan,
	passiveModifierInstance [3]uint32, entryPosition raknet.Vector3, gameTime uint64,
	timeElapsed uint64, deployedCreatureIndex uint32, isDeployedVisible bool,
) ([][]byte, error) {
	gameType := campaignGameType(binding)
	heroTeam := uint8(1)
	if binding.Mode == game.ModeArena {
		if binding.Team != 0 {
			heroTeam = uint8(binding.Team)
		}
	}
	gameStatePacket, err := raknet.MarshalApplication(raknet.GameStateMessage{
		Data: raknet.GameStateData{
			GameTime: gameTime, TimeElapsed: timeElapsed,
			State: raknet.GameDungeon, Type: gameType,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("gameState: %w", err)
	}
	directorPacket, err := raknet.MarshalApplication(raknet.DirectorStateMessage{})
	if err != nil {
		return nil, fmt.Errorf("director: %w", err)
	}
	creatures := binding.Creatures
	if creatures[0].Noun == 0 {
		creatures[0] = game.GameplayCreature{Noun: util.HashID("PC_EL_Rogue.Noun"), Version: 1}
	}
	if deployedCreatureIndex >= uint32(len(creatures)) ||
		creatures[deployedCreatureIndex].Noun == 0 {
		return nil, errors.New("deployed creature unavailable")
	}
	deployedObjectID := zonehero.ObjectID(binding.Slot, deployedCreatureIndex)
	controlledObjectPacket, err := raknet.MarshalApplication(
		raknet.LabsPlayerControlledObjectMessage{
			Slot: uint8(binding.Slot), ObjectID: deployedObjectID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("controlledObject: %w", err)
	}
	deployPacket, err := raknet.MarshalApplication(raknet.PlayerCharacterDeployMessage{
		PlayerIndex: uint8(binding.Slot), CreatureIndex: deployedCreatureIndex,
		ObjectID: deployedObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("deploy: %w", err)
	}
	response := [][]byte{gameStatePacket, directorPacket}
	for index, creature := range creatures {
		if creature.Noun == 0 {
			continue
		}
		objectID := zonehero.ObjectID(binding.Slot, uint32(index))
		objectPacket, marshalErr := raknet.MarshalApplication(raknet.ObjectCreateMessage{
			ObjectID: objectID, Noun: creature.Noun, AssetID: zonehero.AppearanceAsset(creature),
			PositionX: entryPosition.X, PositionY: entryPosition.Y,
			PositionZ: entryPosition.Z, Scale: 1, Team: heroTeam,
			PlayerIndex: uint8(binding.Slot), IsCollisionEnabled: true, IsPlayerControlled: true,
		})
		if marshalErr != nil {
			return nil, fmt.Errorf("object[%d]: %w", index, marshalErr)
		}
		response = append(response, objectPacket)
		updatePacket, marshalErr := raknet.MarshalApplication(raknet.ObjectUpdateMessage{
			ObjectID: objectID, PositionX: entryPosition.X,
			PositionY: entryPosition.Y, PositionZ: entryPosition.Z,
			IsVisible: isDeployedVisible && uint32(index) == deployedCreatureIndex,
		})
		if marshalErr != nil {
			return nil, fmt.Errorf("objectUpdate[%d]: %w", index, marshalErr)
		}
		response = append(response, updatePacket)
		for _, message := range raknet.HeroStateMessages(objectID, creature.HitPoint, creature.PowerPoint) {
			heroStatePacket, marshalErr := raknet.MarshalApplication(message)
			if marshalErr != nil {
				return nil, fmt.Errorf("heroState[%d]: %w", index, marshalErr)
			}
			response = append(response, heroStatePacket)
		}
		isPassivePresented := creature.PassiveAbility != 0 &&
			passiveModifierInstance[index] != 0
		if isPassivePresented {
			passivePacket, marshalErr := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
				TargetID: objectID, ModifierGUID: creature.PassiveAbility,
				InstanceID: passiveModifierInstance[index], StackCount: 1,
				StartMilliseconds: gameTime, SourceID: objectID,
			})
			if marshalErr != nil {
				return nil, fmt.Errorf("passive[%d]: %w", index, marshalErr)
			}
			response = append(response, passivePacket)
		}
		if uint32(index) == deployedCreatureIndex {
			response = append(response, controlledObjectPacket, deployPacket)
		}
	}
	for index, plan := range scriptObjectPlans {
		objectPackets, marshalErr := objectraknet.Script(plan)
		if marshalErr != nil {
			return nil, fmt.Errorf("scriptObject[%d]: %w", index, marshalErr)
		}
		response = append(response, objectPackets...)
	}
	return response, nil
}

func marshalCampaignInitialPlayer(binding game.GameplayBinding, status raknet.PlayerStatus) ([]byte, error) {
	return marshalCampaignPlayer(binding, status, true)
}

func marshalCampaignPlayer(
	binding game.GameplayBinding, status raknet.PlayerStatus,
	isResourceFallbackAllowed bool,
) ([]byte, error) {
	creatures := binding.Creatures
	if creatures[0].Noun == 0 {
		creatures[0] = game.GameplayCreature{Noun: util.HashID("PC_EL_Rogue.Noun"), Version: 1}
	}
	for index := 1; index < len(creatures); index++ {
		if creatures[index].Noun == 0 {
			creatures[index] = creatures[0]
		}
	}
	message := raknet.LabsPlayerStatusMessage{
		OnlineID: binding.UserID, AvatarLevel: binding.AvatarLevel, AvatarXP: binding.AvatarXP,
		ChainProgression: binding.ChainProgression, DNA: binding.DNA,
		Slot: uint8(binding.Slot), Team: uint8(binding.Team),
		Status: status.Status, Progress: status.Progress,
		IsInitial: true, ControlledObjectID: zonehero.ObjectID(binding.Slot, 0),
		HeroNoun: creatures[0].Noun, HeroAsset: zonehero.AppearanceAsset(creatures[0]),
		HeroVersion: int32(creatures[0].Version), HeroType: 2,
		SecondHeroNoun: creatures[1].Noun, SecondHeroAsset: zonehero.AppearanceAsset(creatures[1]),
		SecondHeroVersion: int32(creatures[1].Version), SecondHeroType: 2,
		ThirdHeroNoun: creatures[2].Noun, ThirdHeroAsset: zonehero.AppearanceAsset(creatures[2]),
		ThirdHeroVersion: int32(creatures[2].Version), ThirdHeroType: 2,
		AbilityCount:        zoneunlock.InitialAbilityCount(binding),
		LockedDeckMinimum:   zonehero.CreatureCount(binding),
		EnergyPoint:         campaignInitialOverdriveEnergy(binding),
		IsOverdriveUnlocked: binding.IsOverdriveUnlocked,
		CharacterResources: campaignCharacterResources(
			creatures, isResourceFallbackAllowed,
		),
	}
	return raknet.MarshalApplication(message)
}

func campaignInitialOverdriveEnergy(binding game.GameplayBinding) float32 {
	if binding.IsOverdriveUnlocked {
		return float32(campaignOverdriveMaximumEnergy)
	}
	return 0
}

func marshalZoneHeroRoster(
	roster zoneprojection.HeroRoster,
) ([][]byte, error) {
	heroTeam := uint8(roster.Team)
	if heroTeam == 0 {
		heroTeam = 1
	}
	binding := game.GameplayBinding{
		UserID: roster.UserID, Slot: roster.PlayerSlot,
		Team:                roster.Team,
		AvatarLevel:         roster.Roster.AvatarLevel,
		AvatarXP:            roster.Roster.AvatarXP,
		ChainProgression:    roster.Roster.ChainProgression,
		IsOverdriveUnlocked: roster.Roster.IsOverdriveUnlocked,
		DNA:                 roster.Roster.DNA, Creatures: roster.Roster.Creatures,
	}
	statusPacket, err := marshalCampaignPlayer(
		binding, raknet.PlayerStatus{Status: 2, Progress: 1}, false,
	)
	if err != nil {
		return nil, fmt.Errorf("rosterStatus: %w", err)
	}
	packets := [][]byte{statusPacket}
	creatures := binding.Creatures
	if creatures[0].Noun == 0 {
		creatures[0] = game.GameplayCreature{
			Noun: util.HashID("PC_EL_Rogue.Noun"), Version: 1,
		}
	}
	for index, creature := range creatures {
		if creature.Noun == 0 {
			continue
		}
		objectID := zonehero.ObjectID(roster.PlayerSlot, uint32(index))
		objectPacket, marshalErr := raknet.MarshalApplication(
			raknet.ObjectCreateMessage{
				ObjectID: objectID, Noun: creature.Noun,
				AssetID:   zonehero.AppearanceAsset(creature),
				PositionX: roster.Position.X, PositionY: roster.Position.Y,
				PositionZ: roster.Position.Z, Scale: 1, Team: heroTeam,
				PlayerIndex:        uint8(roster.PlayerSlot),
				IsCollisionEnabled: true, IsPlayerControlled: true,
			},
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("rosterObject[%d]: %w", index, marshalErr)
		}
		packets = append(packets, objectPacket)
		updatePacket, marshalErr := raknet.MarshalApplication(
			raknet.ObjectUpdateMessage{
				ObjectID: objectID, PositionX: roster.Position.X,
				PositionY: roster.Position.Y, PositionZ: roster.Position.Z,
				IsVisible: uint32(index) == roster.CreatureIndex,
			},
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("rosterUpdate[%d]: %w", index, marshalErr)
		}
		packets = append(packets, updatePacket)
		for _, message := range raknet.HeroStateMessages(
			objectID, creature.HitPoint, creature.PowerPoint,
		) {
			statePacket, marshalErr := raknet.MarshalApplication(message)
			if marshalErr != nil {
				return nil, fmt.Errorf("rosterState[%d]: %w", index, marshalErr)
			}
			packets = append(packets, statePacket)
		}
	}
	deployPacket, err := raknet.MarshalApplication(
		raknet.PlayerCharacterDeployMessage{
			PlayerIndex:   uint8(roster.PlayerSlot),
			CreatureIndex: roster.CreatureIndex,
			ObjectID:      zonehero.ObjectID(roster.PlayerSlot, roster.CreatureIndex),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("rosterDeploy: %w", err)
	}
	return append(packets, deployPacket), nil
}

func marshalOtherZoneHeroRosters(
	campaignZone *zone.Zone, userID uint64,
) ([][]byte, error) {
	if campaignZone == nil {
		return nil, nil
	}
	packets := make([][]byte, 0)
	for index, member := range campaignZone.Snapshot().Members {
		if member.UserID == userID {
			continue
		}
		actor, isFound := campaignZone.Hero().Snapshot(
			member.UserID, member.PeerGeneration,
		)
		if !isFound {
			continue
		}
		roster := member.Roster
		if actor.CreatureIndex < uint32(len(roster.Creatures)) {
			roster.Creatures[actor.CreatureIndex].HitPoint = actor.HitPoint
			roster.Creatures[actor.CreatureIndex].PowerPoint = actor.ManaPoint
		}
		rosterPackets, err := marshalZoneHeroRoster(
			zoneprojection.HeroRoster{
				UserID: member.UserID, PlayerSlot: member.Slot,
				Roster: roster, Position: actor.Position,
				CreatureIndex: actor.CreatureIndex,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("zoneRoster[%d]: %w", index, err)
		}
		packets = append(packets, rosterPackets...)
	}
	return packets, nil
}

func campaignInitialCharacterResources(
	creatures [squad.Size]game.GameplayCreature,
) [squad.Size]raknet.LabsCharacterResource {
	return campaignCharacterResources(creatures, true)
}

func campaignCharacterResources(
	creatures [squad.Size]game.GameplayCreature,
	isFallbackAllowed bool,
) [squad.Size]raknet.LabsCharacterResource {
	resources := [squad.Size]raknet.LabsCharacterResource{}
	for index, creature := range creatures {
		hitPoint := creature.HitPoint
		if isFallbackAllowed && hitPoint <= 0 {
			hitPoint = campaignHeroResourceFallback
		}
		manaPoint := creature.PowerPoint
		if isFallbackAllowed && manaPoint <= 0 {
			manaPoint = campaignHeroResourceFallback
		}
		resources[index] = raknet.LabsCharacterResource{
			Health: hitPoint, MaxHealth: hitPoint, Mana: manaPoint, MaxMana: manaPoint,
			GearScore: creature.GearScore, FlattenedGearScore: creature.FlattenedGearScore,
			PartAttribute: creature.PartAttribute,
		}
	}
	return resources
}

func sortScheduledPacketProducersByDelay(producers []raknet.ScheduledPacketProducer) {
	slices.SortStableFunc(producers, func(
		left raknet.ScheduledPacketProducer, right raknet.ScheduledPacketProducer,
	) int {
		switch {
		case left.Delay < right.Delay:
			return -1
		case left.Delay > right.Delay:
			return 1
		default:
			return 0
		}
	})
}

func marshalCampaignCharacterSwitch(
	playerIndex uint8, sourceObjectID uint32, targetObjectID uint32, creatureIndex uint32,
	sourceCreature, targetCreature game.GameplayCreature,
	sourceHitPoint, sourcePowerPoint, targetHitPoint, targetPowerPoint float32,
	position raknet.Vector3, orientation raknet.Quaternion, animationTimestamp uint64,
	isDeathSelection bool,
) ([][]byte, error) {
	sourceBeam := campaignCharacterBeam(sourceCreature, false)
	messages := make([]raknet.ApplicationMessage, 0, 15)
	if !isDeathSelection {
		messages = append(messages,
			raknet.PositionedEffectMessage{
				Asset: util.HashID(sourceBeam + ".ServerEventDef"), Position: position,
			},
			raknet.SetAnimationStateMessage{
				ObjectID: sourceObjectID, State: util.HashID("character_teleport_out"),
				Timestamp: animationTimestamp, Scale: 1,
			},
		)
	}
	messages = append(messages, raknet.ObjectUpdateMessage{
		ObjectID: sourceObjectID, PositionX: position.X, PositionY: position.Y, PositionZ: position.Z,
		IsVisible: false,
	})
	messages = append(messages,
		raknet.CombatantDataDeltaMessage{
			ObjectID: sourceObjectID, HitPoints: sourceHitPoint, IsHitPointChanged: true,
			ManaPoints: sourcePowerPoint, IsManaPointChanged: true,
		},
		raknet.LabsPlayerControlledObjectMessage{Slot: playerIndex, ObjectID: targetObjectID},
		raknet.PlayerCharacterDeployMessage{
			PlayerIndex: playerIndex, CreatureIndex: creatureIndex, ObjectID: targetObjectID,
		},
		raknet.LabsPlayerDeployCooldownMessage{
			PlayerSlot: playerIndex, DeployedCreatureIndex: creatureIndex,
			// Build 103 compares this field directly with a private local
			// gameplay clock that no client request publishes. Keep the
			// authoritative cooldown in the squad session and clear the
			// client field rather than extending its lock with another epoch.
			DeadlineMilliseconds: 0,
		},
	)
	messages = append(messages, campaignCharacterArrivalMessages(
		targetObjectID, targetCreature, position, orientation,
		animationTimestamp,
	)...)
	// Deploy can replace the target object's local combatant state. Publish the
	// authoritative resource state after arrival so the power bar and the
	// client's ability admission read the same value.
	messages = append(messages, raknet.CombatantDataDeltaMessage{
		ObjectID: targetObjectID, HitPoints: targetHitPoint, IsHitPointChanged: true,
		ManaPoints: targetPowerPoint, IsManaPointChanged: true,
	})
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("switchPacket[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func campaignCharacterArrivalMessages(
	targetObjectID uint32, targetCreature game.GameplayCreature,
	position raknet.Vector3, orientation raknet.Quaternion,
	animationTimestamp uint64,
) []raknet.ApplicationMessage {
	if orientation.X == 0 && orientation.Y == 0 &&
		orientation.Z == 0 && orientation.W == 0 {
		orientation.W = 1
	}
	targetBeam := campaignCharacterBeam(targetCreature, true)
	return []raknet.ApplicationMessage{
		raknet.ObjectTeleportMessage{
			ObjectID: targetObjectID, Position: position, Orientation: orientation,
		},
		raknet.ObjectUpdateMessage{
			ObjectID: targetObjectID, PositionX: position.X, PositionY: position.Y, PositionZ: position.Z,
			IsVisible: true,
		},
		raknet.PositionedEffectMessage{
			Asset: util.HashID(targetBeam + ".ServerEventDef"), Position: position,
		},
		raknet.SetAnimationStateMessage{
			ObjectID: targetObjectID, State: util.HashID("character_teleport_in"),
			Timestamp: animationTimestamp, Scale: 1,
		},
	}
}

func marshalCampaignCharacterDeparture(
	sourceObjectID uint32, sourceCreature game.GameplayCreature,
	position raknet.Vector3, animationTimestamp uint64,
) ([][]byte, error) {
	sourceBeam := campaignCharacterBeam(sourceCreature, false)
	messages := []raknet.ApplicationMessage{
		raknet.PositionedEffectMessage{
			Asset: util.HashID(sourceBeam + ".ServerEventDef"), Position: position,
		},
		raknet.SetAnimationStateMessage{
			ObjectID: sourceObjectID, State: util.HashID("character_teleport_out"),
			Timestamp: animationTimestamp, Scale: 1,
		},
	}
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("departurePacket[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

type gameplayJoinRuntime struct {
	registry     *gameplaySessionRegistry
	gameplayJoin *game.GameplayJoin
	progression  tutorialprogress.RestartStore
	modifierPool *modifierPool
	effectPool   *attachedEffectPool
	logger       *log.Logger
}

func (r gameplayJoinRuntime) handle(
	ctx context.Context, packet raknet.Packet,
) ([][]byte, error) {
	responses, binding, err := helloPlayerResponses(
		ctx, r.gameplayJoin, packet, r.logger,
	)
	if err != nil {
		return nil, fmt.Errorf("helloPlayer: %w", err)
	}
	binding = tutorialGameplayBinding(binding)
	if r.progression != nil && binding.Mode == game.ModeTutorial &&
		(binding.AvatarXP != 0 || binding.AvatarLevel != 1) {
		experience, err := tutorialprogress.Restart(
			ctx, r.progression, int64(binding.UserID),
		)
		if err != nil {
			return nil, fmt.Errorf("helloTutorialReset: %w", err)
		}
		binding.AvatarXP = float32(experience.CumulativeXP)
		binding.AvatarLevel = experience.Level
	}
	sessionKey := packet.Address.String()
	r.registry.mutex.Lock()
	currentSession, isCurrentFound := r.registry.sessions[sessionKey]
	isDuplicate := isCurrentFound &&
		currentSession.binding.GameID == binding.GameID &&
		currentSession.binding.UserID == binding.UserID &&
		(packet.TransportGeneration == 0 ||
			currentSession.transportGeneration == packet.TransportGeneration)
	if isDuplicate {
		autonomousPacket := packet.Autonomous()
		currentSession.binding.Endpoint = binding.Endpoint
		currentSession.binding.PlayerMask = binding.PlayerMask
		currentSession.binding.Slot = binding.Slot
		currentSession.binding.Team = binding.Team
		currentSession.schedulePackets = autonomousPacket.Schedule
		currentSession.schedulePacket = gameplaySchedulePacket(autonomousPacket)
		r.registry.sessions[sessionKey] = currentSession
		r.registry.memberTransports[gameplaySessionMemberKey(currentSession)] =
			currentSession.transportGeneration
		r.registry.memberEndpoints[gameplaySessionMemberKey(currentSession)] =
			sessionKey
	}
	r.registry.mutex.Unlock()
	if isDuplicate {
		if r.logger != nil {
			r.logger.Printf(
				"RakNet duplicate gameplay hello retained game=%d user=%d remote=%s",
				binding.GameID, binding.UserID, packet.Address,
			)
		}
		rosterResponses, rosterErr := r.synchronizeRoster(sessionKey)
		if rosterErr != nil {
			return nil, fmt.Errorf("helloRoster: %w", rosterErr)
		}
		return append(responses, rosterResponses...), nil
	}
	if isCurrentFound {
		err = r.retainReplacedEndpoint(
			ctx, sessionKey, currentSession.transportGeneration,
		)
		if err != nil {
			return nil, fmt.Errorf("helloReplace: %w", err)
		}
	}
	memberKey := gameplayMemberKey{
		gameID: binding.GameID, userID: binding.UserID,
	}
	memberMutex, lockErr := r.registry.lockMember(ctx, memberKey)
	if lockErr != nil {
		return nil, fmt.Errorf("joinMember: %w", lockErr)
	}
	defer memberMutex.Unlock()
	contextErr := ctx.Err()
	if contextErr != nil {
		return nil, fmt.Errorf("memberContext: %w", contextErr)
	}
	if r.retainDuplicateSession(
		sessionKey, binding, packet.TransportGeneration,
	) {
		if r.logger != nil {
			r.logger.Printf(
				"RakNet serialized duplicate gameplay hello retained game=%d user=%d remote=%s",
				binding.GameID, binding.UserID, packet.Address,
			)
		}
		rosterResponses, rosterErr := r.synchronizeRoster(sessionKey)
		if rosterErr != nil {
			return nil, fmt.Errorf("serializedRoster: %w", rosterErr)
		}
		return append(responses, rosterResponses...), nil
	}
	r.registry.mutex.RLock()
	serializedSession, isSerializedFound := r.registry.sessions[sessionKey]
	r.registry.mutex.RUnlock()
	isSameMember := isSerializedFound &&
		serializedSession.binding.GameID == binding.GameID &&
		serializedSession.binding.UserID == binding.UserID
	if isSerializedFound && !isSameMember {
		return nil, errors.New("hello endpoint changed during admission")
	}
	if isSameMember && packet.TransportGeneration != 0 &&
		serializedSession.transportGeneration > packet.TransportGeneration {
		return nil, errors.New("hello transport generation superseded")
	}
	if isSameMember {
		r.registry.lifecycle.disconnectMember(
			sessionKey, r.modifierPool, r.effectPool,
		)
	}
	activeSessionKeys := r.registry.activeSessionKeys(memberKey, sessionKey)
	for _, activeSessionKey := range activeSessionKeys {
		r.registry.lifecycle.disconnectMember(
			activeSessionKey, r.modifierPool, r.effectPool,
		)
		if r.logger != nil {
			r.logger.Printf(
				"RakNet authenticated gameplay endpoint superseded game=%d user=%d old=%s new=%s",
				binding.GameID, binding.UserID, activeSessionKey, sessionKey,
			)
		}
	}
	contextErr = ctx.Err()
	if contextErr != nil {
		return nil, fmt.Errorf("replaceContext: %w", contextErr)
	}
	retainedSession, isRetained := r.registry.takeRetained(ctx, memberKey)
	if isRetained && (retainedSession.binding.Level != binding.Level ||
		retainedSession.binding.Mode != binding.Mode || retainedSession.zone == nil) {
		leaveGameplayPeerMembership(retainedSession)
		isRetained = false
	}
	if isRetained {
		err = retainedSession.zone.ValidateReconnect(
			binding.UserID, retainedSession.generation,
		)
		if err != nil {
			leaveGameplayPeerMembership(retainedSession)
			isRetained = false
			if r.logger != nil {
				r.logger.Printf(
					"RakNet gameplay retained rejoin rejected game=%d user=%d: %v",
					binding.GameID, binding.UserID, err,
				)
			}
		}
	}
	transportGeneration := packet.TransportGeneration
	if transportGeneration == 0 {
		transportGeneration = r.registry.lifecycle.nextGeneration()
	}
	nextSession := gameplayPeerSession{
		zoneMembership: zoneMembership{
			generation: r.registry.lifecycle.nextGeneration(),
		},
		binding: binding, transportGeneration: transportGeneration,
		schedulePackets: packet.Autonomous().Schedule,
		schedulePacket:  gameplaySchedulePacket(packet),
	}
	if binding.Slot < 32 {
		nextSession.knownPlayerMask = uint32(1) << binding.Slot
	}
	if binding.IsCheckpointRestore {
		nextSession.stage.AwaitResume()
	}
	if isRetained {
		nextSession = retainedSession
		nextSession.binding.Endpoint = binding.Endpoint
		nextSession.binding.PlayerMask = binding.PlayerMask
		nextSession.binding.Slot = binding.Slot
		nextSession.binding.Team = binding.Team
		nextSession.transportGeneration = transportGeneration
		nextSession.schedulePackets = packet.Autonomous().Schedule
		nextSession.schedulePacket = gameplaySchedulePacket(packet)
		nextSession.isPartyMerged = false
		nextSession.isArenaLobbyTransitionSent = false
		nextSession.isArenaLobbyEntered = false
		nextSession.isArenaSquadAccepted = false
		nextSession.isArenaPreparationSent = false
		nextSession.isArenaPreparationAcknowledged = false
		nextSession.arenaLobbyTransitionAt = time.Time{}
		nextSession.arenaLobbyTransitionCount = 0
		if binding.Slot < 32 {
			nextSession.knownPlayerMask |= uint32(1) << binding.Slot
		}
		nextSession.isRejoinPending = true
	}
	if binding.IsCheckpointRestore {
		nextSession.binding.IsCheckpointRestore = true
		nextSession.stage.AwaitResume()
		nextSession.dungeonSetup.Reset()
		nextSession.isRejoinPending = false
	}
	r.registry.mutex.Lock()
	if r.registry.isClosed {
		r.registry.mutex.Unlock()
		if isRetained {
			leaveGameplayPeerMembership(nextSession)
		} else {
			stopGameplayPeerSession(nextSession, r.modifierPool, r.effectPool)
		}
		return nil, errors.New("gameplay registry closed")
	}
	previousSession, isPreviousFound := r.registry.sessions[sessionKey]
	r.registry.sessions[sessionKey] = nextSession
	r.registry.memberTransports[memberKey] = transportGeneration
	r.registry.memberEndpoints[memberKey] = sessionKey
	r.registry.mutex.Unlock()
	if isPreviousFound {
		stopGameplayPeerSession(previousSession, r.modifierPool, r.effectPool)
	}
	if isRetained && r.logger != nil {
		r.logger.Printf(
			"RakNet gameplay member reattached game=%d user=%d remote=%s transport_generation=%d",
			binding.GameID, binding.UserID, packet.Address, transportGeneration,
		)
	}
	rosterResponses, err := r.synchronizeRoster(sessionKey)
	if err != nil {
		return nil, fmt.Errorf("joinRoster: %w", err)
	}
	return append(responses, rosterResponses...), nil
}

// synchronizeRoster establishes every connected gameplay slot before the
// client is told that the party merge is complete. Build 103 dereferences the
// complete party roster when it receives PartyMergeComplete.
func (r gameplayJoinRuntime) synchronizeRoster(sessionKey string) ([][]byte, error) {
	r.registry.mutex.Lock()
	defer r.registry.mutex.Unlock()
	joiningSession, isFound := r.registry.sessions[sessionKey]
	if !isFound {
		return nil, errors.New("joining session not found")
	}
	peerSessionKeys := make([]string, 0, joiningSession.binding.ParticipantCount)
	var connectedPlayerMask uint32
	for peerSessionKey, peerSession := range r.registry.sessions {
		if peerSession.binding.GameID != joiningSession.binding.GameID {
			continue
		}
		if peerSession.binding.Slot >= 32 {
			return nil, errors.New("connected gameplay slot exceeds mask width")
		}
		peerSessionKeys = append(peerSessionKeys, peerSessionKey)
		connectedPlayerMask |= uint32(1) << peerSession.binding.Slot
	}
	slices.SortFunc(peerSessionKeys, func(left, right string) int {
		leftSlot := r.registry.sessions[left].binding.Slot
		rightSlot := r.registry.sessions[right].binding.Slot
		return cmp.Compare(leftSlot, rightSlot)
	})
	responses := make([][]byte, 0, len(peerSessionKeys))
	for _, peerSessionKey := range peerSessionKeys {
		if peerSessionKey == sessionKey {
			continue
		}
		peerSession := r.registry.sessions[peerSessionKey]
		if peerSession.binding.Slot >= 32 || joiningSession.binding.Slot >= 32 {
			return nil, errors.New("gameplay roster slot exceeds mask width")
		}
		peerBit := uint32(1) << peerSession.binding.Slot
		if joiningSession.knownPlayerMask&peerBit == 0 {
			joinedPacket, err := raknet.MarshalApplication(raknet.PlayerSlotMessage{
				ID: raknet.PlayerJoined, Slot: uint8(peerSession.binding.Slot),
			})
			if err != nil {
				return nil, fmt.Errorf("joiningPlayer[%d]: %w", peerSession.binding.Slot, err)
			}
			responses = append(responses, joinedPacket)
			joiningSession.knownPlayerMask |= peerBit
		}
		joiningBit := uint32(1) << joiningSession.binding.Slot
		if peerSession.knownPlayerMask&joiningBit == 0 {
			joinedPacket, err := raknet.MarshalApplication(raknet.PlayerSlotMessage{
				ID: raknet.PlayerJoined, Slot: uint8(joiningSession.binding.Slot),
			})
			if err != nil {
				return nil, fmt.Errorf("existingPlayer[%d]: %w", joiningSession.binding.Slot, err)
			}
			peerSession.queuePackets([][]byte{joinedPacket})
			peerSession.knownPlayerMask |= joiningBit
			r.registry.sessions[peerSessionKey] = peerSession
		}
	}
	r.registry.sessions[sessionKey] = joiningSession
	expectedCount := int(joiningSession.binding.ParticipantCount)
	if expectedCount < 1 {
		expectedCount = 1
	}
	if len(peerSessionKeys) < expectedCount ||
		connectedPlayerMask != joiningSession.binding.PlayerMask {
		if r.logger != nil {
			r.logger.Printf(
				"RakNet gameplay roster waiting game=%d connected=%d expected=%d connected_mask=%#x expected_mask=%#x",
				joiningSession.binding.GameID, len(peerSessionKeys), expectedCount,
				connectedPlayerMask, joiningSession.binding.PlayerMask,
			)
		}
		return responses, nil
	}
	partyPacket, err := raknet.MarshalApplication(raknet.TimestampMessage{
		ID: raknet.PartyMergeComplete, Timestamp: uint64(time.Now().Unix()),
	})
	if err != nil {
		return nil, fmt.Errorf("partyMarshal: %w", err)
	}
	var arenaPlayerPackets [][]byte
	var arenaBranchPacket []byte
	if joiningSession.binding.Mode == game.ModeArena {
		arenaPlayerPackets = make([][]byte, 0, len(peerSessionKeys))
		for _, peerSessionKey := range peerSessionKeys {
			peerSession := r.registry.sessions[peerSessionKey]
			playerPacket, marshalErr := marshalCampaignInitialPlayer(
				peerSession.binding, raknet.PlayerStatus{},
			)
			if marshalErr != nil {
				return nil, fmt.Errorf(
					"arenaPlayer[%d]: %w", peerSession.binding.Slot, marshalErr,
				)
			}
			arenaPlayerPackets = append(arenaPlayerPackets, playerPacket)
		}
		arenaBranchPacket, err = raknet.MarshalApplication(raknet.ArenaGameBranchMessage{})
		if err != nil {
			return nil, fmt.Errorf("arenaBranchMarshal: %w", err)
		}
	}
	transitionAt := time.Now()
	for _, peerSessionKey := range peerSessionKeys {
		peerSession := r.registry.sessions[peerSessionKey]
		if peerSession.isPartyMerged {
			continue
		}
		peerSession.isPartyMerged = true
		packets := make([][]byte, 0, len(arenaPlayerPackets)+2)
		packets = append(packets, arenaPlayerPackets...)
		packets = append(packets, partyPacket)
		if len(arenaBranchPacket) != 0 && !peerSession.isArenaLobbyTransitionSent {
			packets = append(packets, arenaBranchPacket)
			peerSession.isArenaLobbyTransitionSent = true
			peerSession.arenaLobbyTransitionAt = transitionAt
			peerSession.arenaLobbyTransitionCount++
		}
		if peerSessionKey == sessionKey {
			responses = append(responses, packets...)
		} else {
			peerSession.queuePackets(packets)
		}
		r.registry.sessions[peerSessionKey] = peerSession
	}
	if r.logger != nil {
		r.logger.Printf(
			"RakNet gameplay roster merged game=%d connected=%d mask=%#x",
			joiningSession.binding.GameID, len(peerSessionKeys),
			joiningSession.binding.PlayerMask,
		)
	}
	return responses, nil
}

func (r gameplayJoinRuntime) retainDuplicateSession(
	sessionKey string, binding game.GameplayBinding,
	transportGeneration uint64,
) bool {
	r.registry.mutex.Lock()
	defer r.registry.mutex.Unlock()
	currentSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || currentSession.binding.GameID != binding.GameID ||
		currentSession.binding.UserID != binding.UserID ||
		(transportGeneration != 0 &&
			currentSession.transportGeneration != transportGeneration) {
		return false
	}
	currentSession.binding.Endpoint = binding.Endpoint
	currentSession.binding.PlayerMask = binding.PlayerMask
	currentSession.binding.Slot = binding.Slot
	currentSession.binding.Team = binding.Team
	r.registry.sessions[sessionKey] = currentSession
	memberKey := gameplaySessionMemberKey(currentSession)
	r.registry.memberTransports[memberKey] = currentSession.transportGeneration
	r.registry.memberEndpoints[memberKey] = sessionKey
	return true
}

func (r gameplayJoinRuntime) retainReplacedEndpoint(
	ctx context.Context, sessionKey string, transportGeneration uint64,
) error {
	if sessionKey == "" || transportGeneration == 0 {
		return nil
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	r.registry.mutex.RUnlock()
	if !isFound || peerSession.transportGeneration != transportGeneration {
		return nil
	}
	memberKey := gameplaySessionMemberKey(peerSession)
	memberMutex, err := r.registry.lockMember(ctx, memberKey)
	if err != nil {
		return fmt.Errorf("replaceMember: %w", err)
	}
	defer memberMutex.Unlock()
	r.registry.mutex.RLock()
	peerSession, isFound = r.registry.sessions[sessionKey]
	isCurrent := isFound &&
		peerSession.transportGeneration == transportGeneration
	r.registry.mutex.RUnlock()
	if !isCurrent {
		return nil
	}
	r.registry.lifecycle.disconnectMember(
		sessionKey, r.modifierPool, r.effectPool,
	)
	return nil
}

type gameplayPendingRuntime struct {
	registry             *gameplaySessionRegistry
	projection           gameplayProjectionRuntime
	setup                gameplaySetupRuntime
	gameplayJoin         *game.GameplayJoin
	stats                *playerstat.Recorder
	damage               campaignDamageRuntime
	result               campaignResultRuntime
	overdriveProgression campaignOverdriveProgression
	modifierPool         *modifierPool
	effectPool           *attachedEffectPool
	now                  func() time.Time
	logger               *log.Logger
}

func (r gameplayPendingRuntime) retireCanceledTransport(packet raknet.Packet) {
	if r.registry == nil || packet.Address == nil ||
		packet.TransportGeneration == 0 {
		return
	}
	r.registry.lifecycle.disconnect(
		packet.Address.String(), packet.TransportGeneration,
		r.modifierPool, r.effectPool,
	)
}

func (r gameplayPendingRuntime) poll(
	ctx context.Context, packet raknet.Packet,
) ([][]byte, error) {
	if !r.registry.beginRequest() {
		return nil, errors.New("gameplay registry closed")
	}
	defer r.registry.endRequest()
	if packet.Address == nil {
		return nil, errors.New("pending poll: nil address")
	}
	r.persistOverdriveUnlock(ctx, packet.Address.String())
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[packet.Address.String()]
	isRejoinNeeded := isFound && peerSession.isRejoinPending &&
		peerSession.stage.IsDungeon() && peerSession.zone != nil
	r.registry.mutex.RUnlock()
	if isRejoinNeeded {
		packets, err := r.setup.publishRejoin(packet, peerSession)
		if err != nil {
			if errors.Is(err, errRejoinSnapshotActive) {
				if r.logger != nil {
					r.logger.Printf(
						"RakNet gameplay rejoin baseline deferred game=%d user=%d remote=%s: zone remained active",
						peerSession.binding.GameID, peerSession.binding.UserID,
						packet.Address,
					)
				}
				return nil, nil
			}
			return nil, fmt.Errorf("rejoinPoll: %w", err)
		}
		return packets, nil
	}
	r.registry.mutex.Lock()
	peerSession, isFound = r.registry.sessions[packet.Address.String()]
	queuedPackets, pendingPacketBatchID := peerSession.pendingPackets()
	isPendingPacketOverflow := peerSession.isPendingPacketOverflow
	peerSession.isPendingPacketOverflow = false
	queuedStatDeltas := peerSession.takeStatDeltas()
	isArenaRetryDue := isFound && len(queuedPackets) == 0 &&
		peerSession.binding.Mode == game.ModeArena &&
		peerSession.isPartyMerged &&
		!peerSession.isArenaPreparationAcknowledged &&
		r.now().Sub(peerSession.arenaLobbyTransitionAt) >= arenaLobbyTransitionRetryInterval
	isArenaTransitionRetry := isArenaRetryDue && !peerSession.isArenaLobbyEntered
	isArenaPreparationRetry := isArenaRetryDue && peerSession.isArenaPreparationSent
	if isArenaTransitionRetry {
		peerSession.isArenaLobbyTransitionSent = true
		peerSession.arenaLobbyTransitionCount++
	}
	if isArenaTransitionRetry || isArenaPreparationRetry {
		peerSession.arenaLobbyTransitionAt = r.now()
	}
	if isFound && (len(queuedStatDeltas) != 0 || isPendingPacketOverflow ||
		isArenaTransitionRetry || isArenaPreparationRetry) {
		r.registry.sessions[packet.Address.String()] = peerSession
	}
	r.registry.mutex.Unlock()
	if isPendingPacketOverflow {
		if peerSession.zone != nil && peerSession.binding.Mode != game.ModeArena {
			err := peerSession.zone.RequireProjectionBaseline(
				peerSession.binding.UserID, peerSession.generation,
			)
			if err != nil {
				r.registry.mutex.Lock()
				currentSession, isCurrentFound :=
					r.registry.sessions[packet.Address.String()]
				if isCurrentFound &&
					currentSession.generation == peerSession.generation {
					currentSession.isPendingPacketOverflow = true
					r.registry.sessions[packet.Address.String()] = currentSession
				}
				r.registry.mutex.Unlock()
				if r.logger != nil {
					r.logger.Printf(
						"RakNet co-op pending overflow baseline failed game=%d user=%d: %v",
						peerSession.binding.GameID, peerSession.binding.UserID, err,
					)
				}
			}
		}
		if r.logger != nil {
			r.logger.Printf(
				"RakNet pending peer packets overflowed game=%d user=%d mode=%d retained=%d",
				peerSession.binding.GameID, peerSession.binding.UserID,
				peerSession.binding.Mode, len(queuedPackets),
			)
		}
	}
	for _, delta := range queuedStatDeltas {
		err := r.stats.Record(ctx, peerSession.binding, delta)
		if err != nil && r.logger != nil {
			r.logger.Printf(
				"RakNet queued co-op stats omitted game=%d user=%d: %v",
				peerSession.binding.GameID, peerSession.binding.UserID, err,
			)
		}
	}
	if len(queuedPackets) != 0 {
		err := packet.AfterResponseCommit(func() {
			r.registry.mutex.Lock()
			currentSession, isCurrent := r.registry.sessions[packet.Address.String()]
			if isCurrent && currentSession.transportGeneration == packet.TransportGeneration {
				currentSession.commitPendingPackets(pendingPacketBatchID)
				r.registry.sessions[packet.Address.String()] = currentSession
			}
			r.registry.mutex.Unlock()
		})
		if err != nil {
			return nil, fmt.Errorf("pendingCommit: %w", err)
		}
		return queuedPackets, nil
	}
	if isArenaTransitionRetry || isArenaPreparationRetry {
		var responses [][]byte
		if isArenaTransitionRetry {
			branchPacket, err := raknet.MarshalApplication(raknet.ArenaGameBranchMessage{})
			if err != nil {
				return nil, fmt.Errorf("arenaBranchRetry: %w", err)
			}
			responses = append(responses, branchPacket)
		}
		if isArenaPreparationRetry {
			arenaRuntime := gameplayArenaRuntime{
				registry: r.registry,
				logger:   r.logger,
			}
			preparationPackets, err := arenaRuntime.prepareSelectedDecks(
				packet.Address.String(), peerSession.binding.GameID, true,
			)
			if err != nil {
				return nil, fmt.Errorf("arenaPrepareRetry: %w", err)
			}
			responses = append(responses, preparationPackets...)
		}
		attempt := peerSession.arenaLobbyTransitionCount
		isDiagnosticAttempt := attempt <= 3 || attempt%10 == 0
		if r.logger != nil && isDiagnosticAttempt {
			r.logger.Printf(
				"RakNet Arena lobby transition retry game=%d user=%d attempt=%d",
				peerSession.binding.GameID, peerSession.binding.UserID,
				peerSession.arenaLobbyTransitionCount,
			)
		}

		return responses, nil
	}
	recoveryPackets, err := r.recoverNPCs(packet)
	if err != nil {
		return nil, fmt.Errorf("npcRecovery: %w", err)
	}
	if len(recoveryPackets) != 0 {
		return recoveryPackets, nil
	}
	idlePackets, err := r.recoverIdleNPCActions(packet)
	if err != nil {
		return nil, fmt.Errorf("npcIdleRecovery: %w", err)
	}
	if len(idlePackets) != 0 {
		return idlePackets, nil
	}
	responses, err := r.result.poll(ctx, packet)
	if err != nil {
		return nil, fmt.Errorf("resultPoll: %w", err)
	}
	if len(responses) != 0 {
		return responses, nil
	}
	projected, err := r.projection.drain(
		packet, packet.Address.String(), packet.SourceTime,
	)
	if err != nil {
		return nil, fmt.Errorf("projectionPoll: %w", err)
	}
	if len(projected) != 0 {
		return projected, nil
	}
	responses, _, err = r.consumePlayerResourceCommand(ctx, packet)
	if err != nil {
		return nil, fmt.Errorf("resourcePoll: %w", err)
	}
	if len(responses) != 0 {
		return responses, nil
	}
	responses, _, err = r.consumePlayerEventCommand(ctx, packet)
	if err != nil {
		return nil, fmt.Errorf("eventPoll: %w", err)
	}
	if len(responses) != 0 {
		return responses, nil
	}
	responses, _, err = r.consumeEffectPreview(ctx, packet)
	if err != nil {
		return nil, fmt.Errorf("effectPoll: %w", err)
	}
	if len(responses) != 0 {
		return responses, nil
	}
	responses, _, err = r.consumePlayerLevelUpdate(ctx, packet)
	if err != nil {
		return nil, fmt.Errorf("levelPoll: %w", err)
	}
	if len(responses) != 0 {
		return responses, nil
	}
	responses, _, err = r.consumePlayerDNAUpdate(ctx, packet)
	if err != nil {
		return nil, fmt.Errorf("dnaPoll: %w", err)
	}
	if len(responses) != 0 {
		return responses, nil
	}
	responses, _, err = r.consumeItemPresentation(ctx, packet)
	if err != nil {
		return nil, fmt.Errorf("itemPoll: %w", err)
	}
	return responses, nil
}

func (r gameplayPendingRuntime) persistOverdriveUnlock(
	ctx context.Context, sessionKey string,
) {
	if r.overdriveProgression == nil {
		return
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isPending := isFound && peerSession.isOverdrivePersistencePending
	r.registry.mutex.RUnlock()
	if !isPending {
		return
	}
	_, err := r.overdriveProgression.UnlockOverdrive(
		ctx, int64(peerSession.binding.UserID),
	)
	if err != nil {
		if r.logger != nil {
			r.logger.Printf(
				"RakNet Overdrive entitlement persistence deferred game=%d user=%d: %v",
				peerSession.binding.GameID, peerSession.binding.UserID, err,
			)
		}
		return
	}
	r.registry.mutex.Lock()
	currentSession, isCurrent := r.registry.sessions[sessionKey]
	if isCurrent && currentSession.generation == peerSession.generation {
		currentSession.binding.IsOverdriveUnlocked = true
		currentSession.isOverdrivePersistencePending = false
		r.registry.sessions[sessionKey] = currentSession
	}
	r.registry.mutex.Unlock()
}

func (r gameplayPendingRuntime) recoverIdleNPCActions(
	packet raknet.Packet,
) ([][]byte, error) {
	sessionKey := packet.Address.String()
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.zone != nil &&
		peerSession.zone.NPCs() != nil && !peerSession.isZoneTerminal()
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	plans := make([]zonenpc.SpawnPlan, 0)
	for _, npc := range peerSession.zone.NPCs().Snapshots() {
		if npc.IsDefeated || npc.Plan.IsFixture || npc.TargetObjectID == 0 ||
			npc.IsActionStarted {
			continue
		}
		_, isTargetFound := peerSession.campaignNPCTarget(
			peerSession.generation, npc.TargetObjectID,
		)
		if !isTargetFound {
			continue
		}
		plans = append(plans, npc.Plan)
	}
	generation := peerSession.generation
	r.registry.mutex.RUnlock()
	if len(plans) == 0 {
		return nil, nil
	}
	packets, err := r.damage.npc.scheduleFirstActions(
		packet, sessionKey, generation, plans, packet.SourceTime,
	)
	if err != nil {
		return nil, fmt.Errorf("npcSchedule: %w", err)
	}
	if r.logger != nil {
		r.logger.Printf(
			"RakNet recovered idle aggroed NPC actions remote=%s actors=%d",
			packet.Address, len(plans),
		)
	}
	return packets, nil
}

func (r gameplayPendingRuntime) recoverNPCs(
	packet raknet.Packet,
) ([][]byte, error) {
	sessionKey := packet.Address.String()
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isPending := isFound && peerSession.isNPCRecoveryPending &&
		peerSession.zone != nil && !peerSession.isZoneTerminal()
	if isPending {
		peerSession.isNPCRecoveryPending = false
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
	if !isPending {
		return nil, nil
	}
	_, err := peerSession.zone.RefreshNPCTargets()
	if err != nil {
		r.restoreNPCRecovery(sessionKey, peerSession.transportGeneration)
		return nil, fmt.Errorf("npcTarget: %w", err)
	}
	plans := make([]zonenpc.SpawnPlan, 0)
	for _, npc := range peerSession.zone.NPCs().Snapshots() {
		if npc.IsDefeated || npc.Plan.IsFixture || npc.TargetObjectID == 0 ||
			npc.IsActionStarted {
			continue
		}
		plans = append(plans, npc.Plan)
	}
	packets, err := r.damage.npc.scheduleFirstActions(
		packet, sessionKey, peerSession.generation, plans, packet.SourceTime,
	)
	if err != nil {
		r.restoreNPCRecovery(sessionKey, peerSession.transportGeneration)
		return nil, fmt.Errorf("npcSchedule: %w", err)
	}
	if r.logger != nil {
		r.logger.Printf(
			"RakNet recovered NPC actions after member disconnect remote=%s actors=%d",
			packet.Address, len(plans),
		)
	}
	return packets, nil
}

func (r gameplayPendingRuntime) restoreNPCRecovery(
	sessionKey string, transportGeneration uint64,
) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if isFound && peerSession.transportGeneration == transportGeneration {
		peerSession.isNPCRecoveryPending = true
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
}

func (r gameplayPendingRuntime) consumePacket(
	ctx context.Context, packet raknet.Packet,
) ([][]byte, bool, error) {
	responses, isHandled, err := r.consumePlayerEventCommand(ctx, packet)
	if err != nil {
		return nil, false, fmt.Errorf("playerEvent: %w", err)
	}
	if isHandled {
		return responses, true, nil
	}
	responses, isHandled, err = r.consumePlayerResourceCommand(ctx, packet)
	if err != nil {
		return nil, false, fmt.Errorf("playerResource: %w", err)
	}
	if isHandled {
		return responses, true, nil
	}
	responses, isHandled, err = r.consumeEffectPreview(ctx, packet)
	if err != nil {
		return nil, false, fmt.Errorf("effectPreview: %w", err)
	}
	if isHandled {
		return responses, true, nil
	}
	responses, isHandled, err = r.consumePlayerLevelUpdate(ctx, packet)
	if err != nil {
		return nil, false, fmt.Errorf("playerLevelUpdate: %w", err)
	}
	if isHandled {
		return responses, true, nil
	}
	responses, isHandled, err = r.consumePlayerDNAUpdate(ctx, packet)
	if err != nil {
		return nil, false, fmt.Errorf("playerDNAUpdate: %w", err)
	}
	if isHandled {
		return responses, true, nil
	}
	responses, isHandled, err = r.consumeItemPresentation(ctx, packet)
	if err != nil {
		return nil, false, fmt.Errorf("itemPresentation: %w", err)
	}
	return responses, isHandled, nil
}

func (r gameplayPendingRuntime) consumeEffectPreview(
	ctx context.Context, packet raknet.Packet,
) ([][]byte, bool, error) {
	r.registry.mutex.RLock()
	queuedSession, isSessionFound := r.registry.sessions[packet.Address.String()]
	isPreviewEligible := isSessionFound && queuedSession.stage.IsDungeon() &&
		queuedSession.dungeonSetup.IsCommitted() && queuedSession.deployedObjectID != 0
	r.registry.mutex.RUnlock()
	if !isSessionFound {
		return nil, false, nil
	}
	preview, isPreviewFound, err := r.gameplayJoin.ConsumeEffectPreview(
		ctx, int64(queuedSession.binding.UserID), queuedSession.binding.GameID,
	)
	if err != nil {
		return nil, false, fmt.Errorf("effectConsume: %w", err)
	}
	if !isPreviewFound {
		return nil, false, nil
	}
	if !isPreviewEligible {
		r.logger.Printf(
			"RakNet discarded Fang effect preview asset=0x%08x during unavailable state for %s",
			preview.Asset, packet.Address,
		)
		return nil, false, nil
	}
	packetBytes, err := raknet.MarshalApplication(raknet.DebugEffectPreviewMessage{
		Asset: preview.Asset, Position: queuedSession.playerPosition,
	})
	if err != nil {
		return nil, false, fmt.Errorf("effectMarshal: %w", err)
	}
	r.logger.Printf(
		"RakNet Fang world effect preview asset=0x%08x object=%d for %s",
		preview.Asset, queuedSession.deployedObjectID, packet.Address,
	)
	return [][]byte{packetBytes}, true, nil
}
func (r gameplayPendingRuntime) consumePlayerResourceCommand(
	ctx context.Context, packet raknet.Packet,
) ([][]byte, bool, error) {
	r.registry.mutex.RLock()
	queuedSession, isSessionFound := r.registry.sessions[packet.Address.String()]
	isCommandEligible := isSessionFound && queuedSession.stage.IsDungeon() &&
		queuedSession.dungeonSetup.IsCommitted() && queuedSession.deployedObjectID != 0 &&
		queuedSession.squad != nil && !queuedSession.isZoneTerminal()
	r.registry.mutex.RUnlock()
	if !isSessionFound {
		return nil, false, nil
	}
	command, isCommandFound, err := r.gameplayJoin.ConsumePlayerResourceCommand(
		ctx, int64(queuedSession.binding.UserID), queuedSession.binding.GameID,
	)
	if err != nil {
		return nil, false, fmt.Errorf("resourceConsume: %w", err)
	}
	if !isCommandFound {
		return nil, false, nil
	}
	if !isCommandEligible {
		r.logger.Printf("RakNet discarded developer resource command during unavailable state for %s", packet.Address)
		return nil, false, nil
	}
	r.registry.mutex.Lock()
	currentSession, isCurrentFound := r.registry.sessions[packet.Address.String()]
	isCurrent := isCurrentFound && currentSession.generation == queuedSession.generation
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, false, errors.New("resourceSession: replaced")
	}
	packets, statDelta, applyErr := currentSession.applyDeveloperResourceCommand(
		command, packet.SourceTime, r.now(),
	)
	if applyErr != nil {
		r.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("resourceApply: %w", applyErr)
	}
	binding := currentSession.binding
	r.registry.sessions[packet.Address.String()] = currentSession
	r.registry.mutex.Unlock()
	err = r.stats.Record(context.Background(), binding, statDelta)
	if err != nil {
		return nil, false, fmt.Errorf("resourceStats: %w", err)
	}
	r.logger.Printf(
		"RakNet developer resource command damage=%g power_reduction=%g heal=%t power_fill=%t for %s",
		command.Damage, command.PowerReduction, command.IsHeal, command.IsPowerFill, packet.Address,
	)
	return packets, true, nil
}
func (r gameplayPendingRuntime) consumePlayerEventCommand(
	ctx context.Context, packet raknet.Packet,
) ([][]byte, bool, error) {
	r.registry.mutex.RLock()
	queuedSession, isSessionFound := r.registry.sessions[packet.Address.String()]
	isCommandEligible := isSessionFound && queuedSession.stage.IsDungeon() &&
		queuedSession.dungeonSetup.IsCommitted() &&
		queuedSession.deployedObjectID != 0 && !queuedSession.isZoneTerminal()
	r.registry.mutex.RUnlock()
	if !isSessionFound {
		return nil, false, nil
	}
	command, isCommandFound, err := r.gameplayJoin.ConsumePlayerEventCommand(
		ctx, int64(queuedSession.binding.UserID), queuedSession.binding.GameID,
		packet.ScheduleGroup != nil || packet.ScheduleGroupResult != nil,
	)
	if err != nil {
		return nil, false, fmt.Errorf("eventConsume: %w", err)
	}
	if !isCommandFound {
		return nil, false, nil
	}
	if !isCommandEligible {
		r.logger.Printf("RakNet discarded developer event %q during unavailable state for %s",
			command.Name, packet.Address)
		return nil, false, nil
	}
	if command.Name == "spawn" {
		return r.spawnDeveloperNPC(packet, queuedSession, command)
	}
	if command.Name == "follow" {
		return r.activatePlayerFollow(packet, queuedSession, command)
	}
	if command.Name == "victory" && queuedSession.binding.Mode == game.ModeArena {
		return r.completeDeveloperArenaVictory(packet, queuedSession)
	}
	if command.Name == "victory" && queuedSession.binding.Mode == game.ModeTutorial {
		return r.completeDeveloperTutorialVictory(ctx, packet, queuedSession)
	}
	if command.Name == "victory" &&
		queuedSession.binding.Mode == game.ModeChain &&
		queuedSession.binding.ChainLevelIndex == zoneunlock.OverdriveChainLevelIndex &&
		!queuedSession.binding.IsOverdriveUnlocked {
		if r.overdriveProgression == nil {
			return nil, false, errors.New("eventVictoryOverdrive: progression unavailable")
		}
		_, unlockErr := r.overdriveProgression.UnlockOverdrive(
			ctx, int64(queuedSession.binding.UserID),
		)
		if unlockErr != nil {
			return nil, false, fmt.Errorf("eventVictoryOverdrive: %w", unlockErr)
		}
		queuedSession.binding.IsOverdriveUnlocked = true
	}
	r.registry.mutex.Lock()
	currentSession, isCurrentFound := r.registry.sessions[packet.Address.String()]
	isCurrent := isCurrentFound && currentSession.generation == queuedSession.generation
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, false, errors.New("eventSession: replaced")
	}
	if queuedSession.binding.IsOverdriveUnlocked {
		currentSession.binding.IsOverdriveUnlocked = true
		currentSession.isOverdrivePersistencePending = false
	}
	if command.Name == "victory" {
		completionPacket, bossObjectID, victoryErr :=
			currentSession.applyDeveloperVictoryCommand()
		if victoryErr != nil {
			r.registry.mutex.Unlock()
			return nil, false, fmt.Errorf("eventVictoryApply: %w", victoryErr)
		}
		r.registry.sessions[packet.Address.String()] = currentSession
		r.registry.mutex.Unlock()
		r.logger.Printf(
			"RakNet developer victory published boss completion boss=%d for %s; awaiting Return to Ship",
			bossObjectID, packet.Address,
		)
		return [][]byte{completionPacket}, true, nil
	}
	if command.Name == "defeat" {
		packets, defeatErr := currentSession.applyDeveloperDefeatCommand()
		if defeatErr != nil {
			r.registry.mutex.Unlock()
			return nil, false, fmt.Errorf("eventDefeatApply: %w", defeatErr)
		}
		r.registry.sessions[packet.Address.String()] = currentSession
		r.registry.mutex.Unlock()
		r.logger.Printf("RakNet developer defeat entered Game Over for %s", packet.Address)
		return packets, true, nil
	}
	if command.Name == "kill" {
		result, transition, killErr := currentSession.applyDeveloperKillCommand()
		if killErr != nil {
			r.registry.mutex.Unlock()
			return nil, false, fmt.Errorf("eventKillApply: %w", killErr)
		}
		binding := currentSession.binding
		generation := currentSession.generation
		r.registry.sessions[packet.Address.String()] = currentSession
		r.registry.mutex.Unlock()
		packets, publishErr := r.damage.publishAreaResults(
			packet, packet.Address.String(), generation, currentSession.deployedObjectID,
			packet.SourceTime, binding, result, transition, nil, false,
		)
		if publishErr != nil {
			return nil, false, fmt.Errorf("eventKillPublish: %w", publishErr)
		}
		r.logger.Printf("RakNet developer kill defeated %d enemies for %s", len(result), packet.Address)
		return packets, true, nil
	}
	if command.Name == "reset" && currentSession.securityTransfer != nil {
		currentSession.securityTransfer.Stop()
		currentSession.securityTransfer = nil
	}
	if command.Name == "reset" {
		r.registry.clearActionLeasesLocked(
			packet.Address.String(), currentSession.transportGeneration,
		)
	}
	packets, applyErr := currentSession.applyDeveloperEventCommand(
		command, r.now(), packet.SourceTime,
	)
	if applyErr != nil {
		r.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("eventApply: %w", applyErr)
	}
	if command.Name == "reset" {
		resetGameplayPeerRuntime(
			&currentSession, r.modifierPool, r.effectPool,
		)
	}
	restartedEnemyPlans := make([]zonenpc.SpawnPlan, 0)
	if command.Name == "reset" && currentSession.zone != nil {
		syncErr := currentSession.syncZoneHero()
		if syncErr != nil {
			r.logger.Printf(
				"RakNet developer reset hero resync skipped for %s: %v",
				packet.Address, syncErr,
			)
		} else {
			restartedEnemy, restartErr := currentSession.zone.RestartHeroTarget(
				currentSession.binding.UserID, currentSession.generation,
				currentSession.deployedObjectID,
			)
			if restartErr != nil {
				r.logger.Printf(
					"RakNet developer reset enemy restart skipped for %s: %v",
					packet.Address, restartErr,
				)
			} else {
				for _, enemy := range restartedEnemy {
					restartedEnemyPlans = append(
						restartedEnemyPlans, enemy.Plan,
					)
				}
			}
		}
	}
	generation := currentSession.generation
	r.registry.sessions[packet.Address.String()] = currentSession
	r.registry.mutex.Unlock()
	if command.Name == "reset" {
		restartedEnemyPackets, restartErr := r.damage.npc.scheduleFirstActions(
			packet, packet.Address.String(), generation, restartedEnemyPlans,
			packet.SourceTime,
		)
		if restartErr != nil {
			r.logger.Printf(
				"RakNet developer reset retained player recovery for %s while NPC restart failed: %v",
				packet.Address, restartErr,
			)
		} else {
			packets = append(packets, restartedEnemyPackets...)
		}
	}
	r.logger.Printf("RakNet developer event %q accepted for %s", command.Name, packet.Address)
	return packets, true, nil
}

func (r gameplayPendingRuntime) spawnDeveloperNPC(
	packet raknet.Packet, queuedSession gameplayPeerSession, command game.PlayerEventCommand,
) ([][]byte, bool, error) {
	sessionKey := packet.Address.String()
	r.registry.mutex.Lock()
	currentSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && currentSession.generation == queuedSession.generation &&
		currentSession.binding.IsWarped && currentSession.zone != nil &&
		currentSession.zone.NPCs() != nil && currentSession.deployedObjectID != 0 &&
		!currentSession.isZoneTerminal()
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, false, errors.New("npcSpawnSession: unavailable")
	}
	director := currentSession.zone.DirectorDefinition()
	profile, isProfileFound := developerNPCProfile(director, command.NounName)
	if !isProfileFound {
		r.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("npcSpawnProfile: unavailable %q", command.NounName)
	}
	profile = zoneboss.NormalizeFinalBossProfile(command.NounName, profile)
	position := game.Vec3(currentSession.playerPosition)
	position.X += 5
	plans, err := currentSession.zone.AssignSpawnPlanIDs([]zonenpc.SpawnPlan{{
		NounName: command.NounName, Position: position,
		IsCaptain:          strings.Contains(strings.ToLower(command.NounName), "_captain"),
		IsRewardSuppressed: true, NPCProfile: profile,
	}})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("npcSpawnAllocate: %w", err)
	}
	plan := plans[0]
	targetObjectID := currentSession.deployedObjectID
	packets, err := npcraknet.TargetedSpawn(plan, targetObjectID)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("npcSpawnMarshal: %w", err)
	}
	err = currentSession.zone.NPCs().Add(plans, targetObjectID)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("npcSpawnAdd: %w", err)
	}
	err = currentSession.zone.PublishNPCSpawn(zoneprojection.NPCSpawn{
		Plans: plans, TargetObjectID: targetObjectID,
	}, currentSession.binding.UserID, currentSession.generation)
	if err != nil {
		rollbackErr := currentSession.zone.NPCs().RollbackAdd(plans)
		r.registry.mutex.Unlock()
		return nil, false, fmt.Errorf(
			"npcSpawnPublish: %w", errors.Join(err, rollbackErr),
		)
	}
	generation := currentSession.generation
	r.registry.sessions[sessionKey] = currentSession
	r.registry.mutex.Unlock()
	actionPackets, actionErr := r.damage.npc.scheduleFirstActions(
		packet, sessionKey, generation, plans, packet.SourceTime,
	)
	if actionErr != nil {
		r.logger.Printf(
			"RakNet developer NPC spawn retained object=%d noun=%q for %s while first action failed: %v",
			plan.ObjectID, plan.NounName, packet.Address, actionErr,
		)
	} else {
		packets = append(packets, actionPackets...)
	}
	r.logger.Printf(
		"RakNet developer NPC spawn accepted object=%d noun=%q for %s",
		plan.ObjectID, plan.NounName, packet.Address,
	)
	return packets, true, nil
}

func developerNPCProfile(
	director game.CampaignDirector, nounName string,
) (game.CampaignNPCProfile, bool) {
	for _, pool := range director.Pools {
		for _, entry := range pool.Entries {
			if !strings.EqualFold(entry.NounName, nounName) {
				continue
			}
			profile, isValid := normalizeDeveloperNPCProfile(entry.NPCProfile)
			if isValid {
				return profile, true
			}
		}
	}
	for _, entry := range director.StandaloneBossEntries {
		if !strings.EqualFold(entry.NounName, nounName) {
			continue
		}
		profile, isValid := normalizeDeveloperNPCProfile(entry.NPCProfile)
		if isValid {
			return profile, true
		}
	}
	profile, isFound := director.NPCProfilesByNoun[strings.ToLower(nounName)]
	if isFound {
		normalizedProfile, isValid := normalizeDeveloperNPCProfile(profile)
		if isValid {
			return normalizedProfile, true
		}
	}
	nounStem := nounName
	if len(nounName) > len(".Noun") &&
		strings.EqualFold(nounName[len(nounName)-len(".Noun"):], ".Noun") {
		nounStem = nounName[:len(nounName)-len(".Noun")]
	}
	for _, rankSuffix := range []string{"_2.Noun", "_3.Noun"} {
		profile, isFound = director.NPCProfilesByNoun[strings.ToLower(nounStem+rankSuffix)]
		if !isFound {
			continue
		}
		normalizedProfile, isValid := normalizeDeveloperNPCProfile(profile)
		if isValid {
			return normalizedProfile, true
		}
	}
	captainIndex := strings.Index(strings.ToLower(nounStem), "_captain")
	if captainIndex <= 0 {
		return game.CampaignNPCProfile{}, false
	}
	parentStem := nounStem[:captainIndex]
	for _, parentSuffix := range []string{".Noun", "_2.Noun", "_3.Noun"} {
		profile, isFound = director.NPCProfilesByNoun[strings.ToLower(parentStem+parentSuffix)]
		if !isFound {
			continue
		}
		normalizedProfile, isValid := normalizeDeveloperNPCProfile(profile)
		if isValid {
			return normalizedProfile, true
		}
	}
	return game.CampaignNPCProfile{}, false
}

func normalizeDeveloperNPCProfile(
	profile game.CampaignNPCProfile,
) (game.CampaignNPCProfile, bool) {
	if !profile.IsKnown || !profile.IsTargetable || profile.IsPlayerPet {
		return game.CampaignNPCProfile{}, false
	}
	// Several packaged base nouns retain targetability and identity while their
	// inherited combat fields are zero. A small rewardless zoo actor is safer
	// than rejecting that valid client noun outside ordinary campaign play.
	if profile.HitPoint <= 0 {
		profile.HitPoint = 24
	}
	if profile.PowerPoint <= 0 {
		profile.PowerPoint = 75
	}
	if profile.GraphicsScale <= 0 {
		profile.GraphicsScale = 1
	}
	if profile.FootprintRadius <= 0 {
		profile.FootprintRadius = 0.5
	}
	if profile.PlayerCountHealthScale <= 0 {
		profile.PlayerCountHealthScale = 1
	}
	return profile, true
}

func (r gameplayPendingRuntime) completeDeveloperTutorialVictory(
	ctx context.Context, packet raknet.Packet, queuedSession gameplayPeerSession,
) ([][]byte, bool, error) {
	err := r.gameplayJoin.MarkTutorialComplete(
		ctx, int64(queuedSession.binding.UserID), queuedSession.binding.GameID,
	)
	if err != nil {
		return nil, false, fmt.Errorf("tutorialVictoryMark: %w", err)
	}
	r.registry.mutex.RLock()
	currentSession, isFound := r.registry.sessions[packet.Address.String()]
	isCurrent := isFound && currentSession.generation == queuedSession.generation &&
		currentSession.binding.Mode == game.ModeTutorial &&
		!currentSession.isZoneTerminal()
	r.registry.mutex.RUnlock()
	if !isCurrent {
		rollbackErr := r.gameplayJoin.RollbackTutorialComplete(
			ctx, int64(queuedSession.binding.UserID), queuedSession.binding.GameID,
		)
		if rollbackErr != nil {
			return nil, false, fmt.Errorf("tutorialVictorySession: %w", rollbackErr)
		}
		return nil, false, errors.New("tutorial victory session replaced")
	}
	alertPacket, alertErr := marshalTutorialHordeAlert(
		tutorialHordeDefeatedClientEventID, queuedSession.deployedObjectID,
	)
	if alertErr != nil {
		rollbackErr := r.gameplayJoin.RollbackTutorialComplete(
			ctx, int64(queuedSession.binding.UserID), queuedSession.binding.GameID,
		)
		return nil, false, fmt.Errorf(
			"tutorialVictoryAlert: %w", errors.Join(alertErr, rollbackErr),
		)
	}
	step := developerTutorialVictoryStep{
		registry: r.registry, logger: r.logger,
		sessionKey: packet.Address.String(), generation: queuedSession.generation,
	}
	producer := raknet.ScheduledPacketProducer{
		Delay: tutorialHordeResultDelay, Produce: step.produce,
	}
	producers := r.registry.producerGuard.scheduledProducers(
		packet.Address.String(), []raknet.ScheduledPacketProducer{producer},
	)
	cancel, scheduleErr := packet.Autonomous().ScheduleProducers(producers)
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("tutorial victory cancellation unavailable")
	}
	if scheduleErr == nil {
		r.logger.Printf(
			"RakNet developer tutorial victory scheduled completion for %s delay_ms=%d",
			packet.Address, tutorialHordeResultDelay.Milliseconds(),
		)
		return [][]byte{alertPacket}, true, nil
	}
	completionPackets, completionErr := step.produce()
	if completionErr != nil {
		rollbackErr := r.gameplayJoin.RollbackTutorialComplete(
			ctx, int64(queuedSession.binding.UserID), queuedSession.binding.GameID,
		)
		return nil, false, fmt.Errorf(
			"tutorialVictoryFallback: %w",
			errors.Join(scheduleErr, completionErr, rollbackErr),
		)
	}
	return append([][]byte{alertPacket}, completionPackets...), true, nil
}

type developerTutorialVictoryStep struct {
	registry   *gameplaySessionRegistry
	logger     *log.Logger
	sessionKey string
	generation uint64
}

func (s developerTutorialVictoryStep) produce() ([][]byte, error) {
	s.registry.mutex.Lock()
	peerSession, isFound := s.registry.sessions[s.sessionKey]
	isCurrent := isFound && peerSession.generation == s.generation &&
		peerSession.binding.Mode == game.ModeTutorial && !peerSession.isZoneTerminal()
	if !isCurrent {
		s.registry.mutex.Unlock()
		return nil, nil
	}
	completionPacket, bossObjectID, err := peerSession.applyDeveloperTutorialVictoryCommand()
	if err == nil {
		s.registry.sessions[s.sessionKey] = peerSession
	}
	s.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("tutorialVictoryApply: %w", err)
	}
	if s.logger != nil {
		s.logger.Printf(
			"RakNet developer tutorial victory published boss completion boss=%d for %s; awaiting Return to Ship",
			bossObjectID, s.sessionKey,
		)
	}
	return [][]byte{completionPacket}, nil
}
func (r gameplayPendingRuntime) consumePlayerLevelUpdate(
	ctx context.Context, packet raknet.Packet,
) ([][]byte, bool, error) {
	r.registry.mutex.RLock()
	queuedSession, isSessionFound := r.registry.sessions[packet.Address.String()]
	isUpdateEligible := isSessionFound && queuedSession.stage.IsDungeon() &&
		queuedSession.dungeonSetup.IsCommitted()
	r.registry.mutex.RUnlock()
	if !isUpdateEligible {
		return nil, false, nil
	}
	update, isUpdateFound, err := r.gameplayJoin.ConsumePlayerLevelUpdate(
		ctx, int64(queuedSession.binding.UserID), queuedSession.binding.GameID,
	)
	if err != nil {
		return nil, false, fmt.Errorf("levelConsume: %w", err)
	}
	if !isUpdateFound {
		return nil, false, nil
	}
	packetBytes, err := raknet.MarshalApplication(raknet.LabsPlayerProgressionMessage{
		Slot: uint8(queuedSession.binding.Slot), AvatarLevel: update.Level, AvatarXP: update.XP,
	})
	if err != nil {
		return nil, false, fmt.Errorf("levelMarshal: %w", err)
	}
	r.registry.mutex.Lock()
	currentSession, isCurrentFound := r.registry.sessions[packet.Address.String()]
	isCurrent := isCurrentFound && currentSession.generation == queuedSession.generation
	if isCurrent {
		currentSession.binding.AvatarLevel = update.Level
		currentSession.binding.AvatarXP = update.XP
		r.registry.sessions[packet.Address.String()] = currentSession
	}
	r.registry.mutex.Unlock()
	if !isCurrent {
		return nil, false, errors.New("levelSession: replaced")
	}
	r.logger.Printf("RakNet player level update level=%d slot=%d for %s",
		update.Level, queuedSession.binding.Slot, packet.Address)
	return [][]byte{packetBytes}, true, nil
}

func (r gameplayPendingRuntime) consumePlayerDNAUpdate(
	ctx context.Context, packet raknet.Packet,
) ([][]byte, bool, error) {
	r.registry.mutex.RLock()
	queuedSession, isSessionFound := r.registry.sessions[packet.Address.String()]
	isUpdateEligible := isSessionFound && queuedSession.stage.IsDungeon() &&
		queuedSession.dungeonSetup.IsCommitted()
	r.registry.mutex.RUnlock()
	if !isUpdateEligible {
		return nil, false, nil
	}
	update, isUpdateFound, err := r.gameplayJoin.ConsumePlayerDNAUpdate(
		ctx, int64(queuedSession.binding.UserID), queuedSession.binding.GameID,
	)
	if err != nil {
		return nil, false, fmt.Errorf("dnaConsume: %w", err)
	}
	if !isUpdateFound {
		return nil, false, nil
	}
	packetBytes, err := raknet.MarshalApplication(raknet.LabsPlayerDNAUpdateMessage{
		Slot: uint8(queuedSession.binding.Slot), DNA: update.DNA,
	})
	if err != nil {
		return nil, false, fmt.Errorf("dnaMarshal: %w", err)
	}
	r.registry.mutex.Lock()
	currentSession, isCurrentFound := r.registry.sessions[packet.Address.String()]
	isCurrent := isCurrentFound && currentSession.generation == queuedSession.generation
	if isCurrent {
		currentSession.binding.DNA = update.DNA
		r.registry.sessions[packet.Address.String()] = currentSession
	}
	r.registry.mutex.Unlock()
	if !isCurrent {
		return nil, false, errors.New("dnaSession: replaced")
	}
	r.logger.Printf("RakNet player DNA update dna=%d slot=%d for %s",
		update.DNA, queuedSession.binding.Slot, packet.Address)
	return [][]byte{packetBytes}, true, nil
}

func (r gameplayPendingRuntime) consumeItemPresentation(
	ctx context.Context, packet raknet.Packet,
) ([][]byte, bool, error) {
	r.registry.mutex.RLock()
	queuedSession, isSessionFound := r.registry.sessions[packet.Address.String()]
	isPresentationEligible := isSessionFound && queuedSession.stage.IsDungeon() &&
		queuedSession.dungeonSetup.IsCommitted() && queuedSession.deployedObjectID != 0
	r.registry.mutex.RUnlock()
	if !isPresentationEligible {
		return nil, false, nil
	}
	part, isPartFound, err := r.gameplayJoin.ConsumeItemPresentation(
		ctx, int64(queuedSession.binding.UserID), queuedSession.binding.GameID,
	)
	if err != nil {
		return nil, false, fmt.Errorf("itemConsume: %w", err)
	}
	if !isPartFound {
		return nil, false, nil
	}
	packetBytes, err := raknet.MarshalApplication(raknet.ServerEventMessage{
		Asset:    util.HashID("loot_acquired.ServerEventDef"),
		ObjectID: queuedSession.deployedObjectID, Position: queuedSession.playerPosition,
		ClientEventID: util.HashID("LootAwarded"),
		Loot: &raknet.ServerEventLoot{
			ReferenceID: part.ReferenceID, InstanceID: part.ID,
			RigblockID: part.RigblockAssetHash, SuffixAsset: part.SuffixAssetHash,
			PrefixAsset1: part.PrefixAssetHash, PrefixAsset2: part.PrefixSecondaryAssetHash,
			ItemLevel: int32(part.Level), Rarity: int32(part.Rarity),
			CreationTime: part.CreationDate,
		},
	})
	if err != nil {
		return nil, false, fmt.Errorf("itemMarshal: %w", err)
	}
	r.logger.Printf("RakNet summoned item presented id=%d reference=%d rigblock=%d for %s",
		part.ID, part.ReferenceID, part.RigblockAssetID, packet.Address)
	return [][]byte{packetBytes}, true, nil
}
func marshalApplicationMessages(
	messages []raknet.ApplicationMessage, operation string,
) ([][]byte, error) {
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: %w", operation, index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func campaignObjectiveRecord(record zoneobjective.Record) raknet.ObjectiveRecord {
	return raknet.ObjectiveRecord{
		ObjectiveID: record.ObjectiveID,
		State:       record.State,
		Token:       record.Token,
	}
}

func campaignObjectiveMessages(
	state *sim.ObjectiveState, playerIndex uint8, openingVoiceover uint32,
) ([]raknet.ApplicationMessage, error) {
	publication, err := zoneobjective.InitializationPublication(state, playerIndex)
	if err != nil {
		return nil, fmt.Errorf("campaignObjectivePublication: %w", err)
	}
	if publication == nil {
		return nil, nil
	}
	records := make([]raknet.ObjectiveRecord, 0, len(publication.Records))
	for _, objectiveRecord := range publication.Records {
		records = append(records, campaignObjectiveRecord(objectiveRecord))
	}
	return raknet.ObjectiveInitializationMessages(
		records,
		raknet.ObjectiveUpdatedMessage{
			ObjectiveID: publication.Update.ObjectiveID,
			PlayerIndex: publication.Update.PlayerIndex,
			Medal:       publication.Update.Medal,
			Voiceover:   openingVoiceover,
			Token:       publication.Update.Token,
		},
	), nil
}

func marshalCampaignObjectivesComplete(state *sim.ObjectiveState) ([]byte, error) {
	publication, err := zoneobjective.CompletionPublication(state)
	if err != nil {
		return nil, fmt.Errorf("campaignObjectiveCompletionPublication: %w", err)
	}
	message := raknet.ObjectivesCompleteMessage{
		Record: make([]raknet.ObjectiveRecord, 0, len(publication)),
	}
	for _, objectiveRecord := range publication {
		message.Record = append(message.Record, campaignObjectiveRecord(objectiveRecord))
	}
	packet, err := raknet.MarshalApplication(message)
	if err != nil {
		return nil, fmt.Errorf("campaignObjectiveCompleteMarshal: %w", err)
	}
	return packet, nil
}

type gameplayProjectionRuntime struct {
	registry *gameplaySessionRegistry
	logger   *log.Logger
}

func (r gameplayProjectionRuntime) publishEnemyDamage(
	sessionKey string, generation uint64, damage zonenpc.DamageEvent,
) error {
	if r.registry == nil || sessionKey == "" || generation == 0 {
		return errors.New("damage projection unavailable")
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil
	r.registry.mutex.RUnlock()
	if !isCurrent {
		return errors.New("damage projection session replaced")
	}
	err := peerSession.zone.RecordNPCDamage(context.Background(), damage)
	if err != nil {
		r.logger.Printf(
			"RakNet campaign damage objective omitted game=%d user=%d: %v",
			peerSession.binding.GameID, peerSession.binding.UserID, err,
		)
	}
	return nil
}

func (r gameplayProjectionRuntime) recordEnemyDamageObjective(
	sessionKey string, generation uint64, damage zonenpc.DamageEvent,
) error {
	if r.registry == nil || sessionKey == "" || generation == 0 {
		return errors.New("damage objective unavailable")
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil
	r.registry.mutex.RUnlock()
	if !isCurrent {
		return errors.New("damage objective session replaced")
	}
	return peerSession.zone.ApplyNPCDamageObjectives(
		context.Background(), damage,
	)
}

func (r gameplayProjectionRuntime) publishEnemyDeath(
	sessionKey string, generation uint64, death []zonenpc.DeathEvent,
) error {
	if len(death) == 0 {
		return nil
	}
	if r.registry == nil || sessionKey == "" || generation == 0 {
		return errors.New("death projection unavailable")
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil
	r.registry.mutex.RUnlock()
	if !isCurrent {
		return errors.New("death projection session replaced")
	}
	err := peerSession.zone.RecordNPCDeaths(
		context.Background(), death, peerSession.binding.UserID, generation,
	)
	if err != nil && r.logger != nil {
		r.logger.Printf(
			"RakNet projected enemy-death objective omitted session=%s: %v",
			sessionKey, err,
		)
	}
	return nil
}

func (r gameplayProjectionRuntime) drain(
	request raknet.Packet, sessionKey string, sourceTime uint64,
) ([][]byte, error) {
	if r.registry == nil || sessionKey == "" {
		return nil, nil
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	r.registry.mutex.RUnlock()
	if !isFound || peerSession.zone == nil {
		return nil, nil
	}
	if peerSession.isRejoinPending {
		return nil, nil
	}
	if peerSession.zone.IsProjectionBaselineRequired(
		peerSession.binding.UserID, peerSession.generation,
	) {
		packets, revision, err := marshalGameplayRejoinBaseline(
			peerSession, sourceTime,
		)
		if err != nil {
			return nil, fmt.Errorf("projectionBaseline: %w", err)
		}
		err = request.AfterResponseCommit(func() {
			r.commitProjectionDelivery(
				sessionKey, peerSession, revision, true,
			)
		})
		if err != nil {
			return nil, fmt.Errorf("projectionBaselineCommit: %w", err)
		}
		if r.logger != nil {
			r.logger.Printf(
				"RakNet projection overflow recovered by baseline game=%d user=%d packets=%d",
				peerSession.binding.GameID, peerSession.binding.UserID, len(packets),
			)
		}
		return packets, nil
	}
	event := peerSession.zone.PeekProjection(
		peerSession.binding.UserID, peerSession.generation,
	)
	packets := make([][]byte, 0, len(event))
	for index, current := range event {
		encoded, err := marshalCampaignProjection(current)
		if err != nil {
			if r.logger != nil {
				r.logger.Printf(
					"RakNet projection event discarded remote=%s index=%d sequence=%d: %v",
					sessionKey, index, current.Sequence, err,
				)
			}
			continue
		}
		packets = append(packets, encoded...)
	}
	if len(event) != 0 {
		sequence := event[len(event)-1].Sequence
		err := request.AfterResponseCommit(func() {
			r.commitProjectionDelivery(
				sessionKey, peerSession, sequence, false,
			)
		})
		if err != nil {
			return nil, fmt.Errorf("projectionCommit: %w", err)
		}
	}
	return packets, nil
}

func (r gameplayProjectionRuntime) commitProjectionDelivery(
	sessionKey string, expected gameplayPeerSession, sequence uint64,
	isBaseline bool,
) {
	if r.registry == nil || sessionKey == "" || expected.zone == nil ||
		(!isBaseline && sequence == 0) {
		return
	}
	r.registry.mutex.RLock()
	current, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound &&
		current.transportGeneration == expected.transportGeneration &&
		current.generation == expected.generation &&
		current.zone == expected.zone
	r.registry.mutex.RUnlock()
	if !isCurrent {
		if r.logger != nil {
			r.logger.Printf(
				"RakNet stale projection commit preserved remote=%s transport_generation=%d sequence=%d",
				sessionKey, expected.transportGeneration, sequence,
			)
		}
		return
	}
	isCommitted := false
	if isBaseline {
		isCommitted = expected.zone.ResetProjectionThrough(
			expected.binding.UserID, expected.generation, sequence,
		)
	} else {
		isCommitted = expected.zone.CommitProjection(
			expected.binding.UserID, expected.generation, sequence,
		)
	}
	if isCommitted {
		return
	}
	err := expected.zone.RequireProjectionBaseline(
		expected.binding.UserID, expected.generation,
	)
	if r.logger != nil {
		r.logger.Printf(
			"RakNet projection cursor repaired remote=%s user=%d generation=%d sequence=%d baseline=%t error=%v",
			sessionKey, expected.binding.UserID, expected.generation,
			sequence, isBaseline, err,
		)
	}
}

func marshalCampaignProjection(event zoneprojection.Event) ([][]byte, error) {
	switch event.Kind {
	case zoneprojection.EventObjective:
		update := event.Objective
		packet, err := raknet.MarshalApplication(raknet.ObjectiveUpdatedMessage{
			ObjectiveID: update.ObjectiveID, PlayerIndex: update.PlayerIndex,
			Medal: update.Medal, Token: update.Token,
		})
		if err != nil {
			return nil, fmt.Errorf("objectiveMarshal: %w", err)
		}
		return [][]byte{packet}, nil
	case zoneprojection.EventNPCDamage:
		damage := event.Damage
		if damage.IsDamageImmune {
			packet, err := npcraknet.Immune(
				damage.SourceObjectID, damage.TargetObjectID,
			)
			if err != nil {
				return nil, fmt.Errorf("damageImmuneMarshal: %w", err)
			}
			return [][]byte{packet}, nil
		}
		flags := uint16(0x0001)
		if damage.IsCritical {
			flags |= 0x0008
		}
		eventPacket, err := raknet.MarshalApplication(raknet.DamageCombatEventMessage{
			Flags: flags, DeltaHealth: damage.Damage,
			TargetID: damage.TargetObjectID, SourceID: damage.SourceObjectID,
			IntegerHPChange: -int32(damage.Damage),
		})
		if err != nil {
			return nil, fmt.Errorf("damageEventMarshal: %w", err)
		}
		healthPacket, err := raknet.MarshalApplication(raknet.CombatantDataDeltaMessage{
			ObjectID: damage.TargetObjectID, HitPoints: damage.HitPoint,
			IsHitPointChanged: true,
		})
		if err != nil {
			return nil, fmt.Errorf("damageHealthMarshal: %w", err)
		}
		return [][]byte{eventPacket, healthPacket}, nil
	case zoneprojection.EventNPCDeath:
		return marshalCampaignDeathProjection(event.Death)
	case zoneprojection.EventNPCSpawn:
		spawn := event.Spawn
		if spawn.IsDormant {
			packet, err := npcraknet.DormantSpawns(spawn.Plans)
			if err != nil {
				return nil, fmt.Errorf("enemyDormantSpawnMarshal: %w", err)
			}
			return packet, nil
		}
		packet, err := npcraknet.TargetedSpawns(
			spawn.Plans, spawn.TargetObjectID,
		)
		if err != nil {
			return nil, fmt.Errorf("enemySpawnMarshal: %w", err)
		}
		if spawn.IsBossAddPhase {
			phasePacket, err := bossraknet.AddPhase()
			if err != nil {
				return nil, fmt.Errorf("enemySpawnBossPhase: %w", err)
			}
			packet = append(packet, phasePacket)
		}
		if spawn.IsBossActive {
			for _, plan := range spawn.Plans {
				if plan.ObjectID == spawn.BossObjectID && isCampaignBossIntroDelayed(plan) {
					return packet, nil
				}
			}
			activePacket, err := bossraknet.Active(
				spawn.BossObjectID, spawn.IsFinalBoss,
			)
			if err != nil {
				return nil, fmt.Errorf("enemySpawnBossActive: %w", err)
			}
			packet = append(packet, activePacket)
		}
		return packet, nil
	case zoneprojection.EventNPCAction:
		action := event.Action
		switch action.Kind {
		case zonenpc.ActionEventBossActive:
			activePacket, err := bossraknet.Active(action.Plan.ObjectID, true)
			if err != nil {
				return nil, fmt.Errorf("bossIntroActive: %w", err)
			}
			return [][]byte{activePacket}, nil
		case zonenpc.ActionEventAggro:
			packet, err := npcraknet.FirstAggro(
				action.Plan, action.Timestamp,
			)
			if err != nil {
				return nil, fmt.Errorf("npcActionAggro: %w", err)
			}
			return packet, nil
		case zonenpc.ActionEventPursuit:
			packet, err := npcraknet.Pursuit(action.Plan)
			if err != nil {
				return nil, fmt.Errorf("npcActionPursuit: %w", err)
			}
			return packet, nil
		case zonenpc.ActionEventPursuitStep:
			packet, err := actionraknet.PursuitStep(
				action.Plan.ObjectID, action.Plan.SourcePosition,
			)
			if err != nil {
				return nil, fmt.Errorf("npcActionPursuitStep: %w", err)
			}
			return [][]byte{packet}, nil
		case zonenpc.ActionEventPursuitRedirect:
			var packet []byte
			var err error
			if action.Plan.Profile.Family == zonenpc.ActionProjectile {
				packet, err = npcraknet.ProjectileGoalUpdate(
					action.Plan.ObjectID, action.Plan.SourcePosition,
					action.Plan.TargetPosition, action.Plan.Profile.Range,
				)
			} else {
				packet, err = npcraknet.MovementGoalUpdate(
					action.Plan.ObjectID, action.Plan.TargetPosition,
				)
			}
			if err != nil {
				return nil, fmt.Errorf("npcActionPursuitRedirect: %w", err)
			}
			return [][]byte{packet}, nil
		case zonenpc.ActionEventAttack:
			if action.Plan.Profile.Family == zonenpc.ActionProjectile &&
				action.Plan.Profile.AnimationName == "" {
				return nil, nil
			}
			packet, err := npcraknet.AttackStart(zonenpc.AttackPlan{
				SourceObjectID:   action.Plan.ObjectID,
				TargetObjectID:   action.Plan.TargetObjectID,
				ActionGeneration: action.Plan.ActionGeneration,
				SourcePosition:   action.Plan.SourcePosition,
				TargetPosition:   action.Plan.TargetPosition,
				Profile:          action.Plan.Profile,
			}, action.Timestamp)
			if err != nil {
				return nil, fmt.Errorf("npcActionAttack: %w", err)
			}
			return packet, nil
		case zonenpc.ActionEventCancel:
			packet, err := npcraknet.CancelAction(
				action.Plan.ObjectID, action.Plan.SourcePosition,
				action.Timestamp,
			)
			if err != nil {
				return nil, fmt.Errorf("npcActionCancel: %w", err)
			}
			return packet, nil
		case zonenpc.ActionEventReturn:
			packet, err := actionraknet.PursuitStep(
				action.Plan.ObjectID, action.Plan.TargetPosition,
			)
			if err != nil {
				return nil, fmt.Errorf("npcActionReturn: %w", err)
			}
			return [][]byte{packet}, nil
		default:
			return nil, fmt.Errorf("npcActionKind: %d", action.Kind)
		}
	case zoneprojection.EventHeroResource:
		resource := event.Resource
		combatantPacket, err := raknet.MarshalApplication(
			raknet.CombatantDataDeltaMessage{
				ObjectID:  resource.ObjectID,
				HitPoints: resource.HitPoint, ManaPoints: resource.ManaPoint,
				IsHitPointChanged: true, IsManaPointChanged: true,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("heroResourceCombatant: %w", err)
		}
		squadPacket, err := raknet.MarshalApplication(
			raknet.LabsPlayerCharacterResourceMessage{
				PlayerSlot:    resource.PlayerSlot,
				CreatureIndex: resource.CreatureIndex,
				Resource: raknet.LabsCharacterResource{
					Health: resource.HitPoint, MaxHealth: resource.MaximumHitPoint,
					Mana: resource.ManaPoint, MaxMana: resource.MaximumManaPoint,
				},
			},
		)
		if err != nil {
			return nil, fmt.Errorf("heroResourceSquad: %w", err)
		}
		return [][]byte{combatantPacket, squadPacket}, nil
	case zoneprojection.EventCompanionDamage:
		damage := event.Companion
		flags := uint16(0x0001)
		if damage.IsDefeated {
			flags |= 0x0004
		}
		eventPacket, err := raknet.MarshalApplication(raknet.DamageCombatEventMessage{
			Flags: flags, DeltaHealth: damage.Damage,
			TargetID: damage.TargetObjectID, SourceID: damage.SourceObjectID,
			IntegerHPChange: -int32(damage.Damage),
		})
		if err != nil {
			return nil, fmt.Errorf("companionDamageEvent: %w", err)
		}
		healthPacket, err := raknet.MarshalApplication(raknet.CombatantDataDeltaMessage{
			ObjectID: damage.TargetObjectID, HitPoints: damage.HitPoint,
			IsHitPointChanged: true,
		})
		if err != nil {
			return nil, fmt.Errorf("companionDamageHealth: %w", err)
		}
		packet := [][]byte{eventPacket, healthPacket}
		if !damage.IsDefeated {
			return packet, nil
		}
		deletePacket, err := raknet.MarshalApplication(
			raknet.ObjectDeleteMessage{ObjectID: []uint32{damage.TargetObjectID}},
		)
		if err != nil {
			return nil, fmt.Errorf("companionDamageDelete: %w", err)
		}
		return append(packet, deletePacket), nil
	case zoneprojection.EventCompanionResource:
		resource := event.CompanionResource
		healthPacket, err := raknet.MarshalApplication(raknet.CombatantDataDeltaMessage{
			ObjectID: resource.ObjectID, HitPoints: resource.HitPoint,
			IsHitPointChanged: true,
		})
		if err != nil {
			return nil, fmt.Errorf("companionResourceHealth: %w", err)
		}
		attributePacket, err := raknet.MarshalApplication(
			raknet.AttributeDataUpdateMessage{
				ObjectID: resource.ObjectID,
				Value:    map[uint8]float32{4: resource.MaximumHitPoint},
			},
		)
		if err != nil {
			return nil, fmt.Errorf("companionResourceMaximum: %w", err)
		}
		return [][]byte{healthPacket, attributePacket}, nil
	case zoneprojection.EventNPCForcedMovement:
		movement := event.ForcedMovement
		packet, err := npcraknet.ForcedMovement(
			movement.Plan, movement.Destination, movement.Timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("npcForcedMovement: %w", err)
		}
		return packet, nil
	case zoneprojection.EventHeroMovement:
		return marshalZoneHeroMovement(event.HeroMovement)
	case zoneprojection.EventHeroRoster:
		packets, err := marshalZoneHeroRoster(event.HeroRoster)
		if err != nil {
			return nil, fmt.Errorf("heroRoster: %w", err)
		}
		return packets, nil
	case zoneprojection.EventHeroDeploy:
		deploy := event.HeroDeploy
		packets := make([][]byte, 0, squad.Size+1)
		for creatureIndex := uint32(0); creatureIndex < squad.Size; creatureIndex++ {
			packet, err := raknet.MarshalApplication(raknet.ObjectUpdateMessage{
				ObjectID:  zonehero.ObjectID(deploy.PlayerSlot, creatureIndex),
				PositionX: deploy.Position.X, PositionY: deploy.Position.Y,
				PositionZ: deploy.Position.Z,
				IsVisible: creatureIndex == deploy.CreatureIndex,
			})
			if err != nil {
				return nil, fmt.Errorf("heroDeployUpdate[%d]: %w", creatureIndex, err)
			}
			packets = append(packets, packet)
		}
		packet, err := raknet.MarshalApplication(
			raknet.PlayerCharacterDeployMessage{
				PlayerIndex:   uint8(deploy.PlayerSlot),
				CreatureIndex: deploy.CreatureIndex, ObjectID: deploy.ObjectID,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("heroDeploy: %w", err)
		}
		return append(packets, packet), nil
	case zoneprojection.EventHeroLeave:
		packet, err := raknet.MarshalApplication(
			raknet.ObjectDeleteMessage{ObjectID: event.HeroLeave.ObjectIDs},
		)
		if err != nil {
			return nil, fmt.Errorf("heroLeave: %w", err)
		}
		return [][]byte{packet}, nil
	default:
		return nil, fmt.Errorf("projectionKind: %d", event.Kind)
	}
}

func marshalCampaignDeathProjection(
	death zonenpc.DeathEvent,
) ([][]byte, error) {
	var message raknet.ApplicationMessage
	switch death.Kind {
	case zonenpc.DeathDamage:
		flags := uint16(0x0005)
		if death.IsCritical {
			flags |= 0x0008
		}
		eventPacket, err := raknet.MarshalApplication(raknet.DamageCombatEventMessage{
			Flags: flags, DeltaHealth: death.Damage,
			TargetID: death.TargetObjectID, SourceID: death.SourceObjectID,
			IntegerHPChange: death.IntegerHitPoint,
		})
		if err != nil {
			return nil, fmt.Errorf("deathDamageMarshal: %w", err)
		}
		return [][]byte{eventPacket}, nil
	case zonenpc.DeathHitPoint:
		message = raknet.CombatantDataDeltaMessage{
			ObjectID: death.TargetObjectID, HitPoints: death.HitPoint,
			IsHitPointChanged: true,
		}
	case zonenpc.DeathTargetable:
		message = raknet.AgentBlackboardUpdateMessage{
			ObjectID: death.TargetObjectID, IsTargetable: death.IsTargetable,
		}
	case zonenpc.DeathAnimation:
		state := uint32(0)
		if death.AnimationName != "" {
			state = util.HashID(death.AnimationName)
		}
		message = raknet.SetAnimationStateMessage{
			ObjectID: death.TargetObjectID, State: state,
			Timestamp: death.Timestamp, Scale: 1,
		}
	case zonenpc.DeathEffect:
		asset := uint32(0)
		if death.EffectName != "" {
			asset = util.HashID(death.EffectName)
		}
		message = raknet.AttachedEffectMessage{
			Slot: death.EffectSlot, Asset: asset,
			ObjectID:           death.TargetObjectID,
			IsForceAttached:    death.EffectName != "",
			IsRemovalRequested: death.IsEffectStopped,
			IsHardStop:         death.IsEffectStopped,
		}
	case zonenpc.DeathMovementStop:
		message = raknet.ObjectPlayerMoveMessage{
			ObjectID: death.TargetObjectID, GoalFlags: 0x20,
			GoalPosition: raknet.Vector3{
				X: death.Position.X, Y: death.Position.Y, Z: death.Position.Z,
			},
		}
	case zonenpc.DeathCollision:
		message = raknet.ObjectCollisionUpdateMessage{
			ObjectID:           death.TargetObjectID,
			IsCollisionEnabled: death.IsCollisionEnabled,
		}
	case zonenpc.DeathDelete:
		message = raknet.ObjectDeleteMessage{
			ObjectID: []uint32{death.TargetObjectID},
		}
	case zonenpc.DeathGraphics:
		message = raknet.SetObjectGFXStateMessage{
			ObjectID: death.TargetObjectID, State: death.GraphicsState,
			Timestamp: death.Timestamp,
		}
	case zonenpc.DeathPositionedEffect:
		message = raknet.PositionedEffectMessage{
			Asset: util.HashID(death.EffectName),
			Position: raknet.Vector3{
				X: death.Position.X, Y: death.Position.Y, Z: death.Position.Z,
			},
		}
	default:
		return nil, fmt.Errorf("deathKind: %d", death.Kind)
	}
	packet, err := raknet.MarshalApplication(message)
	if err != nil {
		return nil, fmt.Errorf("deathMarshal: %w", err)
	}
	return [][]byte{packet}, nil
}

func campaignCharacterBeam(creature game.GameplayCreature, isBeamIn bool) string {
	direction := "out"
	if isBeamIn {
		direction = "in"
	}
	switch creature.ElementType {
	case "bio":
		return "character_beam_" + direction + "_bio"
	case "necro":
		return "character_beam_" + direction + "_necro"
	case "cyber":
		return "character_beam_" + direction + "_cyber"
	case "chrono":
		return "character_beam_" + direction + "_spacetime"
	default:
		return "character_beam_" + direction + "_plasma_electric"
	}
}

func stopGameplayPeerSession(
	peerSession gameplayPeerSession, modifierInstancePool *modifierPool,
	effectPool *attachedEffectPool,
) {
	leaveGameplayPeerMembership(peerSession)
	stopGameplayPeerRuntime(peerSession, modifierInstancePool, effectPool)
}

func leaveGameplayPeerMembership(peerSession gameplayPeerSession) {
	if peerSession.zone == nil {
		return
	}
	peerSession.zone.Leave(
		peerSession.binding.UserID, peerSession.generation,
	)
}

// stopGameplayPeerRuntime quiesces connection-local gameplay work without
// removing the player from zone-owned result voting. Campaign Beam Out uses
// this boundary until Continue, Cash Out, or connection teardown ends the
// membership.
func stopGameplayPeerRuntime(
	peerSession gameplayPeerSession, modifierInstancePool *modifierPool,
	effectPool *attachedEffectPool,
) {
	peerSession.basicSequenceSession().ReleaseHeld()
	peerSession.campaignPlayerPursuitSession().Cancel()
	peerSession.resetAbilityRelease()
	if effectPool != nil {
		if peerSession.lightspeedEffectObjectID != 0 {
			effectPool.Release(
				peerSession.lightspeedEffectObjectID,
				peerSession.lightspeedEffectSlot,
			)
		}
		effectPool.ReleaseObject(peerSession.deployedObjectID)
	}
	peerSession.stopCampaignPlasmaModifiers(modifierInstancePool)
	peerSession.stopCampaignNPCModifiers(modifierInstancePool)
	peerSession.stopCampaignNPCProjectiles()
	for _, instanceID := range peerSession.passiveModifierInstance {
		if instanceID != 0 && modifierInstancePool != nil {
			_ = modifierInstancePool.Release(instanceID)
		}
	}
	if peerSession.campaignSchedule != nil {
		peerSession.campaignSchedule.StopAll()
	}
	if peerSession.teleporterHandoff != nil {
		peerSession.teleporterHandoff.Stop()
	}
	if peerSession.securityTransfer != nil {
		peerSession.securityTransfer.Stop()
	}
	if peerSession.campaignUnlockPresentation != nil {
		peerSession.campaignUnlockPresentation.StopAll()
	}
	for _, deathRun := range peerSession.enemyDeaths {
		deathRun.Stop()
	}
	if peerSession.basicAttack != nil {
		peerSession.basicAttack.Stop()
	}
	for _, run := range peerSession.spawnModifiers {
		run.Stop()
	}
	for _, attack := range peerSession.sageAttacks {
		attack.Stop()
	}
	for _, attack := range peerSession.heroBurstAttacks {
		attack.Stop()
	}
	for _, trap := range peerSession.heroTraps {
		trap.Stop()
	}
	if peerSession.heroDrain != nil {
		peerSession.heroDrain.Stop()
	}
	if peerSession.heroTimedArea != nil {
		peerSession.heroTimedArea.Stop()
		peerSession.heroTimedArea.ReleaseEffect()
	}
	if peerSession.heroChannelArea != nil {
		peerSession.heroChannelArea.Stop()
	}
	if peerSession.heroQuantumBlink != nil {
		peerSession.heroQuantumBlink.Stop()
	}
	for _, run := range peerSession.heroStatusAreas {
		run.Stop()
	}
	if peerSession.heroInfection != nil {
		peerSession.heroInfection.Stop()
	}
	if peerSession.heroHealingTicks != nil {
		peerSession.heroHealingTicks.Stop()
	}
	for _, run := range peerSession.heroAuraAreas {
		run.Stop()
	}
	for _, run := range peerSession.heroProjectileRuns {
		run.Stop()
	}
	if peerSession.heroCharge != nil {
		peerSession.heroCharge.Stop()
	}
	if peerSession.fireTempestPassive != nil {
		peerSession.fireTempestPassive.Stop()
	}
	if peerSession.energySentinelPassive != nil {
		peerSession.energySentinelPassive.Stop()
	}
	for _, attack := range peerSession.tossAttacks {
		attack.stop()
	}
	for _, attack := range peerSession.cloudLobAttacks {
		if attack != nil && attack.cancel != nil {
			attack.cancel()
		}
	}
	if peerSession.sphereAttack != nil {
		peerSession.sphereAttack.Stop()
	}
	if peerSession.sagePassive != nil {
		_, _ = peerSession.sagePassive.Stop()
	}
	stopSummonCompanionAttacks(peerSession.sagePassiveActivations)
	_, _ = peerSession.stopFieldMedicDrone()
	_, _ = peerSession.stopBeastPet()
	_, _ = peerSession.stopHeroSummons()
	_, _ = peerSession.stopFireTempestActive()
	_, _ = peerSession.stopPlasmaSentinelActive()
	_, _ = peerSession.stopTrapperStealth()
	if peerSession.treeOfLifeRun != nil {
		peerSession.treeOfLifeRun.Stop()
	}
	if peerSession.enrageRun != nil {
		peerSession.enrageRun.Cancel()
		peerSession.enrageRun.ReleaseEffect()
		if modifierInstancePool != nil {
			_ = modifierInstancePool.Release(peerSession.enrageRun.InstanceID())
		}
	}
	if peerSession.ghostFormRun != nil {
		peerSession.ghostFormRun.Cancel()
		peerSession.ghostFormRun.ReleaseEffect()
		if modifierInstancePool != nil {
			_ = modifierInstancePool.Release(peerSession.ghostFormRun.InstanceID())
		}
	}
	if peerSession.heroModifierRun != nil {
		peerSession.heroModifierRun.Cancel()
		if peerSession.heroModifierRun.isShadowStealth {
			acquired, stealthPacket, err := peerSession.setShadowRavagerStealth(
				peerSession.heroModifierRun.objectID, false,
			)
			_ = acquired
			_ = stealthPacket
			if err != nil {
				// Runtime teardown cannot publish recovery packets; removing the
				// peer immediately retires the same zone hero authority.
			}
		}
		peerSession.heroModifierRun.Remove(&peerSession)
		if modifierInstancePool != nil {
			_ = modifierInstancePool.Release(peerSession.heroModifierRun.instanceID)
		}
	}
	_, _ = stopMissileFlak(&peerSession, modifierInstancePool)
	_, _ = stopPoisonNovaCooldown(&peerSession, modifierInstancePool)
	peerSession.stopFieldMedicHeroBuffs(modifierInstancePool)
	peerSession.stopFieldMedicCompanionBuffs(modifierInstancePool)
	if peerSession.plasmaWreathRun != nil {
		_, _ = peerSession.plasmaWreathRun.Stop()
	}
	if peerSession.rideCancel != nil {
		peerSession.rideCancel()
	}
	if peerSession.obeliskCancel != nil {
		peerSession.obeliskCancel()
	}
	if peerSession.obeliskRun != nil {
		peerSession.obeliskRun.Stop()
	}
	if peerSession.movementContactCancel != nil {
		peerSession.movementContactCancel()
	}
}

func resetGameplayPeerRuntime(
	peerSession *gameplayPeerSession, modifierInstancePool *modifierPool,
	effectPool *attachedEffectPool,
) {
	if peerSession == nil {
		return
	}
	stopGameplayPeerRuntime(*peerSession, modifierInstancePool, effectPool)
	peerSession.zoneEffectPresentation = zoneEffectPresentation{}
	peerSession.zonePresentationRuntime = zonePresentationRuntime{}
	peerSession.controlledHeroPresentation = controlledHeroPresentation{}
	peerSession.enemySilenceExpiresAt = time.Time{}
	peerSession.enemySleepExpiresAt = time.Time{}
	peerSession.enemyStunExpiresAt = time.Time{}
	peerSession.enemyStunTargetObjectID = 0
	peerSession.enemyRootExpiresAt = time.Time{}
	peerSession.enemyRootTargetObjectID = 0
	peerSession.enemyFearExpiresAt = time.Time{}
	peerSession.enemyFearTargetObjectID = 0
	peerSession.passiveKillStack = [squad.Size]uint32{}
	peerSession.passiveReductionStack = [squad.Size]uint32{}
	peerSession.passiveReductionExpiresAt = [squad.Size]time.Time{}
	peerSession.fireRavagerBasicCount = [squad.Size]uint32{}
	peerSession.tcShieldAmount = [squad.Size]float32{}
	peerSession.tcShieldReadyAt = [squad.Size]time.Time{}
	peerSession.tcShieldEffectSlot = [squad.Size]uint8{}
	peerSession.isTCShieldEffectAttached = [squad.Size]bool{}
	peerSession.lightspeedEffectObjectID = 0
	peerSession.lightspeedEffectSlot = 0
	peerSession.lightspeedEffectTier = 0
}

func helloPlayerResponses(ctx context.Context, gameplayJoin *game.GameplayJoin, packet raknet.Packet, logger *log.Logger) ([][]byte, game.GameplayBinding, error) {
	request, err := raknet.DecodeHelloPlayerRequest(packet.Payload)
	if err != nil {
		return nil, game.GameplayBinding{}, fmt.Errorf("helloDecode: %w", err)
	}
	binding, err := gameplayJoin.Execute(ctx, int64(request.UserID))
	if err != nil {
		return nil, game.GameplayBinding{}, fmt.Errorf("helloJoin: %w", err)
	}
	if binding.Slot > 255 {
		return nil, game.GameplayBinding{}, fmt.Errorf("helloSlot: slot %d exceeds build-103 width", binding.Slot)
	}
	hello := raknet.HelloPlayerMessage{
		GameplayIndex: uint8(binding.Slot),
		Address: net.IPv4(
			byte(binding.Endpoint.IP>>24),
			byte(binding.Endpoint.IP>>16),
			byte(binding.Endpoint.IP>>8),
			byte(binding.Endpoint.IP),
		),
		Port: binding.Endpoint.Port,
	}
	helloPacket, err := raknet.MarshalApplication(hello)
	if err != nil {
		return nil, game.GameplayBinding{}, fmt.Errorf("helloMarshal: %w", err)
	}
	logger.Printf("RakNet gameplay user_id=%d joined slot=%d endpoint=%s:%d", request.UserID, binding.Slot, hello.Address, hello.Port)
	return [][]byte{helloPacket}, binding, nil
}

func marshalZoneSetup(binding game.GameplayBinding) ([]byte, error) {
	levelAsset := strings.TrimSuffix(binding.Level, "_v2")
	message := raknet.PrepareForStartMessage{
		Level:      util.HashID(levelAsset + ".Level"),
		Markerset:  util.HashID(levelAsset + "_ai.Markerset"),
		PlayerMask: binding.PlayerMask,
		LevelIndex: uint32(binding.Slot),
	}
	packet, err := raknet.MarshalApplication(message)
	if err != nil {
		return nil, fmt.Errorf("setupMarshal: %w", err)
	}
	return packet, nil
}

func marshalZoneChainVote(
	binding game.GameplayBinding, director game.CampaignDirector,
) ([]byte, error) {
	enemyNoun, err := zonepreview.Nouns(director, binding.Level, binding.Difficulty)
	if err != nil {
		if !binding.IsWarped {
			return nil, fmt.Errorf("votePreview: %w", err)
		}
		enemyNoun = zonepreview.HashNouns(zonepreview.InitialNounNames())
	}
	presentation := zonepreview.CampaignEntryPresentation(
		binding.ChainLevelIndex, binding.ChainProgression,
	)
	message := raknet.ChainSelectionMessage{
		Level:         util.HashID(binding.Level + ".Level"),
		Difficulty:    binding.ChainLevelIndex,
		TimeRemaining: float32(zoneresult.VoteDuration.Seconds()),
		EnemyNoun:     enemyNoun,
		LevelNoun:     [2]uint32{util.HashID("ZelemBoss.Noun"), util.HashID("NomadSpacetimeAgent.Noun")},
		IntroMovie:    presentation.CurrentMovie,
		IntroVoice:    presentation.CurrentVoice,
		NextMovie:     presentation.NextMovie,
	}
	packet, err := raknet.MarshalApplication(message)
	if err != nil {
		return nil, fmt.Errorf("voteMarshal: %w", err)
	}
	return packet, nil
}

func marshalZonePlayerMove(objectID uint32, goal raknet.Vector3) ([][]byte, error) {
	messages := []raknet.ApplicationMessage{
		raknet.ObjectPlayerMoveMessage{ObjectID: objectID, GoalFlags: 0x01, GoalPosition: goal},
		raknet.LocomotionUnreliableMessage{ObjectID: objectID, GoalPosition: goal},
	}
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("playerMove[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func marshalZonePlayerStop(objectID uint32, position raknet.Vector3) ([][]byte, error) {
	if objectID == 0 || !isFiniteZonePosition(position) {
		return nil, errors.New("invalid player stop")
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{
		ObjectID: objectID, GoalFlags: 0x20, GoalPosition: position,
	})
	if err != nil {
		return nil, fmt.Errorf("playerStop: %w", err)
	}
	return [][]byte{packet}, nil
}

func marshalZonePlayerAttackPose(
	objectID uint32, position raknet.Vector3,
	facing raknet.Vector3, targetPosition raknet.Vector3, targetObjectID uint32,
) ([][]byte, error) {
	if objectID == 0 || !isFiniteZonePosition(position) ||
		!isFiniteZonePosition(facing) || !isFiniteZonePosition(targetPosition) {
		return nil, errors.New("invalid player attack pose")
	}
	goalFlags := uint32(0x02)
	if targetObjectID != 0 {
		goalFlags |= 0x40
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{
		ObjectID: objectID, GoalFlags: goalFlags, GoalPosition: position,
		Facing: facing, TargetPosition: targetPosition,
		TargetObjectID: targetObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("playerAttackPose: %w", err)
	}
	return [][]byte{packet}, nil
}

type campaignOpeningPopulation struct {
	decisions  []campaignPopulationDecision
	spawnPlans []zonenpc.SpawnPlan
	aggroPlans []zonenpc.SpawnPlan
	packets    [][]byte
}

func (e campaignPopulationRuntime) activateOpening(
	packet raknet.Packet, sessionKey string, generation uint64, setupEpoch uint64,
	entryPosition raknet.Vector3,
) (campaignOpeningPopulation, error) {
	result := campaignOpeningPopulation{
		decisions:  make([]campaignPopulationDecision, 0, 1),
		spawnPlans: make([]zonenpc.SpawnPlan, 0, 4),
		aggroPlans: make([]zonenpc.SpawnPlan, 0, 4),
		packets:    make([][]byte, 0),
	}
	e.registry.mutex.Lock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation {
		e.registry.mutex.Unlock()
		return result, errors.New("opening reservation stale")
	}
	transition, err := peerSession.zone.PrimePopulation(zone.PopulationRequest{
		Position: zonePosition(entryPosition),
	})
	if err == nil {
		result.decisions = transition.Decisions
		result.spawnPlans = transition.SpawnPlans
		for _, enemy := range transition.Acquired {
			result.aggroPlans = append(result.aggroPlans, enemy.Plan)
		}
		result.packets, err = npcraknet.Population(transition)
	}
	if err == nil && len(result.spawnPlans) != 0 {
		err = peerSession.zone.PublishNPCSpawn(zoneprojection.NPCSpawn{
			Plans: result.spawnPlans, IsDormant: true,
		}, peerSession.binding.UserID, generation)
	}
	if err == nil && !peerSession.dungeonSetup.Commit(setupEpoch) {
		err = errors.New("opening setup commit stale")
	}
	if err == nil {
		e.registry.sessions[sessionKey] = peerSession
	}
	e.registry.mutex.Unlock()
	if err != nil {
		return result, fmt.Errorf("openingCommit: %w", err)
	}
	firstActionPlans, introductionObjectIDs := campaignPopulationFirstActionPlans(
		result.spawnPlans, result.aggroPlans,
	)
	firstActionPackets, err := e.npc.scheduleFirstActionsWithIntroductions(
		packet, sessionKey, generation, firstActionPlans, packet.SourceTime,
		introductionObjectIDs,
	)
	if err != nil {
		return result, fmt.Errorf("openingFirstAction: %w", err)
	}
	result.packets = append(result.packets, firstActionPackets...)
	for _, decision := range result.decisions {
		spawnCount := max(
			decision.ProvisionalCount, int(decision.Wanderer.ClumpSize),
		)
		e.logger.Printf(
			"RakNet campaign opening population decision for %s marker_set=%q locus=%d component=%d floor=%t ambush=%t count=%d",
			packet.Address, decision.MarkerSetName, decision.LocusID,
			decision.NavigationComponentID, decision.IsFloorIntroduction,
			decision.IsAmbush, spawnCount,
		)
	}
	return result, nil
}

func zonePosition(position raknet.Vector3) game.Vec3 {
	return game.Vec3{X: position.X, Y: position.Y, Z: position.Z}
}

type gameplaySetupRuntime struct {
	registry      *gameplaySessionRegistry
	population    campaignPopulationRuntime
	preparation   campaignPreparation
	program       Programs
	passive       fireTempestPassiveRuntime
	energyPassive energySentinelPassiveRuntime
	damage        campaignDamageRuntime
	modifierPool  *modifierPool
	now           func() time.Time
	logger        *log.Logger
}

func (r gameplaySetupRuntime) publishCampaign(
	packet raknet.Packet, peerSession gameplayPeerSession, setupEpoch uint64,
) ([][]byte, bool, error) {
	peerSession.initializeCampaignResourceMaximums()
	entryPosition := campaignEntryPosition(
		peerSession.zone.DirectorDefinition(), peerSession.binding.Slot,
	)
	if peerSession.binding.Mode == game.ModeTutorial {
		entryPosition = tutorialPlayerSpawnPosition
	}
	var err error
	var restoredHero zonecheckpoint.Hero
	isHeroCheckpointFound := false
	isHeroRestored := false
	peerSession.deployedCreatureIndex = 0
	peerSession.deployedObjectID = zonehero.ObjectID(peerSession.binding.Slot, 0)
	if restoredSquad, isFound := peerSession.zone.RestoredSquad(
		peerSession.binding.UserID,
	); isFound {
		peerSession.squad, err = restorePlayableSquad(restoredSquad.State)
		if err != nil {
			return nil, false, fmt.Errorf("pingCampaignSquadRestore: %w", err)
		}
		entryPosition = raknet.Vector3{
			X: restoredSquad.Position.X,
			Y: restoredSquad.Position.Y,
			Z: restoredSquad.Position.Z,
		}
		peerSession.deployedCreatureIndex = peerSession.squad.DeployedIndex()
		peerSession.deployedObjectID = zonehero.ObjectID(
			peerSession.binding.Slot, peerSession.deployedCreatureIndex,
		)
		restoredHero, isHeroCheckpointFound = peerSession.zone.RestoredHero(
			peerSession.binding.UserID,
		)
		isHeroRestored = isHeroCheckpointFound && restoredHero.HitPoint > 0 &&
			restoredHero.CreatureIndex == peerSession.deployedCreatureIndex
		if isHeroRestored {
			entryPosition = raknet.Vector3{
				X: restoredHero.Position.X,
				Y: restoredHero.Position.Y,
				Z: restoredHero.Position.Z,
			}
			_, err = peerSession.squad.SetHitPoints(
				restoredHero.CreatureIndex, restoredHero.HitPoint,
			)
			if err != nil {
				return nil, false, fmt.Errorf("pingCampaignHeroHitPoint: %w", err)
			}
			err = peerSession.squad.SetManaPoints(
				restoredHero.CreatureIndex, restoredHero.ManaPoint,
			)
			if err != nil {
				return nil, false, fmt.Errorf("pingCampaignHeroManaPoint: %w", err)
			}
		}
	}
	playerMotion, err := zoneaction.NewMotion(toSimPosition(entryPosition), r.now())
	if err != nil {
		return nil, false, fmt.Errorf("pingCampaignMovement: %w", err)
	}
	peerSession.playerPosition = entryPosition
	peerSession.playerMotion = playerMotion
	peerSession.passiveStationarySince[peerSession.deployedCreatureIndex] = r.now()
	peerSession.startTCShieldRecharge(peerSession.deployedCreatureIndex, r.now())
	if peerSession.squad == nil {
		peerSession.squad, err = newCampaignSquad(peerSession.binding.Creatures)
		if err != nil {
			return nil, false, fmt.Errorf("pingCampaignSquad: %w", err)
		}
	}
	for creatureIndex := uint32(0); creatureIndex < squad.Size; creatureIndex++ {
		character, isFound := peerSession.squad.Character(creatureIndex)
		if !isFound || !character.IsAvailable {
			continue
		}
		peerSession.binding.Creatures[creatureIndex].HitPoint = character.HitPoints
		peerSession.binding.Creatures[creatureIndex].PowerPoint = character.ManaPoints
	}
	err = peerSession.syncZoneSquadCheckpoint()
	if err != nil {
		return nil, false, fmt.Errorf("pingCampaignSquadCheckpoint: %w", err)
	}
	fieldMedicPackets := make([][]byte, 0)
	if peerSession.binding.Creatures[peerSession.deployedCreatureIndex].PassiveAbility ==
		util.HashID("FieldMedicPassive") {
		firstObjectID, objectIDErr := peerSession.reserveCampaignProjectileIDs(1, 2000)
		if objectIDErr != nil {
			r.logger.Printf(
				"RakNet Field Medic opening drone reservation skipped for %s: %v",
				packet.Address, objectIDErr,
			)
		} else {
			spawnPackets, spawnErr := peerSession.spawnFieldMedicDrone(
				r.program, firstObjectID,
			)
			if spawnErr != nil {
				r.logger.Printf(
					"RakNet Field Medic opening drone spawn skipped for %s: %v",
					packet.Address, spawnErr,
				)
			} else {
				fieldMedicPackets = append(fieldMedicPackets, spawnPackets...)
			}
		}
	}
	r.registry.mutex.Lock()
	currentSession, isFound := r.registry.sessions[packet.Address.String()]
	isCurrent := isFound && currentSession.generation == peerSession.generation &&
		currentSession.dungeonSetup.IsReserved(setupEpoch)
	if isCurrent {
		err = peerSession.syncZoneHero()
		if err == nil {
			r.registry.sessions[packet.Address.String()] = peerSession
		}
	}
	r.registry.mutex.Unlock()
	if err != nil {
		return nil, false, fmt.Errorf("pingCampaignHero: %w", err)
	}
	if !isCurrent {
		return nil, false, nil
	}
	if isHeroRestored && restoredHero.IsStealthed {
		err = peerSession.zone.Hero().SetStealthed(
			peerSession.binding.UserID, peerSession.generation,
			peerSession.deployedObjectID, true,
		)
		if err != nil {
			return nil, false, fmt.Errorf("pingCampaignHeroStealth: %w", err)
		}
	}
	err = peerSession.zone.UpdateMemberRoster(
		peerSession.binding.UserID, peerSession.generation,
		peerSession.binding.Roster(),
	)
	if err != nil {
		return nil, false, fmt.Errorf("pingCampaignRoster: %w", err)
	}
	response, err := marshalCampaignDungeonSetup(
		peerSession.binding, peerSession.zone.ScriptObjectPlans(),
		peerSession.passiveModifierInstance, entryPosition, packet.SourceTime,
		campaignElapsedMilliseconds(peerSession.zone, r.now()),
		peerSession.deployedCreatureIndex, false,
	)
	if err != nil {
		return nil, false, fmt.Errorf("pingCampaignSetup: %w", err)
	}
	for index, use := range peerSession.zone.Script().SnapshotUses() {
		usePackets, marshalErr := objectraknet.ScriptUse(use)
		if marshalErr != nil {
			return nil, false, fmt.Errorf("pingCampaignScriptUse[%d]: %w", index, marshalErr)
		}
		response = append(response, usePackets...)
	}
	memberPackets, err := marshalOtherZoneHeroRosters(
		peerSession.zone, peerSession.binding.UserID,
	)
	if err != nil {
		return nil, false, fmt.Errorf("pingCampaignMembers: %w", err)
	}
	response = append(response, memberPackets...)
	beamPackets, err := heroraknet.BeamIn(
		peerSession.deployedObjectID,
		campaignCharacterBeam(
			peerSession.binding.Creatures[peerSession.deployedCreatureIndex], true,
		),
		zonePosition(entryPosition), packet.SourceTime,
	)
	if err != nil {
		return nil, false, fmt.Errorf("pingCampaignBeamIn: %w", err)
	}
	response = append(response, beamPackets...)
	response = append(response, fieldMedicPackets...)
	isCheckpointBaseline := peerSession.zone.IsRestored() ||
		peerSession.binding.IsCheckpointRestore
	fixturePlans := peerSession.zone.FixturePlans()
	if isCheckpointBaseline {
		fixturePlans = nil
	}
	fixturePackets, err := npcraknet.DormantSpawns(fixturePlans)
	if err != nil {
		return nil, false, fmt.Errorf("pingCampaignFixtures: %w", err)
	}
	response = append(response, fixturePackets...)
	// The objective update owns the first-pass HELIX cue. Publish it only after
	// the hero and level fixtures exist so the client cannot discard the cue
	// while it is still constructing the campaign scene.
	openingVoiceover := zonepreview.CampaignEntryPresentation(
		peerSession.binding.ChainLevelIndex,
		peerSession.binding.ChainProgression,
	).CurrentVoice
	objectiveMessages, err := campaignObjectiveMessages(
		peerSession.zone.Objective().State(), uint8(peerSession.binding.Slot),
		openingVoiceover,
	)
	if err != nil {
		return nil, false, fmt.Errorf("pingCampaignObjective: %w", err)
	}
	for index, message := range objectiveMessages {
		objectivePacket, marshalErr := raknet.MarshalApplication(message)
		if marshalErr != nil {
			return nil, false, fmt.Errorf("pingCampaignObjectiveMarshal[%d]: %w", index, marshalErr)
		}
		response = append(response, objectivePacket)
	}
	tutorialTeleporterPackets, err := peerSession.tutorialTeleporterInitialState()
	if err != nil {
		return nil, false, fmt.Errorf("pingTutorialTeleporterState: %w", err)
	}
	response = append(response, tutorialTeleporterPackets...)
	securitySnapshot := zonesecurity.Snapshot{}
	if peerSession.zone.Security() != nil {
		securitySnapshot = peerSession.zone.Security().Snapshot()
	}
	securityPackets := [][]byte(nil)
	if isCheckpointBaseline {
		securityPackets, err = securityraknet.SnapshotState(securitySnapshot)
	} else {
		securityPackets, err = securityraknet.InitialState(securitySnapshot.ObjectID)
	}
	if err != nil {
		return nil, false, fmt.Errorf("pingCampaignSecurityState: %w", err)
	}
	response = append(response, securityPackets...)
	campaignTeleporterPackets, err := peerSession.campaignTeleporterInitialState()
	if err != nil {
		return nil, false, fmt.Errorf("pingCampaignTeleporterState: %w", err)
	}
	response = append(response, campaignTeleporterPackets...)
	if isCheckpointBaseline {
		restoredPackets, restoreErr := marshalRestoredZoneNPCs(peerSession.zone)
		if restoreErr != nil {
			return nil, false, fmt.Errorf("pingCampaignRestoreNPC: %w", restoreErr)
		}
		response = append(response, restoredPackets...)
		r.logger.Printf(
			"RakNet durable checkpoint baseline restored game=%d user=%d level=%q npc=%d",
			peerSession.binding.GameID, peerSession.binding.UserID,
			peerSession.binding.Level, len(peerSession.zone.NPCs().Snapshots()),
		)
	}
	opening, err := r.population.activateOpening(
		packet, packet.Address.String(), peerSession.generation, setupEpoch, entryPosition,
	)
	if err != nil {
		return nil, false, fmt.Errorf("pingCampaignOpening: %w", err)
	}
	response = append(response, opening.packets...)
	droneAttackPackets, err := r.damage.startCompanionAttacks(
		packet, packet.Address.String(), peerSession.generation, packet.SourceTime,
	)
	if err != nil {
		return nil, false, fmt.Errorf("pingCampaignDroneAttack: %w", err)
	}
	response = append(response, droneAttackPackets...)
	passivePacket, err := r.passive.start(
		packet, packet.Address.String(), peerSession.generation,
	)
	if err != nil {
		return nil, false, fmt.Errorf("pingCampaignPassive: %w", err)
	}
	if passivePacket != nil {
		response = append(response, passivePacket)
	}
	err = r.energyPassive.start(
		packet, packet.Address.String(), peerSession.generation,
	)
	if err != nil {
		return nil, false, fmt.Errorf("pingCampaignEnergyPassive: %w", err)
	}
	err = startTrapperStealth(
		r.damage, packet, packet.Address.String(), peerSession.generation,
	)
	if err != nil {
		return nil, false, fmt.Errorf("pingTrapperStealth: %w", err)
	}
	lightspeedPackets, err := startLightspeedPassive(
		r.damage, packet, packet.Address.String(), peerSession.generation,
	)
	if err != nil {
		return nil, false, fmt.Errorf("pingLightspeedPassive: %w", err)
	}
	response = append(response, lightspeedPackets...)
	err = r.startCampaignClock(packet, peerSession)
	if err != nil {
		return nil, false, fmt.Errorf("pingCampaignClock: %w", err)
	}
	r.logger.Printf(
		"RakNet campaign dungeon state sent to %s level=%q",
		packet.Address, peerSession.binding.Level,
	)
	heroActor, isHeroFound := peerSession.zone.Hero().Snapshot(
		peerSession.binding.UserID, peerSession.generation,
	)
	if isHeroFound {
		rosterPackets, rosterErr := marshalZoneHeroRoster(
			zoneprojection.HeroRoster{
				UserID: peerSession.binding.UserID, PlayerSlot: peerSession.binding.Slot,
				Roster: peerSession.binding.Roster(), Position: heroActor.Position,
				CreatureIndex: heroActor.CreatureIndex,
			},
		)
		if rosterErr != nil {
			r.logger.Printf(
				"RakNet co-op hero roster fanout omitted user=%d: %v",
				peerSession.binding.UserID, rosterErr,
			)
		} else {
			recipients := r.registry.queuePeerPresentation(
				gameplayProducerIdentityFromSession(
					packet.Address.String(), peerSession, true,
				),
				rosterPackets,
			)
			r.logger.Printf(
				"RakNet co-op hero roster fanned out game=%d user=%d slot=%d position=(%.3f,%.3f,%.3f) peers=%d",
				peerSession.binding.GameID, peerSession.binding.UserID,
				peerSession.binding.Slot, heroActor.Position.X,
				heroActor.Position.Y, heroActor.Position.Z, recipients,
			)
		}
	} else {
		r.logger.Printf(
			"RakNet co-op hero roster projection omitted user=%d: hero unavailable",
			peerSession.binding.UserID,
		)
	}
	if isHeroCheckpointFound {
		peerSession.zone.ConsumeRestoredHero(peerSession.binding.UserID)
	}
	return response, true, nil
}

func restorePlayableSquad(state squad.State) (*squad.Session, error) {
	restored, err := squad.Restore(state)
	if err != nil {
		return nil, fmt.Errorf("squadRestore: %w", err)
	}
	deployed, isFound := restored.DeployedCharacter()
	if isFound && deployed.IsAvailable && deployed.HitPoints > 0 {
		return restored, nil
	}
	for creatureIndex := uint32(0); creatureIndex < squad.Size; creatureIndex++ {
		character, isCharacterFound := restored.Character(creatureIndex)
		if !isCharacterFound || !character.IsAvailable || character.HitPoints <= 0 {
			continue
		}
		err = restored.Deploy(creatureIndex)
		if err != nil {
			return nil, fmt.Errorf("squadDeploy[%d]: %w", creatureIndex, err)
		}
		return restored, nil
	}
	return nil, errors.New("squad has no living hero")
}

func marshalRestoredZoneNPCs(currentZone *zone.Zone) ([][]byte, error) {
	if currentZone == nil || currentZone.NPCs() == nil {
		return nil, errors.New("restored npc session unavailable")
	}
	packets := make([][]byte, 0)
	for index, npc := range currentZone.NPCs().LiveSnapshots() {
		if !npc.IsPublished {
			continue
		}
		spawnPackets, err := npcraknet.DormantSpawns([]zonenpc.SpawnPlan{npc.FacingSpawnPlan()})
		if err != nil {
			return nil, fmt.Errorf("restoredNPCSpawn[%d]: %w", index, err)
		}
		resourcePacket, err := raknet.MarshalApplication(
			raknet.CombatantDataUpdateMessage{
				ObjectID: npc.Plan.ObjectID, HitPoints: npc.HitPoint,
				ManaPoints: npc.ManaPoint,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("restoredNPCResource[%d]: %w", index, err)
		}
		facingPackets, facingErr := npcraknet.RestoreFacing(npc.Plan.ObjectID, npc.Plan.Position, npc.Facing)
		if facingErr != nil {
			return nil, fmt.Errorf("restoredNPCFacing[%d]: %w", index, facingErr)
		}
		packets = append(packets, spawnPackets...)
		packets = append(packets, facingPackets...)
		packets = append(packets, resourcePacket)
	}
	return packets, nil
}

type gameplaySetupReservation struct {
	runtime         gameplaySetupRuntime
	sessionKey      string
	generation      uint64
	epoch           uint64
	previousSession gameplayPeerSession
}

func (r gameplaySetupReservation) rollbackUnlessCommitted(isCommitted *bool) {
	if isCommitted != nil && *isCommitted {
		return
	}
	r.runtime.registry.mutex.Lock()
	defer r.runtime.registry.mutex.Unlock()
	peerSession, isFound := r.runtime.registry.sessions[r.sessionKey]
	isCurrent := isFound && peerSession.generation == r.generation &&
		peerSession.dungeonSetup.IsReserved(r.epoch)
	if !isCurrent {
		return
	}
	previousSession := r.previousSession
	previousSession.dungeonSetup = peerSession.dungeonSetup
	previousSession.dungeonSetup.Rollback(r.epoch)
	r.runtime.registry.sessions[r.sessionKey] = previousSession
}

type campaignPrepareFallbackStep struct {
	preparation campaignPreparation
	logger      *log.Logger
	sessionKey  string
	generation  uint64
}

func (s campaignPrepareFallbackStep) produce() ([][]byte, error) {
	packets, isPrepared, err := s.preparation.prepare(
		s.sessionKey, s.generation, true,
	)
	if err != nil {
		return nil, fmt.Errorf("fallbackPrepare: %w", err)
	}
	if !isPrepared {
		return nil, nil
	}

	s.logger.Printf(
		"RakNet chain countdown fallback prepared player for %s", s.sessionKey,
	)
	return packets, nil
}

func (r gameplaySetupRuntime) handle(
	ctx context.Context, packet raknet.Packet,
) ([][]byte, error) {
	_, err := raknet.DecodeTimestamp(packet.Payload)
	if err != nil {
		return nil, fmt.Errorf("debugPing: %w", err)
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[packet.Address.String()]
	isArenaWaiting := isFound && peerSession.binding.Mode == game.ModeArena &&
		!peerSession.stage.IsDungeon()
	if isArenaWaiting {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	isRejoinNeeded := isFound && peerSession.isRejoinPending &&
		peerSession.stage.IsDungeon() && peerSession.zone != nil
	if isRejoinNeeded {
		r.registry.mutex.Unlock()
		return r.publishRejoin(packet, peerSession)
	}
	isCheckpointPrepareNeeded := isFound && peerSession.stage.CanResumePrepare()
	if isCheckpointPrepareNeeded {
		r.registry.mutex.Unlock()
		packets, isPrepared, prepareErr := r.preparation.prepare(
			packet.Address.String(), peerSession.generation, false,
		)
		if prepareErr != nil {
			return nil, fmt.Errorf("checkpointPrepare: %w", prepareErr)
		}
		if isPrepared {
			r.logger.Printf(
				"RakNet checkpoint resume loading started for %s level=%q",
				packet.Address, peerSession.binding.Level,
			)
		}
		return packets, nil
	}
	isVoteNeeded := isFound && peerSession.stage.CanSendChainVote()
	r.registry.mutex.Unlock()
	packets, isPublished, publishErr := r.publishDungeon(ctx, packet)
	if publishErr != nil {
		return nil, fmt.Errorf("pingZone: %w", publishErr)
	}
	if isPublished {
		return packets, nil
	}
	if !isVoteNeeded {
		return nil, nil
	}
	r.registry.mutex.Lock()
	peerSession, isFound = r.registry.sessions[packet.Address.String()]
	isVoteNeeded = isFound && peerSession.stage.CanSendChainVote()
	if isVoteNeeded {
		peerSession.stage.MarkChainVoteSent()
		r.registry.sessions[packet.Address.String()] = peerSession
	}
	r.registry.mutex.Unlock()
	if !isVoteNeeded {
		return nil, nil
	}
	return r.publishVote(ctx, packet, peerSession)
}

func (r gameplaySetupRuntime) publishDungeon(
	ctx context.Context, packet raknet.Packet,
) ([][]byte, bool, error) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[packet.Address.String()]
	isDungeonSetupNeeded := isFound && peerSession.stage.IsDungeon() &&
		peerSession.dungeonSetup.CanReserve()
	if !isDungeonSetupNeeded {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	previousSession := peerSession
	setupEpoch, isReserved := peerSession.dungeonSetup.Reserve()
	if !isReserved {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	reservation := gameplaySetupReservation{
		runtime: r, sessionKey: packet.Address.String(),
		generation: peerSession.generation, epoch: setupEpoch,
		previousSession: previousSession,
	}
	isCommitted := false
	defer reservation.rollbackUnlessCommitted(&isCommitted)
	if peerSession.binding.Mode == game.ModeArena {
		r.registry.sessions[packet.Address.String()] = peerSession
		r.registry.mutex.Unlock()
		packets, isPublished, publishErr := r.publishArena(
			packet, peerSession, setupEpoch,
		)
		if publishErr != nil {
			return nil, false, fmt.Errorf("arenaPublish: %w", publishErr)
		}
		isCommitted = isPublished
		return packets, isPublished, nil
	}
	if peerSession.zone == nil {
		initializeErr := r.preparation.initialize(
			ctx, peerSession.binding, &peerSession,
		)
		if initializeErr != nil {
			r.registry.mutex.Unlock()
			return nil, false, fmt.Errorf("dungeonZone: %w", initializeErr)
		}
	}
	r.registry.sessions[packet.Address.String()] = peerSession
	r.registry.mutex.Unlock()
	packets, isPublished, publishErr := r.publishCampaign(
		packet, peerSession, setupEpoch,
	)
	if publishErr != nil {
		return nil, false, fmt.Errorf("dungeonPublish: %w", publishErr)
	}
	isCommitted = isPublished
	return packets, isPublished, nil
}

func (r gameplaySetupRuntime) publishArena(
	packet raknet.Packet, peerSession gameplayPeerSession, setupEpoch uint64,
) ([][]byte, bool, error) {
	peerSession.initializeCampaignResourceMaximums()
	peerSession.deployedCreatureIndex = 0
	peerSession.deployedObjectID = zonehero.ObjectID(peerSession.binding.Slot, 0)
	var err error
	if peerSession.squad == nil {
		peerSession.squad, err = newCampaignSquad(peerSession.binding.Creatures)
		if err != nil {
			return nil, false, fmt.Errorf("arenaSquad: %w", err)
		}
	}
	entryPlacement := arenaSpawnPlacement(
		r.program, peerSession.binding, peerSession.binding.Team, 0,
	)
	entryPosition := entryPlacement.position
	peerSession.playerMotion, err = zoneaction.NewMotion(
		toSimPosition(entryPosition), r.now(),
	)
	if err != nil {
		return nil, false, fmt.Errorf("arenaMotion: %w", err)
	}
	peerSession.playerPosition = entryPosition
	peerSession.passiveStationarySince[peerSession.deployedCreatureIndex] = r.now()
	peerSession.startTCShieldRecharge(peerSession.deployedCreatureIndex, r.now())
	packets, err := marshalCampaignDungeonSetup(
		peerSession.binding, nil, peerSession.passiveModifierInstance,
		entryPosition, packet.SourceTime, 0,
		peerSession.deployedCreatureIndex, true,
	)
	if err != nil {
		return nil, false, fmt.Errorf("arenaSetup: %w", err)
	}
	r.registry.mutex.RLock()
	peerBindings := make([]game.GameplayBinding, 0, 6)
	for sessionKey, candidate := range r.registry.sessions {
		if sessionKey == packet.Address.String() ||
			candidate.binding.GameID != peerSession.binding.GameID ||
			candidate.binding.Mode != game.ModeArena {
			continue
		}
		peerBindings = append(peerBindings, candidate.binding)
	}
	r.registry.mutex.RUnlock()
	slices.SortFunc(peerBindings, func(left, right game.GameplayBinding) int {
		return cmp.Compare(left.Slot, right.Slot)
	})
	for index, peerBinding := range peerBindings {
		peerPlacement := arenaSpawnPlacement(
			r.program, peerBinding, peerBinding.Team, 0,
		)
		peerPosition := peerPlacement.position
		rosterPackets, rosterErr := marshalZoneHeroRoster(zoneprojection.HeroRoster{
			UserID: peerBinding.UserID, PlayerSlot: peerBinding.Slot,
			Team: peerBinding.Team, Roster: peerBinding.Roster(),
			Position: zonePosition(peerPosition), CreatureIndex: 0,
		})
		if rosterErr != nil {
			return nil, false, fmt.Errorf("arenaRoster[%d]: %w", index, rosterErr)
		}
		packets = append(packets, rosterPackets...)
	}
	r.registry.mutex.Lock()
	currentSession, isFound := r.registry.sessions[packet.Address.String()]
	isCurrent := isFound && currentSession.generation == peerSession.generation &&
		currentSession.dungeonSetup.IsReserved(setupEpoch)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	peerSession.dungeonSetup = currentSession.dungeonSetup
	if !peerSession.dungeonSetup.Commit(setupEpoch) {
		r.registry.mutex.Unlock()
		return nil, false, errors.New("arena setup commit stale")
	}
	r.registry.sessions[packet.Address.String()] = peerSession
	r.registry.mutex.Unlock()
	countdownErr := r.registry.scheduleArenaCountdown(
		r.program, peerSession.binding.GameID,
	)
	if countdownErr != nil && r.logger != nil {
		r.logger.Printf(
			"RakNet Arena countdown unavailable game=%d: %v",
			peerSession.binding.GameID, countdownErr,
		)
	}
	if r.logger != nil {
		r.logger.Printf(
			"RakNet Arena heroes spawned game=%d user=%d slot=%d team=%d peers=%d",
			peerSession.binding.GameID, peerSession.binding.UserID,
			peerSession.binding.Slot, peerSession.binding.Team, len(peerBindings),
		)
	}
	return packets, true, nil
}

func (r gameplaySetupRuntime) publishVote(
	ctx context.Context, packet raknet.Packet, peerSession gameplayPeerSession,
) ([][]byte, error) {
	previewDirector, err := r.preparation.loadPreview(ctx, peerSession.binding)
	if err != nil {
		return nil, fmt.Errorf("pingPreview: %w", err)
	}
	votePacket, err := marshalZoneChainVote(peerSession.binding, previewDirector)
	if err != nil {
		return nil, fmt.Errorf("pingVote: %w", err)
	}
	r.logger.Printf("RakNet campaign initial chain vote sent to %s", packet.Address)
	return [][]byte{votePacket}, nil
}

// campaignPreparation owns director loading and the prepare transition shared
// by campaign levels. Level-specific differences stay in prepared director
// data instead of branching in the gameplay transport.
type campaignPreparation struct {
	setup        *game.CampaignSetup
	difficulty   sim.DifficultyCombatTuning
	session      *gameplaySessionRegistry
	zone         *zone.Registry
	navigation   NavigationSource
	program      Programs
	timer        zone.Timer
	checkpoint   zonecheckpoint.Repository
	gameplayJoin *game.GameplayJoin
	logger       *log.Logger
}

const campaignPopulationAggroRadius = float32(12)

type campaignPopulationDecision = zonepopulation.Decision

func newCampaignPopulationSession(
	director game.CampaignDirector, binding game.GameplayBinding,
	mesh *navigation.Mesh,
) (*zonepopulation.Session, error) {
	selectionID := binding.GameID
	if zoneunlock.IsFirstClear(binding) {
		selectionID = 0
	}
	seed := selectionID ^ (binding.Difficulty << 24) ^ 0x103
	var session *zonepopulation.Session
	var err error
	if zoneunlock.IsFirstClear(binding) {
		session, err = zonepopulation.NewFirstClearSessionWithNavigation(
			director, seed, mesh,
		)
	} else {
		session, err = zonepopulation.NewSessionWithNavigation(director, seed, mesh)
	}
	if err != nil {
		return nil, fmt.Errorf("populationSession: %w", err)
	}
	return session, nil
}

func isFiniteCampaignPopulationPosition(position game.Vec3) bool {
	return zonepopulation.IsFinitePosition(position)
}

func newCampaignNPCSession() *zonenpc.Session {
	return zonenpc.NewSession(
		zoneobject.ProjectileIDStart, campaignPopulationAggroRadius,
	)
}

func (p campaignPreparation) loadDirector(
	ctx context.Context, binding game.GameplayBinding,
) (game.CampaignDirector, error) {
	if p.setup == nil {
		return game.CampaignDirector{}, errors.New("campaign director unavailable")
	}
	director, err := p.setup.Execute(ctx, binding)
	if err != nil {
		return game.CampaignDirector{}, fmt.Errorf("directorLoad: %w", err)
	}
	if binding.Mode == game.ModeChain && !binding.IsWarped {
		director, err = zoneboss.PrepareNamedBossDirector(
			director, binding.ChainLevelIndex,
		)
		if err != nil {
			return game.CampaignDirector{}, fmt.Errorf("directorBossInclude: %w", err)
		}
	}
	if zonedifficulty.HasTuning(p.difficulty) {
		director, err = zonedifficulty.ProjectDirector(
			director, binding.Difficulty, binding.ParticipantCount,
			p.difficulty,
		)
		if err != nil {
			return game.CampaignDirector{}, fmt.Errorf("directorDifficulty: %w", err)
		}
	}
	if binding.Mode == game.ModeChain && !binding.IsWarped {
		err = zoneboss.ValidateNamedBossDirector(
			director, binding.ChainLevelIndex,
		)
		if err != nil {
			return game.CampaignDirector{}, fmt.Errorf("directorBoss: %w", err)
		}
	}
	return director, nil
}

func (p campaignPreparation) loadPreview(
	ctx context.Context, binding game.GameplayBinding,
) (game.CampaignDirector, error) {
	if binding.Mode == game.ModeTutorial ||
		strings.EqualFold(binding.Level, game.InitialChainLevel) {
		return game.CampaignDirector{}, nil
	}
	director, err := p.loadDirector(ctx, binding)
	if err != nil {
		return game.CampaignDirector{}, fmt.Errorf("previewDirector: %w", err)

	}
	return director, nil
}

func (p campaignPreparation) prepare(
	sessionKey string, generation uint64, isFallback bool,
) ([][]byte, bool, error) {
	p.session.mutex.RLock()
	peerSession, isFound := p.session.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation
	if !isCurrent {
		p.session.mutex.RUnlock()
		return nil, false, nil
	}
	type preparationMember struct {
		sessionKey  string
		generation  uint64
		binding     game.GameplayBinding
		setupPacket []byte
	}
	preparationMembers := make([]preparationMember, 0)
	for memberSessionKey, memberSession := range p.session.sessions {
		if memberSession.binding.GameID != peerSession.binding.GameID {
			continue
		}
		preparationMembers = append(preparationMembers, preparationMember{
			sessionKey: memberSessionKey,
			generation: memberSession.generation,
			binding:    memberSession.binding,
		})
	}
	p.session.mutex.RUnlock()
	for index := range preparationMembers {
		setupPacket, err := marshalZoneSetup(preparationMembers[index].binding)
		if err != nil {
			return nil, false, fmt.Errorf("prepareMarshal[%d]: %w", index, err)
		}
		preparationMembers[index].setupPacket = setupPacket
	}
	if len(preparationMembers) > 1 {
		err := p.gameplayJoin.ReserveCampaignLaunchHandoff(peerSession.binding.GameID)
		if err != nil {
			return nil, false, fmt.Errorf("prepareHandoff: %w", err)
		}
	}

	p.session.mutex.Lock()
	peerSession, isFound = p.session.sessions[sessionKey]
	isPrepared := isFound && peerSession.generation == generation &&
		peerSession.binding.GameID != 0 && peerSession.stage.BeginPrepare(isFallback)
	if !isPrepared {
		p.session.mutex.Unlock()
		return nil, false, nil
	}
	var requesterPacket []byte
	preparedMembers := 0
	for _, member := range preparationMembers {
		currentSession, isMemberFound := p.session.sessions[member.sessionKey]
		isMemberCurrent := isMemberFound &&
			currentSession.generation == member.generation &&
			currentSession.binding.GameID == peerSession.binding.GameID
		if !isMemberCurrent {
			continue
		}
		isMemberPrepared := member.sessionKey == sessionKey
		if isMemberPrepared {
			currentSession = peerSession
		} else {
			isMemberPrepared = currentSession.stage.BeginPrepare(isFallback)
		}
		if !isMemberPrepared {
			continue
		}
		if member.sessionKey == sessionKey {
			requesterPacket = member.setupPacket
		} else {
			currentSession.queuePackets([][]byte{member.setupPacket})
		}
		p.session.sessions[member.sessionKey] = currentSession
		preparedMembers++
	}
	p.session.mutex.Unlock()
	if len(requesterPacket) == 0 {
		return nil, false, nil
	}
	if p.logger != nil {
		p.logger.Printf(
			"RakNet chain preparation synchronized game=%d requester=%s members=%d fallback=%t",
			peerSession.binding.GameID, sessionKey, preparedMembers, isFallback,
		)
	}
	return [][]byte{requesterPacket}, true, nil
}

func (p campaignPreparation) initialize(
	ctx context.Context, binding game.GameplayBinding, peerSession *gameplayPeerSession,
) error {
	director, setupErr := p.loadDirector(ctx, binding)
	if setupErr != nil {
		return fmt.Errorf("statusChainDirector: %w", setupErr)
	}
	var campaignNav *navigation.Mesh
	if p.navigation != nil {
		campaignNav, setupErr = p.navigation.LoadCampaignNavigation(
			ctx, binding.Level,
		)
		if setupErr != nil {
			return fmt.Errorf("statusChainNavigation: %w", setupErr)
		}
	}
	directorSession, sessionErr := game.NewCampaignDirectorSession(director)
	if sessionErr != nil {
		return fmt.Errorf("statusChainDirectorSession: %w", sessionErr)
	}
	populationSession, sessionErr := newCampaignPopulationSession(
		director, binding, campaignNav,
	)
	if sessionErr != nil {
		return fmt.Errorf("statusChainPopulationSession: %w", sessionErr)
	}
	scriptRegistry, registryErr := game.NewCampaignScriptRegistry(director)
	if registryErr != nil {
		return fmt.Errorf("statusChainScriptRegistry: %w", registryErr)
	}
	contentSelectionID := binding.GameID
	if zoneunlock.IsFirstClear(binding) {
		contentSelectionID = 0
	}
	var scriptObjects []game.CampaignScriptObject
	var objectErr error
	if strings.EqualFold(binding.Level, game.InitialChainLevel) &&
		zoneunlock.IsFirstClear(binding) {
		scriptObjects, objectErr = director.InitialChainFirstClearInteractables()
	} else {
		scriptObjects, objectErr = zoneobjective.LevelScriptObjects(
			director, contentSelectionID,
		)
	}
	if objectErr != nil {
		return fmt.Errorf("statusChainScriptObjects: %w", objectErr)
	}
	firstObjectID := zonehero.FirstSharedObjectID()
	scriptObjectPlans, nextObjectID, planErr := zoneobject.PlanScripts(
		scriptRegistry, scriptObjects, firstObjectID,
	)
	if planErr != nil {
		return fmt.Errorf("statusChainScriptObjectPlans: %w", planErr)
	}
	tutorialCapsulePlans := make([]tutorialCapsulePlan, 0)
	if binding.Mode == game.ModeTutorial {
		tutorialCapsulePlans, nextObjectID, planErr = planTutorialCapsules(
			nextObjectID,
		)
		if planErr != nil {
			return fmt.Errorf("statusTutorialCapsulePlans: %w", planErr)
		}
	}
	fixturePlans := make([]zonenpc.SpawnPlan, 0)
	initialNPCPlans := make([]zonenpc.SpawnPlan, 0)
	var tutorialHorde *tutorialHordeSession
	hordeBarrierPlans := make(map[string][]zonebarrier.Plan)
	securityObjectID := [zonesecurity.RouteCount]uint32{}
	if binding.Mode == game.ModeTutorial {
		tutorialMarkers, markerErr := director.TutorialActors()
		if markerErr != nil {
			return fmt.Errorf("statusTutorialActors: %w", markerErr)
		}
		initialNPCPlans, nextObjectID, markerErr = zonenpc.PlanActors(
			tutorialMarkers, nextObjectID, zoneobject.ProjectileIDStart,
		)
		if markerErr != nil {
			return fmt.Errorf("statusTutorialActorPlans: %w", markerErr)
		}
		markerErr = attachTutorialActorActionProfiles(initialNPCPlans, p.program)
		if markerErr != nil {
			if p.logger != nil {
				p.logger.Printf(
					"RakNet tutorial actor action profile fallback: %v", markerErr,
				)
			}
		}
		tutorialHorde, nextObjectID, markerErr = planTutorialHorde(
			director, nextObjectID, p.program,
		)
		if markerErr != nil {
			return fmt.Errorf("statusTutorialHordePlans: %w", markerErr)
		}
	}
	if strings.EqualFold(binding.Level, game.InitialChainLevel) {
		fixtureMarkers := make([]game.CampaignDirectorMarker, 0)
		var fixtureErr error
		if zoneunlock.IsFirstClear(binding) {
			fixtureMarkers, fixtureErr = director.InitialChainFirstClearFixtures()
		} else {
			fixtureMarkers, fixtureErr = director.InitialChainFixtures(contentSelectionID)
		}
		if fixtureErr != nil {
			return fmt.Errorf("statusChainFixtures: %w", fixtureErr)
		}
		fixturePlans, nextObjectID, fixtureErr = zonenpc.PlanFixtures(
			fixtureMarkers, nextObjectID, zoneobject.ProjectileIDStart,
		)
		if fixtureErr != nil {
			return fmt.Errorf("statusChainFixturePlans: %w", fixtureErr)
		}
	}
	if binding.Mode == game.ModeChain {
		barrierSets, barrierErr := director.HordeBarriers()
		if barrierErr != nil {
			return fmt.Errorf("statusCampaignHordeBarriers: %w", barrierErr)
		}
		if len(barrierSets) != 0 {
			hordeBarrierPlans, nextObjectID, barrierErr = zonebarrier.PlanSets(
				barrierSets, nextObjectID,
			)
			if barrierErr != nil {
				return fmt.Errorf("statusCampaignHordeBarrierPlans: %w", barrierErr)
			}
		}
	}
	if strings.EqualFold(binding.Level, game.InitialChainLevel) {
		var securityErr error
		securityObjectID, nextObjectID, securityErr =
			zonesecurity.PlanObjectIDs(nextObjectID)
		if securityErr != nil {
			return fmt.Errorf("statusChainSecurityObjectIDs: %w", securityErr)
		}
	}
	objectiveSession, planErr := zoneobjective.NewSession(
		ctx, p.program.ObjectiveInput, scriptObjects, uint8(binding.Slot),
		binding.Mode,
	)
	if planErr != nil {
		return fmt.Errorf("statusChainObjective: %w", planErr)
	}
	objectiveProgress, planErr := zoneobjective.NewProgress(nil)
	if planErr != nil {
		return fmt.Errorf("statusChainObjectiveProgress: %w", planErr)
	}
	var creatureFootprints [squad.Size]float32
	for creatureIndex, creature := range binding.Creatures {
		if creatureIndex >= squad.Size {
			break
		}
		footprintRadius, footprintErr := p.program.FootprintRadiusByNoun(creature.Noun)
		if footprintErr != nil || footprintRadius <= 0 {
			footprintRadius = campaignSecurityBlitzFootprintFallback
		}
		creatureFootprints[creatureIndex] = footprintRadius
	}
	enemySession := newCampaignNPCSession()
	defenseErr := enemySession.ConfigureDefense(binding.Difficulty, p.program.Critical.RatingConversions)
	if defenseErr != nil {
		return fmt.Errorf("statusChainDefense: %w", defenseErr)
	}
	objectIDSession, objectIDErr := zoneobjectid.NewSession(
		nextObjectID, zoneobject.ProjectileIDStart,
	)
	if objectIDErr != nil {
		return fmt.Errorf("statusChainObjectID: %w", objectIDErr)
	}
	firstProjectileObjectID := max(nextObjectID, zoneobject.ProjectileIDStart)
	projectileIDSession, projectileIDErr := zoneobjectid.NewSession(
		firstProjectileObjectID, uint32(0xffffffff),
	)
	if projectileIDErr != nil {
		return fmt.Errorf("statusChainProjectileID: %w", projectileIDErr)
	}
	zoneRegistry := p.zone
	if zoneRegistry == nil {
		zoneRegistry = zone.NewRegistry()
	}
	var restoreSnapshot *zonecheckpoint.Snapshot
	if p.checkpoint != nil {
		checkpointSnapshot, isFound, checkpointErr := p.checkpoint.Load(
			ctx, uint64(binding.GameID),
		)
		if checkpointErr != nil {
			return fmt.Errorf("statusChainCheckpointLoad: %w", checkpointErr)
		}
		if isFound {
			checkpointErr = zonecheckpoint.Validate(
				checkpointSnapshot, uint64(binding.GameID), binding.Level,
				binding.Difficulty, binding.UserID,
			)
			if checkpointErr == nil {
				restoreSnapshot = &checkpointSnapshot
			} else {
				p.checkpoint.Discard(uint64(binding.GameID))
			}
		}
	}
	zone, _, zoneErr := zoneRegistry.Resolve(
		uint64(binding.GameID),
		zone.Member{
			UserID:             binding.UserID,
			PeerGeneration:     peerSession.generation,
			Slot:               binding.Slot,
			AbilityCount:       zoneunlock.InitialAbilityCount(binding),
			IsReplay:           binding.IsReplay,
			Roster:             binding.Roster(),
			CreatureFootprints: creatureFootprints,
		},
		zone.ZoneInfo{
			Level:               binding.Level,
			Difficulty:          binding.Difficulty,
			ChainLevelIndex:     binding.ChainLevelIndex,
			MemberLimit:         binding.MemberLimit,
			DirectorDefinition:  director,
			Navigation:          campaignNav,
			HordeBarrierPlans:   hordeBarrierPlans,
			ScriptObjects:       scriptObjects,
			ScriptObjectPlans:   scriptObjectPlans,
			InitialNPCPlans:     initialNPCPlans,
			FixturePlans:        fixturePlans,
			CatalystProgram:     p.program.CatalystUnlock,
			OverdriveProgram:    p.program.OverdriveUnlock,
			CrystalDefinitions:  p.program.CrystalDefinitions,
			CrystalLevelOffsets: p.program.CrystalLevelOffsets,
			Security:            zonesecurity.NewSession(securityObjectID),
			Effect:              zoneeffect.NewInventory(),
			NPCs:                enemySession,
			Hero:                zonehero.NewSession(),
			Companion:           zonecompanion.NewSession(),
			Interactable:        zoneinteract.NewUseSession(),
			Pickups:             zoneinteract.NewPickupRegistry(),
			PickupPayload:       zoneinteract.NewPickupPayloadRegistry(),
			Orbs:                zoneinteract.NewOrbRegistry(),
			Loot:                zoneloot.NewSession(),
			DNA:                 zoneloot.NewDNASession(),
			Population:          populationSession,
			Director:            directorSession,
			Script:              scriptRegistry,
			Encounter:           zoneencounter.NewStageSession(),
			Horde:               zonehorde.NewSession(),
			Boss:                zoneboss.NewSession(),
			Death:               zonedeath.NewSession(),
			Objective:           objectiveSession,
			ObjectiveProgress:   objectiveProgress,
			ObjectID:            objectIDSession,
			ProjectileID:        projectileIDSession,
			Outcome:             zoneoutcome.NewSession(),
			Result:              zoneresult.NewLedger(),

			ResultVote: zoneresult.NewVoteSession(),
			Timeline:   zonetimeline.NewSession(),
			Timer:      p.timer,
			NPCRandom: sim.NewSimulatorRandom(
				binding.GameID ^ (binding.Difficulty << 24) ^ 0xd20e,
			),
			DropRandom: sim.NewSimulatorRandom(
				binding.GameID ^ (binding.Difficulty << 24) ^ 0xd40f,
			),
			Checkpoint: p.checkpoint,
			Restore:    restoreSnapshot,
		},
	)
	if zoneErr != nil {
		return fmt.Errorf("statusChainZone: %w", zoneErr)
	}
	peerSession.tutorialHorde = tutorialHorde
	peerSession.zone = zone
	if binding.IsCheckpointRestore {
		crystalInventory, isCrystalFound := peerSession.zone.CrystalInventory(
			binding.UserID, peerSession.generation,
		)
		if !isCrystalFound {
			return errors.New("statusChainCrystalRestore: unavailable")
		}
		peerSession.setCrystalInventory(crystalInventory)
	} else {
		peerSession.crystalInventory.IsDiagonalUnlocked = binding.IsDiagonalCatalystUnlocked
		planErr = peerSession.zone.SetCrystalInventory(
			binding.UserID, peerSession.generation, peerSession.crystalInventory,
		)
		if planErr != nil {
			return fmt.Errorf("statusChainCrystal: %w", planErr)
		}
	}
	peerSession.tutorialCapsulePlans = tutorialCapsulePlans
	_, isUnlockFound := peerSession.zone.AbilityCount(
		zoneResultMember(*peerSession),
	)
	if !isUnlockFound {
		return errors.New("statusChainUnlock: unavailable")
	}
	planErr = peerSession.zone.Objective().Join(uint8(binding.Slot))
	if planErr != nil {
		return fmt.Errorf("statusChainObjectiveJoin: %w", planErr)
	}
	peerSession.nextProjectileObjectID = max(
		peerSession.nextProjectileObjectID, firstProjectileObjectID,
	)
	return nil
}
