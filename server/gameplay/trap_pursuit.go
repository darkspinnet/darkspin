package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
)

// The caller holds the registry lock; every return releases it.
func (e campaignAbilityCommandRuntime) pursueTrapLocked(
	req campaignCharacterAbilityRequest, peerSession gameplayPeerSession,
	sessionKey string, position raknet.Vector3, castRange float32,
) ([][]byte, error) {
	if peerSession.isEnemyRootActive(req.startTime) {
		e.registry.mutex.Unlock()
		return req.reject("cannot pursue trap placement while rooted")
	}
	command := req.command
	ability := *command.Ability
	command.Ability = &ability
	command.Ability.TargetID = 0
	command.Ability.TargetPosition = position
	command.Ability.CursorPosition = position
	packets, err := actionraknet.PursuitTransfer(
		command.Common.Unknown[0], command.Common.ObjectID,
	)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("trapTransfer: %w", err)
	}
	pursuit := peerSession.campaignPlayerPursuitSession().Snapshot()
	if pursuit.IsActive && pursuit.IsGround &&
		pursuit.SourceObjectID == command.Common.ObjectID &&
		pursuit.AbilityIndex == command.Ability.Index &&
		pursuit.GroundPosition.Sub(game.Vec3(position)).Length() < 0.1 {
		e.registry.sessions[sessionKey] = peerSession
		e.registry.mutex.Unlock()
		return packets, nil
	}
	// Walk toward the actual point, stopping just inside the authored cast
	// range. A ground cast must not use the target-object follow flag.
	movePacket, err := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{
		ObjectID: command.Common.ObjectID, GoalFlags: 0x01,
		GoalPosition: position, DesiredStopDistance: max(float32(0.1), castRange-0.25),
	})
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("trapMove: %w", err)
	}
	previousMotion := peerSession.playerMotionSnapshot()
	previousPosition, playerPosition, err := peerSession.advancePlayerPursuitMovement(
		req.startTime, peerSession.playerPosition, position,
		e.registry.passiveMovementIncrease(peerSession),
	)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("trapPath: %w", err)
	}
	// Motion owns path projection; reject an unreachable cast point before
	// leaving a pursuit that can never finish.
	if !isInsideZoneTrigger(peerSession.playerMovementGoal, position, castRange) {
		isRestored := peerSession.restorePlayerMotion(previousMotion, peerSession.playerMotionRevision())
		e.registry.mutex.Unlock()
		if !isRestored {
			return nil, errors.New("trap movement rollback unavailable")
		}
		return req.reject("trap destination unreachable")
	}
	command.Common.Position = playerPosition
	stopDistance := max(float32(0.1), castRange-0.25)
	generation := peerSession.campaignPlayerPursuitSession().BeginGround(
		command.Common.ObjectID, command.Ability.Index, command.Common.Unknown[0],
		game.Vec3(position), stopDistance,
	)
	e.registry.sessions[sessionKey] = peerSession
	e.registry.mutex.Unlock()
	progress := campaignPursuitProgressProducer{
		runtime: e, packet: req.packet, sessionKey: sessionKey,
		sessionGeneration:   peerSession.generation,
		transportGeneration: peerSession.transportGeneration,
		pursuitGeneration:   generation, command: command,
		stopDistance: stopDistance, lastTargetPosition: game.Vec3(position),
		startedAt: req.startTime,
	}
	timeout := campaignPursuitTimeoutProducer{
		action: e.action, logger: e.logger, sessionKey: sessionKey,
		sessionGeneration:   peerSession.generation,
		transportGeneration: peerSession.transportGeneration,
		pursuitGeneration:   generation, sourceObjectID: command.Common.ObjectID,
		syncStamp: command.Common.Unknown[0], timeout: zoneaction.PursuitTimeout,
	}
	producers := e.registry.producerGuard.scheduledProducers(sessionKey,
		[]raknet.ScheduledPacketProducer{
			{Delay: campaignPursuitCheckInterval, Produce: progress.produce},
			{Delay: zoneaction.PursuitTimeout, Produce: timeout.produce},
		})
	cancel, err := req.packet.ScheduleProducers(producers)
	if err == nil && cancel == nil {
		err = errors.New("nil pursuit cancellation")
	}
	if err != nil {
		cancelErr := e.cancelPlayerPursuitAdmission(
			sessionKey, peerSession.generation, generation, e.now(),
		)
		return nil, fmt.Errorf("trapSchedule: %w", errors.Join(err, cancelErr))
	}
	peerErr := publishCampaignPeersAfterCommit(e.registry, req.packet, [][]byte{movePacket})
	if peerErr != nil {
		e.logger.Printf("RakNet trap pursuit peer movement omitted source=%d: %v",
			command.Common.ObjectID, peerErr)
	}
	e.logger.Printf("RakNet trap pursuit started source=%d from=(%g,%g,%g) destination=(%g,%g,%g)",
		command.Common.ObjectID, previousPosition.X, previousPosition.Y, previousPosition.Z,
		position.X, position.Y, position.Z)
	return append(packets, movePacket), nil
}
