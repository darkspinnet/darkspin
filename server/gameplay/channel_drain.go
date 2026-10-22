package gameplay

import (
	"errors"
	"fmt"
	"github.com/darkspinnet/darkspin/server/raknet"
)

func (e campaignNPCActionRuntime) allocateDrainEffects(sessionKey string, generation uint64, objectIDs []uint32) (*channelEffects, error) {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.generation != generation || member.zone == nil || member.isZoneTerminal() {
		return nil, errors.New("channel owner unavailable")
	}
	effects, err := e.registry.allocateChannelEffectsLocked(member.zone, e.effectPool, objectIDs)
	if err != nil {
		return nil, fmt.Errorf("drainEffects: %w", err)
	}
	member.channelRegistry = e.registry
	e.registry.sessions[sessionKey] = member
	return effects, nil
}

func (e campaignNPCActionRuntime) publishDrainEffects(sessionKey string, generation uint64, sourceObjectID uint32,
	run *campaignNPCDrainRun, packets [][]byte,
) error {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.generation != generation || member.campaignNPCDrainRuns[sourceObjectID] != run || run.IsEnded() {
		return errors.New("channel publication replaced")
	}
	source, isFound := member.zone.NPCs().NPC(sourceObjectID)
	if !isFound || source.IsDefeated || source.ActionGeneration != run.actionGeneration {
		return errors.New("channel source defeated")
	}
	err := e.registry.publishChannelEffectsLocked(run.effects, packets)
	if err != nil {
		return fmt.Errorf("drainPublish: %w", err)
	}
	return nil
}

func (e campaignNPCActionRuntime) settleDrainEffects() {
	e.registry.mutex.Lock()
	e.registry.commitChannelRemovalsLocked()
	e.registry.mutex.Unlock()
}

type channelDrainFailure struct {
	runtime          campaignNPCActionRuntime
	sessionKey       string
	generation       uint64
	objectID         uint32
	actionGeneration uint64
	run              *campaignNPCDrainRun
}

func (e channelDrainFailure) fail(err error) {
	e.run.Stop()
	e.runtime.untrackHealthDrain(e.sessionKey, e.generation, e.objectID, e.run)
	e.runtime.releaseActionGeneration(e.sessionKey, e.generation, e.objectID, e.actionGeneration)
	e.runtime.settleDrainEffects()
	if e.runtime.logger != nil {
		e.runtime.logger.Printf("Channel scheduler retired source=%d: %v", e.objectID, err)
	}
}

func (e campaignNPCActionRuntime) scheduleDrainProducers(packet raknet.Packet, sessionKey string,
	generation uint64, objectID uint32, actionGeneration uint64, run *campaignNPCDrainRun, producers []raknet.ScheduledPacketProducer,
) (raknet.CancelSchedule, error) {
	if packet.ScheduleGroupResult == nil {
		return nil, errors.New("channel result scheduler unavailable")
	}
	failure := channelDrainFailure{runtime: e, sessionKey: sessionKey, generation: generation, objectID: objectID, actionGeneration: actionGeneration, run: run}
	guarded := e.registry.producerGuard.scheduledProducers(sessionKey, producers)
	cancel, err := packet.ScheduleGroupResult(guarded, failure.fail)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return nil, fmt.Errorf("drainSchedule: %w", err)
	}
	if cancel == nil {
		return nil, errors.New("channel cancellation unavailable")
	}
	run.bindCancel(cancel)
	return cancel, nil
}
