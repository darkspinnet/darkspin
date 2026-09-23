package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/blaze"
)

type systemChatPublisher struct {
	servers []*blaze.Server
}

func (e systemChatPublisher) PublishSystemChat(
	ctx context.Context, userID int64, gameID uint32, message string,
) error {
	if ctx == nil || userID == 0 || strings.TrimSpace(message) == "" {
		return errors.New("system chat publication invalid")
	}
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("systemChatContext: %w", err)
	}
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
			notifyErrors = append(notifyErrors, fmt.Errorf("systemChatServer[%d]: %w", index, err))
		}
	}
	if deliveryCount == 0 {
		notifyErrors = append(notifyErrors, errors.New("system chat recipient session unavailable"))
	}
	return errors.Join(notifyErrors...)
}
