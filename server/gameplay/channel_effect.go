package gameplay

import (
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
)

type channelEffectSlot struct {
	objectID uint32
	slot     uint8
}

// channelEffects owns one channel's exact attachments from allocation through
// their final recipient commits. Retire is safe while registry.mutex is held.
type channelEffects struct {
	mutex           sync.Mutex
	world           *zone.Zone
	pool            *attachedEffectPool
	slots           []channelEffectSlot
	creationPackets [][]byte
	isPublished     bool
	isRetired       bool
}

func (e *channelEffects) Retire() {
	if e == nil {
		return
	}
	e.mutex.Lock()
	e.isRetired = true
	e.mutex.Unlock()
}

func (e *channelEffects) Slots() []channelEffectSlot {
	if e == nil {
		return nil
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return append([]channelEffectSlot(nil), e.slots...)
}

// The caller holds registry.mutex. AllocateHeld makes pool ownership atomic
// even if unrelated actor cleanup is operating directly on the slot pool.
func (e *gameplaySessionRegistry) allocateChannelEffectsLocked(world *zone.Zone, pool *attachedEffectPool, objectIDs []uint32) (*channelEffects, error) {
	if e.isClosed || world == nil || pool == nil || len(objectIDs) == 0 {
		return nil, errors.New("channel effect allocation unavailable")
	}
	effects := &channelEffects{world: world, pool: pool}
	for _, objectID := range objectIDs {
		slot, isAllocated := pool.AllocateHeld(objectID)
		if !isAllocated {
			effects.commit()
			return nil, errors.New("channel attachment capacity unavailable")
		}
		effects.slots = append(effects.slots, channelEffectSlot{objectID: objectID, slot: slot})
	}
	e.channelEffects = append(e.channelEffects, effects)
	return effects, nil
}

// All slot-bearing creates and removals use the same recipient FIFO. A direct
// response must never publish a create after this channel's removal batch.
func (e *gameplaySessionRegistry) publishChannelEffectsLocked(effects *channelEffects, packets [][]byte) error {
	if effects == nil || len(packets) == 0 || e.isClosed {
		return errors.New("channel effect publication unavailable")
	}
	effects.mutex.Lock()
	defer effects.mutex.Unlock()
	if effects.isRetired || effects.isPublished {
		return errors.New("channel effect publication replaced")
	}
	effects.isPublished = true
	effects.creationPackets = clonePendingPackets(packets)
	for sessionKey, member := range e.sessions {
		if member.zone != effects.world || member.isZoneTerminal() {
			continue
		}
		member.queueChannelCreation(effects)
		member.channelRegistry = e
		e.sessions[sessionKey] = member
	}
	for _, retained := range e.retainedSessions {
		if retained.peerSession.zone == effects.world && !retained.peerSession.isZoneTerminal() {
			retained.peerSession.channelRegistry = e
			retained.peerSession.queueChannelCreation(effects)
		}
	}
	return nil
}

func (e *gameplayPeerSession) queueChannelCreation(effects *channelEffects) {
	for _, batch := range e.pendingPacketBatches {
		if batch.channelCreation == effects {
			return
		}
	}
	e.nextPendingPacketID++
	e.pendingPacketBatches = append(e.pendingPacketBatches, pendingPeerPacketBatch{
		id: e.nextPendingPacketID, packets: clonePendingPackets(effects.creationPackets),
		isCampaignPresentation: true, channelCreation: effects,
	})
}

// Rejoin already holds registry.mutex and passes the freshly indexed aggregate.
// A retired lease cannot append a create after its queued removal.
func (e *gameplaySessionRegistry) replayChannelEffectsLocked(member *gameplayPeerSession) {
	if member == nil {
		return
	}
	for _, effects := range e.channelEffects {
		if effects.world != member.zone {
			continue
		}
		effects.mutex.Lock()
		if effects.isPublished && !effects.isRetired {
			member.queueChannelCreation(effects)
			member.channelRegistry = e
		}
		effects.mutex.Unlock()
	}
}

type channelDelivery struct {
	userID      uint64
	generation  uint64
	isQueued    bool
	isCommitted bool
}

type channelRemoval struct {
	effects    *channelEffects
	packets    [][]byte
	deliveries []*channelDelivery
}

func (e *gameplayPeerSession) queueChannelRemoval(packets [][]byte, delivery *channelDelivery) {
	if delivery.isQueued || delivery.isCommitted {
		return
	}
	delivery.isQueued = true
	e.nextPendingPacketID++
	e.pendingPacketBatches = append(e.pendingPacketBatches, pendingPeerPacketBatch{
		id: e.nextPendingPacketID, packets: clonePendingPackets(packets),
		isCampaignPresentation: true, channelRemoval: delivery,
	})
}

func (e *channelEffects) removalPackets() ([][]byte, error) {
	packets := make([][]byte, 0, len(e.slots))
	for _, attachment := range e.slots {
		packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
			ObjectID: attachment.objectID, Slot: attachment.slot + 1,
			IsRemovalRequested: true, IsHardStop: true,
		})
		if err != nil {
			return nil, fmt.Errorf("channelRemoval: %w", err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e *channelEffects) commit() {
	for _, attachment := range e.slots {
		isCommitted := e.pool.CommitRemoval(attachment.objectID, attachment.slot)
		if !isCommitted {
			log.Printf("Channel attachment commit lost object=%d slot=%d", attachment.objectID, attachment.slot)
		}
	}
}

// The caller holds registry.mutex. Encoding failure retains the lease for the
// next drain; neither a marshal error nor queue pressure can release a slot.
func (e *gameplaySessionRegistry) commitChannelRemovalsLocked() {
	effectsList := e.channelEffects[:0]
	for _, effects := range e.channelEffects {
		effects.mutex.Lock()
		isRetired := effects.isRetired
		isPublished := effects.isPublished
		effects.mutex.Unlock()
		if !isRetired && !e.isClosed {
			effectsList = append(effectsList, effects)
			continue
		}
		if !isPublished || e.isClosed {
			effects.commit()
			continue
		}
		packets, err := effects.removalPackets()
		if err != nil {
			log.Printf("Channel removal retained after encoding failure: %v", err)
			effectsList = append(effectsList, effects)
			continue
		}
		removal := &channelRemoval{effects: effects, packets: packets}
		for _, member := range effects.world.Snapshot().Members {
			removal.deliveries = append(removal.deliveries, &channelDelivery{userID: member.UserID, generation: member.PeerGeneration})
		}
		e.channelRemovals = append(e.channelRemovals, removal)
	}
	e.channelEffects = effectsList
	removals := e.channelRemovals[:0]
	for _, removal := range e.channelRemovals {
		e.queuePendingChannelRemovalLocked(removal)
		if !e.isClosed && removal.isPending() {
			removals = append(removals, removal)
			continue
		}
		removal.effects.commit()
	}
	e.channelRemovals = removals
}

func (e *channelRemoval) delivery(userID uint64, generation uint64) *channelDelivery {
	for _, delivery := range e.deliveries {
		if delivery.userID == userID && delivery.generation == generation {
			return delivery
		}
	}
	return nil
}

func (e *gameplaySessionRegistry) queuePendingChannelRemovalLocked(removal *channelRemoval) {
	for sessionKey, member := range e.sessions {
		if member.zone != removal.effects.world {
			continue
		}
		delivery := removal.delivery(member.binding.UserID, member.generation)
		if delivery == nil || delivery.isQueued || delivery.isCommitted {
			continue
		}
		member.queueChannelRemoval(removal.packets, delivery)
		member.channelRegistry = e
		e.sessions[sessionKey] = member
	}
	for _, retained := range e.retainedSessions {
		member := &retained.peerSession
		if member.zone != removal.effects.world {
			continue
		}
		delivery := removal.delivery(member.binding.UserID, member.generation)
		if delivery != nil && !delivery.isQueued && !delivery.isCommitted {
			member.queueChannelRemoval(removal.packets, delivery)
			member.channelRegistry = e
		}
	}
}

func (e *channelRemoval) isPending() bool {
	for _, delivery := range e.deliveries {
		if delivery.isCommitted {
			continue
		}
		for _, member := range e.effects.world.Snapshot().Members {
			if member.UserID == delivery.userID && member.PeerGeneration == delivery.generation {
				return true
			}
		}
	}
	return false
}

// Called after transport/world detachment without registry.mutex held. The
// feature pointer survives pruning channel maps so departed deliveries settle.
func (e *gameplayPeerSession) settleChannelRemovals() {
	if e == nil || e.channelRegistry == nil {
		return
	}
	e.channelRegistry.mutex.Lock()
	e.channelRegistry.commitChannelRemovalsLocked()
	e.channelRegistry.mutex.Unlock()
}
