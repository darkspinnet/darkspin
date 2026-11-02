package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
)

// The registry lock is held. Transfer support is intentionally narrower than
// cleansing: every nearby living hero, including the caster, loses debuffs even
// when that status has no supported enemy-transfer representation.
func (e fieldMedicActiveSchedule) cleanseHeroesLocked(
	peerSession gameplayPeerSession, center game.Vec3, effectTargets map[uint32]bool,
) ([][]byte, error) {
	packets := make([][]byte, 0)
	for sessionKey, member := range e.runtime.registry.sessions {
		if member.zone == nil || member.zone.Effect() != peerSession.zone.Effect() ||
			!fieldMedicAllyInRange(peerSession, member.deployedObjectID, center, e.definition.Radius) {
			continue
		}
		packetCount := len(packets)
		burnPackets, err := e.runtime.registry.retireFixtureBurnsLocked(member.zone.NPCs(), member.deployedObjectID, e.runtime.modifierPool)
		packets = append(packets, burnPackets...)
		if err != nil {
			return packets, fmt.Errorf("cleanseBurn: %w", err)
		}
		cloudPackets, err := e.runtime.registry.retireCloudPoisonLocked(member.zone.NPCs(), member.deployedObjectID, e.runtime.modifierPool)
		packets = append(packets, cloudPackets...)
		if err != nil {
			return packets, fmt.Errorf("cleanseCloud: %w", err)
		}
		debuffPackets, err := e.runtime.purgeHeroDebuffsLocked(sessionKey, member.generation, member.deployedObjectID)
		packets = append(packets, debuffPackets...)
		if err != nil {
			return packets, fmt.Errorf("cleanseHero: %w", err)
		}
		if len(packets) > packetCount {
			effectTargets[member.deployedObjectID] = true
		}
	}
	return packets, nil
}
