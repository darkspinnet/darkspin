package gameplay

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/playerstat"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/squad"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
	zonecontent "github.com/darkspinnet/darkspin/server/zone/content"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	geometryraknet "github.com/darkspinnet/darkspin/server/zone/geometry/raknet103"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const campaignHeldMeleeCursorRadius = float32(3)
const campaignProjectileCursorRadius = float32(3)

const campaignDirectAggroNeighborGap = float32(3)
const campaignPursuitCheckInterval = 100 * time.Millisecond
const campaignPlayerPursuitRedirectDistance = float32(0.75)

type campaignPursuitTimeoutProducer struct {
	action              campaignActionAuthority
	logger              *log.Logger
	sessionKey          string
	sessionGeneration   uint64
	transportGeneration uint64
	pursuitGeneration   uint64
	sourceObjectID      uint32
	targetObjectID      uint32
	syncStamp           uint8
	timeout             time.Duration
}

type campaignPursuitProgressProducer struct {
	runtime             campaignAbilityCommandRuntime
	packet              raknet.Packet
	sessionKey          string
	sessionGeneration   uint64
	transportGeneration uint64
	pursuitGeneration   uint64
	command             raknet.ActionCommandData
	stopDistance        float32
	lastTargetPosition  game.Vec3
	startedAt           time.Time
}

func (r campaignAbilityCommandRuntime) cancelPlayerPursuitAdmission(
	sessionKey string, sessionGeneration uint64, pursuitGeneration uint64,
	now time.Time,
) error {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != sessionGeneration {
		r.registry.mutex.Unlock()
		return nil
	}
	pursuit := peerSession.campaignPlayerPursuitSession().Snapshot()
	if !pursuit.IsActive || pursuit.Generation != pursuitGeneration {
		r.registry.mutex.Unlock()
		return nil
	}
	peerSession.campaignPlayerPursuitSession().Cancel()
	err := peerSession.stopPlayerMovement(now)
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	if err != nil {
		return fmt.Errorf("pursuitStop: %w", err)
	}
	return nil
}

func (e campaignPursuitProgressProducer) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	// Other movement producers can advance the shared motion while this
	// callback waits for the lock. Sample time only after owning that state.
	now := e.runtime.now()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	pursuit := peerSession.campaignPlayerPursuitSession().Snapshot()
	isWorldAvailable := peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if peerSession.binding.Mode == game.ModeArena {
		match := e.runtime.registry.arenaMatches[peerSession.binding.GameID]
		isWorldAvailable = match != nil && match.isCombatActive && peerSession.isArenaCombatActive
	}
	isCurrent := isFound && peerSession.generation == e.sessionGeneration &&
		isWorldAvailable &&
		pursuit.IsActive && pursuit.Generation == e.pursuitGeneration
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	targets := e.runtime.registry.abilityTargetsLocked(e.runtime, peerSession, sim.AbilityDefinition{}, e.packet.SourceTime)
	target, isTargetFound := targets.NPC(pursuit.TargetObjectID)
	if !pursuit.IsGround && (!isTargetFound || target.IsDefeated || target.HitPoint <= 0) {
		cancelPacket, err := abilityraknet.Acknowledge(
			abilityraknet.AcknowledgeRequest{
				SyncStamp: pursuit.SyncStamp, ResponseType: raknet.ActionResponseRejected,
				ObjectID: e.command.Common.ObjectID,
			},
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignPursuitTargetReject: %w", err)
		}
		peerSession.campaignPlayerPursuitSession().Cancel()
		movementErr := peerSession.stopPlayerMovement(now)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		e.runtime.registry.mutex.Unlock()
		e.runtime.registry.clearPursuitActionLeases(
			e.sessionKey, e.transportGeneration, e.command.Common.ObjectID,
		)
		if movementErr != nil {
			e.runtime.logger.Printf(
				"RakNet campaign pursuit target loss movement cleanup skipped source=%d target=%d: %v",
				e.command.Common.ObjectID, pursuit.TargetObjectID, movementErr,
			)
		}
		e.runtime.logger.Printf(
			"RakNet campaign pursuit target lost source=%d target=%d sync=%d",
			e.command.Common.ObjectID, pursuit.TargetObjectID, pursuit.SyncStamp,
		)
		packets := [][]byte{cancelPacket}
		stopPackets, stopErr := marshalZonePlayerStop(
			e.command.Common.ObjectID, peerSession.playerPosition,
		)
		if stopErr != nil {
			e.runtime.logger.Printf(
				"RakNet campaign pursuit target loss presentation skipped source=%d: %v",
				e.command.Common.ObjectID, stopErr,
			)
			return packets, nil
		}
		return append(packets, stopPackets...), nil
	}
	targetPosition := target.Plan.Position
	if pursuit.IsGround {
		targetPosition = pursuit.GroundPosition
	}
	pursuitGoal := targetPosition
	if !pursuit.IsGround {
		var goalErr error
		pursuitGoal, goalErr = zoneaction.PlayerPursuitDestination(
			e.runtime.registry.abilityNavigationLocked(peerSession), zonePosition(peerSession.playerPosition), targetPosition,
			peerSession.deployedCampaignNavigationRadius(), uint8(peerSession.binding.Mode), e.stopDistance,
		)
		if goalErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignPursuitGoal: %w", goalErr)
		}
	}
	_, playerPosition, err := peerSession.advancePlayerPursuitMovement(
		now, raknet.Vector3{},
		raknet.Vector3(pursuitGoal),
		e.runtime.registry.passiveMovementIncrease(peerSession),
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignPursuitAdvance: %w", err)
	}
	distance := zonegeometry.Distance(
		game.Vec3{
			X: playerPosition.X,
			Y: playerPosition.Y,
			Z: playerPosition.Z,
		},
		targetPosition,
	)
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	if distance <= e.stopDistance {
		movementErr := peerSession.stopPlayerMovement(now)
		if movementErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignPursuitArrivalStop: %w", movementErr)
		}
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		command := e.command
		command.Common.Position = playerPosition
		command.Common.Unknown[0] = pursuit.SyncStamp
		command.Ability.TargetID = pursuit.TargetObjectID
		command.Ability.TargetPosition = raknet.Vector3{
			X: targetPosition.X,
			Y: targetPosition.Y,
			Z: targetPosition.Z,
		}
		if pursuit.IsGround {
			command.Ability.CursorPosition = command.Ability.TargetPosition
		}
		packet := e.packet
		packet.SourceTime += uint64(now.Sub(e.startedAt) / time.Millisecond)
		e.runtime.registry.mutex.Unlock()
		e.runtime.registry.clearPursuitActionLeases(
			e.sessionKey, e.transportGeneration, command.Common.ObjectID,
		)
		e.runtime.logger.Printf(
			"RakNet campaign basic pursuit arrived source=%d target=%d distance=%g stop=%g",
			command.Common.ObjectID, pursuit.TargetObjectID, distance, e.stopDistance,
		)
		stopPackets, stopErr := marshalZonePlayerStop(
			command.Common.ObjectID, playerPosition,
		)
		if stopErr != nil {
			return nil, fmt.Errorf("campaignPursuitArrivalMarshal: %w", stopErr)
		}
		actionPackets, actionErr := e.runtime.handleCharacter(
			context.Background(), packet, command, peerSession,
		)
		if actionErr != nil {
			cancelErr := e.runtime.cancelPlayerPursuitAdmission(
				e.sessionKey, e.sessionGeneration, e.pursuitGeneration, now,
			)
			rejectPacket, rejectErr := actionraknet.Reject(command)
			if rejectErr != nil {
				return nil, fmt.Errorf("campaignPursuitArrivalReject: %w", rejectErr)
			}
			e.runtime.logger.Printf(
				"RakNet campaign basic pursuit arrival rejected source=%d target=%d: %v",
				command.Common.ObjectID, pursuit.TargetObjectID,
				errors.Join(actionErr, cancelErr),
			)
			return append(stopPackets, rejectPacket), nil
		}
		e.runtime.registry.mutex.RLock()
		current, isCurrentFound := e.runtime.registry.sessions[e.sessionKey]
		isPursuitCurrent := false
		if isCurrentFound && current.generation == e.sessionGeneration &&
			current.campaignPlayerPursuit != nil {
			currentPursuit := current.campaignPlayerPursuit.Snapshot()
			isPursuitCurrent = currentPursuit.IsActive &&
				currentPursuit.Generation == e.pursuitGeneration
		}
		e.runtime.registry.mutex.RUnlock()
		if isPursuitCurrent {
			next := e
			next.lastTargetPosition = targetPosition
			err = e.schedule(next)
			if err != nil {
				cancelErr := e.runtime.cancelPlayerPursuitAdmission(
					e.sessionKey, e.sessionGeneration, e.pursuitGeneration, now,
				)
				rejectPacket, rejectErr := actionraknet.Reject(command)
				if rejectErr != nil {
					return nil, fmt.Errorf(
						"campaignPursuitArrivalRescheduleReject: %w", rejectErr,
					)
				}
				e.runtime.logger.Printf(
					"RakNet campaign basic pursuit arrival reschedule rejected source=%d target=%d: %v",
					command.Common.ObjectID, pursuit.TargetObjectID,
					errors.Join(err, cancelErr),
				)
				return append(stopPackets, rejectPacket), nil
			}
			e.runtime.logger.Printf(
				"RakNet campaign basic pursuit target moved during arrival source=%d target=%d; continuing",
				command.Common.ObjectID, pursuit.TargetObjectID,
			)
		}
		return append(stopPackets, actionPackets...), nil
	}
	e.runtime.registry.mutex.Unlock()

	next := e
	next.lastTargetPosition = targetPosition
	err = e.schedule(next)
	if err != nil {
		return nil, fmt.Errorf("campaignPursuitReschedule: %w", err)
	}
	if zonegeometry.Distance(e.lastTargetPosition, targetPosition) <
		campaignPlayerPursuitRedirectDistance {
		return nil, nil
	}
	packets, err := actionraknet.PursuitRedirect(
		e.command.Common.ObjectID,
		game.Vec3{
			X: playerPosition.X,
			Y: playerPosition.Y,
			Z: playerPosition.Z,
		},
		pursuit.TargetObjectID, targetPosition, e.stopDistance,
	)
	if err != nil {
		return nil, fmt.Errorf("campaignPursuitRedirect: %w", err)
	}
	return packets, nil
}

func (e campaignPursuitProgressProducer) schedule(
	next campaignPursuitProgressProducer,
) error {
	producers := e.runtime.registry.producerGuard.scheduledProducers(
		e.sessionKey, []raknet.ScheduledPacketProducer{{
			Delay: campaignPursuitCheckInterval, Produce: next.produce,
		}},
	)
	var cancel raknet.CancelSchedule
	var err error
	if e.packet.ScheduleGroupResult != nil {
		cancel, err = e.packet.ScheduleGroupResult(producers, nil)
	} else if e.packet.ScheduleGroup != nil {
		cancel, err = e.packet.ScheduleGroup(producers)
	} else {
		return errors.New("campaign pursuit reschedule unavailable")
	}
	if err != nil {
		return fmt.Errorf("pursuitSchedule: %w", err)
	}
	if cancel == nil {
		return errors.New("campaign pursuit reschedule returned no cancellation")
	}
	return nil
}

