package game

import (
	"errors"
	"fmt"
	"math/rand/v2"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

// GenerateShopParts uses the imported collector masks, level bounds and affix
// budgets. Stock selection across genesis types is reconstruction policy.
func (e *PartCatalog) GenerateShopParts(req sporenet.PartShopRequest) ([]sporenet.Part, error) {
	if e == nil || req.ItemLevel == 0 || req.Count == 0 || req.Count > 30 ||
		req.UncommonCount > 5 || req.UncommonCount > req.Count {
		return nil, errors.New("part shop generation request invalid")
	}
	random := rand.New(rand.NewPCG(req.Seed, req.Seed^0x7061727473686f70))
	scienceMasks := []uint32{1, 2, 4, 8, 16}
	random.Shuffle(len(scienceMasks), func(first, second int) {
		scienceMasks[first], scienceMasks[second] = scienceMasks[second], scienceMasks[first]
	})
	pools := make([][]PartDefinition, len(scienceMasks))
	const slotMask = (1 << 0) | (1 << 1) | (1 << 2) | (1 << 4) | (1 << 5)
	for index, scienceMask := range scienceMasks {
		for _, definition := range e.rigblocks {
			if isCampaignRigblockEligible(definition, slotMask, scienceMask, uint32(req.ItemLevel), false) {
				pools[index] = append(pools[index], definition)
			}
		}
		random.Shuffle(len(pools[index]), func(first, second int) {
			pools[index][first], pools[index][second] = pools[index][second], pools[index][first]
		})
	}
	rarityTiers := make([]sporenet.PartRarity, req.Count)
	for index := uint32(0); index < req.UncommonCount; index++ {
		rarityTiers[index] = sporenet.PartUncommon
	}
	random.Shuffle(len(rarityTiers), func(first, second int) {
		rarityTiers[first], rarityTiers[second] = rarityTiers[second], rarityTiers[first]
	})
	parts := make([]sporenet.Part, 0, req.Count)
	usedRigblocks := make(map[uint16]struct{}, req.Count)
	var lastErr error
	for len(parts) < int(req.Count) {
		isAdvanced := false
		for index := range pools {
			for len(pools[index]) > 0 && len(parts) < int(req.Count) {
				definition := pools[index][0]
				pools[index] = pools[index][1:]
				isAdvanced = true
				_, isUsed := usedRigblocks[definition.RigblockID]
				if isUsed {
					continue
				}
				part := sporenet.NewPart(definition.RigblockID)
				part.Level = req.ItemLevel
				part.Rarity = rarityTiers[len(parts)]
				err := e.rollBudgetedCampaignAffixes(&part, random.Uint32())
				if err != nil {
					lastErr = fmt.Errorf("shopAffix[%d]: %w", definition.RigblockID, err)
					continue
				}
				err = e.ValidateGeneratedCampaignPart(part)
				if err != nil {
					return nil, fmt.Errorf("shopDescriptor: %w", err)
				}
				parts = append(parts, part)
				usedRigblocks[definition.RigblockID] = struct{}{}
				break
			}
		}
		if !isAdvanced {
			return nil, fmt.Errorf("shopPool: %w", errors.Join(lastErr,
				fmt.Errorf("only %d of %d distinct eligible parts at item level %d", len(parts), req.Count, req.ItemLevel)))
		}
	}
	// The genesis round-robin controls coverage, not the visible offer order.
	random.Shuffle(len(parts), func(first, second int) {
		parts[first], parts[second] = parts[second], parts[first]
	})
	return parts, nil
}
