package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/blaze"
)

type inventoryPublisher struct {
	servers []*blaze.Server
}

func (e inventoryPublisher) PublishInventoryFull(
	ctx context.Context, userID int64, gameID uint32, ownedCount uint32, capacity uint32,
) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("inventoryContext: %w", err)
	}
	message := fmt.Sprintf(
		"Inventory full (%d/%d). Sell or discard an item in the Editor before collecting more equipment.",
		ownedCount, capacity,
	)
	var notifyErrors []error
	deliveryCount := 0
	for index, server := range e.servers {
		if server == nil {
			continue
		}
		var serverDeliveryCount int
		serverDeliveryCount, err = server.NotifySystemChat(userID, gameID, message)
		deliveryCount += serverDeliveryCount
		if err != nil {
			notifyErrors = append(notifyErrors, fmt.Errorf("inventoryServer[%d]: %w", index, err))
		}
	}
	if deliveryCount == 0 {
		notifyErrors = append(notifyErrors, errors.New("inventory recipient session unavailable"))
	}
	return errors.Join(notifyErrors...)
}
