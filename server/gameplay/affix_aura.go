package gameplay

import (
	"fmt"

	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

func (e gameplayPendingRuntime) pollAffixAurasLocked(member *gameplayPeerSession, timestamp uint64) ([][]byte, error) {
	playerCount := member.zone.NPCParticipantCount()
	changes := member.zone.NPCs().RefreshAffixEffects(playerCount)
	packets, err := npcraknet.AffixChanges(changes)
	if err != nil {
		return nil, fmt.Errorf("affixChanges: %w", err)
	}
	for _, objectID := range member.zone.NPCs().RenewAbsorptionShields(e.now()) {
		shieldPackets, shieldErr := npcraknet.AbsorptionShieldGenerate(objectID)
		if shieldErr != nil {
			return nil, fmt.Errorf("shieldRenew: %w", shieldErr)
		}
		packets = append(packets, shieldPackets...)
	}
	mirrorChanges := member.zone.NPCs().RefreshAttributeMirrors()
	mirrorPackets, mirrorErr := npcraknet.AttributeMirrorChanges(mirrorChanges)
	if mirrorErr != nil {
		return nil, fmt.Errorf("mirrorChanges: %w", mirrorErr)
	}
	member.zone.NPCs().ConfirmAttributeMirrorChanges(mirrorChanges)
	packets = append(packets, mirrorPackets...)
	silencePackets, silenceErr := e.pollNativeSilenceAuraLocked(member, timestamp)
	if silenceErr != nil {
		return nil, fmt.Errorf("silenceAura: %w", silenceErr)
	}
	packets = append(packets, silencePackets...)
	allySilencePackets, allySilenceErr := e.pollNativeAllySilenceLocked(member, timestamp)
	if allySilenceErr != nil {
		return nil, fmt.Errorf("allySilenceAura: %w", allySilenceErr)
	}
	packets = append(packets, allySilencePackets...)
	if len(packets) == 0 {
		return nil, nil
	}
	for sessionKey, ally := range e.registry.sessions {
		if ally.zone != member.zone || ally.binding.UserID == member.binding.UserID {
			continue
		}
		ally.queuePackets(packets)
		e.registry.sessions[sessionKey] = ally
	}
	return packets, nil
}
