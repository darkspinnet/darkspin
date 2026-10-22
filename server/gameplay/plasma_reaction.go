package gameplay

import (
	"errors"
)

type plasmaWreathReaction struct {
	schedule       plasmaWreathSchedule
	id             uint64
	targetObjectID uint32
	timestamp      uint64
	cancel         func()
}

// Called only for a committed incoming damage event while registry.mutex is
// held. Deferred feature work avoids recursively entering damage publication.
func (e campaignNPCActionRuntime) reservePlasmaWreathReactionLocked(
	peerSession *gameplayPeerSession, attackerObjectID uint32, timestamp uint64,
) {
	if peerSession == nil || peerSession.plasmaWreathRun == nil ||
		peerSession.plasmaWreathState.schedule == nil ||
		!isPlasmaWreathReactive(peerSession.plasmaWreathState.rank) ||
		!isPlasmaWreathTarget(*peerSession, attackerObjectID, true) {
		return
	}
	schedule := *peerSession.plasmaWreathState.schedule
	if !schedule.isCurrent(*peerSession, true) {
		return
	}
	if e.registry.timer == nil {
		e.logger.Printf("Plasma Wreath retaliation source=%d: timer unavailable", schedule.sourceObjectID)
		return
	}
	if peerSession.plasmaWreathState.reactions == nil {
		peerSession.plasmaWreathState.reactions = make(map[uint64]*plasmaWreathReaction)
	}
	peerSession.plasmaWreathState.nextReactionID++
	reaction := &plasmaWreathReaction{
		schedule: schedule, id: peerSession.plasmaWreathState.nextReactionID,
		targetObjectID: attackerObjectID, timestamp: timestamp,
	}
	peerSession.plasmaWreathState.reactions[reaction.id] = reaction
	cancel, err := e.registry.timer.Schedule(0, reaction.execute)
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		delete(peerSession.plasmaWreathState.reactions, reaction.id)
		e.logger.Printf("Plasma Wreath retaliation source=%d: %v", schedule.sourceObjectID, err)
		return
	}
	reaction.cancel = cancel
}

func (e *plasmaWreathReaction) execute() {
	e.schedule.runtime.registry.mutex.Lock()
	peerSession, isFound := e.schedule.runtime.registry.sessions[e.schedule.sessionKey]
	if !e.schedule.isCurrent(peerSession, isFound) ||
		peerSession.plasmaWreathState.reactions[e.id] != e {
		e.schedule.runtime.registry.mutex.Unlock()
		return
	}
	delete(peerSession.plasmaWreathState.reactions, e.id)
	zone := peerSession.zone
	e.schedule.runtime.registry.sessions[e.schedule.sessionKey] = peerSession
	e.schedule.runtime.registry.mutex.Unlock()
	packets, err := e.schedule.fireTarget(e.targetObjectID, e.timestamp, true)
	if err != nil {
		e.schedule.handleScheduleFailure(err)
		return
	}
	e.schedule.runtime.registry.mutex.Lock()
	for sessionKey, member := range e.schedule.runtime.registry.sessions {
		if member.zone != zone {
			continue
		}
		member.queueCampaignPackets(packets)
		e.schedule.runtime.registry.sessions[sessionKey] = member
	}
	e.schedule.runtime.registry.mutex.Unlock()
}
