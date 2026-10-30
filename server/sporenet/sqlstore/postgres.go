package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

// NewPostgres initializes the player schema and owns the pool on success.
func NewPostgres(ctx context.Context, database *sqlx.DB) (*Repository, error) {
	if ctx == nil || database == nil {
		return nil, errors.New("PostgreSQL player database unavailable")
	}
	tx, err := database.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("schemaBegin: %w", err)
	}
	defer rollback(tx)
	// Serialize startup DDL when administration and the server open together.
	_, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(1936748398, 1)`)
	if err != nil {
		return nil, fmt.Errorf("schemaLock: %w", err)
	}
	for index, statement := range postgresSchemaStatements {
		_, err = tx.ExecContext(ctx, statement)
		if err != nil {
			return nil, fmt.Errorf("schemaStep[%d]: %w", index, err)
		}
	}
	_, err = tx.ExecContext(ctx, `ALTER TABLE pvp_match_result ADD COLUMN IF NOT EXISTS dna_reward BIGINT NOT NULL DEFAULT 0`)
	if err != nil {
		return nil, fmt.Errorf("schemaPVPReward: %w", err)
	}
	err = tx.Commit()
	if err != nil {
		return nil, fmt.Errorf("schemaCommit: %w", err)
	}
	return &Repository{db: database, isPostgres: true}, nil
}

func (e *Repository) readOptions() *sql.TxOptions {
	options := &sql.TxOptions{ReadOnly: true}
	if e.isPostgres {
		options.Isolation = sql.LevelRepeatableRead
	}
	return options
}
