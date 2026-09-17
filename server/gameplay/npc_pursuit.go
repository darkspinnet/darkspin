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
}

func (s campaignNPCPursuitStep) produce() ([][]byte, error) {
	return s.runtime.produceStep(
		s.packet, s.sessionKey, s.generation, s.actionGeneration,
		s.objectID, s.timestamp,
		s.targetObjectID, s.publishedGoal, s.lastPublicationTimestamp,
		s.profile, s.onArrival, s.movementAt,
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
	_, err := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{producer})
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
	cancel, err := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: remaining, Produce: resume,
	}})
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
		profile, onArrival, r.now(),
	)
}

func (r campaignNPCPursuitRuntime) scheduleActionCorrection(
	packet raknet.Packet, sessionKey string, generation uint64,
	actionGeneration uint64, objectID uint32, targetObjectID uint32,
	timestamp uint64, publishedGoal game.Vec3, lastPublicationTimestamp uint64,
	profile zonenpc.ActionProfile, onArrival campaignNPCArrival, movementAt time.Time,
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
		profile:                  profile, onArrival: onArrival,
	}
	producer := raknet.ScheduledPacketProducer{

		Delay:   campaignPursuitFallbackTick,
		Produce: step.produce,
	}
	cancel, err := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{producer})
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
	if profile.AbilityName != "Flee" &&
		npcSession.FearRemaining(objectID, r.now()) > 0 {
		r.registry.mutex.Unlock()
		nextTimestamp := timestamp +
			uint64(elapsed/time.Millisecond)
		err := r.scheduleActionCorrection(
			packet, sessionKey, generation, actionGeneration, objectID,
			targetObjectID, nextTimestamp,
			publishedGoal, lastPublicationTimestamp,
			profile, onArrival, advancedAt,
		)
		if err != nil {
			npcSession.ReleaseActionGeneration(objectID, owner, actionGeneration)
			return nil, fmt.Errorf("enemyPursuitFearReschedule: %w", err)
		}
		return nil, nil
	}
	nounProfile, isProfileFound := zonenpc.ActionProfileForPlan(enemy.Plan)
	isSecondaryFamily := isCampaignNPCSecondaryPursuit(profile.AbilityName)
	if !isProfileFound ||
		(nounProfile.Family != profile.Family && !isSecondaryFamily) {
		npcSession.ReleaseActionGeneration(objectID, owner, actionGeneration)
		r.registry.mutex.Unlock()
		return nil, nil
	}
	resolvedTargetObjectID := enemy.TargetObjectID
	targetPosition := game.Vec3{}
	isTargetFound := false
	if targetObjectID != 0 {
		targetNPC, isNPCFound := npcSession.NPC(targetObjectID)
		isTargetFound = isNPCFound && !targetNPC.IsDefeated &&
			targetNPC.IsPublished && targetNPC.HitPoint > 0
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
		npcSession.ReleaseActionGeneration(objectID, owner, actionGeneration)
		r.registry.mutex.Unlock()
		return nil, nil
	}
	if npcSession.StunRemaining(objectID, r.now()) > 0 {
		r.registry.mutex.Unlock()
		nextTimestamp := timestamp +
			uint64(elapsed/time.Millisecond)
		err := r.scheduleActionCorrection(
			packet, sessionKey, generation, actionGeneration, objectID,
			targetObjectID, nextTimestamp,
			publishedGoal, lastPublicationTimestamp,
			profile, onArrival, advancedAt,
		)
		if err != nil {
			npcSession.ReleaseActionGeneration(objectID, owner, actionGeneration)
			return nil, fmt.Errorf("enemyPursuitStunReschedule: %w", err)
		}
		return nil, nil
	}
	if npcSession.SleepRemaining(objectID, r.now()) > 0 {
		r.registry.mutex.Unlock()
		nextTimestamp := timestamp +
			uint64(elapsed/time.Millisecond)
		err := r.scheduleActionCorrection(
			packet, sessionKey, generation, actionGeneration, objectID,
			targetObjectID, nextTimestamp,
			publishedGoal, lastPublicationTimestamp,
			profile, onArrival, advancedAt,
		)
		if err != nil {
			npcSession.ReleaseActionGeneration(objectID, owner, actionGeneration)
			return nil, fmt.Errorf("enemyPursuitSleepReschedule: %w", err)
		}
		return nil, nil
	}
	if npcSession.RootRemaining(objectID, r.now()) > 0 {
		r.registry.mutex.Unlock()
		nextTimestamp := timestamp +
			uint64(elapsed/time.Millisecond)
		err := r.scheduleActionCorrection(
			packet, sessionKey, generation, actionGeneration, objectID,
			targetObjectID, nextTimestamp,
			publishedGoal, lastPublicationTimestamp,
			profile, onArrival, advancedAt,
		)
		if err != nil {
			npcSession.ReleaseActionGeneration(objectID, owner, actionGeneration)
			return nil, fmt.Errorf("enemyPursuitRootReschedule: %w", err)
		}
		return nil, nil
	}
	facingTargetPosition := targetPosition
	if profile.AbilityName == "StealthAttack" || profile.AbilityName == "Flee" ||
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
	if movementSpeed <= 0 {
		r.registry.mutex.Unlock()
		nextTimestamp := timestamp +
			uint64(elapsed/time.Millisecond)
		err := r.scheduleActionCorrection(
			packet, sessionKey, generation, actionGeneration, objectID,
			targetObjectID, nextTimestamp, publishedGoal,
			lastPublicationTimestamp, profile, onArrival, advancedAt,
		)
		if err != nil {
			npcSession.ReleaseActionGeneration(objectID, owner, actionGeneration)
			return nil, fmt.Errorf("enemyPursuitSlowReschedule: %w", err)
		}
		return nil, nil
	}
	step, err := npcSession.AdvancePursuit(
		peerSession.zone.Navigation(), objectID, targetPosition, profile.Range,
		movementSpeed, enemy.Plan.NPCProfile.FootprintRadius,
		elapsed,
	)
	if err != nil {
		npcSession.ReleaseActionGeneration(objectID, owner, actionGeneration)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyPursuitAdvance: %w", err)
	}
	// Ordinary pursuit publishes 0x41 with a live target ID. Projectile
	// pursuit and fleeing publish 0x01 and retain movement-derived heading.
	if profile.Family != zonenpc.ActionProjectile && profile.AbilityName != "Flee" {
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
	campaignZone := peerSession.zone
	sourceUserID := peerSession.binding.UserID
	r.registry.mutex.Unlock()

	nextTimestamp := timestamp +
		uint64(elapsed/time.Millisecond)
	// The client owns interpolation after the pursuit generation's complete
	// locomotion command. Later moving-target changes update only its goal;
	// replaying the command resets transient locomotion state, while publishing
	// authoritative current positions creates a second competing destination.
	stepPackets := make([][]byte, 0, 4)
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
	projectionPlan := zonenpc.FirstActionPlan{
		ObjectID: objectID, TargetObjectID: resolvedTargetObjectID,
		ActionGeneration: actionGeneration,
		SourcePosition:   step.Position, TargetPosition: targetPosition,
		Profile: profile,
	}
	sincePublication := timestamp - lastPublicationTimestamp
	redirectInterval := uint64(campaignPursuitRedirectInterval / time.Millisecond)
	isGoalChanged := zonegeometry.DistanceSquared(
		publishedGoal, targetPosition,
	) >= campaignPursuitRedirectDistance*campaignPursuitRedirectDistance
	isRedirectDue := !isStalkerLeap && isGoalChanged && sincePublication >= redirectInterval
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
		var goalPacket []byte
		var goalErr error
		if profile.Family == zonenpc.ActionProjectile {
			goalPacket, goalErr = npcraknet.ProjectileGoalUpdate(
				objectID, step.Position, targetPosition, profile.Range,
			)
		} else {
			goalPacket, goalErr = npcraknet.MovementGoalUpdate(
				objectID, targetPosition,
			)
		}
		if goalErr != nil {
			npcSession.ReleaseActionGeneration(objectID, owner, actionGeneration)
			return nil, fmt.Errorf("enemyPursuitGoal: %w", goalErr)
		}
		stepPackets = append(stepPackets, goalPacket)
		campaignZone.PublishNPCAction(zonenpc.ActionEvent{
			Kind: zonenpc.ActionEventPursuitRedirect,
			Plan: projectionPlan, Timestamp: nextTimestamp,
		}, sourceUserID, generation)
		publishedGoal = targetPosition
		lastPublicationTimestamp = timestamp
	}
	if step.IsInRange {
		stopPackets, stopErr := npcraknet.MovementStop(objectID, step.Position)
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
		profile, onArrival, advancedAt,
	)
	if err != nil {
		npcSession.ReleaseActionGeneration(objectID, owner, actionGeneration)
		return nil, fmt.Errorf("enemyPursuitReschedule: %w", err)
	}
	return stepPackets, nil
}
