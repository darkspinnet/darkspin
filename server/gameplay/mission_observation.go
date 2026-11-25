package gameplay

import (
	"context"
	"time"

	"github.com/darkspinnet/darkspin/server/activity"
	"github.com/darkspinnet/darkspin/server/game"
)

type campaignMissionProgression interface {
	ObserveCampaignMissionStart(context.Context, activity.MissionStart) error
}

// A start describes the accepted server handshake, not measured active time.
// Checkpoint continuations retain CompletionID and therefore the original start.
func (e gameplayStatusRuntime) observeCampaignMissionStart(ctx context.Context, peerSession gameplayPeerSession) {
	if peerSession.binding.Mode != game.ModeChain || peerSession.binding.IsServerBot ||
		peerSession.zone == nil || peerSession.binding.UserID == 0 || peerSession.binding.ChainLevelIndex == 0 {
		return
	}
	progression, isSupported := e.progression.(campaignMissionProgression)
	if !isSupported {
		return
	}
	levelIndex := peerSession.binding.ChainLevelIndex
	observationCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err := progression.ObserveCampaignMissionStart(observationCtx, activity.MissionStart{
		UserID: int64(peerSession.binding.UserID), ResultID: peerSession.zone.CompletionID(), At: time.Now().UTC(),
		CampaignNumber: (levelIndex-1)/4 + 1, LevelNumber: (levelIndex-1)%4 + 1,
		Map: peerSession.binding.Level, Difficulty: peerSession.binding.Difficulty,
	})
	if err != nil && e.logger != nil {
		// Metrics never reject an otherwise accepted mission start.
		e.logger.Printf("Campaign participation observation failed: %v", err)
	}
}
