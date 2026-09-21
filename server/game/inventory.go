package game

import (
	"context"
	"fmt"
)

// InventoryPublisher delivers inventory-capacity feedback through the
// player's existing control connection.
type InventoryPublisher interface {
	PublishInventoryFull(context.Context, int64, uint32, uint32, uint32) error
}

// UseInventoryPublisher is configured before accepting gameplay connections.
func (e *GameplayJoin) UseInventoryPublisher(publisher InventoryPublisher) {
	e.inventoryPublisher = publisher
}

// PublishInventoryFull reports why an equipment pickup was rejected while the
// dropped item remains available in the campaign.
func (e *GameplayJoin) PublishInventoryFull(
	ctx context.Context, userID int64, gameID uint32, ownedCount uint32, capacity uint32,
) error {
	if e == nil || e.inventoryPublisher == nil {
		return nil
	}
	err := e.inventoryPublisher.PublishInventoryFull(
		ctx, userID, gameID, ownedCount, capacity,
	)
	if err != nil {
		return fmt.Errorf("inventoryPublish: %w", err)
	}
	return nil
}