func (p campaignPursuitTimeoutProducer) produce() ([][]byte, error) {
	playerPosition, isExpired, movementErr := p.action.expirePursuit(
		p.sessionKey, p.sessionGeneration, p.pursuitGeneration,
	)
	if !isExpired {
		return nil, nil
	}
	p.action.registry.clearPursuitActionLeases(
		p.sessionKey, p.transportGeneration, p.sourceObjectID,
	)
	if movementErr != nil {
		p.logger.Printf(
			"RakNet campaign basic pursuit timeout movement cleanup skipped source=%d target=%d: %v",
			p.sourceObjectID, p.targetObjectID, movementErr,
		)
	}
	cancelPacket, err := abilityraknet.Acknowledge(
		abilityraknet.AcknowledgeRequest{
			SyncStamp: p.syncStamp, ResponseType: raknet.ActionResponseRejected,
			ObjectID: p.sourceObjectID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("campaignBasicPursuitTimeout: %w", err)
	}
	p.logger.Printf(
		"RakNet campaign basic pursuit expired source=%d target=%d sync=%d timeout=%s",
		p.sourceObjectID, p.targetObjectID, p.syncStamp, p.timeout,
	)
	packets := [][]byte{cancelPacket}
	stopPackets, stopErr := marshalZonePlayerStop(
		p.sourceObjectID, playerPosition,
	)
	if stopErr != nil {
		p.logger.Printf(
			"RakNet campaign basic pursuit timeout presentation skipped source=%d: %v",
			p.sourceObjectID, stopErr,
		)
		return packets, nil
	}
	return append(packets, stopPackets...), nil
}

func (r campaignAbilityCommandRuntime) handleBasic(
	ctx context.Context, request campaignCharacterAbilityRequest,
) ([][]byte, error) {
	var err error
	packet := request.packet
	command := request.command
	commandSession := request.commandSession
	abilityStartTime := request.startTime
	targetObjectID := request.targetObjectID
	deployedCreature := request.deployedCreature
	pursuitAdmission := r.action.admitPursuit(
		packet.Address.String(), command.Common.ObjectID,
		command.Ability.TargetID, command.Ability.Index,
		command.Common.Unknown[0],
	)
	if pursuitAdmission.pursuit.IsActive && command.Ability.TargetID != 0 &&
		pursuitAdmission.pursuit.SourceObjectID == command.Common.ObjectID &&
		pursuitAdmission.pursuit.AbilityIndex == command.Ability.Index {
		activeTargetObjectID := pursuitAdmission.pursuit.TargetObjectID
		r.logger.Printf("RakNet campaign basic pursuit retry source=%d active_target=%d request_target=%d index=%d pursuit_sync=%d request_sync=%d authority_position=(%g,%g,%g) reported_position=(%g,%g,%g)",
			command.Common.ObjectID, activeTargetObjectID, command.Ability.TargetID,
			command.Ability.Index,
			pursuitAdmission.pursuit.SyncStamp, command.Common.Unknown[0],
			pursuitAdmission.playerPosition.X, pursuitAdmission.playerPosition.Y,
			pursuitAdmission.playerPosition.Z, command.Common.Position.X,
			command.Common.Position.Y, command.Common.Position.Z)
	}
	activeAbilityID := uint32(0)
	activeAbility := zonecontent.HeroAbility{}
	activeDefinition := sim.AbilityDefinition{}
	activeSlot := zonecontent.HeroAbilitySlot(0)
	activeSlotName := ""
	switch command.Ability.Index {
	case 2:
		activeSlot = zonecontent.HeroAbilitySpecialTwo
		activeSlotName = "special-two"
	case 3:
		activeSlot = zonecontent.HeroAbilityRandom
		activeSlotName = "random"
	case 6, 7, 8:
		supportCreatureIndex := command.Ability.Index - 6
		if supportCreatureIndex >= uint32(len(commandSession.binding.Creatures)) {
			return request.reject("special-one creature unavailable")
		}
		deployedCreature = commandSession.binding.Creatures[supportCreatureIndex]
		activeSlot = zonecontent.HeroAbilitySpecialOne
		activeSlotName = "special-one"
	}
	if activeSlotName != "" {
		var isActiveFound bool
		activeAbility, isActiveFound = r.program.HeroAbility(
			deployedCreature.Noun, activeSlot,
		)
		if !isActiveFound || activeAbility.ID == 0 {
			return request.reject(activeSlotName + " ability identity unavailable")
		}
		if activeAbility.Definition.Name == "" {
			return request.reject(activeSlotName + " ability definition unavailable: " + activeAbility.AssetName)
		}
		if !isGenericHeroActiveKind(activeAbility.Definition.Kind) {
			return request.reject(activeSlotName + " ability runtime unavailable: " + activeAbility.AssetName)
		}
		activeAbilityID = activeAbility.ID
		activeDefinition = projectHeroAbilityRank(
			activeAbility.Definition, command.Ability.Rank,
		)
		activeAbility.Definition = activeDefinition
	} else if command.Ability.Index != 0 {
		return request.reject("unsupported character ability index")
	}
	isActiveRequest := activeAbilityID != 0
	isElectronSphereRequest := activeDefinition.Name == "PlasmaRandom_LightningBall"
	if isActiveRequest && activeDefinition.Kind == sim.AbilityKindModifier {
		return r.handleHeroSelfModifier(
			request, activeAbility, zoneability.HeroAbilityCooldown(activeAbilityID),
		)
	}
	if packet.ScheduleGroup == nil && packet.ScheduleGroupResult == nil {
		return nil, errors.New("campaignBasicSchedule: unavailable")
	}
	sessionKey := packet.Address.String()
	r.registry.mutex.Lock()
	abilityStartTime = r.now()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isSessionCurrent := isFound &&
		peerSession.generation == commandSession.generation &&
		command.Common.ObjectID == peerSession.deployedObjectID &&
		peerSession.deployedCreatureIndex < uint32(len(peerSession.binding.Creatures)) &&
		((peerSession.binding.Mode == game.ModeArena && peerSession.isArenaCombatActive) ||
			(peerSession.zone != nil && peerSession.zone.NPCs() != nil && peerSession.zone.Population() != nil))
	pendingBasic, isPendingBasic := ctx.Value(campaignPendingBasicKey{}).(*campaignPendingBasic)
	if isPendingBasic {
		if !pendingBasic.isCurrent(peerSession, isFound) {
			r.registry.mutex.Unlock()
			retainBasicAction(ctx)
			return nil, nil
		}
		peerSession.pendingBasic = nil
		r.registry.sessions[sessionKey] = peerSession
	}
	var interruptedBasic *abilityraknet.MeleeRun
	isActiveCooldownReady := isSessionCurrent && isActiveRequest &&
		peerSession.abilityCooldownSession().IsReady(
			zoneability.HeroAbilityCooldown(activeAbilityID), abilityStartTime,
		)
	if isActiveCooldownReady && peerSession.basicAttack != nil {
		interruptedBasic = peerSession.resetSharedActionAdmission()
		r.registry.sessions[sessionKey] = peerSession
	}
	defer interruptedBasic.Stop()
	basicHeldGeneration, isBasicHeldRepeat := ctx.Value(basicHeldContextKey{}).(uint64)
	isBasicHeldInput := !isActiveRequest && command.Ability.TargetID == 0 &&
		byte(command.Ability.Unknown) == 1
	basicHeldAbility := *command.Ability
	isBasicHeldCurrent := !isBasicHeldRepeat || isFound &&
		peerSession.basicSequenceSession().IsHeldAt(basicHeldGeneration)
	if isSessionCurrent && isBasicHeldRepeat && isBasicHeldCurrent &&
		peerSession.pendingBasic != nil {
		// The queued native request owns the next cycle and its response token.
		// Recheck under the admission lock in case it arrived after the repeat
		// producer inspected the session.
		r.registry.mutex.Unlock()
		retainBasicAction(ctx)
		return nil, nil
	}
	isBasicHeldAimRefresh := !isBasicHeldRepeat && isBasicHeldInput &&
		peerSession.basicSequenceSession().IsHeld()
	if isBasicHeldAimRefresh {
		peerSession.basicHeldAbility = basicHeldAbility
		peerSession.isBasicHeldAbilitySet = true
		r.registry.sessions[sessionKey] = peerSession
	}
	isBasicReady := isFound && peerSession.basicAttack == nil &&
		peerSession.basicSequenceSession().IsReady(abilityStartTime) &&
		peerSession.isAbilityReleaseReady(abilityStartTime) &&
		isBasicHeldCurrent
	isActiveReady := isActiveCooldownReady &&
		peerSession.basicAttack == nil &&
		peerSession.isAbilityReleaseReady(abilityStartTime)
	isAccepted := isSessionCurrent &&
		(isActiveRequest && isActiveReady || !isActiveRequest && isBasicReady)
	if !isAccepted {
		basicCycleRemaining := peerSession.basicSequenceSession().CooldownEnd().Sub(
			abilityStartTime,
		)
		if isSessionCurrent && !isActiveRequest && !isBasicHeldRepeat &&
			isBasicHeldCurrent &&
			(peerSession.basicAttack != nil || basicCycleRemaining > 0) {
			// Targetless native repeats also predict a new action token. Merely
			// refreshing held aim leaves that prediction unanswered after release.
			pending := &campaignPendingBasic{
				runtime: r, packet: packet, command: command, sessionKey: sessionKey,
				generation:          peerSession.generation,
				transportGeneration: peerSession.transportGeneration,
				heldGeneration:      peerSession.basicSequenceSession().HeldGeneration(),
				startedAt:           abilityStartTime,
			}
			delay := pendingBasicDelay(peerSession, abilityStartTime)
			pending.expiresAt = abilityStartTime.Add(delay + 2*time.Second)
			pendingErr := peerSession.cancelPendingBasic()
			if pendingErr != nil {
				r.registry.mutex.Unlock()
				return nil, fmt.Errorf("pendingReplace: %w", pendingErr)
			}
			peerSession.pendingBasic = pending
			r.registry.sessions[sessionKey] = peerSession
			r.registry.mutex.Unlock()
			err := pending.schedule(delay)
			if err != nil {
				return pending.reject(err)
			}
			retainBasicAction(ctx)
			return nil, nil
		}
		r.registry.mutex.Unlock()
		if isSessionCurrent && !isActiveRequest && isBasicHeldRepeat &&
			isBasicHeldCurrent && basicCycleRemaining > 0 {
			// A server-generated repeat carries the preceding accepted token;
			// it must not interrupt that cycle or replace a newer native request.
			retainBasicAction(ctx)
			return nil, nil
		}
		return request.reject("session unavailable or busy")
	}
	enemies := r.registry.abilityTargetsLocked(r, peerSession, sim.AbilityDefinition{}, packet.SourceTime)
	creature := r.registry.projectPassiveCreature(peerSession,
		peerSession.deployedCreatureIndex, abilityStartTime,
	)
	definition, isDefinitionFound := r.program.PlayerBasicAbility[creature.Noun]
	if isActiveRequest {
		definition = activeDefinition
		isDefinitionFound = definition.Name != ""
	}
	if !isDefinitionFound {
		reason := r.program.PlayerBasicUnsupported[creature.Noun]
		if reason == "" {
			reason = "definition unavailable"
		}
		r.registry.mutex.Unlock()
		return request.reject(reason)
	}
	if !isActiveRequest && definition.Kind == sim.AbilityKindProjectile {
		targetObjectID = zoneability.CursorTarget(
			enemies, command.Common.ObjectID,
			game.Vec3(command.Ability.CursorPosition),
			game.Vec3(command.Ability.TargetPosition),
			campaignProjectileCursorRadius,
		)
		command.Ability.TargetID = targetObjectID
		if targetObjectID == 0 &&
			isReportedZonePosition(command.Ability.CursorPosition) &&
			isFiniteZonePosition(command.Ability.CursorPosition) {
			command.Ability.TargetPosition = command.Ability.CursorPosition
		}
		request.command = command
		request.targetObjectID = targetObjectID
	}
	reportedPosition := command.Common.Position
	if !isActiveRequest && peerSession.playerMotion != nil &&
		!peerSession.playerMotion.Snapshot().Movement().IsMoving {
		// Basic animations can displace the native rendered pose while the
		// locomotion goal remains stopped. Adopting that pose on every repeat
		// turns animation offsets into permanent movement without a move command.
		reportedPosition = peerSession.playerPosition
	}
	err = peerSession.advancePlayerPosition(abilityStartTime, reportedPosition)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignBasicPosition: %w", err)
	}
	isPursuitRetry := peerSession.campaignPlayerPursuitSession().IsRetry(
		command.Common.ObjectID, command.Ability.TargetID,
		command.Ability.Index, command.Common.Unknown[0],
	)
	if isPursuitRetry {
		targetObjectID = peerSession.campaignPlayerPursuitSession().Snapshot().TargetObjectID
	}
	if isBasicHeldInput && definition.Kind == sim.AbilityKindMelee &&
		!isPursuitRetry {
		// Cursor coordinates select among authoritative NPC positions; they must
		// never relocate an idle enemy when a held attack retains a stale target.
		targetObjectID = zoneability.CursorTarget(
			enemies, command.Common.ObjectID,
			game.Vec3{
				X: command.Ability.CursorPosition.X,
				Y: command.Ability.CursorPosition.Y,
				Z: command.Ability.CursorPosition.Z,
			},
			game.Vec3{
				X: command.Ability.TargetPosition.X,
				Y: command.Ability.TargetPosition.Y,
				Z: command.Ability.TargetPosition.Z,
			},
			campaignHeldMeleeCursorRadius,
		)
	}
	// Build 103 resubmits targetless basics while Shift remains held but does
	// not reliably publish ActionCancel for the ground-fire path. Keep each
	// targetless request single-cycle and let subsequent client requests express
	// a continued hold.
	if targetObjectID == 0 {
		isBasicHeldInput = false
	}
	if isBasicHeldInput {
		peerSession.basicHeldAbility = basicHeldAbility
		peerSession.isBasicHeldAbilitySet = true
	}
	isAreaBasic := definition.Kind == sim.AbilityKindCone ||
		(definition.Kind == sim.AbilityKindCursorArea ||
			definition.Kind == sim.AbilityKindTeleportArea) ||
		definition.Kind == sim.AbilityKindPointBlank &&
			(isActiveRequest || definition.BonusDamageMultiplier > 0)
	if isAreaBasic {
		return r.handleAreaBasic(
			request, peerSession, creature, definition, targetObjectID,
			activeAbilityID, isBasicHeldRepeat, isBasicHeldInput,
			sessionKey, abilityStartTime,
		)
	}
	if definition.Kind == sim.AbilityKindProjectileBurst {
		return r.handleHeroProjectileBurst(
			request, peerSession, creature, definition, activeAbilityID,
			targetObjectID, sessionKey, abilityStartTime,
		)
	}
	if definition.Kind == sim.AbilityKindTrap {
		return r.handleHeroTrap(
			request, peerSession, creature, definition, activeAbilityID,
			sessionKey, abilityStartTime,
		)
	}
	if definition.Kind == sim.AbilityKindChannelDrain {
		return r.handleHeroChannelDrain(
			request, peerSession, creature, definition, activeAbilityID,
			targetObjectID, sessionKey, abilityStartTime,
		)
	}
	if definition.Kind == sim.AbilityKindTimedArea {
		return r.handleHeroTimedArea(
			request, peerSession, creature, definition, activeAbilityID,
			sessionKey, abilityStartTime,
		)
	}
	if definition.Kind == sim.AbilityKindStatusArea {
		return r.handleHeroStatusArea(
			request, peerSession, creature, definition, activeAbilityID,
			sessionKey, abilityStartTime,
		)
	}
	if definition.Kind == sim.AbilityKindInfection {
		return r.handleHeroInfection(
			request, peerSession, creature, definition, activeAbilityID,
			targetObjectID, sessionKey, abilityStartTime,
		)
	}
	if definition.Kind == sim.AbilityKindHealingTicks {
		return r.handleHeroHealingTicks(
			request, peerSession, creature, definition, activeAbilityID,
			targetObjectID, sessionKey, abilityStartTime,
		)
	}
	if definition.Kind == sim.AbilityKindAuraArea {
		return r.handleHeroAuraArea(
			request, peerSession, creature, definition, activeAbilityID,
			sessionKey, abilityStartTime,
		)
	}
	if definition.Kind == sim.AbilityKindProjectileStatus {
		return r.handleHeroProjectileStatus(
			request, peerSession, creature, definition, activeAbilityID,
			targetObjectID, sessionKey, abilityStartTime,
		)
	}
	if definition.Kind == sim.AbilityKindCharge {
		return r.handleHeroCharge(
			request, peerSession, creature, definition, activeAbilityID,
			targetObjectID, sessionKey, abilityStartTime,
		)
	}
	if definition.Kind == sim.AbilityKindRepulsion {
		return r.handleHeroRepulsion(
			request, peerSession, creature, definition, activeAbilityID,
			sessionKey, abilityStartTime,
		)
	}
	if definition.Kind == sim.AbilityKindChannelArea {
		return r.handleHeroChannelArea(
			request, peerSession, creature, definition, activeAbilityID,
			sessionKey, abilityStartTime,
		)
	}
	if definition.Kind == sim.AbilityKindQuantumBlink {
		return r.handleHeroQuantumBlink(
			request, peerSession, creature, definition, activeAbilityID,
			sessionKey, abilityStartTime,
		)
	}
	if definition.Kind == sim.AbilityKindReactiveSummon {
		return r.handlePlasmaSentinelActive(
			request, peerSession, creature, activeAbility, sessionKey,
			abilityStartTime,
		)
	}
	if definition.Kind == sim.AbilityKindModifierArea {
		return r.handleFieldMedicActive(
			request, peerSession, creature, activeAbility, sessionKey,
			abilityStartTime,
		)
	}
	if definition.Kind == sim.AbilityKindSummonBuff {
		if definition.Name == "FireTempestActive" {
			return r.handleFireTempestActive(
				request, peerSession, creature, activeAbility, sessionKey,
				abilityStartTime,
			)
		}
		return r.handleHeroSummon(
			request, peerSession, creature, activeAbility, sessionKey, abilityStartTime,
		)
	}
	if definition.Kind == sim.AbilityKindCloudLob {
		return r.handleCloudLobBasic(
			ctx, request, peerSession, creature, definition, targetObjectID,
			isBasicHeldRepeat, isBasicHeldInput, sessionKey, abilityStartTime,
		)
	}
	if definition.Kind == sim.AbilityKindToss {
		return r.handleTossBasic(
			ctx, request, peerSession, creature, definition, targetObjectID,
			isBasicHeldRepeat, isBasicHeldInput, sessionKey, abilityStartTime,
		)
	}
	actorFootprint := float32(0)
	admissionRange := heroAbilityAdmissionRange(creature, definition)
	maximumRange := admissionRange
	if isPursuitRetry {
		// A target can advance between the scheduled arrival sample and the
		// retried attack admission. Preserve the completed pursuit through one
		// redirect quantum so moving melee targets do not trap both peers in an
		// endless pursuit-transfer loop at the edge of contact range.
		maximumRange += campaignPlayerPursuitRedirectDistance
	}
	targetFootprint := float32(0)
	directAggroPlans := make([]zonenpc.SpawnPlan, 0, 4)
	directAggroPackets := make([][]byte, 0, 1)
	if targetObjectID != 0 {
		// Native 103 sub_9DE710 uses both scaled actor footprints for
		// object-target admission, including ranged attacks. Physics collision
		// radii are separate from this range calculation.
		actorFootprint = peerSession.deployedCampaignActorFootprintRadius()
		targetEnemy, isTargetFound := enemies.NPC(targetObjectID)
		if isTargetFound {
			targetFootprint = targetEnemy.Plan.ActorFootprintRadius()
			maximumRange += actorFootprint + targetFootprint
			if peerSession.binding.Mode != game.ModeArena && !targetEnemy.Plan.IsFixture {
				aggroCandidates := []zonenpc.Snapshot{targetEnemy}
				if definition.Kind == sim.AbilityKindProjectile {
					for _, candidate := range enemies.LiveSnapshots() {
						if candidate.Plan.ObjectID == targetObjectID ||
							candidate.Plan.IsFixture || candidate.TargetObjectID != 0 {
							continue
						}
						maximumNeighborDistance := campaignDirectAggroNeighborGap +
							max(float32(0), targetEnemy.Plan.NPCProfile.FootprintRadius) +
							max(float32(0), candidate.Plan.NPCProfile.FootprintRadius)
						if zonegeometry.Distance(
							targetEnemy.Plan.Position, candidate.Plan.Position,
						) > maximumNeighborDistance {
							continue
						}
						aggroCandidates = append(aggroCandidates, candidate)
					}
				}
				acquiredEnemies := make([]zonenpc.Snapshot, 0, len(aggroCandidates))
				for _, candidate := range aggroCandidates {
					acquiredEnemy, isAcquired, acquireErr :=
						peerSession.zone.NPCs().AcquireTarget(
							candidate.Plan.ObjectID, command.Common.ObjectID,
						)
					if acquireErr != nil {
						r.registry.mutex.Unlock()
						return request.reject(acquireErr.Error())
					}
					if !isAcquired {
						if acquiredEnemy.TargetObjectID == command.Common.ObjectID &&
							!acquiredEnemy.IsActionStarted {
							directAggroPlans = append(
								directAggroPlans, acquiredEnemy.Plan,
							)
						}
						continue
					}
					acquiredEnemies = append(acquiredEnemies, acquiredEnemy)
					directAggroPlans = append(directAggroPlans, acquiredEnemy.Plan)
				}
				if len(acquiredEnemies) != 0 {
					directAggroPackets, err = npcraknet.TargetUpdates(acquiredEnemies)
					if err != nil {
						r.registry.mutex.Unlock()
						return nil, fmt.Errorf("campaignBasicAcquireMarshal: %w", err)
					}
				}
			}
		}
	}
	directAggro := campaignDirectAggroPublication{
		runtime: r.npc, logger: r.logger, packet: packet,
		sessionKey: sessionKey, generation: peerSession.generation,
		plans: directAggroPlans, packetDataItems: directAggroPackets,
	}
	plan, planErr := zoneability.PlanProjected(
		enemies, command.Common.ObjectID, targetObjectID,
		game.Vec3{
			X: peerSession.playerPosition.X, Y: peerSession.playerPosition.Y,
			Z: peerSession.playerPosition.Z,
		},
		creature, definition, maximumRange,
	)
	if planErr == nil && !isActiveRequest &&
		(definition.Kind == sim.AbilityKindProjectile || definition.Kind == sim.AbilityKindMelee) &&
		targetObjectID == 0 {
		// Targetless player basics are cursor-directed. The generic planner's
		// nearby-attacker fallback is useful for autonomous attacks, but would
		// redirect a player's ground-directed attack to an unrelated enemy.
		plan.TargetObjectID = 0
	}
	if planErr == nil && targetObjectID != 0 &&
		definition.Name == "SupportHealerBasic" {
		target, isTargetFound := enemies.NPC(targetObjectID)
		if !isTargetFound || !isSupportHealerBasicImpactTarget(target) {
			planErr = errors.New(
				"support healer basic target is not hostile or destructible",
			)
		}
	}
	if planErr == nil && isElectronSphereRequest {
		impactPosition := command.Ability.TargetPosition
		if !isReportedZonePosition(impactPosition) {
			impactPosition = command.Ability.CursorPosition
		}
		isImpactAccepted := isReportedZonePosition(impactPosition) &&
			isFiniteZonePosition(impactPosition) && isInsideZoneTrigger(
			peerSession.playerPosition, impactPosition, admissionRange,
		)
		if !isImpactAccepted {
			planErr = errors.New("electron sphere cursor unavailable or out of range")
		} else {
			targetObjectID = zoneability.NearestTarget(
				enemies, command.Common.ObjectID,
				game.Vec3{X: impactPosition.X, Y: impactPosition.Y, Z: impactPosition.Z},
				definition.Radius,
			)
			plan.TargetObjectID = targetObjectID
		}
	}
	if planErr != nil {
		var rangeErr zoneability.TargetRangeError
		// Native 103 sub_9E0660 returns pursuit (3) for an out-of-range
		// ability with shouldPursue, including FireRavagerBasic projectiles.
		// Advance the authoritative route instead of rejecting every retry
		// while the client's local pursuit has already moved into range.
		isPursuit := errors.As(planErr, &rangeErr) && targetObjectID != 0 &&
			(definition.Kind == sim.AbilityKindMelee ||
				definition.Kind == sim.AbilityKindProjectile) && definition.IsShouldPursue &&
			targetFootprint > 0
		if isPursuit {
			targetEnemy, isTargetFound := enemies.NPC(targetObjectID)
			if !isTargetFound {
				r.registry.mutex.Unlock()
				return request.reject("pursuit target unavailable")
			}
			request.startTime = abilityStartTime
			pursuitPackets, isPursuitStarted, pursuitErr := r.transferPlayerAbilityPursuit(
				request, peerSession, targetEnemy, admissionRange, isPursuitRetry,
			)
			if pursuitErr != nil {
				return nil, fmt.Errorf("basicPursuit: %w", pursuitErr)
			}
			if !isPursuitStarted {
				return pursuitPackets, nil
			}
			return directAggro.publish(pursuitPackets)
		}
		r.registry.mutex.Unlock()
		return request.reject(planErr.Error())
	}
	if isPursuitRetry {
		peerSession.campaignPlayerPursuitSession().Cancel()
		movementErr := peerSession.stopPlayerMovement(abilityStartTime)
		if movementErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignBasicPursuitArrivalStop: %w", movementErr)
		}
	}
	targetObjectID = plan.TargetObjectID
	if definition.Kind == sim.AbilityKindProjectile {
		return r.handleProjectileBasic(
			request, peerSession, creature, definition, plan, targetObjectID, maximumRange,
			activeAbilityID, isElectronSphereRequest, isBasicHeldRepeat, isBasicHeldInput,
			sessionKey, abilityStartTime, directAggro,
		)
	}
	return r.handleMeleeBasic(
		request, peerSession, creature, definition, plan, targetObjectID,
		activeAbilityID, isBasicHeldRepeat, isBasicHeldInput,
		sessionKey, abilityStartTime, directAggro,
	)
}

func (r campaignAbilityCommandRuntime) handleCharacter(
	ctx context.Context, packet raknet.Packet, command raknet.ActionCommandData,
	commandSession gameplayPeerSession,
) ([][]byte, error) {
	r.damage.activationID = nextDamageActivationID()
	abilityStartTime := r.now()
	targetObjectID := command.Ability.TargetID
	deployedCreature := game.GameplayCreature{}
	if commandSession.deployedCreatureIndex <
		uint32(len(commandSession.binding.Creatures)) {
		deployedCreature =
			commandSession.binding.Creatures[commandSession.deployedCreatureIndex]
	}
	request := campaignCharacterAbilityRequest{
		runtime: r, packet: packet, command: command,
		commandSession: commandSession, startTime: abilityStartTime,
		targetObjectID: targetObjectID, deployedCreature: deployedCreature,
	}
	// Accepted Arena statuses share recipient-owned maps with expiry jobs.
	// Sample control gates under their registry lock before dispatching.
	r.registry.mutex.RLock()
	isSilenced := commandSession.isEnemySilenceActive(abilityStartTime)
	isAsleep := commandSession.isEnemySleepActive(abilityStartTime)
	isDazed := commandSession.isScarabDazed()
	isStunned := commandSession.isEnemyStunActive(abilityStartTime)
	isTerrified := commandSession.isEnemyFearActive(abilityStartTime)
	r.registry.mutex.RUnlock()
	if command.Ability.Index != 0 && isSilenced {
		return request.reject("silenced")
	}
	if isAsleep {
		return request.reject("asleep")
	}
	if isDazed {
		return request.reject("dazed")
	}
	if isStunned {
		return request.reject("stunned")
	}
	if isTerrified {
		return request.reject("terrified")
	}
	specialResponse, specialErr := r.handleSpecial(request)
	if !errors.Is(specialErr, errCampaignAbilityNotSpecial) {
		if specialErr == nil {
			useErr := r.recordFireRavagerUse(packet, command, commandSession, specialResponse, abilityStartTime)
			if useErr != nil {
				return nil, fmt.Errorf("characterSpecialUse: %w", useErr)
			}
			r.recordScenarioAbilityDispatch(packet, command, commandSession, specialResponse, abilityStartTime)
		}
		return specialResponse, specialErr
	}
	packets, err := r.handleBasic(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("characterBasic: %w", err)
	}
	err = r.recordFireRavagerUse(packet, command, commandSession, packets, abilityStartTime)
	if err != nil {
		return nil, fmt.Errorf("characterBasicUse: %w", err)
	}
	r.recordScenarioAbilityDispatch(packet, command, commandSession, packets, abilityStartTime)
	return packets, nil
}

type campaignAbilityCommandRuntime struct {
	actionRuntime *gameplayActionRuntime
	registry      *gameplaySessionRegistry
	action        campaignActionAuthority
	program       Programs
	modifierPool  *modifierPool
	effectPool    *attachedEffectPool
	npc           campaignNPCActionRuntime
	damage        campaignDamageRuntime
	stats         *playerstat.Recorder
	now           func() time.Time
	logger        *log.Logger
}

type campaignCharacterAbilityRequest struct {
	runtime          campaignAbilityCommandRuntime
	packet           raknet.Packet
	command          raknet.ActionCommandData
	commandSession   gameplayPeerSession
	startTime        time.Time
	targetObjectID   uint32
	deployedCreature game.GameplayCreature
}

func (r campaignCharacterAbilityRequest) reject(
	reason string,
) ([][]byte, error) {
	ackPacket, err := actionraknet.Reject(r.command)
	if err != nil {
		return nil, fmt.Errorf("campaignAbilityReject: %w", err)
	}
	r.runtime.logger.Printf(
		"RakNet campaign ability rejected source=%d noun=%#08x target=%d index=%d reason=%s actor=(%.3f,%.3f,%.3f) cursor=(%.3f,%.3f,%.3f) target_position=(%.3f,%.3f,%.3f)",
		r.command.Common.ObjectID, r.deployedCreature.Noun, r.targetObjectID,
		r.command.Ability.Index, reason,
		r.command.Common.Position.X, r.command.Common.Position.Y,
		r.command.Common.Position.Z, r.command.Ability.CursorPosition.X,
		r.command.Ability.CursorPosition.Y, r.command.Ability.CursorPosition.Z,
		r.command.Ability.TargetPosition.X, r.command.Ability.TargetPosition.Y,
		r.command.Ability.TargetPosition.Z,
	)
	return [][]byte{ackPacket}, nil
}

