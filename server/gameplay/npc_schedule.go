package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
)

// scheduleNPCProducers publishes the committed NPC presentation to every
// teammate, including continuations scheduled by another delayed producer.
func scheduleNPCProducers(
	registry *gameplaySessionRegistry, packet raknet.Packet,
	producers []raknet.ScheduledPacketProducer,
) (raknet.CancelSchedule, error) {
	if registry == nil || packet.Address == nil {
		return nil, errors.New("NPC scheduler session unavailable")
	}
	guardedProducers := make([]raknet.ScheduledPacketProducer, len(producers))
	for index, producer := range producers {
		observer := &campaignNPCProducerObserver{
			registry: registry, sessionKey: packet.Address.String(),
			transportGeneration: packet.TransportGeneration,
			observer: gameplayProducerObserver{
				guard: registry.producerGuard, produceFunc: producer.Produce,
				commitFunc: producer.AfterCommit,
			},
		}
		guardedProducers[index] = producer
		if producer.Produce != nil {
			guardedProducers[index].Produce = observer.produce
			guardedProducers[index].AfterCommit = observer.observer.commit
		}
	}
	cancel, err := packet.ScheduleProducers(guardedProducers)
	if err != nil {
		return nil, fmt.Errorf("npcSchedule: %w", err)
	}
	if cancel == nil {
		return nil, errors.New("NPC scheduler cancellation unavailable")
	}
	return cancel, nil
}

// Some NPC operations schedule while holding the registry lock. Resolve the
// observer at execution, when that lock has been released. NPC producers retain
// their own zone/action-generation checks; the observer also fences transport
// replacement and records the exact identity to use after commit.
type campaignNPCProducerObserver struct {
	registry            *gameplaySessionRegistry
	sessionKey          string
	transportGeneration uint64
	observer            gameplayProducerObserver
}

func (e *campaignNPCProducerObserver) produce() ([][]byte, error) {
	identity := e.registry.producerGuard.identity(e.sessionKey)
	if !identity.isFound || identity.transportGeneration != e.transportGeneration {
		return nil, nil
	}
	e.observer.identity = identity
	packets, err := e.observer.produce()
	if err != nil {
		return packets, fmt.Errorf("npcProduce: %w", err)
	}
	return packets, nil
}

func scheduleNPCProducer(
	registry *gameplaySessionRegistry, packet raknet.Packet,
	delay time.Duration, produce func() ([][]byte, error),
) error {
	cancel, err := scheduleNPCProducers(registry, packet, []raknet.ScheduledPacketProducer{{
		Delay: delay, Produce: produce,
	}})
	if err != nil {
		return fmt.Errorf("npcContinuation: %w", err)
	}
	if cancel == nil {
		return errors.New("NPC continuation cancellation unavailable")
	}
	return nil
}
