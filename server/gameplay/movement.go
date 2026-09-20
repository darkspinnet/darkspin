package gameplay

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/squad"
	zone "github.com/darkspinnet/darkspin/server/zone"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	"github.com/darkspinnet/darkspin/server/zone/action"
	barrierraknet "github.com/darkspinnet/darkspin/server/zone/barrier/raknet103"
	zoneboss "github.com/darkspinnet/darkspin/server/zone/boss"
	bossraknet "github.com/darkspinnet/darkspin/server/zone/boss/raknet103"
	zonecallback "github.com/darkspinnet/darkspin/server/zone/callback"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	companionraknet "github.com/darkspinnet/darkspin/server/zone/companion/raknet103"
	zonehero "github.com/darkspinnet/darkspin/server/zone/hero"
	zonehorde "github.com/darkspinnet/darkspin/server/zone/horde"
	horderaknet "github.com/darkspinnet/darkspin/server/zone/horde/raknet103"
	zoneloot "github.com/darkspinnet/darkspin/server/zone/loot"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
	zonesecurity "github.com/darkspinnet/darkspin/server/zone/security"
	securityraknet "github.com/darkspinnet/darkspin/server/zone/security/raknet103"
	zoneteleport "github.com/darkspinnet/darkspin/server/zone/teleport"
	zoneunlock "github.com/darkspinnet/darkspin/server/zone/unlock"
	unlockraknet "github.com/darkspinnet/darkspin/server/zone/unlock/raknet103"
)

const campaignSecurityBlitzFootprintFallback = zonesecurity.DefaultFootprintRadius
const campaignPlayerPursuitGoalTolerance = float32(1)

func (e gameplayPeerSession) deployedCampaignFootprintRadius() float32 {
	if e.deployedCreatureIndex < squad.Size {
		footprintRadius := e.zone.CreatureFootprintRadius(
			e.binding.UserID, e.generation,
			e.deployedCreatureIndex,
		)
		if footprintRadius > 0 {
			return footprintRadius
		}
	}
	return campaignSecurityBlitzFootprintFallback
}

type campaignSecurityTransferAuthority struct {
	registry *gameplaySessionRegistry
}

func (e campaignSecurityTransferAuthority) AdvanceSecurityTransfer(
	req securityraknet.TransferAdvanceRequest,
) ([][]byte, error) {
	if e.registry == nil {
		return nil, errors.New("campaign security registry unavailable")
	}
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	peerSession, isFound := e.registry.sessions[req.SessionKey]
	isCurrent := isFound && peerSession.generation == req.Generation &&
		peerSession.securityTransfer == req.Transfer
	if !isCurrent {
		return nil, nil
	}
	packets, err := req.Transfer.Advance(req.Deadline)
	if err != nil {
		return nil, fmt.Errorf("campaignSecurityAdvance[%s]: %w", req.Deadline, err)
	}
	if req.Deadline == zoneteleport.Deadline[0] {
		err = peerSession.teleportPlayer(req.Now, req.Transfer.Destination())
		if err != nil {
			return nil, fmt.Errorf("campaignSecurityPosition: %w", err)
		}
		companionPackets, companionErr := peerSession.teleportOwnedCompanions(
			game.Vec3(req.Transfer.Destination()),
			game.Quaternion{W: 1},
		)
		if companionErr != nil {
			return nil, fmt.Errorf("campaignSecurityCompanion: %w", companionErr)
		}
		packets = append(packets, companionPackets...)
		if peerSession.zone.Security() == nil {
			return nil, errors.New("campaignSecurityRoute: unavailable")
		}
		err = peerSession.zone.Security().Advance(req.Transfer.RouteIndex())
		if err != nil {
			return nil, fmt.Errorf("campaignSecurityAdvance: %w", err)
		}
	}
	if req.Deadline == zoneteleport.Deadline[len(zoneteleport.Deadline)-1] {
		deletedPacket, releaseErr := req.Transfer.Complete()
		if releaseErr != nil {
			return nil, fmt.Errorf("campaignSecurityRelease: %w", releaseErr)
		}
		if deletedPacket != nil {
			packets = append(packets, deletedPacket)
		}
		peerSession.securityTransfer = nil
	}
	e.registry.sessions[req.SessionKey] = peerSession
	return packets, nil
}

func (e campaignSecurityTransferAuthority) CancelSecurityTransfer(
	req securityraknet.TransferCancelRequest,
) bool {
	if e.registry == nil {
		return false
	}
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	peerSession, isFound := e.registry.sessions[req.SessionKey]
	isCurrent := isFound && peerSession.generation == req.Generation &&
		peerSession.securityTransfer == req.Transfer
	if !isCurrent {
		return false
	}
	req.Transfer.Stop()
	peerSession.securityTransfer = nil
	e.registry.sessions[req.SessionKey] = peerSession
	return true
}

type campaignPopulationRuntime struct {
	registry *gameplaySessionRegistry
	npc      campaignNPCActionRuntime
	logger   *log.Logger
}

type campaignMovementCommandRuntime struct {
	registry     *gameplaySessionRegistry
	action       campaignActionAuthority
	program      Programs
	encounter    campaignEncounterRuntime
	security     *securityraknet.TransferRuntime
	npc          campaignNPCActionRuntime
	damage       campaignDamageRuntime
	modifierPool *modifierPool
	now          func() time.Time
	logger       *log.Logger
	publishEvent func(game.CampaignDirectorPublication)
}

type campaignCompanionFollowStep struct {
	registry   *gameplaySessionRegistry
	sessionKey string
	generation uint64
	plan       zonecompanion.Follow
}

func (e campaignCompanionFollowStep) produce() ([][]byte, error) {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	peerSession, isFound := e.registry.sessions[e.sessionKey]
	if !isFound || peerSession.generation != e.generation ||
		peerSession.zone == nil || peerSession.zone.Companion() == nil {
		return nil, nil
	}
	peerSession.zone.Companion().ReleaseFollow(
		e.plan.ObjectID, e.plan.Revision, e.plan.Destination,
	)
	return nil, nil
}

func (r campaignMovementCommandRuntime) scheduleCompanionFollows(
	packet raknet.Packet, sessionKey string, generation uint64,
	plans []zonecompanion.Follow,
) error {
	if len(plans) == 0 {
		return nil
	}
	producers := make([]raknet.ScheduledPacketProducer, 0, len(plans))
	for _, plan := range plans {
		step := campaignCompanionFollowStep{
			registry: r.registry, sessionKey: sessionKey,
			generation: generation, plan: plan,
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: plan.TravelDuration, Produce: step.produce,
		})
	}
	producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
	if packet.ScheduleGroup == nil {
		r.cancelCompanionFollows(sessionKey, generation, plans)
		return errors.New("companion follow schedule unavailable")
	}
	_, err := packet.ScheduleGroup(producers)
	if err != nil {
		r.cancelCompanionFollows(sessionKey, generation, plans)
		return fmt.Errorf("companionFollowSchedule: %w", err)
	}
	return nil
}

func (r campaignMovementCommandRuntime) cancelCompanionFollows(
	sessionKey string, generation uint64, plans []zonecompanion.Follow,
) {
	r.registry.mutex.Lock()
	defer r.registry.mutex.Unlock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation ||
		peerSession.zone == nil || peerSession.zone.Companion() == nil {
		return
	}
	for _, plan := range plans {
		peerSession.zone.Companion().CancelFollow(plan.ObjectID, plan.Revision)
	}
}

