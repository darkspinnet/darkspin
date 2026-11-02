package gameplay

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/chat"
)

func (e Lifecycle) ToggleGodMode(ctx context.Context, req chat.GodModeRequest) (bool, error) {
	if e.godModeToggler == nil {
		return false, chat.ErrGodModeUnavailable
	}
	isEnabled, err := e.godModeToggler.ToggleGodMode(ctx, req)
	if err != nil {
		return false, fmt.Errorf("godGameplay: %w", err)
	}
	return isEnabled, nil
}

func (e *gameplaySessionRegistry) ToggleGodMode(ctx context.Context, req chat.GodModeRequest) (bool, error) {
	err := ctx.Err()
	if err != nil {
		return false, fmt.Errorf("godContext: %w", err)
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	for key, member := range e.sessions {
		if member.binding.UserID != uint64(req.Sender.ID) || member.binding.GameID != req.GameID ||
			!member.stage.IsDungeon() || !member.dungeonSetup.IsCommitted() ||
			member.isZoneTerminal() || member.deployedObjectID == 0 || member.deployedHitPoint() <= 0 {
			continue
		}
		member.isGodModeEnabled = !member.isGodModeEnabled
		e.sessions[key] = member
		return member.isGodModeEnabled, nil
	}
	return false, chat.ErrGodModeUnavailable
}
