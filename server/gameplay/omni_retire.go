package gameplay

import (
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
)

type omniShieldDelivery struct {
	userID      uint64
	generation  uint64
	isQueued    bool
	isCommitted bool
}

type omniShieldRemoval struct {
	world      *zone.Zone
	run        *omniShieldRun
	packets    [][]byte
	deliveries []*omniShieldDelivery
}

func (e *gameplayPeerSession) queueOmniShieldDeletion(packets [][]byte, delivery *omniShieldDelivery) {
	if delivery.isQueued {
		return
	}
	delivery.isQueued = true
	e.nextPendingPacketID++
	e.pendingPacketBatches = append(e.pendingPacketBatches, pendingPeerPacketBatch{
		id: e.nextPendingPacketID, packets: clonePendingPackets(packets), isCampaignPresentation: true, omniRemoval: delivery,
	})
}

func (e *gameplayPeerSession) retireOmniShieldsLocked(objectID uint32) {
	if e == nil {
		return
	}
	for _, run := range e.omniShields {
		if objectID == 0 || run.objectID == objectID {
			run.runtime.registry.retireOmniShieldsLocked(e, objectID)
			return
		}
	}
}

func (e *gameplayPeerSession) cancelPendingOmniShieldsLocked() {
	if e == nil {
		return
	}
	for _, run := range e.omniShields {
		if !run.isActive {
			run.runtime.registry.retireOmniShieldRunLocked(e, run)
		}
	}
}

// Final membership leave runs outside the registry lock. Ordinary disconnect
// does not call this wrapper; retained active children continue independently.
func (e *gameplayPeerSession) retireOmniShields() {
	if e == nil || e.omniShieldRegistry == nil {
		return
	}
	registry := e.omniShieldRegistry
	registry.mutex.Lock()
	registry.retireOmniShieldsLocked(e, 0)
	registry.commitOmniShieldRemovalsLocked()
	registry.mutex.Unlock()
}

// The local member may already be detached from the active registry map. Its
// deletion ticket must travel with that exact retained/local aggregate.
func (e *gameplaySessionRegistry) retireOmniShieldsLocked(member *gameplayPeerSession, objectID uint32) {
	if member == nil {
		return
	}
	for _, run := range member.omniShields {
		if objectID == 0 || run.objectID == objectID {
			e.retireOmniShieldRunLocked(member, run)
		}
	}
}

func (e *gameplaySessionRegistry) retireOmniShieldRunLocked(member *gameplayPeerSession, run *omniShieldRun) {
	if run.isRetired {
		return
	}
	packets := make([][]byte, 0, 2)
	if run.isActive {
		for _, message := range []raknet.ApplicationMessage{
			raknet.ModifierDeletedMessage{TargetID: run.objectID, InstanceID: run.instanceID},
			raknet.AttachedEffectMessage{ObjectID: run.objectID, Slot: run.effectSlot + 1, IsRemovalRequested: true, IsHardStop: true},
		} {
			packet, err := raknet.MarshalApplication(message)
			if err != nil {
				// Ownership remains discoverable for a later cleanup attempt.
				run.runtime.logger.Printf("Omni Shield removal encode failed instance=%d: %v", run.instanceID, err)
				return
			}
			packets = append(packets, packet)
		}
	}
	if !run.isPaid {
		key, owner, retained, isFound := run.ownerLocked()
		if member != nil {
			member.abilityReleaseSession().Rollback(run.releaseReservation)
		} else if isFound {
			owner.abilityReleaseSession().Rollback(run.releaseReservation)
			if retained != nil {
				retained.peerSession = owner
			} else {
				e.sessions[key] = owner
			}
		}
	}
	run.isRetired = true
	if run.activationCancel != nil {
		run.activationCancel()
	}
	if run.releaseCancel != nil && !run.isPaid {
		run.releaseCancel()
	}
	if run.expiryCancel != nil {
		run.expiryCancel()
	}
	if member != nil {
		delete(member.omniShields, run.instanceID)
	}
	for key, candidate := range e.sessions {
		if candidate.omniShields[run.instanceID] == run {
			delete(candidate.omniShields, run.instanceID)
			e.sessions[key] = candidate
		}
	}
	for _, retained := range e.retainedSessions {
		if retained.peerSession.omniShields[run.instanceID] == run {
			delete(retained.peerSession.omniShields, run.instanceID)
		}
	}
	if !run.isActive {
		run.releaseAllocation()
		return
	}
	if run.isArena {
		e.queueFieldMedicRealmPacketsLocked(nil, run.gameID, packets)
		run.releaseAllocation()
		return
	}
	removal := &omniShieldRemoval{world: run.world, run: run, packets: packets}
	for _, actor := range run.world.Snapshot().Members {
		removal.deliveries = append(removal.deliveries, &omniShieldDelivery{userID: actor.UserID, generation: actor.PeerGeneration})
	}
	if member != nil {
		delivery := removal.delivery(member.binding.UserID, member.generation)
		if delivery != nil {
			member.queueOmniShieldDeletion(packets, delivery)
		}
	}
	for key, candidate := range e.sessions {
		if candidate.zone != run.world {
			continue
		}
		delivery := removal.delivery(candidate.binding.UserID, candidate.generation)
		if delivery != nil && !delivery.isQueued {
			candidate.queueOmniShieldDeletion(packets, delivery)
			e.sessions[key] = candidate
		}
	}
	for _, retained := range e.retainedSessions {
		candidate := &retained.peerSession
		if candidate.zone != run.world {
			continue
		}
		delivery := removal.delivery(candidate.binding.UserID, candidate.generation)
		if delivery != nil {
			candidate.queueOmniShieldDeletion(packets, delivery)
		}
	}
	e.omniShieldRemovals = append(e.omniShieldRemovals, removal)
	e.commitOmniShieldRemovalsLocked()
}

func (e *omniShieldRemoval) delivery(userID, generation uint64) *omniShieldDelivery {
	for _, delivery := range e.deliveries {
		if delivery.userID == userID && delivery.generation == generation {
			return delivery
		}
	}
	return nil
}

func (e *gameplaySessionRegistry) commitOmniShieldRemovalsLocked() {
	removals := e.omniShieldRemovals[:0]
	for _, removal := range e.omniShieldRemovals {
		isPending := false
		for _, delivery := range removal.deliveries {
			if delivery.isCommitted {
				continue
			}
			for _, member := range removal.world.Snapshot().Members {
				if member.UserID == delivery.userID && member.PeerGeneration == delivery.generation {
					isPending = true
					break
				}
			}
			if isPending {
				break
			}
		}
		if isPending && !e.isClosed {
			removals = append(removals, removal)
		} else {
			removal.run.releaseAllocation()
		}
	}
	e.omniShieldRemovals = removals
}
