package gameplay

import (
	"fmt"

	memberraknet "github.com/darkspinnet/darkspin/server/zone/member/raknet103"
)

// rejoinPeerStatuses restores the loading state omitted by identity-only roster
// updates. Existing clients will not repeat their readiness report just because
// another client reconnects. Send it after the replacement client's zone setup.
func (r gameplayStatusRuntime) rejoinPeerStatuses(
	sessionKey string, peerSession gameplayPeerSession,
) ([][]byte, error) {
	if !peerSession.isRejoinPending {
		return nil, nil
	}
	r.registry.mutex.RLock()
	defer r.registry.mutex.RUnlock()
	currentSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || currentSession.generation != peerSession.generation ||
		!currentSession.isRejoinPending {
		return nil, nil
	}
	packets := make([][]byte, 0)
	for candidateKey, candidate := range r.registry.sessions {
		if candidateKey == sessionKey ||
			candidate.binding.GameID != currentSession.binding.GameID ||
			!candidate.isPlayerStatusKnown {
			continue
		}
		status := candidate.lastPlayerStatus
		packet, err := memberraknet.Status(memberraknet.StatusRequest{
			PlayerIndex: uint8(candidate.binding.Slot),
			Status:      status.Status, Progress: status.Progress,
		})
		if err != nil {
			return nil, fmt.Errorf("peerStatus[%d]: %w", candidate.binding.Slot, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}
