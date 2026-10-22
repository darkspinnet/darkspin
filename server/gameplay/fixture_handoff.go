package gameplay

import (
	"fmt"
	"net"
	"sort"

	"github.com/darkspinnet/darkspin/server/raknet"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

type fixtureStaleTransportRetry struct {
	lifecycle           gameplaySessionLifecycle
	sessionKey          string
	transportGeneration uint64
	modifierPool        *modifierPool
}

func (e fixtureStaleTransportRetry) execute() {
	e.lifecycle.recoverStaleTransport(e.sessionKey, e.transportGeneration, e.modifierPool)
}

func (e fixtureStaleTransportRetry) schedule() {
	registry := e.lifecycle.registry
	if registry.timer == nil {
		if registry.logger != nil {
			registry.logger.Printf("Fixture stale transport retry has no timer remote=%s", e.sessionKey)
		}
		return
	}
	cancel, err := registry.timer.Schedule(disconnectRetryDelay, e.execute)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		if registry.logger != nil {
			registry.logger.Printf("Fixture stale transport retry failed remote=%s: %v", e.sessionKey, err)
		}
		return
	}
	if cancel == nil && registry.logger != nil {
		registry.logger.Printf("Fixture stale transport retry missing cancellation remote=%s", e.sessionKey)
	}
}

type fixtureDiscardMemberRetry struct {
	lifecycle    gameplaySessionLifecycle
	gameID       uint32
	userID       uint64
	modifierPool *modifierPool
	effectPool   *attachedEffectPool
}

func (e fixtureDiscardMemberRetry) execute() {
	e.lifecycle.discardMember(e.gameID, e.userID, e.modifierPool, e.effectPool)
}

func (e fixtureDiscardMemberRetry) schedule() {
	registry := e.lifecycle.registry
	if registry.timer == nil {
		if registry.logger != nil {
			registry.logger.Printf("Fixture member removal retry has no timer game=%d user=%d", e.gameID, e.userID)
		}
		return
	}
	cancel, err := registry.timer.Schedule(disconnectRetryDelay, e.execute)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		if registry.logger != nil {
			registry.logger.Printf("Fixture member removal retry failed game=%d user=%d: %v", e.gameID, e.userID, err)
		}
		return
	}
	if cancel == nil && registry.logger != nil {
		registry.logger.Printf("Fixture member removal retry missing cancellation game=%d user=%d", e.gameID, e.userID)
	}
}

type fixtureHandoffBatch struct {
	cancels      []func()
	publications []*campaignFixturePublication
	steps        []campaignFixtureBurnStep
	blasts       []*campaignFixturePublication
}

type fixturePublicationHandoffRetry struct {
	publication *campaignFixturePublication
	generation  uint64
}

func (e fixturePublicationHandoffRetry) execute() {
	publication := e.publication
	registry := publication.source().runtime.registry
	registry.mutex.Lock()
	if !publication.isCurrentLocked() || !publication.isHandoffPending ||
		publication.handoffRetryGeneration != e.generation {
		registry.mutex.Unlock()
		return
	}
	publication.isHandoffRetryScheduled = false
	publication.handoffRetryCancel = nil
	registry.mutex.Unlock()
	publication.fail(raknet.ErrScheduledPeerStale)
}

func (e fixturePublicationHandoffRetry) schedule() {
	publication := e.publication
	blast := publication.source()
	registry := blast.runtime.registry
	var cancel func()
	var err error
	if registry.timer == nil {
		err = fmt.Errorf("fixture retry timer unavailable")
	} else {
		cancel, err = registry.timer.Schedule(disconnectRetryDelay, e.execute)
		if err == nil && cancel == nil {
			err = fmt.Errorf("fixture retry cancellation unavailable")
		}
	}
	registry.mutex.Lock()
	isCurrent := publication.isCurrentLocked() && publication.isHandoffPending &&
		publication.isHandoffRetryScheduled && publication.handoffRetryGeneration == e.generation
	if isCurrent {
		if err == nil {
			publication.handoffRetryCancel = cancel
		} else {
			// Keep the pending indication: the producing sibling also wakes it
			// after saving either its successful output or its domain failure.
			publication.isHandoffRetryScheduled = false
		}
	}
	registry.mutex.Unlock()
	if (!isCurrent || err != nil) && cancel != nil {
		cancel()
	}
	if err != nil && registry.logger != nil {
		registry.logger.Printf("Fixture transport handoff retry failed source=%d: %v", blast.source.Plan.ObjectID, err)
	}
}

