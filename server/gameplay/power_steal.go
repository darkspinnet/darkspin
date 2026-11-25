package gameplay

import (
	"fmt"

	heroraknet "github.com/darkspinnet/darkspin/server/zone/hero/raknet103"
)

func (e *gameplayPeerSession) applyEquippedPowerSteal(damage float32) ([][]byte, error) {
	if e.squad == nil || e.deployedObjectID == 0 || e.deployedHitPoint() <= 0 ||
		e.deployedCreatureIndex >= uint32(len(e.binding.Creatures)) {
		return nil, nil
	}
	const manaStealAttribute = 52 // Build 103 nAttributeType.ManaSteal.
	fraction := e.binding.Creatures[e.deployedCreatureIndex].PartAttribute[manaStealAttribute]
	if fraction <= 0 {
		return nil, nil
	}
	_, maximum, err := e.deployedResourceMaximum()
	if err != nil {
		return nil, fmt.Errorf("stealMaximum: %w", err)
	}
	previous := e.deployedManaPoint()
	power := min(maximum, previous+damage*fraction)
	if power <= previous {
		return nil, nil
	}
	packet, err := heroraknet.ManaPoint(e.deployedObjectID, power)
	if err != nil {
		return nil, fmt.Errorf("stealPowerPacket: %w", err)
	}
	rosterPacket, err := e.marshalCampaignCharacterResourceValues(e.deployedCreatureIndex, e.deployedHitPoint(), power)
	if err != nil {
		return nil, fmt.Errorf("stealRosterPacket: %w", err)
	}
	err = e.setDeployedManaPoints(power)
	if err != nil {
		return nil, fmt.Errorf("stealPowerApply: %w", err)
	}
	return [][]byte{packet, rosterPacket}, nil
}
