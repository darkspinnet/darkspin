//go:build scenario

package sporenet

import (
	"errors"
	"fmt"
)

// DebugEquippedPart provisions a real owned item on a fresh isolated profile.
// It does not synthesize an edited creature asset or claim an editor save.
type DebugEquippedPart struct {
	SquadCategory   string
	HeroIndex       uint32
	RigblockAssetID uint16
	Level           uint16
	Rarity          PartRarity
}

func (e *UserManager) seedDebugEquipment(user *User, parts []DebugEquippedPart, creatureIDs, pvpCreatureIDs [3]uint32) error {
	if len(parts) > 36 {
		return errors.New("scenario equipment exceeds six functional items per hero")
	}
	counts := make(map[uint32]int)
	for index, req := range parts {
		if req.HeroIndex >= 3 || req.Level == 0 || req.Level > 100 || req.Rarity > PartEpic {
			return fmt.Errorf("part[%d]: invalid hero, item level or rarity", index)
		}
		var creatureID uint32
		switch req.SquadCategory {
		case "pve":
			creatureID = creatureIDs[req.HeroIndex]
		case "pvp":
			creatureID = pvpCreatureIDs[req.HeroIndex]
		default:
			return fmt.Errorf("part[%d]: invalid squad category", index)
		}
		if creatureID == 0 || counts[creatureID] >= 6 {
			return fmt.Errorf("part[%d]: unavailable hero or full loadout", index)
		}
		part := NewPart(req.RigblockAssetID)
		if part.RigblockAssetID != req.RigblockAssetID {
			return fmt.Errorf("part[%d]: invalid rigblock", index)
		}
		part.ID = uint64(len(user.Parts) + 1)
		part.Level = req.Level
		part.Rarity = req.Rarity
		part.EquippedToCreatureID = creatureID
		part.Normalize()
		user.Parts = append(user.Parts, part)
		counts[creatureID]++
	}
	if len(parts) == 0 {
		return nil
	}
	if e.partScorer == nil {
		return errors.New("scenario equipment catalog unavailable")
	}
	for _, creature := range user.Creatures {
		if counts[creature.ID] == 0 {
			continue
		}
		equippedParts := make([]Part, 0, counts[creature.ID])
		for _, part := range user.Parts {
			if part.EquippedToCreatureID == creature.ID {
				equippedParts = append(equippedParts, part)
			}
		}
		creature.GearScore, creature.ItemPoints = e.partScorer.EquipmentScore(equippedParts)
	}
	return nil
}
