package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	outcomeraknet "github.com/darkspinnet/darkspin/server/zone/outcome/raknet103"
)

// Queue independently for every member so already-defeated squads receive the
// result when the last surviving ally falls. The pending queue retains it until
// transport commit, without replaying mission failure on every poll.
func (e *gameplayPeerSession) queuePartyDefeat() error {
	if e.binding.Mode != game.ModeChain || e.zone == nil ||
		!e.dungeonSetup.IsCommitted() || e.isPartyDefeatQueued ||
		!e.zone.IsPartyDefeated() {
		return nil
	}
	packet, err := outcomeraknet.GameOver()
	if err != nil {
		return fmt.Errorf("partyDefeatMarshal: %w", err)
	}
	// Flush shared removals before changing the client's mission state.
	err = e.queueCampaignPresentation([][]byte{packet})
	if err != nil {
		return fmt.Errorf("partyDefeatPresentation: %w", err)
	}
	e.isPartyDefeatQueued = true
	return nil
}
