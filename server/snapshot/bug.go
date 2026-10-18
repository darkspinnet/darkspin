package snapshot

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/chat"
)

// CaptureBugSnapshot reuses the active automatic observer's bounded history
// and captures a fresh client/server boundary for inclusion in a bug archive.
func (e *Service) CaptureBugSnapshot(ctx context.Context, req chat.BugCommand) (string, error) {
	if e == nil {
		return "", nil
	}
	e.mu.Lock()
	isAutomatic := e.mode == ModeAuto
	e.mu.Unlock()
	if !isAutomatic {
		return "", nil
	}
	result, err := e.dump(ctx, dumpRequest{
		Actor:   Actor{UserID: req.Sender.ID, UserName: req.Sender.Name, GameID: req.GameID},
		Trigger: "bug", Context: req.Description, IsArchiveDisabled: true,
	})
	if err != nil {
		return "", fmt.Errorf("bugCapture: %w", err)
	}
	return result.Directory, nil
}
