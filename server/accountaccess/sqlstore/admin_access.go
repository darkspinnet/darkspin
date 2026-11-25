package sqlstore

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
)

var _ accountaccess.AccessEditor = (*Store)(nil)

func (e *Store) ChangeAccess(ctx context.Context, req accountaccess.ChangeAccessRequest) (accountaccess.StatusChange, error) {
	if ctx == nil {
		return accountaccess.StatusChange{}, errors.New("nil context")
	}
	req, err := accountaccess.NormalizeChangeAccessRequest(req)
	if err != nil {
		return accountaccess.StatusChange{}, fmt.Errorf("accessValidate: %w", err)
	}
	tx, err := e.db.BeginTxx(ctx, nil)
	if err != nil {
		return accountaccess.StatusChange{}, fmt.Errorf("accessBegin: %w", err)
	}
	defer rollback(tx)
	// The existing lock authenticates the actor and locks both active accounts
	// in stable order. Both statuses remain locked until the audit commits.
	targetRow, err := e.lockActorTarget(ctx, tx, req.Actor, req.TargetUserID)
	if err != nil {
		return accountaccess.StatusChange{}, fmt.Errorf("accessLock: %w", err)
	}
	var actorRow accessRow
	err = tx.GetContext(ctx, &actorRow, tx.Rebind(readAccessQuery), req.Actor.UserID)
	if err != nil {
		return accountaccess.StatusChange{}, fmt.Errorf("accessActorRead: %w", err)
	}
	if targetRow.Status < 0 || targetRow.Revision <= 0 || targetRow.Status > actorRow.Status || req.Status > actorRow.Status {
		return accountaccess.StatusChange{}, fmt.Errorf("accessRank: %w", accountaccess.ErrAccessProtected)
	}
	if targetRow.Status == req.Status && targetRow.IPLimit == req.IPLimit {
		return accountaccess.StatusChange{}, fmt.Errorf("accessUnchanged: %w", accountaccess.ErrConflict)
	}
	if targetRow.Revision == math.MaxInt64 {
		return accountaccess.StatusChange{}, fmt.Errorf("accessRevision: %w", accountaccess.ErrRevisionExhausted)
	}
	before := targetRow.access()
	result, err := tx.ExecContext(ctx, tx.Rebind(`UPDATE account_access
		SET status = ?, ip_limit = ?, revision = revision + 1 WHERE user_id = ? AND revision = ?
		AND EXISTS (SELECT 1 FROM "user" WHERE id = ? AND trmt_id = 0)`),
		req.Status, req.IPLimit, targetRow.UserID, targetRow.Revision, targetRow.UserID)
	if err != nil {
		return accountaccess.StatusChange{}, fmt.Errorf("accessUpdate: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return accountaccess.StatusChange{}, fmt.Errorf("accessAffected: %w", err)
	}
	if affected != 1 {
		return accountaccess.StatusChange{}, fmt.Errorf("accessChanged: %w", accountaccess.ErrConflict)
	}
	targetRow.Status = req.Status
	targetRow.IPLimit = req.IPLimit
	targetRow.Revision++
	result, err = tx.ExecContext(ctx, tx.Rebind(`INSERT INTO admin_event
		(actor_type, actor_user_id, target_user_id, action, occurred_at_unix,
		before_detail, after_detail, reason, operation_id, outcome)
		VALUES ('account', ?, ?, 'account.access', ?, ?, ?, ?, '', 'completed')`),
		req.Actor.UserID, targetRow.UserID, time.Now().UTC().Unix(),
		statusDetail(before), statusDetail(targetRow.access()), req.Reason)
	if err != nil {
		return accountaccess.StatusChange{}, fmt.Errorf("accessAudit: %w", err)
	}
	affected, err = result.RowsAffected()
	if err != nil {
		return accountaccess.StatusChange{}, fmt.Errorf("accessAuditAffected: %w", err)
	}
	if affected != 1 {
		return accountaccess.StatusChange{}, errors.New("access audit did not create one event")
	}
	err = tx.Commit()
	if err != nil {
		return accountaccess.StatusChange{}, fmt.Errorf("accessCommit: %w", err)
	}
	return accountaccess.StatusChange{Before: before, After: targetRow.access()}, nil
}
