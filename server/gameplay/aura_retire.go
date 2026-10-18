package gameplay

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

func (e heroAuraAreaSchedule) prepareNPCMembership(objectID, instanceID uint32, deadline time.Duration) ([][]byte, error) {
	messages := []raknet.ApplicationMessage{raknet.ModifierCreatedMessage{
		TargetID: objectID, ModifierGUID: e.definition.RootModifierID, InstanceID: instanceID,
		StackCount: 1, SourceID: e.sourceObjectID,
		StartMilliseconds: e.packet.SourceTime + uint64(deadline/time.Millisecond),
	}}
	if e.definition.HitEffectName != "" && !e.run.isTimeBubble {
		messages = append(messages, raknet.ServerEventMessage{Asset: util.HashID(e.definition.HitEffectName), ObjectID: objectID})
	}
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("auraEntryPacket[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

// Registry lock is held. Save/reload the working owner around queues.
func (e heroAuraAreaSchedule) queueNPCMembershipLocked(peerSession *gameplayPeerSession, packets [][]byte, isCreate bool) {
	e.runtime.registry.sessions[e.sessionKey] = *peerSession
	if isCreate {
		e.runtime.registry.queueTimeBubblePresentationLocked(e.run.originalZone, packets)
	} else {
		e.runtime.registry.queueProjectileRetirementLocked(e.run.originalZone, packets)
	}
	*peerSession = e.runtime.registry.sessions[e.sessionKey]
}

func (e *heroAuraAreaRun) releaseAuraInstance(instanceID uint32) {
	err := e.modifierPool.Release(instanceID)
	if err != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("RakNet aura modifier release failed instance=%d: %v", instanceID, err)
	}
}

func (e *heroAuraAreaRun) retainAuraLocked() {
	if e.runtime.registry.auraRuns == nil {
		e.runtime.registry.auraRuns = make(map[*heroAuraAreaRun]struct{})
	}
	e.runtime.registry.auraRuns[e] = struct{}{}
	retirement := auraRetirement{run: e}
	e.stopContext = context.AfterFunc(e.originalZone.Context(), retirement.execute)
}

// Caller owns the registry write lock; cleanup retains identity until encoded.
func (e *heroAuraAreaRun) retireAuraLocked() error {
	if e.isTimeBubble {
		err := e.retireTimeBubbleLocked(e.runtime.now())
		if err != nil {
			return fmt.Errorf("auraBubbleRetire: %w", err)
		}
		return nil
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isCleaned {
		return nil
	}
	e.isStopping = true
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	deletes := make([]heroAuraAreaTargetDelete, 0, len(e.targets))
	for objectID, target := range e.targets {
		deletes = append(deletes, heroAuraAreaTargetDelete{objectID: objectID, instanceID: target.instanceID})
	}
	packets, err := marshalAuraAreaDeletes(deletes)
	if err != nil {
		return fmt.Errorf("auraRetireTargets: %w", err)
	}
	if e.isObjectPublished {
		packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{e.objectID}})
		if err != nil {
			return fmt.Errorf("auraRetireField: %w", err)
		}
		packets = append(packets, packet)
	}
	e.runtime.registry.queueProjectileRetirementLocked(e.originalZone, packets)
	for objectID, target := range e.targets {
		e.clearStatus(objectID, target.expiresAt)
		e.releaseAuraInstance(target.instanceID)
	}
	e.targets = nil
	e.isCleaned = true
	if e.stopContext != nil {
		isStopped := e.stopContext()
		if !isStopped {
			// Dispatched cleanup observes this exact cleaned run.
		}
		e.stopContext = nil
	}
	delete(e.runtime.registry.auraRuns, e)
	return nil
}

type auraRetirement struct{ run *heroAuraAreaRun }

func (e auraRetirement) execute() {
	e.run.runtime.registry.mutex.Lock()
	err := e.run.retireAuraLocked()
	e.run.runtime.registry.mutex.Unlock()
	if err != nil {
		if e.run.runtime.logger != nil {
			e.run.runtime.logger.Printf("RakNet aura retirement deferred object=%d: %v", e.run.objectID, err)
		}
		e.schedule(heroAuraAreaScanInterval)
	}
}

func (e auraRetirement) schedule(delay time.Duration) {
	if e.run.runtime.registry.timer == nil {
		if delay == 0 {
			go e.execute()
		}
		// Failed encoding remains retained for a later explicit retirement.
		return
	}
	cancel, err := e.run.runtime.registry.timer.Schedule(delay, e.execute)
	if err != nil || cancel == nil {
		if cancel != nil {
			cancel()
		}
		if e.run.runtime.logger != nil {
			e.run.runtime.logger.Printf("RakNet aura retirement scheduling failed object=%d: %v", e.run.objectID, err)
		}
		if delay == 0 {
			go e.execute()
		}
	}
}

func (e *heroAuraAreaRun) stopAura() {
	e.mutex.Lock()
	if e.isCleaned || e.isStopping {
		e.mutex.Unlock()
		return
	}
	e.isStopping = true
	cancel := e.cancel
	e.cancel = nil
	e.mutex.Unlock()
	if cancel != nil {
		cancel()
	}
	auraRetirement{run: e}.schedule(0)
}

func (e *gameplayPeerSession) retireAuraAreasLocked() {
	e.retireTimeBubblesLocked()
	registry := (*gameplaySessionRegistry)(nil)
	for _, run := range e.heroAuraAreas {
		if run != nil {
			registry = run.runtime.registry
			break
		}
	}
	if registry == nil {
		return
	}
	for run := range registry.auraRuns {
		if run.originalZone != e.zone || run.cast.squad != e.squad ||
			run.cast.generation != e.generation || run.cast.userID != e.binding.UserID {
			continue
		}
		ownerSessionKey := ""
		for sessionKey, member := range registry.sessions {
			if member.zone == e.zone && member.binding.UserID == e.binding.UserID && member.generation == e.generation {
				ownerSessionKey = sessionKey
				registry.sessions[sessionKey] = *e
				break
			}
		}
		err := run.retireAuraLocked()
		if err != nil {
			if run.runtime.logger != nil {
				run.runtime.logger.Printf("RakNet aura caster retirement deferred: %v", err)
			}
			auraRetirement{run: run}.schedule(heroAuraAreaScanInterval)
		}
		if ownerSessionKey != "" {
			*e = registry.sessions[ownerSessionKey]
		}
	}
}
