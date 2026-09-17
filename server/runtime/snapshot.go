package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/blaze"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/snapshot"
)

type snapshotNotifier struct {
	servers []*blaze.Server
}

func (e snapshotNotifier) NotifySnapshot(
	ctx context.Context, notice snapshot.Notice,
) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("noticeContext: %w", err)
	}
	if notice.Actor.UserID == 0 || notice.Message == "" {
		return errors.New("snapshot notice invalid")
	}
	var notifyErrors []error
	deliveryCount := 0
	for index, server := range e.servers {
		if server == nil {
			continue
		}
		var serverDeliveryCount int
		serverDeliveryCount, err = server.NotifySystemChat(
			notice.Actor.UserID, notice.Actor.GameID, notice.Message,
		)
		deliveryCount += serverDeliveryCount
		if err != nil {
			notifyErrors = append(notifyErrors, fmt.Errorf("noticeServer[%d]: %w", index, err))
		}
	}
	if deliveryCount == 0 {
		notifyErrors = append(notifyErrors, errors.New("notice recipient session unavailable"))
	}
	return errors.Join(notifyErrors...)
}

// SetSnapshotMode applies a persisted launcher mode to the running service.
func (s *Server) SetSnapshotMode(rawMode string) error {
	if s == nil || s.snapshot == nil || s.config == nil {
		return errors.New("sync snapshot service unavailable")
	}
	mode, err := snapshot.ParseMode(rawMode)
	if err != nil {
		return fmt.Errorf("snapshotMode: %w", err)
	}
	s.snapshot.SetMode(mode)
	err = s.config.Set(game.ConfigSnapshotMode, string(mode))
	if err != nil {
		return fmt.Errorf("snapshotConfig: %w", err)
	}
	return nil
}
