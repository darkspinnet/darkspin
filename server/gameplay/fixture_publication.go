package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
)

// The registry owns these phases, including the interval between a committed
// NPC mutation and its final feedback. Transport callbacks never own the run.
type campaignFixturePublication struct {
	burn                    *campaignFixtureBurnStep
	blast                   *campaignFixtureBlastStep
	stagedBurns             []campaignFixtureBurnStep
	packets                 [][]byte
	isCommitted             bool
	isFailed                bool
	isProduced              bool
	isProducing             bool
	isHandoffPending        bool
	isHandoffRetryScheduled bool
	handoffRetryGeneration  uint64
	handoffRetryCancel      func()
	produceErr              error
	handoffGeneration       uint64
	identity                gameplayProducerIdentity
	blastCancel             func()
	isBlastScheduled        bool
	isDiscardPending        bool
	deaths                  []campaignFixtureDeath
	deathIndex              int
	productionDone          chan struct{}
	isAdvanced              bool
}

func (e *campaignFixturePublication) source() campaignFixtureBlastStep {
	if e.burn != nil {
		return e.burn.blast
	}
	return *e.blast
}

func (e *campaignFixturePublication) isCurrentLocked() bool {
	blast := e.source()
	if e.burn == nil {
		_, isIndexed := blast.runtime.registry.fixturePublications[e]
		if !isIndexed || e.isFailed {
			return false
		}
	}
	member, isFound := blast.runtime.registry.sessions[blast.sessionKey]
	if !isFound || member.generation != blast.generation ||
		member.transportGeneration != e.identity.transportGeneration ||
		gameplaySessionMemberKey(member) != e.identity.memberKey || member.zone == nil || member.zone.NPCs() != blast.npc {
		return false
	}
	if e.burn == nil {
		return true
	}
	if member.isZoneTerminal() {
		return false
	}
	state := e.burn.run.fixtureBurn
	return member.campaignNPCModifiers[e.burn.run.instanceID] == e.burn.run &&
		state.handoffGeneration == e.handoffGeneration && state.publication == e
}

func (e *campaignFixturePublication) stage(step campaignFixtureBurnStep) {
	registry := step.blast.runtime.registry
	registry.mutex.Lock()
	e.stagedBurns = append(e.stagedBurns, step)
	registry.mutex.Unlock()
}

func (e *campaignFixturePublication) recordPreparedPackets(packets [][]byte) {
	if e == nil || e.burn != nil || len(packets) == 0 {
		return
	}
	registry := e.source().runtime.registry
	registry.mutex.Lock()
	e.packets = append(e.packets, packets...)
	registry.mutex.Unlock()
}

func (e *campaignFixturePublication) produce() ([][]byte, error) {
	blast := e.source()
	registry := blast.runtime.registry
	registry.mutex.Lock()
	if !e.isCurrentLocked() || e.isFailed || e.isCommitted || e.isProduced || e.isProducing {
		registry.mutex.Unlock()
		return nil, nil
	}
	e.beginProductionLocked()
	productionDone := e.productionDone
	registry.mutex.Unlock()
	var packets [][]byte
	defer e.settleAbandonedProduction(productionDone, &packets)
	var err error
	if e.burn != nil {
		step := *e.burn
		step.blast.publication = e
		packets, err = step.produce()
	} else {
		blast.publication = e
		packets, err = blast.produce()
	}
	registry.mutex.Lock()
	e.packets = packets
	e.produceErr = err
	e.isProduced = true
	e.finishProductionLocked()
	pendingHandoffs := registry.fixturePendingHandoffsLocked(blast.sessionKey)
	isDiscardPending := e.isDiscardPending
	registry.mutex.Unlock()
	if err != nil {
		e.fail(err)
		for _, publication := range pendingHandoffs {
			publication.fail(raknet.ErrScheduledPeerStale)
		}
		return nil, fmt.Errorf("fixtureProduce: %w", err)
	}
	if isDiscardPending && e.burn == nil {
		e.fail(errors.New("fixture world discarded"))
		return nil, nil
	}
	for _, publication := range pendingHandoffs {
		publication.fail(raknet.ErrScheduledPeerStale)
	}
	if e.burn == nil {
		return nil, nil
	}
	return packets, nil
}

func (e *campaignFixturePublication) commit() {
	e.complete(false)
}

// A handoff uses the same publication boundary as AfterCommit, but its old
// transport did not deliver to the new driver, so the new driver is included.
func (e *campaignFixturePublication) complete(isHandoff bool) {
	if e.burn == nil {
		e.completeBlast()
		return
	}
	blast := e.source()
	registry := blast.runtime.registry
	registry.mutex.Lock()
	if !e.isCurrentLocked() || !e.isProduced || e.isProducing || e.isFailed || e.isCommitted {
		registry.mutex.Unlock()
		return
	}
	if e.produceErr != nil {
		err := e.produceErr
		registry.mutex.Unlock()
		e.fail(err)
		return
	}
	if e.isHandoffPending {
		registry.mutex.Unlock()
		e.fail(raknet.ErrScheduledPeerStale)
		return
	}
	e.isCommitted = true
	e.queueLocked(e.packets, isHandoff)
	e.packets = nil
	steps := make([]campaignFixtureBurnStep, 0, len(e.stagedBurns)+1)
	for _, step := range e.stagedBurns {
		state := step.run.fixtureBurn
		if state.publication != e || !step.run.isActive() {
			continue
		}
		if state.isRetiring {
			e.queueLocked(state.pendingDeletes, true)
			state.pendingDeletes = nil
			step.finalizeLocked()
			continue
		}
		state.isCreationPublished = true
		state.publication = nil
		state.cursor = step
		state.isCursorReady = true
		steps = append(steps, step)
	}
	if e.burn != nil {
		step := *e.burn
		state := step.run.fixtureBurn
		if state.isRetiring {
			e.queueLocked(state.pendingDeletes, true)
			state.pendingDeletes = nil
			step.finalizeLocked()
		} else if step.run.isActive() && (step.isPermanent || step.remainingTickCount > 1) {
			step.blast.timestamp += 1000
			step.deadline += time.Second
			if !step.isPermanent {
				step.remainingTickCount--
			}
			state.publication = nil
			state.cursor = step
			state.isCursorReady = true
			steps = append(steps, step)
		}
	}
	registry.mutex.Unlock()
	for _, step := range steps {
		err := step.schedule()
		if err != nil {
			step.failSchedule(err)
		}
	}
}