func (s *gameplayPeerSession) collectCampaignTeleportContacts(
	destination raknet.Vector3, now time.Time, timestamp uint64,
	orientation raknet.Quaternion,
) ([][]byte, error) {
	if s == nil {
		return nil, nil
	}
	packets, err := s.collectCampaignOrbs(
		destination, destination, now.Add(campaignOrbLobDuration),
	)
	if err != nil {
		return nil, fmt.Errorf("teleportOrb: %w", err)
	}
	if s.securityTransfer != nil ||
		s.zone.Security() == nil {
		return packets, nil
	}
	threats := s.zone.SecurityThreats()
	previous := game.Vec3{
		X: destination.X, Y: destination.Y, Z: destination.Z,
	}
	decision, err := s.zone.Security().ObserveMovement(
		zonesecurity.MovementRequest{
			Previous: previous, Current: previous, Threats: threats,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("teleportSecurityObserve: %w", err)
	}
	if decision.IsActivation {
		activationPackets, marshalErr := securityraknet.State(
			decision.ObjectID, decision.Teleport, true, true,
		)
		if marshalErr != nil {
			s.zone.Security().CancelPresentation(decision.RouteIndex)
			return nil, fmt.Errorf(
				"teleportSecurityActivationMarshal: %w", marshalErr,
			)
		}
		commitErr := s.zone.Security().CommitPresentation(decision.RouteIndex)
		if commitErr != nil {
			return nil, fmt.Errorf(
				"teleportSecurityActivationCommit: %w", commitErr,
			)
		}
		packets = append(packets, activationPackets...)
		if !decision.IsTeleport {
			return packets, nil
		}
	}
	if !decision.IsTeleport {
		return packets, nil
	}
	securityPackets, err := securityraknet.Teleport(
		securityraknet.TeleportRequest{
			ObjectID: s.deployedObjectID, Teleport: decision.Teleport,
			Orientation: orientation, Timestamp: timestamp,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("teleportSecurityMarshal: %w", err)
	}
	securityDestination := raknet.Vector3{
		X: decision.Teleport.Destination.X,
		Y: decision.Teleport.Destination.Y,
		Z: decision.Teleport.Destination.Z,
	}
	err = s.teleportPlayer(now, securityDestination)
	if err != nil {
		return nil, fmt.Errorf("teleportSecurityMove: %w", err)
	}
	companionPackets, err := s.teleportOwnedCompanions(
		game.Vec3(securityDestination),
		game.Quaternion{
			X: orientation.X, Y: orientation.Y,
			Z: orientation.Z, W: orientation.W,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("teleportSecurityCompanion: %w", err)
	}
	err = s.zone.Security().Advance(decision.RouteIndex)
	if err != nil {
		return nil, fmt.Errorf("teleportSecurityAdvance: %w", err)
	}
	packets = append(packets, securityPackets...)
	packets = append(packets, companionPackets...)
	return packets, nil
}

type campaignMovementInterruption struct {
	basicAttack        *abilityraknet.MeleeRun
	heroDrain          *heroDrainRun
	basicSyncStamp     uint8
	attackPose         retainedAttackPose
	isAttackBlocked    bool
	isPursuitContinued bool
	isDanceStopped     bool
	playerPosition     raknet.Vector3
	pursuitTargetID    uint32
	pursuitAbility     uint32
	pursuitSync        uint8
}

// interruptMovement atomically classifies an admitted movement command and
// cancels only action state that the movement supersedes. The caller stops a
// detached run after the authority releases the registry lock.
func (e campaignActionAuthority) interruptMovement(
	sessionKey string, generation uint64, objectID uint32, goal game.Vec3,
	now time.Time,
) campaignMovementInterruption {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()

	peerSession, isFound := e.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		objectID == peerSession.deployedObjectID
	if !isCurrent {
		return campaignMovementInterruption{}
	}
	if peerSession.heroDrain != nil {
		interruption := campaignMovementInterruption{
			heroDrain:      peerSession.heroDrain,
			playerPosition: peerSession.playerPosition,
		}
		peerSession.heroDrain = nil
		peerSession.resetAbilityRelease()
		e.registry.sessions[sessionKey] = peerSession
		return interruption
	}
	if peerSession.basicAttack != nil || !peerSession.isAbilityReleaseReady(now) {
		attackPose := peerSession.attackPose
		if peerSession.basicAttack == nil && !attackPose.isActiveAt(now) {
			attackPose = retainedAttackPose{}
		}
		return campaignMovementInterruption{
			basicSyncStamp:  peerSession.basicAttackSyncStamp,
			attackPose:      attackPose,
			isAttackBlocked: true,
			playerPosition:  peerSession.playerPosition,
		}
	}
	pursuit := peerSession.campaignPlayerPursuitSession().Snapshot()
	if isCampaignPursuitMovement(peerSession, pursuit, objectID, goal) {
		return campaignMovementInterruption{
			isPursuitContinued: true,
			pursuitTargetID:    pursuit.TargetObjectID,
			pursuitAbility:     pursuit.AbilityIndex,
			pursuitSync:        pursuit.SyncStamp,
		}
	}
	interruption := campaignMovementInterruption{
		pursuitTargetID: pursuit.TargetObjectID,
		pursuitAbility:  pursuit.AbilityIndex,
		pursuitSync:     pursuit.SyncStamp,
		isDanceStopped:  peerSession.isDancing,
		basicSyncStamp:  peerSession.basicAttackSyncStamp,
	}
	peerSession.isDancing = false
	interruption.basicAttack = peerSession.interruptBasicForMovement()
	e.registry.sessions[sessionKey] = peerSession
	return interruption
}

func isCampaignPursuitMovement(
	peerSession gameplayPeerSession, pursuit action.PursuitSnapshot,
	objectID uint32, goal game.Vec3,
) bool {
	if !pursuit.IsActive || pursuit.SourceObjectID != objectID ||
		pursuit.StopDistance <= 0 || peerSession.zone == nil ||
		peerSession.zone.NPCs() == nil {
		return false
	}
	target, isTargetFound := peerSession.zone.NPCs().NPC(pursuit.TargetObjectID)
	if !isTargetFound || target.IsDefeated || target.HitPoint <= 0 {
		return false
	}
	return target.Plan.Position.Sub(goal).Length() <=
		pursuit.StopDistance+campaignPlayerPursuitGoalTolerance
}

func (r campaignMovementCommandRuntime) handle(
	ctx context.Context, packet raknet.Packet, command raknet.ActionCommandData,
	commandSession gameplayPeerSession,
) ([][]byte, error) {
	var err error
	if commandSession.isEnemySleepActive(r.now()) {
		response, marshalErr := marshalZonePlayerMove(
			command.Common.ObjectID, commandSession.playerPosition,
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("moveCampaignSleepMarshal: %w", marshalErr)
		}
		r.logger.Printf(
			"RakNet campaign movement rejected source=%d reason=asleep",
			command.Common.ObjectID,
		)
		return response, nil
	}
	if commandSession.isEnemyStunActive(r.now()) {
		response, marshalErr := marshalZonePlayerMove(
			command.Common.ObjectID, commandSession.playerPosition,
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("moveCampaignStunMarshal: %w", marshalErr)
		}
		r.logger.Printf(
			"RakNet campaign movement rejected source=%d reason=stunned",
			command.Common.ObjectID,
		)
		return response, nil
	}
	if commandSession.isEnemyRootActive(r.now()) {
		response, marshalErr := marshalZonePlayerMove(
			command.Common.ObjectID, commandSession.playerPosition,
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("moveCampaignRootMarshal: %w", marshalErr)
		}
		r.logger.Printf(
			"RakNet campaign movement rejected source=%d reason=rooted",
			command.Common.ObjectID,
		)
		return response, nil
	}
	if commandSession.isEnemyFearActive(r.now()) {
		response, marshalErr := marshalZonePlayerMove(
			command.Common.ObjectID, commandSession.playerPosition,
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("moveCampaignFearMarshal: %w", marshalErr)
		}
		r.logger.Printf(
			"RakNet campaign movement rejected source=%d reason=feared",
			command.Common.ObjectID,
		)
		return response, nil
	}
	goal := command.Movement.GoalPosition
	footprintRadius := campaignSecurityBlitzFootprintFallback
	if commandSession.deployedCreatureIndex < uint32(len(commandSession.binding.Creatures)) {
		creature := commandSession.binding.Creatures[commandSession.deployedCreatureIndex]
		resolvedRadius, radiusErr := r.program.FootprintRadiusByNoun(creature.Noun)
		if radiusErr == nil && resolvedRadius > 0 {
			footprintRadius = resolvedRadius
		}
	}
	admission, admissionErr := action.AdmitMovement(
		action.MovementCommand{
			ObjectID:         command.Common.ObjectID,
			DeployedObjectID: commandSession.deployedObjectID,
			Position: game.Vec3{
				X: command.Common.Position.X, Y: command.Common.Position.Y,
				Z: command.Common.Position.Z,
			},
			Goal:            game.Vec3{X: goal.X, Y: goal.Y, Z: goal.Z},
			FootprintRadius: footprintRadius,
			Navigation:      commandSession.zone.Navigation(),
		},
	)
	if !admission.IsObjectOwned || !admission.IsPositionValid ||
		!admission.IsGoalValid {
		r.logger.Printf("RakNet campaign movement rejected source=%d deployed=%d position=(%g,%g,%g) goal=(%g,%g,%g) owned=%t position_valid=%t goal_valid=%t flags=%#x",
			command.Common.ObjectID, commandSession.deployedObjectID,
			command.Common.Position.X, command.Common.Position.Y, command.Common.Position.Z,
			goal.X, goal.Y, goal.Z, admission.IsObjectOwned,
			admission.IsPositionValid, admission.IsGoalValid,
			command.Movement.GoalFlags)
		return nil, nil
	}
	if admissionErr != nil {
		response, marshalErr := marshalZonePlayerMove(
			command.Common.ObjectID, commandSession.playerPosition,
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("moveCampaignNavigationMarshal: %w", marshalErr)
		}
		r.logger.Printf("RakNet campaign movement rejected by navigation source=%d position=(%g,%g,%g) goal=(%g,%g,%g): %v",
			command.Common.ObjectID,
			command.Common.Position.X, command.Common.Position.Y, command.Common.Position.Z,
			goal.X, goal.Y, goal.Z, admissionErr)
		return response, nil
	}
	sessionKey := packet.Address.String()
	interruption := r.action.interruptMovement(
		sessionKey, commandSession.generation, command.Common.ObjectID,
		game.Vec3{X: goal.X, Y: goal.Y, Z: goal.Z}, r.now(),
	)
	if interruption.isAttackBlocked {
		return r.rejectActiveAttackMovement(
			command, interruption.playerPosition, interruption.basicSyncStamp,
			interruption.attackPose,
		)
	}
	if !interruption.isPursuitContinued {
		r.registry.clearPursuitActionLeases(
			sessionKey, commandSession.transportGeneration,
			command.Common.ObjectID,
		)
	}
	if interruption.isPursuitContinued {
		r.logger.Printf("RakNet campaign movement continues pursuit source=%d target=%d ability=%d pursuit_sync=%d movement_sync=%d goal=(%g,%g,%g)",
			command.Common.ObjectID, interruption.pursuitTargetID,
			interruption.pursuitAbility, interruption.pursuitSync,
			command.Common.Unknown[0], goal.X, goal.Y, goal.Z)
	} else if interruption.pursuitTargetID != 0 {
		r.logger.Printf("RakNet campaign movement replaces pursuit source=%d target=%d ability=%d pursuit_sync=%d movement_sync=%d goal=(%g,%g,%g)",
			command.Common.ObjectID, interruption.pursuitTargetID,
			interruption.pursuitAbility, interruption.pursuitSync,
			command.Common.Unknown[0], goal.X, goal.Y, goal.Z)
	}
	if interruption.basicAttack != nil {
		r.registry.clearActionLeasesForSync(
			sessionKey, commandSession.transportGeneration,
			command.Common.ObjectID, interruption.basicSyncStamp,
		)
	}
	if interruption.basicAttack != nil {
		interruption.basicAttack.Stop()
	}
	drainStopPackets := make([][]byte, 0, 3)
	if interruption.heroDrain != nil {
		var drainErr error
		drainStopPackets, drainErr = interruption.heroDrain.interruptionPacketsAt(
			packet.SourceTime,
		)
		interruption.heroDrain.Stop()
		if drainErr != nil {
			return nil, fmt.Errorf("moveCampaignDrainStop: %w", drainErr)
		}
	}
	danceStopPackets := make([][]byte, 0, 1)
	if interruption.isDanceStopped {
		danceStopPacket, marshalErr := abilityraknet.AnimationReset(
			command.Common.ObjectID, packet.SourceTime,
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("moveCampaignDanceReset: %w", marshalErr)
		}
		danceStopPackets = append(danceStopPackets, danceStopPacket)
	}
	interruptionPackets := append(drainStopPackets, danceStopPackets...)
	r.registry.mutex.Lock()
	peerSession, isSessionFound := r.registry.sessions[packet.Address.String()]
	isCurrent := isSessionFound && peerSession.generation == commandSession.generation
	if isCurrent && (peerSession.basicAttack != nil ||
		!peerSession.isAbilityReleaseReady(r.now())) {
		playerPosition := peerSession.playerPosition
		basicSyncStamp := peerSession.basicAttackSyncStamp
		attackPose := peerSession.attackPose
		if peerSession.basicAttack == nil && !attackPose.isActiveAt(r.now()) {
			attackPose = retainedAttackPose{}
		}
		r.registry.mutex.Unlock()
		return r.rejectActiveAttackMovement(
			command, playerPosition, basicSyncStamp, attackPose,
		)
	}
	isMovementRejected := isCurrent &&
		(peerSession.isHeroSelectionPending || peerSession.deployedHitPoint() <= 0)
	if isCurrent && peerSession.isHeroSelectionPending &&
		isReportedZonePosition(command.Common.Position) {
		positionErr := peerSession.advancePlayerPosition(
			r.now(), command.Common.Position,
		)
		if positionErr != nil {
			r.logger.Printf(
				"RakNet campaign defeated hero position omitted source=%d: %v",
				command.Common.ObjectID, positionErr,
			)
		} else {
			r.registry.sessions[packet.Address.String()] = peerSession
		}
	}
	rejectedPosition := peerSession.playerPosition
	isCurrent = isCurrent && !isMovementRejected
	publications := make([]game.CampaignDirectorPublication, 0)
	hordePlans := make([]zonenpc.SpawnPlan, 0)
	hordePackets := make([][]byte, 0)
	bossPlans := make([]zonenpc.SpawnPlan, 0)
	bossPackets := make([][]byte, 0)
	bossPublication := game.CampaignDirectorPublication{}
	namedBossPlans := make([]zonenpc.SpawnPlan, 0)
	namedBossPackets := make([][]byte, 0)
	randomUnlockPublication := game.CampaignDirectorPublication{}
	supportUnlockPublication := game.CampaignDirectorPublication{}
	var tutorialAbilityUnlock *unlockraknet.AbilityUnlockRun
	var tutorialCreatureUnlock *unlockraknet.CreatureUnlockRun
	var supportUnlockRun *unlockraknet.Run
	var overdriveUnlock *unlockraknet.Run
	encounterUnlockPackets := make([][]byte, 0)
	securityTeleport := zonesecurity.Teleport{}
	isSecurityTeleport := false
	var securityTransfer *securityraknet.Transfer
	securityTransferPackets := make([][]byte, 0)
	securityActivationPackets := make([][]byte, 0)
	securityDeactivationPackets := make([][]byte, 0)
	tutorialTeleporterPackets := make([][]byte, 0)
	isTutorialTeleport := false
	campaignTunnelPackets := make([][]byte, 0)
	campaignTunnel := game.CampaignTeleportRoute{}
	isCampaignTunnel := false
	publishedGoal := goal
	populationDecisions := make([]campaignPopulationDecision, 0)
	populationPlans := make([]zonenpc.SpawnPlan, 0)
	populationPackets := make([][]byte, 0)
	populationAggroPlans := make([]zonenpc.SpawnPlan, 0)
	populationAggroPackets := make([][]byte, 0)
	campaignOrbPackets := make([][]byte, 0)
	campaignDNAPackets := make([][]byte, 0)
	tutorialCapsulePackets := make([][]byte, 0)
	tutorialHordePackets := make([][]byte, 0)
	var tutorialHordeWave *tutorialHordeWaveStep
	companionFollowPackets := make([][]byte, 0)
	companionFollows := make([]zonecompanion.Follow, 0)
	var companionFollowErr error
	playerFollowPublications := make([]playerFollowPublication, 0)
	if isCurrent {
		peerSession.followTargetUserID = 0
		movementNow := r.now()
		previousPosition, currentPosition, movementErr := peerSession.advancePlayerMovement(
			movementNow, command.Common.Position, goal, false,
			r.registry.passiveMovementIncrease(peerSession),
		)
		if movementErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("moveCampaignAdvance: %w", movementErr)
		}
		publishedGoal = peerSession.playerMovementGoal
		previous := game.Vec3{
			X: previousPosition.X, Y: previousPosition.Y, Z: previousPosition.Z,
		}
		current := game.Vec3{
			X: currentPosition.X, Y: currentPosition.Y, Z: currentPosition.Z,
		}
		encounterResult, encounterErr := r.encounter.advance(
			ctx, packet, &peerSession, command.Common.ObjectID, movementNow,
			previousPosition, previous, current,
		)
		if encounterErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("moveCampaignEncounter: %w", encounterErr)
		}
		if encounterResult.isGateRepelled {
			syncErr := peerSession.syncZoneHeroPose()
			if syncErr != nil {
				r.registry.mutex.Unlock()
				return nil, fmt.Errorf("moveCampaignHeroGate: %w", syncErr)
			}
			r.registry.sessions[packet.Address.String()] = peerSession
			r.registry.mutex.Unlock()
			return encounterResult.gatePackets, nil
		}
		current = encounterResult.current
		campaignTunnelPackets, campaignTunnel, isCampaignTunnel, movementErr =
			peerSession.observeCampaignTunnel(
				previous, current, raknet.Quaternion{W: 1},
				packet.SourceTime, movementNow,
			)
		if movementErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("moveCampaignTunnel: %w", movementErr)
		}
		if isCampaignTunnel {
			current = campaignTunnel.Destination
			publishedGoal = raknet.Vector3{
				X: current.X, Y: current.Y, Z: current.Z,
			}
		}
		syncErr := peerSession.syncZoneHeroPose()
		if syncErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("moveCampaignHero: %w", syncErr)
		}
		companionFollows, companionFollowErr = peerSession.zone.Companion().FollowOwner(
			peerSession.binding.UserID, peerSession.generation,
			command.Common.ObjectID, current, peerSession.zone.NPCs().Snapshots(),
			r.program.SupportHealerPassive.SpawnRadius*0.5,
			zonecompanion.CompatibilityMovementSpeed,
		)
		if companionFollowErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf(
				"moveCampaignCompanionFollow: %w", companionFollowErr,
			)
		}
		for _, follow := range companionFollows {
			activation, isActivationFound :=
				peerSession.sagePassiveActivations[follow.ObjectID]
			if !isActivationFound {
				continue
			}
			if activation.Attack != nil {
				activation.Attack.Stop()
				activation.Attack = nil
			}
			activation.TargetObjectID = 0
			peerSession.sagePassiveActivations[follow.ObjectID] = activation
		}
		companionFollowPackets, companionFollowErr = companionraknet.Follow(companionFollows)
		if companionFollowErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf(
				"moveCampaignCompanionMarshal: %w", companionFollowErr,
			)
		}
		campaignOrbPackets = encounterResult.orbPackets
		campaignDNAPackets = encounterResult.dnaPackets
		tutorialCapsulePackets = encounterResult.tutorialCapsulePackets
		tutorialHordePackets = encounterResult.tutorialHordePackets
		tutorialHordeWave = encounterResult.tutorialHordeWave
		publications = encounterResult.publications
		hordePlans = encounterResult.hordePlans
		hordePackets = encounterResult.hordePackets
		bossPlans = encounterResult.bossPlans
		bossPackets = encounterResult.bossPackets
		bossPublication = encounterResult.bossPublication
		namedBossPlans = encounterResult.namedBossPlans
		namedBossPackets = encounterResult.namedBossPackets
		randomUnlockPublication = encounterResult.randomUnlockPublication
		supportUnlockPublication = encounterResult.supportUnlockPublication
		tutorialAbilityUnlock = encounterResult.tutorialAbilityUnlock
		tutorialCreatureUnlock = encounterResult.tutorialCreatureUnlock
		supportUnlockRun = encounterResult.supportUnlockRun
		overdriveUnlock = encounterResult.overdriveUnlock
		encounterUnlockPackets = encounterResult.unlockPackets
		populationResult, populationErr := peerSession.zone.AdvancePopulation(
			zone.PopulationRequest{Position: current},
		)
		if populationErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("moveCampaignPopulation: %w", populationErr)
		}
		populationDecisions = populationResult.Decisions
		populationPlans = populationResult.SpawnPlans
		for _, npc := range populationResult.Acquired {
			populationAggroPlans = append(populationAggroPlans, npc.Plan)
		}
		populationPackets, populationErr =
			npcraknet.Population(populationResult)
		if populationErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("moveCampaignPopulationMarshal: %w", populationErr)
		}
		if len(populationPlans) != 0 {
			populationErr = peerSession.zone.PublishNPCSpawn(
				zoneprojection.NPCSpawn{
					Plans: populationPlans, IsDormant: true,
				},
				peerSession.binding.UserID, commandSession.generation,
			)
			if populationErr != nil {
				r.registry.mutex.Unlock()
				return nil, fmt.Errorf("moveCampaignPopulationPublish: %w", populationErr)
			}
		}
		peerSession.zone.PublishNPCTargets(
			populationResult.Acquired,
			peerSession.binding.UserID, commandSession.generation,
		)
		if len(namedBossPlans) != 0 {
			populationErr = peerSession.zone.PublishNPCSpawn(
				zoneprojection.NPCSpawn{
					Plans: namedBossPlans, TargetObjectID: peerSession.deployedObjectID,
				},
				peerSession.binding.UserID, commandSession.generation,
			)
			if populationErr != nil {
				r.registry.mutex.Unlock()
				return nil, fmt.Errorf("moveCampaignNearBossPublish: %w", populationErr)
			}
		}
		threats := peerSession.zone.SecurityThreats()
		if peerSession.zone.Security() != nil {
			decision, observationErr := peerSession.zone.Security().ObserveMovement(
				zonesecurity.MovementRequest{
					Previous: previous, Current: current, Threats: threats,
					IsTransferActive: peerSession.securityTransfer != nil,
				},
			)
			if observationErr != nil {
				r.registry.mutex.Unlock()
				return nil, fmt.Errorf(
					"moveCampaignSecurityObserve: %w", observationErr,
				)
			}
			if decision.IsActivation {
				securityActivationPackets, observationErr =
					securityraknet.State(
						decision.ObjectID, decision.Teleport, true, true,
					)
				if observationErr != nil {
					peerSession.zone.Security().CancelPresentation(
						decision.RouteIndex,
					)
					r.registry.mutex.Unlock()
					return nil, fmt.Errorf(
						"moveCampaignSecurityActivationMarshal: %w",
						observationErr,
					)
				}
				observationErr = peerSession.zone.Security().CommitPresentation(
					decision.RouteIndex,
				)
				if observationErr != nil {
					r.registry.mutex.Unlock()
					return nil, fmt.Errorf(
						"moveCampaignSecurityActivationCommit: %w",
						observationErr,
					)
				}
			}
			if decision.IsDeactivation {
				securityDeactivationPackets, observationErr =
					securityraknet.State(
						decision.ObjectID, decision.Teleport, false, false,
					)
				if observationErr != nil {
					peerSession.zone.Security().RollbackDeactivation(
						decision.RouteIndex,
					)
					r.registry.mutex.Unlock()
					return nil, fmt.Errorf(
						"moveCampaignSecurityDeactivationMarshal: %w",
						observationErr,
					)
				}
			}
			if decision.IsTeleport {
				securityTeleport = decision.Teleport
				isSecurityTeleport = true
				teleportProgram, compileErr := zonesecurity.CompileTeleport(
					r.program.TeleporterModifier, securityTeleport,
				)
				if compileErr != nil {
					r.registry.mutex.Unlock()
					return nil, fmt.Errorf("moveCampaignSecurityCompile: %w", compileErr)
				}
				securityTransfer, securityTransferPackets, movementErr =
					securityraknet.NewTransfer(securityraknet.TransferRequest{
						Program: teleportProgram, Pool: r.modifierPool,
						ObjectID: command.Common.ObjectID,
						Position: raknet.Vector3{
							X: current.X, Y: current.Y, Z: current.Z,
						},
						Teleport:   securityTeleport,
						RouteIndex: decision.RouteIndex,
						SourceTime: packet.SourceTime,
					})
				if movementErr != nil {
					r.registry.mutex.Unlock()
					return nil, fmt.Errorf("moveCampaignSecurityTransfer: %w", movementErr)
				}
				peerSession.securityTransfer = securityTransfer
			}
		}
		tutorialTeleporterPackets, isTutorialTeleport, movementErr =
			peerSession.observeTutorialTeleporter(
				previous, current, raknet.Quaternion{W: 1},
				packet.SourceTime, movementNow,
			)
		if movementErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("moveTutorialTeleporter: %w", movementErr)
		}
		if tutorialAbilityUnlock != nil {
			installErr := peerSession.campaignUnlockPresentationSession().
				InstallTutorialAbility(tutorialAbilityUnlock)
			if installErr != nil {
				tutorialAbilityUnlock.Stop()
				r.registry.mutex.Unlock()
				return nil, fmt.Errorf("moveTutorialAbilityUnlockInstall: %w", installErr)
			}
		}
		if tutorialCreatureUnlock != nil {
			installErr := peerSession.campaignUnlockPresentationSession().
				InstallTutorialCreature(tutorialCreatureUnlock)
			if installErr != nil {
				tutorialCreatureUnlock.Stop()
				r.registry.mutex.Unlock()
				return nil, fmt.Errorf("moveTutorialCreatureUnlockInstall: %w", installErr)
			}
		}
		r.registry.sessions[packet.Address.String()] = peerSession
		playerFollowPublications = r.registry.updatePlayerFollowersLocked(
			peerSession, movementNow, packet.SourceTime,
		)
	}
	r.registry.mutex.Unlock()
	publishPlayerFollowMovements(playerFollowPublications)
	if tutorialHordeWave != nil {
		err = scheduleTutorialHordeWave(packet, *tutorialHordeWave)
		if err != nil {
			r.registry.mutex.Lock()
			currentSession, isFound := r.registry.sessions[sessionKey]
			isCurrentSession := isFound &&
				currentSession.generation == commandSession.generation &&
				currentSession.tutorialHorde != nil
			if isCurrentSession {
				currentSession.tutorialHorde.rollbackStart()
				r.registry.sessions[sessionKey] = currentSession
			}
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("moveTutorialHordeSchedule: %w", err)
		}
		r.logger.Printf(
			"RakNet tutorial horde entered for %s; first wave scheduled after %s",
			packet.Address, tutorialHordeFirstWaveDelay,
		)
	}
	if isMovementRejected {
		response, marshalErr := marshalZonePlayerStop(
			command.Common.ObjectID, rejectedPosition,
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("moveCampaignDefeatedMarshal: %w", marshalErr)
		}
		r.logger.Printf(
			"RakNet campaign movement rejected source=%d reason=hero selection pending",
			command.Common.ObjectID,
		)
		return response, nil
	}
	if !isCurrent {
		return nil, nil
	}
	if len(hordePlans) != 0 {
		err = commandSession.zone.PublishNPCSpawn(
			zoneprojection.NPCSpawn{
				Plans: hordePlans, TargetObjectID: command.Common.ObjectID,
			},
			commandSession.binding.UserID, commandSession.generation,
		)
		if err != nil {
			return nil, fmt.Errorf("moveCampaignHordePublish: %w", err)
		}
	}
	if securityTransfer != nil {
		producers := r.security.Producers(
			sessionKey, commandSession.generation, securityTransfer,
		)
		producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
		schedule := r.security.Failure(
			sessionKey, commandSession.generation, securityTransfer,
		)
		var cancel raknet.CancelSchedule
		if packet.ScheduleGroupResult != nil {
			cancel, err = packet.ScheduleGroupResult(producers, schedule.Handle)
		} else if packet.ScheduleGroup != nil {
			cancel, err = packet.ScheduleGroup(producers)
		} else {
			err = errors.New("schedule unavailable")
		}
		if err != nil {
			schedule.Handle(err)
			r.logger.Printf("RakNet campaign security transfer rejected after schedule failure for %s: %v",
				sessionKey, err)
			securityTransfer = nil
			securityTransferPackets = nil
			isSecurityTeleport = false
		} else {
			securityTransfer.SetCancel(cancel)
		}
	}
	populationFirstActionPlans, introductionObjectIDs :=
		campaignPopulationFirstActionPlans(populationPlans, populationAggroPlans)
	firstActionPlans := make(
		[]zonenpc.SpawnPlan, 0,
		len(hordePlans)+len(populationFirstActionPlans)+len(namedBossPlans),
	)
	firstActionPlans = append(firstActionPlans, hordePlans...)
	firstActionPlans = append(firstActionPlans, populationFirstActionPlans...)
	firstActionPlans = append(firstActionPlans, namedBossPlans...)
	firstActionPackets, err := r.npc.scheduleFirstActionsWithIntroductions(
		packet, packet.Address.String(), commandSession.generation,
		firstActionPlans, packet.SourceTime, introductionObjectIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("moveCampaignNPCFirstAction: %w", err)
	}
	err = publishCampaignPeersAfterCommit(r.registry, packet, firstActionPackets)
	if err != nil {
		return nil, fmt.Errorf("moveNPCPresentation: %w", err)
	}
	// Movement responses bypass the general action broadcast. DNA collection
	// still removes a shared object; the peer filter excludes the balance update.
	err = publishCampaignPeersAfterCommit(r.registry, packet, campaignDNAPackets)
	if err != nil {
		return nil, fmt.Errorf("moveDNAPresentation: %w", err)
	}
	// Pad state and the initial transfer presentation are shared, even though
	// movement itself has a separate projection. Delayed transfer steps are
	// published by the producer guard after their delivery commits.
	teleportPackets := make([][]byte, 0)
	teleportPackets = append(teleportPackets, securityDeactivationPackets...)
	teleportPackets = append(teleportPackets, securityActivationPackets...)
	teleportPackets = append(teleportPackets, securityTransferPackets...)
	teleportPackets = append(teleportPackets, tutorialTeleporterPackets...)
	teleportPackets = append(teleportPackets, campaignTunnelPackets...)
	err = publishCampaignPeersAfterCommit(r.registry, packet, teleportPackets)
	if err != nil {
		return nil, fmt.Errorf("moveTeleportPresentation: %w", err)
	}
	companionAttackPackets, err := r.damage.startCompanionAttacks(
		packet, packet.Address.String(), commandSession.generation,
		packet.SourceTime,
	)
	if err != nil {
		return nil, fmt.Errorf("moveCampaignCompanionAttack: %w", err)
	}
	immediateUnlockPackets, immediateBossPackets, err := r.encounter.schedulePublications(
		packet, commandSession, campaignEncounterAdvance{
			bossPlans: bossPlans, bossPackets: bossPackets,
			bossPublication:          bossPublication,
			randomUnlockPublication:  randomUnlockPublication,
			supportUnlockPublication: supportUnlockPublication,
			tutorialAbilityUnlock:    tutorialAbilityUnlock,
			tutorialCreatureUnlock:   tutorialCreatureUnlock,
			supportUnlockRun:         supportUnlockRun,
			overdriveUnlock:          overdriveUnlock,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("moveCampaignPublications: %w", err)
	}
	for _, publication := range publications {
		if len(publication.Listeners) > 0 {
			r.logger.Printf("RakNet campaign director event observed for %s marker_set=%q trigger=%q event=%q callback=%q listeners=%d",
				packet.Address, publication.MarkerSetName, publication.TriggerName,
				publication.EventName, publication.CallbackName, len(publication.Listeners))
		}
		if r.publishEvent != nil {
			r.publishEvent(publication)
		}
	}
	for _, decision := range populationDecisions {
		r.logger.Printf("RakNet campaign population decision for %s marker_set=%q locus=%d kind=%d section=%d component=%d floor=%t ambush=%t provisional_count=%d wanderer_spawn=%t wanderer_count=%d spike_outcome=%d challenge=%d",
			packet.Address, decision.MarkerSetName, decision.LocusID, decision.Kind,
			decision.Section, decision.NavigationComponentID,
			decision.IsFloorIntroduction, decision.IsAmbush,
			decision.ProvisionalCount, decision.Wanderer.IsSpawn, decision.Wanderer.ClumpSize,
			decision.SpikeOutcome, decision.Challenge)
	}
	for _, plan := range populationPlans {
		r.logger.Printf("RakNet campaign enemy planned for %s object=%d noun=%q marker_set=%q locus=%d kind=%d captain=%t position=(%.3f,%.3f,%.3f)",
			packet.Address, plan.ObjectID, plan.NounName, plan.MarkerSetName, plan.LocusID,
			plan.Kind, plan.IsCaptain, plan.Position.X, plan.Position.Y, plan.Position.Z)
	}
	for _, plan := range populationAggroPlans {
		r.logger.Printf("RakNet campaign enemy acquired hero for %s object=%d noun=%q radius=%.1f",
			packet.Address, plan.ObjectID, plan.NounName, campaignPopulationAggroRadius)
	}
	for _, plan := range hordePlans {
		r.logger.Printf("RakNet campaign horde enemy admitted for %s object=%d noun=%q marker_set=%q trigger=%d position=(%.3f,%.3f,%.3f)",
			packet.Address, plan.ObjectID, plan.NounName, plan.MarkerSetName, plan.LocusID,
			plan.Position.X, plan.Position.Y, plan.Position.Z)
	}
	response, marshalErr := marshalZonePlayerMove(command.Common.ObjectID, goal)
	if marshalErr != nil {
		return nil, fmt.Errorf("moveCampaignMarshal: %w", marshalErr)
	}
	commandSession.zone.PublishHeroMovement(zoneprojection.HeroMovement{
		ObjectID:           command.Common.ObjectID,
		AnimationTimestamp: packet.SourceTime,
		IsDanceStopped:     interruption.isDanceStopped,
		Goal: game.Vec3{
			X: publishedGoal.X, Y: publishedGoal.Y, Z: publishedGoal.Z,
		},
	}, commandSession.binding.UserID, commandSession.generation)
	response = append(response, interruptionPackets...)
	response = append(response, hordePackets...)
	response = append(response, populationPackets...)
	response = append(response, populationAggroPackets...)
	response = append(response, campaignOrbPackets...)
	response = append(response, campaignDNAPackets...)
	response = append(response, tutorialCapsulePackets...)
	response = append(response, tutorialHordePackets...)
	response = append(response, companionFollowPackets...)
	response = append(response, firstActionPackets...)
	response = append(response, companionAttackPackets...)
	response = append(response, encounterUnlockPackets...)
	response = append(response, immediateUnlockPackets...)
	response = append(response, immediateBossPackets...)
	response = append(response, namedBossPackets...)
	response = append(response, securityDeactivationPackets...)
	response = append(response, securityActivationPackets...)
	response = append(response, tutorialTeleporterPackets...)
	response = append(response, campaignTunnelPackets...)
	if isCampaignTunnel {
		r.logger.Printf(
			"RakNet campaign tunnel accepted for %s marker=%d source=(%.3f,%.3f,%.3f) destination=(%.3f,%.3f,%.3f)",
			packet.Address, campaignTunnel.MarkerID,
			campaignTunnel.Source.X, campaignTunnel.Source.Y, campaignTunnel.Source.Z,
			campaignTunnel.Destination.X, campaignTunnel.Destination.Y,
			campaignTunnel.Destination.Z,
		)
	}
	if isTutorialTeleport {
		r.logger.Printf(
			"RakNet tutorial boss teleporter accepted for %s destination=(%.3f,%.3f,%.3f)",
			packet.Address, tutorialBossTeleport.Destination.X,
			tutorialBossTeleport.Destination.Y, tutorialBossTeleport.Destination.Z,
		)
	}
	if isSecurityTeleport {
		response = append(response, securityTransferPackets...)
		r.logger.Printf("RakNet campaign security teleporter accepted for %s source=(%.3f,%.3f,%.3f) destination=(%.3f,%.3f,%.3f)",
			packet.Address, securityTeleport.Source.X, securityTeleport.Source.Y,
			securityTeleport.Source.Z, securityTeleport.Destination.X,
			securityTeleport.Destination.Y, securityTeleport.Destination.Z)
	}
	for _, plan := range namedBossPlans {
		r.logger.Printf(
			"RakNet campaign named boss admitted by authored proximity for %s object=%d noun=%q marker_set=%q position=(%.3f,%.3f,%.3f)",
			packet.Address, plan.ObjectID, plan.NounName, plan.MarkerSetName,
			plan.Position.X, plan.Position.Y, plan.Position.Z,
		)
	}
	err = r.scheduleCompanionFollows(
		packet, sessionKey, commandSession.generation, companionFollows,
	)
	if err != nil {
		return nil, fmt.Errorf("moveCampaignCompanionFollowSchedule: %w", err)
	}
	r.logger.Printf("RakNet campaign movement accepted source=%d position=(%g,%g,%g) goal=(%g,%g,%g) reported_position=(%g,%g,%g) requested_goal=(%g,%g,%g) flags=%#x",
		command.Common.ObjectID, peerSession.playerPosition.X, peerSession.playerPosition.Y,
		peerSession.playerPosition.Z, publishedGoal.X, publishedGoal.Y, publishedGoal.Z,
		command.Common.Position.X, command.Common.Position.Y, command.Common.Position.Z,
		goal.X, goal.Y, goal.Z,
		command.Movement.GoalFlags)
	return response, nil

}

