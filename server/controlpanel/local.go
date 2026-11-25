package controlpanel

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/accountaccess"
)

// OpenLocalSession is a trusted in-process launcher operation. It is deliberately
// absent from Handler: a network caller must always supply account credentials.
// Sessions on this local host receive operator privileges without changing the
// profile's persisted status. Dedicated hosts never accept this entry point.
func (e *Service) OpenLocalSession(ctx context.Context, userID int64) (string, error) {
	if ctx == nil {
		return "", errors.New("nil panel context")
	}
	if e.config.IsDedicated {
		return "", errors.New("local panel access is unavailable on dedicated servers")
	}
	ctx = accountaccess.WithTrustedLocal(ctx)
	identity, err := e.config.IdentityReader.Identity(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("localIdentity: %w", err)
	}
	if userID <= 0 || identity.ID != userID || identity.LoginName == "" {
		return "", fmt.Errorf("localAccount: %w", accountaccess.ErrNotFound)
	}
	access, err := e.config.AccessReader.Read(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("localAccess: %w", err)
	}
	if access.UserID != userID || access.Revision <= 0 || access.IsBlocked() {
		return "", fmt.Errorf("localAuthorize: %w", accountaccess.ErrAccessDenied)
	}
	err = accountaccess.CheckPeer(ctx, access)
	if err != nil {
		return "", fmt.Errorf("localPeer: %w", err)
	}
	token, entry, err := e.createSession(userID, access.Revision)
	if err != nil {
		return "", fmt.Errorf("localSession: %w", err)
	}
	if entry.accountID != userID {
		return "", errors.New("local panel session account mismatch")
	}
	digest := sha256.Sum256([]byte(token))
	e.mu.Lock()
	current, isFound := e.sessions[digest]
	if !isFound || current.id != entry.id {
		e.mu.Unlock()
		return "", fmt.Errorf("localRevoked: %w", accountaccess.ErrAccessDenied)
	}
	current.isLocal = true
	e.sessions[digest] = current
	e.mu.Unlock()
	return token, nil
}
