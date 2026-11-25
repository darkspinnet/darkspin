package sqlstore

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
)

var _ accountaccess.ServerOperationStore = (*Store)(nil)

func (e *Store) BeginServerOperation(ctx context.Context, actor accountaccess.Actor, action, reason string) (string, error) {
	if ctx == nil {
		return "", errors.New("nil context")
	}
	err := accountaccess.ValidateServerOperation(actor, action, reason)
	if err != nil {
		return "", fmt.Errorf("operationValidate: %w", err)
	}
	operationBytes := make([]byte, 16)
	count, err := rand.Read(operationBytes)
	if err != nil {
		return "", fmt.Errorf("operationRandom: %w", err)
	}
	if count != len(operationBytes) {
		return "", errors.New("server operation ID generation incomplete")
	}
	operationID := hex.EncodeToString(operationBytes)
	tx, err := e.db.BeginTxx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("operationBegin: %w", err)
	}
	defer rollback(tx)
	err = e.authorizeActor(ctx, tx, actor)
	if err != nil {
		return "", fmt.Errorf("operationAuthorize: %w", err)
	}
	result, err := tx.ExecContext(ctx, tx.Rebind(`INSERT INTO admin_event
		(actor_type, actor_user_id, target_user_id, action, occurred_at_unix,
		before_detail, after_detail, reason, operation_id, outcome)
		VALUES ('account', ?, NULL, ?, ?, '{}', '{}', ?, ?, 'accepted')`),
		actor.UserID, action, time.Now().UTC().Unix(), reason, operationID)
	if err != nil {
		return "", fmt.Errorf("operationInsert: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("operationAffected: %w", err)
	}
	if affected != 1 {
		return "", errors.New("server operation acceptance did not create one event")
	}
	err = tx.Commit()
	if err != nil {
		return "", fmt.Errorf("operationCommit: %w", err)
	}
	return operationID, nil
}

func (e *Store) CompleteServerOperation(ctx context.Context, operationID string, isSuccess bool, safeOutcome string) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	err := accountaccess.ValidateServerOutcome(safeOutcome)
	if err != nil {
		return fmt.Errorf("outcomeValidate: %w", err)
	}
	operationBytes, err := hex.DecodeString(operationID)
	if err != nil {
		return fmt.Errorf("outcomeOperation: %w", accountaccess.ErrInvalidRequest)
	}
	if len(operationBytes) != 16 && len(operationBytes) != 32 {
		return fmt.Errorf("outcomeLength: %w", accountaccess.ErrInvalidRequest)
	}
	detail, err := json.Marshal(struct {
		Result string `json:"result"`
	}{Result: safeOutcome})
	if err != nil {
		return fmt.Errorf("outcomeDetail: %w", err)
	}
	tx, err := e.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("outcomeBegin: %w", err)
	}
	defer rollback(tx)
	query := `SELECT action FROM admin_event WHERE operation_id = ? AND outcome = 'accepted'
		AND action IN ('server.identity', 'server.announcement', 'server.matchmaking', 'server.capacity', 'maintenance.restart',
			'maintenance.update', 'maintenance.hold', 'maintenance.cancel',
			'diagnostic.download', 'diagnostic.delete') ORDER BY id LIMIT 1`
	if e.isPostgres {
		query += ` FOR UPDATE`
	}
	var action string
	err = tx.GetContext(ctx, &action, tx.Rebind(query), operationID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("outcomeMissing: %w", accountaccess.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("outcomeRead: %w", err)
	}
	var count int
	err = tx.GetContext(ctx, &count, tx.Rebind(`SELECT COUNT(*) FROM admin_event
		WHERE operation_id = ? AND action = ? AND outcome IN ('completed', 'failed')`), operationID, action)
	if err != nil {
		return fmt.Errorf("outcomeCount: %w", err)
	}
	if count > 0 {
		return nil
	}
	outcome := "failed"
	if isSuccess {
		outcome = "completed"
	}
	result, err := tx.ExecContext(ctx, tx.Rebind(`INSERT INTO admin_event
		(actor_type, actor_user_id, target_user_id, action, occurred_at_unix,
		before_detail, after_detail, reason, operation_id, outcome)
		VALUES ('system', NULL, NULL, ?, ?, '{}', ?, '', ?, ?)`),
		action, time.Now().UTC().Unix(), string(detail), operationID, outcome)
	if err != nil {
		return fmt.Errorf("outcomeInsert: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("outcomeAffected: %w", err)
	}
	if affected != 1 {
		return errors.New("server operation outcome did not create one event")
	}
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("outcomeCommit: %w", err)
	}
	return nil
}