func marshalZoneHeroMovement(
	movement zoneprojection.HeroMovement,
) ([][]byte, error) {
	packets, err := marshalZonePlayerMove(movement.ObjectID, raknet.Vector3{
		X: movement.Goal.X, Y: movement.Goal.Y, Z: movement.Goal.Z,
	})
	if err != nil {
		return nil, fmt.Errorf("heroMovement: %w", err)
	}
	if !movement.IsDanceStopped {
		return packets, nil
	}
	danceStopPacket, err := abilityraknet.AnimationReset(
		movement.ObjectID, movement.AnimationTimestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("heroMovementDance: %w", err)
	}
	return append(packets, danceStopPacket), nil
}

func (r campaignMovementCommandRuntime) rejectActiveAttackMovement(
	command raknet.ActionCommandData, playerPosition raknet.Vector3,
	basicSyncStamp uint8, attackPose retainedAttackPose,
) ([][]byte, error) {
	response, err := marshalZonePlayerStop(command.Common.ObjectID, playerPosition)
	if attackPose.isActive {
		response, err = marshalZonePlayerAttackPose(
			command.Common.ObjectID, playerPosition,
			attackPose.facing, attackPose.targetPosition,
			attackPose.targetObjectID,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("moveCampaignAttackMarshal: %w", err)
	}
	r.logger.Printf(
		"RakNet campaign movement held source=%d action_sync=%d position=(%g,%g,%g) requested_goal=(%g,%g,%g) reason=attack_active",
		command.Common.ObjectID, basicSyncStamp,
		playerPosition.X, playerPosition.Y, playerPosition.Z,
		command.Movement.GoalPosition.X, command.Movement.GoalPosition.Y,
		command.Movement.GoalPosition.Z,
	)
	return response, nil
}

type campaignEncounterRuntime struct {
	supportUnlock sim.Program
	program       Programs
	progression   zoneloot.DNAGranter
	logger        *log.Logger
	registry      *gameplaySessionRegistry
	npc           campaignNPCActionRuntime
	damage        campaignDamageRuntime
	timer         zone.Timer
	modifierPool  *modifierPool
}

type campaignEncounterAdvance struct {
	current                  game.Vec3
	isGateRepelled           bool
	gatePackets              [][]byte
	orbPackets               [][]byte
	dnaPackets               [][]byte
	tutorialCapsulePackets   [][]byte
	tutorialHordePackets     [][]byte
	tutorialHordeWave        *tutorialHordeWaveStep
	publications             []game.CampaignDirectorPublication
	hordePlans               []zonenpc.SpawnPlan
	hordePackets             [][]byte
	bossPlans                []zonenpc.SpawnPlan
	bossPackets              [][]byte
	bossPublication          game.CampaignDirectorPublication
	namedBossPlans           []zonenpc.SpawnPlan
	namedBossPackets         [][]byte
	randomUnlockPublication  game.CampaignDirectorPublication
	supportUnlockPublication game.CampaignDirectorPublication
	tutorialAbilityUnlock    *unlockraknet.AbilityUnlockRun
	tutorialCreatureUnlock   *unlockraknet.CreatureUnlockRun
	supportUnlockRun         *unlockraknet.Run
	overdriveUnlock          *unlockraknet.Run
	unlockPackets            [][]byte
}

var initialChainSecondaryAbilityTrigger = sim.MarkerTrigger{
	LevelID: 1, MarkerID: 0x11f01105,
	Center: sim.Position{X: 645.392, Y: -2.394, Z: 15.106}, Radius: 8,
	IsTriggerOnceOnly: true, IsServerOnly: true,
}

type campaignTutorialCreatureUnlockStep struct {
	runtime    campaignEncounterRuntime
	ctx        context.Context
	sessionKey string
	generation uint64
	timestamp  uint64
	run        *unlockraknet.CreatureUnlockRun
}

func (s campaignTutorialCreatureUnlockStep) produce() ([][]byte, error) {
	packets, err := s.run.Advance(s.ctx, zoneunlock.SecondCreatureDelay)
	if err != nil {
		return nil, fmt.Errorf("tutorialCreatureUnlockAdvance: %w", err)
	}
	s.runtime.registry.mutex.Lock()
	peerSession, isFound := s.runtime.registry.sessions[s.sessionKey]
	presentation := peerSession.campaignUnlockPresentationSession()
	isCurrent := isFound && peerSession.generation == s.generation &&
		presentation != nil && presentation.TutorialCreature() == s.run
	if !isCurrent {
		s.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	peerSession.binding = tutorialSageBinding(peerSession.binding)
	passiveInstanceID := peerSession.passiveModifierInstance[1]
	isPassiveAllocated := false
	if passiveInstanceID == 0 {
		passiveInstanceID, err = s.runtime.modifierPool.Allocate()
		isPassiveAllocated = err == nil
	}
	if err == nil {
		passivePacket, marshalErr := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
			TargetID:     zonehero.ObjectID(peerSession.binding.Slot, 1),
			ModifierGUID: peerSession.binding.Creatures[1].PassiveAbility,
			InstanceID:   passiveInstanceID, StackCount: 1,
			StartMilliseconds: s.timestamp,
			SourceID:          zonehero.ObjectID(peerSession.binding.Slot, 1),
		})
		if marshalErr != nil {
			err = fmt.Errorf("passiveMarshal: %w", marshalErr)
		} else {
			packets = append(packets, passivePacket)
		}
	}
	if err == nil {
		peerSession.passiveModifierInstance[1] = passiveInstanceID
		peerSession.maximumHitPoints[1] = peerSession.binding.Creatures[1].HitPoint
		peerSession.maximumManaPoints[1] = peerSession.binding.Creatures[1].PowerPoint
		err = peerSession.squad.SetAvailable(
			1, peerSession.binding.Creatures[1].HitPoint,
		)
	}
	if err == nil {
		err = peerSession.squad.SetManaPoints(
			1, peerSession.binding.Creatures[1].PowerPoint,
		)
	}
	if err == nil {
		err = peerSession.syncZoneSquadCheckpoint()
	}
	if err == nil {
		presentation.ClearTutorialCreature(s.run)
		s.run.SetCancel(nil)
		s.runtime.registry.sessions[s.sessionKey] = peerSession
	}
	s.runtime.registry.mutex.Unlock()
	if err != nil {
		if isPassiveAllocated {
			releaseErr := s.runtime.modifierPool.Release(passiveInstanceID)
			if releaseErr != nil {
				err = errors.Join(err, fmt.Errorf("passiveRelease: %w", releaseErr))
			}
		}
		return nil, fmt.Errorf("tutorialCreatureUnlockCommit: %w", err)
	}
	s.runtime.logger.Printf(
		"RakNet tutorial Sage unlocked for %s passive=%#x instance=%#x",
		s.sessionKey, peerSession.binding.Creatures[1].PassiveAbility,
		passiveInstanceID,
	)
	return packets, nil
}

