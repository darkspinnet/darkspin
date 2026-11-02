package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonehero "github.com/darkspinnet/darkspin/server/zone/hero"
)

// Catalyst inventory reflection does not refresh either native hero capacity
// or the squad resource HUD. Publish both views after pickup, drag and removal.
func (e gameplayPeerSession) marshalCrystalResources() ([][]byte, error) {
	if e.squad == nil {
		return nil, nil
	}
	packets := make([][]byte, 0)
	for index, creature := range e.binding.Creatures {
		if creature.Noun == 0 {
			continue
		}
		character, isFound := e.squad.Character(uint32(index))
		if !isFound {
			continue
		}
		if !character.IsAvailable {
			continue
		}
		maximum := e.characterResourceMaximums(uint32(index))
		capacityPacket, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
			ObjectID: zonehero.ObjectID(e.binding.Slot, uint32(index)),
			Value:    map[uint8]float32{4: maximum.hitPoint, 5: maximum.manaPoint},
		})
		if err != nil {
			return nil, fmt.Errorf("crystalCapacity[%d]: %w", index, err)
		}
		resourcePacket, err := e.marshalCampaignCharacterResource(uint32(index))
		if err != nil {
			return nil, fmt.Errorf("crystalResource[%d]: %w", index, err)
		}
		packets = append(packets, capacityPacket, resourcePacket)
	}
	return packets, nil
}
