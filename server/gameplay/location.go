package gameplay

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/chat"
)

func (e *gameplaySessionRegistry) Location(
	ctx context.Context, req chat.LocationRequest,
) (chat.LocationResult, error) {
	err := ctx.Err()
	if err != nil {
		return chat.LocationResult{}, fmt.Errorf("locationContext: %w", err)
	}
	if e == nil || req.Sender.ID <= 0 || req.GameID == 0 {
		return chat.LocationResult{}, fmt.Errorf(
			"locationRequest: %w", chat.ErrLocationUnavailable,
		)
	}
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	for _, peerSession := range e.sessions {
		if peerSession.binding.UserID != uint64(req.Sender.ID) ||
			peerSession.binding.GameID != req.GameID ||
			!peerSession.stage.IsDungeon() || peerSession.deployedObjectID == 0 ||
			peerSession.isZoneTerminal() {
			continue
		}
		return chat.LocationResult{
			X: peerSession.playerPosition.X,
			Y: peerSession.playerPosition.Y,
			Z: peerSession.playerPosition.Z,
		}, nil
	}
	return chat.LocationResult{}, fmt.Errorf(
		"locationSession: %w", chat.ErrLocationUnavailable,
	)
}
