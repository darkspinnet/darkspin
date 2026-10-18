package gameplay

import (
	"log"
	"time"

	"github.com/darkspinnet/darkspin/server/zone"
	zoneinteract "github.com/darkspinnet/darkspin/server/zone/interact"
	interactraknet "github.com/darkspinnet/darkspin/server/zone/interact/raknet103"
)

// campaignPickupSweep follows the shared zone lifetime, not the peer that
// happened to create a pickup. A collected pickup is absent by sweep time.
type campaignPickupSweep struct {
	registry *gameplaySessionRegistry
	zone     *zone.Zone
	logger   *log.Logger
}

func (r gameplaySetupRuntime) startCampaignPickupSweep(currentZone *zone.Zone) {
	if currentZone == nil || !currentZone.StartPickupSweep() {
		return
	}
	sweep := campaignPickupSweep{
		registry: r.registry, zone: currentZone, logger: r.logger,
	}
	go sweep.run()
}

func (e campaignPickupSweep) run() {
	clock := time.NewTicker(100 * time.Millisecond)
	defer clock.Stop()
	ctx := e.zone.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-clock.C:
			if !e.zone.IsActive() {
				return
			}
			e.expire(now)
		}
	}
}

func (e campaignPickupSweep) expire(now time.Time) {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	if !e.zone.IsActive() {
		return
	}
	expiredPickups := e.zone.Pickups().Expire(e.zone.Elapsed(now))
	for _, pickup := range expiredPickups {
		switch pickup.Kind {
		case zoneinteract.PickupOrb:
			e.zone.Orbs().Remove(pickup.ObjectID)
		case zoneinteract.PickupCrystal:
			e.zone.PickupPayload().RemoveCrystal(pickup.ObjectID)
		}
		packet, err := interactraknet.DeletePickup(pickup.ObjectID)
		if err != nil {
			if e.logger != nil {
				e.logger.Printf("RakNet campaign pickup expiry marshal failed object=%d: %v", pickup.ObjectID, err)
			}
			continue
		}
		for sessionKey, peerSession := range e.registry.sessions {
			if peerSession.zone != e.zone || peerSession.isZoneTerminal() {
				continue
			}
			publishErr := peerSession.publishPackets([][]byte{packet})
			if publishErr != nil && e.logger != nil {
				e.logger.Printf("RakNet campaign pickup expiry queued session=%s object=%d: %v", sessionKey, pickup.ObjectID, publishErr)
			}
			e.registry.sessions[sessionKey] = peerSession
		}
	}
}