type campaignDirectAggroPublication struct {
	runtime         campaignNPCActionRuntime
	logger          *log.Logger
	packet          raknet.Packet
	sessionKey      string
	generation      uint64
	plans           []zonenpc.SpawnPlan
	packetDataItems [][]byte
}

func (p campaignDirectAggroPublication) publish(
	responsePackets [][]byte,
) ([][]byte, error) {
	if len(p.plans) == 0 {
		return responsePackets, nil
	}
	firstActionPackets, err := p.runtime.scheduleFirstActions(
		p.packet, p.sessionKey, p.generation, p.plans, p.packet.SourceTime,
	)
	if err != nil {
		return nil, fmt.Errorf("campaignBasicAcquireAction: %w", err)
	}
	for _, aggroPlan := range p.plans {
		p.logger.Printf(
			"RakNet campaign enemy acquired by direct attack for %s object=%d noun=%q",
			p.packet.Address, aggroPlan.ObjectID, aggroPlan.NounName,
		)
	}
	responsePackets = append(responsePackets, p.packetDataItems...)
	return append(responsePackets, firstActionPackets...), nil
}

func (r campaignAbilityCommandRuntime) handle(
	ctx context.Context, packet raknet.Packet, command raknet.ActionCommandData,
) ([][]byte, error) {
	r.registry.mutex.RLock()
	commandSession, isSessionFound :=
		r.registry.sessions[packet.Address.String()]
	r.registry.mutex.RUnlock()
	if isSessionFound && command.Ability != nil {
		ability := *command.Ability
		ability.Rank = commandSession.scenarioHeroAbilityRank(ability.Rank)
		command.Ability = &ability
	}
	if isSessionFound && commandSession.binding.Mode == game.ModeArena {
		return r.handleArenaAbility(ctx, packet, command, commandSession)
	}
	if !isSessionFound ||
		(commandSession.binding.Mode != game.ModeChain &&
			commandSession.binding.Mode != game.ModeTutorial) {
		return nil, nil
	}
	if command.Common.Type == raknet.ActionUseSquadAbility &&
		command.Ability != nil {
		isSupportCreatureFound := command.Ability.Index >= 6 && command.Ability.Index <= 8
		supportCreatureIndex := uint32(0)
		if isSupportCreatureFound {
			supportCreatureIndex = command.Ability.Index - 6
			isSupportCreatureFound = supportCreatureIndex <
				uint32(len(commandSession.binding.Creatures))
		}
		supportCreature := game.GameplayCreature{}
		if isSupportCreatureFound {
			supportCreature = commandSession.binding.Creatures[supportCreatureIndex]
		}
		creatureIndex := commandSession.deployedCreatureIndex
		deployedSpecialTwo := zonecontent.HeroAbility{}
		isDeployedSpecialTwoFound := false
		if creatureIndex < uint32(len(commandSession.binding.Creatures)) {
			deployedSpecialTwo, isDeployedSpecialTwoFound = r.program.HeroAbility(
				commandSession.binding.Creatures[creatureIndex].Noun,
				zonecontent.HeroAbilitySpecialTwo,
			)
		}
		isWraithDeathsEmbrace := command.Ability.Index == 2 &&
			isDeployedSpecialTwoFound &&
			deployedSpecialTwo.AssetName == "DeathsEmbrace"
		supportAbility, isSupportAbilityFound := r.program.HeroAbility(
			supportCreature.Noun, zonecontent.HeroAbilitySpecialOne,
		)
		isGenericSupport := isSupportCreatureFound && isSupportAbilityFound &&
			(isGenericHeroActiveKind(supportAbility.Definition.Kind) ||
				supportAbility.AssetName == "ArborealMight") &&
			supportAbility.AssetName != "Ghostform" &&
			supportAbility.AssetName != "EnergySentinelActive"
		isNamedCharacterSupport := isSupportAbilityFound &&
			(supportAbility.AssetName == "LightningRogueSupport" ||
				supportAbility.AssetName == "EnergySentinelActive")
		if isNamedCharacterSupport || isGenericSupport ||
			isWraithDeathsEmbrace {
			// Per-hero special_1 abilities arrive through the squad-action
			// wire family. Preserve the live index while routing them
			// through character combat authority.
			command.Common.Type = raknet.ActionUseCharacterAbility
		}
	}
	if command.Common.Type == raknet.ActionUseSquadAbility &&
		command.Ability != nil {
		return r.handleSquadActivation(packet, command, commandSession)
	}
	if command.Common.Type == raknet.ActionUseCharacterAbility &&
		command.Ability != nil {
		packets, err := r.handleCharacter(ctx, packet, command, commandSession)
		if err != nil {
			return nil, err
		}
		breakPackets, err := r.breakHeroModifierOnAcceptedAbility(
			packet, packet.Address.String(), commandSession.generation, packets,
		)
		if err != nil {
			return nil, fmt.Errorf("campaignAbilityModifierBreak: %w", err)
		}
		return append(packets, breakPackets...), nil
	}
	if command.Ability != nil {
		r.logger.Printf(
			"RakNet campaign action ignored pending runtime support source=%d type=%d target=%d index=%d rank=%d cursor=(%g,%g,%g) target_position=(%g,%g,%g)",
			command.Common.ObjectID, command.Common.Type,
			command.Ability.TargetID, command.Ability.Index,
			command.Ability.Rank, command.Ability.CursorPosition.X,
			command.Ability.CursorPosition.Y,
			command.Ability.CursorPosition.Z,
			command.Ability.TargetPosition.X,
			command.Ability.TargetPosition.Y,
			command.Ability.TargetPosition.Z,
		)
		return nil, nil
	}
	r.logger.Printf(
		"RakNet campaign action ignored pending runtime support source=%d type=%d",
		command.Common.ObjectID, command.Common.Type,
	)
	return nil, nil
}

type campaignAreaReleaseStep struct {
	registry       *gameplaySessionRegistry
	sessionKey     string
	generation     uint64
	areaGeneration uint64
	packet         []byte
}

const celestialCometDamageDelay = 300 * time.Millisecond

type campaignCursorAreaEffectStep struct {
	registry       *gameplaySessionRegistry
	sessionKey     string
	generation     uint64
	areaGeneration uint64
	assetName      string
	muzzleName     string
	objectID       uint32
	position       raknet.Vector3
}

func (e campaignCursorAreaEffectStep) produce() ([][]byte, error) {
	e.registry.mutex.RLock()
	current, isFound := e.registry.sessions[e.sessionKey]
	isCurrent := isFound && current.generation == e.generation &&
		current.areaBasicGeneration == e.areaGeneration
	e.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packet, err := abilityraknet.CursorAreaImpact(e.assetName, e.position)
	if err != nil {
		return nil, fmt.Errorf("cursorAreaEffect: %w", err)
	}
	if e.muzzleName == "" {
		return [][]byte{packet}, nil
	}
	muzzle, muzzleErr := raknet.MarshalApplication(raknet.ObjectPresentationMessage{
		Asset: util.HashID(e.muzzleName), ObjectID: e.objectID,
	})
	if muzzleErr != nil {
		return nil, fmt.Errorf("cursorMuzzle: %w", muzzleErr)
	}
	return [][]byte{muzzle, packet}, nil
}

type campaignTeleportAreaStep struct {
	runtime        campaignAbilityCommandRuntime
	sessionKey     string
	generation     uint64
	areaGeneration uint64
	sourceObjectID uint32
	center         game.Vec3
	effectName     string
}

func (e campaignTeleportAreaStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	current, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && current.generation == e.generation &&
		current.areaBasicGeneration == e.areaGeneration &&
		current.deployedObjectID == e.sourceObjectID &&
		current.deployedHitPoint() > 0
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	destination := raknet.Vector3{
		X: e.center.X, Y: e.center.Y, Z: e.center.Z,
	}
	err := current.teleportPlayer(e.runtime.now(), destination)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("teleportAreaMove: %w", err)
	}
	e.runtime.registry.sessions[e.sessionKey] = current
	e.runtime.registry.mutex.Unlock()

	// TimeRavagerSupport calls nLocomotion.TeleportObject after blinkintime.
	// Its native path (A09A90 -> A20110) sends the teleport snapshot, which
	// moves the client mover as well as updating the reflected object pose.
	packets, err := actionraknet.Teleport(actionraknet.TeleportRequest{
		ObjectID: e.sourceObjectID, Position: e.center,
	})
	if err != nil {
		return nil, fmt.Errorf("teleportAreaPosition: %w", err)
	}
	if e.effectName == "" {
		return packets, nil
	}
	effectPacket, err := abilityraknet.CursorAreaImpact(e.effectName, destination)
	if err != nil {
		return nil, fmt.Errorf("teleportAreaEffect: %w", err)
	}
	return append(packets, effectPacket), nil
}

func (e campaignAreaReleaseStep) produce() ([][]byte, error) {
	e.registry.mutex.RLock()
	current, isFound := e.registry.sessions[e.sessionKey]
	isCurrent := isFound && current.generation == e.generation &&
		current.areaBasicGeneration == e.areaGeneration
	e.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	return [][]byte{e.packet}, nil
}

type campaignAreaLogger interface {
	Printf(format string, args ...any)
}

type campaignPointBlankImpact struct {
	assetName            string
	largeTargetAssetName string
	attackerID           uint32
	source               raknet.Vector3
}

func (e campaignPointBlankImpact) marshal(
	result zoneability.AreaResult,
) ([]byte, error) {
	position := result.Snapshot.Plan.Position
	assetName := e.assetName
	if e.largeTargetAssetName != "" &&
		result.Definition.LargeTargetDamageMultiplier > 0 &&
		(result.Snapshot.Plan.IsCaptain || result.Snapshot.Plan.IsElite ||
			result.Snapshot.Plan.IsBoss) {
		assetName = e.largeTargetAssetName
	}
	if assetName == "" {
		return nil, nil
	}
	return abilityraknet.PointBlankImpact(abilityraknet.PointBlankImpactRequest{
		AssetName: assetName, ObjectID: result.Damage.ObjectID,
		AttackerID: e.attackerID, Source: e.source,
		Target:     raknet.Vector3{X: position.X, Y: position.Y, Z: position.Z},
		IsCritical: result.IsCritical,
	})
}

type campaignAreaHitStep struct {
	runtime             campaignAbilityCommandRuntime
	packet              raknet.Packet
	command             raknet.ActionCommandData
	sessionKey          string
	generation          uint64
	areaGeneration      uint64
	targetObjectID      uint32
	hitTimestamp        uint64
	sourcePosition      game.Vec3
	center              game.Vec3
	creature            game.GameplayCreature
	definition          sim.AbilityDefinition
	binding             game.GameplayBinding
	activeAbilityID     uint32
	abilityRank         int32
	cooldownReservation zoneability.CooldownReservation
	isRetained          bool
	isEffectPresented   bool
	admissionRange      float32
}

func (e campaignAreaHitStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	current, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && current.generation == e.generation &&
		current.areaBasicGeneration == e.areaGeneration
	if e.isRetained {
		isCurrent = isFound && current.generation == e.generation &&
			current.deployedObjectID == e.command.Common.ObjectID &&
			current.deployedHitPoint() > 0
	}
	if current.binding.Mode == game.ModeArena {
		isCurrent = isCurrent && current.isArenaCombatActive
	}
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	enemies := e.runtime.registry.abilityTargetsLocked(e.runtime, current, e.definition, e.hitTimestamp)
	random, randomErr := e.runtime.registry.abilityRandomLocked(current)
	if randomErr != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("areaRandom: %w", randomErr)
	}
	var plan zoneability.AreaPlan
	var err error
	if e.definition.TimeLapse != nil {
		if current.deployedObjectID != e.command.Common.ObjectID ||
			current.deployedCreatureIndex >= uint32(len(current.binding.Creatures)) {
			e.runtime.registry.mutex.Unlock()
			return nil, nil
		}
		e.sourcePosition = game.Vec3(current.playerPosition)
	}
	if e.definition.Name == "TechRandom" {
		e.sourcePosition = game.Vec3(current.playerPosition)
		plan, err = zoneability.PlanZetawattBeam(
			enemies, e.command.Common.ObjectID, e.targetObjectID, e.sourcePosition, e.center, e.creature, e.definition,
		)
	} else if e.definition.Kind == sim.AbilityKindCone {
		plan, err = zoneability.PlanCone(
			enemies, e.command.Common.ObjectID,
			e.sourcePosition, e.center, e.creature, e.definition,
		)
	} else if e.definition.Kind == sim.AbilityKindCursorArea ||
		e.definition.Kind == sim.AbilityKindTeleportArea {
		plan, err = zoneability.PlanCursorAreaWithAdmission(
			enemies, e.command.Common.ObjectID, e.targetObjectID,
			e.sourcePosition, e.center, e.creature, e.definition, e.admissionRange,
		)
	} else if e.definition.Kind == sim.AbilityKindPointBlank {
		if e.definition.BonusDamageMultiplier > 0 {
			plan, err = zoneability.ResolvePointBlankBasicHit(
				enemies, e.command.Common.ObjectID,
				e.sourcePosition, e.creature, e.definition,
			)
		} else {
			plan, err = zoneability.PlanArea(
				enemies, e.command.Common.ObjectID,
				e.sourcePosition, e.creature, e.definition,
			)
		}
	}
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignAreaBasicLivePlan: %w", err)
	}
	dexterity := e.creature.Dexterity
	if e.definition.TimeLapse != nil {
		dexterity = current.binding.Creatures[current.deployedCreatureIndex].Dexterity
	}
	history, isHistoryAvailable := enemies.(zoneability.RecentDamageReader)
	if plan.Definition.TimeLapse != nil && !isHistoryAvailable {
		e.runtime.registry.mutex.Unlock()
		return nil, errors.New("area damage history unavailable")
	}
	err = zoneability.ApplyTimeLapseWithDexterity(
		&plan, history, e.creature, dexterity, e.runtime.now(),
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignTimeLapsePlan: %w", err)
	}
	if current.binding.Mode == game.ModeArena {
		// A victim's cleanup can queue field/modifier removals to the caster.
		// Publish the working source before hits and retain those queued updates.
		e.runtime.registry.sessions[e.sessionKey] = current
	}
	var arenaFlakPackets [][]byte
	if current.binding.Mode == game.ModeArena && plan.Definition.Name == "MissileTempestSupport" {
		arenaFlakPackets, err = e.prepareArenaFlakLocked(&current, plan)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("areaArenaFlak: %w", err)
		}
	}
	results, err := zoneability.CommitArea(
		random, enemies,
		plan, e.creature, current.binding.Difficulty, e.runtime.program.Critical,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignAreaBasicDamage: %w", err)
	}
	if arenaTargets, isArena := enemies.(*arenaAbilityTargets); isArena {
		if plan.Definition.Name == "SpacetimeRandom3" {
			publishErr := arenaTargets.publishCometLocked(plan, results)
			e.runtime.registry.mutex.Unlock()
			arenaTargets.finish()
			if publishErr != nil {
				return nil, fmt.Errorf("cometPublish: %w", publishErr)
			}
			return nil, nil
		}
		e.runtime.registry.mutex.Unlock()
		packets, publishErr := e.publishArena(arenaTargets, plan, results)
		if publishErr != nil {
			arenaTargets.finish()
			return nil, fmt.Errorf("areaArenaPublish: %w", publishErr)
		}
		if plan.Definition.Name == "MissileTempestSupport" {
			// Keep Lua presentation, pre-damage slow and damage on one FIFO
			// queue. Returning damage here overtakes queued modifier creation.
			e.runtime.registry.mutex.Lock()
			e.runtime.registry.queueFieldMedicRealmPacketsLocked(nil, current.binding.GameID, append(arenaFlakPackets, packets...))
			e.runtime.registry.mutex.Unlock()
			arenaTargets.finish()
			return nil, nil
		}
		arenaTargets.finish()
		return append(arenaFlakPackets, packets...), nil
	}
	transitions := make([]campaignDamageTransition, 0, len(results))
	for index, result := range results {
		transition, transitionErr := current.applyCampaignDamageTransition(result.Damage)
		if transitionErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignAreaBasicTransition[%d]: %w", index, transitionErr)
		}
		transitions = append(transitions, transition)
	}
	projectilePackets := make([][]byte, 0)
	if plan.Definition.Name == "MissileTempestSupport" {
		var destroyedCount uint32
		projectilePackets, destroyedCount, err = destroyHostileProjectilesLocked(
			e.runtime, &current, e.sessionKey, plan.Center, plan.Definition.Radius, e.runtime.now(),
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignAreaProjectileControl: %w", err)
		}
		abilityRank := int32(1)
		if e.command.Ability != nil && e.command.Ability.Rank > 0 {
			abilityRank = e.command.Ability.Rank
		}
		flakPackets, flakErr := e.runtime.applyMissileFlakLocked(
			e.packet, &current, e.sessionKey, e.generation,
			abilityRank, destroyedCount, e.hitTimestamp,
		)
		if flakErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignAreaFlak: %w", flakErr)
		}
		projectilePackets = append(projectilePackets, flakPackets...)
	}
	if plan.Definition.Name == poisonNovaName {
		resetPackets, resetErr := e.runtime.applyPoisonNovaCooldownResetLocked(
			e.packet, &current, e.sessionKey, e.generation,
			e.activeAbilityID, e.abilityRank, e.cooldownReservation,
			plan.Definition.Cooldown, e.hitTimestamp,
		)
		if resetErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignPoisonNovaCooldown: %w", resetErr)
		}
		projectilePackets = append(projectilePackets, resetPackets...)
	}
	if e.definition.Kind == sim.AbilityKindTeleportArea {
		now := e.runtime.now()
		for _, result := range results {
			if result.Damage.IsDamageImmune || result.Damage.IsDefeated {
				continue
			}
			duration := zonenpc.CrowdControlDuration(
				result.Snapshot.Plan, e.definition.StatusDuration,
			)
			expiresAt := now.Add(duration)
			err = current.zone.NPCs().ApplyStun(result.Damage.ObjectID, expiresAt)
			if err != nil {
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("campaignTeleportAreaStun: %w", err)
			}
			err = current.zone.NPCs().ApplySilence(result.Damage.ObjectID, expiresAt)
			if err != nil {
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("campaignTeleportAreaSilence: %w", err)
			}
			if e.definition.Name == "TimeRavagerSupport" {
				e.runtime.registry.sessions[e.sessionKey] = current
				freezePacket, freezeErr := freezeChronoObjectLocked(
					e.runtime, e.packet, e.sessionKey, e.generation,
					current.zone, result.Damage.ObjectID,
					duration,
				)
				current = e.runtime.registry.sessions[e.sessionKey]
				if freezeErr != nil {
					e.runtime.registry.mutex.Unlock()
					return nil, fmt.Errorf("campaignTeleportFreeze: %w", freezeErr)
				}
				if len(freezePacket) > 0 {
					projectilePackets = append(projectilePackets, freezePacket)
				}
				stopPackets, stopErr := npcraknet.StopAtPose(
					result.Damage.ObjectID, result.Snapshot.Plan.Position, result.Snapshot.Facing,
				)
				if stopErr != nil {
					e.runtime.registry.mutex.Unlock()
					return nil, fmt.Errorf("campaignTeleportAreaStop: %w", stopErr)
				}
				projectilePackets = append(projectilePackets, stopPackets...)
			}
		}
		if e.definition.Name == "TimeRavagerSupport" {
			freezePackets, freezeErr := freezeHostileProjectilesLocked(
				e.runtime, e.packet, &current, e.sessionKey, e.generation,
				e.command.Common.ObjectID, plan.Center, e.definition.Radius,
				e.definition.RootModifierID, e.definition.StatusDuration,
				e.hitTimestamp, now,
			)
			if freezeErr != nil {
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("campaignTeleportAreaProjectileFreeze: %w", freezeErr)
			}
			projectilePackets = append(projectilePackets, freezePackets...)
		}
	}
	e.runtime.registry.sessions[e.sessionKey] = current
	e.runtime.registry.mutex.Unlock()

	prefix, effect, isEffectAfterDamage, effectErr := e.impactPresentation(plan)
	if effectErr != nil {
		return nil, fmt.Errorf("areaEffect: %w", effectErr)
	}
	prefix = append(projectilePackets, prefix...)
	packets, err := e.runtime.damage.publishAreaResults(
		e.packet, e.sessionKey, e.generation, e.command.Common.ObjectID,
		e.hitTimestamp, e.binding, results, transitions, effect, isEffectAfterDamage,
	)
	if err != nil {
		return nil, fmt.Errorf("campaignAreaBasicPublish: %w", err)
	}
	statusPackets, err := e.runtime.applyAcceptedHitStatus(
		e.packet, e.sessionKey, e.generation, e.command.Common.ObjectID,
		e.hitTimestamp, plan, results,
	)
	if err != nil {
		e.runtime.logger.Printf(
			"RakNet hero accepted-hit status omitted for %s: %v",
			e.sessionKey, err,
		)
		return append(prefix, packets...), nil
	}
	packets = append(packets, statusPackets...)
	hastePackets, err := e.runtime.applyLightspeedHasteSteal(
		e.packet, e.sessionKey, e.generation, e.command.Common.ObjectID,
		e.hitTimestamp, plan, results,
	)
	if err != nil {
		e.runtime.logger.Printf(
			"RakNet Lightspeed Tempest haste transfer omitted for %s: %v",
			e.sessionKey, err,
		)
	} else {
		packets = append(packets, hastePackets...)
	}
	tauntPackets, err := e.runtime.damage.applyAcceptedHitTaunts(
		e.packet, e.sessionKey, e.generation, e.command.Common.ObjectID,
		e.hitTimestamp, plan.Definition, results,
	)
	if err != nil {
		e.runtime.logger.Printf(
			"RakNet hero accepted-hit taunt omitted for %s: %v",
			e.sessionKey, err,
		)
		return append(prefix, packets...), nil
	}
	packets = append(packets, tauntPackets...)
	pullPackets, err := e.runtime.damage.applyAcceptedHitPulls(
		e.packet, e.sessionKey, e.generation, e.command.Common.ObjectID,
		e.hitTimestamp, plan, results,
	)
	if err != nil {
		e.runtime.logger.Printf(
			"RakNet hero accepted-hit pull omitted for %s: %v",
			e.sessionKey, err,
		)
		return append(prefix, packets...), nil
	}
	packets = append(packets, pullPackets...)
	pushPackets, err := e.runtime.damage.applyAcceptedHitPushes(
		e.packet, e.sessionKey, e.generation, e.command.Common.ObjectID,
		e.hitTimestamp, plan, results,
	)
	packets = append(packets, pushPackets...)
	if err != nil {
		e.runtime.logger.Printf(
			"RakNet hero accepted-hit push omitted for %s: %v",
			e.sessionKey, err,
		)
		return append(prefix, packets...), nil
	}
	return append(prefix, packets...), nil
}

