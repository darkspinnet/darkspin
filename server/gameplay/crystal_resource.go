package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/zone"
	zonehero "github.com/darkspinnet/darkspin/server/zone/hero"
)

// Binding creatures and their part attributes are fixed arrays, so this value
// copy isolates stat changes. Clone the mutable squad before capping resources.
func (e gameplayPeerSession) prepareCrystalInventory(inventory sim.CrystalInventory) (gameplayPeerSession, error) {
	if e.squad == nil {
		return gameplayPeerSession{}, errors.New("crystal squad unavailable")
	}
	staged := e
	var err error
	staged.squad, err = e.squad.Clone()
	if err != nil {
		return gameplayPeerSession{}, fmt.Errorf("crystalSquadClone: %w", err)
	}
	staged.setCrystalInventory(inventory)
	for index, creature := range staged.binding.Creatures {
		character, isFound := staged.squad.Character(uint32(index))
		if creature.Noun == 0 || !isFound || !character.IsAvailable {
			continue
		}
		maximum := staged.characterResourceMaximums(uint32(index))
		hitPoint := min(character.HitPoints, maximum.hitPoint)
		manaPoint := min(character.ManaPoints, maximum.manaPoint)
		if hitPoint != character.HitPoints {
			isGameOver, healthErr := staged.squad.SetHitPoints(uint32(index), hitPoint)
			if healthErr != nil {
				return gameplayPeerSession{}, fmt.Errorf("crystalHealth[%d]: %w", index, healthErr)
			}
			if isGameOver {
				return gameplayPeerSession{}, errors.New("crystal capacity exhausted squad")
			}
		}
		if manaPoint != character.ManaPoints {
			err = staged.squad.SetManaPoints(uint32(index), manaPoint)
			if err != nil {
				return gameplayPeerSession{}, fmt.Errorf("crystalMana[%d]: %w", index, err)
			}
		}
		staged.binding.Creatures[index].HitPoint = hitPoint
		staged.binding.Creatures[index].PowerPoint = manaPoint
		staged.binding.Creatures[index].MaximumHitPoint = maximum.hitPoint
		staged.binding.Creatures[index].MaximumPowerPoint = maximum.manaPoint
	}
	return staged, nil
}

func (e *gameplayPeerSession) commitCrystalInventory(staged gameplayPeerSession, pickupObjectID uint32) error {
	if e == nil || e.squad == nil || staged.squad == nil || e.zone == nil {
		return errors.New("crystal commit unavailable")
	}
	previousSquad, err := e.squad.Clone()
	if err != nil {
		return fmt.Errorf("crystalBackup: %w", err)
	}
	err = e.squad.CommitClone(staged.squad)
	if err != nil {
		return fmt.Errorf("crystalSquadCommit: %w", err)
	}
	err = e.zone.CommitCrystalInventory(zone.CrystalInventoryChange{
		UserID: e.binding.UserID, PeerGeneration: e.generation,
		PickupObjectID: pickupObjectID, Inventory: staged.crystalInventory,
		Roster: staged.binding.Roster(), Squad: staged.squad.State(),
		Position: zonePosition(e.playerPosition),
	})
	if err != nil {
		rollbackErr := e.squad.CommitClone(previousSquad)
		return fmt.Errorf("crystalZoneCommit: %w", errors.Join(err, rollbackErr))
	}
	e.binding = staged.binding
	e.crystalInventory = staged.crystalInventory
	return nil
}

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
		currentPacket, err := raknet.MarshalApplication(raknet.CombatantDataDeltaMessage{
			ObjectID:  zonehero.ObjectID(e.binding.Slot, uint32(index)),
			HitPoints: character.HitPoints, ManaPoints: character.ManaPoints,
			IsHitPointChanged: true, IsManaPointChanged: true,
		})
		if err != nil {
			return nil, fmt.Errorf("crystalCurrent[%d]: %w", index, err)
		}
		packets = append(packets, capacityPacket, currentPacket, resourcePacket)
	}
	return packets, nil
}
