package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// transferPlayerAbilityPursuit consumes the registry lock and preserves the
// requested ability slot for the bounded pursuit's arrival retry.
func (r campaignAbilityCommandRuntime) transferPlayerAbilityPursuit(
	req campaignCharacterAbilityRequest, peerSession gameplayPeerSession,
	targetEnemy zonenpc.Snapshot, admissionRange float32, isPursuitRetry bool,
) ([][]byte, bool, error) {
	command := req.command
	packet := req.packet
	sessionKey := packet.Address.String()
	abilityStartTime := req.startTime
	targetObjectID := targetEnemy.Plan.ObjectID
	command.Ability.TargetID = targetObjectID
	actorFootprint := peerSession.deployedCampaignActorFootprintRadius()
	targetFootprint := targetEnemy.Plan.ActorFootprintRadius()
	if peerSession.isEnemyRootActive(abilityStartTime) {
		r.registry.mutex.Unlock()
		packets, rejectErr := req.reject("cannot pursue while rooted")
		if rejectErr != nil {
			return nil, false, fmt.Errorf("pursuitRootReject: %w", rejectErr)
		}
		return packets, false, nil
	}
	adjustedRange, adjustedErr := zoneaction.AdjustedPursuitRange(admissionRange)
	if adjustedErr != nil {
		r.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("campaignBasicPursuitRange: %w", adjustedErr)
	}
	stopDistance := actorFootprint + targetFootprint + adjustedRange
	if isPursuitRetry {
		r.registry.sessions[sessionKey] = peerSession
		r.registry.mutex.Unlock()
		pursuitResponse, responseErr := actionraknet.PursuitTransfer(
			command.Common.Unknown[0], command.Common.ObjectID,
		)
		if responseErr != nil {
			return nil, false, fmt.Errorf(
				"campaignBasicPursuitResponse: %w", responseErr,
			)
		}
		peerErr := r.publishPursuitToPeers(
			packet, command.Common.ObjectID, peerSession.playerPosition,
			targetObjectID, targetEnemy.Plan.Position, stopDistance,
		)
		if peerErr != nil {
			return nil, false, fmt.Errorf("pursuitRetryPeers: %w", peerErr)
		}
		return pursuitResponse, false, nil
	}
	pursuitPackets, marshalErr := actionraknet.PursuitTransfer(
		command.Common.Unknown[0], command.Common.ObjectID,
	)
	if marshalErr != nil {
		r.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("campaignBasicPursuitTransfer: %w", marshalErr)
	}
	previousPosition, currentPosition, movementErr := peerSession.advancePlayerPursuitMovement(
		abilityStartTime, peerSession.playerPosition,
		raknet.Vector3{
			X: targetEnemy.Plan.Position.X,
			Y: targetEnemy.Plan.Position.Y,
			Z: targetEnemy.Plan.Position.Z,
		},
		r.registry.passiveMovementIncrease(peerSession),
	)
	if movementErr != nil {
		r.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("campaignBasicPursuitMotion: %w", movementErr)
	}
	pursuitGeneration := peerSession.campaignPlayerPursuitSession().Begin(
		command.Common.ObjectID, targetObjectID, command.Ability.Index,
		command.Common.Unknown[0], stopDistance,
	)
	pursuitSyncStamp := command.Common.Unknown[0]
	pursuitSourceObjectID := command.Common.ObjectID
	sessionGeneration := peerSession.generation
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	timeout := campaignPursuitTimeoutProducer{
		action: r.action, logger: r.logger,
		sessionKey: sessionKey, sessionGeneration: sessionGeneration,
		transportGeneration: peerSession.transportGeneration,
		pursuitGeneration:   pursuitGeneration,
		sourceObjectID:      pursuitSourceObjectID, targetObjectID: targetObjectID,
		syncStamp: pursuitSyncStamp, timeout: zoneaction.PursuitTimeout,
	}
	timeoutProducer := raknet.ScheduledPacketProducer{
		Delay:   zoneaction.PursuitTimeout,
		Produce: timeout.produce,
	}
	progress := campaignPursuitProgressProducer{
		runtime: r, packet: packet, sessionKey: sessionKey,
		sessionGeneration:   sessionGeneration,
		transportGeneration: peerSession.transportGeneration,
		pursuitGeneration:   pursuitGeneration, command: command,
		stopDistance:       stopDistance,
		lastTargetPosition: targetEnemy.Plan.Position,
		startedAt:          abilityStartTime,
	}
	progressProducer := raknet.ScheduledPacketProducer{
		Delay: campaignPursuitCheckInterval, Produce: progress.produce,
	}
	pursuitProducers := r.registry.producerGuard.scheduledProducers(
		sessionKey, []raknet.ScheduledPacketProducer{
			progressProducer, timeoutProducer,
		},
	)
	pursuitCancel, scheduleErr := packet.ScheduleProducers(pursuitProducers)
	err := scheduleErr
	if err == nil && pursuitCancel == nil {
		err = errors.New("campaign pursuit schedule returned no cancellation")
	}
	if err != nil {
		rollbackErr := r.cancelPlayerPursuitAdmission(
			sessionKey, sessionGeneration, pursuitGeneration, abilityStartTime,
		)
		if rollbackErr != nil {
			return nil, false, fmt.Errorf(
				"campaignBasicPursuitRollback: %w", rollbackErr,
			)
		}
		packets, rejectErr := req.reject("pursuit scheduling unavailable")
		if rejectErr != nil {
			return nil, false, fmt.Errorf("pursuitScheduleReject: %w", rejectErr)
		}
		return packets, false, nil
	}
	r.logger.Printf("RakNet campaign ability pursuit transferred source=%d target=%d index=%d stop=%g noun=%q previous_position=%v current_position=%v",
		command.Common.ObjectID, targetObjectID, command.Ability.Index, stopDistance, targetEnemy.Plan.NounName, previousPosition, currentPosition)
	peerErr := r.publishPursuitToPeers(
		packet, command.Common.ObjectID, peerSession.playerPosition,
		targetObjectID, targetEnemy.Plan.Position, stopDistance,
	)
	if peerErr != nil {
		return nil, false, fmt.Errorf("pursuitStartPeers: %w", peerErr)
	}
	return pursuitPackets, true, nil
}
