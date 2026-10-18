package gameplay

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
)

type campaignPickupTimeout struct {
	runtime        campaignInteractionRuntime
	sessionKey     string
	generation     uint64
	motionRevision uint64
	command        raknet.ActionCommandData
}

func (e campaignInteractionRuntime) schedulePickupTimeout(
	packet raknet.Packet, sessionKey string, command raknet.ActionCommandData,
) error {
	e.registry.mutex.Lock()
	session, isFound := e.registry.sessions[sessionKey]
	if !isFound || session.deployedObjectID != command.Common.ObjectID {
		e.registry.mutex.Unlock()
		return errors.New("pickup actor unavailable")
	}
	step := &campaignPickupTimeout{
		runtime: e, sessionKey: sessionKey, generation: session.generation,
		command: command, motionRevision: session.playerMotionRevision(),
	}
	session.pickupPursuit = step
	e.registry.sessions[sessionKey] = session
	e.registry.mutex.Unlock()
	cancel, err := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: zoneaction.PursuitTimeout, Produce: step.produce,
	}})
	if err == nil && cancel == nil {
		err = errors.New("nil pursuit cancellation")
	}
	if err != nil {
		e.registry.mutex.Lock()
		session, isFound = e.registry.sessions[sessionKey]
		if isFound && session.pickupPursuit == step {
			session.pickupPursuit = nil
			e.registry.sessions[sessionKey] = session
		}
		e.registry.mutex.Unlock()
		return fmt.Errorf("pickupTimeout: %w", err)
	}
	return nil
}

func (e gameplayPendingRuntime) pollPickupPursuit(ctx context.Context, packet raknet.Packet) ([][]byte, error) {
	e.registry.mutex.Lock()
	session, isFound := e.registry.sessions[packet.Address.String()]
	if !isFound || session.pickupPursuit == nil {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	pursuit := session.pickupPursuit
	if session.generation != pursuit.generation || session.transportGeneration != packet.TransportGeneration ||
		!session.isPickupActorAvailable() || session.playerMotionRevision() != pursuit.motionRevision {
		session.pickupPursuit = nil
		e.registry.sessions[packet.Address.String()] = session
		e.registry.mutex.Unlock()
		return nil, nil
	}
	pickup, isPickupFound := session.zone.Pickups().Pickup(pursuit.command.Value)
	if !isPickupFound {
		session.pickupPursuit = nil
		e.registry.sessions[packet.Address.String()] = session
		e.registry.mutex.Unlock()
		return nil, nil
	}
	position := game.Vec3(session.playerPosition)
	if session.playerMotion != nil {
		sampledPosition, err := session.playerMotion.SamplePosition(e.now())
		if err != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("pickupPursuitPosition: %w", err)
		}
		position = game.Vec3(sampledPosition)
	}
	distance := zoneability.Distance(position, pickup.Position)
	if pickup.IsSourcePositionKnown {
		distance = min(distance, zoneability.Distance(position, pickup.SourcePosition))
	}
	if distance > session.campaignPickupMaximumDistance() {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	command := pursuit.command
	command.Common.Position = raknet.Vector3(position)
	session.pickupPursuit = nil
	e.registry.sessions[packet.Address.String()] = session
	e.registry.mutex.Unlock()
	packets, err := pursuit.runtime.handlePickup(ctx, packet, command, pursuit.sessionKey)
	if err != nil {
		return nil, fmt.Errorf("pickupPursuitArrival: %w", err)
	}
	return packets, nil
}

func (e *campaignPickupTimeout) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	session, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && session.generation == e.generation &&
		session.pickupPursuit == e
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	session.pickupPursuit = nil
	e.runtime.registry.sessions[e.sessionKey] = session
	e.runtime.registry.mutex.Unlock()
	if session.deployedObjectID != e.command.Common.ObjectID {
		return nil, nil
	}
	packets, err := e.runtime.rejectPickup(e.command, "pursuit timed out")
	if err != nil {
		return nil, fmt.Errorf("pickupRelease: %w", err)
	}
	return packets, nil
}
