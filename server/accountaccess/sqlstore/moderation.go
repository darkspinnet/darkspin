package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
	"github.com/jmoiron/sqlx"
)

var _ accountaccess.ModerationStore = (*Store)(nil)

func checkActor(ctx context.Context, row accessRow, actor accountaccess.Actor) error {
	if row.UserID != actor.UserID || actor.Revision <= 0 || row.Revision != actor.Revision ||
		!row.access().IsAdmin() {
		return accountaccess.ErrAccessDenied
	}
	err := accountaccess.CheckPeer(ctx, row.access())
	if err != nil {
		return fmt.Errorf("actorPeer: %w", err)
	}
	return nil
}

func (e *Store) authorizeActor(ctx context.Context, tx *sqlx.Tx, actor accountaccess.Actor) error {
	query := readAccessQuery
	if e.isPostgres {
		query += ` FOR SHARE OF u, a`
	}
	var row accessRow
	err := tx.GetContext(ctx, &row, tx.Rebind(query), actor.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("actorMissing: %w", accountaccess.ErrAccessDenied)
	}
	if err != nil {
		return fmt.Errorf("actorRead: %w", err)
	}
	err = checkActor(ctx, row, actor)
	if err != nil {
		return fmt.Errorf("actorCheck: %w", err)
	}
	return nil
}

