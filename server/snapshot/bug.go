package snapshot

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/chat"
)

// CaptureBugSnapshot reuses the enabled observer's bounded history
// and captures a fresh client/server boundary for inclusion in a bug archive.
func (e *Service) CaptureBugSnapshot(ctx context.Context, req chat.BugCommand) (string, error) {
	if e == nil {
		return "", errors.New("snapshot service unavailable")
	}
	e.mu.Lock()
	isDisabled := e.mode == ModeOff
	e.mu.Unlock()
	if isDisabled {
		return "", errors.New("snapshot mode is off; enable Manual or Auto before reproducing to retain client/server history")
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
