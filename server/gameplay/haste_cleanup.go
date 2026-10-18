package gameplay

import (
	"fmt"

	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

func (e campaignDamageRuntime) stopNPCHaste(sessionKey string, generation uint64, objectID uint32) ([][]byte, error) {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	peer, isFound := e.registry.sessions[sessionKey]
	if !isFound || peer.generation != generation || peer.zone == nil {
		return nil, nil
	}
	packets := make([][]byte, 0)
	for key, member := range e.registry.sessions {
		if member.zone != peer.zone {
			continue
		}
		for _, run := range member.campaignNPCModifiers {
			if run == nil || !run.record.IsHaste || run.record.TargetObjectID != objectID {
				continue
			}
			packet, err := effectraknet.ModifierDelete(objectID, run.instanceID)
			if err != nil {
				return nil, fmt.Errorf("hasteDeathDelete: %w", err)
			}
			isCreated, err := run.release(e.npc.modifierPool)
			if err != nil {
				return nil, fmt.Errorf("hasteDeathRelease: %w", err)
			}
			member.zone.Effect().Remove(run.instanceID)
			member.zone.NPCs().ClearHaste(objectID, run.hasteExpiresAt)
			// Keep the source Haster's scheduled attack release; only its
			// target's buff has ended. The expiry step checks run identity.
			member.untrackCampaignNPCModifier(run)
			if isCreated {
				packets = append(packets, packet)
			}
		}
		e.registry.sessions[key] = member
	}
	return packets, nil
}
