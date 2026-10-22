package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
)

type plasmaWreathState struct {
	rank                  int32
	schedule              *plasmaWreathSchedule
	nextReactionID        uint64
	reactions             map[uint64]*plasmaWreathReaction
	energyDefenseIncrease float32
	attackSpeedIncrease   float32
	expiresAt             time.Time
	initialCancel         raknet.CancelSchedule
	tickCancel            raknet.CancelSchedule
	isAccepted            bool
	scheduleErr           error
}

func (e plasmaWreathState) cancel() {
	for _, reaction := range e.reactions {
		if reaction.cancel != nil {
			reaction.cancel()
		}
	}
	if e.initialCancel != nil {
		e.initialCancel()
	}
	if e.tickCancel != nil {
		e.tickCancel()
	}
}

func (e plasmaWreathState) isActive(now time.Time) bool {
	return e.expiresAt.IsZero() || now.Before(e.expiresAt)
}

func (e *gameplaySessionRegistry) plasmaWreathAttributePacket(
	peerSession gameplayPeerSession, now time.Time,
) ([]byte, error) {
	creature := e.projectPassiveCreature(peerSession, peerSession.deployedCreatureIndex, now)
	packet, err := plasmaWreathAttributePacket(peerSession.deployedObjectID, creature)
	if err != nil {
		return nil, fmt.Errorf("wreathProjection: %w", err)
	}
	return packet, nil
}

func plasmaWreathAttributePacket(objectID uint32, creature game.GameplayCreature) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: objectID,
		Value: map[uint8]float32{
			9: creature.EnergyDefense, 23: creature.TimingProfile.AttackSpeed,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("wreathAttribute: %w", err)
	}
	return packet, nil
}

func (e *gameplayPeerSession) stopPlasmaWreath(now time.Time) ([][]byte, error) {
	if e == nil {
		return nil, nil
	}
	e.rollbackPendingPlasmaWreath()
	if e.plasmaWreathRun == nil {
		return nil, nil
	}
	run := e.plasmaWreathRun
	packets, stopErr := run.Stop()
	// Deletion packets and their held slots travel together through the registry.
	// Returning deletes here would permit a later replay after those slots reuse.
	if len(packets) != 0 {
		e.plasmaWreathStoppedRuns = append(e.plasmaWreathStoppedRuns, run)
	}
	e.plasmaWreathState.cancel()
	e.plasmaWreathRun = nil
	e.plasmaWreathState = plasmaWreathState{}
	// The connection-local state cannot project allied auras. Its registry
	// operation completes this semantic restoration with the current world.
	e.plasmaWreathRestoreObjectID = e.deployedObjectID
	if stopErr != nil {
		return nil, fmt.Errorf("wreathStop: %w", stopErr)
	}
	return nil, nil
}

func (e plasmaWreathSchedule) scheduleProducers(producers []raknet.ScheduledPacketProducer) (raknet.CancelSchedule, error) {
	producers = e.runtime.registry.producerGuard.scheduledProducers(e.sessionKey, producers)
	var cancel raknet.CancelSchedule
	var err error
	if e.packet.ScheduleGroupResult != nil {
		cancel, err = e.packet.ScheduleGroupResult(producers, e.handleScheduleFailure)
	} else if e.packet.ScheduleGroup != nil {
		cancel, err = e.packet.ScheduleGroup(producers)
	} else {
		return nil, errors.New("Plasma Wreath group schedule unavailable")
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return nil, fmt.Errorf("wreathSchedule: %w", err)
	}
	if cancel == nil {
		return nil, errors.New("Plasma Wreath cancellation unavailable")
	}
	return cancel, nil
}

func (e plasmaWreathSchedule) handleScheduleFailure(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if isFound && peerSession.generation == e.generation && peerSession.plasmaWreathPending == e.admission {
		e.admission.scheduleErr = scheduleErr
		e.runtime.registry.mutex.Unlock()
		return
	}
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return
	}
	if !peerSession.plasmaWreathState.isAccepted {
		peerSession.plasmaWreathState.scheduleErr = scheduleErr
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		e.runtime.registry.mutex.Unlock()
		return
	}
	zone := peerSession.zone
	e.runtime.registry.mutex.Unlock()
	packets := e.retire("schedule failed")
	e.runtime.registry.mutex.Lock()
	for sessionKey, member := range e.runtime.registry.sessions {
		if member.zone != zone {
			continue
		}
		member.queueCampaignPackets(packets)
		e.runtime.registry.sessions[sessionKey] = member
	}
	e.runtime.registry.mutex.Unlock()
	e.runtime.logger.Printf("Plasma Wreath schedule source=%d: %v", e.sourceObjectID, scheduleErr)
}

func (e plasmaWreathSchedule) scheduleTick() error {
	cancel, err := e.scheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: 2 * time.Second, Produce: e.produceTick, AfterCommit: e.commitTick,
	}})
	if err != nil {
		return fmt.Errorf("wreathTick: %w", err)
	}
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if e.isCurrent(peerSession, isFound) {
		peerSession.plasmaWreathState.tickCancel = cancel
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	} else {
		cancel()
	}
	e.runtime.registry.mutex.Unlock()
	return nil
}

func (e plasmaWreathSchedule) commitTick() {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return
	}
	err := e.scheduleTick()
	if err != nil {
		e.handleScheduleFailure(err)
	}
}
