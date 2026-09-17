package inventory

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

// PartDropper is the persistence port consumed by the inventory feature.
type PartDropper interface {
	DropPartTo(context.Context, int64, uint64, sporenet.PartDropTarget) (bool, error)
}

// DropCommand identifies one authenticated account inventory mutation.
type DropCommand struct {
	UserID int64
	ItemID uint64
}

// DropResult reports whether the requested item was owned and removed.
type DropResult struct {
	IsDropped bool
}

// Drop validates and transfers one inventory item to a prepared destination. Wire decoding and session
// lookup remain transport concerns.
func Drop(
	ctx context.Context, partDropper PartDropper, command DropCommand,
	target sporenet.PartDropTarget,
) (DropResult, error) {
	if command.UserID <= 0 {
		return DropResult{}, errors.New("drop user unavailable")
	}
	if command.ItemID == 0 {
		return DropResult{}, errors.New("drop item unavailable")
	}
	if partDropper == nil {
		return DropResult{}, errors.New("drop persistence unavailable")
	}
	isDropped, err := partDropper.DropPartTo(ctx, command.UserID, command.ItemID, target)
	if err != nil {
		return DropResult{}, fmt.Errorf("dropPart: %w", err)
	}
	return DropResult{IsDropped: isDropped}, nil
}
