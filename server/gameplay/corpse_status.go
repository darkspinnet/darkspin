package gameplay

import (
	"fmt"

	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

// A corpse belongs to the shared zone, so effects applied by another player
// must retire alongside those owned by the player delivering the killing blow.
func (e campaignDamageRuntime) retireCorpseStatuses(
	sessionKey string, generation uint64, objectID uint32,
) ([][]byte, error) {
	if objectID == 0 {
		return nil, nil
	}
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.generation != generation || member.zone == nil {
		return nil, nil
	}
	npc := member.zone.NPCs()
	corpse, isCorpseFound := npc.NPC(objectID)
	if !isCorpseFound || !corpse.IsDefeated {
		return nil, nil
	}
	packets, err := e.registry.retireFixtureBurnsLocked(npc, objectID, e.npc.modifierPool)
	if err != nil {
		return packets, fmt.Errorf("corpseFixture: %w", err)
	}
	cloudPackets, err := e.registry.retireCloudPoisonLocked(npc, objectID, e.npc.modifierPool)
	if err != nil {
		return packets, fmt.Errorf("corpseCloud: %w", err)
	}
	packets = append(packets, cloudPackets...)
	packets = append(packets, e.registry.retireTargetInfectionRootsLocked(npc, objectID)...)
	basicPackets, err := e.registry.retireTargetBasicModifiersLocked(member, objectID, e.npc.modifierPool, false)
	if err != nil {
		return packets, fmt.Errorf("corpseBasic: %w", err)
	}
	packets = append(packets, basicPackets...)
	beastPacket, err := e.registry.retireBeastVulnerabilityLocked(npc, member.zone.Effect(), objectID, e.npc.modifierPool)
	if err != nil {
		return packets, fmt.Errorf("corpseBeast: %w", err)
	}
	if beastPacket != nil {
		packets = append(packets, beastPacket)
	}
	instanceIDs := make(map[uint32]bool)
	for _, modifier := range member.zone.Effect().Snapshot() {
		if modifier.TargetObjectID == objectID {
			instanceIDs[modifier.InstanceID] = true
		}
	}
	for key, owner := range e.registry.sessions {
		if owner.zone != member.zone {
			continue
		}
		for instanceID, run := range owner.campaignNPCModifiers {
			if run == nil || (!instanceIDs[instanceID] &&
				run.record.TargetObjectID != objectID && run.fearTargetObjectID != objectID) {
				continue
			}
			// Accepted fixture hits can already be in flight. Their retirement
			// owns the final deletion and allocation until that hit commits.
			if run.fixtureBurn != nil && run.fixtureBurn.isRetiring {
				continue
			}
			packet, marshalErr := effectraknet.ModifierDelete(objectID, instanceID)
			if marshalErr != nil {
				return packets, fmt.Errorf("corpseDelete: %w", marshalErr)
			}
			if run.cancel != nil {
				run.cancel()
				run.cancel = nil
			}
			isCreated, releaseErr := run.release(e.npc.modifierPool)
			if releaseErr != nil {
				return packets, fmt.Errorf("corpseRelease: %w", releaseErr)
			}
			owner.untrackCampaignNPCModifier(run)
			member.zone.Effect().Remove(instanceID)
			if isCreated {
				packets = append(packets, packet)
			}
		}
		e.registry.sessions[key] = owner
	}
	npc.ClearCorpseStatuses(objectID)
	return packets, nil
}
