package sporenet

import (
	"context"
	"errors"
	"fmt"
)

const PartStatusDetail uint8 = 4

var ErrPartStatus = errors.New("part status invalid")

// ConvertPartsToDetail atomically persists build 103's observed Create Detail
// transition. Other updatePartStatus operators and status values remain
// unsupported until their inventory semantics are recovered.
func (m *UserManager) ConvertPartsToDetail(
	ctx context.Context, user *User, partIDs []uint64,
) ([]Part, error) {
	if user == nil {
		return nil, ErrInvalidUser
	}
	if len(partIDs) == 0 {
		return nil, ErrPartStatus
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	user.mu.Lock()
	previousParts := append([]Part(nil), user.Parts...)
	changedParts := make([]Part, 0, len(partIDs))
	seenIDs := make(map[uint64]bool, len(partIDs))
	isChanged := false
	rollback := func() {
		user.Parts = previousParts
	}
	for _, currentPartID := range partIDs {
		if currentPartID == 0 || seenIDs[currentPartID] {
			rollback()
			user.mu.Unlock()
			return nil, ErrPartStatus
		}
		seenIDs[currentPartID] = true
		partIndex := -1
		for index := range user.Parts {
			if user.Parts[index].ID == currentPartID {
				partIndex = index
				break
			}
		}
		if partIndex < 0 {
			rollback()
			user.mu.Unlock()
			return nil, ErrPartNotFound
		}
		part := &user.Parts[partIndex]
		if part.MarketStatus != PartMarketOwned {
			rollback()
			user.mu.Unlock()
			return nil, ErrVendorPartStatus
		}
		if part.EquippedToCreatureID != 0 {
			rollback()
			user.mu.Unlock()
			return nil, ErrPartEquipped
		}
		if part.Status != 0 && part.Status != PartStatusDetail {
			rollback()
			user.mu.Unlock()
			return nil, ErrPartStatus
		}
		if part.Status != PartStatusDetail {
			part.Status = PartStatusDetail
			isChanged = true
		}
		changedParts = append(changedParts, *part)
	}
	user.mu.Unlock()
	if !isChanged {
		return changedParts, nil
	}
	err := m.repository.Save(ctx, user.Record())
	if err != nil {
		user.mu.Lock()
		rollback()
		user.mu.Unlock()
		return nil, fmt.Errorf("partStatusSave: %w", err)
	}
	return changedParts, nil
}
