package gameplay

import (
	"fmt"

	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// Shared by the cast's scheduled pulses; accessed under the registry mutex.
type campaignLaserZone struct {
	objectIDs []uint32
	isActive  bool
}

func (e *campaignLaserZone) finish() ([][]byte, error) {
	if e == nil || !e.isActive {
		return nil, nil
	}
	packets, err := npcraknet.LaserZoneEnd(e.objectIDs)
	if err != nil {
		return nil, fmt.Errorf("laserCleanup: %w", err)
	}
	e.isActive = false
	return packets, nil
}

func (e campaignConeSchedule) finishLaserZone() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || peerSession.generation != e.generation {
		return nil, nil
	}
	packets, err := e.laserZone.finish()
	if err != nil {
		return nil, fmt.Errorf("laserFinish: %w", err)
	}
	return packets, nil
}