func (s campaignTutorialCreatureUnlockStep) clear() {
	s.runtime.registry.mutex.Lock()
	peerSession, isFound := s.runtime.registry.sessions[s.sessionKey]
	if isFound && peerSession.generation == s.generation {
		peerSession.campaignUnlockPresentationSession().
			ClearTutorialCreature(s.run)
	}
	s.runtime.registry.mutex.Unlock()
}

type campaignTutorialAbilityUnlockStep struct {
	runtime    campaignEncounterRuntime
	ctx        context.Context
	zone       *zone.Zone
	member     zone.Member
	sessionKey string
	generation uint64
	slot       uint16
	run        *unlockraknet.AbilityUnlockRun
}

func (s campaignTutorialAbilityUnlockStep) produce() ([][]byte, error) {
	lessonPacket, err := raknet.MarshalApplication(
		raknet.TutorialAbilityLessonMessage(
			tutorialAbilityLessonObjectiveID, uint8(s.slot),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("tutorialAbilityLessonMarshal: %w", err)
	}
	packets, err := s.run.Advance(s.ctx, zoneunlock.AbilitySecondDelay)
	if err != nil {
		return nil, fmt.Errorf("tutorialAbilityUnlockAdvance: %w", err)
	}
	s.runtime.registry.mutex.Lock()
	peerSession, isFound := s.runtime.registry.sessions[s.sessionKey]
	var presentation *unlockraknet.ActiveSession
	if isFound {
		presentation = peerSession.campaignUnlockPresentationSession()
	}
	isCurrent := isFound && peerSession.generation == s.generation &&
		presentation != nil && presentation.TutorialAbility() == s.run
	isAdvanced := false
	if isCurrent {
		isAdvanced = s.zone.CompareAndSetAbilityCount(
			s.member, zoneunlock.TutorialInitialBoundary,
			zoneunlock.TutorialAbilityBoundary,
		)
		presentation.ClearTutorialAbility(s.run)
		s.run.ClearCancel()
	}
	s.runtime.registry.mutex.Unlock()
	if !isCurrent || !isAdvanced {
		return nil, nil
	}
	s.runtime.logger.Printf(
		"RakNet tutorial active ability unlocked for %s boundary=%d",
		s.sessionKey, zoneunlock.TutorialAbilityBoundary,
	)
	return append(packets, lessonPacket), nil
}

func (s campaignTutorialAbilityUnlockStep) clear() {
	s.runtime.registry.mutex.Lock()
	peerSession, isFound := s.runtime.registry.sessions[s.sessionKey]
	if isFound && peerSession.generation == s.generation {
		peerSession.campaignUnlockPresentationSession().
			ClearTutorialAbility(s.run)
	}
	s.runtime.registry.mutex.Unlock()
}

type campaignRandomUnlockStep struct {
	runtime    campaignEncounterRuntime
	zone       *zone.Zone
	member     zone.Member
	sessionKey string
	slot       uint16
	markerID   uint32
}

func (s campaignRandomUnlockStep) produce() ([][]byte, error) {
	packet, err := unlockraknet.AbilityCount(s.slot, zoneunlock.RandomAbilityBoundary)
	if err != nil {
		return nil, fmt.Errorf("campaignRandomUnlockMarshal: %w", err)
	}
	isCurrent := s.zone.CompareAndSetAbilityCount(
		s.member,
		zoneunlock.InitialAbilityBoundary,
		zoneunlock.RandomAbilityBoundary,
	)
	if !isCurrent {
		return nil, nil
	}
	s.runtime.logger.Printf(
		"RakNet campaign random ability unlocked for %s marker=%d boundary=%d",
		s.sessionKey, s.markerID, zoneunlock.RandomAbilityBoundary,
	)
	return [][]byte{packet}, nil
}

type campaignSupportUnlockStep struct {
	runtime    campaignEncounterRuntime
	ctx        context.Context
	zone       *zone.Zone
	member     zone.Member
	sessionKey string
	generation uint64
	run        *unlockraknet.Run
	deadline   time.Duration
}

func (s campaignSupportUnlockStep) produce() ([][]byte, error) {
	packets, err := s.run.Advance(s.ctx, s.deadline)
	if err != nil {
		return nil, fmt.Errorf("supportUnlockAdvance[%s]: %w", s.deadline, err)
	}
	s.runtime.registry.mutex.Lock()
	peerSession, isFound := s.runtime.registry.sessions[s.sessionKey]
	isCurrent := isFound && peerSession.generation == s.generation &&
		peerSession.campaignUnlockPresentationSession().Support() == s.run
	if isCurrent && len(packets) != 0 {
		s.zone.RaiseAbilityCount(s.member, zoneunlock.SupportAbilityBoundary)
	}
	if isCurrent && s.deadline == zoneunlock.SupportFinalDeadline {
		peerSession.campaignUnlockPresentationSession().ClearSupport(s.run)
		s.run.SetCancel(nil)
	}
	if isCurrent {
		s.runtime.registry.sessions[s.sessionKey] = peerSession
	}
	s.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	return packets, nil
}

func (s campaignSupportUnlockStep) clear() {
	s.runtime.registry.mutex.Lock()
	peerSession, isFound := s.runtime.registry.sessions[s.sessionKey]
	if isFound && peerSession.generation == s.generation &&
		peerSession.campaignUnlockPresentationSession().Support() == s.run {
		peerSession.campaignUnlockPresentationSession().ClearSupport(s.run)
		s.runtime.registry.sessions[s.sessionKey] = peerSession
	}
	s.runtime.registry.mutex.Unlock()
}

type campaignFullAbilityUnlockStep struct {
	runtime    campaignEncounterRuntime
	zone       *zone.Zone
	member     zone.Member
	sessionKey string
	slot       uint16
}

func (s campaignFullAbilityUnlockStep) produce() ([][]byte, error) {
	packet, err := unlockraknet.AbilityCount(s.slot, zoneunlock.FullAbilityBoundary)
	if err != nil {
		return nil, fmt.Errorf("campaignSupportUnlockMarshal: %w", err)
	}
	isCurrent := s.zone.RaiseAbilityCount(
		s.member, zoneunlock.FullAbilityBoundary,
	)
	if !isCurrent {
		return nil, nil
	}
	s.runtime.logger.Printf(
		"RakNet campaign squad support unlocked for %s boundary=%d",
		s.sessionKey, zoneunlock.FullAbilityBoundary,
	)
	return [][]byte{packet}, nil
}

type campaignBossAdmissionStep struct {
	runtime        campaignEncounterRuntime
	zone           *zone.Zone
	targetObjectID uint32
	plans          []zonenpc.SpawnPlan
	markerSetName  string
}

func (s campaignBossAdmissionStep) admit() error {
	err := s.zone.AdmitBoss(s.targetObjectID, s.plans)
	if err != nil {
		return fmt.Errorf("campaignBossCommit: %w", err)
	}
	return nil
}

func (s campaignBossAdmissionStep) admitAndPublish() error {
	err := s.admit()
	if err != nil {
		return err
	}
	err = s.zone.PublishNPCSpawn(zoneprojection.NPCSpawn{
		Plans: s.plans[1:], TargetObjectID: s.targetObjectID,
		IsBossAddPhase: true,
	}, 0, 0)
	if err != nil {
		return fmt.Errorf("campaignBossPublish: %w", err)
	}
	return nil
}

func (s campaignBossAdmissionStep) execute() {
	err := s.admitAndPublish()
	if err != nil {
		s.runtime.logger.Printf(
			"Campaign instance boss admission failed marker_set=%q: %v",
			s.markerSetName, err,
		)
		return
	}
	s.runtime.logger.Printf(
		"Campaign instance boss add phase admitted marker_set=%q reserved_leader=%d actors=%d",
		s.markerSetName, s.plans[0].ObjectID, len(s.plans)-1,
	)
}

func (r campaignEncounterRuntime) schedulePublications(
	packet raknet.Packet, commandSession gameplayPeerSession, encounter campaignEncounterAdvance,
) ([][]byte, [][]byte, error) {
	immediateUnlockPackets := make([][]byte, 0)
	immediateBossPackets := make([][]byte, 0)
	sessionKey := packet.Address.String()
	if encounter.tutorialAbilityUnlock != nil {
		step := campaignTutorialAbilityUnlockStep{
			runtime: r, ctx: commandSession.zone.Context(),
			zone: commandSession.zone, member: zoneResultMember(commandSession),
			sessionKey: sessionKey, generation: commandSession.generation,
			slot: commandSession.binding.Slot, run: encounter.tutorialAbilityUnlock,
		}
		producers := r.registry.producerGuard.scheduledProducers(
			sessionKey, []raknet.ScheduledPacketProducer{{
				Delay: zoneunlock.AbilitySecondDelay, Produce: step.produce,
			}},
		)
		cancel, scheduleErr := scheduleEncounterProducerGroup(packet, producers)
		if scheduleErr == nil {
			encounter.tutorialAbilityUnlock.SetCancel(cancel)
		} else {
			var err error
			immediateUnlockPackets, err = step.produce()
			if err != nil {
				return nil, nil, fmt.Errorf("tutorialAbilityUnlockFallback: %w", err)
			}
			step.clear()
			encounter.tutorialAbilityUnlock.Stop()
			r.logger.Printf(
				"RakNet tutorial ability published immediately after schedule failure: %v",
				scheduleErr,
			)
		}
	}
	if encounter.tutorialCreatureUnlock != nil {
		step := campaignTutorialCreatureUnlockStep{
			runtime: r, ctx: commandSession.zone.Context(),
			sessionKey: sessionKey, generation: commandSession.generation,
			timestamp: packet.SourceTime +
				uint64(zoneunlock.SecondCreatureDelay/time.Millisecond),
			run: encounter.tutorialCreatureUnlock,
		}
		producers := r.registry.producerGuard.scheduledProducers(
			sessionKey, []raknet.ScheduledPacketProducer{{
				Delay: zoneunlock.SecondCreatureDelay, Produce: step.produce,
			}},
		)
		cancel, scheduleErr := scheduleEncounterProducerGroup(packet, producers)
		if scheduleErr == nil {
			encounter.tutorialCreatureUnlock.SetCancel(cancel)
		} else {
			fallbackPackets, fallbackErr := step.produce()
			if fallbackErr != nil {
				return nil, nil, fmt.Errorf("tutorialCreatureUnlockFallback: %w", fallbackErr)
			}
			immediateUnlockPackets = append(immediateUnlockPackets, fallbackPackets...)
			step.clear()
			encounter.tutorialCreatureUnlock.Stop()
			r.logger.Printf(
				"RakNet tutorial Sage published immediately after schedule failure: %v",
				scheduleErr,
			)
		}
	}
	if encounter.randomUnlockPublication.TriggerMarkerID != 0 {
		step := campaignRandomUnlockStep{
			runtime: r, zone: commandSession.zone,
			member: zoneResultMember(commandSession), sessionKey: sessionKey,
			slot:     commandSession.binding.Slot,
			markerID: encounter.randomUnlockPublication.TriggerMarkerID,
		}
		producers := r.registry.producerGuard.scheduledProducers(
			sessionKey, []raknet.ScheduledPacketProducer{{
				Delay: zoneunlock.RandomDelay, Produce: step.produce,
			}},
		)
		scheduleErr := scheduleEncounterProducers(packet, producers)
		if scheduleErr != nil {
			var err error
			immediateUnlockPackets, err = step.produce()
			if err != nil {
				return nil, nil, fmt.Errorf("campaignRandomUnlockFallback: %w", err)
			}
			r.logger.Printf(
				"RakNet campaign random ability published immediately after schedule failure marker=%d: %v",
				step.markerID, scheduleErr,
			)
		}
	}
	if encounter.supportUnlockRun != nil {
		producers := make(
			[]raknet.ScheduledPacketProducer, 0, len(zoneunlock.SupportDeadlines()),
		)
		for _, deadline := range zoneunlock.SupportDeadlines() {
			step := campaignSupportUnlockStep{
				runtime: r, ctx: commandSession.zone.Context(),
				zone: commandSession.zone, member: zoneResultMember(commandSession),
				sessionKey: sessionKey, generation: commandSession.generation,
				run: encounter.supportUnlockRun, deadline: deadline,
			}
			producers = append(producers, raknet.ScheduledPacketProducer{
				Delay: deadline, Produce: step.produce,
			})
		}
		producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
		cancel, scheduleErr := scheduleEncounterProducerGroup(packet, producers)
		if scheduleErr == nil {
			encounter.supportUnlockRun.SetCancel(cancel)
		} else {
			fallback := campaignSupportUnlockStep{
				runtime: r, ctx: commandSession.zone.Context(),
				zone: commandSession.zone, member: zoneResultMember(commandSession),
				sessionKey: sessionKey, generation: commandSession.generation,
				run: encounter.supportUnlockRun, deadline: zoneunlock.SupportMutationDeadline,
			}
			var err error
			immediateUnlockPackets, err = fallback.produce()
			if err != nil {
				return nil, nil, fmt.Errorf("campaignSupportUnlockFallback: %w", err)
			}
			fallback.clear()
			encounter.supportUnlockRun.Stop()
			r.logger.Printf(
				"RakNet campaign support ability published immediately after schedule failure marker=%d: %v",
				encounter.supportUnlockPublication.TriggerMarkerID, scheduleErr,
			)
		}
	}
	if encounter.overdriveUnlock != nil {
		overdrivePackets, scheduleErr := r.scheduleOverdriveUnlock(
			packet, sessionKey, commandSession.generation,
			encounter.overdriveUnlock,
		)
		if scheduleErr != nil {
			return nil, nil, fmt.Errorf("campaignOverdriveUnlock: %w", scheduleErr)
		}
		immediateUnlockPackets = append(immediateUnlockPackets, overdrivePackets...)
	}
	if len(encounter.bossPlans) == 0 {
		return immediateUnlockPackets, immediateBossPackets, nil
	}
	warningPacket, err := bossraknet.AddPhase()
	if err != nil {
		return nil, nil, fmt.Errorf("campaignBossWarning: %w", err)
	}
	immediateBossPackets = append(immediateBossPackets, warningPacket)
	bossDelay := zoneboss.InitialArmingDelay
	abilityCount, isAbilityCountFound := commandSession.zone.AbilityCount(
		zoneResultMember(commandSession),
	)
	isFullAbilityUnlockNeeded := !isAbilityCountFound ||
		abilityCount < zoneunlock.FullAbilityBoundary
	if isFullAbilityUnlockNeeded {
		bossDelay = zoneunlock.FirstClearBossArmingDelay
	}
	bossStep := campaignBossAdmissionStep{
		runtime: r, zone: commandSession.zone,
		targetObjectID: commandSession.deployedObjectID,
		plans:          encounter.bossPlans,
		markerSetName:  encounter.bossPublication.MarkerSetName,
	}
	if isFullAbilityUnlockNeeded {
		step := campaignFullAbilityUnlockStep{
			runtime: r, zone: commandSession.zone,
			member: zoneResultMember(commandSession), sessionKey: sessionKey,
			slot: commandSession.binding.Slot,
		}
		producers := r.registry.producerGuard.scheduledProducers(
			sessionKey, []raknet.ScheduledPacketProducer{{
				Delay: zoneunlock.FirstClearSupportDelay, Produce: step.produce,
			}},
		)
		scheduleErr := scheduleEncounterProducers(packet, producers)
		if scheduleErr != nil {
			unlockPackets, err := step.produce()
			if err != nil {
				return nil, nil, fmt.Errorf("campaignSupportUnlockFallback: %w", err)
			}
			immediateUnlockPackets = append(immediateUnlockPackets, unlockPackets...)
		}
	}
	if commandSession.zone == nil ||
		commandSession.zone.Timeline() == nil || r.timer == nil {
		return nil, nil, errors.New("campaign boss timeline unavailable")
	}
	key := fmt.Sprintf("boss-admission:%d", encounter.bossPlans[0].ObjectID)
	scheduleErr := commandSession.zone.Timeline().Schedule(
		key, bossDelay, bossStep.execute, r.timer.Schedule,
	)
	if scheduleErr == nil {
		return immediateUnlockPackets, immediateBossPackets, nil
	}
	err = bossStep.admitAndPublish()
	if err != nil {
		return nil, nil, fmt.Errorf(
			"campaignBossFallback: %w", errors.Join(scheduleErr, err),
		)
	}
	r.logger.Printf(
		"Campaign boss admitted and projected immediately after timeline failure marker_set=%q: %v",
		encounter.bossPublication.MarkerSetName, scheduleErr,
	)
	return immediateUnlockPackets, immediateBossPackets, nil
}

func (r campaignEncounterRuntime) scheduleOverdriveUnlock(
	packet raknet.Packet, sessionKey string, generation uint64,
	run *unlockraknet.Run,
) ([][]byte, error) {
	producers := make(
		[]raknet.ScheduledPacketProducer, 0, len(zoneunlock.OverdriveDeadlines()),
	)
	for _, deadline := range zoneunlock.OverdriveDeadlines() {
		step := campaignOverdriveUnlockStep{
			runtime: r.damage, sessionKey: sessionKey, generation: generation,
			run: run, deadline: deadline,
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: deadline, Produce: step.produce,
		})
	}
	producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
	failure := campaignOverdriveUnlockFailure{
		runtime: r.damage, sessionKey: sessionKey, generation: generation,
		run: run,
	}
	cancel, err := scheduleEncounterProducerGroup(packet, producers)
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err == nil {
		run.SetCancel(cancel)
		return nil, nil
	}
	fallback := campaignOverdriveUnlockStep{
		runtime: r.damage, sessionKey: sessionKey, generation: generation,
		run: run, deadline: zoneunlock.OverdriveMutationDeadline,
	}
	packets, fallbackErr := fallback.produce()
	if fallbackErr != nil {
		failure.handle(err)
		return nil, fmt.Errorf("overdriveFallback: %w", fallbackErr)
	}
	failure.handle(err)
	r.logger.Printf(
		"RakNet campaign overdrive unlock published immediately after schedule failure: %v",
		err,
	)
	return packets, nil
}

