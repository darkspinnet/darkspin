package accountaccess

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type LoginHistoryReviewStore interface {
	RecordLoginHistoryReview(context.Context, Actor, int64, string, string) error
}

func ValidateLoginHistoryReview(actor Actor, targetID int64, rangeCode, reason string) error {
	err := ValidateActor(actor)
	if err != nil {
		return fmt.Errorf("loginReviewActor: %w", err)
	}
	if targetID <= 0 || strings.TrimSpace(reason) == "" || len(reason) > 512 || !isSafeOperationText(reason) {
		return fmt.Errorf("loginReviewRequest: %w", ErrInvalidRequest)
	}
	switch rangeCode {
	case "hour", "day", "week", "month", "year":
		return nil
	default:
		return fmt.Errorf("loginReviewRange: %w", ErrInvalidRequest)
	}
}

// RecordLoginHistoryReview audits a prepared administrator read before addresses
// are delivered. The store rechecks actor credentials and active target together.
func (e *Service) RecordLoginHistoryReview(ctx context.Context, actor Actor, targetID int64, rangeCode, reason string) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	err := ValidateLoginHistoryReview(actor, targetID, rangeCode, reason)
	if err != nil {
		return fmt.Errorf("loginReviewValidate: %w", err)
	}
	store, isSupported := e.store.(LoginHistoryReviewStore)
	if !isSupported {
		return errors.New("login history review audit unavailable")
	}
	err = store.RecordLoginHistoryReview(ctx, actor, targetID, rangeCode, strings.TrimSpace(reason))
	if err != nil {
		return fmt.Errorf("loginReviewRecord: %w", err)
	}
	return nil
}
