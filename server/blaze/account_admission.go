package blaze

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/accountaccess"
)

// authorizeSession rechecks current account policy for an established TCP peer.
func (e *Registry) authorizeSession(ctx context.Context, session *Session) error {
	user := requestUser(&Request{Session: session})
	if user == nil {
		return nil
	}
	e.mu.RLock()
	authorizer := e.accountAuthorizer
	e.mu.RUnlock()
	if authorizer == nil {
		return fmt.Errorf("sessionPolicy: %w", accountaccess.ErrAccessDenied)
	}
	ctx = accountaccess.WithPeer(ctx, sessionRemoteIP(session))
	err := authorizer.AuthorizeSession(ctx, user.AccountID())
	if err != nil {
		return fmt.Errorf("sessionAdmission: %w", err)
	}
	return nil
}
