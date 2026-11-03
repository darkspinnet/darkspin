package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
)

var _ accountaccess.IPLimitStore = (*Store)(nil)

// SetIPLimit is a trusted local-operator command. The revision update and audit
// event commit together so existing credentials cannot retain the old policy.
func (e *Store) SetIPLimit(ctx context.Context, req accountaccess.SetIPLimitRequest) (accountaccess.IPLimitChange, error) {
	if ctx == nil {
		return accountaccess.IPLimitChange{}, errors.New("nil context")
	}
	err := accountaccess.ValidateIPLimitRequest(req)
	if err != nil {
		return accountaccess.IPLimitChange{}, fmt.Errorf("ipValidate: %w", err)
	}
	req.IPLimit, err = accountaccess.NormalizeIPLimit(req.IPLimit)
	if err != nil {
		return accountaccess.IPLimitChange{}, fmt.Errorf("ipNormalize: %w", err)
	}
	tx, err := e.db.BeginTxx(ctx, nil)
	if err != nil {
		return accountaccess.IPLimitChange{}, fmt.Errorf("ipBegin: %w", err)
	}
	defer rollback(tx)
	query := readAccessQuery
	var accountKey any = req.UserID
	if req.LoginName != "" {
		query = `SELECT a.user_id, u.login_name, a.status, a.is_banned, a.ban_reason, a.timeout_until_unix, a.timeout_reason, a.ip_limit, a.revision
			FROM account_access a JOIN "user" u ON u.id = a.user_id
			WHERE u.login_name = ? AND u.trmt_id = 0`
		accountKey = req.LoginName
	}
	if e.isPostgres {
		query += ` FOR UPDATE OF u, a`
	}
	var row accessRow
	err = tx.GetContext(ctx, &row, tx.Rebind(query), accountKey)
	if errors.Is(err, sql.ErrNoRows) {
		return accountaccess.IPLimitChange{}, fmt.Errorf("ipMissing: %w", accountaccess.ErrNotFound)
	}
	if err != nil {
		return accountaccess.IPLimitChange{}, fmt.Errorf("ipRead: %w", err)
	}
	if row.Revision == math.MaxInt64 {
		return accountaccess.IPLimitChange{}, fmt.Errorf("ipRevision: %w", accountaccess.ErrRevisionExhausted)
	}
	if row.Status < 0 || row.Revision <= 0 {
		return accountaccess.IPLimitChange{}, fmt.Errorf("ipState: %w", accountaccess.ErrAccessDenied)
	}
	before := row.access()
	result, err := tx.ExecContext(ctx, tx.Rebind(`UPDATE account_access
		SET ip_limit = ?, revision = revision + 1 WHERE user_id = ? AND revision = ?
		AND EXISTS (SELECT 1 FROM "user" WHERE id = ? AND trmt_id = 0)`),
		req.IPLimit, row.UserID, row.Revision, row.UserID)
	if err != nil {
		return accountaccess.IPLimitChange{}, fmt.Errorf("ipUpdate: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return accountaccess.IPLimitChange{}, fmt.Errorf("ipAffected: %w", err)
	}
	if affected != 1 {
		return accountaccess.IPLimitChange{}, fmt.Errorf("ipChanged: %w", accountaccess.ErrNotFound)
	}
	row.IPLimit = req.IPLimit
	row.Revision++
	result, err = tx.ExecContext(ctx, tx.Rebind(`INSERT INTO admin_event
		(actor_type, actor_user_id, target_user_id, action, occurred_at_unix,
		before_detail, after_detail, reason, operation_id, outcome)
		VALUES ('local_operator', NULL, ?, 'account.ipLimit', ?, ?, ?, ?, '', 'completed')`),
		row.UserID, time.Now().UTC().Unix(), ipLimitDetail(before), ipLimitDetail(row.access()), req.Reason)
	if err != nil {
		return accountaccess.IPLimitChange{}, fmt.Errorf("ipAudit: %w", err)
	}
	affected, err = result.RowsAffected()
	if err != nil {
		return accountaccess.IPLimitChange{}, fmt.Errorf("ipAuditAffected: %w", err)
	}
	if affected != 1 {
		return accountaccess.IPLimitChange{}, errors.New("IP limit audit did not create one event")
	}
	err = tx.Commit()
	if err != nil {
		return accountaccess.IPLimitChange{}, fmt.Errorf("ipCommit: %w", err)
	}
	return accountaccess.IPLimitChange{Before: before, After: row.access()}, nil
}

func ipLimitDetail(access accountaccess.Access) string {
	return `{"ip_limit":` + strconv.Quote(access.IPLimit) + `}`
}
