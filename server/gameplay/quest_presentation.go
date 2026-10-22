package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/zone"
	interactraknet "github.com/darkspinnet/darkspin/server/zone/interact/raknet103"
	questraknet "github.com/darkspinnet/darkspin/server/zone/quest/raknet103"
)

// Quest object IDs belong to this exact world and are never reused. A queued
// frame is refreshed at delivery, including after a separately built baseline.
type questPendingFrame struct {
	world        *zone.Zone
	objectID     uint32
	movementType uint8
	isCollected  bool
	isPrepared   bool
}

func (e *gameplayPeerSession) queueQuestFrame(frame questPendingFrame) {
	batches := make([]pendingPeerPacketBatch, 0, len(e.pendingPacketBatches)+1)
	for _, batch := range e.pendingPacketBatches {
		if batch.questFrame != nil && batch.questFrame.world == frame.world && batch.questFrame.objectID == frame.objectID {
			frame.isCollected = frame.isCollected || batch.questFrame.isCollected
			continue
		}
		batches = append(batches, batch)
	}
	e.nextPendingPacketID++
	e.pendingPacketBatches = append(batches, pendingPeerPacketBatch{
		id: e.nextPendingPacketID, questFrame: &frame, isCampaignPresentation: true,
	})
}

// The caller holds registry.mutex and must store any local source aggregate
// before fanout. Pending baseline peers use the same guarded campaign FIFO.
func (e *gameplaySessionRegistry) queueQuestFrameLocked(world *zone.Zone, objectID uint32, isCollected bool) {
	frame := questPendingFrame{world: world, objectID: objectID, isCollected: isCollected}
	for key, member := range e.sessions {
		if member.zone != world {
			continue
		}
		member.queueQuestFrame(frame)
		e.sessions[key] = member
	}
	for _, retained := range e.retainedSessions {
		if retained.peerSession.zone == world {
			retained.peerSession.queueQuestFrame(frame)
		}
	}
}

func (e *gameplaySessionRegistry) refreshQuestFramesLocked(member *gameplayPeerSession, at time.Time, sourceTime uint64) error {
	batches := make([]pendingPeerPacketBatch, 0, len(member.pendingPacketBatches))
	replacements := make([]pendingPeerPacketBatch, 0)
	for _, batch := range member.pendingPacketBatches {
		pending := batch.questFrame
		if pending == nil {
			batches = append(batches, batch)
			continue
		}
		if pending.world != member.zone {
			continue
		}
		next := *pending
		crystal, isFound := pending.world.QuestCrystals().Crystal(pending.objectID, at)
		next.isCollected = pending.isCollected || !isFound
		next.movementType = 0
		if next.isCollected {
			packet, err := interactraknet.DeletePickup(pending.objectID)
			if err != nil {
				return fmt.Errorf("questPendingDelete: %w", err)
			}
			batch.packets = [][]byte{packet}
		} else {
			packets, err := questraknet.SpawnAt(crystal, at, sourceTime)
			if err != nil {
				return fmt.Errorf("questPendingSpawn: %w", err)
			}
			batch.packets = packets
			next.movementType = crystal.Flight.MovementType
		}
		next.isPrepared = true
		batch.questFrame = &next
		if pending.isPrepared && (pending.isCollected != next.isCollected || pending.movementType != next.movementType) {
			replacements = append(replacements, batch)
			continue
		}
		batches = append(batches, batch)
	}
	for _, batch := range replacements {
		member.nextPendingPacketID++
		batch.id = member.nextPendingPacketID
		batches = append(batches, batch)
	}
	member.pendingPacketBatches = batches
	return nil
}

func (e gameplayPeerSession) pendingQuestFrames(batchID uint64) []questPendingFrame {
	frames := make([]questPendingFrame, 0)
	for _, batch := range e.pendingPacketBatches {
		if batch.id > batchID {
			break
		}
		if batch.questFrame != nil {
			frames = append(frames, *batch.questFrame)
		}
	}
	return frames
}

// A prepared create can be emitted after collection or after a newer settled
// baseline. Its transport receipt reserves a current correction in that case.
// An unchanged in-flight lob already carries its native absolute clock.
func (e *gameplaySessionRegistry) repairQuestDeliveryLocked(member *gameplayPeerSession, frames []questPendingFrame, at time.Time) {
	for _, frame := range frames {
		if member.zone != frame.world || frame.isCollected {
			continue
		}
		crystal, isFound := frame.world.QuestCrystals().Crystal(frame.objectID, at)
		if !isFound {
			frame.isCollected = true
			member.queueQuestFrame(frame)
		} else if crystal.Flight.MovementType != frame.movementType {
			frame.isPrepared = false
			member.queueQuestFrame(frame)
		}
	}
}
