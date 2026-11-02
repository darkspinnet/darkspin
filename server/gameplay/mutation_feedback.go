package gameplay

import (
	"encoding/binary"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type mutationFeedbackTarget struct {
	objectID      uint32
	userID        uint64
	generation    uint64
	deathRevision uint64
	isHero        bool
	isDefeated    bool
}

type mutationFeedbackHit struct {
	objectID         uint32
	hitPoint         float32
	manaPoint        float32
	deathRevision    uint64
	isManaPointKnown bool
}

type mutationDamageFeedback struct {
	receipt             *mutationFeedbackReceipt
	world               *zone.Zone
	targets             []mutationFeedbackTarget
	packets             [][]byte
	hits                []mutationFeedbackHit
	transportGeneration uint64
	areEffectsAllowed   bool
	isPrepared          bool
}

func mutationFeedbackActor(world *zone.Zone, objectID uint32) (mutationFeedbackTarget, mutationFeedbackHit, bool) {
	for _, actor := range world.Hero().Snapshots() {
		if actor.ObjectID != objectID {
			continue
		}
		return mutationFeedbackTarget{objectID: objectID, userID: actor.UserID, generation: actor.PeerGeneration, isHero: true, deathRevision: actor.DeathRevision, isDefeated: actor.HitPoint <= 0},
			mutationFeedbackHit{objectID: objectID, hitPoint: actor.HitPoint, manaPoint: actor.ManaPoint, deathRevision: actor.DeathRevision, isManaPointKnown: true}, true
	}
	actor, isFound := world.Companion().Snapshot(objectID)
	if isFound {
		return mutationFeedbackTarget{objectID: objectID, userID: actor.UserID, generation: actor.PeerGeneration, deathRevision: actor.DeathRevision, isDefeated: actor.HitPoint <= 0},
			mutationFeedbackHit{objectID: objectID, hitPoint: actor.HitPoint, deathRevision: actor.DeathRevision}, true
	}
	// Reflected damage and reactions can update the attacking NPC as well.
	npc, isNPCFound := world.NPCs().NPC(objectID)
	if !isNPCFound || !npc.IsPublished {
		return mutationFeedbackTarget{}, mutationFeedbackHit{}, false
	}
	return mutationFeedbackTarget{objectID: objectID, generation: npc.Mutation.Generation, isDefeated: npc.HitPoint <= 0},
		mutationFeedbackHit{objectID: objectID, hitPoint: npc.HitPoint, manaPoint: npc.ManaPoint, isManaPointKnown: true}, true
}

func (e *gameplayPeerSession) queueMutationFeedback(frame mutationDamageFeedback) {
	if frame.receipt == nil {
		frame.receipt = &mutationFeedbackReceipt{}
	}
	e.nextPendingPacketID++
	e.pendingPacketBatches = append(append([]pendingPeerPacketBatch(nil), e.pendingPacketBatches...), pendingPeerPacketBatch{
		id: e.nextPendingPacketID, mutationFeedback: &frame, isCampaignPresentation: true,
	})
}

// Historical hit visuals belong only to transports ready at handoff. A new
// transport's baseline reconstructs HP instead of replaying earlier deaths.
func (e *gameplaySessionRegistry) queueMutationFeedbackLocked(world *zone.Zone, work zonenpc.MutationDamageWork, packets [][]byte) {
	frame := mutationDamageFeedback{world: world, packets: clonePendingPackets(gameplayPeerPresentationPackets(packets)), areEffectsAllowed: true}
	objects := make(map[uint32]bool)
	for index := uint32(0); index < work.TargetCount; index++ {
		target := work.Targets[index]
		if !target.IsCommitted || !target.IsComplete {
			continue
		}
		entry, current, isFound := mutationFeedbackActor(world, target.ObjectID)
		entry.objectID = target.ObjectID
		entry.userID = target.UserID
		entry.generation = target.PeerGeneration
		entry.deathRevision = target.DeathRevision
		entry.isDefeated = target.HitPoint <= 0
		if target.PreviousHitPoint > 0 && target.HitPoint <= 0 {
			entry.deathRevision++
		}
		if !isFound || current.deathRevision != entry.deathRevision {
			frame.areEffectsAllowed = false
		}
		frame.targets = append(frame.targets, entry)
		objects[target.ObjectID] = true
	}
	// Reactions may create or heal an additional actor. Keep their structural
	// packets, but refresh every encoded resource owner, not just aura victims.
	for _, packet := range frame.packets {
		if len(packet) < 6 || raknet.PacketID(packet[0]) != raknet.CombatantDataUpdate {
			continue
		}
		objectID := binary.LittleEndian.Uint32(packet[1:5])
		if objects[objectID] {
			continue
		}
		entry, current, isFound := mutationFeedbackActor(world, objectID)
		if !isFound || current.objectID != objectID {
			frame.areEffectsAllowed = false
			continue
		}
		frame.targets = append(frame.targets, entry)
		objects[objectID] = true
	}
	for key, member := range e.sessions {
		if member.zone != world || member.isRejoinPending || !member.dungeonSetup.IsCommitted() || !member.isCampaignPresentationAvailable() {
			continue
		}
		frame.transportGeneration = member.transportGeneration
		member.queueMutationFeedback(frame)
		e.sessions[key] = member
	}
}

func areMutationFeedbackHitsEqual(left, right []mutationFeedbackHit) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (e *gameplaySessionRegistry) mutationFeedbackHitsLocked(frame mutationDamageFeedback) ([]mutationFeedbackHit, bool) {
	hits := make([]mutationFeedbackHit, 0, len(frame.targets))
	areEffectsAllowed := frame.areEffectsAllowed
	for _, target := range frame.targets {
		actor, current, isFound := mutationFeedbackActor(frame.world, target.objectID)
		if !isFound || actor.userID != target.userID || actor.generation != target.generation {
			areEffectsAllowed = false
			continue
		}
		if current.deathRevision != target.deathRevision {
			areEffectsAllowed = false
		}
		if target.isDefeated && current.hitPoint > 0 {
			areEffectsAllowed = false
		}
		hits = append(hits, current)
	}
	return hits, areEffectsAllowed
}

func isMutationFeedbackLifeCurrent(frame mutationDamageFeedback, objectID uint32) bool {
	for _, target := range frame.targets {
		if target.objectID != objectID {
			continue
		}
		actor, current, isFound := mutationFeedbackActor(frame.world, objectID)
		return isFound && actor.userID == target.userID && actor.generation == target.generation &&
			current.deathRevision == target.deathRevision && (!target.isDefeated || current.hitPoint <= 0)
	}
	// Cleanup for separately owned objects is not a victim life transition.
	return true
}

func mutationFeedbackLifePacket(frame mutationDamageFeedback, packet []byte) []byte {
	if len(packet) == 0 {
		return nil
	}
	switch raknet.PacketID(packet[0]) {
	case raknet.SetAnimationState, raknet.ObjectPlayerMove:
		if len(packet) >= 5 && !isMutationFeedbackLifeCurrent(frame, binary.LittleEndian.Uint32(packet[1:5])) {
			return nil
		}
	case raknet.ObjectUpdate:
		if len(packet) == 8 && packet[5] == 17 && packet[7] == 0xff && !isMutationFeedbackLifeCurrent(frame, binary.LittleEndian.Uint32(packet[1:5])) {
			return nil
		}
	case raknet.ObjectDelete:
		filtered := []byte{packet[0]}
		for offset := 1; offset+4 <= len(packet); offset += 4 {
			objectID := binary.LittleEndian.Uint32(packet[offset : offset+4])
			actor, hit, isFound := mutationFeedbackActor(frame.world, objectID)
			// An already removed corpse still needs deletion on the original
			// transport. Only a currently reused life can make it unsafe.
			if !isFound || (actor.objectID == hit.objectID && isMutationFeedbackLifeCurrent(frame, objectID)) {
				filtered = append(filtered, packet[offset:offset+4]...)
			}
		}
		if len(filtered) == 1 {
			return nil
		}
		return filtered
	}
	return packet
}

func (e *gameplaySessionRegistry) refreshMutationFeedbackLocked(member *gameplayPeerSession) error {
	batches := make([]pendingPeerPacketBatch, 0, len(member.pendingPacketBatches))
	replacements := make([]pendingPeerPacketBatch, 0)
	for _, batch := range member.pendingPacketBatches {
		pending := batch.mutationFeedback
		if pending == nil {
			batches = append(batches, batch)
			continue
		}
		if pending.world != member.zone || pending.transportGeneration != member.transportGeneration || member.isRejoinPending {
			continue
		}
		next := *pending
		if next.receipt != nil && next.receipt.isCommitted {
			next = mutationFeedbackResourceRepair(next)
		}
		next.hits, next.areEffectsAllowed = e.mutationFeedbackHitsLocked(next)
		next.isPrepared = true
		packets := make([][]byte, 0, len(next.packets)+len(next.hits))
		for _, packet := range next.packets {
			if len(packet) == 0 {
				continue
			}
			packet = mutationFeedbackLifePacket(next, packet)
			if len(packet) == 0 {
				continue
			}
			// Replace captured resources with fresh authority. Preserve object
			// create/delete and cleanup packets when a historical hit expires.
			if raknet.PacketID(packet[0]) == raknet.CombatantDataUpdate {
				continue
			}
			isHit := raknet.PacketID(packet[0]) == raknet.CombatEvent && len(packet) > 1 && packet[1] == 0x9b
			isHitEffect := raknet.PacketID(packet[0]) == raknet.ServerEvent && len(packet) >= 6 && packet[1] == 6 && binary.LittleEndian.Uint32(packet[2:6]) == util.HashID("mutant_agent_infect_smoke_hit.ServerEventDef")
			if !next.areEffectsAllowed && (isHit || isHitEffect) {
				continue
			}
			packets = append(packets, packet)
		}
		for _, hit := range next.hits {
			packet, err := raknet.MarshalApplication(raknet.CombatantDataDeltaMessage{ObjectID: hit.objectID, HitPoints: hit.hitPoint, ManaPoints: hit.manaPoint, IsHitPointChanged: true, IsManaPointChanged: hit.isManaPointKnown})
			if err != nil {
				return fmt.Errorf("mutationFeedbackHP: %w", err)
			}
			packets = append(packets, packet)
		}
		if len(packets) == 0 {
			continue
		}
		batch.packets = packets
		batch.mutationFeedback = &next
		if pending.isPrepared && (!areMutationFeedbackHitsEqual(pending.hits, next.hits) || pending.areEffectsAllowed != next.areEffectsAllowed) {
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

func (e gameplayPeerSession) pendingMutationFeedbacks(batchID uint64) []mutationDamageFeedback {
	frames := make([]mutationDamageFeedback, 0)
	for _, batch := range e.pendingPacketBatches {
		if batch.id > batchID {
			break
		}
		if batch.mutationFeedback != nil {
			frames = append(frames, *batch.mutationFeedback)
		}
	}
	return frames
}

func (e *gameplaySessionRegistry) repairMutationFeedbackLocked(member *gameplayPeerSession, frames []mutationDamageFeedback) {
	for _, frame := range frames {
		if member.zone != frame.world || member.transportGeneration != frame.transportGeneration || member.isRejoinPending {
			continue
		}
		hits, areEffectsAllowed := e.mutationFeedbackHitsLocked(frame)
		for _, target := range frame.targets {
			if !isMutationFeedbackLifeCurrent(frame, target.objectID) {
				e.requireMutationBaselineLocked(member, target.objectID, "stale feedback life")
			}
		}
		if !areMutationFeedbackHitsEqual(frame.hits, hits) || frame.areEffectsAllowed != areEffectsAllowed {
			// A delivery receipt repairs resources, never repeats accepted
			// object creation, death, deletion, or reaction presentation.
			frame = mutationFeedbackResourceRepair(frame)
			member.queueMutationFeedback(frame)
		}
	}
}

// ReliableOrdered transport places the existing full baseline after the stale
// response. This repairs structural presentation eventually; it cannot retract
// a death pose or delete that has already reached the client.
func (e *gameplaySessionRegistry) requireMutationBaselineLocked(member *gameplayPeerSession, objectID uint32, reason string) {
	member.mutationBaselineRevision++
	member.isMutationBaselineRequired = true
	isRetained := false
	for _, retainedID := range member.mutationResetObjectIDs {
		if retainedID == objectID {
			isRetained = true
			break
		}
	}
	if !isRetained {
		member.mutationResetObjectIDs = append(append([]uint32(nil), member.mutationResetObjectIDs...), objectID)
	}
	if e.logger != nil {
		e.logger.Printf("RakNet mutation presentation baseline required game=%d user=%d revision=%d reason=%s",
			member.binding.GameID, member.binding.UserID, member.mutationBaselineRevision, reason)
	}
}
