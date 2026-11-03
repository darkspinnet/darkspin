package accountaccess

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrInvalidRequest = errors.New("invalid administration request")
	ErrConflict       = errors.New("account moderation state already matches request")
)

type Actor struct {
	UserID   int64
	Revision int64
}

type UserListRequest struct {
	AfterUserID int64
	Limit       int
	Search      string
}

type UserSummary struct {
	UserID           int64
	LoginName        string
	DisplayName      string
	Level            int64
	Status           int64
	IsBanned         bool
	TimeoutUntilUnix int64
	IsTimedOut       bool
}

type UserList struct {
	Users      []UserSummary
	NextUserID int64
}

type ModerationRequest struct {
	Actor           Actor
	TargetUserID    int64
	Action          string
	Reason          string
	DurationMinutes int64
}

type ModerationResult struct {
	Before Access
	After  Access
}

type HistoryRequest struct {
	BeforeEventID int64
	TargetUserID  int64
	ActorUserID   int64
	ActorType     string
	AfterUnix     int64
	BeforeUnix    int64
	Action        string
	Outcome       string
	OperationID   string
	Limit         int
}

// AdminEvent is an administrator-only projection; details are generated from
// explicit access fields rather than serializing account storage records.
type AdminEvent struct {
	ID             int64
	ActorType      string
	ActorUserID    int64
	TargetUserID   int64
	Action         string
	OccurredAtUnix int64
	BeforeDetail   string
	AfterDetail    string
	Reason         string
	OperationID    string
	Outcome        string
}

type History struct {
	Events      []AdminEvent
	NextEventID int64
}

// ModerationStore rechecks administrator credentials inside each transaction.
type ModerationStore interface {
	ListUsers(context.Context, Actor, UserListRequest) (UserList, error)
	Moderate(context.Context, ModerationRequest) (ModerationResult, error)
	ListHistory(context.Context, Actor, HistoryRequest) (History, error)
}

func ValidateActor(actor Actor) error {
	if actor.UserID <= 0 || actor.Revision <= 0 {
		return ErrAccessDenied
	}
	return nil
}

func NormalizeUserListRequest(req UserListRequest) (UserListRequest, error) {
	if req.Limit == 0 {
		req.Limit = 50
	}
	req.Search = strings.TrimSpace(req.Search)
	if req.AfterUserID < 0 || req.Limit < 1 || req.Limit > 100 || len(req.Search) > 80 {
		return req, ErrInvalidRequest
	}
	return req, nil
}

func NormalizeModerationRequest(req ModerationRequest) (ModerationRequest, error) {
	err := ValidateActor(req.Actor)
	if err != nil {
		return req, fmt.Errorf("moderationActor: %w", err)
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.TargetUserID <= 0 || (req.Action != "ban" && req.Action != "unban" && req.Action != "timeout") ||
		req.Reason == "" || len(req.Reason) > 512 {
		return req, ErrInvalidRequest
	}
	if req.Action == "timeout" {
		if req.DurationMinutes < 1 || req.DurationMinutes > 525600 {
			return req, ErrInvalidRequest
		}
	} else if req.DurationMinutes != 0 {
		return req, ErrInvalidRequest
	}
	if req.TargetUserID == req.Actor.UserID {
		return req, fmt.Errorf("moderationSelf: %w", ErrAccessDenied)
	}
	return req, nil
}

func NormalizeHistoryRequest(req HistoryRequest) (HistoryRequest, error) {
	if req.Limit == 0 {
		req.Limit = 50
	}
	req.ActorType = strings.TrimSpace(req.ActorType)
	req.Action = strings.TrimSpace(req.Action)
	req.Outcome = strings.TrimSpace(req.Outcome)
	req.OperationID = strings.TrimSpace(req.OperationID)
	if req.BeforeEventID < 0 || req.TargetUserID < 0 || req.ActorUserID < 0 ||
		req.AfterUnix < 0 || req.BeforeUnix < 0 || req.Limit < 1 || req.Limit > 100 ||
		!isHistoryFilterToken(req.ActorType, 32) || !isHistoryFilterToken(req.Action, 80) ||
		!isHistoryFilterToken(req.Outcome, 32) || !isHistoryFilterToken(req.OperationID, 128) {
		return req, ErrInvalidRequest
	}
	if req.BeforeUnix > 0 && req.BeforeUnix <= req.AfterUnix {
		return req, ErrInvalidRequest
	}
	return req, nil
}

// Empty tokens omit a filter. Unknown tokens remain valid exact-match queries.
func isHistoryFilterToken(token string, maxLength int) bool {
	if len(token) > maxLength {
		return false
	}
	for _, character := range token {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '.' || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func (e *Service) ListUsers(ctx context.Context, actor Actor, req UserListRequest) (UserList, error) {
	if ctx == nil {
		return UserList{}, errors.New("nil context")
	}
	err := ValidateActor(actor)
	if err != nil {
		return UserList{}, fmt.Errorf("usersActor: %w", err)
	}
	req, err = NormalizeUserListRequest(req)
	if err != nil {
		return UserList{}, fmt.Errorf("usersRequest: %w", err)
	}
	store, isSupported := e.store.(ModerationStore)
	if !isSupported {
		return UserList{}, errors.New("account moderation unavailable")
	}
	result, err := store.ListUsers(ctx, actor, req)
	if err != nil {
		return UserList{}, fmt.Errorf("usersRead: %w", err)
	}
	return result, nil
}

func (e *Service) Moderate(ctx context.Context, req ModerationRequest) (ModerationResult, error) {
	if ctx == nil {
		return ModerationResult{}, errors.New("nil context")
	}
	req, err := NormalizeModerationRequest(req)
	if err != nil {
		return ModerationResult{}, fmt.Errorf("moderationRequest: %w", err)
	}
	store, isSupported := e.store.(ModerationStore)
	if !isSupported {
		return ModerationResult{}, errors.New("account moderation unavailable")
	}
	result, err := store.Moderate(ctx, req)
	if err != nil {
		return ModerationResult{}, fmt.Errorf("moderationSave: %w", err)
	}
	return result, nil
}

func (e *Service) ListHistory(ctx context.Context, actor Actor, req HistoryRequest) (History, error) {
	if ctx == nil {
		return History{}, errors.New("nil context")
	}
	err := ValidateActor(actor)
	if err != nil {
		return History{}, fmt.Errorf("historyActor: %w", err)
	}
	req, err = NormalizeHistoryRequest(req)
	if err != nil {
		return History{}, fmt.Errorf("historyRequest: %w", err)
	}
	store, isSupported := e.store.(ModerationStore)
	if !isSupported {
		return History{}, errors.New("account moderation unavailable")
	}
	result, err := store.ListHistory(ctx, actor, req)
	if err != nil {
		return History{}, fmt.Errorf("historyRead: %w", err)
	}
	return result, nil
}
