package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// Shared by the cast's scheduled pulses; accessed under the registry mutex.
type campaignLaserZone struct {
	objectIDs []uint32
	isActive  bool
	origin    game.Vec3
	endpoint  game.Vec3
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

// interruptCampaignNPCForForcedMovementLocked invalidates an active attack and
// immediately removes any connection-owned Laser Tank beam presentation. The
// caller must hold the gameplay registry mutex.
func (r *gameplaySessionRegistry) interruptCampaignNPCForForcedMovementLocked(
	sessionKey string, peerSession *gameplayPeerSession, objectID uint32,
) ([][]byte, error) {
	interruption, err := r.prepareCampaignNPCForcedMovementLocked(peerSession, objectID)
	if err != nil {
		return nil, fmt.Errorf("forcedMovementPrepare: %w", err)
	}
	r.commitCampaignNPCForcedMovementLocked(sessionKey, peerSession, interruption)
	return interruption.packets, nil
}

type campaignNPCForcedMovementInterruption struct {
	objectID   uint32
	laserZones map[*campaignLaserController]struct{}
	packets    [][]byte
}

func (r *gameplaySessionRegistry) prepareCampaignNPCForcedMovementLocked(
	peerSession *gameplayPeerSession, objectID uint32,
) (campaignNPCForcedMovementInterruption, error) {
	interruption := campaignNPCForcedMovementInterruption{objectID: objectID}
	if r == nil || peerSession == nil || peerSession.zone == nil ||
		peerSession.zone.NPCs() == nil || objectID == 0 {
		return interruption, nil
	}
	interruption.laserZones = make(map[*campaignLaserController]struct{})
	laserZone := peerSession.campaignNPCLaserZones[objectID]
	if laserZone != nil {
		interruption.laserZones[laserZone] = struct{}{}
	}
	for _, candidateSession := range r.sessions {
		if candidateSession.zone != peerSession.zone {
			continue
		}
		laserZone = candidateSession.campaignNPCLaserZones[objectID]
		if laserZone == nil {
			continue
		}
		interruption.laserZones[laserZone] = struct{}{}
	}
	for laserZone := range interruption.laserZones {
		for _, beam := range laserZone.zones {
			if !beam.isActive {
				continue
			}
			cleanupPackets, err := npcraknet.LaserZoneEnd(beam.objectIDs)
			if err != nil {
				return campaignNPCForcedMovementInterruption{}, fmt.Errorf("forcedMovementLaserCleanup: %w", err)
			}
			interruption.packets = append(interruption.packets, cleanupPackets...)
		}
	}
	return interruption, nil
}

// Preparation and commit must share the registry lock so the captured beam runs
// cannot change between encoding cleanup and invalidating the old attack.
func (r *gameplaySessionRegistry) commitCampaignNPCForcedMovementLocked(
	sessionKey string, peerSession *gameplayPeerSession,
	interruption campaignNPCForcedMovementInterruption,
) {
	if r == nil || peerSession == nil || peerSession.zone == nil ||
		peerSession.zone.NPCs() == nil || interruption.objectID == 0 {
		return
	}
	peerSession.zone.NPCs().ResetAction(interruption.objectID)
	delete(peerSession.campaignNPCLaserZones, interruption.objectID)
	for candidateSessionKey, candidateSession := range r.sessions {
		if candidateSessionKey == sessionKey || candidateSession.zone != peerSession.zone {
			continue
		}
		delete(candidateSession.campaignNPCLaserZones, interruption.objectID)
		r.sessions[candidateSessionKey] = candidateSession
	}
	for laserZone := range interruption.laserZones {
		for _, beam := range laserZone.zones {
			beam.isActive = false
		}
	}
}
