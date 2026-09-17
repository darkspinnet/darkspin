package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
)

type campaignPickupTimeout struct {
	runtime    campaignInteractionRuntime
	sessionKey string
	generation uint64
	command    raknet.ActionCommandData
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
		command: command,
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