func (e *gameplaySessionRegistry) fixturePendingHandoffsLocked(sessionKey string) []*campaignFixturePublication {
	member, isFound := e.sessions[sessionKey]
	if !isFound {
		return nil
	}
	publications := make([]*campaignFixturePublication, 0)
	seenPublications := make(map[*campaignFixturePublication]bool)
	for publication := range e.fixturePublications {
		if publication.source().sessionKey == sessionKey && publication.isHandoffPending {
			publications = append(publications, publication)
			seenPublications[publication] = true
		}
	}
	for _, run := range member.campaignNPCModifiers {
		if run == nil || run.fixtureBurn == nil {
			continue
		}
		publication := run.fixtureBurn.publication
		if publication == nil || !publication.isHandoffPending || seenPublications[publication] {
			continue
		}
		seenPublications[publication] = true
		publications = append(publications, publication)
	}
	return publications
}

func (e fixtureHandoffBatch) complete() {
	for _, cancel := range e.cancels {
		cancel()
	}
	for _, publication := range e.publications {
		publication.complete(true)
	}
	for _, publication := range e.blasts {
		err := publication.scheduleBlast()
		if err != nil {
			publication.fail(err)
		}
	}
	for _, step := range e.steps {
		err := step.schedule()
		if err != nil {
			step.failSchedule(err)
		}
	}
}

func (e *gameplaySessionRegistry) isFixtureProducingLocked(member gameplayPeerSession) bool {
	for publication := range e.fixturePublications {
		if publication.matchesMember(member) && publication.isProducing {
			return true
		}
	}
	for _, run := range member.campaignNPCModifiers {
		if run == nil || run.fixtureBurn == nil {
			continue
		}
		publication := run.fixtureBurn.publication
		if publication != nil && publication.isProducing {
			return true
		}
	}
	return false
}

