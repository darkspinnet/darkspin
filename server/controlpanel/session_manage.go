package controlpanel

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
)

var (
	errSessionUnavailable = errors.New("panel session unavailable")
	errSessionNotFound    = errors.New("panel session not found")
)

type sessionSummary struct {
	ID         string
	CreatedAt  time.Time
	LastUsedAt time.Time
	ExpiresAt  time.Time
	IsCurrent  bool
}

type sessionRevocation struct {
	IsCurrent    bool
	RevokedCount int
}

// checkSessionLocked rechecks the durable revision at the session operation's
// boundary. The caller holds mu through this check and its session mutation.
func (e *Service) checkSessionLocked(ctx context.Context, digest [sha256.Size]byte, expected session) error {
	current, isFound := e.sessions[digest]
	if !isFound || current.id != expected.id || current.accountID != expected.accountID ||
		current.revision != expected.revision || !time.Now().Before(current.expiresAt) {
		return errSessionUnavailable
	}
	access, err := e.config.AccessReader.Read(ctx, current.accountID)
	if errors.Is(err, accountaccess.ErrNotFound) {
		delete(e.sessions, digest)
		return fmt.Errorf("sessionAccount: %w", errSessionUnavailable)
	}
	if err != nil {
		return fmt.Errorf("sessionAccess: %w", err)
	}
	if access.UserID != current.accountID || access.IsBlocked() || access.Revision <= 0 || access.Revision != current.revision ||
		!time.Now().Before(current.expiresAt) {
		delete(e.sessions, digest)
		return fmt.Errorf("sessionRevision: %w", errSessionUnavailable)
	}
	err = accountaccess.CheckPeer(ctx, access)
	if err != nil {
		return fmt.Errorf("sessionPeer: %w", errSessionUnavailable)
	}
	return nil
}

func (e *Service) listSessions(ctx context.Context, digest [sha256.Size]byte, current session) ([]sessionSummary, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	err := e.checkSessionLocked(ctx, digest, current)
	if err != nil {
		return nil, fmt.Errorf("listSession: %w", err)
	}
	overviews := make([]sessionSummary, 0, maximumAccountSessions)
	now := time.Now()
	for key, entry := range e.sessions {
		if !now.Before(entry.expiresAt) {
			delete(e.sessions, key)
			continue
		}
		if entry.accountID != current.accountID || entry.revision != current.revision {
			continue
		}
		overviews = append(overviews, sessionSummary{ID: entry.id, CreatedAt: entry.createdAt,
			LastUsedAt: entry.lastUsedAt, ExpiresAt: entry.expiresAt, IsCurrent: key == digest})
	}
	sort.Slice(overviews, func(i, j int) bool {
		if overviews[i].CreatedAt.Equal(overviews[j].CreatedAt) {
			return overviews[i].ID < overviews[j].ID
		}
		return overviews[i].CreatedAt.After(overviews[j].CreatedAt)
	})
	return overviews, nil
}

func (e *Service) revokeSessions(ctx context.Context, digest [sha256.Size]byte, current session, id string, areOthers bool) (sessionRevocation, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	err := e.checkSessionLocked(ctx, digest, current)
	if err != nil {
		return sessionRevocation{}, fmt.Errorf("revokeSession: %w", err)
	}
	result := sessionRevocation{}
	now := time.Now()
	for key, entry := range e.sessions {
		if !now.Before(entry.expiresAt) {
			delete(e.sessions, key)
			continue
		}
		if entry.accountID != current.accountID {
			continue
		}
		if areOthers && key == digest || !areOthers && entry.id != id {
			continue
		}
		delete(e.sessions, key)
		result.RevokedCount++
		result.IsCurrent = result.IsCurrent || key == digest
	}
	if !areOthers && result.RevokedCount == 0 {
		return sessionRevocation{}, errSessionNotFound
	}
	return result, nil
}
