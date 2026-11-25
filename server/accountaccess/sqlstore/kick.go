package sqlstore

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
)

var _ accountaccess.KickStore = (*Store)(nil)

func (e *Store) BeginKick(ctx context.Context, req accountaccess.KickRequest) (accountaccess.KickOperation, error) {
	if ctx == nil {
		return accountaccess.KickOperation{}, errors.New("nil context")
	}
	req, err := accountaccess.NormalizeKickRequest(req)
	if err != nil {
		return accountaccess.KickOperation{}, fmt.Errorf("kickValidate: %w", err)
	}
	operationBytes := make([]byte, 16)
	count, err := rand.Read(operationBytes)
	if err != nil {
		return accountaccess.KickOperation{}, fmt.Errorf("kickRandom: %w", err)
	}
	if count != len(operationBytes) {
		return accountaccess.KickOperation{}, errors.New("kick operation ID generation incomplete")
	}
	operationID := hex.EncodeToString(operationBytes)
	tx, err := e.db.BeginTxx(ctx, nil)
	if err != nil {
		return accountaccess.KickOperation{}, fmt.Errorf("kickBegin: %w", err)
	}
	defer rollback(tx)
	targetRow, err := e.lockActorTarget(ctx, tx, req.Actor, req.TargetUserID)
	if err != nil {
		return accountaccess.KickOperation{}, fmt.Errorf("kickLock: %w", err)
	}
	if targetRow.Status < 0 || targetRow.Revision <= 0 {
		return accountaccess.KickOperation{}, fmt.Errorf("kickTarget: %w", accountaccess.ErrAccessDenied)
	}
	result, err := tx.ExecContext(ctx, tx.Rebind(`INSERT INTO admin_event
		(actor_type, actor_user_id, target_user_id, action, occurred_at_unix,
		before_detail, after_detail, reason, operation_id, outcome)
		VALUES ('account', ?, ?, 'account.kick', ?, '{}', '{}', ?, ?, 'accepted')`),
		req.Actor.UserID, req.TargetUserID, time.Now().UTC().Unix(), req.Reason, operationID)
	if err != nil {
		return accountaccess.KickOperation{}, fmt.Errorf("kickAccept: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return accountaccess.KickOperation{}, fmt.Errorf("kickAffected: %w", err)
	}
	if affected != 1 {
		return accountaccess.KickOperation{}, errors.New("kick acceptance did not create one event")
	}
	err = tx.Commit()
	if err != nil {
		return accountaccess.KickOperation{}, fmt.Errorf("kickCommit: %w", err)
	}
	return accountaccess.KickOperation{OperationID: operationID, TargetUserID: req.TargetUserID}, nil
}

// CompleteKick trusts only the local runtime callback. The accepted event is
// locked to serialize completion and make duplicate callbacks idempotent.
func (e *Store) CompleteKick(ctx context.Context, req accountaccess.KickOutcomeRequest) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	operationBytes, err := hex.DecodeString(req.OperationID)
	if err != nil {
		return fmt.Errorf("kickOperation: %w", accountaccess.ErrInvalidRequest)
	}
	if len(operationBytes) != 16 {
		return fmt.Errorf("kickOperationLength: %w", accountaccess.ErrInvalidRequest)
	}
	tx, err := e.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("kickOutcomeBegin: %w", err)
	}
	defer rollback(tx)
	query := `SELECT target_user_id FROM admin_event
		WHERE operation_id = ? AND action = 'account.kick' AND outcome = 'accepted' ORDER BY id LIMIT 1`
	if e.isPostgres {
		query += ` FOR UPDATE`
	}
	var targetUserID int64
	err = tx.GetContext(ctx, &targetUserID, tx.Rebind(query), req.OperationID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("kickAcceptance: %w", accountaccess.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("kickOutcomeRead: %w", err)
	}
	var count int
	err = tx.GetContext(ctx, &count, tx.Rebind(`SELECT COUNT(*) FROM admin_event
		WHERE operation_id = ? AND action = 'account.kick' AND outcome IN ('completed', 'failed')`), req.OperationID)
	if err != nil {
		return fmt.Errorf("kickOutcomeCount: %w", err)
	}
	if count > 0 {
		return nil
	}
	outcome := "failed"
	if req.IsCompleted {
		outcome = "completed"
	}
	result, err := tx.ExecContext(ctx, tx.Rebind(`INSERT INTO admin_event
		(actor_type, actor_user_id, target_user_id, action, occurred_at_unix,
		before_detail, after_detail, reason, operation_id, outcome)
		VALUES ('system', NULL, ?, 'account.kick', ?, '{}', '{}', '', ?, ?)`),
		targetUserID, time.Now().UTC().Unix(), req.OperationID, outcome)
	if err != nil {
		return fmt.Errorf("kickOutcomeInsert: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("kickOutcomeAffected: %w", err)
	}
	if affected != 1 {
		return errors.New("kick outcome did not create one event")
	}
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("kickOutcomeCommit: %w", err)
	}
	return nil
}
