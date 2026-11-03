// Package sqlstore persists distinct login addresses in the profile database.
package sqlstore

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/loginhistory"
	"github.com/jmoiron/sqlx"
)

type Store struct {
	db *sqlx.DB
}

// New borrows the profile connection pool without creating any schema.
func New(db *sqlx.DB) *Store {
	return &Store{db: db}
}

// EnsureSchema is called only by dedicated server composition after profile setup.
// The schema is shared by SQLite and PostgreSQL.
func (e *Store) EnsureSchema(ctx context.Context) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if e == nil || e.db == nil {
		return errors.New("nil database")
	}
	result, err := e.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS user_login_ip (
		user_id BIGINT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
		ip_address TEXT NOT NULL,
		first_seen_at BIGINT NOT NULL,
		last_seen_at BIGINT NOT NULL,
		login_count BIGINT NOT NULL CHECK (login_count > 0),
		PRIMARY KEY (user_id, ip_address),
		CHECK (first_seen_at <= last_seen_at)
	)`)
	if err != nil {
		return fmt.Errorf("schemaCreate: %w", err)
	}
	// DDL has no meaningful affected-row count.
	if result == nil {
		return errors.New("schema creation returned no result")
	}
	return nil
}

func (e *Store) Record(ctx context.Context, req loginhistory.Record) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if e == nil || e.db == nil {
		return errors.New("nil database")
	}
	at := req.At.UTC().Unix()
	result, err := e.db.ExecContext(ctx, e.db.Rebind(`INSERT INTO user_login_ip
		(user_id, ip_address, first_seen_at, last_seen_at, login_count)
		SELECT id, ?, ?, ?, 1 FROM "user" WHERE id = ? AND trmt_id = 0
		ON CONFLICT (user_id, ip_address) DO UPDATE SET
			first_seen_at = CASE
				WHEN excluded.first_seen_at < user_login_ip.first_seen_at
				THEN excluded.first_seen_at ELSE user_login_ip.first_seen_at END,
			last_seen_at = CASE
				WHEN excluded.last_seen_at > user_login_ip.last_seen_at
				THEN excluded.last_seen_at ELSE user_login_ip.last_seen_at END,
			login_count = user_login_ip.login_count + 1
		WHERE user_login_ip.login_count < ?`),
		req.IPAddress, at, at, req.UserID, int64(math.MaxInt64))
	if err != nil {
		return fmt.Errorf("recordUpsert: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("recordAffected: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("recordActive: %w", loginhistory.ErrNotRecorded)
	}
	return nil
}