type campaignAreaRepeatStep struct {
	runtime        campaignAbilityCommandRuntime
	packet         raknet.Packet
	command        raknet.ActionCommandData
	sessionKey     string
	generation     uint64
	areaGeneration uint64
	heldGeneration uint64
}

func latestBasicHeldCommand(
	peerSession gameplayPeerSession, command raknet.ActionCommandData,
) raknet.ActionCommandData {
	if command.Ability == nil || !peerSession.isBasicHeldAbilitySet {
		return command
	}
	ability := peerSession.basicHeldAbility
	command.Ability = &ability
	return command
}

func (e campaignAreaRepeatStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	current, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && current.generation == e.generation &&
		current.pendingBasic == nil &&
		current.basicSequenceSession().IsHeldAt(e.heldGeneration)
	command := latestBasicHeldCommand(current, e.command)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	ctx := context.WithValue(context.Background(), basicHeldContextKey{}, e.heldGeneration)
	packets, err := e.runtime.handle(ctx, e.packet, command)
	e.runtime.registry.mutex.Lock()
	latest, isLatestFound := e.runtime.registry.sessions[e.sessionKey]
	isRepeated := isLatestFound && latest.generation == e.generation &&
		latest.basicSequenceSession().HeldGeneration() == e.heldGeneration &&
		latest.areaBasicGeneration > e.areaGeneration
	if !isRepeated && isLatestFound && latest.pendingBasic == nil &&
		latest.generation == e.generation &&
		latest.basicSequenceSession().HeldGeneration() == e.heldGeneration {
		latest.basicSequenceSession().ReleaseHeld()
		e.runtime.registry.sessions[e.sessionKey] = latest
	}
	e.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("campaignAreaBasicRepeat: %w", err)
	}
	return packets, nil
}

type campaignAreaScheduleFailure struct {
	registry       *gameplaySessionRegistry
	logger         campaignAreaLogger
	sessionKey     string
	generation     uint64
	areaGeneration uint64
}

func (e campaignAreaScheduleFailure) handle(scheduleErr error) {
	e.registry.mutex.Lock()
	current, isFound := e.registry.sessions[e.sessionKey]
	isCurrent := isFound && current.generation == e.generation &&
		current.areaBasicGeneration == e.areaGeneration
	if isCurrent {
		current.basicSequenceSession().ReleaseHeld()
		e.registry.sessions[e.sessionKey] = current
	}
	e.registry.mutex.Unlock()
	if isCurrent {
		e.logger.Printf(
			"RakNet campaign area basic stopped after schedule failure for %s: %v",
			e.sessionKey, scheduleErr,
		)
	}
}

func (r campaignAbilityCommandRuntime) handleAreaBasic(
	request campaignCharacterAbilityRequest, peerSession gameplayPeerSession,
	creature game.GameplayCreature, definition sim.AbilityDefinition, targetObjectID uint32,
	activeAbilityID uint32, isBasicHeldRepeat bool, isBasicHeldInput bool, sessionKey string,
	abilityStartTime time.Time,
) ([][]byte, error) {
	var err error
	packet := request.packet
	command := request.command
	abilityRank := int32(1)
	if command.Ability.Rank > 0 {
		abilityRank = command.Ability.Rank
	}
	if definition.Name == "MissileTempestSupport" && abilityRank%2 == 0 {
		definition.StatusDuration = 4 * time.Second
	}
	if definition.Name == poisonNovaName && abilityRank >= 7 {
		definition.StatusDuration = 5 * time.Second
		definition.TickDuration = time.Second
	}
	isActiveRequest := activeAbilityID != 0
	center := command.Ability.CursorPosition
	admissionRange := heroAbilityAdmissionRange(creature, definition)
	isPursuitRetry := peerSession.campaignPlayerPursuitSession().IsRetry(
		command.Common.ObjectID, targetObjectID, command.Ability.Index,
		command.Common.Unknown[0],
	)
	sourcePosition := game.Vec3{
		X: peerSession.playerPosition.X, Y: peerSession.playerPosition.Y,
		Z: peerSession.playerPosition.Z,
	}
	enemies := r.registry.abilityTargetsLocked(r, peerSession, definition, packet.SourceTime)
	var areaPlan zoneability.AreaPlan
	var planErr error
	if definition.Name == "TechRandom" {
		areaPlan, planErr = zoneability.PlanZetawattBeam(
			enemies, command.Common.ObjectID, targetObjectID, sourcePosition,
			game.Vec3(center), creature, definition,
		)
	} else if definition.Kind == sim.AbilityKindCone {
		if !isReportedZonePosition(center) {
			center = command.Ability.TargetPosition
		}
		if !isReportedZonePosition(center) {
			r.registry.mutex.Unlock()
			return request.reject("cone direction unavailable")
		}
		areaPlan, planErr = zoneability.PlanCone(
			enemies, command.Common.ObjectID, sourcePosition,
			game.Vec3{X: center.X, Y: center.Y, Z: center.Z}, creature, definition,
		)
	} else if definition.Kind == sim.AbilityKindCursorArea ||
		definition.Kind == sim.AbilityKindTeleportArea {
		if definition.Name == "FireTempestBasic" && targetObjectID != 0 {
			target, isTargetFound := enemies.NPC(targetObjectID)
			if isTargetFound && !target.IsDefeated && target.HitPoint > 0 {
				center = raknet.Vector3(target.Plan.Position)
				// Native 103 sub_9DE710 admits object targets using both
				// actor footprints, including cursor-area basic attacks.
				admissionRange += peerSession.deployedCampaignActorFootprintRadius() +
					target.Plan.ActorFootprintRadius()
				if isPursuitRetry {
					admissionRange += campaignPlayerPursuitRedirectDistance
				}
			}
		}
		if !isReportedZonePosition(center) {
			center = command.Ability.TargetPosition
		}
		if !isReportedZonePosition(center) {
			r.registry.mutex.Unlock()
			return request.reject("cursor position unavailable")
		}
		if definition.Kind == sim.AbilityKindTeleportArea {
			peerSession.enemyRootExpiresAt = time.Time{}
			peerSession.enemyRootTargetObjectID = 0
			var projected game.Vec3
			var isProjected bool
			var projectionErr error
			if peerSession.binding.Mode == game.ModeArena {
				match := r.registry.arenaMatches[peerSession.binding.GameID]
				if match == nil || match.navigation == nil || !match.isCombatActive {
					r.registry.mutex.Unlock()
					return request.reject("Arena teleport destination unavailable")
				}
				projected, isProjected, projectionErr = zonenavigation.ReachableTeleportDestination(
					match.navigation, sourcePosition,
					game.Vec3{X: center.X, Y: center.Y, Z: center.Z},
					peerSession.deployedCampaignNavigationRadius(),
				)
			} else {
				projected, isProjected, projectionErr = zonenavigation.ReachableTeleportDestination(
					peerSession.zone.Navigation(), sourcePosition,
					game.Vec3{X: center.X, Y: center.Y, Z: center.Z},
					peerSession.deployedCampaignNavigationRadius(),
				)
			}
			if projectionErr != nil {
				r.registry.mutex.Unlock()
				return nil, fmt.Errorf("teleportAreaNavigation: %w", projectionErr)
			}
			if !isProjected {
				r.registry.mutex.Unlock()
				return request.reject("teleport destination unreachable")
			}
			center = raknet.Vector3{X: projected.X, Y: projected.Y, Z: projected.Z}
		}
		areaPlan, planErr = zoneability.PlanCursorAreaWithAdmission(
			enemies, command.Common.ObjectID, targetObjectID,
			sourcePosition, game.Vec3{X: center.X, Y: center.Y, Z: center.Z},
			creature, definition, admissionRange,
		)
	} else {
		center = raknet.Vector3{X: sourcePosition.X, Y: sourcePosition.Y, Z: sourcePosition.Z}
		if isActiveRequest {
			areaPlan, planErr = zoneability.PlanArea(
				enemies, command.Common.ObjectID,
				sourcePosition, creature, definition,
			)
		} else {
			areaPlan, planErr = zoneability.PlanPointBlankBasic(
				enemies, command.Common.ObjectID,
				sourcePosition, creature, definition,
			)
		}
	}
	if planErr != nil {
		var rangeErr zoneability.TargetRangeError
		if definition.Name == "FireTempestBasic" && definition.IsShouldPursue &&
			targetObjectID != 0 && errors.As(planErr, &rangeErr) {
			target, isTargetFound := enemies.NPC(targetObjectID)
			if isTargetFound && !target.IsDefeated && target.HitPoint > 0 {
				request.startTime = abilityStartTime
				packets, isPursuitStarted, pursuitErr := r.transferPlayerAbilityPursuit(
					request, peerSession, target,
					heroAbilityAdmissionRange(creature, definition), isPursuitRetry,
				)
				if pursuitErr != nil {
					return nil, fmt.Errorf("areaPursuit: %w", pursuitErr)
				}
				if isPursuitStarted {
					r.logger.Printf("RakNet campaign cursor area pursuit started source=%d target=%d ability=%s", command.Common.ObjectID, targetObjectID, definition.Name)
				}
				return packets, nil
			}
		}
		r.registry.mutex.Unlock()
		return request.reject(planErr.Error())
	}
	if isPursuitRetry {
		peerSession.campaignPlayerPursuitSession().Cancel()
	}
	previousBasicSequence := peerSession.basicSequenceSession().Snapshot()
	previousAreaGeneration := peerSession.areaBasicGeneration
	previousPlayerMotion := peerSession.playerMotionSnapshot()
	previousManaPoint := peerSession.deployedManaPoint()
	remainingManaPoint := previousManaPoint
	if isActiveRequest {
		manaCost, manaErr := game.ResolveAbilityManaCost(
			areaPlan.Definition.ManaCost, creature.DamageProfile.PrimaryAttribute,
			areaPlan.Definition.ManaCoefficient,
			peerSession.isOverdriveActiveAt(abilityStartTime),
		)
		if manaErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignAreaManaProjection: %w", manaErr)
		}
		if remainingManaPoint < manaCost {
			r.registry.mutex.Unlock()
			return request.reject("power unavailable")
		}
		remainingManaPoint -= manaCost
	}
	poisonNovaCue, cueErr := peerSession.preparePoisonNovaCooldownCue(definition.Name, abilityRank, abilityStartTime)
	if cueErr != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("areaCooldownCue: %w", cueErr)
	}
	if !isActiveRequest && !isBasicHeldRepeat {
		peerSession.basicSequenceSession().SetHeld(isBasicHeldInput)
	}
	heldGeneration := peerSession.basicSequenceSession().HeldGeneration()
	selection, selectionErr := zoneability.SelectActivation(areaPlan.Definition)
	if !isActiveRequest {
		selection, selectionErr = peerSession.basicSequenceSession().Accept(
			abilityStartTime, areaPlan.Definition,
		)
	}
	if selectionErr != nil {
		if !isActiveRequest {
			peerSession.basicSequenceSession().Restore(
				previousBasicSequence,
				peerSession.basicSequenceSession().Revision(),
			)
		}
		r.registry.mutex.Unlock()
		return request.reject(selectionErr.Error())
	}
	basicSequenceRevision := peerSession.basicSequenceSession().Revision()
	areaPlan.Definition.AnimationName = selection.AnimationName
	areaPlan.Definition.HitDelay = selection.HitDelay
	areaPlan.Definition.ReleaseDelay = selection.ReleaseDelay
	var posePackets [][]byte
	if definition.Name == "FireTempestBasic" {
		facing := geometryraknet.Direction(peerSession.playerPosition, center)
		posePackets, err = marshalZonePlayerAttackPose(command.Common.ObjectID,
			peerSession.playerPosition, facing, center)
		if err != nil {
			peerSession.basicSequenceSession().Restore(previousBasicSequence, basicSequenceRevision)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("areaBasicFacing: %w", err)
		}
		peerSession.retainAttackPose(facing, center, targetObjectID,
			abilityStartTime.Add(selection.ReleaseDelay))
	}
	if definition.Kind != sim.AbilityKindTeleportArea {
		err = peerSession.stopPlayerMovement(abilityStartTime)
	}
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignAreaBasicStop: %w", err)
	}
	playerMotionRevision := peerSession.playerMotionRevision()
	muzzleEffectName := areaPlan.Definition.MuzzleEffectName
	if definition.Name == "TCShieldedSentinelSupport" || definition.Name == "MissileTempestSupport" || definition.Name == "SpacetimeRandom3" {
		// Its Lua emits the object-only muzzle after waiting for the hit.
		muzzleEffectName = ""
	}
	start, marshalErr := abilityraknet.StartAreaBasic(abilityraknet.AreaBasicStartRequest{
		SyncStamp: command.Common.Unknown[0], SourceID: command.Common.ObjectID,
		AbilityID: areaPlan.AbilityID, AbilityIndex: command.Ability.Index,
		SourceTime: packet.SourceTime, AnimationName: selection.AnimationName,
		MuzzleEffectName: muzzleEffectName,
		HitDelay:         selection.HitDelay, ReleaseDelay: selection.ReleaseDelay,
		Cooldown: areaPlan.Definition.Cooldown,
	})
	if marshalErr != nil {
		if !isActiveRequest {
			peerSession.basicSequenceSession().Restore(previousBasicSequence, basicSequenceRevision)
		}
		peerSession.restorePlayerMotion(previousPlayerMotion, playerMotionRevision)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignAreaBasicStart: %w", marshalErr)
	}
	cooldownReservation := zoneability.CooldownReservation{}
	var manaPacket []byte
	if isActiveRequest {
		isCooldownReserved := false
		cooldownReservation, isCooldownReserved =
			peerSession.abilityCooldownSession().Reserve(
				zoneability.HeroAbilityCooldown(activeAbilityID),
				abilityStartTime, areaPlan.Definition.Cooldown,
			)
		if !isCooldownReserved {
			peerSession.restorePlayerMotion(previousPlayerMotion, playerMotionRevision)
			r.registry.mutex.Unlock()
			return request.reject("cooldown unavailable")
		}
		manaPacket, marshalErr = abilityraknet.Mana(
			command.Common.ObjectID, remainingManaPoint,
		)
		if marshalErr == nil {
			marshalErr = peerSession.setDeployedManaPoints(remainingManaPoint)
		}
		if marshalErr != nil {
			peerSession.abilityCooldownSession().Rollback(cooldownReservation)
			peerSession.restorePlayerMotion(previousPlayerMotion, playerMotionRevision)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignAreaManaCommit: %w", marshalErr)
		}
	}
	peerSession.areaBasicGeneration++
	areaGeneration := peerSession.areaBasicGeneration
	generation := peerSession.generation
	binding := peerSession.binding
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	livePlanCenter := areaPlan.Center
	if definition.Kind == sim.AbilityKindCone || definition.Name == "TechRandom" {
		livePlanCenter = game.Vec3{X: center.X, Y: center.Y, Z: center.Z}
	}
	hitStep := campaignAreaHitStep{
		runtime: r, packet: packet, command: command, sessionKey: sessionKey,
		generation: generation, areaGeneration: areaGeneration,
		targetObjectID: targetObjectID,
		hitTimestamp:   packet.SourceTime + uint64(selection.HitDelay/time.Millisecond),
		admissionRange: admissionRange,
		sourcePosition: sourcePosition, center: livePlanCenter,
		creature: creature, definition: definition, binding: binding,
		activeAbilityID: activeAbilityID, abilityRank: abilityRank,
		cooldownReservation: cooldownReservation,
	}
	hitDelay := selection.HitDelay
	var cursorEffectStep *campaignCursorAreaEffectStep
	if definition.Name == "SpacetimeRandom3" {
		hitDelay += celestialCometDamageDelay
		hitStep.hitTimestamp = packet.SourceTime + uint64(hitDelay/time.Millisecond)
		hitStep.isEffectPresented = true
		cursorEffectStep = &campaignCursorAreaEffectStep{
			registry: r.registry, sessionKey: sessionKey,
			generation: generation, areaGeneration: areaGeneration,
			assetName:  areaPlan.Definition.HitEffectName,
			muzzleName: areaPlan.Definition.MuzzleEffectName, objectID: command.Common.ObjectID,
			position: center,
		}
	} else if definition.Name == "PlasmaRandom" {
		hitStep.isEffectPresented = true
		cursorEffectStep = &campaignCursorAreaEffectStep{
			registry: r.registry, sessionKey: sessionKey,
			generation: generation, areaGeneration: areaGeneration,
			assetName: areaPlan.Definition.HitEffectName,
			position:  center,
		}
	}
	repeatDelay := max(areaPlan.Definition.Cooldown, areaPlan.Definition.ReleaseDelay)
	if !isActiveRequest {
		repeatDelay = peerSession.basicSequenceSession().CooldownEnd().Sub(abilityStartTime)
	}
	repeatPacket := packet
	repeatPacket.Payload = append([]byte(nil), packet.Payload...)
	repeatPacket.SourceTime += uint64(repeatDelay / time.Millisecond)
	repeatStep := campaignAreaRepeatStep{
		runtime: r, packet: repeatPacket, command: command, sessionKey: sessionKey,
		generation: generation, areaGeneration: areaGeneration,
		heldGeneration: heldGeneration,
	}
	releaseStep := campaignAreaReleaseStep{
		registry: r.registry, sessionKey: sessionKey, generation: generation,
		areaGeneration: areaGeneration, packet: start.Release,
	}
	producers := []raknet.ScheduledPacketProducer{
		{Delay: hitDelay, Produce: hitStep.produce},
		{Delay: selection.ReleaseDelay, Produce: releaseStep.produce},
	}
	if cursorEffectStep != nil {
		effectDelay := selection.HitDelay
		if definition.Name == "PlasmaRandom" {
			effectDelay = 100 * time.Millisecond
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: effectDelay, Produce: cursorEffectStep.produce,
		})
	}
	if definition.Kind == sim.AbilityKindTeleportArea &&
		definition.TeleportDelay > 0 {
		teleportStep := campaignTeleportAreaStep{
			runtime: r, sessionKey: sessionKey, generation: generation,
			areaGeneration: areaGeneration,
			sourceObjectID: command.Common.ObjectID,
			center:         areaPlan.Center, effectName: definition.ImpactEffectName,
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: definition.TeleportDelay, Produce: teleportStep.produce,
		})
	}
	if definition.Name == "MissileTempestSupport" &&
		(abilityRank == 5 || abilityRank == 6) {
		for _, additionalDelay := range []time.Duration{
			selection.ReleaseDelay + 400*time.Millisecond,
			selection.ReleaseDelay + 1100*time.Millisecond,
		} {
			additionalHit := hitStep
			additionalHit.hitTimestamp = packet.SourceTime +
				uint64(additionalDelay/time.Millisecond)
			additionalHit.isRetained = true
			producers = append(producers, raknet.ScheduledPacketProducer{
				Delay: additionalDelay, Produce: additionalHit.produce,
			})
		}
	}
	if isBasicHeldInput {
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: repeatDelay, Produce: repeatStep.produce,
		})
	}
	sortScheduledPacketProducersByDelay(producers)
	producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
	failure := campaignAreaScheduleFailure{
		registry: r.registry, logger: r.logger, sessionKey: sessionKey,
		generation: generation, areaGeneration: areaGeneration,
	}
	if packet.ScheduleGroupResult != nil {
		_, err = packet.ScheduleGroupResult(producers, failure.handle)
	} else {
		_, err = packet.ScheduleGroup(producers)
	}
	if err != nil {
		r.registry.mutex.Lock()
		currentSession, isCurrentFound := r.registry.sessions[sessionKey]
		isCurrent := isCurrentFound && currentSession.generation == generation &&
			currentSession.areaBasicGeneration == areaGeneration
		if isCurrent {
			if !isActiveRequest {
				currentSession.basicSequenceSession().Restore(
					previousBasicSequence, basicSequenceRevision,
				)
			}
			currentSession.areaBasicGeneration = previousAreaGeneration
			currentSession.restorePlayerMotion(previousPlayerMotion, playerMotionRevision)
			if isActiveRequest {
				_ = currentSession.setCampaignCharacterManaPoints(
					currentSession.deployedCreatureIndex, previousManaPoint,
				)
				currentSession.abilityCooldownSession().Rollback(cooldownReservation)
			}
			r.registry.sessions[sessionKey] = currentSession
		}
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignAreaBasicSchedule: %w", err)
	}
	r.logger.Printf("RakNet campaign area basic accepted source=%d targets=%d ability=%q",
		command.Common.ObjectID, len(areaPlan.Target), areaPlan.Definition.Name)
	startPackets := make([][]byte, 0, len(start.Presentation)+1)
	if len(poisonNovaCue) != 0 {
		startPackets = append(startPackets, poisonNovaCue)
	}
	startPackets = append(startPackets, start.Presentation...)
	if definition.Kind == sim.AbilityKindTeleportArea &&
		definition.ActivationEffectName != "" {
		exitPacket, exitErr := abilityraknet.CursorAreaImpact(
			definition.ActivationEffectName, center,
		)
		if exitErr != nil {
			return nil, fmt.Errorf("teleportAreaPreview: %w", exitErr)
		}
		startPackets = append(startPackets, exitPacket)
	}
	if len(manaPacket) != 0 {
		startPackets = append(startPackets, manaPacket)
	}
	responsePackets := append([][]byte{start.Acknowledge}, posePackets...)
	return append(responsePackets, startPackets...), nil
}

