package gameplay

import (
	zone "github.com/darkspinnet/darkspin/server/zone"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	"log"
)

// Retire before the caster's original zone or registry entry changes. Each
// recipient owns its reliable deletion batch independently of that transport.
func (e *gameplaySessionRegistry) retirePlasmaWreathLocked(caster *gameplayPeerSession) {
	if caster == nil {
		return
	}
	caster.rollbackPendingPlasmaWreath()
	caster.queueCampaignPackets(e.takePlasmaWreathRestorationLocked(caster))
	if caster.plasmaWreathRun == nil {
		return
	}
	run := caster.plasmaWreathRun
	packets, stopErr := run.Stop()
	caster.plasmaWreathState.cancel()
	caster.plasmaWreathRun = nil
	caster.plasmaWreathState = plasmaWreathState{}
	caster.plasmaWreathRestoreObjectID = 0
	if stopErr != nil {
		log.Printf("Plasma Wreath lifecycle cleanup user=%d: %v", caster.binding.UserID, stopErr)
	}
	// Registry projection retains allied attack-speed auras when the Wreath's
	// own haste disappears. A connection-local projection omits those auras.
	attributePacket, attributeErr := e.plasmaWreathAttributePacket(*caster, e.now())
	if attributeErr != nil {
		log.Printf("Plasma Wreath lifecycle attributes user=%d: %v", caster.binding.UserID, attributeErr)
	} else {
		packets = append(packets, attributePacket)
	}
	e.queuePlasmaWreathRemovalsLocked(caster, run, packets)
}

// Complete a local death transition without publishing an incomplete aura
// baseline or retaining a registry dependency in the peer's gameplay state.
func (e *gameplaySessionRegistry) takePlasmaWreathRestorationLocked(member *gameplayPeerSession) [][]byte {
	if member == nil {
		return nil
	}
	// Preserve the original removal lease even if the controlled hero changed
	// before the registry completed the local death transition.
	for _, run := range member.plasmaWreathStoppedRuns {
		e.queuePlasmaWreathRemovalsLocked(member, run, run.PendingRemovals())
	}
	member.plasmaWreathStoppedRuns = nil
	if member.plasmaWreathRestoreObjectID == 0 {
		return nil
	}
	if member.plasmaWreathRestoreObjectID != member.deployedObjectID {
		member.plasmaWreathRestoreObjectID = 0
		return nil
	}
	packet, err := e.plasmaWreathAttributePacket(*member, e.now())
	if err != nil {
		log.Printf("Plasma Wreath death attributes user=%d: %v", member.binding.UserID, err)
		return nil
	}
	member.plasmaWreathRestoreObjectID = 0
	return [][]byte{packet}
}

// Called with registry.mutex held. Slots remain held through actual transport
// commit, because direct action responses can precede retained queue drains.
func (e *gameplaySessionRegistry) queuePlasmaWreathRemovalsLocked(caster *gameplayPeerSession, run *abilityraknet.PlasmaWreathRun, packets [][]byte) {
	if run == nil || caster == nil || len(packets) == 0 {
		return
	}
	removal := run.TakeRemovals()
	if removal == nil {
		return
	}
	publication := plasmaWreathPublication{removal: removal, world: caster.zone}
	delivery := &plasmaWreathDelivery{userID: caster.binding.UserID, generation: caster.generation}
	caster.queuePlasmaWreathDeletion(packets, delivery)
	publication.deliveries = append(publication.deliveries, delivery)
	for sessionKey, member := range e.sessions {
		if caster.zone == nil || member.zone != caster.zone || (member.binding.UserID == caster.binding.UserID && member.generation == caster.generation) {
			continue
		}
		delivery = &plasmaWreathDelivery{userID: member.binding.UserID, generation: member.generation}
		member.queuePlasmaWreathDeletion(packets, delivery)
		publication.deliveries = append(publication.deliveries, delivery)
		e.sessions[sessionKey] = member
	}
	e.plasmaWreathPublications = append(e.plasmaWreathPublications, publication)
}

type plasmaWreathDelivery struct {
	userID      uint64
	generation  uint64
	isCommitted bool
}
type plasmaWreathPublication struct {
	world      *zone.Zone
	removal    *abilityraknet.PlasmaWreathRemoval
	deliveries []*plasmaWreathDelivery
}

func (e *gameplaySessionRegistry) commitPlasmaWreathPublicationsLocked() {
	publications := e.plasmaWreathPublications[:0]
	for _, publication := range e.plasmaWreathPublications {
		isPending := false
		for _, delivery := range publication.deliveries {
			if delivery.isCommitted {
				continue
			}
			// Semantic membership survives disconnect/rejoin handover gaps,
			// including batches retained outside the active transport map.
			if publication.world != nil {
				for _, member := range publication.world.Snapshot().Members {
					if member.UserID == delivery.userID && member.PeerGeneration == delivery.generation {
						isPending = true
						break
					}
				}
			} else {
				for _, member := range e.sessions {
					if member.binding.UserID == delivery.userID && member.generation == delivery.generation {
						isPending = true
						break
					}
				}
			}

			if isPending {
				break
			}
		}
		if isPending && !e.isClosed {
			publications = append(publications, publication)
		} else {
			publication.removal.Commit()
		}
	}
	e.plasmaWreathPublications = publications
}

// Deletion leases cannot be evicted by ordinary presentation queue limits.
// There are at most sixteen reserved attachment slots per source object.
func (e *gameplayPeerSession) queuePlasmaWreathDeletion(packets [][]byte, delivery *plasmaWreathDelivery) {
	e.nextPendingPacketID++
	e.pendingPacketBatches = append(e.pendingPacketBatches, pendingPeerPacketBatch{id: e.nextPendingPacketID, packets: clonePendingPackets(packets), isCampaignPresentation: true, wreathRemoval: delivery})
}
