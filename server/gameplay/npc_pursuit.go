package gameplay

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const campaignPursuitFallbackTick = 50 * time.Millisecond
const campaignPursuitRedirectInterval = 250 * time.Millisecond
const campaignPursuitRedirectDistance = 0.75

type campaignNPCArrival func(uint64) ([][]byte, error)

type campaignNPCPursuitRuntime struct {
	registry *gameplaySessionRegistry
	logger   *log.Logger
	now      func() time.Time
}

func (r campaignNPCPursuitRuntime) advanceTargetPoseLocked(
	peerSession gameplayPeerSession, targetObjectID uint32,
) (gameplayPeerSession, error) {
	updated, err := r.advanceTargetPoseAtLocked(peerSession, targetObjectID, r.now())
	if err != nil {
		return gameplayPeerSession{}, fmt.Errorf("targetPose: %w", err)
	}
	return updated, nil
}

func (r campaignNPCPursuitRuntime) advanceTargetPoseAtLocked(
	peerSession gameplayPeerSession, targetObjectID uint32, now time.Time,
) (gameplayPeerSession, error) {
	if targetObjectID == 0 || peerSession.zone == nil {
		return peerSession, nil
	}
	if peerSession.deployedObjectID == targetObjectID {
		err := peerSession.advancePlayerPosition(now, raknet.Vector3{})
		if err != nil {
			return gameplayPeerSession{}, fmt.Errorf("enemyTargetAdvance: %w", err)
		}
		err = peerSession.syncZoneHeroPose()
		if err != nil {
			return gameplayPeerSession{}, fmt.Errorf("enemyTargetSync: %w", err)
		}
		return peerSession, nil
	}
	for targetSessionKey, targetSession := range r.registry.sessions {
		if targetSession.zone != peerSession.zone ||
			targetSession.deployedObjectID != targetObjectID {
			continue
		}
		err := targetSession.advancePlayerPosition(now, raknet.Vector3{})
		if err != nil {
			return gameplayPeerSession{}, fmt.Errorf("enemyRemoteTargetAdvance: %w", err)
		}
		err = targetSession.syncZoneHeroPose()
		if err != nil {
			return gameplayPeerSession{}, fmt.Errorf("enemyRemoteTargetSync: %w", err)
		}
		r.registry.sessions[targetSessionKey] = targetSession
		break
	}
	return peerSession, nil
}

type campaignNPCStunResumeStep struct {
	timestamp uint64
	resume    campaignNPCArrival
}

func (s campaignNPCStunResumeStep) produce() ([][]byte, error) {
	return s.resume(s.timestamp)
}

type campaignNPCPursuitStep struct {
	runtime                  campaignNPCPursuitRuntime
	packet                   raknet.Packet
	sessionKey               string
	generation               uint64
	actionGeneration         uint64
	objectID                 uint32
	targetObjectID           uint32
	timestamp                uint64
	movementAt               time.Time
	publishedGoal            game.Vec3
	lastPublicationTimestamp uint64
	profile                  zonenpc.ActionProfile
	onArrival                campaignNPCArrival
	isMovementPaused         bool
	isCorridorActive         bool
}

func (s campaignNPCPursuitStep) produce() ([][]byte, error) {
	return s.runtime.produceStep(
		s.packet, s.sessionKey, s.generation, s.actionGeneration,
		s.objectID, s.timestamp,
		s.targetObjectID, s.publishedGoal, s.lastPublicationTimestamp,
		s.profile, s.onArrival, s.movementAt, s.isMovementPaused,
		s.isCorridorActive,
	)
}

