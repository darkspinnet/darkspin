package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
	"github.com/darkspinnet/darkspin/server/gameplay"
	partyraknet "github.com/darkspinnet/darkspin/server/party/raknet103"
	"github.com/darkspinnet/darkspin/server/zone"
)

type panelConnectionControl struct {
	server            *Server
	accessService     *accountaccess.Service
	gameplayLifecycle gameplay.Lifecycle
	partyRelay        *partyraknet.Relay
	timer             zone.Timer
	mu                sync.Mutex
	attempts          map[int64]*banDisconnectRetry
}

type banDisconnectRetry struct {
	owner  *panelConnectionControl
	userID int64
}

func (e *panelConnectionControl) IsOnline(userID int64) bool {
	return e.server != nil && e.server.userManager.UserByID(userID) != nil
}

func (e *panelConnectionControl) DisconnectAccount(ctx context.Context, userID int64) error {
	if e.server == nil || userID <= 0 {
		return errors.New("account disconnect unavailable")
	}
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("disconnectContext: %w", err)
	}
	transports := e.gameplayLifecycle.AccountTransports(userID)
	failures := make([]error, 0)
	for _, transport := range transports {
		isRetired, retireErr := e.server.udpServer.RetireGameplayTransport(transport.Address, transport.Generation)
		if retireErr != nil {
			failures = append(failures, fmt.Errorf("disconnectUDP: %w", retireErr))
		}
		if !isRetired && retireErr == nil {
			// An already retired or replaced generation needs no further transport action.
		}
		e.gameplayLifecycle.DiscardMember(transport.GameID, uint64(userID))
	}
	user := e.server.userManager.UserByID(userID)
	if user != nil {
		e.removeGameMembership(userID)
		view := user.View()
		if e.partyRelay != nil && view.Account.CurrentPlaygroupID != 0 {
			e.partyRelay.RemoveMember(view.Account.CurrentPlaygroupID, userID)
		}
	}
	for _, service := range e.server.maintenanceServers {
		err = service.DisconnectAccount(ctx, userID)
		if err != nil {
			failures = append(failures, fmt.Errorf("disconnectBlaze: %w", err))
		}
	}
	if user != nil {
		// A request already executing when sockets closed can have completed
		// game admission. Blaze has now quiesced those request stages.
		e.removeGameMembership(userID)
		err = e.server.userManager.Logout(ctx, user)
		if err != nil {
			failures = append(failures, fmt.Errorf("disconnectLogout: %w", err))
		}
	}
	if len(e.gameplayLifecycle.AccountTransports(userID)) != 0 {
		failures = append(failures, errors.New("gameplay retirement is pending"))
	}
	if len(failures) != 0 {
		return fmt.Errorf("disconnectAccount: %w", errors.Join(failures...))
	}
	return nil
}

func (e *panelConnectionControl) removeGameMembership(userID int64) {
	user := e.server.userManager.UserByID(userID)
	if user == nil {
		return
	}
	gameID := user.CurrentGameID()
	if gameID == 0 {
		return
	}
	e.gameplayLifecycle.DiscardMember(gameID, uint64(userID))
	instance := e.server.gameManager.Game(gameID)
	if instance != nil {
		instance.RemovePlayer(userID)
	}
}

// EnforceBan enforces a persisted ban or timeout despite transport/save failures.
// Retries use the server scheduler and stop when the restriction ends.
func (e *panelConnectionControl) EnforceBan(ctx context.Context, userID int64) bool {
	operationCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := e.DisconnectAccount(operationCtx, userID)
	if err == nil {
		return false
	}
	e.server.logger.Printf("Blocked account disconnect pending user=%d: %v", userID, err)
	e.mu.Lock()
	if e.attempts == nil {
		e.attempts = make(map[int64]*banDisconnectRetry)
	}
	retry := e.attempts[userID]
	if retry == nil {
		retry = &banDisconnectRetry{owner: e, userID: userID}
		e.attempts[userID] = retry
		e.mu.Unlock()
		retry.schedule()
		return true
	}
	e.mu.Unlock()
	return true
}

func (e *banDisconnectRetry) schedule() {
	cancel, err := e.owner.timer.Schedule(5*time.Second, e.execute)
	if err != nil {
		e.owner.server.logger.Printf("Blocked account disconnect retry unavailable user=%d: %v", e.userID, err)
		e.finish()
		return
	}
	if cancel == nil {
		e.owner.server.logger.Printf("Blocked account disconnect retry returned no cancellation user=%d", e.userID)
		e.finish()
	}
	// The server scheduler owns cancellation during shutdown.
}

func (e *banDisconnectRetry) execute() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	access, err := e.owner.accessService.Read(ctx, e.userID)
	if errors.Is(err, accountaccess.ErrNotFound) || err == nil && !access.IsBlocked() {
		e.finish()
		return
	}
	if err != nil {
		e.owner.server.logger.Printf("Restriction retry access unavailable user=%d: %v", e.userID, err)
		e.schedule()
		return
	}
	err = e.owner.DisconnectAccount(ctx, e.userID)
	if err == nil {
		e.finish()
		return
	}
	e.owner.server.logger.Printf("Blocked account disconnect retry pending user=%d: %v", e.userID, err)
	e.schedule()
}

func (e *banDisconnectRetry) finish() {
	e.owner.mu.Lock()
	if e.owner.attempts[e.userID] == e {
		delete(e.owner.attempts, e.userID)
	}
	e.owner.mu.Unlock()
}