type campaignBasicScheduleRun struct {
	runtime        campaignAbilityCommandRuntime
	sessionKey     string
	generation     uint64
	attack         *abilityraknet.MeleeRun
	selection      zoneability.Selection
	releaseAck     []byte
	heldGeneration uint64
	repeatPacket   raknet.Packet
	command        raknet.ActionCommandData
}

type campaignMeleeSchedule struct {
	squad             *squad.Session
	hitState          *campaignMeleeHitState
	runtime           campaignAbilityCommandRuntime
	packet            raknet.Packet
	sessionKey        string
	generation        uint64
	sourceObjectID    uint32
	targetObjectID    uint32
	creature          game.GameplayCreature
	definition        sim.AbilityDefinition
	selection         zoneability.Selection
	selected          sim.AbilityDefinition
	plan              zoneability.BasicPlan
	binding           game.GameplayBinding
	run               *abilityraknet.MeleeRun
	releaseResponse   []byte
	isFireRavagerStun bool
	isShadowCloakCrit bool
}

type campaignMeleeHitStep struct {
	schedule campaignMeleeSchedule
	index    int
	delay    time.Duration
}

// Scheduled value copies share one activation's retained selection state.
// Access is serialized by the gameplay registry mutex.
type campaignMeleeHitState struct {
	isTargetInRangeAtStart bool
	initialTargetPosition  game.Vec3
	arc                    *zoneability.MeleeArc
}

type campaignMeleeImpact struct {
	assetName      string
	sourceObjectID uint32
	sourcePosition raknet.Vector3
}

func (e campaignMeleeImpact) marshal(
	result zoneability.AreaResult,
) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.ObjectEffectMessage{
		IsCritical: result.IsCritical,
		Asset:      util.HashID(e.assetName),
		ObjectID:   result.Damage.ObjectID,
		AttackerID: e.sourceObjectID,
		Facing: geometryraknet.Direction(
			e.sourcePosition, raknet.Vector3{
				X: result.Snapshot.Plan.Position.X,
				Y: result.Snapshot.Plan.Position.Y,
				Z: result.Snapshot.Plan.Position.Z,
			},
		),
	})
	if err != nil {
		return nil, fmt.Errorf("meleeImpactMarshal: %w", err)
	}
	return packet, nil
}

type campaignExpungeStep struct {
	schedule campaignMeleeSchedule
}

func (e campaignExpungeStep) produce() ([][]byte, error) {
	return e.schedule.expunge()
}

func (e campaignMeleeSchedule) producer(
	index int, delay time.Duration,
) raknet.ScheduledPacketProducer {
	step := campaignMeleeHitStep{schedule: e, index: index, delay: delay}
	return raknet.ScheduledPacketProducer{Delay: delay, Produce: step.produce}
}

func (e campaignMeleeHitStep) produce() ([][]byte, error) {
	schedule := e.schedule
	runtime := schedule.runtime
	runtime.registry.mutex.Lock()
	current, isFound := runtime.registry.sessions[schedule.sessionKey]
	isCurrent := isFound && current.generation == schedule.generation &&
		current.basicAttack == schedule.run
	if current.binding.Mode == game.ModeArena {
		isCurrent = isCurrent && current.isArenaCombatActive
	}
	if !isCurrent {
		runtime.registry.mutex.Unlock()
		return nil, nil
	}
	enemies := runtime.registry.abilityTargetsLocked(runtime, current, schedule.selected, schedule.packet.SourceTime+uint64(e.delay/time.Millisecond))
	random, randomErr := runtime.registry.abilityRandomLocked(current)
	if randomErr != nil {
		runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("meleeHitRandom: %w", randomErr)
	}
	currentCreature := current.binding.Creatures[current.deployedCreatureIndex]
	// Shadow Cloak ends on UsedAbility, after this attack is accepted. Its
	// attribute snapshot still grants the triggering attack its guaranteed crit.
	if schedule.isShadowCloakCrit {
		currentCreature.AutoCrit = max(float32(1), currentCreature.AutoCrit)
	}
	livePlan := schedule.plan
	livePlan.SourcePosition = game.Vec3{
		X: current.playerPosition.X, Y: current.playerPosition.Y,
		Z: current.playerPosition.Z,
	}
	var err error
	livePlan.Definition.AnimationName = schedule.selection.AnimationName
	livePlan.Definition.HitDelay = e.delay
	livePlan.Definition.ReleaseDelay = schedule.selection.ReleaseDelay
	livePlan.Definition.HitEffectName = schedule.selected.HitEffectName
	state := schedule.hitState
	if state.arc == nil {
		length := schedule.selected.Range
		if schedule.selected.HitArcLength != nil {
			length = *schedule.selected.HitArcLength
		}
		length += current.deployedCampaignActorFootprintRadius()
		facing := current.attackPose.facing
		arc, arcErr := zoneability.NewMeleeArc(livePlan.SourcePosition,
			game.Vec3{X: facing.X, Y: facing.Y, Z: facing.Z}, length, schedule.selected.HitAngle)
		if arcErr != nil {
			runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignMeleeArc: %w", arcErr)
		}
		state.arc = &arc
	}
	liveNPC, isLiveNPCFound := enemies.NPC(schedule.targetObjectID)
	isSelected := isLiveNPCFound && zoneability.IsHostileMeleeTarget(liveNPC)
	if isSelected {
		movement := zonegeometry.Distance(state.initialTargetPosition, liveNPC.Plan.Position)
		isSelected = (state.isTargetInRangeAtStart && movement < schedule.selected.MissMovementAmount) ||
			state.arc.Contains(liveNPC.Plan.Position, liveNPC.Plan.ActorFootprintRadius())
	}
	if !isSelected {
		facing := current.attackPose.facing
		liveNPC, isSelected = state.arc.BestTarget(enemies, livePlan.SourcePosition,
			game.Vec3{X: facing.X, Y: facing.Y, Z: facing.Z})
	}
	// Revalidate the original primary before grace/arc admission, then try the
	// authored arc fallback before finishing an empty swing.
	if !isSelected {
		position := toSimPosition(current.playerPosition)
		err = schedule.run.PrepareHitAt(
			e.index, false, 0, schedule.plan.Damage.Maximum, false,
			position, position,
		)
		if err != nil {
			runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignBasicMissPrepare: %w", err)
		}
		packets, advanceErr := schedule.run.Advance(context.Background(), e.delay)
		runtime.registry.sessions[schedule.sessionKey] = current
		runtime.registry.mutex.Unlock()
		if advanceErr != nil {
			return nil, fmt.Errorf("campaignBasicMissAdvance: %w", advanceErr)
		}
		return packets, nil
	}
	livePlan.TargetObjectID = liveNPC.Plan.ObjectID
	if current.binding.Mode == game.ModeArena {
		runtime.registry.sessions[schedule.sessionKey] = current
	}
	result, err := zoneability.CommitBasic(
		random, enemies,
		livePlan, currentCreature, current.binding.Difficulty,
		runtime.program.Critical,
	)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignBasicCommit: %w", err)
	}
	areaResults := []zoneability.AreaResult{{
		SourceRank: schedule.plan.SourceRank, SourceCreature: schedule.creature,
		Snapshot: liveNPC, Damage: result.Damage, IsCritical: result.IsCritical,
		Definition: livePlan.Definition,
	}}
	if arenaTargets, isArena := enemies.(*arenaAbilityTargets); isArena {
		packets, publishErr := e.commitArenaMeleeLocked(current, arenaTargets, livePlan, liveNPC, result, areaResults)
		runtime.registry.mutex.Unlock()
		arenaTargets.finish()
		if publishErr != nil {
			return nil, fmt.Errorf("meleeArenaPublish: %w", publishErr)
		}
		if schedule.selected.ChainLightning.MaximumHopCount > 0 {
			chainPackets, chainErr := schedule.startChainLightning(livePlan, liveNPC.Plan.Position, e.delay)
			packets = append(packets, chainPackets...)
			if chainErr != nil && runtime.logger != nil {
				runtime.logger.Printf("Arena chain continuation omitted source=%d: %v", schedule.sourceObjectID, chainErr)
			}
		}
		return packets, nil
	}
	plasmaPlan := zoneability.PlasmaModifierPlan{}
	isPlasmaApplied := false
	if !result.Damage.IsDefeated && !result.Damage.IsDamageImmune &&
		!result.Damage.IsShieldStarted && !result.Damage.IsTurtleStarted &&
		len(schedule.selected.MeleeHitPolicies) != 0 {
		plasmaPlan, isPlasmaApplied, err = zoneability.PlanPlasmaModifier(
			random,
			schedule.selected.MeleeHitPolicies,
			schedule.selection.AnimationIndex, currentCreature,
		)
		if err != nil {
			runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignBasicPlasmaModifier: %w", err)
		}
	}
	transition, err := current.applyCampaignDamageTransition(result.Damage)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignBasicTransition: %w", err)
	}
	transitions := []campaignDamageTransition{transition}
	additionalTargetCount := uint32(0)
	if schedule.selection.AnimationIndex <
		len(schedule.selected.AnimationAdditionalTargetCounts) {
		additionalTargetCount = schedule.selected.AnimationAdditionalTargetCounts[schedule.selection.AnimationIndex]
	}
	if additionalTargetCount != 0 {
		additionalTargets, targetErr := zoneability.AdditionalMeleeTargets(
			enemies, livePlan.TargetObjectID,
			livePlan.SourcePosition, state.arc.Facing,
			schedule.selected.AdditionalTargetRange,
			schedule.selected.AdditionalTargetAngle, additionalTargetCount,
		)
		if targetErr != nil {
			runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignBasicAdditionalTargets: %w", targetErr)
		}
		for _, additionalTarget := range additionalTargets {
			additionalPlan := livePlan
			additionalPlan.TargetObjectID = additionalTarget.Plan.ObjectID
			additionalResult, commitErr := zoneability.CommitBasic(
				random, enemies,
				additionalPlan, currentCreature, current.binding.Difficulty,
				runtime.program.Critical,
			)
			if commitErr != nil {
				runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("campaignBasicAdditionalCommit: %w", commitErr)
			}
			additionalTransition, transitionErr :=
				current.applyCampaignDamageTransition(additionalResult.Damage)
			if transitionErr != nil {
				runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("campaignBasicAdditionalTransition: %w", transitionErr)
			}
			areaResults = append(areaResults, zoneability.AreaResult{
				SourceRank: schedule.plan.SourceRank, SourceCreature: schedule.creature,
				Snapshot: additionalTarget, Damage: additionalResult.Damage,
				IsCritical: additionalResult.IsCritical, Definition: livePlan.Definition,
			})
			transitions = append(transitions, additionalTransition)
		}
	}
	physics := runtime.program.NPCDeathPhysics(liveNPC.Plan.NounName)
	deathDefinition, err := campaignNPCDeathDefinition(liveNPC, physics)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignBasicDeathDefinition: %w", err)
	}
	facing := geometryraknet.Direction(
		current.playerPosition, deathDefinition.position,
	)
	preparedDamage := result.Damage.Damage
	if result.Damage.IsDamageImmune {
		preparedDamage = schedule.plan.Damage.Maximum
	}
	err = schedule.run.PrepareTargetHitAt(
		e.index, livePlan.TargetObjectID, !result.Damage.IsDamageImmune,
		result.Damage.PreviousHealth, preparedDamage, result.IsCritical,
		toSimPosition(deathDefinition.position),
		sim.Position{X: facing.X, Y: facing.Y, Z: facing.Z},
	)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignBasicPrepare: %w", err)
	}
	// Movement can stop the run after releasing the registry lock. Encode this
	// committed hit first so cancellation cannot discard its damage/death packets.
	packets, err := schedule.run.Advance(context.Background(), e.delay)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignBasicHit: %w", err)
	}
	packets = filterProjectileCombatPackets(packets)
	runtime.registry.sessions[schedule.sessionKey] = current
	runtime.registry.mutex.Unlock()

	timestamp := schedule.packet.SourceTime + uint64(e.delay/time.Millisecond)
	var effect func(zoneability.AreaResult) ([]byte, error)
	if schedule.selected.HitEffectName != "" && schedule.selected.ChainLightning.MaximumHopCount == 0 {
		impact := campaignMeleeImpact{
			assetName:      schedule.selected.HitEffectName,
			sourceObjectID: schedule.sourceObjectID,
			sourcePosition: current.playerPosition,
		}
		effect = impact.marshal
	}
	publishedPackets, err := runtime.damage.publishAreaResults(
		schedule.packet, schedule.sessionKey, schedule.generation,
		schedule.sourceObjectID, timestamp, schedule.binding,
		areaResults, transitions, effect, true,
	)
	if err != nil {
		return nil, fmt.Errorf("campaignBasicPublish: %w", err)
	}
	if schedule.selected.ChainLightning.MaximumHopCount > 0 {
		chainPackets, chainErr := schedule.startChainLightning(livePlan, liveNPC.Plan.Position, e.delay)
		if chainErr != nil {
			runtime.logger.Printf("RakNet Chain Lightning continuation omitted source=%d: %v", schedule.sourceObjectID, chainErr)
		}
		publishedPackets = append(publishedPackets, chainPackets...)
	}
	tauntPackets, tauntErr := runtime.damage.applyAcceptedHitTaunts(
		schedule.packet, schedule.sessionKey, schedule.generation,
		schedule.sourceObjectID, timestamp, schedule.plan.Definition,
		areaResults,
	)
	if tauntErr != nil {
		return nil, fmt.Errorf("campaignBasicTaunt: %w", tauntErr)
	}
	publishedPackets = append(publishedPackets, tauntPackets...)
	if schedule.selected.Name == energySentinelBasicName {
		for _, areaResult := range areaResults {
			if areaResult.Damage.IsDefeated {
				continue
			}
			modifierPackets, modifierErr := runtime.damage.applyHeroHealingReduction(
				schedule.packet, schedule.sessionKey, schedule.generation,
				schedule.sourceObjectID, timestamp, schedule.selected,
				areaResult.Damage.ObjectID,
			)
			if modifierErr != nil {
				return nil, fmt.Errorf("campaignBasicHealingReduction: %w", modifierErr)
			}
			publishedPackets = append(publishedPackets, modifierPackets...)
		}
	}
	timeRavagerPackets, timeRavagerErr := runtime.damage.applyTimeRavagerSlow(
		schedule.packet, schedule.sessionKey, schedule.generation,
		schedule.sourceObjectID, timestamp,
		zoneability.AreaResult{
			Snapshot: liveNPC, Damage: result.Damage, IsCritical: result.IsCritical,
			Definition: schedule.plan.Definition,
		},
	)
	if timeRavagerErr != nil {
		return nil, fmt.Errorf("campaignBasicTimeRavager: %w", timeRavagerErr)
	}
	publishedPackets = append(publishedPackets, timeRavagerPackets...)
	if schedule.selected.Name == "ShadowRavagerActive" {
		statusPackets, statusErr := runtime.applyAcceptedHitStatus(
			schedule.packet, schedule.sessionKey, schedule.generation,
			schedule.sourceObjectID, timestamp,
			zoneability.AreaPlan{
				SourceObjectID: schedule.sourceObjectID,
				AbilityID:      schedule.plan.AbilityID,
				Definition:     schedule.selected,
			},
			[]zoneability.AreaResult{{
				Snapshot: liveNPC, Damage: result.Damage,
				IsCritical: result.IsCritical, Definition: schedule.selected,
			}},
		)
		if statusErr != nil {
			return nil, fmt.Errorf("campaignBasicShadowSting: %w", statusErr)
		}
		publishedPackets = append(publishedPackets, statusPackets...)
	}
	if isPlasmaApplied {
		modifierPackets, modifierErr := runtime.damage.publishPlasmaModifier(
			schedule.packet, schedule.sessionKey, schedule.generation,
			schedule.sourceObjectID, livePlan.TargetObjectID,
			timestamp, schedule.binding, plasmaPlan,
			currentCreature,
		)
		if modifierErr != nil {
			return nil, fmt.Errorf("campaignBasicPlasmaPublish: %w", modifierErr)
		}
		publishedPackets = append(publishedPackets, modifierPackets...)
	}
	if schedule.isFireRavagerStun {
		stunPackets, stunErr := runtime.damage.publishNPCStuns(
			schedule.packet, schedule.sessionKey, schedule.generation,
			schedule.sourceObjectID, timestamp,
			[]zoneability.AreaResult{{
				Snapshot: liveNPC, Damage: result.Damage,
				IsCritical: result.IsCritical, Definition: schedule.plan.Definition,
			}},
			util.HashID("FireRavagerStunModifier"), time.Second,
		)
		if stunErr != nil {
			return nil, fmt.Errorf("campaignBasicFireRavagerStun: %w", stunErr)
		}
		publishedPackets = append(publishedPackets, stunPackets...)
	}
	packets = append(packets, publishedPackets...)
	// A lethal hit does not skip later authored hit frames. They can select a
	// fallback victim within the same captured arc; release has its own step.
	return packets, nil
}

type campaignHeldRepeatStep struct {
	runtime        campaignAbilityCommandRuntime
	sessionKey     string
	generation     uint64
	heldGeneration uint64
	packet         raknet.Packet
	command        raknet.ActionCommandData
}

type campaignBasicSequenceRollback struct {
	sequence *zoneability.Sequence
	snapshot zoneability.SequenceSnapshot
	revision uint64
	isArmed  bool
}

func (e *campaignBasicSequenceRollback) restore() {
	if e == nil || !e.isArmed || e.sequence == nil {
		return
	}
	e.sequence.Restore(e.snapshot, e.revision)
}