func (e *Store) Moderate(ctx context.Context, req accountaccess.ModerationRequest) (accountaccess.ModerationResult, error) {
	if ctx == nil {
		return accountaccess.ModerationResult{}, errors.New("nil context")
	}
	req, err := accountaccess.NormalizeModerationRequest(req)
	if err != nil {
		return accountaccess.ModerationResult{}, fmt.Errorf("moderationValidate: %w", err)
	}
	tx, err := e.db.BeginTxx(ctx, nil)
	if err != nil {
		return accountaccess.ModerationResult{}, fmt.Errorf("moderationBegin: %w", err)
	}
	defer rollback(tx)
	targetRow, err := e.lockActorTarget(ctx, tx, req.Actor, req.TargetUserID)
	if err != nil {
		return accountaccess.ModerationResult{}, fmt.Errorf("moderationLock: %w", err)
	}
	// Status alone protects administrators, even when an earlier ban blocks
	// their access. Remote moderation cannot become administrator recovery.
	if targetRow.Status > 250 {
		return accountaccess.ModerationResult{}, fmt.Errorf("moderationProtected: %w", accountaccess.ErrAccessDenied)
	}
	if targetRow.Status < 0 || targetRow.Revision <= 0 {
		return accountaccess.ModerationResult{}, fmt.Errorf("moderationState: %w", accountaccess.ErrAccessDenied)
	}
	if (req.Action == "ban" || req.Action == "timeout") && targetRow.IsBanned ||
		req.Action == "unban" && !targetRow.IsBanned && targetRow.TimeoutUntilUnix == 0 {
		return accountaccess.ModerationResult{}, fmt.Errorf("moderationUnchanged: %w", accountaccess.ErrConflict)
	}
	if targetRow.Revision == math.MaxInt64 {
		return accountaccess.ModerationResult{}, fmt.Errorf("moderationRevision: %w", accountaccess.ErrRevisionExhausted)
	}
	before := targetRow.access()
	isBanned := req.Action == "ban"
	banReason := ""
	banFlag := int64(0)
	timeoutUntilUnix := int64(0)
	timeoutReason := ""
	occurredAtUnix := time.Now().UTC().Unix()
	if isBanned {
		banReason = req.Reason
		banFlag = 1
	}
	if req.Action == "timeout" {
		timeoutUntilUnix = occurredAtUnix + req.DurationMinutes*60
		timeoutReason = req.Reason
	}
	result, err := tx.ExecContext(ctx, tx.Rebind(`UPDATE account_access
		SET is_banned = ?, ban_reason = ?, timeout_until_unix = ?, timeout_reason = ?, revision = revision + 1
		WHERE user_id = ? AND revision = ?`), banFlag, banReason, timeoutUntilUnix, timeoutReason,
		targetRow.UserID, targetRow.Revision)
	if err != nil {
		return accountaccess.ModerationResult{}, fmt.Errorf("moderationUpdate: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return accountaccess.ModerationResult{}, fmt.Errorf("moderationAffected: %w", err)
	}
	if affected != 1 {
		return accountaccess.ModerationResult{}, fmt.Errorf("moderationChanged: %w", accountaccess.ErrConflict)
	}
	targetRow.IsBanned = isBanned
	targetRow.BanReason = banReason
	targetRow.TimeoutUntilUnix = timeoutUntilUnix
	targetRow.TimeoutReason = timeoutReason
	targetRow.Revision++
	beforeDetail, err := moderationDetail(before)
	if err != nil {
		return accountaccess.ModerationResult{}, fmt.Errorf("moderationBefore: %w", err)
	}
	afterDetail, err := moderationDetail(targetRow.access())
	if err != nil {
		return accountaccess.ModerationResult{}, fmt.Errorf("moderationAfter: %w", err)
	}
	result, err = tx.ExecContext(ctx, tx.Rebind(`INSERT INTO admin_event
		(actor_type, actor_user_id, target_user_id, action, occurred_at_unix,
		before_detail, after_detail, reason, operation_id, outcome)
		VALUES ('account', ?, ?, ?, ?, ?, ?, ?, '', 'completed')`),
		req.Actor.UserID, req.TargetUserID, "account."+req.Action, occurredAtUnix,
		beforeDetail, afterDetail, req.Reason)
	if err != nil {
		return accountaccess.ModerationResult{}, fmt.Errorf("moderationAudit: %w", err)
	}
	affected, err = result.RowsAffected()
	if err != nil {
		return accountaccess.ModerationResult{}, fmt.Errorf("moderationAuditAffected: %w", err)
	}
	if affected != 1 {
		return accountaccess.ModerationResult{}, errors.New("moderation audit did not create one event")
	}
	err = tx.Commit()
	if err != nil {
		return accountaccess.ModerationResult{}, fmt.Errorf("moderationCommit: %w", err)
	}
	return accountaccess.ModerationResult{Before: before, After: targetRow.access()}, nil
}

func moderationDetail(access accountaccess.Access) (string, error) {
	detail := struct {
		Status           int64  `json:"status"`
		IsBanned         bool   `json:"is_banned"`
		BanReason        string `json:"ban_reason"`
		TimeoutUntilUnix int64  `json:"timeout_until_unix"`
		TimeoutReason    string `json:"timeout_reason"`
	}{
		Status: access.Status, IsBanned: access.IsBanned, BanReason: access.BanReason,
		TimeoutUntilUnix: access.TimeoutUntilUnix, TimeoutReason: access.TimeoutReason,
	}
	payload, err := json.Marshal(detail)
	if err != nil {
		return "", fmt.Errorf("detailMarshal: %w", err)
	}
	return string(payload), nil
}

func (e *Store) lockActorTarget(ctx context.Context, tx *sqlx.Tx, actor accountaccess.Actor, targetUserID int64) (accessRow, error) {
	// Lock both identities in stable order, including the profile rows whose
	// active/deleted state participates in authorization and target selection.
	query := `SELECT a.user_id, u.login_name, a.status, a.is_banned, a.ban_reason, a.timeout_until_unix, a.timeout_reason, a.ip_limit, a.revision
		FROM account_access a JOIN "user" u ON u.id = a.user_id
		WHERE u.id IN (?, ?) AND u.trmt_id = 0 ORDER BY u.id`
	if e.isPostgres {
		query += ` FOR UPDATE OF u, a`
	}
	rows := make([]accessRow, 0, 2)
	err := tx.SelectContext(ctx, &rows, tx.Rebind(query), actor.UserID, targetUserID)
	if err != nil {
		return accessRow{}, fmt.Errorf("targetRead: %w", err)
	}
	var actorRow, targetRow accessRow
	for _, row := range rows {
		if row.UserID == actor.UserID {
			actorRow = row
		}
		if row.UserID == targetUserID {
			targetRow = row
		}
	}
	err = checkActor(ctx, actorRow, actor)
	if err != nil {
		return accessRow{}, fmt.Errorf("targetActor: %w", err)
	}
	if targetRow.UserID == 0 {
		return accessRow{}, fmt.Errorf("targetMissing: %w", accountaccess.ErrNotFound)
	}
	return targetRow, nil
}
