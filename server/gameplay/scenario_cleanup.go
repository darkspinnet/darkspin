//go:build scenario

package gameplay

import (
	"bytes"

	"github.com/darkspinnet/darkspin/server/game"
)

// Capture before retiring any producer: some owners already publish their
// removals to every recipient, while legacy owners only queue local cleanup.
func (e *gameplaySessionRegistry) scenarioCleanupBatchIDsLocked(gameID uint32) map[string]uint64 {
	batchIDs := make(map[string]uint64)
	for key, member := range e.sessions {
		if member.binding.Mode == game.ModeArena && member.binding.GameID == gameID {
			batchIDs[key] = member.nextPendingPacketID
		}
	}
	return batchIDs
}

func hasScenarioCleanupPacket(member gameplayPeerSession, previousBatchID uint64, packet []byte) bool {
	for _, batch := range member.pendingPacketBatches {
		if batch.id <= previousBatchID {
			continue
		}
		for _, queued := range batch.packets {
			if bytes.Equal(queued, packet) {
				return true
			}
		}
	}
	return false
}

// This comparison is scoped to one retirement, never the general packet
// queue. An identical packet from an earlier operation must still be sent.
func (e *gameplaySessionRegistry) queueScenarioCleanupLocked(
	gameID uint32, ownerUserID uint64, packets [][]byte, previousBatchIDs map[string]uint64,
) {
	for key, member := range e.sessions {
		if member.binding.Mode != game.ModeArena || member.binding.GameID != gameID ||
			member.binding.UserID == ownerUserID || member.isRejoinPending {
			continue
		}
		previousBatchID, isCaptured := previousBatchIDs[key]
		missingPackets := make([][]byte, 0, len(packets))
		for _, packet := range packets {
			if isCaptured && hasScenarioCleanupPacket(member, previousBatchID, packet) {
				continue
			}
			missingPackets = append(missingPackets, packet)
		}
		member.queuePackets(missingPackets)
		e.sessions[key] = member
	}
}