func (r campaignNPCPursuitRuntime) deferAction(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64, resume campaignNPCArrival,
) (bool, error) {
	if resume == nil {
		return false, errors.New("enemy stun resume unavailable")
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone.NPCs() != nil
	remaining := time.Duration(0)
	if isCurrent {
		remaining = max(
			peerSession.zone.NPCs().StunRemaining(objectID, r.now()),
			peerSession.zone.NPCs().SleepRemaining(objectID, r.now()),
			peerSession.zone.NPCs().SilenceRemaining(objectID, r.now()),
		)
	}
	r.registry.mutex.RUnlock()
	if remaining <= 0 {
		return false, nil
	}
	nextTimestamp := timestamp + uint64(remaining/time.Millisecond)
	step := campaignNPCStunResumeStep{
		timestamp: nextTimestamp, resume: resume,
	}
	producer := raknet.ScheduledPacketProducer{
		Delay:   remaining,
		Produce: step.produce,
	}
	producers := r.registry.producerGuard.scheduledProducers(
		sessionKey, []raknet.ScheduledPacketProducer{producer},
	)
	cancel, err := packet.ScheduleProducers(producers)
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		return false, fmt.Errorf("enemyStunResumeSchedule: %w", err)
	}
	return true, nil
}

func (r campaignNPCPursuitRuntime) deferCommit(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, resume func() ([][]byte, error),
) (bool, error) {
	if resume == nil {
		return false, errors.New("enemy commit resume unavailable")
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	remaining := time.Duration(0)
	if isCurrent {
		remaining = max(
			peerSession.zone.NPCs().StunRemaining(objectID, r.now()),
			peerSession.zone.NPCs().SleepRemaining(objectID, r.now()),
		)
	}
	r.registry.mutex.RUnlock()
	if remaining <= 0 {
		return false, nil
	}
	producers := r.registry.producerGuard.scheduledProducers(
		sessionKey, []raknet.ScheduledPacketProducer{{
			Delay: remaining, Produce: resume,
		}},
	)
	cancel, err := packet.ScheduleProducers(producers)
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		return false, fmt.Errorf("enemyCommitResumeSchedule: %w", err)
	}
	return true, nil
}

func (r campaignNPCPursuitRuntime) schedule(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64, publishedGoal game.Vec3,
	profile zonenpc.ActionProfile, onArrival campaignNPCArrival,
) error {
	return r.scheduleCorrection(
		packet, sessionKey, generation, objectID, timestamp, publishedGoal,
		timestamp, profile, onArrival,
	)
}

func (r campaignNPCPursuitRuntime) scheduleTarget(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, targetObjectID uint32, timestamp uint64,
	publishedGoal game.Vec3, profile zonenpc.ActionProfile,
	onArrival campaignNPCArrival,
) error {
	return r.scheduleTargetCorrection(
		packet, sessionKey, generation, objectID, targetObjectID, timestamp,
		publishedGoal, timestamp, profile, onArrival,
	)
}

func (r campaignNPCPursuitRuntime) scheduleCorrection(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64, publishedGoal game.Vec3,
	lastPublicationTimestamp uint64,
	profile zonenpc.ActionProfile,
	onArrival campaignNPCArrival,
) error {
	return r.scheduleTargetCorrection(
		packet, sessionKey, generation, objectID, 0, timestamp,
		publishedGoal, lastPublicationTimestamp,
		profile, onArrival,
	)
}

