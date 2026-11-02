package gameplay

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

type campaignInteractablePursuit struct {
	runtime             campaignInteractionRuntime
	sessionKey          string
	generation          uint64
	transportGeneration uint64
	motionRevision      uint64
	command             raknet.ActionCommandData
	destination         game.Vec3
	expiresAt           time.Time
}

func (e campaignInteractionRuntime) rejectScriptUse(
	command raknet.ActionCommandData, reason string,
) ([][]byte, error) {
	packet, err := actionraknet.Reject(command)
	if err != nil {
		return nil, fmt.Errorf("interactableReject: %w", err)
	}
	e.logger.Printf("RakNet campaign interactable rejected source=%d target=%d reason=%s",
		command.Common.ObjectID, command.Value, reason)
	return [][]byte{packet}, nil
}

func (e gameplayPendingRuntime) pollInteractablePursuit(
	ctx context.Context, packet raknet.Packet,
) ([][]byte, error) {
	e.registry.mutex.Lock()
	sessionKey := packet.Address.String()
	session, isFound := e.registry.sessions[sessionKey]
	if !isFound || session.interactablePursuit == nil {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	pursuit := session.interactablePursuit
	if session.generation != pursuit.generation ||
		session.transportGeneration != pursuit.transportGeneration ||
		packet.TransportGeneration != pursuit.transportGeneration {
		session.interactablePursuit = nil
		e.registry.sessions[sessionKey] = session
		e.registry.mutex.Unlock()
		return nil, nil
	}
	now := e.now()
	isMotionCurrent := session.deployedObjectID == pursuit.command.Common.ObjectID &&
		session.playerMotion != nil && session.playerMotionRevision() == pursuit.motionRevision
	if !isMotionCurrent || !session.isPickupActorAvailable() ||
		session.isEnemyRootActive(now) || session.isSleepingCloudActive(now) ||
		!now.Before(pursuit.expiresAt) {
		session.interactablePursuit = nil
		var stopPackets [][]byte
		if isMotionCurrent {
			stopErr := session.stopPlayerMovement(now)
			if stopErr != nil {
				e.registry.sessions[sessionKey] = session
				e.registry.mutex.Unlock()
				return nil, fmt.Errorf("interactableCancelStop: %w", stopErr)
			}
			var marshalErr error
			stopPackets, marshalErr = marshalZonePlayerStop(session.deployedObjectID, session.playerPosition)
			if marshalErr != nil {
				e.registry.sessions[sessionKey] = session
				e.registry.mutex.Unlock()
				return nil, fmt.Errorf("interactableCancelMarshal: %w", marshalErr)
			}
		}
		e.registry.sessions[sessionKey] = session
		e.registry.mutex.Unlock()
		packets, rejectErr := pursuit.runtime.rejectScriptUse(pursuit.command, "approach interrupted or expired")
		if rejectErr != nil {
			return nil, fmt.Errorf("interactableInterrupted: %w", rejectErr)
		}
		return append(stopPackets, packets...), nil
	}
	position, err := session.playerMotion.SamplePosition(now)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("interactableSample: %w", err)
	}
	// Wait for the validated exterior contact, rather than opening as soon as
	// the broad script-use compatibility range is crossed.
	if zonegeometry.Distance(game.Vec3(position), pursuit.destination) > campaignPlayerPursuitGoalTolerance {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	command := pursuit.command
	command.Common.Position = raknet.Vector3(position)
	session.interactablePursuit = nil
	e.registry.sessions[sessionKey] = session
	e.registry.mutex.Unlock()
	packets, err := pursuit.runtime.handleScriptUse(ctx, packet, command, pursuit.sessionKey, false)
	if err != nil {
		return nil, fmt.Errorf("interactableArrival: %w", err)
	}
	return packets, nil
}