type campaignTossSchedule struct {
	runtime                    campaignAbilityCommandRuntime
	packet                     raknet.Packet
	sessionKey                 string
	generation                 uint64
	sourceObjectID             uint32
	projectileObjectID         uint32
	previousProjectileObjectID uint32
	startTime                  time.Time
	creature                   game.GameplayCreature
	plan                       zoneability.TossPlan
	binding                    game.GameplayBinding
	run                        *campaignTossRun
	previousSequence           zoneability.SequenceSnapshot
	sequenceRevision           uint64
	previousMotion             zoneaction.MotionSnapshot
	motionRevision             uint64
	releaseReservation         zoneaction.ReleaseReservation
	releaseResponse            []byte
}

type campaignTossLandingStep struct {
	schedule campaignTossSchedule
	elapsed  time.Duration
	deadline time.Duration
}

type campaignProjectileScheduleFailure struct {
	runtime                    campaignAbilityCommandRuntime
	sessionKey                 string
	generation                 uint64
	projectileObjectIDs        []uint32
	previousProjectileObjectID uint32
	creatureIndex              uint32
	previousManaPoint          float32
	runs                       []*abilityraknet.ProjectileRun
	retirements                []*campaignProjectileRetirement
	previousSequence           zoneability.SequenceSnapshot
	sequenceRevision           uint64
	cooldownReservation        zoneability.CooldownReservation
	releaseReservation         zoneaction.ReleaseReservation
}

type campaignProjectileReleaseStep struct {
	releaseReservation zoneaction.ReleaseReservation
	registry           *gameplaySessionRegistry
	sessionKey         string
	generation         uint64
	sourceObjectID     uint32
	timestamp          uint64
	packet             []byte
}

type campaignElectronSecondarySchedule struct {
	runtime            campaignAbilityCommandRuntime
	packet             raknet.Packet
	sessionKey         string
	generation         uint64
	sourceObjectID     uint32
	projectileObjectID uint32
	impactDeadline     time.Duration
	travelDistance     float32
	startPosition      raknet.Vector3
	facing             raknet.Vector3
	creature           game.GameplayCreature
	definition         sim.AbilityDefinition
	binding            game.GameplayBinding
	run                *abilityraknet.ProjectileRun
}

func stopProjectileRuns(runs []*abilityraknet.ProjectileRun) {
	for _, run := range runs {
		run.Stop()
	}
}

func campaignHeroProjectileSource(
	source raknet.Vector3, facing raknet.Vector3, offset sim.Position,
) raknet.Vector3 {
	source.X += -facing.Y*offset.X + facing.X*offset.Y
	source.Y += facing.X*offset.X + facing.Y*offset.Y
	source.Z += offset.Z
	return source
}

func campaignProjectileSpreadTarget(
	aimSource, source, target raknet.Vector3, angleDegrees float32,
) (raknet.Vector3, raknet.Vector3) {
	// Client projectile template chunk38, prototype0.16, computes direction
	// before adding the transformed shot offset. Muzzle placement must not
	// change pitch or make parallel launches converge on the same point.
	facing := geometryraknet.Direction(aimSource, target)
	radians := float64(angleDegrees) * math.Pi / 180
	cosine := float32(math.Cos(radians))
	sine := float32(math.Sin(radians))
	spreadFacing := raknet.Vector3{
		X: facing.X*cosine - facing.Y*sine,
		Y: facing.X*sine + facing.Y*cosine,
		Z: facing.Z,
	}
	distance := zoneability.Distance(game.Vec3(aimSource), game.Vec3(target))
	return raknet.Vector3{
		X: source.X + spreadFacing.X*distance,
		Y: source.Y + spreadFacing.Y*distance,
		Z: source.Z + spreadFacing.Z*distance,
	}, spreadFacing
}

type campaignElectronSecondaryStep struct {
	schedule campaignElectronSecondarySchedule
	deadline time.Duration
}

func (e campaignElectronSecondarySchedule) producer(
	deadline time.Duration,
) raknet.ScheduledPacketProducer {
	step := campaignElectronSecondaryStep{
		schedule: e, deadline: deadline,
	}
	return raknet.ScheduledPacketProducer{
		Delay: deadline, Produce: step.produce,
	}
}

func (e campaignElectronSecondaryStep) produce() ([][]byte, error) {
	schedule := e.schedule
	schedule.runtime.registry.mutex.Lock()
	peerSession, isFound :=
		schedule.runtime.registry.sessions[schedule.sessionKey]
	isCurrent := isFound && peerSession.generation == schedule.generation &&
		peerSession.sageAttacks[schedule.projectileObjectID] == schedule.run
	if !isCurrent {
		schedule.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	snapshot := schedule.run.Snapshot(schedule.runtime.now())
	if !snapshot.IsActive {
		schedule.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	projectilePosition := game.Vec3(snapshot.Position)
	enemies := schedule.runtime.registry.abilityTargetsLocked(schedule.runtime, peerSession, schedule.definition,
		schedule.packet.SourceTime+uint64(e.deadline/time.Millisecond))
	random, randomErr := schedule.runtime.registry.abilityRandomLocked(peerSession)
	if randomErr != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("electronRandom: %w", randomErr)
	}
	secondaryPlan, err := zoneability.PlanElectronSphereSecondary(
		enemies, schedule.sourceObjectID,
		projectilePosition, schedule.creature, schedule.definition,
	)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignElectronSecondaryPlan: %w", err)
	}
	results, err := zoneability.CommitElectronSphereSecondary(
		random, enemies, secondaryPlan, schedule.creature,
		peerSession.binding.Difficulty, schedule.runtime.program.Critical,
	)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignElectronSecondaryCommit: %w", err)
	}
	transitions := make([]campaignDamageTransition, 0, len(results))
	for _, result := range results {
		if peerSession.binding.Mode == game.ModeArena {
			continue
		}
		transition, transitionErr :=
			peerSession.applyCampaignDamageTransition(result.Damage)
		if transitionErr != nil {
			schedule.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf(
				"campaignElectronSecondaryTransition: %w", transitionErr,
			)
		}
		transitions = append(transitions, transition)
	}
	nextDelay, err := zoneability.ElectronSphereSecondaryDelay(
		random,
		schedule.definition.LightningSecondary,
	)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignElectronSecondaryDelay: %w", err)
	}
	nextDeadline := e.deadline + nextDelay
	isRescheduled := snapshot.RemainingFlightDuration > nextDelay
	if peerSession.binding.Mode != game.ModeArena {
		schedule.runtime.registry.sessions[schedule.sessionKey] = peerSession
	}
	schedule.runtime.registry.mutex.Unlock()

	timestamp := schedule.packet.SourceTime +
		uint64(e.deadline/time.Millisecond)
	var packets [][]byte
	if arenaTargets, isArena := enemies.(*arenaAbilityTargets); isArena {
		arenaTargets.finish()
		packets, err = arenaTargets.packets(results)
	} else {
		packets, err = schedule.runtime.damage.publishAreaResults(
			schedule.packet, schedule.sessionKey, schedule.generation,
			schedule.sourceObjectID, timestamp, schedule.binding,
			results, transitions, nil, false,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("campaignElectronSecondaryPublish: %w", err)
	}
	for _, result := range results {
		effectPackets, effectErr := effectraknet.ChainHit(
			effectraknet.ChainHitRequest{
				EffectID: util.HashID(
					schedule.definition.LightningSecondary.EffectName,
				),
				HitEffectID: util.HashID(
					schedule.definition.LightningSecondary.HitEffectName,
				),
				SourceObjectID: schedule.projectileObjectID,
				TargetObjectID: result.Damage.ObjectID,
			},
		)
		if effectErr != nil {
			return nil, fmt.Errorf(
				"campaignElectronSecondaryEffect: %w", effectErr,
			)
		}
		packets = append(packets, effectPackets...)
	}
	if isRescheduled {
		err = schedule.packet.ScheduleFunc(
			nextDelay, schedule.producer(nextDeadline).Produce,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"campaignElectronSecondaryReschedule: %w", err,
			)
		}
	}
	return packets, nil
}

func (e campaignProjectileReleaseStep) produce() ([][]byte, error) {
	e.registry.mutex.RLock()
	peerSession, isFound := e.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.deployedObjectID == e.sourceObjectID
	isReleaseCurrent := isCurrent &&
		peerSession.abilityReleaseSession().IsCurrent(e.releaseReservation)
	e.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	if !isReleaseCurrent {
		return [][]byte{e.packet}, nil
	}
	resetPacket, err := abilityraknet.AnimationReset(
		e.sourceObjectID, e.timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("campaignProjectileReleaseAnimation: %w", err)
	}
	return [][]byte{e.packet, resetPacket}, nil
}

type campaignCloudLobSchedule struct {
	runtime              campaignAbilityCommandRuntime
	packet               raknet.Packet
	sessionKey           string
	generation           uint64
	sourceObjectID       uint32
	previousNextObjectID uint32
	projectileObjectID   uint32
	cloudObjectID        uint32
	projectileIndex      int
	landingDelay         time.Duration
	creature             game.GameplayCreature
	plan                 zoneability.CloudLobPlan
	binding              game.GameplayBinding
	run                  *campaignCloudLobRun
	previousSequence     zoneability.SequenceSnapshot
	sequenceRevision     uint64
	previousMotion       zoneaction.MotionSnapshot
	motionRevision       uint64
	releaseReservation   zoneaction.ReleaseReservation
	releaseResponse      []byte
}

func (e campaignCloudLobSchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.cloudLobAttacks[e.projectileObjectID] == e.run
}

func (e campaignCloudLobSchedule) produceLaunch() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		e.run.lob = newGraviticLob(e.runtime.now(), e.plan.LaunchPosition[e.projectileIndex], e.plan.Lob[e.projectileIndex])
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	// lobStartTime's reflected setter maps the server clock into simulation
	// time. ClientTime is a wall-clock observation, not a server timestamp.
	packets, err := abilityraknet.CloudLobLaunch(
		e.plan, e.projectileObjectID, e.projectileIndex, e.packet.SourceTime,
	)
	if err != nil {
		return nil, fmt.Errorf("campaignCloudLobLaunch: %w", err)
	}
	return packets, nil
}

func (e campaignCloudLobSchedule) produceLanding() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(member, isFound)
	lob := e.run.lob
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	remaining := lob.remaining(e.runtime.now(), 0)
	if remaining > abilityraknet.ProjectileCollisionTick {
		err := scheduleNPCProducer(e.runtime.registry, e.packet,
			min(remaining, campaignProjectileMotionPollInterval), e.produceLanding)
		if err != nil {
			return nil, fmt.Errorf("cloudSlowResume: %w", err)
		}
		return nil, nil
	}
	landingPackets, err := abilityraknet.CloudLobLanding(
		e.plan, e.projectileObjectID, e.cloudObjectID,
	)
	if err != nil {
		return nil, fmt.Errorf("campaignCloudLobCreate: %w", err)
	}
	timestamp := e.packet.SourceTime +
		uint64(e.landingDelay/time.Millisecond)
	poisonPackets, err := e.commitPoison(true, timestamp)
	if err != nil {
		e.runtime.logger.Printf("Sprout accepted landing publication: %v", err)
	}
	// Queue accepted world presentation before arming any periodic damage.
	// Source transport departure cannot discard the co-op cloud baseline.
	e.publishCloudLanding(append(landingPackets, poisonPackets...))
	return nil, nil
}

func (e campaignCloudLobSchedule) produceRelease() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	return [][]byte{e.releaseResponse}, nil
}

func (e campaignCloudLobSchedule) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		delete(peerSession.cloudLobAttacks, e.projectileObjectID)
		e.removeCloudPoisonRegionLocked(peerSession)
		peerSession.basicSequenceSession().ReleaseHeld()
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if isCurrent {
		e.runtime.logger.Printf(
			"RakNet campaign cloud lob stopped after schedule failure for %s: %v",
			e.sessionKey, scheduleErr,
		)
	}
}

func (e campaignCloudLobSchedule) rollbackAdmission() {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if isFound && peerSession.generation == e.generation {
		peerSession.basicSequenceSession().Restore(
			e.previousSequence, e.sequenceRevision,
		)
		peerSession.restoreCampaignProjectileID(e.previousNextObjectID)
		peerSession.abilityReleaseSession().Rollback(e.releaseReservation)
		peerSession.restorePlayerMotion(e.previousMotion, e.motionRevision)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
}

func (e campaignProjectileScheduleFailure) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	if !isFound || peerSession.generation != e.generation ||
		len(e.projectileObjectIDs) == 0 ||
		len(e.projectileObjectIDs) != len(e.runs) {
		return false
	}
	for index, projectileObjectID := range e.projectileObjectIDs {
		if peerSession.sageAttacks[projectileObjectID] == e.runs[index] {
			return true
		}
	}
	return false
}

func (e campaignProjectileScheduleFailure) handle(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	for _, retirement := range e.retirements {
		retireErr := e.runtime.registry.retireCampaignProjectileLocked(retirement)
		if retireErr != nil && e.runtime.logger != nil {
			e.runtime.logger.Printf("RakNet projectile schedule cleanup failed projectile=%d: %v", retirement.objectID, retireErr)
		}
	}
	if isCurrent {
		// Retirement updated the stored member's queued packets and run map.
		peerSession = e.runtime.registry.sessions[e.sessionKey]
		peerSession.basicSequenceSession().ReleaseHeld()
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if isCurrent || len(e.retirements) > 0 {
		if e.runtime.logger != nil {
			e.runtime.logger.Printf(
				"RakNet campaign projectile stopped after schedule failure for %s: %v",
				e.sessionKey, scheduleErr,
			)
		}
	}
}

func (e campaignProjectileScheduleFailure) rollbackAdmission() {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if isFound && peerSession.generation == e.generation {
		peerSession.basicSequenceSession().Restore(
			e.previousSequence, e.sequenceRevision,
		)
		peerSession.restoreCampaignProjectileID(
			e.previousProjectileObjectID,
		)
		peerSession.abilityCooldownSession().Rollback(
			e.cooldownReservation,
		)
		peerSession.abilityReleaseSession().Rollback(
			e.releaseReservation,
		)
		_ = peerSession.setCampaignCharacterManaPoints(
			e.creatureIndex, e.previousManaPoint,
		)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
}

func (e campaignTossSchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.tossAttacks[e.projectileObjectID] == e.run
}

func (e campaignTossSchedule) produceRelease() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	animationPacket, err := abilityraknet.AnimationReset(
		e.sourceObjectID,
		e.packet.SourceTime+uint64(e.plan.Definition.ReleaseDelay/time.Millisecond),
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignTossReleaseAnimation: %w", err)
	}
	e.run.isReleased = true
	if e.run.isLanded {
		delete(peerSession.tossAttacks, e.projectileObjectID)
		e.run.cancel = nil
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	return [][]byte{e.releaseResponse, animationPacket}, nil
}

func (e campaignTossLandingStep) produce() ([][]byte, error) {
	schedule := e.schedule
	schedule.runtime.registry.mutex.Lock()
	peerSession, isFound :=
		schedule.runtime.registry.sessions[schedule.sessionKey]
	if !schedule.isCurrent(peerSession, isFound) || schedule.run.isLanded {
		schedule.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	remaining := schedule.run.lob.remaining(schedule.runtime.now(), e.elapsed)
	if remaining > abilityraknet.ProjectileCollisionTick {
		schedule.runtime.registry.mutex.Unlock()
		err := scheduleNPCProducer(schedule.runtime.registry, schedule.packet,
			min(remaining, campaignProjectileMotionPollInterval), e.produce)
		if err != nil {
			return nil, fmt.Errorf("tossSlowResume: %w", err)
		}
		return nil, nil
	}
	// Lob locomotion publishes a fixed destination. Damage and effects must
	// resolve there even when the selected enemy has moved during flight.
	impactPlan := schedule.plan
	enemies := schedule.runtime.registry.abilityTargetsLocked(schedule.runtime, peerSession, impactPlan.Definition,
		schedule.packet.SourceTime+uint64(e.deadline/time.Millisecond))
	tossTargets, isTossSupported := enemies.(zoneability.TossTargets)
	if !isTossSupported {
		schedule.runtime.registry.mutex.Unlock()
		return nil, errors.New("toss target status unavailable")
	}
	liveTarget, isTargetFound := enemies.NPC(impactPlan.TargetObjectID)
	landingPlan, isStrongImpact, err := zoneability.ResolveTossImpact(
		tossTargets, impactPlan, schedule.creature,
		schedule.startTime.Add(e.deadline),
	)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignTossLanding: %w", err)
	}
	isDetonated, err := zoneability.ShouldDetonate(
		schedule.plan.Definition, e.elapsed, len(landingPlan.Target),
	)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignTossFuse: %w", err)
	}
	if !isDetonated {
		schedule.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	random, err := schedule.runtime.registry.abilityRandomLocked(peerSession)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("tossRandom: %w", err)
	}
	results, err := zoneability.CommitArea(
		random, enemies, landingPlan, schedule.creature,
		peerSession.binding.Difficulty, schedule.runtime.program.Critical,
	)
	if err != nil {
		schedule.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignTossDamage: %w", err)
	}
	if peerSession.binding.Mode == game.ModeArena {
		// Hits publish through the real target sessions and may queue source packets.
		peerSession = schedule.runtime.registry.sessions[schedule.sessionKey]
	}
	transitions := make([]campaignDamageTransition, 0, len(results))
	for _, result := range results {
		if peerSession.binding.Mode == game.ModeArena {
			continue
		}
		transition, transitionErr :=
			peerSession.applyCampaignDamageTransition(result.Damage)
		if transitionErr != nil {
			schedule.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf(
				"campaignTossTransition: %w", transitionErr,
			)
		}
		transitions = append(transitions, transition)
	}
	schedule.run.isLanded = true
	schedule.run.landingCancel = nil
	isCastReleased := schedule.run.isReleased
	if schedule.run.isReleased {
		delete(peerSession.tossAttacks, schedule.projectileObjectID)
		schedule.run.cancel = nil
	}
	schedule.runtime.registry.sessions[schedule.sessionKey] = peerSession
	schedule.runtime.registry.mutex.Unlock()

	timestamp := schedule.packet.SourceTime +
		uint64(e.deadline/time.Millisecond)
	var resultPackets [][]byte
	if arenaTargets, isArena := enemies.(*arenaAbilityTargets); isArena {
		arenaTargets.finish()
		resultPackets, err = arenaTargets.packets(results)
	} else {
		resultPackets, err = schedule.runtime.damage.publishAreaResults(
			schedule.packet, schedule.sessionKey, schedule.generation,
			schedule.sourceObjectID, timestamp, schedule.binding,
			results, transitions, nil, false,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("campaignTossPublish: %w", err)
	}
	landingPackets, err := abilityraknet.TossLanding(
		impactPlan, schedule.projectileObjectID,
		resultPackets, isStrongImpact,
	)
	if err != nil {
		return nil, fmt.Errorf("campaignTossCleanup: %w", err)
	}
	schedule.runtime.logger.Printf(
		"RakNet projectile trajectory resolved kind=hero-toss projectile=%d source=%d target=%d ability=%q destination=(%.3f,%.3f,%.3f) target_now=(%.3f,%.3f,%.3f) target_found=%t hit_count=%d cast_released=%t",
		schedule.projectileObjectID, schedule.sourceObjectID, impactPlan.TargetObjectID,
		impactPlan.Definition.Name, impactPlan.Destination.X, impactPlan.Destination.Y,
		impactPlan.Destination.Z, liveTarget.Plan.Position.X, liveTarget.Plan.Position.Y,
		liveTarget.Plan.Position.Z, isTargetFound, len(results), isCastReleased,
	)
	return landingPackets, nil
}

func (e campaignTossSchedule) landingProducer(
	elapsed time.Duration,
) raknet.ScheduledPacketProducer {
	deadline := e.plan.Lob.StartTime + e.plan.Lob.Duration + elapsed
	step := campaignTossLandingStep{
		schedule: e, elapsed: elapsed, deadline: deadline,
	}
	return raknet.ScheduledPacketProducer{
		Delay: e.plan.Lob.Duration + elapsed, Produce: step.produce,
	}
}

func (e campaignTossSchedule) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		e.run.stop()
		delete(peerSession.tossAttacks, e.projectileObjectID)
		peerSession.basicSequenceSession().ReleaseHeld()
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if isCurrent {
		e.runtime.logger.Printf(
			"RakNet campaign toss stopped after schedule failure for %s: %v",
			e.sessionKey, scheduleErr,
		)
	}
}

func (e campaignTossSchedule) rollbackAdmission() {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if isFound && peerSession.generation == e.generation {
		peerSession.basicSequenceSession().Restore(
			e.previousSequence, e.sequenceRevision,
		)
		peerSession.restoreCampaignProjectileID(
			e.previousProjectileObjectID,
		)
		peerSession.abilityReleaseSession().Rollback(e.releaseReservation)
		peerSession.restorePlayerMotion(e.previousMotion, e.motionRevision)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
}

func (e campaignHeldRepeatStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.pendingBasic == nil &&
		peerSession.basicSequenceSession().IsHeldAt(e.heldGeneration)
	command := latestBasicHeldCommand(peerSession, e.command)
	previousCooldownEnd := peerSession.basicSequenceSession().CooldownEnd()
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	repeatContext := context.WithValue(
		context.Background(), basicHeldContextKey{}, e.heldGeneration,
	)
	packets, err := e.runtime.handle(repeatContext, e.packet, command)
	e.runtime.registry.mutex.Lock()
	latestSession, isLatestFound :=
		e.runtime.registry.sessions[e.sessionKey]
	isMatching := isLatestFound &&
		latestSession.generation == e.generation &&
		latestSession.basicSequenceSession().HeldGeneration() ==
			e.heldGeneration
	isRepeated := isMatching &&
		latestSession.basicSequenceSession().CooldownEnd().After(previousCooldownEnd)
	if isMatching && !isRepeated && latestSession.pendingBasic == nil {
		latestSession.basicSequenceSession().ReleaseHeld()
		e.runtime.registry.sessions[e.sessionKey] = latestSession
	}
	e.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("campaignHeldRepeat: %w", err)
	}
	return packets, nil
}

func (r campaignBasicScheduleRun) release() ([][]byte, error) {
	r.runtime.registry.mutex.Lock()
	peerSession, isFound := r.runtime.registry.sessions[r.sessionKey]
	isCurrent := isFound && peerSession.generation == r.generation &&
		peerSession.basicAttack == r.attack
	if !isCurrent {
		r.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	packet, err := r.attack.Advance(
		context.Background(), r.selection.ReleaseDelay,
	)
	if err == nil {
		r.attack.ClearCancel()
		peerSession.basicAttack = nil
		peerSession.basicAttackSyncStamp = 0
		r.runtime.registry.sessions[r.sessionKey] = peerSession
	}
	r.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("campaignBasicRelease: %w", err)
	}
	packet = append(packet, r.releaseAck)
	return packet, nil
}

func (r campaignBasicScheduleRun) repeat() ([][]byte, error) {
	r.runtime.registry.mutex.Lock()
	peerSession, isFound := r.runtime.registry.sessions[r.sessionKey]
	isCurrent := isFound && peerSession.generation == r.generation &&
		peerSession.pendingBasic == nil &&
		peerSession.basicSequenceSession().IsHeldAt(r.heldGeneration)
	command := latestBasicHeldCommand(peerSession, r.command)
	r.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	repeatContext := context.WithValue(
		context.Background(), basicHeldContextKey{}, r.heldGeneration,
	)
	packet, err := r.runtime.handle(repeatContext, r.repeatPacket, command)
	r.runtime.registry.mutex.Lock()
	latestSession, isLatestFound := r.runtime.registry.sessions[r.sessionKey]
	isRepeated := isLatestFound &&
		latestSession.generation == r.generation &&
		latestSession.basicSequenceSession().HeldGeneration() == r.heldGeneration &&
		latestSession.basicAttack != nil
	isMatching := isLatestFound &&
		latestSession.generation == r.generation &&
		latestSession.basicSequenceSession().HeldGeneration() == r.heldGeneration
	if !isRepeated && isMatching && latestSession.pendingBasic == nil {
		latestSession.basicSequenceSession().ReleaseHeld()
		r.runtime.registry.sessions[r.sessionKey] = latestSession
	}
	r.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("campaignBasicRepeat: %w", err)
	}
	return packet, nil
}

