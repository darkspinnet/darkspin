package gameplay

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
)

// Registry-owned identity captured before scheduling. Publication is recorded
// after a successful launch producer or admission with an immediate launch.
type campaignProjectileRetirement struct {
	originalZone         *zone.Zone
	generation           uint64
	sessionKey           string
	objectID             uint32
	run                  *abilityraknet.ProjectileRun
	isAdmitted           bool
	isPublished          bool
	isRetired            bool
	isRetirementPending  bool
	activeProducerCount  int
	packetBatches        [][][]byte
	extraPackets         [][]byte
	controlPacketBatches []timeBubbleControlBatch
}

// retireCampaignProjectileLocked queues reliable cleanup before retiring the
// identity. The scheduler may discard every returned packet when a producer
// fails, so cleanup never depends on that producer's output.
func (e *gameplaySessionRegistry) retireCampaignProjectileLocked(req *campaignProjectileRetirement) error {
	if req == nil || req.run == nil || req.isRetired {
		return nil
	}
	if req.activeProducerCount > 0 {
		req.isRetirementPending = true
		return nil
	}
	if !req.isAdmitted {
		req.packetBatches = nil
		req.isRetired = true
		delete(e.projectileRetirements, req.run)
		e.untrackRetiredProjectileLocked(req)
		req.run.Stop()
		return nil
	}
	for _, member := range e.sessions {
		if member.zone != req.originalZone {
			continue
		}
		heroRun := member.sageAttacks[req.objectID]
		enemyRun := member.campaignNPCProjectiles[req.objectID]
		if (heroRun != nil && heroRun != req.run) || (enemyRun != nil && enemyRun != req.run) {
			// This callback belongs to an older flight using a recycled object ID.
			req.isRetired = true
			delete(e.projectileRetirements, req.run)
			e.untrackRetiredProjectileLocked(req)
			req.run.Stop()
			return nil
		}
	}
	var fallbackPacket []byte
	var err error
	if req.isPublished {
		fallbackPacket, err = raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{req.objectID}})
		if err != nil {
			return fmt.Errorf("retireDeleteEncode: %w", err)
		}
	}
	packets, isDeleted, deleteErr := req.run.DeleteProjectile(context.Background())
	if req.isPublished && (!isDeleted || deleteErr != nil) {
		// A failed dispatch may already have consumed the simulator's active
		// flag. The retained published identity still owns this one deletion.
		packets = [][]byte{fallbackPacket}
	}
	if !req.isPublished {
		packets = nil
	}
	freezeErr := e.retireProjectileFreezesLocked(req)
	e.retireTimeBubbleProjectileLocked(req.originalZone, req.objectID, req.run, e.now())
	if req.isPublished {
		e.queueProjectileRetirementLocked(req.originalZone, req.extraPackets)
	}
	e.queueProjectileRetirementLocked(req.originalZone, packets)
	req.isRetired = true
	delete(e.projectileRetirements, req.run)
	e.untrackRetiredProjectileLocked(req)
	req.run.Stop()
	retirementErr := errors.Join(deleteErr, freezeErr)
	if retirementErr != nil {
		return fmt.Errorf("retireCleanup: %w", retirementErr)
	}
	return nil
}

func (e *gameplaySessionRegistry) beginCampaignProjectilePublicationLocked(req *campaignProjectileRetirement) bool {
	if req == nil || req.isRetired || req.isRetirementPending {
		return false
	}
	if req.isAdmitted {
		e.projectileRetirements[req.run] = req
	}
	req.activeProducerCount++
	return true
}

