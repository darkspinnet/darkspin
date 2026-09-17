package sporenet

import (
	"context"
	"errors"
	"fmt"
)

// PartDropTarget prepares a recoverable destination before inventory removal.
// Its owner must exclude collection until DropPartTo returns.
type PartDropTarget interface {
	PlacePart(Part) error
	RollbackPart()
}

func (e *UserManager) DropPartTo(
	ctx context.Context, userID int64, itemID uint64, target PartDropTarget,
) (bool, error) {
	if target == nil {
		return false, errors.New("part drop destination unavailable")
	}
	user := e.UserByID(userID)
	if user == nil {
		return false, ErrInvalidUser
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	user.mu.Lock()
	partIndex := -1
	var droppedPart Part
	for index, part := range user.Parts {
		if part.ID == itemID {
			partIndex = index
			droppedPart = part
			break
		}
	}
	if partIndex < 0 {
		user.mu.Unlock()
		return false, nil
	}
	if droppedPart.EquippedToCreatureID != 0 {
		user.mu.Unlock()
		return false, ErrPartEquipped
	}
	if droppedPart.MarketStatus != PartMarketOwned {
		user.mu.Unlock()
		return false, errors.New("part is not owned inventory")
	}
	previousParts := append([]Part(nil), user.Parts...)
	user.mu.Unlock()
	err := target.PlacePart(droppedPart)
	if err != nil {
		target.RollbackPart()
		return false, fmt.Errorf("dropPlace: %w", err)
	}
	user.mu.Lock()
	user.Parts = append(user.Parts[:partIndex], user.Parts[partIndex+1:]...)
	user.mu.Unlock()
	err = e.repository.Save(ctx, user.Record())
	if err != nil {
		user.mu.Lock()
		user.Parts = previousParts
		user.mu.Unlock()
		target.RollbackPart()
		return false, fmt.Errorf("dropSave: %w", err)
	}
	return true, nil
}
