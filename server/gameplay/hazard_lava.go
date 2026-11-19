package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

func (s *gameplayPeerSession) applyCampaignLavaContact(
	position game.Vec3, timestamp uint64, now time.Time,
	runtime campaignNPCActionRuntime,
) ([][]byte, sporenet.PlayerStatDelta, error) {
	if s == nil || s.isGodModeEnabled || s.isHeroArrivalProtected(now) {
		return nil, sporenet.PlayerStatDelta{}, nil
	}
	packets, delta, isContact, err := runtime.applyPlasmaPoolContact(s, position, timestamp, now)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("plasmaContact: %w", err)
	}
	// Selected authored pools own their contact volumes, damage and presentation.
	// The former coordinate-only Infinity circle damaged ordinary walkable ground.
	_ = isContact
	return packets, delta, nil
}