// Call before ANY old-peer retirement, transport reset or map overwrite. A
// deferred result requires retrying the enclosing lifecycle operation.
func (e *gameplaySessionRegistry) prepareFixtureHandoffLocked(member *gameplayPeerSession) (fixtureHandoffBatch, bool) {
	batch := fixtureHandoffBatch{}
	if e.isFixtureProducingLocked(*member) {
		return batch, true
	}
	keys := make([]string, 0)
	for key, candidate := range e.sessions {
		if candidate.zone == nil || candidate.zone != member.zone ||
			gameplaySessionMemberKey(candidate) == gameplaySessionMemberKey(*member) ||
			candidate.isRejoinPending || candidate.isZoneTerminal() || !candidate.dungeonSetup.IsCommitted() ||
			candidate.transportGeneration == 0 || candidate.schedulePacket.ScheduleGroupResult == nil {
			continue
		}
		memberKey := gameplaySessionMemberKey(candidate)
		endpoint := e.memberEndpoints[memberKey]
		if endpoint != key || e.memberTransports[memberKey] != candidate.transportGeneration || !candidate.zone.IsActive() {
			continue
		}
		isConnected := false
		for _, participant := range candidate.zone.Snapshot().Members {
			if participant.UserID == candidate.binding.UserID && participant.PeerGeneration == candidate.generation && participant.IsConnected {
				isConnected = true
				break
			}
		}
		if !isConnected {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	driverKey := ""
	driver := gameplayPeerSession{}
	packet := raknet.Packet{}
	for _, key := range keys {
		candidate := e.sessions[key]
		address, err := net.ResolveUDPAddr("udp", key)
		if err != nil {
			if e.logger != nil {
				e.logger.Printf("Fixture handoff address rejected remote=%s: %v", key, err)
			}
			continue
		}
		driverKey = key
		driver = candidate
		packet = candidate.schedulePacket.Autonomous()
		packet.Address = address
		packet.TransportGeneration = candidate.transportGeneration
		break
	}
	e.prepareInitialFixtureHandoffsLocked(*member, &driver, driverKey, packet, &batch)
	runs := make([]*campaignNPCModifierRun, 0)
	for _, run := range member.campaignNPCModifiers {
		if run != nil && run.fixtureBurn != nil {
			runs = append(runs, run)
		}
	}
	publications := make(map[*campaignFixturePublication]bool)
	sort.SliceStable(runs, func(a, b int) bool {
		return runs[a].fixtureBurn.isCreationPublished && !runs[b].fixtureBurn.isCreationPublished
	})
	for _, run := range runs {
		state := run.fixtureBurn
		// Moving a produced parent's staged child already removes its old alias.
		if member.campaignNPCModifiers[run.instanceID] != run {
			continue
		}
		publication := state.publication
		if driverKey == "" || !state.isCreationPublished {
			// Unpublished initial creation and no-ready-driver retain the existing
			// retirement policy, but accepted output is queued before pool reuse.
			e.retireDetachedFixtureLocked(run, &batch)
			continue
		}
		if publication != nil && publication.isProduced && !publication.isCommitted && !publication.isFailed {
			if publications[publication] {
				continue
			}
			publications[publication] = true
			clone := *publication
			clone.isHandoffPending = false
			clone.isHandoffRetryScheduled = false
			clone.handoffRetryGeneration = 0
			clone.handoffRetryCancel = nil
			step := *publication.burn
			e.moveFixtureLocked(&step, &driver, driverKey, packet, &batch)
			clone.burn = &step
			clone.handoffGeneration = step.handoffGeneration
			clone.identity = gameplayProducerIdentityFromSession(driverKey, driver, true)
			clone.stagedBurns = append([]campaignFixtureBurnStep(nil), publication.stagedBurns...)
			for index := range clone.stagedBurns {
				child := &clone.stagedBurns[index]
				if !child.run.isActive() {
					continue
				}
				e.moveFixtureLocked(child, &driver, driverKey, packet, &batch)
				child.run.fixtureBurn.publication = &clone
			}
			state.publication = &clone
			batch.publications = append(batch.publications, &clone)
			continue
		}
		if state.isRetiring || !run.isActive() {
			e.retireDetachedFixtureLocked(run, &batch)
			continue
		}
		step := state.cursor
		if publication != nil && !publication.isProduced && publication.burn != nil {
			step = *publication.burn
		} else if !state.isCursorReady {
			e.retireDetachedFixtureLocked(run, &batch)
			continue
		}
		e.moveFixtureLocked(&step, &driver, driverKey, packet, &batch)
		state.publication = nil
		state.cursor = step
		state.isCursorReady = true
		batch.steps = append(batch.steps, step)
	}
	if driverKey != "" {
		e.sessions[driverKey] = driver
	}
	return batch, false
}

func (e *gameplaySessionRegistry) moveFixtureLocked(step *campaignFixtureBurnStep, driver *gameplayPeerSession, key string, packet raknet.Packet, batch *fixtureHandoffBatch) {
	run := step.run
	state := run.fixtureBurn
	if state.publication != nil {
		state.publication.detachHandoffRetryLocked(batch)
	}
	run.mu.Lock()
	state.handoffGeneration++
	state.scheduleGeneration++
	if run.cancel != nil {
		batch.cancels = append(batch.cancels, run.cancel)
		run.cancel = nil
	}
	run.mu.Unlock()
	e.removeFixtureAliasesLocked(run)
	if driver.campaignNPCModifiers == nil {
		driver.campaignNPCModifiers = make(map[uint32]*campaignNPCModifierRun)
	}
	driver.campaignNPCModifiers[run.instanceID] = run
	state.blast.sessionKey = key
	state.blast.generation = driver.generation
	state.blast.packet = packet
	step.blast.sessionKey = key
	step.blast.generation = driver.generation
	step.blast.packet = packet
	step.handoffGeneration = state.handoffGeneration
}

func (e *gameplaySessionRegistry) removeFixtureAliasesLocked(run *campaignNPCModifierRun) {
	for key, member := range e.sessions {
		if member.campaignNPCModifiers[run.instanceID] == run {
			delete(member.campaignNPCModifiers, run.instanceID)
			e.sessions[key] = member
		}
	}
	for _, retained := range e.retainedSessions {
		if retained.peerSession.campaignNPCModifiers[run.instanceID] == run {
			delete(retained.peerSession.campaignNPCModifiers, run.instanceID)
		}
	}
}

func (e *gameplaySessionRegistry) retireDetachedFixtureLocked(run *campaignNPCModifierRun, batch *fixtureHandoffBatch) {
	state := run.fixtureBurn
	publication := state.publication
	if publication != nil {
		publication.detachHandoffRetryLocked(batch)
	}
	packets := make([][]byte, 0)
	if publication != nil && !publication.isCommitted && !publication.isFailed {
		packets = append(packets, publication.packets...)
		publication.packets = nil
	}
	packets = append(packets, state.pendingDeletes...)
	state.pendingDeletes = nil
	if !state.isRetiring {
		deletePacket, err := effectraknet.ModifierDelete(run.record.TargetObjectID, run.instanceID)
		if err != nil {
			if e.logger != nil {
				e.logger.Printf("Fixture departure delete failed instance=%d: %v", run.instanceID, err)
			}
		} else {
			packets = append(packets, deletePacket)
		}
	}
	output := campaignFixturePublication{blast: &state.blast}
	output.queueLocked(packets, true)
	state.handoffGeneration++
	run.mu.Lock()
	if run.cancel != nil {
		batch.cancels = append(batch.cancels, run.cancel)
		run.cancel = nil
	}
	run.mu.Unlock()
	step := campaignFixtureBurnStep{run: run, blast: state.blast}
	step.finalizeLocked()
}

func (e *campaignFixturePublication) detachHandoffRetryLocked(batch *fixtureHandoffBatch) {
	e.handoffRetryGeneration++
	e.isHandoffRetryScheduled = false
	if e.handoffRetryCancel != nil {
		batch.cancels = append(batch.cancels, e.handoffRetryCancel)
		e.handoffRetryCancel = nil
	}
}

// Stop retains the allocation and map entry until its final packets have a
// durable queue owner. The current publication remains visible to detach.
func (e campaignFixtureBurnStep) stop() ([][]byte, error) {
	registry := e.blast.runtime.registry
	registry.mutex.Lock()
	state := e.run.fixtureBurn
	if state.isRetiring || !e.run.isActive() {
		registry.mutex.Unlock()
		return nil, nil
	}
	state.isRetiring = true
	registry.mutex.Unlock()
	packet, err := effectraknet.ModifierDelete(e.targetID, e.run.instanceID)
	if err != nil {
		return nil, fmt.Errorf("fixtureBurnDelete: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e campaignFixtureBurnStep) finalizeLocked() {
	registry := e.blast.runtime.registry
	isCreated, err := e.run.release(e.blast.runtime.npc.modifierPool)
	if err != nil {
		e.blast.runtime.logger.Printf("Fixture final release failed instance=%d created=%t: %v", e.run.instanceID, isCreated, err)
		return
	}
	registry.removeFixtureAliasesLocked(e.run)
	e.run.fixtureBurn.publication = nil
	e.run.fixtureBurn.isCursorReady = false
}
