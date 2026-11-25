package accountaccess

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var ErrAccessProtected = errors.New("account access change is protected")

// ChangeAccessRequest changes both remote access settings as one audited command.
type ChangeAccessRequest struct {
	Actor        Actor
	TargetUserID int64
	Status       int64
	IPLimit      string
	Reason       string
}

type AccessEditor interface {
	ChangeAccess(context.Context, ChangeAccessRequest) (StatusChange, error)
}

func NormalizeChangeAccessRequest(req ChangeAccessRequest) (ChangeAccessRequest, error) {
	err := ValidateActor(req.Actor)
	if err != nil {
		return req, fmt.Errorf("accessActor: %w", err)
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.TargetUserID <= 0 || req.Status < 0 || req.Reason == "" || len(req.Reason) > 512 || !isSafeOperationText(req.Reason) {
		return req, ErrInvalidRequest
	}
	if req.TargetUserID == req.Actor.UserID {
		return req, fmt.Errorf("accessSelf: %w", ErrAccessProtected)
	}
	limit, err := NormalizeIPLimit(req.IPLimit)
	if err != nil {
		return req, fmt.Errorf("accessLimit: %w", ErrInvalidRequest)
	}
	if req.Status >= ProtectedStatus && limit == "" {
		return req, fmt.Errorf("accessProtected: %w", ErrInvalidRequest)
	}
	req.IPLimit = limit
	return req, nil
}

func (e *Service) ChangeAccess(ctx context.Context, req ChangeAccessRequest) (StatusChange, error) {
	if ctx == nil {
		return StatusChange{}, errors.New("nil context")
	}
	req, err := NormalizeChangeAccessRequest(req)
	if err != nil {
		return StatusChange{}, fmt.Errorf("accessRequest: %w", err)
	}
	store, isSupported := e.store.(AccessEditor)
	if !isSupported {
		return StatusChange{}, errors.New("account access editing unavailable")
	}
	change, err := store.ChangeAccess(ctx, req)
	if err != nil {
		return StatusChange{}, fmt.Errorf("accessSave: %w", err)
	}
	return change, nil
}