func scheduleEncounterProducers(
	packet raknet.Packet, producers []raknet.ScheduledPacketProducer,
) error {
	if packet.ScheduleGroupResult != nil {
		_, err := packet.ScheduleGroupResult(producers, nil)
		if err != nil {
			return fmt.Errorf("encounterSchedule: %w", err)
		}
		return nil
	}
	if packet.ScheduleGroup == nil {
		return errors.New("encounter schedule unavailable")
	}
	_, err := packet.ScheduleGroup(producers)
	if err != nil {
		return fmt.Errorf("encounterSchedule: %w", err)
	}
	return nil
}

func scheduleEncounterProducerGroup(
	packet raknet.Packet, producers []raknet.ScheduledPacketProducer,
) (raknet.CancelSchedule, error) {
	if packet.ScheduleGroupResult != nil {
		cancel, err := packet.ScheduleGroupResult(producers, nil)
		if err != nil {
			return nil, fmt.Errorf("encounterSchedule: %w", err)
		}
		return cancel, nil
	}
	if packet.ScheduleGroup == nil {
		return nil, errors.New("encounter schedule unavailable")
	}
	cancel, err := packet.ScheduleGroup(producers)
	if err != nil {
		return nil, fmt.Errorf("encounterSchedule: %w", err)
	}
	return cancel, nil
}

