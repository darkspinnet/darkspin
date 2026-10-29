package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

func (e *campaignFixturePublication) beginProductionLocked() {
	e.isProducing = true
	e.productionDone = make(chan struct{})
}

func (e *campaignFixturePublication) finishProductionLocked() {
	e.isProducing = false
	if e.productionDone != nil {
		close(e.productionDone)
		e.productionDone = nil
	}
}

// invokeProducer recovers outside this owner. Release only this exact phase
// on an abnormal exit so a discard waiter cannot hang or close a successor.
func (e *campaignFixturePublication) settleAbandonedProduction(done chan struct{}, packets *[][]byte) {
	registry := e.source().runtime.registry
	registry.mutex.Lock()
	if e.productionDone != done || !e.isProducing {
		registry.mutex.Unlock()
		return
	}
	if packets != nil && len(*packets) > len(e.packets) {
		e.packets = *packets
	}
	err := errors.New("fixture production interrupted")
	e.produceErr = err
	e.isProduced = true
	e.finishProductionLocked()
	pendingHandoffs := registry.fixturePendingHandoffsLocked(e.source().sessionKey)
	batch := fixtureHandoffBatch{}
	if e.isFailed && e.burn == nil {
		e.retireBlastLocked(&batch)
	}
	registry.mutex.Unlock()
	batch.complete()
	e.fail(err)
	for _, publication := range pendingHandoffs {
		publication.fail(raknet.ErrScheduledPeerStale)
	}
}

func (e *campaignFixturePublication) scheduleBlast() error {
	blast := e.source()
	registry := blast.runtime.registry
	registry.mutex.Lock()
	if !e.isCurrentLocked() || e.isBlastScheduled || e.isProduced || e.isProducing || e.isDiscardPending {
		registry.mutex.Unlock()
		return nil
	}
	e.isBlastScheduled = true
	registry.mutex.Unlock()
	packet := blast.packet.Autonomous()
	if packet.ScheduleGroupResult == nil {
		return errors.New("fixture blast result scheduler unavailable")
	}
	delay := max(time.Duration(0), blast.notBefore.Sub(blast.runtime.npc.now()))
	producers := []raknet.ScheduledPacketProducer{{Delay: delay, Produce: e.produce, AfterCommit: e.commit}}
	cancel, err := packet.ScheduleGroupResult(producers, e.fail)
	registry.mutex.Lock()
	isCurrent := e.isCurrentLocked() && !e.isCommitted && !e.isFailed
	if isCurrent && err == nil && cancel != nil {
		e.blastCancel = cancel
	}
	registry.mutex.Unlock()
	if !isCurrent {
		if cancel != nil {
			cancel()
		}
		return nil
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return fmt.Errorf("fixtureBlastSchedule: %w", err)
	}
	if cancel == nil {
		return errors.New("fixture blast cancellation unavailable")
	}
	return nil
}

func (e *campaignFixturePublication) detachBlastLocked(batch *fixtureHandoffBatch) {
	e.detachHandoffRetryLocked(batch)
	if e.blastCancel != nil {
		batch.cancels = append(batch.cancels, e.blastCancel)
		e.blastCancel = nil
	}
}

func (e *campaignFixturePublication) stageDeath(death campaignFixtureDeath) {
	registry := e.source().runtime.registry
	registry.mutex.Lock()
	e.deaths = append(e.deaths, death)
	registry.mutex.Unlock()
}

