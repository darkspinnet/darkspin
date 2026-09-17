package blaze

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/blaze/tdf"
	"github.com/darkspinnet/darkspin/server/game"
)

// NotifyTutorialEnd reproduces build 103's control-channel completion flow:
// GameStateChange(POST_GAME) -> cBlazeGameManager::onGameEnded (0xC3A8F0)
// -> Game::leaveGame (0xE072B0) -> RemovePlayer(REAS=6). The existing removal
// handler replies and emits PlayerRemoved, which clears the native join state
// and refreshes the account before the ship UI can try to launch another game.
func (e *Server) NotifyTutorialEnd(ctx context.Context, userID int64, gameID uint32) (int, error) {
	deliveryCount := 0
	for _, session := range e.sessionsForUser(userID) {
		err := ctx.Err()
		if err != nil {
			return deliveryCount, fmt.Errorf("tutorialContext: %w", err)
		}
		err = session.Notify(GameManagerComponentID, 0x64, []tdf.Field{
			tdf.FieldNamed("GID", tdf.IntegerValue(uint64(gameID))),
			tdf.FieldNamed("GSTA", tdf.IntegerValue(uint64(game.StatePostGame))),
		})
		if err != nil {
			return deliveryCount, fmt.Errorf("tutorialNotify: %w", err)
		}
		deliveryCount++
	}
	return deliveryCount, nil
}
