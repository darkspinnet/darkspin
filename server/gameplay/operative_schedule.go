package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
)

type operativeCageStep uint8

const (
	operativeCageNone operativeCageStep = iota
	operativeCageStart
	operativeCageBegin
	operativeCageTick
	operativeCageRetry
)

// One transport deadline owns its proposed continuation. No later deadline is
// admitted until this deadline's presentation has successfully committed.
type operativeCageProducer struct {
	schedule  operativeCageSchedule
	step      operativeCageStep
	nextStep  operativeCageStep
	nextDelay time.Duration
	packets   [][]byte
}

// The registry lock owns the cancellation handle, including unbound windups.
func (e *operativeCageState) cancelSchedule() {
	e.scheduleGeneration++
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
}

func (e operativeCageSchedule) admit(step operativeCageStep, delay time.Duration) error {
	if e.packet.ScheduleGroupResult == nil {
		return errors.New("operative result scheduler unavailable")
	}
	e.runtime.registry.mutex.Lock()
	member, isCurrent := e.currentLocked()
	if !isCurrent || member.isZoneTerminal() {
		e.runtime.registry.mutex.Unlock()
		return errors.New("operative source unavailable")
	}
	e.state.scheduleGeneration++
	scheduleGeneration := e.state.scheduleGeneration
	e.runtime.registry.mutex.Unlock()
	producer := &operativeCageProducer{schedule: e, step: step}
	observer := &campaignNPCProducerObserver{
		registry: e.runtime.registry, sessionKey: e.sessionKey,
		transportGeneration: e.packet.TransportGeneration,
		observer: gameplayProducerObserver{
			guard: e.runtime.registry.producerGuard, produceFunc: producer.produce,
			commitFunc: producer.commit,
		},
	}
	cancel, err := e.packet.ScheduleGroupResult([]raknet.ScheduledPacketProducer{{
		Delay: delay, Produce: observer.produce, AfterCommit: observer.observer.commit,
	}}, producer.failed)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return fmt.Errorf("cageSchedule: %w", err)
	}
	if cancel == nil {
		return errors.New("operative cancellation unavailable")
	}
	e.runtime.registry.mutex.Lock()
	member, isCurrent = e.currentLocked()
	if isCurrent && !member.isZoneTerminal() &&
		e.state.scheduleGeneration == scheduleGeneration {
		e.state.cancel = cancel
	} else {
		cancel()
	}
	e.runtime.registry.mutex.Unlock()
	return nil
}

func (e *operativeCageProducer) produce() ([][]byte, error) {
	var packets [][]byte
	var err error
	switch e.step {
	case operativeCageStart:
		e.schedule.runtime.registry.mutex.RLock()
		member, isCurrent := e.schedule.currentLocked()
		isCurrent = isCurrent && !e.schedule.state.isComplete && !member.isZoneTerminal()
		e.schedule.runtime.registry.mutex.RUnlock()
		if !isCurrent {
			packets, err = e.schedule.finish(e)
			break
		}
		packets, err = e.schedule.runtime.startNPCAttack(e.schedule.sessionKey,
			e.schedule.generation, e.schedule.state.plan, e.schedule.timestamp)
		if err == nil {
			e.nextStep = operativeCageBegin
			e.nextDelay = e.schedule.state.plan.Profile.HitDelay
		}
	case operativeCageBegin:
		packets, err = e.schedule.begin(e)
	case operativeCageTick:
		packets, err = e.schedule.tick(e)
	case operativeCageRetry:
		// Retargeting/retry may itself admit a new cast. Run it only after
		// this cooldown deadline has committed, even when it has no packets.
	default:
		err = errors.New("operative schedule step invalid")
	}
	e.packets = packets
	if e.step == operativeCageStart {
		e.schedule.runtime.registry.mutex.RLock()
		retirementPackets := append([][]byte(nil), e.schedule.state.retirementPackets...)
		e.schedule.runtime.registry.mutex.RUnlock()
		packets = append(retirementPackets, packets...)
	}
	if err != nil {
		return nil, fmt.Errorf("cageProduce: %w", err)
	}
	return packets, nil
}

func (e *operativeCageProducer) commit() {
	if e.step == operativeCageStart {
		e.schedule.runtime.registry.mutex.Lock()
		member, isFound := e.schedule.runtime.registry.sessions[e.schedule.sessionKey]
		if isFound && member.generation == e.schedule.generation &&
			member.operativeChannels[e.schedule.state.plan.SourceObjectID] == e.schedule.state {
			e.schedule.state.retirementPackets = nil
		}
		e.schedule.runtime.registry.mutex.Unlock()
	}
	if e.step == operativeCageRetry {
		packets, err := e.schedule.retry()
		if err != nil {
			e.failed(err)
			return
		}
		e.queuePackets(packets)
		return
	}
	if e.nextStep == operativeCageNone {
		return
	}
	// An intervening target poll may have completed this state. Admit its
	// next tick anyway: that tick retires the cast and advances its cooldown.
	err := e.schedule.admit(e.nextStep, e.nextDelay)
	if err != nil {
		e.packets = nil // This deadline's packets already committed.
		e.failed(err)
	}
}

func (e *operativeCageProducer) failed(scheduleErr error) {
	packets, err := e.schedule.fail("cagePublication", scheduleErr, e.packets)
	if len(packets) != 0 {
		// fail currently queues cleanup itself; retain this boundary if it
		// later returns additional presentation rather than dropping it.
		e.queuePackets(packets)
	}
	if err != nil && e.schedule.runtime.logger != nil {
		e.schedule.runtime.logger.Printf("RakNet operative schedule source=%d: %v",
			e.schedule.state.plan.SourceObjectID, err)
	}
}

func (e *operativeCageProducer) queuePackets(packets [][]byte) {
	if len(packets) == 0 {
		return
	}
	e.schedule.runtime.registry.mutex.Lock()
	defer e.schedule.runtime.registry.mutex.Unlock()
	source, isFound := e.schedule.runtime.registry.sessions[e.schedule.sessionKey]
	if !isFound || source.generation != e.schedule.generation || source.zone == nil {
		return
	}
	for sessionKey, observer := range e.schedule.runtime.registry.sessions {
		if observer.zone != source.zone || observer.isRejoinPending || !observer.stage.IsDungeon() {
			continue
		}
		observer.queuePackets(packets)
		e.schedule.runtime.registry.sessions[sessionKey] = observer
	}
}