func (r campaignBasicScheduleRun) fail(scheduleErr error) {
	r.runtime.registry.mutex.Lock()
	peerSession, isFound := r.runtime.registry.sessions[r.sessionKey]
	isCurrent := isFound && peerSession.generation == r.generation &&
		peerSession.basicAttack == r.attack
	if isCurrent {
		r.attack.ClearCancel()
		peerSession.basicAttack = nil
		peerSession.basicAttackSyncStamp = 0
		peerSession.basicSequenceSession().ReleaseHeld()
		r.runtime.registry.sessions[r.sessionKey] = peerSession
	}
	r.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return
	}
	r.attack.Stop()
	r.runtime.logger.Printf(
		"RakNet campaign basic stopped after schedule failure for %s: %v",
		r.sessionKey, scheduleErr,
	)
}

func (r campaignAbilityCommandRuntime) handleMeleeBasic(
	request campaignCharacterAbilityRequest,
	peerSession gameplayPeerSession, creature game.GameplayCreature,
	definition sim.AbilityDefinition, plan zoneability.BasicPlan,
	targetObjectID uint32,
	activeAbilityID uint32, isBasicHeldRepeat bool, isBasicHeldInput bool,
	sessionKey string, abilityStartTime time.Time,
	directAggro campaignDirectAggroPublication,
) ([][]byte, error) {
	var err error
	packet := request.packet
	command := request.command
	plan.SourceRank = 1
	if peerSession.isOverdriveActiveAt(abilityStartTime) {
		plan.SourceRank = 3
	}
	isActiveRequest := activeAbilityID != 0
	if definition.Kind != sim.AbilityKindMelee {
		r.registry.mutex.Unlock()
		return request.reject("basic runtime shape unsupported")
	}
	enemies := r.registry.abilityTargetsLocked(r, peerSession, definition, packet.SourceTime)
	random, randomErr := r.registry.abilityRandomLocked(peerSession)
	if randomErr != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("meleeRandom: %w", randomErr)
	}
	if plan.TargetObjectID != 0 {
		err = zoneability.ValidateBasicCommit(
			random, enemies,
			plan, creature, peerSession.binding.Difficulty, r.program.Critical,
		)
		if err != nil {
			r.registry.mutex.Unlock()
			return request.reject(err.Error())
		}
	}
	previousBasicSequence := peerSession.basicSequenceSession().Snapshot()
	previousPlayerMotion := peerSession.playerMotionSnapshot()
	previousManaPoint := peerSession.deployedManaPoint()
	previousFireRavagerBasicCount :=
		peerSession.fireRavagerBasicCount[peerSession.deployedCreatureIndex]
	remainingManaPoint := previousManaPoint
	if isActiveRequest {
		manaCost, manaErr := game.ResolveAbilityManaCost(
			plan.Definition.ManaCost, creature.DamageProfile.PrimaryAttribute,
			plan.Definition.ManaCoefficient,
			peerSession.isOverdriveActiveAt(abilityStartTime),
		)
		if manaErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignMeleeManaProjection: %w", manaErr)
		}
		if remainingManaPoint < manaCost {
			r.registry.mutex.Unlock()
			return request.reject("power unavailable")
		}
		remainingManaPoint -= manaCost
	}
	if !isActiveRequest && !isBasicHeldRepeat {
		peerSession.basicSequenceSession().SetHeld(isBasicHeldInput)
	}
	heldGeneration := peerSession.basicSequenceSession().HeldGeneration()
	selection, selectionErr := zoneability.SelectActivation(plan.Definition)
	if !isActiveRequest {
		selection, selectionErr = peerSession.basicSequenceSession().Accept(
			abilityStartTime, plan.Definition,
		)
	}
	if selectionErr != nil {
		if !isActiveRequest {
			peerSession.basicSequenceSession().Restore(
				previousBasicSequence,
				peerSession.basicSequenceSession().Revision(),
			)
		}
		r.registry.mutex.Unlock()
		return request.reject(selectionErr.Error())
	}
	basicSequenceRevision := peerSession.basicSequenceSession().Revision()
	selectedDefinition := plan.Definition
	selectedDefinition.AnimationName = selection.AnimationName
	selectedDefinition.HitDelay = selection.HitDelay
	selectedDefinition.ReleaseDelay = selection.ReleaseDelay
	if selection.AnimationIndex < len(selectedDefinition.AnimationDamageMultipliers) {
		damageMultiplier := selectedDefinition.AnimationDamageMultipliers[selection.AnimationIndex]
		if damageMultiplier <= 0 {
			r.registry.mutex.Unlock()
			return request.reject("invalid animation damage multiplier")
		}
		plan.Damage.Minimum *= damageMultiplier
		plan.Damage.Maximum *= damageMultiplier
	}
	if len(selectedDefinition.RandomMeleeDamagePolicies) != 0 {
		policyIndex, policyErr := random.Index(
			uint32(len(selectedDefinition.RandomMeleeDamagePolicies)),
		)
		if policyErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignMeleeDamagePolicy: %w", policyErr)
		}
		damagePolicy := selectedDefinition.RandomMeleeDamagePolicies[policyIndex]
		if damagePolicy.MinimumMultiplier <= 0 || damagePolicy.MaximumMultiplier <= 0 {
			r.registry.mutex.Unlock()
			return request.reject("invalid random melee damage policy")
		}
		plan.Damage.Minimum *= damagePolicy.MinimumMultiplier
		plan.Damage.Maximum *= damagePolicy.MaximumMultiplier
		selectedDefinition.HitEffectName = damagePolicy.EffectName
	}
	if len(selectedDefinition.MeleeHitPolicies) != 0 {
		policyIndex := selection.AnimationIndex % len(selectedDefinition.MeleeHitPolicies)
		if selectedDefinition.MeleeHitPolicies[policyIndex].EffectName != "" {
			selectedDefinition.HitEffectName = selectedDefinition.MeleeHitPolicies[policyIndex].EffectName
		}
	}
	plan.Damage = normalizeCampaignMeleeDamage(plan.Damage)
	plan.Definition = selectedDefinition
	enemy, isEnemyFound := enemies.NPC(targetObjectID)
	hitState := &campaignMeleeHitState{}
	if isEnemyFound {
		hitState.initialTargetPosition = enemy.Plan.Position
		// Pursuit retries may admit a little beyond reach; that tolerance must
		// not grant the native start-range latch used by the movement grace.
		startRange := heroAbilityAdmissionRange(creature, definition) +
			peerSession.deployedCampaignActorFootprintRadius() + enemy.Plan.ActorFootprintRadius()
		hitState.isTargetInRangeAtStart = zonegeometry.Distance(plan.SourcePosition, enemy.Plan.Position) <= startRange
	}
	targetPosition := command.Ability.TargetPosition
	if !isReportedZonePosition(targetPosition) {
		targetPosition = command.Ability.CursorPosition
	}
	if isEnemyFound {
		targetPosition = raknet.Vector3{
			X: enemy.Plan.Position.X, Y: enemy.Plan.Position.Y, Z: enemy.Plan.Position.Z,
		}
	}
	if !isEnemyFound && (!isReportedZonePosition(targetPosition) ||
		!isFiniteZonePosition(targetPosition)) {
		r.registry.mutex.Unlock()
		return request.reject("cursor position unavailable")
	}
	facing := geometryraknet.Direction(peerSession.playerPosition, targetPosition)
	if isEnemyFound && targetPosition.X == peerSession.playerPosition.X && targetPosition.Y == peerSession.playerPosition.Y {
		resolvedFacing, facingErr := campaignMeleeFacing(peerSession, command, targetPosition)
		if facingErr != nil {
			// Active melee abilities use SelectActivation and never mutate the
			// basic sequence. Restore only admission actually made above.
			isSequenceRestored := true
			if !isActiveRequest {
				isSequenceRestored = peerSession.basicSequenceSession().Restore(previousBasicSequence, basicSequenceRevision)
			}
			r.registry.mutex.Unlock()
			if !isSequenceRestored {
				return nil, fmt.Errorf("meleeFacingRestore: %w", errors.Join(facingErr, errors.New("basic sequence changed before facing rejection rollback")))
			}
			return request.reject(facingErr.Error())
		}
		facing = resolvedFacing
	}
	runTargetObjectID := targetObjectID
	targetHitPoint := float32(0)
	if runTargetObjectID == 0 {
		runTargetObjectID = command.Common.ObjectID
	} else {
		targetHitPoint = enemy.HitPoint
	}
	meleeRunDefinition := selectedDefinition
	// Hero impact presentation is projected from the authoritative committed hit
	// below. Keeping it out of the speculative timed run prevents an invalidated
	// target from receiving an effect and gives every accepted hit one exact
	// content-authored sound/effect packet.
	meleeRunDefinition.HitEffectName = ""
	basicRun, immediatePackets, runErr := abilityraknet.NewMeleeRun(abilityraknet.MeleeInput{
		Ability: meleeRunDefinition, ActorObjectID: command.Common.ObjectID,
		TargetObjectID: runTargetObjectID,
		ActorPosition:  toSimPosition(peerSession.playerPosition),
		TargetPosition: sim.Position{
			X: targetPosition.X, Y: targetPosition.Y, Z: targetPosition.Z,
		},
		TargetFacing: sim.Position{X: facing.X, Y: facing.Y, Z: facing.Z},
		Damage:       plan.Damage.Maximum, TargetHitPoint: targetHitPoint,
		ActorTeam: abilityActorTeam(peerSession), SourceTime: packet.SourceTime, IsUnreliableStopNeeded: true,
		HitDelays: selection.HitDelays,
	})
	if runErr != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignBasicRun: %w", runErr)
	}
	err = peerSession.stopPlayerMovement(abilityStartTime)
	if err != nil {
		basicRun.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignBasicStop: %w", err)
	}
	movementPackets, marshalErr := marshalZonePlayerAttackPose(
		command.Common.ObjectID, peerSession.playerPosition, facing, targetPosition,
	)
	if marshalErr != nil {
		basicRun.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignBasicStopMarshal: %w", marshalErr)
	}
	playerMotionRevision := peerSession.playerMotionRevision()
	ackPacket, marshalErr := abilityraknet.Acknowledge(
		abilityraknet.AcknowledgeRequest{
			SyncStamp:    command.Common.Unknown[0],
			ResponseType: raknet.ActionResponseAccepted,
			ObjectID:     plan.AbilityID, AbilityIndex: command.Ability.Index,
			SourceStartMilliseconds: packet.SourceTime,
			SourceCommitMilliseconds: packet.SourceTime +
				uint64(selection.HitDelay/time.Millisecond),
			SourceEndMilliseconds: packet.SourceTime +
				uint64(selection.ReleaseDelay/time.Millisecond),
		},
	)
	if marshalErr != nil {
		basicRun.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignBasicAck: %w", marshalErr)
	}
	releaseAckPacket, marshalErr := abilityraknet.ReleaseResponse(
		command.Common.Unknown[0], plan.AbilityID, command.Ability.Index,
		packet.SourceTime, selection.HitDelay, selection.ReleaseDelay,
	)
	if marshalErr != nil {
		basicRun.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignBasicReleaseAck: %w", marshalErr)
	}
	cooldownPacket, marshalErr := abilityraknet.Cooldown(
		abilityraknet.CooldownRequest{
			ObjectID: command.Common.ObjectID, AbilityID: plan.AbilityID,
			Duration:  selectedDefinition.Cooldown,
			StartTime: packet.SourceTime,
		},
	)
	if marshalErr != nil {
		basicRun.Stop()
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignBasicCooldown: %w", marshalErr)
	}
	var manaPacket []byte
	if isActiveRequest {
		manaPacket, marshalErr = abilityraknet.Mana(
			command.Common.ObjectID, remainingManaPoint,
		)
		if marshalErr != nil {
			basicRun.Stop()
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignMeleeMana: %w", marshalErr)
		}
	}
	cooldownReservation := zoneability.CooldownReservation{}
	if isActiveRequest {
		isCooldownReserved := false
		cooldownReservation, isCooldownReserved =
			peerSession.abilityCooldownSession().Reserve(
				zoneability.HeroAbilityCooldown(activeAbilityID),
				abilityStartTime, selectedDefinition.Cooldown,
			)
		if !isCooldownReserved {
			basicRun.Stop()
			r.registry.mutex.Unlock()
			return request.reject("cooldown unavailable")
		}
		err = peerSession.setDeployedManaPoints(remainingManaPoint)
		if err != nil {
			peerSession.abilityCooldownSession().Rollback(cooldownReservation)
			basicRun.Stop()
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("campaignMeleeManaCommit: %w", err)
		}
	}
	generation := peerSession.generation
	binding := peerSession.binding
	// Chunk555's UsedAbility/DealtDamage callbacks exclude basic attacks.
	// The shared accepted-cast and damage publication owners handle the passive.
	isFireRavagerStun := false
	isShadowCloakCrit := peerSession.heroModifierRun != nil &&
		peerSession.heroModifierRun.isShadowStealth
	peerSession.basicAttack = basicRun
	peerSession.basicAttackSyncStamp = command.Common.Unknown[0]
	peerSession.retainAttackPose(
		facing, targetPosition, targetObjectID,
		abilityStartTime.Add(selectedDefinition.ReleaseDelay),
	)
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	meleeSchedule := campaignMeleeSchedule{
		squad:    peerSession.squad,
		hitState: hitState,
		runtime:  r, packet: packet, sessionKey: sessionKey, generation: generation,
		sourceObjectID: command.Common.ObjectID, targetObjectID: targetObjectID,
		creature: creature, definition: definition,
		selection: selection, selected: selectedDefinition, plan: plan,
		binding: binding, run: basicRun, releaseResponse: releaseAckPacket,
		isFireRavagerStun: isFireRavagerStun,
		isShadowCloakCrit: isShadowCloakCrit,
	}
	repeatDelay := max(selectedDefinition.Cooldown, selectedDefinition.ReleaseDelay)
	if !isActiveRequest {
		repeatDelay = peerSession.basicSequenceSession().CooldownEnd().Sub(abilityStartTime)
	}
	repeatPayload := append([]byte(nil), packet.Payload...)
	repeatPacket := packet
	repeatPacket.Payload = repeatPayload
	repeatPacket.SourceTime += uint64(repeatDelay / time.Millisecond)
	scheduleRun := campaignBasicScheduleRun{
		runtime: r, sessionKey: sessionKey, generation: generation,
		attack: basicRun, selection: selection,
		releaseAck: releaseAckPacket, heldGeneration: heldGeneration,
		repeatPacket: repeatPacket, command: command,
	}
	producers := make([]raknet.ScheduledPacketProducer, 0, len(selection.HitDelays)+2)
	for hitIndex, hitDelay := range selection.HitDelays {
		producers = append(producers, meleeSchedule.producer(hitIndex, hitDelay))
	}
	if selectedDefinition.Name == "LFPoisonRavager_Expunge" {
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay:   500 * time.Millisecond,
			Produce: campaignExpungeStep{schedule: meleeSchedule}.produce,
		})
	}
	producers = append(producers, raknet.ScheduledPacketProducer{
		Delay: selection.ReleaseDelay, Produce: scheduleRun.release,
	})
	if isBasicHeldInput {
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: repeatDelay, Produce: scheduleRun.repeat,
		})
	}
	producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
	var cancel raknet.CancelSchedule
	if packet.ScheduleGroupResult != nil {
		cancel, err = packet.ScheduleGroupResult(producers, scheduleRun.fail)
	} else {
		cancel, err = packet.ScheduleGroup(producers)
	}
	if err != nil {
		r.registry.mutex.Lock()
		currentSession, isCurrentFound := r.registry.sessions[sessionKey]
		isCurrent := isCurrentFound && currentSession.generation == generation &&
			currentSession.basicAttack == basicRun
		if isCurrent {
			currentSession.basicAttack = nil
			currentSession.basicAttackSyncStamp = 0
			currentSession.fireRavagerBasicCount[currentSession.deployedCreatureIndex] =
				previousFireRavagerBasicCount
			if !isActiveRequest {
				currentSession.basicSequenceSession().Restore(
					previousBasicSequence, basicSequenceRevision,
				)
			}
			currentSession.restorePlayerMotion(previousPlayerMotion, playerMotionRevision)
			if isActiveRequest {
				_ = currentSession.setCampaignCharacterManaPoints(
					currentSession.deployedCreatureIndex, previousManaPoint,
				)
				currentSession.abilityCooldownSession().Rollback(cooldownReservation)
			}
			r.registry.sessions[sessionKey] = currentSession
		}
		r.registry.mutex.Unlock()
		basicRun.Stop()
		return nil, fmt.Errorf("campaignBasicSchedule: %w", err)
	}
	basicRun.SetCancel(cancel)
	r.logger.Printf("RakNet campaign basic accepted source=%d target=%d ability=%q",
		command.Common.ObjectID, targetObjectID, selectedDefinition.Name)
	startPackets := append(immediatePackets, cooldownPacket)
	if len(manaPacket) != 0 {
		startPackets = append(startPackets, manaPacket)
	}
	responsePackets := append([][]byte{ackPacket}, movementPackets...)
	responsePackets = append(responsePackets, startPackets...)
	return directAggro.publish(responsePackets)
}

