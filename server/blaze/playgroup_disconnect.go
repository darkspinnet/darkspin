package blaze

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/party"
)

// A leave RPC has no reason field. Preserve actual new game/party handoffs,
// but honor a leave followed by shutdown even when cleanup RPCs trail it.
type playgroupDisconnectLeave struct {
	partyService    *party.Service
	networkRegistry *playgroupNetworkRegistry
	partyID         uint32
	userID          int64
}

type playgroupShipDisconnect struct {
	partyService    *party.Service
	networkRegistry *playgroupNetworkRegistry
	userManager     socialUserManager
	matchmaking     matchmakingDirectory
}

func (e *Registry) lockPartyLifecycle(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := ctx.Err()
		if err != nil {
			return fmt.Errorf("partyLifecycleContext: %w", err)
		}
		if e.partyLifecycle.TryLock() {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("partyLifecycleWait: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func isPlaygroupHandoffFrame(frame Frame) bool {
	if frame.Component == PlaygroupsComponentID {
		return frame.Command == 0x01 || frame.Command == 0x03
	}
	if frame.Component != GameManagerComponentID {
		return false
	}
	switch frame.Command {
	case 0x01, 0x03, 0x09, 0x0d, 0x0f, 0x19:
		return true
	default:
		return false
	}
}

func (e *Session) completeDisconnectedPlaygroupLeave(ctx context.Context) {
	pending := e.pendingPlaygroupLeave
	e.pendingPlaygroupLeave = nil
	if e.server == nil {
		return
	}
	cleanupContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := e.server.registry.lockPartyLifecycle(cleanupContext)
	if err != nil {
		e.server.logger.Printf("Disconnected playgroup stage failed session=%d: %v", e.ID, err)
		return
	}
	req, response, err := e.prepareDisconnectedPlaygroupLeave(cleanupContext, pending)
	e.server.registry.partyLifecycle.Unlock()
	if req == nil {
		if err != nil {
			e.server.logger.Printf("Disconnected playgroup preparation failed session=%d: %v", e.ID, err)
		}
		return
	}
	if err != nil {
		e.server.logger.Printf("Disconnected playgroup leave failed session=%d: %v", e.ID, err)
	}
	// Deliver already-encoded notifications even if a later encoding failed.
	for _, notification := range req.targetedNotifications {
		target := notification.session
		target.traceFrame("out", notification.frame)
		sendErr := target.send(notification.frame)
		if sendErr != nil {
			e.server.logger.Printf("Disconnected playgroup notification failed session=%d recipient=%d: %v",
				e.ID, target.ID, sendErr)
		}
	}
	if err == nil && response != nil && response.ErrorCode != 0 {
		e.server.logger.Printf("Disconnected playgroup leave rejected session=%d code=%d",
			e.ID, response.ErrorCode)
	}
}

// Called under the disconnected session's request stage and the shared
// lifecycle gate. Auth replacement and ordinary game admission cannot pass
// the final-session/idle checks before the feature-owned leave commits.
func (e *Session) prepareDisconnectedPlaygroupLeave(
	ctx context.Context, pending *playgroupDisconnectLeave,
) (*Request, *Response, error) {
	user := requestUser(&Request{Session: e})
	if user == nil || len(e.server.sessionsForUser(user.Account.ID)) != 0 || user.CurrentGameID() != 0 {
		return nil, nil, nil
	}
	e.server.registry.mu.RLock()
	owner := e.server.registry.playgroupDisconnect
	e.server.registry.mu.RUnlock()
	if owner == nil {
		return nil, nil, nil
	}
	before, isFound := owner.partyService.PartyForMember(user.Account.ID)
	if !isFound {
		return nil, nil, nil
	}
	if pending != nil && (pending.userID != user.Account.ID || pending.partyID != before.ID) {
		return nil, nil, nil
	}
	for _, member := range before.Members {
		peer := owner.userManager.UserByID(member.ID)
		if peer == nil || peer.CurrentGameID() != 0 || (owner.matchmaking != nil && owner.matchmaking.IsQueued(member.ID)) {
			return nil, nil, nil
		}
	}
	req := &Request{Session: e}
	response, err := completePlaygroupLeave(ctx, req, owner.partyService, owner.networkRegistry, before, user.Account.ID)
	if err != nil {
		return req, response, fmt.Errorf("disconnectLeave: %w", err)
	}
	return req, response, nil
}
