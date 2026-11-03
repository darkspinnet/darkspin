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
	err = ensureProfileDeletionColumns(ctx, tx, true)
	if err != nil {
		return nil, fmt.Errorf("schemaDeletion: %w", err)
	}
	dnaResult, err := tx.ExecContext(ctx, `ALTER TABLE user_stat ADD COLUMN IF NOT EXISTS dna_earned BIGINT NOT NULL DEFAULT 0`)
	if err != nil {
		return nil, fmt.Errorf("schemaDNAEarned: %w", err)
	}
	dnaAffectedCount, err := dnaResult.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("schemaDNAAffected: %w", err)
	}
	if dnaAffectedCount < 0 {
		return nil, errors.New("negative DNA column affected count")
	}
	_, err = tx.ExecContext(ctx, `ALTER TABLE pvp_match_result ADD COLUMN IF NOT EXISTS dna_reward BIGINT NOT NULL DEFAULT 0`)
	if err != nil {
		return nil, fmt.Errorf("schemaPVPReward: %w", err)
	}
	err = tx.Commit()
	if err != nil {
		return nil, fmt.Errorf("schemaCommit: %w", err)
	}
	repository := &Repository{db: database, isPostgres: true}
	err = repository.AccountAccess().EnsureSchema(ctx)
	if err != nil {
		return nil, fmt.Errorf("accessSchema: %w", err)
	}
	err = repository.ensureActivitySchema(ctx)
	if err != nil {
		return nil, fmt.Errorf("activitySchema: %w", err)
	}
	return repository, nil
}

func (e *Repository) readOptions() *sql.TxOptions {
	options := &sql.TxOptions{ReadOnly: true}
	if e.isPostgres {
		options.Isolation = sql.LevelRepeatableRead
	}
	return options
}
