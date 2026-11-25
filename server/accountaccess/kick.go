package accountaccess

import (
	"context"
	"errors"
	"fmt"
)

type KickRequest struct {
	Actor        Actor
	TargetUserID int64
	Reason       string
}

type KickOperation struct {
	OperationID  string
	TargetUserID int64
}

type KickOutcomeRequest struct {
	OperationID string
	IsCompleted bool
}

type KickStore interface {
	BeginKick(context.Context, KickRequest) (KickOperation, error)
	CompleteKick(context.Context, KickOutcomeRequest) error
}

func NormalizeKickRequest(req KickRequest) (KickRequest, error) {
	moderation, err := NormalizeModerationRequest(ModerationRequest{
		Actor: req.Actor, TargetUserID: req.TargetUserID, Action: "ban", Reason: req.Reason,
	})
	if err != nil {
		return req, fmt.Errorf("kickRequest: %w", err)
	}
	req.Reason = moderation.Reason
	return req, nil
}

// BeginKick persists acceptance before the runtime attempts the external
// disconnect. Pending events remain observable after interruption; they are not
// automatically replayed against a later game session.
func (e *Service) BeginKick(ctx context.Context, req KickRequest) (KickOperation, error) {
	if ctx == nil {
		return KickOperation{}, errors.New("nil context")
	}
	req, err := NormalizeKickRequest(req)
	if err != nil {
		return KickOperation{}, fmt.Errorf("kickValidate: %w", err)
	}
	store, isSupported := e.store.(KickStore)
	if !isSupported {
		return KickOperation{}, errors.New("kick audit unavailable")
	}
	operation, err := store.BeginKick(ctx, req)
	if err != nil {
		return KickOperation{}, fmt.Errorf("kickAccept: %w", err)
	}
	return operation, nil
}

// CompleteKick is a trusted runtime callback, never a remote operation. It can
// complete accepted work after the initiating administrator loses permission.
func (e *Service) CompleteKick(ctx context.Context, req KickOutcomeRequest) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	store, isSupported := e.store.(KickStore)
	if !isSupported {
		return errors.New("kick audit unavailable")
	}
	err := store.CompleteKick(ctx, req)
	if err != nil {
		return fmt.Errorf("kickOutcome: %w", err)
	}
	return nil
}