func (r campaignEncounterRuntime) advance(
	ctx context.Context, packet raknet.Packet, peerSession *gameplayPeerSession,
	objectID uint32, movementNow time.Time, previousPosition raknet.Vector3,
	previous game.Vec3, current game.Vec3,
) (campaignEncounterAdvance, error) {
	result := campaignEncounterAdvance{current: current}
	var err error
	routeMovement, err := peerSession.zone.ConstrainRoute(previous, current)
	if err != nil {
		return result, fmt.Errorf("moveCampaignRoute: %w", err)
	}
	result.current = routeMovement.Current
	isTutorialAbilityMarkerEntered := false
	isTutorialCreatureMarkerEntered := false
	isTutorialCapsuleMarkerEntered := false
	if routeMovement.GateContact.MarkerID != 0 {
		constrainedPosition := raknet.Vector3{
			X: result.current.X, Y: result.current.Y, Z: result.current.Z,
		}
		movementErr := peerSession.teleportPlayer(movementNow, constrainedPosition)
		if movementErr != nil {
			return result, fmt.Errorf("moveCampaignGatePosition: %w", movementErr)
		}
		result.gatePackets, err = horderaknet.GateRepel(
			objectID, routeMovement.GateContact,
		)
		if err != nil {
			return result, fmt.Errorf("moveCampaignHordeGateMarshal: %w", err)
		}
		r.logger.Printf("RakNet campaign horde gate repelled %s marker_set=%q marker=%d return=(%.3f,%.3f,%.3f)",
			packet.Address, routeMovement.GateContact.MarkerSetName,
			routeMovement.GateContact.MarkerID,
			routeMovement.GateContact.ReturnPosition.X,
			routeMovement.GateContact.ReturnPosition.Y,
			routeMovement.GateContact.ReturnPosition.Z)
		result.isGateRepelled = true
		return result, nil
	}
	if peerSession.binding.Mode == game.ModeTutorial &&
		peerSession.tutorialHorde != nil &&
		peerSession.tutorialHorde.reserveStart(previous, result.current) {
		alertPacket, alertErr := marshalTutorialHordeAlert(
			tutorialHordeIncomingClientEventID, objectID,
		)
		if alertErr != nil {
			peerSession.tutorialHorde.rollbackStart()
			return result, fmt.Errorf("moveTutorialHordeAlert: %w", alertErr)
		}
		step := tutorialHordeWaveStep{
			registry: r.registry, npc: r.npc, logger: r.logger,
			packet: packet, sessionKey: packet.Address.String(),
			generation: peerSession.generation, timestamp: packet.SourceTime,
			delay: tutorialHordeFirstWaveDelay,
		}
		result.tutorialHordeWave = &step
		result.tutorialHordePackets = append(
			result.tutorialHordePackets, alertPacket,
		)
	}
	if routeMovement.IsAuthored {
		if zoneunlock.IsFirstClear(peerSession.binding) {
			movement := sim.MovementCommand{
				ActorRole: "player",
				From:      sim.Position{X: previous.X, Y: previous.Y, Z: previous.Z},
				To: sim.Position{
					X: result.current.X, Y: result.current.Y, Z: result.current.Z,
				},
			}
			_, isSecondaryAbilityTriggerEntered := sim.MarkerEntered(
				movement, initialChainSecondaryAbilityTrigger,
			)
			abilityCount, isAbilityCountFound := peerSession.zone.AbilityCount(
				zoneResultMember(*peerSession),
			)
			if isSecondaryAbilityTriggerEntered && isAbilityCountFound &&
				abilityCount == zoneunlock.RandomAbilityBoundary {
				unlockPacket, marshalErr := unlockraknet.AbilityCount(
					peerSession.binding.Slot, zoneunlock.FullAbilityBoundary,
				)
				if marshalErr != nil {
					return result, fmt.Errorf(
						"moveCampaignSecondaryAbilityMarshal: %w", marshalErr,
					)
				}
				isRaised := peerSession.zone.RaiseAbilityCount(
					zoneResultMember(*peerSession), zoneunlock.FullAbilityBoundary,
				)
				if isRaised {
					result.unlockPackets = append(result.unlockPackets, unlockPacket)
					r.logger.Printf(
						"RakNet campaign secondary abilities unlocked for %s boundary=%d",
						packet.Address, zoneunlock.FullAbilityBoundary,
					)
				}
			}
		}
		if peerSession.binding.Mode == game.ModeTutorial {
			movement := sim.MovementCommand{
				ActorRole: "player",
				From:      sim.Position{X: previous.X, Y: previous.Y, Z: previous.Z},
				To: sim.Position{
					X: result.current.X, Y: result.current.Y, Z: result.current.Z,
				},
			}
			_, isTutorialAbilityMarkerEntered = sim.MarkerEntered(
				movement, r.program.IntroAbilitySecond.Trigger,
			)
			_, isTutorialCreatureMarkerEntered = sim.MarkerEntered(
				movement, r.program.IntroSecondCreature.Trigger,
			)
			_, isTutorialCapsuleMarkerEntered = sim.MarkerEntered(
				movement, r.program.IntroHealthAndPower,
			)
		}
		result.orbPackets, err = peerSession.collectCampaignOrbs(
			previousPosition,
			raknet.Vector3{X: result.current.X, Y: result.current.Y, Z: result.current.Z},
			movementNow,
		)
		if err != nil {
			return result, fmt.Errorf("moveCampaignOrb: %w", err)
		}
		result.dnaPackets, err = peerSession.collectCampaignDNA(
			ctx, r.progression, previousPosition,
			raknet.Vector3{X: result.current.X, Y: result.current.Y, Z: result.current.Z},
			movementNow,
		)
		if err != nil {
			return result, fmt.Errorf("moveCampaignDNA: %w", err)
		}
		result.publications, err = peerSession.zone.AdvanceDirector(
			previous, result.current,
		)
		if err != nil {
			return result, fmt.Errorf("moveCampaignDirector: %w", err)
		}
		for _, publication := range result.publications {
			if zonehorde.IsTrigger(publication) &&
				peerSession.zone.Horde().IsComplete(publication.MarkerSetName) {
				acceptErr := peerSession.zone.AcceptPublication(publication)
				if acceptErr != nil {
					return result, fmt.Errorf(
						"moveCampaignRestoredHordeAccept: %w", acceptErr,
					)
				}
				continue
			}
			if zonecallback.IsClientOnly(publication.CallbackName) &&
				publication.EventName == "" {
				if publication.CallbackName == zonecallback.OverdriveClient {
					peerSession.isClientBossBoundaryPending = true
				}
				acceptErr := peerSession.zone.AcceptPublication(publication)
				if acceptErr != nil {
					return result, fmt.Errorf(
						"moveCampaignClientCallbackAccept: %w", acceptErr,
					)
				}
				continue
			}
			encounterPlan, planErr := peerSession.zone.PlanInitialEncounter(
				publication, peerSession.binding.GameID,
				peerSession.binding.ChainLevelIndex,
			)
			if planErr != nil {
				return result, fmt.Errorf("moveCampaignEncounterPlan: %w", planErr)
			}
			abilityCount, isAbilityCountFound := peerSession.zone.AbilityCount(
				zoneResultMember(*peerSession),
			)
			if zoneunlock.IsRandomPublication(publication) &&
				isAbilityCountFound &&
				abilityCount == zoneunlock.InitialAbilityBoundary {
				unlockErr := peerSession.zone.AcceptPublication(publication)
				if unlockErr != nil {
					return result, fmt.Errorf("moveCampaignRandomUnlockAccept: %w", unlockErr)
				}
				result.randomUnlockPublication = publication
				continue
			}
			if zoneunlock.IsSupportPublication(publication) {
				unlockErr := peerSession.zone.CanAcceptPublication(publication)
				if unlockErr != nil {
					return result, fmt.Errorf("moveCampaignSupportUnlockCheck: %w", unlockErr)
				}
				abilityCount, isAbilityCountFound := peerSession.zone.AbilityCount(
					zoneResultMember(*peerSession),
				)
				isUnlockNeeded := abilityCount ==
					zoneunlock.FullAbilityBoundary &&
					isAbilityCountFound &&
					peerSession.binding.ChainProgression <
						zoneunlock.SecondChainLevelIndex
				if isUnlockNeeded {
					if peerSession.campaignUnlockPresentationSession().Support() != nil {
						return result, errors.New("moveCampaignSupportUnlock: already active")
					}
					result.supportUnlockRun, unlockErr = unlockraknet.NewSupportRun(
						r.supportUnlock, uint8(peerSession.binding.Slot),
						abilityCount,
					)
					if unlockErr != nil {
						return result, fmt.Errorf("moveCampaignSupportUnlockRun: %w", unlockErr)
					}
				}
				// The initial 1-1 final-arena trigger owns both the support
				// presentation and Illust's encounter. Let ArmBossEncounter accept
				// that shared publication so the support route cannot consume it
				// before boss planning.
				if len(encounterPlan.Boss) == 0 {
					unlockErr = peerSession.zone.AcceptPublication(publication)
					if unlockErr != nil {
						if result.supportUnlockRun != nil {
							result.supportUnlockRun.Stop()
						}
						return result, fmt.Errorf("moveCampaignSupportUnlockAccept: %w", unlockErr)
					}
				}
				if result.supportUnlockRun != nil && len(encounterPlan.Boss) == 0 {
					unlockErr = peerSession.campaignUnlockPresentationSession().
						InstallSupport(result.supportUnlockRun)
					if unlockErr != nil {
						result.supportUnlockRun.Stop()
						return result, fmt.Errorf(
							"moveCampaignSupportUnlockInstall: %w", unlockErr,
						)
					}
					result.supportUnlockPublication = publication
				}
				if len(encounterPlan.Boss) == 0 {
					continue
				}
			}
			if encounterPlan.IsBossDeferred {
				if isAbilityCountFound &&
					abilityCount == zoneunlock.RandomAbilityBoundary {
					unlockPacket, marshalErr := unlockraknet.AbilityCount(
						peerSession.binding.Slot,
						zoneunlock.FullAbilityBoundary,
					)
					if marshalErr != nil {
						return result, fmt.Errorf(
							"moveCampaignBossAbilityMarshal: %w", marshalErr,
						)
					}
					isRaised := peerSession.zone.RaiseAbilityCount(
						zoneResultMember(*peerSession),
						zoneunlock.FullAbilityBoundary,
					)
					if isRaised {
						result.unlockPackets = append(
							result.unlockPackets, unlockPacket,
						)
						r.logger.Printf(
							"RakNet campaign squad ability unlocked at deferred boss arena for user=%d",
							peerSession.binding.UserID,
						)
					}
				}
				if result.supportUnlockRun != nil {
					result.supportUnlockRun.Stop()
					result.supportUnlockRun = nil
				}
				continue
			}
			plannedHorde := encounterPlan.Horde
			if len(plannedHorde) == 0 {
				plannedBoss := encounterPlan.Boss
				if len(plannedBoss) == 0 {
					continue
				}
				plannedBossPackets, marshalErr := npcraknet.TargetedSpawns(
					plannedBoss[1:], peerSession.deployedObjectID,
				)
				if marshalErr != nil {
					return result, fmt.Errorf("moveCampaignBossMarshal: %w", marshalErr)
				}
				activePacket, marshalErr := bossraknet.AddPhase()
				if marshalErr != nil {
					return result, fmt.Errorf("moveCampaignBossStateMarshal: %w", marshalErr)
				}
				bossErr := peerSession.zone.ArmBossEncounter(
					publication, peerSession.deployedObjectID, plannedBoss,
				)
				if bossErr != nil {
					if result.supportUnlockRun != nil {
						result.supportUnlockRun.Stop()
						result.supportUnlockRun = nil
					}
					if errors.Is(bossErr, zoneboss.ErrSecondHordeIncomplete) {
						continue
					}
					return result, fmt.Errorf("moveCampaignBossArm: %w", bossErr)
				}
				if result.supportUnlockRun != nil {
					installErr := peerSession.campaignUnlockPresentationSession().
						InstallSupport(result.supportUnlockRun)
					if installErr != nil {
						result.supportUnlockRun.Stop()
						return result, fmt.Errorf(
							"moveCampaignBossSupportInstall: %w", installErr,
						)
					}
					result.supportUnlockPublication = publication
				}
				result.bossPlans = plannedBoss
				result.bossPackets = append(plannedBossPackets, activePacket)
				result.bossPublication = publication
				continue
			}
			plannedPackets, marshalErr := npcraknet.TargetedSpawns(
				plannedHorde, peerSession.deployedObjectID,
			)
			if marshalErr != nil {
				return result, fmt.Errorf("moveCampaignHordeMarshal: %w", marshalErr)
			}
			barrierPlans := peerSession.zone.HordeBarrierPlans(publication.MarkerSetName)
			barrierPackets := make([][]byte, 0)
			if len(barrierPlans) != 0 {
				barrierPackets, marshalErr = barrierraknet.Create(barrierPlans)
				if marshalErr != nil {
					return result, fmt.Errorf("moveCampaignHordeBarrierMarshal: %w", marshalErr)
				}
			}
			plannedPackets = append(barrierPackets, plannedPackets...)
			hordeErr := peerSession.zone.AdmitFirstHorde(
				publication, peerSession.deployedObjectID, plannedHorde,
			)
			if hordeErr != nil {
				if errors.Is(hordeErr, zone.ErrHordeDeferred) {
					continue
				}
				return result, fmt.Errorf("moveCampaignHordeAdmit: %w", hordeErr)
			}
			result.hordePlans = append(result.hordePlans, plannedHorde...)
			result.hordePackets = append(result.hordePackets, plannedPackets...)
		}
		if peerSession.binding.Mode == game.ModeChain && len(result.bossPlans) == 0 {
			namedBossPlan, isNamedBossPlanned, namedBossErr :=
				peerSession.zone.PlanBossNearPosition(
					result.current, peerSession.binding.GameID,
					peerSession.binding.ChainLevelIndex,
				)
			if namedBossErr != nil {
				return result, fmt.Errorf("moveCampaignNearBossPlan: %w", namedBossErr)
			}
			if isNamedBossPlanned {
				var isNamedBossAdmitted bool
				result.namedBossPlans, result.namedBossPackets,
					isNamedBossAdmitted, namedBossErr =
					peerSession.admitCampaignGenericBossPlan(namedBossPlan)
				if namedBossErr != nil {
					return result, fmt.Errorf("moveCampaignNearBossAdmit: %w", namedBossErr)
				}
				if !isNamedBossAdmitted {
					result.namedBossPlans = nil
					result.namedBossPackets = nil
				} else {
					peerSession.isClientBossBoundaryPending = false
					result.overdriveUnlock = peerSession.
						campaignUnlockPresentationSession().Overdrive()
				}
			}
		}
		if isTutorialAbilityMarkerEntered {
			abilityCount, isAbilityCountFound := peerSession.zone.AbilityCount(
				zoneResultMember(*peerSession),
			)
			presentation := peerSession.campaignUnlockPresentationSession()
			if isAbilityCountFound &&
				abilityCount == zoneunlock.TutorialInitialBoundary &&
				presentation.TutorialAbility() == nil {
				abilityUnlock, unlockErr := unlockraknet.NewAbilityUnlockRun(
					r.program.IntroAbilitySecond.Program,
					uint8(peerSession.binding.Slot),
				)
				if unlockErr != nil {
					return result, fmt.Errorf("moveTutorialAbilityUnlockRun: %w", unlockErr)
				}
				result.tutorialAbilityUnlock = abilityUnlock
			}
		}
		if isTutorialCreatureMarkerEntered &&
			peerSession.binding.Creatures[1].Noun == 0 {
			presentation := peerSession.campaignUnlockPresentationSession()
			if presentation.TutorialCreature() == nil {
				sageBinding := tutorialSageBinding(peerSession.binding)
				creatureUnlock, unlockErr := unlockraknet.NewSecondCreatureUnlockRun(
					r.program.IntroSecondCreature.Program, sageBinding,
					raknet.Vector3{
						X: result.current.X, Y: result.current.Y, Z: result.current.Z,
					},
				)
				if unlockErr != nil {
					return result, fmt.Errorf("moveTutorialCreatureUnlockRun: %w", unlockErr)
				}
				result.tutorialCreatureUnlock = creatureUnlock
			}
		}
		if isTutorialCapsuleMarkerEntered {
			result.tutorialCapsulePackets, err =
				peerSession.unlockTutorialCapsules()
			if err != nil {
				return result, fmt.Errorf("moveTutorialCapsules: %w", err)
			}
		}
	}
	return result, nil
}

