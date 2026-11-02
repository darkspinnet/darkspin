package gameplay

import "github.com/darkspinnet/darkspin/server/game"

// Arrival and ability producers publish the hero's movement hold to observers.
// Expiry is polled by the owning session; publish that same authoritative
// attribute transition to peers so their native mover can leave the hold too.
// The caller holds the registry lock and delivers packets to the owner itself.
func (e *gameplaySessionRegistry) queueHeroControlPeersLocked(
	sessionKey string, owner gameplayPeerSession, packets [][]byte,
) {
	if len(packets) == 0 {
		return
	}
	for candidateKey, candidate := range e.sessions {
		if candidateKey == sessionKey || candidate.isRejoinPending ||
			candidate.binding.GameID != owner.binding.GameID || !candidate.stage.IsDungeon() {
			continue
		}
		if owner.binding.Mode == game.ModeArena {
			if candidate.binding.Mode != game.ModeArena {
				continue
			}
			candidate.queuePackets(packets)
		} else {
			if owner.zone == nil || candidate.zone != owner.zone || !candidate.isCampaignPresentationAvailable() {
				continue
			}
			err := candidate.queueCampaignPresentation(packets)
			if err != nil && e.logger != nil {
				e.logger.Printf("RakNet co-op hero control publication failed user=%d: %v", candidate.binding.UserID, err)
			}
		}
		e.sessions[candidateKey] = candidate
	}
}
