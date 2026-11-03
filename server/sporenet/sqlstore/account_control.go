package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/jmoiron/sqlx"
)

type accountControlRow struct {
	ID               int64  `db:"id"`
	LoginName        string `db:"login_name"`
	DisplayName      string `db:"display_name"`
	Password         string `db:"password"`
	Revision         int64  `db:"revision"`
	Status           int64  `db:"status"`
	IPLimit          string `db:"ip_limit"`
	IsBanned         bool   `db:"is_banned"`
	TimeoutUntilUnix int64  `db:"timeout_until_unix"`
}

func (e *Repository) LoadAccountCredential(ctx context.Context, userID int64) (sporenet.AccountCredential, error) {
	if ctx == nil || userID <= 0 {
		return sporenet.AccountCredential{}, errors.New("invalid credential lookup")
	}
	var row accountControlRow
	err := e.db.GetContext(ctx, &row, e.db.Rebind(`SELECT id, login_name, password
		FROM "user" WHERE id = ? AND trmt_id = 0`), userID)
	if errors.Is(err, sql.ErrNoRows) {
		return sporenet.AccountCredential{}, fmt.Errorf("credentialMissing: %w", sporenet.ErrUserNotFound)
	}
	if err != nil {
		return sporenet.AccountCredential{}, fmt.Errorf("credentialRead: %w", err)
	}
	return sporenet.AccountCredential{UserID: row.ID, LoginName: row.LoginName, Password: row.Password}, nil
}

func (e *Repository) lockAccountControl(
	ctx context.Context, tx *sqlx.Tx, req sporenet.AccountCredentialMatch,
) (accountControlRow, error) {
	query := `SELECT u.id, u.login_name, u.display_name, u.password, a.revision, a.status, a.ip_limit, a.is_banned, a.timeout_until_unix
		FROM "user" u JOIN account_access a ON a.user_id = u.id
		WHERE u.id = ? AND u.trmt_id = 0`
	if e.isPostgres {
		query += ` FOR UPDATE OF u, a`
	}
	var row accountControlRow
	err := tx.GetContext(ctx, &row, tx.Rebind(query), req.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		return accountControlRow{}, fmt.Errorf("controlMissing: %w", sporenet.ErrUserNotFound)
	}
	if err != nil {
		return accountControlRow{}, fmt.Errorf("controlRead: %w", err)
	}
	access := accountaccess.Access{UserID: row.ID, Revision: row.Revision, Status: row.Status,
		IPLimit: row.IPLimit, IsBanned: row.IsBanned, TimeoutUntilUnix: row.TimeoutUntilUnix}
	if access.IsBlocked() || req.Revision <= 0 || row.Revision != req.Revision {
		return accountControlRow{}, fmt.Errorf("controlAccess: %w", accountaccess.ErrAccessDenied)
	}
	if row.Password != req.ExpectedPassword {
		return accountControlRow{}, fmt.Errorf("controlCredential: %w", sporenet.ErrPasswordIncorrect)
	}
	err = accountaccess.CheckPeer(ctx, access)
	if err != nil {
		return accountControlRow{}, fmt.Errorf("controlPeer: %w", err)
	}
	return row, nil
}

