package gameplay

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

func isHeroInfectionCurrent(peerSession gameplayPeerSession, run *heroInfectionRun) bool {
	_, isFound := peerSession.heroInfections[run]
	return isFound
}

// acceptPrimary runs under the registry lock after scheduler admission. No old
// modifier or NPC status is changed by a rejected cast.
func (e heroInfectionSchedule) acceptPrimary(entry heroInfectionEntry, cancel raknet.CancelSchedule) ([][]byte, error) {
	// Marshal before committing replacement so serialization cannot leave a
	// partially accepted target. Instance zero is never published.
	packets := make([][]byte, 0, 2)
	replacedID, replacedRootID := e.run.npc.PlagueInstances(entry.objectID)
	for _, instanceID := range []uint32{replacedID, replacedRootID} {
		if instanceID == 0 {
			continue
		}
		packet, err := effectraknet.ModifierDelete(entry.objectID, instanceID)
		if err != nil {
			return nil, fmt.Errorf("replaceDelete: %w", err)
		}
		packets = append(packets, packet)
	}
	// Primary admission is serialized by the registry. Detached cleanup can
	// retire these old tokens, but cannot install another primary between reads.
	e.run.mutex.Lock()
	if e.run.isCleaned {
		e.run.mutex.Unlock()
		return nil, errors.New("infection stopped before acceptance")
	}
	replacedID, replacedRootID, isAccepted := e.run.npc.AcceptPlague(
		entry.objectID, entry.modifierInstanceID, entry.rootInstanceID,
		entry.rootExpiresAt, e.runtime.now(),
	)
	if !isAccepted {
		e.run.mutex.Unlock()
		return nil, errors.New("infection target unavailable at acceptance")
	}
	e.run.isAccepted = true
	e.run.cancel = cancel
	e.run.mutex.Unlock()
	for _, candidate := range e.runtime.registry.sessions {
		if candidate.zone == nil || candidate.zone.NPCs() != e.run.npc {
			continue
		}
		for run := range candidate.heroInfections {
			if run != e.run {
				run.retireReplaced(entry.objectID, replacedID, replacedRootID)
			}
		}
	}
	return packets, nil
}

// Disease and BioEntangle have separate Unique GUIDs. A spreading entry can
// coexist with the new primary and keeps the source snapshot of its own run.
func (e *heroInfectionRun) retireReplaced(objectID, instanceID, rootInstanceID uint32) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	entry, isFound := e.infections[objectID]
	if !isFound {
		return
	}
	if instanceID != 0 && entry.modifierInstanceID == instanceID {
		e.releaseReplaced(instanceID)
		entry.modifierInstanceID = 0
		entry.isConsumed = true
	}
	if rootInstanceID != 0 && entry.rootInstanceID == rootInstanceID {
		e.releaseReplaced(rootInstanceID)
		entry.rootInstanceID = 0
		entry.rootExpiresAt = time.Time{}
	}
	if entry.isConsumed && entry.rootInstanceID == 0 {
		delete(e.infections, objectID)
		return
	}
	e.infections[objectID] = entry
}

func (e *heroInfectionRun) releaseReplaced(instanceID uint32) {
	err := e.modifierPool.Release(instanceID)
	if err != nil {
		log.Printf("Infection replaced modifier release failed instance=%d: %v", instanceID, err)
	}
}
