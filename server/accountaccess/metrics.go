package accountaccess

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type MetricsReviewStore interface {
	RecordMetricsReview(context.Context, Actor, int64, string, string) error
}

func ValidateMetricsReview(actor Actor, targetID int64, rangeCode, reason string) error {
	err := ValidateActor(actor)
	if err != nil {
		return fmt.Errorf("metricsActor: %w", err)
	}
	if targetID <= 0 || strings.TrimSpace(reason) == "" || len(reason) > 512 || !isSafeOperationText(reason) {
		return fmt.Errorf("metricsRequest: %w", ErrInvalidRequest)
	}
	switch rangeCode {
	case "hour", "day", "week", "month", "year":
		return nil
	default:
		return fmt.Errorf("metricsRange: %w", ErrInvalidRequest)
	}
}

// RecordMetricsReview records a prepared administrator read before any metrics
// are delivered. The store rechecks actor credentials and active target together.
func (e *Service) RecordMetricsReview(ctx context.Context, actor Actor, targetID int64, rangeCode, reason string) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	err := ValidateMetricsReview(actor, targetID, rangeCode, reason)
	if err != nil {
		return fmt.Errorf("metricsValidate: %w", err)
	}
	store, isSupported := e.store.(MetricsReviewStore)
	if !isSupported {
		return errors.New("metrics review audit unavailable")
	}
	err = store.RecordMetricsReview(ctx, actor, targetID, rangeCode, strings.TrimSpace(reason))
	if err != nil {
		return fmt.Errorf("metricsRecord: %w", err)
	}
	return nil
}
