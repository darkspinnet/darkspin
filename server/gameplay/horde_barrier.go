package gameplay

import (
	"fmt"
	"time"

	zonebarrier "github.com/darkspinnet/darkspin/server/zone/barrier"
	barrierraknet "github.com/darkspinnet/darkspin/server/zone/barrier/raknet103"
)

func (e *gameplayPeerSession) openCampaignHordeBarriers(plans []zonebarrier.Plan) ([][]byte, error) {
	timestamp := uint64(0)
	if e.campaignClock != nil {
		clock := e.campaignClock
		elapsed := max(time.Duration(0), clock.now().Sub(clock.sourceStartedAt))
		timestamp = clock.sourceTime + uint64(elapsed/time.Millisecond)
	}
	packets, err := barrierraknet.Open(plans, timestamp)
	if err != nil {
		return nil, fmt.Errorf("barrierOpen: %w", err)
	}
	return packets, nil
}
