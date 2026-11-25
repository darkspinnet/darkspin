// Package sqlstore persists hourly activity and durable daily rollups.
package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/darkspinnet/darkspin/server/activity"
	"github.com/jmoiron/sqlx"
)

type Store struct {
	db         *sqlx.DB
	isPostgres bool
}

var _ activity.Store = (*Store)(nil)

func New(db *sqlx.DB, isPostgres bool) *Store {
	return &Store{db: db, isPostgres: isPostgres}
}

func (e *Store) Record(ctx context.Context, req activity.Observation) error {
	tx, err := e.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("recordBegin: %w", err)
	}
	defer rollback(tx)
	err = e.lock(ctx, tx)
	if err != nil {
		return fmt.Errorf("recordLock: %w", err)
	}
	hour := req.At.UTC().Truncate(time.Hour).Unix()
	err = recordQueues(ctx, tx, req)
	if err != nil {
		return fmt.Errorf("recordQueues: %w", err)
	}
	err = recordDistribution(ctx, tx, req)
	if err != nil {
		return fmt.Errorf("recordDistribution: %w", err)
	}
	err = execStatement(ctx, tx, `INSERT INTO activity_hour_dna (hour_at, earned) VALUES (?, 0) ON CONFLICT (hour_at) DO NOTHING`, hour)
	if err != nil {
		return fmt.Errorf("recordDNAHour: %w", err)
	}
	err = execStatement(ctx, tx, `INSERT INTO activity_collection (id, started_at) VALUES (1, ?) ON CONFLICT (id) DO NOTHING`, hour)
	if err != nil {
		return fmt.Errorf("recordCollection: %w", err)
	}
	// A startup retry in the same hour preserves its highest observed population.
	err = execStatement(ctx, tx, `INSERT INTO activity_hour_sample (hour_at, online_count, peak_count, source_count, coverage_count)
 VALUES (?, ?, ?, 1, 1) ON CONFLICT (hour_at) DO UPDATE SET
 online_count = CASE WHEN excluded.online_count > activity_hour_sample.online_count THEN excluded.online_count ELSE activity_hour_sample.online_count END,
 peak_count = CASE WHEN excluded.peak_count > activity_hour_sample.peak_count THEN excluded.peak_count ELSE activity_hour_sample.peak_count END,
 coverage_count = CASE WHEN excluded.coverage_count > activity_hour_sample.coverage_count THEN excluded.coverage_count ELSE activity_hour_sample.coverage_count END`, hour, len(req.Players), len(req.Players))
	if err != nil {
		return fmt.Errorf("recordSample: %w", err)
	}
	for _, player := range req.Players {
		err = recordPlayer(ctx, tx, hour, player.UserID)
		if err != nil {
			return fmt.Errorf("recordPresence: %w", err)
		}
	}
	err = execStatement(ctx, tx, `INSERT INTO activity_signup_sample (day_at, account_count) VALUES (?, 0) ON CONFLICT (day_at) DO NOTHING`, req.At.UTC().Truncate(24*time.Hour).Unix())
	if err != nil {
		return fmt.Errorf("recordSignupDay: %w", err)
	}
	err = recordPopulation(ctx, tx, req.At)
	if err != nil {
		return fmt.Errorf("recordPopulation: %w", err)
	}
	err = recordLevel(ctx, tx, req.At)
	if err != nil {
		return fmt.Errorf("recordLevel: %w", err)
	}
	err = recordProgression(ctx, tx, req.At)
	if err != nil {
		return fmt.Errorf("recordProgression: %w", err)
	}
	err = recordUserDays(ctx, tx, req.At, 0)
	if err != nil {
		return fmt.Errorf("recordUserDays: %w", err)
	}
	err = maintain(ctx, tx, req.At)
	if err != nil {
		return fmt.Errorf("recordMaintain: %w", err)
	}
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("recordCommit: %w", err)
	}
	return nil
}

func (e *Store) RecordPlayer(ctx context.Context, at time.Time, player activity.Player) error {
	tx, err := e.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("playerBegin: %w", err)
	}
	defer rollback(tx)
	err = e.lock(ctx, tx)
	if err != nil {
		return fmt.Errorf("playerLock: %w", err)
	}
	err = recordPlayer(ctx, tx, at.UTC().Truncate(time.Hour).Unix(), player.UserID)
	if err != nil {
		return fmt.Errorf("playerPresence: %w", err)
	}
	err = execStatement(ctx, tx, `INSERT INTO activity_first_admission (user_id, first_at) VALUES (?, ?) ON CONFLICT (user_id) DO NOTHING`, player.UserID, at.UTC().Unix())
	if err != nil {
		return fmt.Errorf("playerFirstAdmission: %w", err)
	}
	err = recordUserDays(ctx, tx, at, player.UserID)
	if err != nil {
		return fmt.Errorf("playerDay: %w", err)
	}
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("playerCommit: %w", err)
	}
	return nil
}

func recordPlayer(ctx context.Context, tx *sqlx.Tx, hour, userID int64) error {
	err := execStatement(ctx, tx, `INSERT INTO activity_hour_presence (hour_at, user_id) VALUES (?, ?) ON CONFLICT (hour_at, user_id) DO NOTHING`, hour, userID)
	if err != nil {
		return fmt.Errorf("presenceInsert: %w", err)
	}
	return nil
}

func (e *Store) lock(ctx context.Context, tx *sqlx.Tx) error {
	if !e.isPostgres {
		return nil
	}
	err := execStatement(ctx, tx, `SELECT pg_advisory_xact_lock(1936748398, 3)`)
	if err != nil {
		return fmt.Errorf("advisoryLock: %w", err)
	}
	return nil
}

func execStatement(ctx context.Context, tx *sqlx.Tx, statement string, arguments ...any) error {
	result, err := tx.ExecContext(ctx, tx.Rebind(statement), arguments...)
	if err != nil {
		return fmt.Errorf("statementExec: %w", err)
	}
	affectedCount, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("statementAffected: %w", err)
	}
	if affectedCount < 0 {
		return errors.New("negative activity statement affected count")
	}
	return nil
}

func rollback(tx *sqlx.Tx) {
	err := tx.Rollback()
	if err != nil && !errors.Is(err, sql.ErrTxDone) {
		log.Printf("Activity transaction rollback failed: %v", err)
	}
}