func (e *campaignFixturePublication) queueLocked(packets [][]byte, isDriverIncluded bool) {
	blast := e.source()
	registry := blast.runtime.registry
	for key, member := range registry.sessions {
		if member.zone == nil || member.zone.NPCs() != blast.npc ||
			(!isDriverIncluded && key == e.identity.sessionKey) {
			continue
		}
		member.queueCampaignPackets(packets)
		registry.sessions[key] = member
	}
}

func isFixturePeerUnavailable(err error) bool {
	return errors.Is(err, raknet.ErrScheduledPeerStale) || errors.Is(err, raknet.ErrSchedulePeerUnavailable)
}

func (e *campaignFixturePublication) fail(scheduleErr error) {
	blast := e.source()
	registry := blast.runtime.registry
	registry.mutex.Lock()
	if !e.isCurrentLocked() || e.isFailed {
		registry.mutex.Unlock()
		return
	}
	isPeerUnavailable := isFixturePeerUnavailable(scheduleErr)
	if isPeerUnavailable && e.produceErr == nil && !e.isDiscardPending && (!e.isCommitted || e.burn == nil) {
		e.isHandoffPending = true
		member := registry.sessions[blast.sessionKey]
		// The typed scheduler result proves this transport has gone away even
		// if its asynchronous lifecycle notification has not reached us yet.
		member.isRejoinPending = true
		registry.sessions[blast.sessionKey] = member
		batch, isDeferred := registry.prepareFixtureHandoffLocked(&member)
		if isDeferred {
			isRetryNeeded := !e.isHandoffRetryScheduled
			if isRetryNeeded {
				e.isHandoffRetryScheduled = true
				e.handoffRetryGeneration++
			}
			retryGeneration := e.handoffRetryGeneration
			registry.mutex.Unlock()
			if isRetryNeeded {
				retry := fixturePublicationHandoffRetry{publication: e, generation: retryGeneration}
				retry.schedule()
			}
			return
		}
		registry.sessions[blast.sessionKey] = member
		registry.mutex.Unlock()
		batch.complete()
		return
	}
	if e.isProducing {
		registry.mutex.Unlock()
		return
	}
	e.isFailed = true
	e.beginProductionLocked()
	retryBatch := fixtureHandoffBatch{}
	e.detachHandoffRetryLocked(&retryBatch)
	packets := e.packets
	if e.isCommitted {
		packets = nil
	}
	e.packets = nil
	steps := append([]campaignFixtureBurnStep(nil), e.stagedBurns...)
	if e.burn != nil {
		steps = append(steps, *e.burn)
	}
	productionDone := e.productionDone
	registry.mutex.Unlock()
	defer e.settleAbandonedProduction(productionDone, &packets)
	cleanupErrors := make([]error, 0)
	cancels := retryBatch.cancels
	retryBatch.cancels = nil
	for _, step := range steps {
		deletePackets, err := step.stop()
		packets = append(packets, deletePackets...)
		if err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	registry.mutex.Lock()
	// A failure owns the same exact publication until its output is queued.
	for _, step := range steps {
		packets = append(packets, step.run.fixtureBurn.pendingDeletes...)
		step.run.fixtureBurn.pendingDeletes = nil
	}
	e.queueLocked(packets, true)
	e.isCommitted = true
	for _, step := range steps {
		step.run.mu.Lock()
		cancel := step.run.cancel
		step.run.cancel = nil
		step.run.mu.Unlock()
		if cancel != nil {
			cancels = append(cancels, cancel)
		}
		step.finalizeLocked()
	}
	e.finishProductionLocked()
	if e.burn == nil {
		e.detachBlastLocked(&retryBatch)
		delete(registry.fixturePublications, e)
		for index := e.deathIndex; index < len(e.deaths); index++ {
			e.deaths[index].stop()
		}
		cancels = append(cancels, retryBatch.cancels...)
	}
	registry.mutex.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	blast.runtime.logger.Printf("Fixture publication failed source=%d: %v", blast.source.Plan.ObjectID, errors.Join(append(cleanupErrors, scheduleErr)...))
}

func (e campaignFixtureBurnStep) failSchedule(err error) {
	registry := e.blast.runtime.registry
	registry.mutex.RLock()
	state := e.run.fixtureBurn
	publication := state.publication
	if state.handoffGeneration != e.handoffGeneration {
		publication = nil
	}
	registry.mutex.RUnlock()
	if publication != nil {
		publication.fail(fmt.Errorf("fixtureAdmission: %w", err))
	}
}
