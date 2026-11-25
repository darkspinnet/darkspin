package gameplay

import (
	"encoding/binary"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e gameplayPeerSession) pruneCorpseAnimationBatches(batches []pendingPeerPacketBatch) []pendingPeerPacketBatch {
	for index, batch := range batches {
		if !e.isRetiredHasterBatch(batch) {
			continue
		}
		retainedBatches := make([]pendingPeerPacketBatch, 0, len(batches)-1)
		retainedBatches = append(retainedBatches, batches[:index]...)
		for _, remainingBatch := range batches[index+1:] {
			if !e.isRetiredHasterBatch(remainingBatch) {
				retainedBatches = append(retainedBatches, remainingBatch)
			}
		}
		return retainedBatches
	}
	return batches
}

func (e gameplayPeerSession) isRetiredHasterBatch(batch pendingPeerPacketBatch) bool {
	if len(batch.packets) == 0 || len(batch.cinematics) != 0 || batch.mutationFeedback != nil ||
		batch.mutationEvent != nil || batch.mutationElite != nil || batch.questFrame != nil ||
		batch.overdriveFrame != nil || batch.wreathRemoval != nil || batch.omniRemoval != nil ||
		batch.channelRemoval != nil || batch.channelCreation != nil || batch.actionTerminal != nil {
		return false
	}
	for _, packet := range batch.packets {
		if !e.isDefeatedHasterCast(packet) {
			return false
		}
	}
	return true
}

// A queued Haster cast can arrive after its death animation. Discard those
// exact cast states at the final drain, retaining death and released missiles.
func (e gameplayPeerSession) retainCorpseAnimations(packets [][]byte) [][]byte {
	for index, packet := range packets {
		if !e.isDefeatedHasterCast(packet) {
			continue
		}
		retainedPackets := make([][]byte, 0, len(packets)-1)
		retainedPackets = append(retainedPackets, packets[:index]...)
		for _, remainingPacket := range packets[index+1:] {
			if !e.isDefeatedHasterCast(remainingPacket) {
				retainedPackets = append(retainedPackets, remainingPacket)
			}
		}
		return retainedPackets
	}
	return packets
}

func (e gameplayPeerSession) isDefeatedHasterCast(packet []byte) bool {
	if len(packet) < 9 || raknet.PacketID(packet[0]) != raknet.SetAnimationState ||
		e.zone == nil || e.zone.NPCs() == nil {
		return false
	}
	state := binary.LittleEndian.Uint32(packet[5:9])
	if state != util.HashID("zlm_lieu_sp_03_attack1") && state != util.HashID("zlm_lieu_sp_03_attack2") {
		return false
	}
	objectID := binary.LittleEndian.Uint32(packet[1:5])
	actor, isFound := e.zone.NPCs().NPC(objectID)
	if !isFound || !actor.IsDefeated {
		return false
	}
	profile, isProfileFound := zonenpc.ActionProfileForPlan(actor.Plan)
	return isProfileFound && profile.Family == zonenpc.ActionZelemHaster
}