func (e *Repository) RenameAccount(ctx context.Context, req sporenet.AccountRename) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	tx, err := e.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("renameBegin: %w", err)
	}
	defer rollback(tx)
	row, err := e.lockAccountControl(ctx, tx, req.Match)
	if err != nil {
		return fmt.Errorf("renameConfirm: %w", err)
	}
	var count int
	err = tx.GetContext(ctx, &count, tx.Rebind(`SELECT COUNT(*) FROM "user"
		WHERE LOWER(display_name) = LOWER(?) AND id <> ?`), req.DisplayName, row.ID)
	if err != nil {
		return fmt.Errorf("renameCollision: %w", err)
	}
	if count != 0 {
		return fmt.Errorf("renameOccupied: %w", sporenet.ErrUserExists)
	}
	result, err := tx.ExecContext(ctx, tx.Rebind(`UPDATE "user" SET display_name = ?
		WHERE id = ? AND trmt_id = 0 AND password = ?`), req.DisplayName, row.ID, req.Match.ExpectedPassword)
	if err != nil {
		return fmt.Errorf("renameUpdate: %w", err)
	}
	err = requireAccountChange(result)
	if err != nil {
		return fmt.Errorf("renameAffected: %w", err)
	}
	associationResult, err := tx.ExecContext(ctx, tx.Rebind(`UPDATE user_association SET name = ? WHERE member_id = ?`), req.DisplayName, row.ID)
	if err != nil {
		return fmt.Errorf("renameAssociation: %w", err)
	}
	associationCount, err := associationResult.RowsAffected()
	if err != nil || associationCount < 0 {
		return fmt.Errorf("associationAffected: %w", errors.Join(err, errors.New("association update count unavailable")))
	}
	beforeDetail, err := json.Marshal(struct {
		DisplayName string `json:"display_name"`
	}{DisplayName: row.DisplayName})
	if err != nil {
		return fmt.Errorf("renameBefore: %w", err)
	}
	afterDetail, err := json.Marshal(struct {
		DisplayName string `json:"display_name"`
	}{DisplayName: req.DisplayName})
	if err != nil {
		return fmt.Errorf("renameAfter: %w", err)
	}
	err = auditAccountControl(ctx, tx, row.ID, "account.rename", string(beforeDetail), string(afterDetail))
	if err != nil {
		return fmt.Errorf("renameAudit: %w", err)
	}
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("renameCommit: %w", err)
	}
	return nil
}

func (e *Repository) ChangeAccountPassword(ctx context.Context, req sporenet.AccountPasswordChange) error {
	if ctx == nil || req.Password == "" {
		return errors.New("invalid password change")
	}
	tx, err := e.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("passwordBegin: %w", err)
	}
	defer rollback(tx)
	row, err := e.lockAccountControl(ctx, tx, req.Match)
	if err != nil {
		return fmt.Errorf("passwordConfirm: %w", err)
	}
	result, err := tx.ExecContext(ctx, tx.Rebind(`UPDATE "user" SET password = ?
		WHERE id = ? AND trmt_id = 0 AND password = ?`), req.Password, row.ID, req.Match.ExpectedPassword)
	if err != nil {
		return fmt.Errorf("passwordUpdate: %w", err)
	}
	err = requireAccountChange(result)
	if err != nil {
		return fmt.Errorf("passwordAffected: %w", err)
	}
	err = e.AccountAccess().InvalidateUser(ctx, tx, row.ID)
	if err != nil {
		return fmt.Errorf("passwordRevision: %w", err)
	}
	err = auditAccountControl(ctx, tx, row.ID, "account.password", "", "")
	if err != nil {
		return fmt.Errorf("passwordAudit: %w", err)
	}
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("passwordCommit: %w", err)
	}
	return nil
}

func (e *Repository) DeleteAccount(ctx context.Context, req sporenet.AccountCredentialMatch) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	tx, err := e.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("deleteBegin: %w", err)
	}
	defer rollback(tx)
	row, err := e.lockAccountControl(ctx, tx, req)
	if err != nil {
		return fmt.Errorf("deleteConfirm: %w", err)
	}
	err = e.archiveAccount(ctx, tx, row.ID, row.LoginName, row.DisplayName, req.ExpectedPassword)
	if err != nil {
		return fmt.Errorf("deleteArchive: %w", err)
	}
	err = auditAccountControl(ctx, tx, row.ID, "account.delete", "", "")
	if err != nil {
		return fmt.Errorf("deleteAudit: %w", err)
	}
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("deleteCommit: %w", err)
	}
	return nil
}

func requireAccountChange(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("accountAffected: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("accountChanged: %w", sporenet.ErrUserNotFound)
	}
	return nil
}

func auditAccountControl(ctx context.Context, tx *sqlx.Tx, userID int64, action, beforeDetail, afterDetail string) error {
	result, err := tx.ExecContext(ctx, tx.Rebind(`INSERT INTO admin_event
		(actor_type, actor_user_id, target_user_id, action, occurred_at_unix,
		before_detail, after_detail, reason, operation_id, outcome)
		VALUES ('account', ?, ?, ?, ?, ?, ?, '', '', 'completed')`),
		userID, userID, action, time.Now().UTC().Unix(), beforeDetail, afterDetail)
	if err != nil {
		return fmt.Errorf("accountAudit: %w", err)
	}
	err = requireAccountChange(result)
	if err != nil {
		return fmt.Errorf("auditCount: %w", err)
	}
	return nil
}