func (e *gameplaySessionRegistry) finishCampaignProjectilePublicationLocked(
	req *campaignProjectileRetirement, sessionKey string, packets [][]byte, produceErr error,
) error {
	req.activeProducerCount--
	if produceErr != nil {
		req.isRetirementPending = true
	} else if !req.isAdmitted {
		// A zero-delay callback can run while its factory is still admitting.
		// Preserve initial activation before this callback's launch/impact.
		req.packetBatches = append(req.packetBatches, clonePendingPackets(packets))
	} else {
		e.queueCampaignProjectilePublicationLocked(req, sessionKey, packets)
		if req.isPublished {
			member, isFound := e.sessions[sessionKey]
			isCurrent := isFound && member.generation == req.generation && member.zone == req.originalZone &&
				(member.sageAttacks[req.objectID] == req.run || member.campaignNPCProjectiles[req.objectID] == req.run)
			if !isCurrent || !req.run.Snapshot(e.now()).IsActive {
				// Keep committed impact consequences, then remove any active
				// presentation left behind by an owner departure during produce.
				req.isRetirementPending = true
			}
		}
	}
	if req.activeProducerCount == 0 {
		if req.isPublished {
			for _, controlBatch := range req.controlPacketBatches {
				if len(controlBatch.releases) != 0 {
					e.queueProjectileRetirementLocked(req.originalZone, controlBatch.packets)
				} else {
					e.queueTimeBubblePresentationLocked(req.originalZone, controlBatch.packets)
				}
			}
		} else {
			// A terminal producer already deleted the flight. Drop staged creates
			// and trajectories, then retire exact source memberships without
			// recreating presentation after that deletion.
			e.retireTimeBubbleProjectileLocked(req.originalZone, req.objectID, req.run, e.now())
		}
		for _, controlBatch := range req.controlPacketBatches {
			e.releaseTimeBubbleInstancesLocked(controlBatch.releases)
		}
		req.controlPacketBatches = nil
	}
	if req.isRetirementPending && req.activeProducerCount == 0 {
		err := e.retireCampaignProjectileLocked(req)
		if err != nil {
			return fmt.Errorf("publicationRetire: %w", err)
		}
	}
	if !req.isPublished && !req.run.Snapshot(e.now()).IsActive {
		freezeErr := e.retireProjectileFreezesLocked(req)
		if freezeErr != nil {
			return fmt.Errorf("publicationFreezeRetire: %w", freezeErr)
		}
		delete(e.projectileRetirements, req.run)
	}
	return nil
}

func (e *gameplaySessionRegistry) admitCampaignProjectilePublicationLocked(req *campaignProjectileRetirement, sessionKey string) {
	req.isAdmitted = true
	req.sessionKey = sessionKey
	if e.projectileRetirements == nil {
		e.projectileRetirements = make(map[*abilityraknet.ProjectileRun]*campaignProjectileRetirement)
	}
	e.projectileRetirements[req.run] = req
	for _, packets := range req.packetBatches {
		e.queueCampaignProjectilePublicationLocked(req, sessionKey, packets)
	}
	req.packetBatches = nil
}

func (e *gameplaySessionRegistry) queueCampaignProjectilePublicationLocked(
	req *campaignProjectileRetirement, ownerSessionKey string, packets [][]byte,
) {
	if req.isRetired || req.originalZone == nil || len(packets) == 0 {
		return
	}
	presentations := gameplayPeerPresentationPackets(packets)
	for sessionKey, member := range e.sessions {
		if member.zone != req.originalZone {
			continue
		}
		if sessionKey == ownerSessionKey && member.generation == req.generation {
			member.queueCampaignPackets(packets)
		} else {
			if member.isRejoinPending || !member.isCampaignPresentationAvailable() {
				continue
			}
			err := member.queueCampaignPresentation(presentations)
			if err != nil && e.logger != nil {
				e.logger.Printf("RakNet projectile publication prerequisite failed projectile=%d user=%d: %v", req.objectID, member.binding.UserID, err)
			}
		}
		e.sessions[sessionKey] = member
	}
	req.recordPublication(packets)
}

// Track the queued wire lifetime rather than the simulator's active flag:
// teardown can stop the run after produce unlocks but before publication.
func (e *campaignProjectileRetirement) recordPublication(packets [][]byte) {
	for _, packet := range packets {
		if len(packet) < 5 {
			continue
		}
		switch raknet.PacketID(packet[0]) {
		case raknet.ObjectCreate:
			if binary.LittleEndian.Uint32(packet[1:5]) == e.objectID {
				e.isPublished = true
			}
		case raknet.ObjectDelete:
			for offset := 1; offset+4 <= len(packet); offset += 4 {
				if binary.LittleEndian.Uint32(packet[offset:offset+4]) == e.objectID {
					e.isPublished = false
				}
			}
		}
	}
}

func (e *gameplaySessionRegistry) untrackRetiredProjectileLocked(req *campaignProjectileRetirement) {
	for sessionKey, member := range e.sessions {
		if member.zone != req.originalZone {
			continue
		}
		if member.sageAttacks[req.objectID] == req.run {
			delete(member.sageAttacks, req.objectID)
		}
		member.untrackCampaignNPCProjectile(req.objectID, req.run)
		e.sessions[sessionKey] = member
	}
}

func (e *gameplaySessionRegistry) queueProjectileRetirementLocked(originalZone *zone.Zone, packets [][]byte) {
	if originalZone == nil || len(packets) == 0 {
		return
	}
	for sessionKey, member := range e.sessions {
		if member.zone != originalZone {
			continue
		}
		member.queueCampaignPackets(packets)
		e.sessions[sessionKey] = member
	}
}