func normalizeCampaignMeleeDamage(damage game.DamageRange) game.DamageRange {
	minimum := float64(damage.Minimum)
	maximum := float64(damage.Maximum)
	if math.IsNaN(minimum) || math.IsNaN(maximum) ||
		math.IsInf(minimum, 0) || math.IsInf(maximum, 0) {
		return damage
	}
	damage.Minimum = max(float32(1), float32(math.Floor(minimum)))
	damage.Maximum = max(damage.Minimum, float32(math.Ceil(maximum)))
	return damage
}

func (r campaignAbilityCommandRuntime) handleTossBasic(
	ctx context.Context, request campaignCharacterAbilityRequest,
	peerSession gameplayPeerSession, creature game.GameplayCreature,
	definition sim.AbilityDefinition, targetObjectID uint32,
	isBasicHeldRepeat bool, isBasicHeldInput bool,
	sessionKey string, abilityStartTime time.Time,
) ([][]byte, error) {
	var err error
	packet := request.packet
	command := request.command
	// Toss template 630 applies muzzle offsets to GetCenterPoint, while
	// the actor's navigation/attack pose remains at its ground origin.
	sourceCenter := peerSession.playerPosition
	sourceHeight := peerSession.deployedCampaignFootprintRadius()
	physics, isPhysicsFound := r.program.NounPhysicsByID[creature.Noun]
	if isPhysicsFound {
		sourceHeight = max(sourceHeight, (physics.BoundMinimum.Z+physics.BoundMaximum.Z)*0.5)
	}
	sourceCenter.Z += max(float32(0), sourceHeight)
	isPursuitRetry := peerSession.campaignPlayerPursuitSession().IsRetry(
		command.Common.ObjectID, targetObjectID, command.Ability.Index,
		command.Common.Unknown[0],
	)
	admissionAllowance := float32(0)
	if isPursuitRetry {
		// Match basic admission when a target moves during the arrival retry.
		admissionAllowance = campaignPlayerPursuitRedirectDistance
	}
	requestedPosition := command.Ability.TargetPosition
	if !isReportedZonePosition(requestedPosition) {
		requestedPosition = command.Ability.CursorPosition
	}
	isTargetPositionFound := isReportedZonePosition(requestedPosition) &&
		isFiniteZonePosition(requestedPosition)
	enemies := r.registry.abilityTargetsLocked(r, peerSession, definition, packet.SourceTime)
	tossPlan, planErr := zoneability.PlanTossWithAdmission(
		enemies, zoneability.TossRequest{
			SourceObjectID: command.Common.ObjectID, TargetObjectID: targetObjectID,
			TargetPosition:        game.Vec3(requestedPosition),
			IsTargetPositionFound: isTargetPositionFound,
			SourcePosition:        game.Vec3(peerSession.playerPosition),
			SourceCenter:          game.Vec3(sourceCenter),
			Creature:              creature, Definition: definition,
			SourceFootprint:    peerSession.deployedCampaignActorFootprintRadius(),
			AdmissionAllowance: admissionAllowance,
		},
	)
	if planErr != nil {
		var rangeErr zoneability.TargetRangeError
		// VoodooTempestBasic authors shouldPursue. Toss attacks must transfer
		// to the same authoritative pursuit used by other ranged abilities.
		if errors.As(planErr, &rangeErr) && definition.IsShouldPursue && targetObjectID != 0 && peerSession.zone != nil {
			targetEnemy, isTargetFound := enemies.NPC(targetObjectID)
			if isTargetFound {
				request.startTime = abilityStartTime
				admissionRange := heroAbilityAdmissionRange(creature, definition)
				packets, isPursuitStarted, pursuitErr := r.transferPlayerAbilityPursuit(
					request, peerSession, targetEnemy, admissionRange, isPursuitRetry,
				)
				if pursuitErr != nil {
					return nil, fmt.Errorf("tossPursuit: %w", pursuitErr)
				}
				if isPursuitStarted {
					r.logger.Printf("RakNet campaign toss pursuit started source=%d target=%d ability=%s", command.Common.ObjectID, targetObjectID, definition.Name)
				}
				return packets, nil
			}
		}
		r.registry.mutex.Unlock()
		return request.reject(planErr.Error())
	}
	previousBasicSequence := peerSession.basicSequenceSession().Snapshot()
	previousProjectileObjectID := peerSession.nextProjectileObjectID
	previousPlayerMotion := peerSession.playerMotionSnapshot()
	if !isBasicHeldRepeat {
		peerSession.basicSequenceSession().SetHeld(isBasicHeldInput)
	}
	heldGeneration := peerSession.basicSequenceSession().HeldGeneration()
	tossPlan.Definition.AnimationName = tossPlan.Cast.AnimationName
	tossPlan.Definition.AnimationNames = []string{tossPlan.Cast.AnimationName}
	selection, selectionErr := peerSession.basicSequenceSession().Accept(
		abilityStartTime, tossPlan.Definition,
	)
	if selectionErr != nil {
		peerSession.basicSequenceSession().Restore(
			previousBasicSequence,
			peerSession.basicSequenceSession().Revision(),
		)
		r.registry.mutex.Unlock()
		return request.reject(selectionErr.Error())
	}
	basicSequenceRevision := peerSession.basicSequenceSession().Revision()
	tossPlan.Definition.AnimationName = selection.AnimationName
	tossPlan.Definition.HitDelay = selection.HitDelay
	tossPlan.Definition.ReleaseDelay = selection.ReleaseDelay
	projectileObjectID, err := r.registry.reserveAbilityObjectIDsLocked(&peerSession, 1, 1000)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignTossObjectID: %w", err)
	}
	if isPursuitRetry {
		peerSession.campaignPlayerPursuitSession().Cancel()
	}
	err = peerSession.stopPlayerMovement(abilityStartTime)
	if err != nil {
		peerSession.basicSequenceSession().Restore(previousBasicSequence, basicSequenceRevision)
		peerSession.restoreCampaignProjectileID(previousProjectileObjectID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignTossStop: %w", err)
	}
	playerMotionRevision := peerSession.playerMotionRevision()
	targetPosition := raknet.Vector3(tossPlan.Destination)
	facing := geometryraknet.Direction(peerSession.playerPosition, targetPosition)
	movementPackets, marshalErr := marshalZonePlayerAttackPose(
		command.Common.ObjectID, peerSession.playerPosition, facing,
		targetPosition,
	)
	if marshalErr != nil {
		peerSession.basicSequenceSession().Restore(previousBasicSequence, basicSequenceRevision)
		peerSession.restoreCampaignProjectileID(previousProjectileObjectID)
		peerSession.restorePlayerMotion(previousPlayerMotion, playerMotionRevision)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignTossStopMarshal: %w", marshalErr)
	}
	ackPacket, marshalErr := abilityraknet.Acknowledge(
		abilityraknet.AcknowledgeRequest{
			SyncStamp:    command.Common.Unknown[0],
			ResponseType: raknet.ActionResponseAccepted,
			ObjectID:     tossPlan.AbilityID, AbilityIndex: command.Ability.Index,
			SourceStartMilliseconds: packet.SourceTime,
			SourceCommitMilliseconds: packet.SourceTime +
				uint64(selection.HitDelay/time.Millisecond),
			SourceEndMilliseconds: packet.SourceTime +
				uint64(selection.ReleaseDelay/time.Millisecond),
		},
	)
	if marshalErr != nil {
		peerSession.basicSequenceSession().Restore(previousBasicSequence, basicSequenceRevision)
		peerSession.restoreCampaignProjectileID(previousProjectileObjectID)
		peerSession.restorePlayerMotion(previousPlayerMotion, playerMotionRevision)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignTossAck: %w", marshalErr)
	}
	releaseResponsePacket, marshalErr := abilityraknet.ReleaseResponse(
		command.Common.Unknown[0], tossPlan.AbilityID, command.Ability.Index,
		packet.SourceTime, selection.HitDelay, selection.ReleaseDelay,
	)
	if marshalErr != nil {
		peerSession.basicSequenceSession().Restore(previousBasicSequence, basicSequenceRevision)
		peerSession.restoreCampaignProjectileID(previousProjectileObjectID)
		peerSession.restorePlayerMotion(previousPlayerMotion, playerMotionRevision)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignTossReleaseResponse: %w", marshalErr)
	}
	startPackets, marshalErr := abilityraknet.StartPresentation(
		command.Common.ObjectID, tossPlan.AbilityID, selection.AnimationName,
		tossPlan.Definition.Cooldown, packet.SourceTime, 0,
	)
	if marshalErr != nil {
		peerSession.basicSequenceSession().Restore(previousBasicSequence, basicSequenceRevision)
		peerSession.restoreCampaignProjectileID(previousProjectileObjectID)
		peerSession.restorePlayerMotion(previousPlayerMotion, playerMotionRevision)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignTossStart: %w", marshalErr)
	}
	releaseReservation, isReleaseReserved :=
		peerSession.abilityReleaseSession().Reserve(
			abilityStartTime, selection.ReleaseDelay,
		)
	if !isReleaseReserved {
		peerSession.basicSequenceSession().Restore(previousBasicSequence, basicSequenceRevision)
		peerSession.restoreCampaignProjectileID(previousProjectileObjectID)
		peerSession.restorePlayerMotion(previousPlayerMotion, playerMotionRevision)
		r.registry.mutex.Unlock()
		return request.reject("release unavailable")
	}
	run := &campaignTossRun{}
	if peerSession.tossAttacks == nil {
		peerSession.tossAttacks = make(map[uint32]*campaignTossRun)
	}
	peerSession.tossAttacks[projectileObjectID] = run
	generation := peerSession.generation
	binding := peerSession.binding
	repeatDelay := peerSession.basicSequenceSession().CooldownEnd().Sub(abilityStartTime)
	peerSession.retainAttackPose(
		facing, targetPosition, tossPlan.TargetObjectID,
		abilityStartTime.Add(selection.ReleaseDelay),
	)
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	schedule := campaignTossSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: command.Common.ObjectID,
		projectileObjectID:         projectileObjectID,
		previousProjectileObjectID: previousProjectileObjectID,
		startTime:                  abilityStartTime, creature: creature, plan: tossPlan,
		binding: binding, run: run, previousSequence: previousBasicSequence,
		sequenceRevision:   basicSequenceRevision,
		previousMotion:     previousPlayerMotion,
		motionRevision:     playerMotionRevision,
		releaseReservation: releaseReservation,
		releaseResponse:    releaseResponsePacket,
	}
	launchProducer := raknet.ScheduledPacketProducer{
		Delay: selection.HitDelay, Produce: schedule.produceLaunch,
	}
	producers := []raknet.ScheduledPacketProducer{
		launchProducer,
		{
			Delay:   selection.ReleaseDelay,
			Produce: schedule.produceRelease,
		},
	}
	repeatPacket := packet
	repeatPacket.Payload = append([]byte(nil), packet.Payload...)
	repeatPacket.SourceTime += uint64(repeatDelay / time.Millisecond)
	if isBasicHeldInput {
		repeatStep := campaignHeldRepeatStep{
			runtime: r, sessionKey: sessionKey, generation: generation,
			heldGeneration: heldGeneration, packet: repeatPacket,
			command: command,
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: repeatDelay, Produce: repeatStep.produce,
		})
	}
	producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
	var cancel raknet.CancelSchedule
	if packet.ScheduleGroupResult != nil {
		cancel, err = packet.ScheduleGroupResult(producers, schedule.fail)
	} else {
		cancel, err = packet.ScheduleGroup(producers)
	}
	if err != nil {
		schedule.fail(err)
		schedule.rollbackAdmission()
		return nil, fmt.Errorf("campaignTossSchedule: %w", err)
	}
	run.cancel = cancel
	r.logger.Printf("RakNet campaign toss basic accepted source=%d target=%d object=%d ability=%q",
		command.Common.ObjectID, tossPlan.TargetObjectID, projectileObjectID,
		tossPlan.Definition.Name)
	responsePackets := append([][]byte{ackPacket}, movementPackets...)
	return append(responsePackets, startPackets...), nil
}

func (r campaignAbilityCommandRuntime) handleCloudLobBasic(
	ctx context.Context, request campaignCharacterAbilityRequest,
	peerSession gameplayPeerSession, creature game.GameplayCreature,
	definition sim.AbilityDefinition, targetObjectID uint32,
	isBasicHeldRepeat bool, isBasicHeldInput bool,
	sessionKey string, abilityStartTime time.Time,
) ([][]byte, error) {
	var err error
	packet := request.packet
	command := request.command
	cloudReq, planErr := campaignCloudLobAim(peerSession, creature, definition, command)
	if planErr != nil {
		r.registry.mutex.Unlock()
		return request.reject(planErr.Error())
	}
	cloudPlan, planErr := zoneability.PlanCloudLobAt(cloudReq)
	if planErr != nil {
		r.registry.mutex.Unlock()
		return request.reject(planErr.Error())
	}
	targetPosition := raknet.Vector3(cloudPlan.Destination)
	facing := geometryraknet.Direction(peerSession.playerPosition, targetPosition)
	posePackets, poseErr := marshalZonePlayerAttackPose(command.Common.ObjectID,
		peerSession.playerPosition, facing, targetPosition)
	if poseErr != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("cloudLobFacing: %w", poseErr)
	}
	previousBasicSequence := peerSession.basicSequenceSession().Snapshot()
	previousNextObjectID := peerSession.nextProjectileObjectID
	previousPlayerMotion := peerSession.playerMotionSnapshot()
	if !isBasicHeldRepeat {
		peerSession.basicSequenceSession().SetHeld(isBasicHeldInput)
	}
	heldGeneration := peerSession.basicSequenceSession().HeldGeneration()
	selection, selectionErr := peerSession.basicSequenceSession().Accept(
		abilityStartTime, cloudPlan.Definition,
	)
	if selectionErr != nil {
		peerSession.basicSequenceSession().Restore(
			previousBasicSequence,
			peerSession.basicSequenceSession().Revision(),
		)
		r.registry.mutex.Unlock()
		return request.reject(selectionErr.Error())
	}
	basicSequenceRevision := peerSession.basicSequenceSession().Revision()
	cloudPlan.Definition.AnimationName = selection.AnimationName
	cloudPlan.Definition.HitDelay = selection.HitDelay
	cloudPlan.Definition.ReleaseDelay = selection.ReleaseDelay
	projectileObjectID, err := peerSession.reserveCampaignProjectileIDs(2, 1000)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignCloudLobObjectID: %w", err)
	}
	cloudObjectID := projectileObjectID + 1
	err = peerSession.stopPlayerMovement(abilityStartTime)
	if err != nil {
		peerSession.basicSequenceSession().Restore(previousBasicSequence, basicSequenceRevision)
		peerSession.restoreCampaignProjectileID(previousNextObjectID)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignCloudLobStop: %w", err)
	}
	playerMotionRevision := peerSession.playerMotionRevision()
	ackPacket, marshalErr := abilityraknet.Acknowledge(
		abilityraknet.AcknowledgeRequest{
			SyncStamp:    command.Common.Unknown[0],
			ResponseType: raknet.ActionResponseAccepted,
			ObjectID:     cloudPlan.AbilityID, AbilityIndex: command.Ability.Index,
			SourceStartMilliseconds: packet.SourceTime,
			SourceCommitMilliseconds: packet.SourceTime +
				uint64(selection.HitDelay/time.Millisecond),
			SourceEndMilliseconds: packet.SourceTime +
				uint64(selection.ReleaseDelay/time.Millisecond),
		},
	)
	if marshalErr != nil {
		peerSession.basicSequenceSession().Restore(previousBasicSequence, basicSequenceRevision)
		peerSession.restoreCampaignProjectileID(previousNextObjectID)
		peerSession.restorePlayerMotion(previousPlayerMotion, playerMotionRevision)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignCloudLobAck: %w", marshalErr)
	}
	releaseResponsePacket, marshalErr := abilityraknet.ReleaseResponse(
		command.Common.Unknown[0], cloudPlan.AbilityID, command.Ability.Index,
		packet.SourceTime, selection.HitDelay, selection.ReleaseDelay,
	)
	if marshalErr != nil {
		peerSession.basicSequenceSession().Restore(previousBasicSequence, basicSequenceRevision)
		peerSession.restoreCampaignProjectileID(previousNextObjectID)
		peerSession.restorePlayerMotion(previousPlayerMotion, playerMotionRevision)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignCloudLobReleaseResponse: %w", marshalErr)
	}
	startPackets, marshalErr := abilityraknet.StartPresentation(
		command.Common.ObjectID, cloudPlan.AbilityID, selection.AnimationName,
		cloudPlan.Definition.Cooldown, packet.SourceTime, 0,
	)
	if marshalErr != nil {
		peerSession.basicSequenceSession().Restore(previousBasicSequence, basicSequenceRevision)
		peerSession.restoreCampaignProjectileID(previousNextObjectID)
		peerSession.restorePlayerMotion(previousPlayerMotion, playerMotionRevision)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("campaignCloudLobStart: %w", marshalErr)
	}
	releaseReservation, isReleaseReserved :=
		peerSession.abilityReleaseSession().Reserve(
			abilityStartTime, selection.ReleaseDelay,
		)
	if !isReleaseReserved {
		peerSession.basicSequenceSession().Restore(previousBasicSequence, basicSequenceRevision)
		peerSession.restoreCampaignProjectileID(previousNextObjectID)
		peerSession.restorePlayerMotion(previousPlayerMotion, playerMotionRevision)
		r.registry.mutex.Unlock()
		return request.reject("release unavailable")
	}
	run := &campaignCloudLobRun{}
	if peerSession.cloudLobAttacks == nil {
		peerSession.cloudLobAttacks = make(map[uint32]*campaignCloudLobRun)
	}
	peerSession.cloudLobAttacks[projectileObjectID] = run
	generation := peerSession.generation
	binding := peerSession.binding
	repeatDelay := peerSession.basicSequenceSession().CooldownEnd().Sub(abilityStartTime)
	peerSession.retainAttackPose(facing, targetPosition, cloudPlan.TargetObjectID,
		abilityStartTime.Add(selection.ReleaseDelay))
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	projectileIndex := selection.AnimationIndex % len(cloudPlan.Lob)
	landingDelay := selection.HitDelay + cloudPlan.Lob[projectileIndex].Duration
	schedule := campaignCloudLobSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: command.Common.ObjectID,
		previousNextObjectID: previousNextObjectID,
		projectileObjectID:   projectileObjectID,
		cloudObjectID:        cloudObjectID, projectileIndex: projectileIndex,
		landingDelay: landingDelay,
		creature:     creature, plan: cloudPlan, binding: binding, run: run,
		previousSequence:   previousBasicSequence,
		sequenceRevision:   basicSequenceRevision,
		previousMotion:     previousPlayerMotion,
		motionRevision:     playerMotionRevision,
		releaseReservation: releaseReservation,
		releaseResponse:    releaseResponsePacket,
	}
	launchProducer := raknet.ScheduledPacketProducer{
		Delay: selection.HitDelay, Produce: schedule.produceLaunch,
	}
	landingProducer := raknet.ScheduledPacketProducer{
		Delay: landingDelay, Produce: schedule.produceLanding,
	}
	producers := []raknet.ScheduledPacketProducer{
		launchProducer, landingProducer,
		{
			Delay: selection.ReleaseDelay, Produce: schedule.produceRelease,
		},
	}
	repeatPacket := packet
	repeatPacket.Payload = append([]byte(nil), packet.Payload...)
	repeatPacket.SourceTime += uint64(repeatDelay / time.Millisecond)
	if isBasicHeldInput {
		repeatStep := campaignHeldRepeatStep{
			runtime: r, sessionKey: sessionKey, generation: generation,
			heldGeneration: heldGeneration, packet: repeatPacket,
			command: command,
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: repeatDelay, Produce: repeatStep.produce,
		})
	}
	producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
	var cancel raknet.CancelSchedule
	if packet.ScheduleGroupResult != nil {
		cancel, err = packet.ScheduleGroupResult(producers, schedule.fail)
	} else {
		cancel, err = packet.ScheduleGroup(producers)
	}
	if err != nil {
		schedule.fail(err)
		schedule.rollbackAdmission()
		return nil, fmt.Errorf("campaignCloudLobSchedule: %w", err)
	}
	run.cancel = cancel
	r.logger.Printf("RakNet campaign cloud lob basic accepted source=%d target=%d ability=%q",
		command.Common.ObjectID, cloudPlan.TargetObjectID, cloudPlan.Definition.Name)
	responsePackets := append([][]byte{ackPacket}, posePackets...)
	return append(responsePackets, startPackets...), nil
}
