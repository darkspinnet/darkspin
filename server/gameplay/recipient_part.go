package gameplay

import "github.com/darkspinnet/darkspin/server/game"

// campaignRecipientPartRequestLocked counts native participating players,
// excluding status 0x40 without requiring a living deployed hero. Callers hold
// the registry lock; retained membership prevents duplicate rejoin sessions.
func (e *gameplaySessionRegistry) campaignRecipientPartRequestLocked(
	recipient gameplayPeerSession,
) game.RecipientPartRequest {
	req := game.RecipientPartRequest{
		Progression:  recipient.binding.ChainProgression,
		MissionStage: recipient.binding.ChainLevelIndex,
		IsChain:      recipient.binding.Mode == game.ModeChain,
	}
	if recipient.zone == nil {
		return req
	}
	for _, member := range recipient.zone.Snapshot().Members {
		if member.IsServerBot {
			continue
		}
		for _, candidate := range e.sessions {
			if candidate.zone != recipient.zone || candidate.binding.UserID != member.UserID ||
				candidate.generation != member.PeerGeneration || candidate.lastPlayerStatus.Status&0x40 != 0 {
				continue
			}
			req.ParticipantCount++
			break
		}
	}
	return req
}