func (r campaignNPCPursuitRuntime) scheduleTargetCorrection(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, targetObjectID uint32, timestamp uint64,
	publishedGoal game.Vec3, lastPublicationTimestamp uint64,
	profile zonenpc.ActionProfile, onArrival campaignNPCArrival,
) error {
	if r.registry == nil || r.now == nil {
		return errors.New("enemy pursuit runtime unavailable")
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	actionGeneration := uint64(0)
	if isCurrent {
		npc, isNPCFound := peerSession.zone.NPCs().NPC(objectID)
		if isNPCFound && npc.IsActionStarted {
			actionGeneration = npc.ActionGeneration
		}
	}
	r.registry.mutex.RUnlock()
	if actionGeneration == 0 {
		return errors.New("enemy pursuit action unavailable")
	}
	return r.scheduleActionCorrection(
		packet, sessionKey, generation, actionGeneration, objectID,
		targetObjectID, timestamp, publishedGoal, lastPublicationTimestamp,
		profile, onArrival, r.now(), false, false,
	)
}

func (r campaignNPCPursuitRuntime) scheduleActionCorrection(
	packet raknet.Packet, sessionKey string, generation uint64,
	actionGeneration uint64, objectID uint32, targetObjectID uint32,
	timestamp uint64, publishedGoal game.Vec3, lastPublicationTimestamp uint64,
	profile zonenpc.ActionProfile, onArrival campaignNPCArrival, movementAt time.Time,
	isMovementPaused bool, isCorridorActive bool,
) error {
	if onArrival == nil {
		return errors.New("enemy pursuit arrival unavailable")
	}
	step := campaignNPCPursuitStep{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, actionGeneration: actionGeneration,
		objectID: objectID, timestamp: timestamp, movementAt: movementAt,
		targetObjectID:           targetObjectID,
		publishedGoal:            publishedGoal,
		lastPublicationTimestamp: lastPublicationTimestamp,
		profile:                  profile,
		onArrival:                onArrival,
		isMovementPaused:         isMovementPaused,
		isCorridorActive:         isCorridorActive,
	}
	producer := raknet.ScheduledPacketProducer{

		Delay:   campaignPursuitFallbackTick,
		Produce: step.produce,
	}
	producers := r.registry.producerGuard.scheduledProducers(
		sessionKey, []raknet.ScheduledPacketProducer{producer},
	)
	cancel, err := packet.ScheduleProducers(producers)
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		return fmt.Errorf("enemyPursuitSchedule: %w", err)
	}
	return nil
}

