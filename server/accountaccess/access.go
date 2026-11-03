// Package accountaccess owns persisted account permissions independently of profiles.
package accountaccess

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrNotFound          = errors.New("active account access not found")
	ErrAccessDenied      = errors.New("account access denied")
	ErrInvalidStatus     = errors.New("status must be a nonnegative integer")
	ErrRevisionExhausted = errors.New("account credential revision exhausted")
)

// Access is the current access state of an active immutable account ID.
// It is an internal feature result, never a response DTO.
type Access struct {
	UserID           int64
	LoginName        string
	Status           int64
	IPLimit          string
	IsBanned         bool
	BanReason        string
	TimeoutUntilUnix int64
	TimeoutReason    string
	Revision         int64
}

func (e Access) IsAdmin() bool {
	return !e.IsBlocked() && e.Status > 250
}

func (e Access) IsTimedOut() bool {
	return e.TimeoutUntilUnix > time.Now().UTC().Unix()
}

func (e Access) IsBlocked() bool {
	return e.IsBanned || e.IsTimedOut()
}

// SetStatusRequest is a trusted local-operator command. Remote role changes are
// deliberately not supported by this operation.
type SetStatusRequest struct {
	IPLimit   *string
	UserID    int64
	LoginName string
	Status    int64
	Reason    string
}

type StatusChange struct {
	Before Access
	After  Access
}

// Store commits status changes and their audit event in the same transaction.
type Store interface {
	Read(context.Context, int64) (Access, error)
	SetStatus(context.Context, SetStatusRequest) (StatusChange, error)
}

type Service struct {
	store Store
}

func New(store Store) *Service {
	return &Service{store: store}
}

func (e *Service) Read(ctx context.Context, userID int64) (Access, error) {
	if ctx == nil {
		return Access{}, errors.New("nil context")
	}
	if userID <= 0 {
		return Access{}, fmt.Errorf("readID: %w", ErrNotFound)
	}
	access, err := e.store.Read(ctx, userID)
	if err != nil {
		return Access{}, fmt.Errorf("readAccess: %w", err)
	}
	return access, nil
}

// Authorize re-reads persisted state; callers must supply the authenticated
// immutable ID and credential revision, never a request's claimed role.
func (e *Service) Authorize(ctx context.Context, userID, revision int64, isAdminRequired bool) (Access, error) {
	access, err := e.Read(ctx, userID)
	if err != nil {
		return Access{}, fmt.Errorf("authorizeRead: %w", err)
	}
	if access.UserID != userID || access.IsBlocked() || revision <= 0 || access.Revision != revision {
		return Access{}, fmt.Errorf("authorizeState: %w", ErrAccessDenied)
	}
	if isAdminRequired && !access.IsAdmin() {
		return Access{}, fmt.Errorf("authorizeAdmin: %w", ErrAccessDenied)
	}
	err = CheckPeer(ctx, access)
	if err != nil {
		return Access{}, fmt.Errorf("authorizePeer: %w", err)
	}
	return access, nil
}

func ValidateStatusRequest(req SetStatusRequest) error {
	if req.UserID < 0 || (req.UserID > 0) == (req.LoginName != "") || strings.TrimSpace(req.LoginName) != req.LoginName {
		return fmt.Errorf("statusID: %w", ErrNotFound)
	}
	if req.Status < 0 {
		return ErrInvalidStatus
	}
	if len(req.Reason) > 512 {
		return errors.New("status reason exceeds 512 bytes")
	}
	return nil
}

// SetStatus changes local-operator permissions and invalidates old credentials.
// Do not expose this command through an authenticated remote profile patch.
func (e *Service) SetStatus(ctx context.Context, req SetStatusRequest) (StatusChange, error) {
	if ctx == nil {
		return StatusChange{}, errors.New("nil context")
	}
	err := ValidateStatusRequest(req)
	if err != nil {
		return StatusChange{}, fmt.Errorf("statusValidate: %w", err)
	}
	if req.IPLimit != nil {
		limit, normalizeErr := NormalizeIPLimit(*req.IPLimit)
		if normalizeErr != nil {
			return StatusChange{}, fmt.Errorf("statusLimit: %w", normalizeErr)
		}
		req.IPLimit = &limit
	}
	change, err := e.store.SetStatus(ctx, req)
	if err != nil {
		return StatusChange{}, fmt.Errorf("statusSave: %w", err)
	}
	return change, nil
}
