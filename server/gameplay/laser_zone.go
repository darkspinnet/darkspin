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
	if peerSession.campaignNPCLaserZones[e.objectID] == e.laserZone {
		delete(peerSession.campaignNPCLaserZones, e.objectID)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	packets, err := e.laserZone.finish()
	if err != nil {
		return nil, fmt.Errorf("laserFinish: %w", err)
	}
	return packets, nil
}

// interruptCampaignNPCForForcedMovementLocked invalidates an active attack and
// immediately removes any connection-owned Laser Tank beam presentation. The
// caller must hold the gameplay registry mutex.
func (r *gameplaySessionRegistry) interruptCampaignNPCForForcedMovementLocked(
	sessionKey string, peerSession *gameplayPeerSession, objectID uint32,
) ([][]byte, error) {
	if r == nil || peerSession == nil || peerSession.zone == nil ||
		peerSession.zone.NPCs() == nil || objectID == 0 {
		return nil, nil
	}
	peerSession.zone.NPCs().ResetAction(objectID)
	laserZones := make(map[*campaignLaserZone]struct{})
	laserZone := peerSession.campaignNPCLaserZones[objectID]
	if laserZone != nil {
		laserZones[laserZone] = struct{}{}
		delete(peerSession.campaignNPCLaserZones, objectID)
	}
	for candidateSessionKey, candidateSession := range r.sessions {
		if candidateSessionKey == sessionKey || candidateSession.zone != peerSession.zone {
			continue
		}
		laserZone = candidateSession.campaignNPCLaserZones[objectID]
		if laserZone == nil {
			continue
		}
		laserZones[laserZone] = struct{}{}
		delete(candidateSession.campaignNPCLaserZones, objectID)
		r.sessions[candidateSessionKey] = candidateSession
	}
	packets := make([][]byte, 0)
	for activeLaserZone := range laserZones {
		cleanupPackets, err := activeLaserZone.finish()
		if err != nil {
			return packets, fmt.Errorf("forcedMovementLaserCleanup: %w", err)
		}
		packets = append(packets, cleanupPackets...)
	}
	return packets, nil
}
