package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"

	"github.com/jmoiron/sqlx"
)

// NewPostgres uses the shared player pool; its owner closes it after checkpoint drain.
func NewPostgres(ctx context.Context, database *sqlx.DB) (*Store, error) {
	if ctx == nil || database == nil {
		return nil, errors.New("PostgreSQL checkpoint database unavailable")
	}
	tx, err := database.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("schemaBegin: %w", err)
	}
	defer rollback(tx)
	_, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(1936748398, 2)`)
	if err != nil {
		return nil, fmt.Errorf("schemaLock: %w", err)
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS zone_checkpoint (
			zone_id BIGINT PRIMARY KEY, revision BIGINT NOT NULL,
			level TEXT NOT NULL, difficulty BIGINT NOT NULL,
			payload BYTEA NOT NULL, updated_at BIGINT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS zone_checkpoint_member (
			user_id BIGINT PRIMARY KEY, zone_id BIGINT NOT NULL,
			updated_at BIGINT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS zone_checkpoint_member_zone ON zone_checkpoint_member(zone_id)`,
	}
	for index, statement := range statements {
		_, err = tx.ExecContext(ctx, statement)
		if err != nil {
			return nil, fmt.Errorf("schemaStep[%d]: %w", index, err)
		}
	}
	err = tx.Commit()
	if err != nil {
		return nil, fmt.Errorf("schemaCommit: %w", err)
	}
	return &Store{db: database, isShared: true}, nil
}

func rollback(tx *sqlx.Tx) {
	err := tx.Rollback()
	if err != nil && !errors.Is(err, sql.ErrTxDone) {
		log.Printf("Checkpoint transaction rollback failed: %v", err)
	}
}
