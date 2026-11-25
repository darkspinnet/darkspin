package blaze

import (
	"context"
	"net/netip"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

func observeGameplayLoginIP(ctx context.Context, request *Request, userManager UserManager, user *sporenet.User) {
	if user == nil || request == nil || request.Session == nil {
		return
	}
	observer, isSupported := userManager.(interface {
		ObserveGameplayLoginIP(context.Context, *sporenet.User, string) (bool, error)
	})
	if !isSupported {
		return
	}
	session := request.Session
	session.loginHistoryMutex.Lock()
	defer session.loginHistoryMutex.Unlock()
	userID := user.AccountID()
	_, isRecorded := session.loginHistoryUsers[userID]
	if isRecorded {
		return
	}
	peerIP, err := netip.ParseAddr(serverLoginRemoteIP(request))
	if err != nil {
		if session.server != nil && session.server.logger != nil {
			session.server.logger.Printf("Login IP history skipped: session=%d user=%d has no valid TCP peer IP", session.ID, userID)
		}
		return
	}
	isRecorded, err = observer.ObserveGameplayLoginIP(ctx, user, peerIP.WithZone("").Unmap().String())
	if err != nil {
		if session.server != nil && session.server.logger != nil {
			session.server.logger.Printf("Login IP history failed: session=%d user=%d: %v", session.ID, userID, err)
		}
		return
	}
	if !isRecorded {
		return
	}
	if session.loginHistoryUsers == nil {
		session.loginHistoryUsers = make(map[int64]struct{})
	}
	session.loginHistoryUsers[userID] = struct{}{}
}
