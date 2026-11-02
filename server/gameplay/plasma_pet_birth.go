package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
)

// Reactive birth runs inside the incoming damage transaction. Publish the
// creature before starting the normal companion selector and pursuit driver.
type plasmaPetBirthStep struct {
	run        *plasmaSentinelActiveRun
	identity   gameplayProducerIdentity
	packet     raknet.Packet
	objectID   uint32
	timestamp  uint64
	packets    [][]byte
	isProduced bool
}

func (e plasmaPetBirthStep) schedule() error {
	step := &e
	cancel, err := e.packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Produce: step.produce, AfterCommit: step.commit,
	}})
	if err != nil {
		return fmt.Errorf("birthSchedule: %w", err)
	}
	if cancel == nil {
		return errors.New("birth cancellation unavailable")
	}
	return nil
}

func (e *plasmaPetBirthStep) produce() ([][]byte, error) {
	if !e.run.runtime.registry.producerGuard.isCurrent(e.identity) {
		return nil, nil
	}
	e.run.runtime.registry.mutex.RLock()
	member, isFound := e.run.runtime.registry.sessions[e.run.sessionKey]
	isCurrent := e.run.isCurrent(member, isFound) && e.run.hasPet(e.objectID) &&
		member.zone != nil && member.zone.Companion() != nil
	if isCurrent {
		actor, isActorFound := member.zone.Companion().Snapshot(e.objectID)
		isCurrent = isActorFound && actor.HitPoint > 0 && actor.OwnerObjectID == e.run.ownerObjectID
	}
	e.run.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	e.isProduced = true
	return e.packets, nil
}

func (e *plasmaPetBirthStep) commit() {
	if !e.isProduced {
		return
	}
	// The owner has committed the birth. Queue the same packets to observers
	// before any subsequent action can enter their presentation queues.
	e.run.runtime.registry.queuePeerPresentation(e.identity, e.packets)
	producers := e.run.runtime.registry.producerGuard.scheduledProducersForIdentity(
		e.identity, []raknet.ScheduledPacketProducer{{Produce: e.startAttack}},
	)
	cancel, err := e.packet.ScheduleProducers(producers)
	if err == nil && cancel == nil {
		err = errors.New("attack cancellation unavailable")
	}
	if err != nil && e.run.runtime.logger != nil {
		e.run.runtime.logger.Printf("RakNet Pain Hound attack initialization failed object=%d: %v", e.objectID, err)
	}
}

func (e *plasmaPetBirthStep) startAttack() ([][]byte, error) {
	packets, err := e.run.runtime.damage.startPlasmaSentinelPetActorAttack(
		e.packet, e.run.sessionKey, e.run.generation, e.timestamp, e.objectID,
	)
	if err != nil {
		return nil, fmt.Errorf("birthAttack: %w", err)
	}
	return packets, nil
}
