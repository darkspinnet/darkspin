package sqlstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
)

func (e *Store) BeginTrustedServerOperation(ctx context.Context, actorType, action, reason, operationID string) (string, error) {
	if ctx == nil {
		return "", errors.New("nil context")
	}
	err := accountaccess.ValidateTrustedServerOperation(actorType, action, reason)
	if err != nil {
		return "", fmt.Errorf("trustedValidate: %w", err)
	}
	if operationID == "" {
		operationBytes := make([]byte, 16)
		count, randomErr := rand.Read(operationBytes)
		if randomErr != nil {
			return "", fmt.Errorf("trustedRandom: %w", randomErr)
		}
		if count != len(operationBytes) {
			return "", errors.New("trusted operation randomness incomplete")
		}
		operationID = hex.EncodeToString(operationBytes)
	}
	operationBytes, err := hex.DecodeString(operationID)
	if err != nil || len(operationBytes) != 16 && len(operationBytes) != 32 {
		return "", fmt.Errorf("trustedID: %w", accountaccess.ErrInvalidRequest)
	}
	operationID = hex.EncodeToString(operationBytes)
	tx, err := e.db.BeginTxx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("trustedBegin: %w", err)
	}
	defer rollback(tx)
	if e.isPostgres {
		// Serialize acceptance replays independently of the running child. The
		// lock lasts only for this transaction and requires no schema migration.
		checksum := sha256.Sum256([]byte(operationID))
		lockID := int64(binary.BigEndian.Uint64(checksum[:8]))
		lockResult, lockErr := tx.ExecContext(ctx, tx.Rebind(`SELECT pg_advisory_xact_lock(?)`), lockID)
		if lockErr != nil {
			return "", fmt.Errorf("trustedLock: %w", lockErr)
		}
		if lockResult == nil {
			return "", errors.New("trusted acceptance lock returned no result")
		}
	}
	var accepted struct {
		ActorType string `db:"actor_type"`
		Action    string `db:"action"`
		Reason    string `db:"reason"`
	}
	err = tx.GetContext(ctx, &accepted, tx.Rebind(`SELECT actor_type, action, reason FROM admin_event
		WHERE operation_id = ? AND outcome = 'accepted' ORDER BY id LIMIT 1`), operationID)
	if err == nil {
		if accepted.ActorType != actorType || accepted.Action != action || accepted.Reason != reason {
			return "", fmt.Errorf("trustedReplay: %w", accountaccess.ErrInvalidRequest)
		}
		return operationID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("trustedRead: %w", err)
	}
	result, err := tx.ExecContext(ctx, tx.Rebind(`INSERT INTO admin_event
		(actor_type, actor_user_id, target_user_id, action, occurred_at_unix,
		before_detail, after_detail, reason, operation_id, outcome)
		VALUES (?, NULL, NULL, ?, ?, '{}', '{}', ?, ?, 'accepted')`),
		actorType, action, time.Now().UTC().Unix(), reason, operationID)
	if err != nil {
		return "", fmt.Errorf("trustedInsert: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("trustedAffected: %w", err)
	}
	if affected != 1 {
		return "", errors.New("trusted operation acceptance did not create one event")
	}
	err = tx.Commit()
	if err != nil {
		return "", fmt.Errorf("trustedCommit: %w", err)
	}
	return operationID, nil
}
