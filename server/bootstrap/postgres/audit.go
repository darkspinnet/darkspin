package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/accountaccess/sqlstore"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
)

// CompleteServerOperation borrows the audit feature's transactional owner. A
// fresh short-lived pool survives stopped-state database replacement and never
// retains a connection that could obstruct PostgreSQL rollback.
func (e *Backup) CompleteServerOperation(ctx context.Context, operationID string, isSuccess bool, outcome string) error {
	if e.auditConfig == nil {
		return errors.New("bootstrap audit destination unavailable")
	}
	db := sqlx.NewDb(stdlib.OpenDB(*e.auditConfig.Copy()), "pgx")
	store := sqlstore.New(db, true)
	err := store.CompleteServerOperation(ctx, operationID, isSuccess, outcome)
	closeErr := db.Close()
	if err != nil || closeErr != nil {
		return fmt.Errorf("auditComplete: %w", errors.Join(err, closeErr))
	}
	return nil
}
