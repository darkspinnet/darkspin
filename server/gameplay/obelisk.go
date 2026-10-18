package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

func (e *gameplayPeerSession) spawnHealthObeliskCapsules(
	req game.CampaignScriptInvocation, sourceTime uint64, now time.Time,
	registry *gameplaySessionRegistry,
) ([][]byte, []uint32, error) {
	if e == nil || e.zone == nil || e.zone.DropRandom() == nil {
		return nil, nil, errors.New("obelisk drop runtime unavailable")
	}
	// The authored script requests one orb budget from the source challenge.
	// The shared generator already handles multiple capsules and rollback.
	packets, objectIDs, err := e.spawnCampaignHealthOrb(req, sourceTime, now, registry)
	if err != nil {
		return nil, nil, fmt.Errorf("obeliskCapsule: %w", err)
	}
	return packets, objectIDs, nil
}
