package gameplay

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/chat"
)

func (e *gameplaySessionRegistry) ResourceStatus(
	ctx context.Context, req chat.ResourceStatusRequest,
) (chat.ResourceStatusResult, error) {
	err := ctx.Err()
	if err != nil {
		return chat.ResourceStatusResult{}, fmt.Errorf("resourceStatusContext: %w", err)
	}
	if e == nil || req.Sender.ID <= 0 || req.GameID == 0 {
		return chat.ResourceStatusResult{}, fmt.Errorf(
			"resourceStatusRequest: %w", chat.ErrResourceStatusUnavailable,
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
		return chat.ResourceStatusResult{
			ObjectID:          peerSession.deployedObjectID,
			HitPoint:          peerSession.deployedHitPoint(),
			MaximumHitPoint:   peerSession.characterHitPointMaximum(peerSession.deployedCreatureIndex),
			PowerPoint:        peerSession.deployedManaPoint(),
			MaximumPowerPoint: peerSession.characterManaPointMaximum(peerSession.deployedCreatureIndex),
		}, nil
	}
	return chat.ResourceStatusResult{}, fmt.Errorf(
		"resourceStatusSession: %w", chat.ErrResourceStatusUnavailable,
	)
}