func (e *campaignFixturePublication) completeBlast() {
	blast := e.source()
	registry := blast.runtime.registry
	registry.mutex.Lock()
	if !e.isCurrentLocked() || !e.isProduced || e.isProducing || e.isAdvanced {
		registry.mutex.Unlock()
		return
	}
	if e.produceErr != nil || e.isDiscardPending {
		err := e.produceErr
		if err == nil {
			err = errors.New("fixture world discarded")
		}
		registry.mutex.Unlock()
		e.fail(err)
		return
	}
	if e.isHandoffPending {
		registry.mutex.Unlock()
		e.fail(raknet.ErrScheduledPeerStale)
		return
	}
	if !e.isCommitted {
		// Initial Produce returns no transport bytes: this is the sole delivery
		// owner, including the original driver during a developer reset race.
		e.queueLocked(e.packets, true)
		e.packets = nil
		e.isCommitted = true
	}
	e.beginProductionLocked()
	productionDone := e.productionDone
	registry.mutex.Unlock()
	admissions := make([]*fixtureDeathAdmission, 0)
	defer activateFixtureDeaths(&admissions)
	defer e.settleAbandonedProduction(productionDone, nil)
	var admissionErr error
	for e.deathIndex < len(e.deaths) {
		registry.mutex.RLock()
		isDiscardPending := e.isDiscardPending
		registry.mutex.RUnlock()
		if isDiscardPending {
			break
		}
		var admission *fixtureDeathAdmission
		admission, admissionErr = e.deaths[e.deathIndex].schedule(blast)
		if admissionErr != nil {
			break
		}
		admissions = append(admissions, admission)
		registry.mutex.Lock()
		e.deathIndex++
		registry.mutex.Unlock()
	}
	registry.mutex.Lock()
	e.finishProductionLocked()
	pendingHandoffs := registry.fixturePendingHandoffsLocked(blast.sessionKey)
	if admissionErr != nil || e.isDiscardPending {
		if admissionErr == nil {
			admissionErr = errors.New("fixture world discarded")
		}
		registry.mutex.Unlock()
		e.fail(fmt.Errorf("fixtureDeathAdmission: %w", admissionErr))
		for _, publication := range pendingHandoffs {
			publication.fail(raknet.ErrScheduledPeerStale)
		}
		return
	}
	steps := make([]campaignFixtureBurnStep, 0, len(e.stagedBurns))
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
	e.isAdvanced = true
	delete(registry.fixturePublications, e)
	batch := fixtureHandoffBatch{}
	e.detachBlastLocked(&batch)
	registry.mutex.Unlock()
	batch.complete()
	for _, publication := range pendingHandoffs {
		publication.fail(raknet.ErrScheduledPeerStale)
	}
	for _, step := range steps {
		err := step.schedule()
		if err != nil {
			step.failSchedule(err)
		}
	}
}

func (e *gameplaySessionRegistry) prepareInitialFixtureHandoffsLocked(member gameplayPeerSession, driver *gameplayPeerSession, key string, packet raknet.Packet, batch *fixtureHandoffBatch) {
	publications := make([]*campaignFixturePublication, 0)
	for publication := range e.fixturePublications {
		if publication.matchesMember(member) {
			publications = append(publications, publication)
		}
	}
	for _, publication := range publications {
		if key == "" || publication.isDiscardPending {
			publication.retireBlastLocked(batch)
			continue
		}
		clone := *publication
		clone.isHandoffPending = false
		clone.isHandoffRetryScheduled = false
		clone.handoffRetryGeneration = 0
		clone.handoffRetryCancel = nil
		clone.blastCancel = nil
		clone.isBlastScheduled = false
		clone.productionDone = nil
		blast := *publication.blast
		blast.sessionKey, blast.generation, blast.packet = key, driver.generation, packet
		clone.blast = &blast
		clone.identity = gameplayProducerIdentityFromSession(key, *driver, true)
		clone.stagedBurns = append([]campaignFixtureBurnStep(nil), publication.stagedBurns...)
		publication.detachBlastLocked(batch)
		delete(e.fixturePublications, publication)
		for index := range clone.stagedBurns {
			step := &clone.stagedBurns[index]
			if !step.run.isActive() {
				continue
			}
			e.moveFixtureLocked(step, driver, key, packet, batch)
			step.run.fixtureBurn.publication = &clone
		}
		e.fixturePublications[&clone] = struct{}{}
		if clone.isProduced {
			batch.publications = append(batch.publications, &clone)
		} else {
			batch.blasts = append(batch.blasts, &clone)
		}
	}
}

func (e *campaignFixturePublication) matchesMember(member gameplayPeerSession) bool {
	return e.identity.memberKey == gameplaySessionMemberKey(member) &&
		e.identity.zoneGeneration == member.generation && e.identity.transportGeneration == member.transportGeneration
}

func (e *campaignFixturePublication) retireBlastLocked(batch *fixtureHandoffBatch) {
	registry := e.source().runtime.registry
	packets := make([][]byte, 0)
	if !e.isCommitted {
		packets = append(packets, e.packets...)
	}
	e.packets = nil
	for _, step := range e.stagedBurns {
		if !step.run.isActive() {
			continue
		}
		state := step.run.fixtureBurn
		packets = append(packets, state.pendingDeletes...)
		state.pendingDeletes = nil
		if !state.isRetiring {
			packet, err := effectraknet.ModifierDelete(step.targetID, step.run.instanceID)
			if err != nil {
				if registry.logger != nil {
					registry.logger.Printf("Fixture initial retirement delete failed target=%d: %v", step.targetID, err)
				}
			} else {
				packets = append(packets, packet)
			}
		}
	}
	e.queueLocked(packets, true)
	for _, step := range e.stagedBurns {
		step.finalizeLocked()
	}
	for index := e.deathIndex; index < len(e.deaths); index++ {
		e.deaths[index].stop()
	}
	e.detachBlastLocked(batch)
	e.isFailed = true
	delete(registry.fixturePublications, e)
}