func (s *gameplayPeerSession) admitCampaignGenericBossAfterHorde(
	completion zonehorde.Completion,
) ([]zonenpc.SpawnPlan, [][]byte, bool, error) {
	if s == nil {
		return nil, nil, false, nil
	}
	encounterPlan, isPlanned, err := s.zone.PlanBossAfterHorde(
		completion, s.binding.GameID, s.binding.ChainLevelIndex,
	)
	if err != nil {
		return nil, nil, false, fmt.Errorf("genericBossPlan: %w", err)
	}
	if !isPlanned {
		return nil, nil, false, nil
	}
	return s.admitCampaignGenericBossPlan(encounterPlan)
}

func (s *gameplayPeerSession) admitCampaignGenericBossPlan(
	encounterPlan zone.NamedBossPlan,
) ([]zonenpc.SpawnPlan, [][]byte, bool, error) {
	namedPublication := encounterPlan.NamedPublication
	publication := encounterPlan.Publication
	plans := encounterPlan.Actors
	livePlans := plans
	isCaptainWave := len(plans) > 1 && plans[0].IsCaptain
	if isCaptainWave {
		livePlans = plans[1:]
	}
	var err error
	var catalystUnlock *unlockraknet.CatalystRun
	if publication.CallbackName == zonecallback.CatalystUnlock {
		if s.campaignUnlockPresentationSession().Catalyst() != nil {
			return nil, nil, false, errors.New("genericBossCatalyst: already active")
		}
		catalystUnlock, err = unlockraknet.NewCatalystRun(
			s.zone.CatalystProgram(), uint8(s.binding.Slot),
		)
		if err != nil {
			return nil, nil, false, fmt.Errorf("genericBossCatalyst: %w", err)
		}
	}
	var overdriveUnlock *unlockraknet.Run
	if publication.CallbackName == zonecallback.OverdriveUnlock &&
		!s.binding.IsOverdriveUnlocked {
		if s.campaignUnlockPresentationSession().Overdrive() != nil {
			stopCampaignBossUnlocks(catalystUnlock, nil)
			return nil, nil, false, errors.New("genericBossOverdrive: already active")
		}
		overdriveUnlock, err = unlockraknet.NewOverdriveRun(
			s.zone.OverdriveProgram(), uint8(s.binding.Slot),
		)
		if err != nil {
			stopCampaignBossUnlocks(catalystUnlock, nil)
			return nil, nil, false, fmt.Errorf("genericBossOverdrive: %w", err)
		}
	}
	packets, err := npcraknet.TargetedSpawns(livePlans, s.deployedObjectID)
	if err != nil {
		stopCampaignBossUnlocks(catalystUnlock, overdriveUnlock)
		return nil, nil, false, fmt.Errorf("genericBossMarshal: %w", err)
	}
	if !isCaptainWave && !isCampaignBossIntroDelayed(plans[0]) {
		activePacket, activeErr := bossraknet.Active(
			plans[0].ObjectID, zoneboss.IsFinalBossNoun(plans[0].NounName),
		)
		if activeErr != nil {
			stopCampaignBossUnlocks(catalystUnlock, overdriveUnlock)
			return nil, nil, false, fmt.Errorf("genericBossActive: %w", activeErr)
		}
		packets = append(packets, activePacket)
	}
	err = s.zone.AdmitNamedBossEncounter(
		namedPublication, publication, s.deployedObjectID, plans,
	)
	if err != nil {
		stopCampaignBossUnlocks(catalystUnlock, overdriveUnlock)
		return nil, nil, false, fmt.Errorf("genericBossAdmit: %w", err)
	}
	if catalystUnlock != nil {
		err = s.campaignUnlockPresentationSession().InstallCatalyst(catalystUnlock)
		if err != nil {
			rollbackErr := s.zone.NPCs().RollbackAdd(livePlans)
			stopCampaignBossUnlocks(catalystUnlock, overdriveUnlock)
			return nil, nil, false, fmt.Errorf(
				"genericBossCatalystInstall: %w", errors.Join(err, rollbackErr),
			)
		}
	}
	if overdriveUnlock != nil {
		err = s.campaignUnlockPresentationSession().InstallOverdrive(overdriveUnlock)
		if err != nil {
			s.campaignUnlockPresentationSession().ClearCatalyst(catalystUnlock)
			rollbackErr := s.zone.NPCs().RollbackAdd(livePlans)
			stopCampaignBossUnlocks(catalystUnlock, overdriveUnlock)
			return nil, nil, false, fmt.Errorf(
				"genericBossOverdriveInstall: %w", errors.Join(err, rollbackErr),
			)
		}
		s.binding.IsOverdriveUnlocked = true
		s.isOverdrivePersistencePending = true
	}
	return livePlans, packets, true, nil
}

func stopCampaignBossUnlocks(
	catalyst *unlockraknet.CatalystRun, overdrive *unlockraknet.Run,
) {
	if catalyst != nil {
		catalyst.Stop()
	}
	if overdrive != nil {
		overdrive.Stop()
	}
}
