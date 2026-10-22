package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type mutationEliteSnapshot struct {
	hitPoint        float32
	maximumHitPoint float32
	bodyScale       float32
	affixID         uint32
	isDefeated      bool
}

type mutationEliteFrame struct {
	world            *zone.Zone
	objectID         uint32
	snapshot         mutationEliteSnapshot
	isModifierNeeded bool
	isPrepared       bool
}

func currentMutationElite(npc zonenpc.Snapshot) mutationEliteSnapshot {
	affixID := uint32(0)
	if name := npc.Plan.EliteModifierAffixName(); name != "" {
		affixID = util.HashID(name)
	}
	return mutationEliteSnapshot{
		hitPoint: npc.HitPoint, maximumHitPoint: npc.Plan.NPCProfile.HitPoint,
		bodyScale: npc.MutationBodyScale(), affixID: affixID,
		isDefeated: npc.IsDefeated || npc.HitPoint <= 0,
	}
}

func (e *gameplayPeerSession) queueMutationElite(frame mutationEliteFrame) {
	// A later full heal or stale-stat correction has its own stats-only frame;
	// it must neither consume an undelivered attachment nor inherit its flag.
	batches := make([]pendingPeerPacketBatch, 0, len(e.pendingPacketBatches)+1)
	for _, batch := range e.pendingPacketBatches {
		if batch.mutationElite != nil && batch.mutationElite.world == frame.world && batch.mutationElite.objectID == frame.objectID && batch.mutationElite.isModifierNeeded == frame.isModifierNeeded {
			continue
		}
		batches = append(batches, batch)
	}
	e.nextPendingPacketID++
	e.pendingPacketBatches = append(batches, pendingPeerPacketBatch{
		id: e.nextPendingPacketID, mutationElite: &frame, isCampaignPresentation: true,
	})
}

// The accepted permanent modifier belongs to the NPC, not the polling peer.
// Source aggregates must be stored before this registry-locked fanout.
func (e *gameplaySessionRegistry) queueMutationEliteLocked(world *zone.Zone, objectID uint32, isModifierNeeded bool) {
	frame := mutationEliteFrame{world: world, objectID: objectID, isModifierNeeded: isModifierNeeded}
	for key, member := range e.sessions {
		if member.zone != world {
			continue
		}
		member.queueMutationElite(frame)
		e.sessions[key] = member
	}
	for _, retained := range e.retainedSessions {
		if retained.peerSession.zone == world {
			retained.peerSession.queueMutationElite(frame)
		}
	}
}

func marshalMutationElite(frame mutationEliteFrame, snapshot mutationEliteSnapshot) ([][]byte, error) {
	messages := make([]raknet.ApplicationMessage, 0, 3)
	if !snapshot.isDefeated {
		messages = append(messages, raknet.AttributeDataUpdateMessage{
			ObjectID: frame.objectID, Value: map[uint8]float32{
				4: snapshot.maximumHitPoint, uint8(game.AttributeBodyScale): snapshot.bodyScale,
			},
		})
		if frame.isModifierNeeded {
			messages = append(messages, raknet.ModifierCreatedMessage{
				TargetID: frame.objectID, ModifierGUID: util.HashID(zonenpc.EliteModifierName),
				InstanceID:        zoneeffect.NounModifierInstanceID(frame.objectID),
				StartMilliseconds: 1, StackCount: 1,
				DurationMilliseconds: uint32(zonenpc.EliteModifierDuration.Milliseconds()),
				NPCAffixAssetID:      snapshot.affixID,
			})
		}
	}
	hitPoint := snapshot.hitPoint
	if snapshot.isDefeated {
		hitPoint = 0
	}
	messages = append(messages, raknet.CombatantDataDeltaMessage{
		ObjectID: frame.objectID, HitPoints: hitPoint, IsHitPointChanged: true,
	})
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("mutationFrame[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e *gameplaySessionRegistry) refreshMutationElitesLocked(member *gameplayPeerSession) error {
	batches := make([]pendingPeerPacketBatch, 0, len(member.pendingPacketBatches))
	replacements := make([]pendingPeerPacketBatch, 0)
	for _, batch := range member.pendingPacketBatches {
		pending := batch.mutationElite
		if pending == nil {
			batches = append(batches, batch)
			continue
		}
		if member.zone != pending.world {
			continue
		}
		npc, isFound := pending.world.NPCs().NPC(pending.objectID)
		if !isFound || !npc.IsPublished || !npc.Plan.IsEliteModifierApplied() {
			continue
		}
		next := *pending
		next.snapshot = currentMutationElite(npc)
		next.isPrepared = true
		packets, err := marshalMutationElite(next, next.snapshot)
		if err != nil {
			return fmt.Errorf("mutationPending: %w", err)
		}
		batch.packets = packets
		batch.mutationElite = &next
		if pending.isPrepared && pending.snapshot != next.snapshot {
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

func (e gameplayPeerSession) pendingMutationElites(batchID uint64) []mutationEliteFrame {
	frames := make([]mutationEliteFrame, 0)
	for _, batch := range e.pendingPacketBatches {
		if batch.id > batchID {
			break
		}
		if batch.mutationElite != nil {
			frames = append(frames, *batch.mutationElite)
		}
	}
	return frames
}

// Correct only current permanent state. HP correction must not replay the
// beam, infection hit effect, grow animation, or a new modifier attachment.
func (e *gameplaySessionRegistry) repairMutationDeliveryLocked(member *gameplayPeerSession, frames []mutationEliteFrame) {
	for _, frame := range frames {
		if member.zone != frame.world {
			continue
		}
		npc, isFound := frame.world.NPCs().NPC(frame.objectID)
		if !isFound || !npc.IsPublished || !npc.Plan.IsEliteModifierApplied() {
			continue
		}
		if frame.snapshot != currentMutationElite(npc) {
			frame.isModifierNeeded = false
			frame.isPrepared = false
			member.queueMutationElite(frame)
		}
	}
}
