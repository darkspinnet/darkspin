package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/blaze"
)

type tutorialEndPublisher struct {
	servers []*blaze.Server
}

func (e tutorialEndPublisher) PublishTutorialEnd(ctx context.Context, userID int64, gameID uint32) error {
	deliveryCount := 0
	for index, server := range e.servers {
		count, err := server.NotifyTutorialEnd(ctx, userID, gameID)
		if err != nil {
			return fmt.Errorf("tutorialServer[%d]: %w", index, err)
		}
		deliveryCount += count
	}
	if deliveryCount == 0 {
		return errors.New("tutorial recipient session unavailable")
	}
	return nil
}
