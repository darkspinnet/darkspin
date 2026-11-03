// Package sqlstore persists account access and its append-only administration log.
package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"strconv"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
	"github.com/jmoiron/sqlx"
)

type Store struct {
	db         *sqlx.DB
	isPostgres bool
}

var _ accountaccess.Store = (*Store)(nil)

// New borrows an existing profile connection pool; the caller retains ownership.
func New(db *sqlx.DB, isPostgres bool) *Store {
	return &Store{db: db, isPostgres: isPostgres}
}

type accessRow struct {
	UserID           int64  `db:"user_id"`
	LoginName        string `db:"login_name"`
	Status           int64  `db:"status"`
	IPLimit          string `db:"ip_limit"`
	IsBanned         bool   `db:"is_banned"`
	BanReason        string `db:"ban_reason"`
	TimeoutUntilUnix int64  `db:"timeout_until_unix"`
	TimeoutReason    string `db:"timeout_reason"`
	Revision         int64  `db:"revision"`
}

func (e accessRow) access() accountaccess.Access {
	return accountaccess.Access{
		UserID: e.UserID, LoginName: e.LoginName, Status: e.Status, IPLimit: e.IPLimit,
		IsBanned: e.IsBanned, BanReason: e.BanReason, Revision: e.Revision,
		TimeoutUntilUnix: e.TimeoutUntilUnix, TimeoutReason: e.TimeoutReason,
	}
}

const readAccessQuery = `SELECT a.user_id, u.login_name, a.status, a.is_banned, a.ban_reason, a.timeout_until_unix, a.timeout_reason, a.ip_limit, a.revision
	FROM account_access a JOIN "user" u ON u.id = a.user_id
	WHERE u.id = ? AND u.trmt_id = 0`

func (e *Store) Read(ctx context.Context, userID int64) (accountaccess.Access, error) {
	if ctx == nil {
		return accountaccess.Access{}, errors.New("nil context")
	}
	var row accessRow
	err := e.db.GetContext(ctx, &row, e.db.Rebind(readAccessQuery), userID)
	if errors.Is(err, sql.ErrNoRows) {
		return accountaccess.Access{}, fmt.Errorf("readMissing: %w", accountaccess.ErrNotFound)
	}
	if err != nil {
		return accountaccess.Access{}, fmt.Errorf("readSelect: %w", err)
	}
	if row.Status < 0 || row.Revision <= 0 {
		return accountaccess.Access{}, fmt.Errorf("readState: %w", accountaccess.ErrAccessDenied)
	}
	return row.access(), nil
}

func (e *Store) SetStatus(ctx context.Context, req accountaccess.SetStatusRequest) (accountaccess.StatusChange, error) {
	if ctx == nil {
		return accountaccess.StatusChange{}, errors.New("nil context")
	}
	err := accountaccess.ValidateStatusRequest(req)
	if err != nil {
		return accountaccess.StatusChange{}, fmt.Errorf("statusValidate: %w", err)
	}
	if req.IPLimit != nil {
		ipLimit, normalizeErr := accountaccess.NormalizeIPLimit(*req.IPLimit)
		if normalizeErr != nil {
			return accountaccess.StatusChange{}, fmt.Errorf("statusIPLimit: %w", normalizeErr)
		}
		req.IPLimit = &ipLimit
	}
	tx, err := e.db.BeginTxx(ctx, nil)
	if err != nil {
		return accountaccess.StatusChange{}, fmt.Errorf("statusBegin: %w", err)
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
		return accountaccess.StatusChange{}, fmt.Errorf("statusMissing: %w", accountaccess.ErrNotFound)
	}
	if err != nil {
		return accountaccess.StatusChange{}, fmt.Errorf("statusRead: %w", err)
	}
	if row.Revision == math.MaxInt64 {
		return accountaccess.StatusChange{}, fmt.Errorf("statusRevision: %w", accountaccess.ErrRevisionExhausted)
	}
	if row.Status < 0 || row.Revision <= 0 {
		return accountaccess.StatusChange{}, fmt.Errorf("statusState: %w", accountaccess.ErrAccessDenied)
	}
	before := row.access()
	ipLimit := row.IPLimit
	if req.IPLimit != nil {
		ipLimit = *req.IPLimit
	}
	result, err := tx.ExecContext(ctx, tx.Rebind(`UPDATE account_access
		SET status = ?, ip_limit = ?, revision = revision + 1 WHERE user_id = ? AND revision = ?
		AND EXISTS (SELECT 1 FROM "user" WHERE id = ? AND trmt_id = 0)`),
		req.Status, ipLimit, row.UserID, row.Revision, row.UserID)
	if err != nil {
		return accountaccess.StatusChange{}, fmt.Errorf("statusUpdate: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return accountaccess.StatusChange{}, fmt.Errorf("statusAffected: %w", err)
	}
	if affected != 1 {
		return accountaccess.StatusChange{}, fmt.Errorf("statusChanged: %w", accountaccess.ErrNotFound)
	}
	row.Status = req.Status
	row.IPLimit = ipLimit
	row.Revision++
	result, err = tx.ExecContext(ctx, tx.Rebind(`INSERT INTO admin_event
		(actor_type, actor_user_id, target_user_id, action, occurred_at_unix,
		before_detail, after_detail, reason, operation_id, outcome)
		VALUES ('local_operator', NULL, ?, 'account.status', ?, ?, ?, ?, '', 'completed')`),
		row.UserID, time.Now().UTC().Unix(), statusDetail(before), statusDetail(row.access()), req.Reason)
	if err != nil {
		return accountaccess.StatusChange{}, fmt.Errorf("statusAudit: %w", err)
	}
	affected, err = result.RowsAffected()
	if err != nil {
		return accountaccess.StatusChange{}, fmt.Errorf("auditAffected: %w", err)
	}
	if affected != 1 {
		return accountaccess.StatusChange{}, errors.New("audit insertion did not create one event")
	}
	err = tx.Commit()
	if err != nil {
		return accountaccess.StatusChange{}, fmt.Errorf("statusCommit: %w", err)
	}
	return accountaccess.StatusChange{Before: before, After: row.access()}, nil
}

func statusDetail(access accountaccess.Access) string {
	return `{"status":` + strconv.FormatInt(access.Status, 10) +
		`,"is_admin":` + strconv.FormatBool(access.IsAdmin()) +
		`,"ip_limit":` + strconv.Quote(access.IPLimit) + `}`
}

func rollback(tx *sqlx.Tx) {
	err := tx.Rollback()
	if err != nil && !errors.Is(err, sql.ErrTxDone) {
		log.Printf("Account access transaction rollback failed: %v", err)
	}
}
