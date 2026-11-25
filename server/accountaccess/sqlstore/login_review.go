package sqlstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
)

func (e *Store) RecordLoginHistoryReview(ctx context.Context, actor accountaccess.Actor, targetID int64, rangeCode, reason string) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	err := accountaccess.ValidateLoginHistoryReview(actor, targetID, rangeCode, reason)
	if err != nil {
		return fmt.Errorf("loginReviewValidate: %w", err)
	}
	tx, err := e.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("loginReviewBegin: %w", err)
	}
	defer rollback(tx)
	target, err := e.lockActorTarget(ctx, tx, actor, targetID)
	if err != nil {
		return fmt.Errorf("loginReviewLock: %w", err)
	}
	if target.Status < 0 || target.Revision <= 0 {
		return fmt.Errorf("loginReviewTarget: %w", accountaccess.ErrAccessDenied)
	}
	// rangeCode has a closed allowlist. Details never contain account records,
	// history payloads, credentials, network addresses, or arbitrary error text.
	detail := `{"range":"` + rangeCode + `"}`
	result, err := tx.ExecContext(ctx, tx.Rebind(`INSERT INTO admin_event
		(actor_type, actor_user_id, target_user_id, action, occurred_at_unix,
		before_detail, after_detail, reason, operation_id, outcome)
		VALUES ('account', ?, ?, 'account.loginHistory.read', ?, '{}', ?, ?, '', 'completed')`),
		actor.UserID, targetID, time.Now().UTC().Unix(), detail, reason)
	if err != nil {
		return fmt.Errorf("loginReviewInsert: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("loginReviewAffected: %w", err)
	}
	if affected != 1 {
		return errors.New("login history review did not create one event")
	}
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("loginReviewCommit: %w", err)
	}
	return nil
}
