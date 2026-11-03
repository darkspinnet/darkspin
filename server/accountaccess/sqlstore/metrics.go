package sqlstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
)

var _ accountaccess.MetricsReviewStore = (*Store)(nil)

func (e *Store) RecordMetricsReview(ctx context.Context, actor accountaccess.Actor, targetID int64, rangeCode, reason string) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	err := accountaccess.ValidateMetricsReview(actor, targetID, rangeCode, reason)
	if err != nil {
		return fmt.Errorf("metricsValidate: %w", err)
	}
	tx, err := e.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("metricsBegin: %w", err)
	}
	defer rollback(tx)
	target, err := e.lockActorTarget(ctx, tx, actor, targetID)
	if err != nil {
		return fmt.Errorf("metricsLock: %w", err)
	}
	if target.Status < 0 || target.Revision <= 0 {
		return fmt.Errorf("metricsTarget: %w", accountaccess.ErrAccessDenied)
	}
	// rangeCode has a closed allowlist. Details never contain account records,
	// metrics payloads, credentials, network addresses, or arbitrary error text.
	detail := `{"range":"` + rangeCode + `"}`
	result, err := tx.ExecContext(ctx, tx.Rebind(`INSERT INTO admin_event
		(actor_type, actor_user_id, target_user_id, action, occurred_at_unix,
		before_detail, after_detail, reason, operation_id, outcome)
		VALUES ('account', ?, ?, 'account.metrics.read', ?, '{}', ?, ?, '', 'completed')`),
		actor.UserID, targetID, time.Now().UTC().Unix(), detail, reason)
	if err != nil {
		return fmt.Errorf("metricsInsert: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("metricsAffected: %w", err)
	}
	if affected != 1 {
		return errors.New("metrics review did not create one event")
	}
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("metricsCommit: %w", err)
	}
	return nil
}
