package blaze

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/darkspinnet/darkspin/server/blaze/tdf"
	"github.com/darkspinnet/darkspin/server/pvp"
)

// DisconnectAccount closes exact TCP connections and forces authoritative queue
// and party cleanup. The runtime removes gameplay membership before calling it.
// Persisted bans must already be enforced by admission before this operation.
func (e *Server) DisconnectAccount(ctx context.Context, userID int64) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if userID <= 0 {
		return errors.New("invalid disconnect account ID")
	}
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("disconnectContext: %w", err)
	}
	sessions := e.sessionsForUser(userID)
	failures := make([]error, 0)
	for _, session := range sessions {
		// Recheck the indexed binding before closing, so a connection that has
		// already switched accounts since the snapshot is preserved.
		e.mu.Lock()
		isCurrent := e.sessionsByUser[userID][session.ID] == session
		if !isCurrent {
			e.mu.Unlock()
			continue
		}
		closeErr := session.conn.Close()
		e.mu.Unlock()
		if closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			failures = append(failures, fmt.Errorf("sessionClose[%d]: %w", session.ID, closeErr))
		}
		stageErr := session.lockOperation(ctx)
		if stageErr != nil {
			failures = append(failures, fmt.Errorf("sessionStage[%d]: %w", session.ID, stageErr))
			continue
		}
		user := requestUser(&Request{Session: session})
		if user != nil && user.Account.ID == userID {
			// Preserve the connection index until normal teardown emits offline
			// presence, while preventing already-read RPCs from using stale auth.
			session.data.Delete(sessionUserKey)
			session.pendingPlaygroupLeave = nil
		}
		session.operationMutex.Unlock()
	}
	cleanupErr := e.forceAccountPartyLeave(ctx, userID)
	if cleanupErr != nil {
		failures = append(failures, fmt.Errorf("accountCleanup: %w", cleanupErr))
	}
	if len(failures) > 0 {
		return fmt.Errorf("disconnectClose: %w", errors.Join(failures...))
	}
	return nil
}

type accountMatchmakingCanceller interface {
	CancelForMember(int64) []pvp.MatchmakingSession
}

func (e *Server) forceAccountPartyLeave(ctx context.Context, userID int64) error {
	// No session operation is held while taking the lifecycle gate. Ordinary
	// dispatch takes operation -> lifecycle, so this cannot invert that order.
	err := e.registry.lockPartyLifecycle(ctx)
	if err != nil {
		return fmt.Errorf("forceLifecycle: %w", err)
	}
	req, prepareErr := e.prepareAccountPartyLeave(ctx, userID)
	e.registry.partyLifecycle.Unlock()
	failures := make([]error, 0)
	if prepareErr != nil {
		failures = append(failures, fmt.Errorf("forcePrepare: %w", prepareErr))
	}
	if req != nil {
		for _, notification := range req.targetedNotifications {
			target := notification.session
			if target == nil {
				continue
			}
			e.mu.Lock()
			isDeparting := target.userID == userID
			e.mu.Unlock()
			if isDeparting {
				continue
			}
			target.traceFrame("out", notification.frame)
			sendErr := target.send(notification.frame)
			if sendErr != nil && !errors.Is(sendErr, net.ErrClosed) {
				failures = append(failures, fmt.Errorf("forceNotify[%d]: %w", target.ID, sendErr))
			}
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("forceLeave: %w", errors.Join(failures...))
	}
	return nil
}

// Called with the shared party lifecycle gate. The synthetic request only owns
// notification routing; feature operations receive the explicit target ID.
// This also clears stale reservations/parties when no TCP session survives.
func (e *Server) prepareAccountPartyLeave(ctx context.Context, userID int64) (*Request, error) {
	e.registry.mu.RLock()
	owner := e.registry.playgroupDisconnect
	e.registry.mu.RUnlock()
	if owner == nil {
		return nil, nil
	}
	req := &Request{Session: &Session{server: e}}
	failures := make([]error, 0)
	canceller, isSupported := owner.matchmaking.(accountMatchmakingCanceller)
	if isSupported {
		sessions := canceller.CancelForMember(userID)
		err := queueAccountMatchCancellation(req, sessions)
		if err != nil {
			failures = append(failures, fmt.Errorf("forceQueueNotify: %w", err))
		}
	} else if owner.matchmaking != nil && owner.matchmaking.IsQueued(userID) {
		failures = append(failures, errors.New("administrative matchmaking cancellation unavailable"))
	}
	before, isFound := owner.partyService.PartyForMember(userID)
	if isFound {
		response, err := completePlaygroupLeave(ctx, req, owner.partyService, owner.networkRegistry, before, userID)
		if err != nil {
			failures = append(failures, fmt.Errorf("forcePartyLeave: %w", err))
		} else if response != nil && response.ErrorCode != 0 && response.ErrorCode != 0x0006 {
			failures = append(failures, fmt.Errorf("forced party leave rejected with code %d", response.ErrorCode))
		}
	}
	if len(failures) > 0 {
		return req, fmt.Errorf("forceCleanup: %w", errors.Join(failures...))
	}
	return req, nil
}

func queueAccountMatchCancellation(req *Request, sessions []pvp.MatchmakingSession) error {
	for _, session := range sessions {
		err := req.QueueUserNotification(session.UserID, GameManagerComponentID, 0x0a, []tdf.Field{
			tdf.FieldNamed("MAXF", tdf.IntegerValue(0)),
			tdf.FieldNamed("MSID", tdf.IntegerValue(session.ID)),
			tdf.FieldNamed("RSLT", tdf.IntegerValue(matchmakingResultCancelled)),
			tdf.FieldNamed("USID", tdf.IntegerValue(uint64(session.UserID))),
		})
		if err != nil {
			return fmt.Errorf("cancelNotify[%d]: %w", session.UserID, err)
		}
	}
	return nil
}