func (r campaignNPCPursuitRuntime) produceStep(
	packet raknet.Packet, sessionKey string, generation uint64,
	actionGeneration uint64,
	objectID uint32, timestamp uint64, targetObjectID uint32, publishedGoal game.Vec3,
	lastPublicationTimestamp uint64,
	profile zonenpc.ActionProfile,
	onArrival campaignNPCArrival, movementAt time.Time,
	isMovementPaused bool, isCorridorActive bool,
) ([][]byte, error) {
	r.registry.mutex.Lock()
	advancedAt := r.now()
	elapsed := max(time.Duration(0), advancedAt.Sub(movementAt))
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		!peerSession.isZoneTerminal() && peerSession.zone.NPCs() != nil &&
		peerSession.squad != nil &&
		(peerSession.zone.Boss() == nil ||
			!peerSession.zone.Boss().IsBeamOutCommitted())
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	npcSession := peerSession.zone.NPCs()
	enemy, isEnemyFound := npcSession.NPC(objectID)
	if !isEnemyFound || enemy.IsDefeated {
		if isEnemyFound {
			npcSession.ReleaseActionGeneration(
				objectID, enemy.ActionOwner, actionGeneration,
			)
		}
		r.registry.mutex.Unlock()
		return nil, nil
	}
	owner := zonenpc.ActionOwner{
		UserID: peerSession.binding.UserID, PeerGeneration: generation,
	}
	if !enemy.IsActionStarted || enemy.ActionOwner != owner ||
		enemy.ActionGeneration != actionGeneration {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	if enemy.IsTurtleActive {
		// The turtle modifier resumes the owned action after recovery. Retire
		// this pursuit producer so its old goal cannot move the closed shell.
		r.registry.mutex.Unlock()
		return nil, nil
	}
	if profile.AbilityName != "Flee" &&
		npcSession.FearRemaining(objectID, r.now()) > 0 {
		r.registry.mutex.Unlock()
		nextTimestamp := timestamp +
			uint64(elapsed/time.Millisecond)
		err := r.scheduleActionCorrection(
			packet, sessionKey, generation, actionGeneration, objectID,
			targetObjectID, nextTimestamp,
			publishedGoal, lastPublicationTimestamp,
			profile, onArrival, advancedAt, isMovementPaused,
			isCorridorActive,
		)
		if err != nil {
			npcSession.ReleaseActionGeneration(objectID, owner, actionGeneration)
			return nil, fmt.Errorf("enemyPursuitFearReschedule: %w", err)
		}
		return nil, nil
	}
	nounProfile, isProfileFound := zonenpc.ActionProfileForPlan(enemy.Plan)
	isSecondaryFamily := isCampaignNPCSecondaryPursuit(profile.AbilityName) || profile.AbilityName == "Repair"
	if !isProfileFound ||
		(nounProfile.Family != profile.Family && !isSecondaryFamily) {
		npcSession.ReleaseActionGeneration(objectID, owner, actionGeneration)
		r.registry.mutex.Unlock()
		return nil, nil
	}
	// Ordinary pursuit must observe aura entry/exit while a path is already
	// running. Secondary actions retain their scripted charge/leap speed.
	if !isSecondaryFamily && profile.Family != zonenpc.ActionCharge {
		profile.MovementSpeed = nounProfile.MovementSpeed
		profile.NonCombatMovementSpeed = nounProfile.NonCombatMovementSpeed
	}
	resolvedTargetObjectID := enemy.TargetObjectID
	targetPosition := game.Vec3{}
	isTargetFound := false
	if targetObjectID != 0 {
		targetNPC, isNPCFound := npcSession.NPC(targetObjectID)
		isTargetFound = isNPCFound && !targetNPC.IsDefeated &&
			targetNPC.IsPublished && targetNPC.HitPoint > 0
		if profile.AbilityName == "Repair" {
			isTargetFound = isNPCFound && targetNPC.IsDefeated && targetNPC.IsPublished
		}
		if isTargetFound {
			resolvedTargetObjectID = targetObjectID
			targetPosition = targetNPC.Plan.Position
		}
	} else {
		var poseErr error
		peerSession, poseErr = r.advanceTargetPoseLocked(
			peerSession, enemy.TargetObjectID,
		)
		if poseErr != nil {
			npcSession.ReleaseActionGeneration(objectID, owner, actionGeneration)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemyPursuitTargetPose: %w", poseErr)
		}
		target, isCampaignTargetFound := peerSession.campaignNPCTarget(
			generation, enemy.TargetObjectID,
		)
		isTargetFound = isCampaignTargetFound && target.HitPoint > 0
		if isTargetFound {
			targetPosition = target.Position
		}
	}
	if !isTargetFound {
		restorePackets, restoreErr := chargeCancelSpeed(objectID, enemy, profile)
		npcSession.ReleaseActionGeneration(objectID, owner, actionGeneration)
		r.registry.mutex.Unlock()
		if restoreErr != nil {
			return nil, fmt.Errorf("enemyPursuitCancelSpeed: %w", restoreErr)
		}
		if profile.AbilityName == "NomadSpecialOneCharge" ||
			profile.AbilityName == "VerdanthBasicPicky" {
			stopPackets, stopErr := npcraknet.StopAtPose(
				objectID, enemy.Plan.Position, enemy.Facing,
			)
			if stopErr != nil {
				return nil, fmt.Errorf("enemyPursuitCancelStop: %w", stopErr)
			}
			return append(stopPackets, restorePackets...), nil
		}
		return nil, nil
	}
	facingTargetPosition := targetPosition
	// GhostlyBoltFlee is the Rezzer's fixed retreat point, not a chase of
	// the hero retained as its combat target.
	if profile.AbilityName == "StealthAttack" || profile.AbilityName == "Flee" ||
		profile.AbilityName == "GhostlyBoltFlee" ||
		profile.AbilityName == "NoctGhostCharge" ||
		profile.AbilityName == "NocturnaSpecialDriftCharge" {
		targetPosition = publishedGoal
	}
	movementSpeed := graspingDeadMovementSpeed(
		peerSession, enemy.Plan.Position, profile.MovementSpeed,
	)
	slowMovementScale := npcSession.SlowMovementScale(
		objectID, r.now(),
	)
	movementSpeed *= slowMovementScale
	// ModifierCreated only presents the debuff. Native locomotion reads
	// attribute 48, so mirror the authoritative slow and restore it on expiry.
	// Preserve the noun's base combat speed and its permanent movement bonus.
	if profile.Family != zonenpc.ActionCharge {
		movementBuff := (1+zonenpc.EffectiveMovementSpeedBuff(enemy.Plan))*slowMovementScale - 1
		slowPacket, slowErr := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
			ObjectID: objectID, Value: map[uint8]float32{48: movementBuff},
		})
		if slowErr != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("pursuitSlow: %w", slowErr)
		}
		if npcSession.CommitSlowMovementPublication(objectID, slowMovementScale) {
			r.registry.queueChannelPresentationLocked(peerSession.zone, [][]byte{slowPacket})
			peerSession = r.registry.sessions[sessionKey]
		}
	}
	isMovementDisabled := movementSpeed <= 0 ||
		(enemy.IsShieldActive && profile.Family == zonenpc.ActionNomadDrone) ||
		peerSession.campaignNPCShielderShieldSetups[objectID] ||
		peerSession.campaignNPCShielderShields[objectID] != nil ||
		npcSession.StunRemaining(objectID, advancedAt) > 0 ||
		npcSession.SleepRemaining(objectID, advancedAt) > 0 ||
		npcSession.RootRemaining(objectID, advancedAt) > 0
	if isMovementDisabled {
		// Modifiers do not cancel the client's already-issued locomotion goal.
		// Stop once at the retained server pose, then keep polling so early
		// dispels and overlapping holds use the current authoritative status.
		var pausePackets [][]byte
		if !isMovementPaused {
			var pauseErr error
			pausePackets, pauseErr = npcraknet.MovementPause(
				objectID, enemy.Plan.Position, enemy.Facing,
			)
			if pauseErr != nil {
				npcSession.ReleaseActionGeneration(objectID, owner, actionGeneration)
				r.registry.mutex.Unlock()
				return nil, fmt.Errorf("enemyPursuitPause: %w", pauseErr)
			}
		}
		r.registry.mutex.Unlock()
		nextTimestamp := timestamp +
			uint64(elapsed/time.Millisecond)
		err := r.scheduleActionCorrection(
			packet, sessionKey, generation, actionGeneration, objectID,
			targetObjectID, nextTimestamp, publishedGoal,
			lastPublicationTimestamp, profile, onArrival, advancedAt, true,
			false,
		)
		if err != nil {
			npcSession.ReleaseActionGeneration(objectID, owner, actionGeneration)
			return nil, fmt.Errorf("enemyPursuitPauseReschedule: %w", err)
		}
		return pausePackets, nil
	}
	step, err := npcSession.AdvancePursuit(
		peerSession.zone.Navigation(), objectID, targetPosition, profile.Range,
		movementSpeed, enemy.Plan.ActorFootprintRadius(),
		elapsed,
	)
	if err != nil {
		npcSession.ReleaseActionGeneration(objectID, owner, actionGeneration)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyPursuitAdvance: %w", err)
	}
	if step.IsBlocked {
		restorePackets, restoreErr := chargeCancelSpeed(objectID, enemy, profile)
		npcSession.ReleaseActionGeneration(objectID, owner, actionGeneration)
		r.registry.sessions[sessionKey] = peerSession
		r.registry.mutex.Unlock()
		if restoreErr != nil {
			return nil, fmt.Errorf("enemyBlockedSpeed: %w", restoreErr)
		}
		if r.logger != nil {
			r.logger.Printf("RakNet NPC pursuit blocked object=%d noun=%q target=%d reason=%q",
				objectID, enemy.Plan.NounName, resolvedTargetObjectID, step.NavigationFallbackReason)
		}
		packets, stopErr := npcraknet.StopAtPose(objectID, step.Position, enemy.Facing)
		if stopErr != nil {
			return nil, fmt.Errorf("enemyBlockedStop: %w", stopErr)
		}
		return append(packets, restorePackets...), nil
	}
	// Ordinary pursuit publishes 0x41 with a live target ID. Projectile
	// pursuit, fleeing, and pass-through charges retain movement-derived heading.
	if profile.Family != zonenpc.ActionProjectile && profile.AbilityName != "Flee" &&
		profile.AbilityName != "NocturnaSpecialDriftCharge" && profile.AbilityName != "NoctGhostCharge" {
		isFacingCommitted := npcSession.CommitFacing(zonenpc.AttackPlan{
			SourceObjectID: objectID, TargetObjectID: resolvedTargetObjectID,
			ActionGeneration: actionGeneration, SourcePosition: step.Position,
			TargetPosition: facingTargetPosition,
		})
		if !isFacingCommitted {
			r.registry.mutex.Unlock()
			return nil, nil
		}
	}
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	nextTimestamp := timestamp +
		uint64(elapsed/time.Millisecond)
	// Enter fixed-point corridor movement once, then update its goal without
	// resetting native locomotion on every waypoint. Native 539810 replaces
	// both goal vectors while preserving the mode installed by 5393D0.
	stepPackets := make([][]byte, 0, 4)
	isCorridorPursuit := step.IsWaypointKnown &&
		(!isSecondaryFamily || profile.AbilityName == "RezMelee") &&
		profile.Family != zonenpc.ActionCharge && profile.AbilityName != "StealthAttack" &&
		profile.AbilityName != "Flee"
	isWaypointChanged := zonegeometry.DistanceSquared(publishedGoal, step.Waypoint) > 0.0001
	if isCorridorPursuit && !step.IsInRange &&
		(!isCorridorActive || isMovementPaused || isWaypointChanged) {
		if !isCorridorActive || isMovementPaused {
			waypointPackets, waypointErr := npcraknet.PursuitWaypoint(objectID, step.Waypoint)
			if waypointErr != nil {
				npcSession.ReleaseActionGeneration(objectID, owner, actionGeneration)
				return nil, fmt.Errorf("enemyWaypointStart: %w", waypointErr)
			}
			stepPackets = append(stepPackets, waypointPackets...)
			isCorridorActive = true
		} else {
			waypointPacket, waypointErr := npcraknet.MovementGoalUpdate(objectID, step.Waypoint)
			if waypointErr != nil {
				npcSession.ReleaseActionGeneration(objectID, owner, actionGeneration)
				return nil, fmt.Errorf("enemyWaypointUpdate: %w", waypointErr)
			}
			stepPackets = append(stepPackets, waypointPacket)
		}
		publishedGoal = step.Waypoint
		lastPublicationTimestamp = timestamp
	}
	if !isCorridorPursuit && isMovementPaused && !step.IsInRange {
		// A goal-only update cannot restart the stopped native mover. Reissue
		// the complete pursuit command even if the target has not moved.
		resumePackets, resumeErr := npcraknet.Pursuit(zonenpc.FirstActionPlan{
			ObjectID: objectID, TargetObjectID: resolvedTargetObjectID,
			SourcePosition: step.Position, TargetPosition: targetPosition,
			Profile: profile, IsPursuitNeeded: true,
		})
		if resumeErr != nil {
			npcSession.ReleaseActionGeneration(objectID, owner, actionGeneration)
			return nil, fmt.Errorf("enemyPursuitResume: %w", resumeErr)
		}
		stepPackets = append(stepPackets, resumePackets...)
		publishedGoal = targetPosition
		lastPublicationTimestamp = timestamp
		isCorridorActive = false
	}
	isStalkerLeap := profile.AbilityName == "NomadBioSpecialTwoJumpAttack" &&
		profile.MovementSpeed == profile.ForcedMovementSpeed
	if isStalkerLeap {
		positionPacket, positionErr := npcraknet.LeapPosition(
			objectID, step.Position, targetPosition,
		)
		if positionErr != nil {
			npcSession.ReleaseActionGeneration(objectID, owner, actionGeneration)
			return nil, fmt.Errorf("stalkerLeapPosition: %w", positionErr)
		}
		stepPackets = append(stepPackets, positionPacket)
	}
	sincePublication := timestamp - lastPublicationTimestamp
	redirectInterval := uint64(campaignPursuitRedirectInterval / time.Millisecond)
	isGoalChanged := zonegeometry.DistanceSquared(
		publishedGoal, targetPosition,
	) >= campaignPursuitRedirectDistance*campaignPursuitRedirectDistance
	isRedirectDue := !isCorridorPursuit && !isStalkerLeap && isGoalChanged && sincePublication >= redirectInterval
	if step.IsNavigationFallback && isRedirectDue && r.logger != nil {
		r.logger.Printf(
			"RakNet NPC navigation fallback object=%d target=%d position=(%g,%g,%g) goal=(%g,%g,%g) reason=%q",
			objectID, resolvedTargetObjectID,
			step.Position.X, step.Position.Y, step.Position.Z,
			targetPosition.X, targetPosition.Y, targetPosition.Z,
			step.NavigationFallbackReason,
		)
	}
	if isRedirectDue {
		var redirectPackets [][]byte
		var redirectErr error
		if profile.AbilityName == "BoomerCharge" {
			// The charge already installed its target and stopping distance.
			// Native 5393D0/539900 reset mover progress on every complete
			// command/reflection. Preserve that progress while following a
			// moving hero: 539810 updates both goals without resetting it.
			var goalPacket []byte
			goalPacket, redirectErr = npcraknet.MovementGoalUpdate(objectID, targetPosition)
			if redirectErr == nil {
				redirectPackets = append(redirectPackets, goalPacket)
			}
		} else if profile.Family == zonenpc.ActionProjectile {
			redirectPackets, redirectErr = npcraknet.ProjectilePursuitRedirect(
				objectID, step.Position, resolvedTargetObjectID, targetPosition,
				profile.Range,
			)
		} else {
			redirectPackets, redirectErr = npcraknet.PursuitRedirect(
				objectID, step.Position, resolvedTargetObjectID, targetPosition,
				profile.Range,
			)
		}
		if redirectErr != nil {
			npcSession.ReleaseActionGeneration(objectID, owner, actionGeneration)
			return nil, fmt.Errorf("enemyPursuitRedirect: %w", redirectErr)
		}
		stepPackets = append(stepPackets, redirectPackets...)
		publishedGoal = targetPosition
		lastPublicationTimestamp = timestamp
		isCorridorActive = false
	}
	if step.IsInRange {
		stopPackets, stopErr := npcraknet.StopAtPose(objectID, step.Position, enemy.Facing)
		if stopErr != nil {
			npcSession.ReleaseActionGeneration(objectID, owner, actionGeneration)
			return nil, fmt.Errorf("enemyPursuitStop: %w", stopErr)
		}
		attackPackets, attackErr := onArrival(nextTimestamp)
		if attackErr != nil {
			npcSession.ReleaseActionGeneration(objectID, owner, actionGeneration)
			return nil, fmt.Errorf("enemyPursuitAttack: %w", attackErr)
		}
		// Peer publication uses these exact arrival and cast packets.
		stepPackets = append(stepPackets, stopPackets...)
		return append(stepPackets, attackPackets...), nil
	}
	err = r.scheduleActionCorrection(
		packet, sessionKey, generation, actionGeneration, objectID,
		targetObjectID, nextTimestamp,
		publishedGoal, lastPublicationTimestamp,
		profile, onArrival, advancedAt, false, isCorridorActive,
	)
	if err != nil {
		npcSession.ReleaseActionGeneration(objectID, owner, actionGeneration)
		return nil, fmt.Errorf("enemyPursuitReschedule: %w", err)
	}
	return stepPackets, nil
}
