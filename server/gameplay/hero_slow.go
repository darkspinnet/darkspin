package gameplay

import (
	"fmt"

	zonehero "github.com/darkspinnet/darkspin/server/zone/hero"
)

// Build 103's attribute updates replace each supplied attribute. The initial
// hero baseline includes equipment in 23/48; a temporary slow must compose with
// that baseline, rather than erase it when the slow is applied or removed.
func (e gameplayPeerSession) marshalHeroSlowAttributes(
	objectID uint32, attackSpeed float32, movementSpeed float32,
) ([]byte, error) {
	for index, creature := range e.binding.Creatures {
		if zonehero.ObjectID(e.binding.Slot, uint32(index)) != objectID {
			continue
		}
		attackSpeed += creature.PartAttribute[23]
		movementSpeed += creature.PartAttribute[48]
		break
	}
	packet, err := marshalCampaignNPCSlowAttributes(objectID, attackSpeed, movementSpeed)
	if err != nil {
		return nil, fmt.Errorf("heroSlowMarshal: %w", err)
	}
	return packet, nil
}
