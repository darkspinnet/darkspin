// Package sqlite persists zone checkpoints in the runtime save database.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"time"

	"github.com/darkspinnet/darkspin/server/zone/checkpoint"
	_ "modernc.org/sqlite"
)

type Options struct {
	BusyTimeout time.Duration
}

type Store struct {
	db *sql.DB
}

func New(ctx context.Context, path string, options Options) (*Store, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if path == "" {
		return nil, errors.New("empty path")
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("pathResolve: %w", err)
	}
	query := url.Values{}
	query.Set("_pragma", "busy_timeout("+strconv.FormatInt(options.BusyTimeout.Milliseconds(), 10)+")")
	query.Add("_pragma", "journal_mode(WAL)")
	dsn := "file:" + filepath.ToSlash(absolutePath) + "?" + query.Encode()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("databaseOpen: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	err = db.PingContext(ctx)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("databasePing: %w", err)
	}
	_, err = db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS zone_checkpoint (
    zone_id INTEGER PRIMARY KEY,
    revision INTEGER NOT NULL,
    level TEXT NOT NULL,
    difficulty INTEGER NOT NULL,
    payload BLOB NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS zone_checkpoint_member (
    user_id INTEGER PRIMARY KEY,
    zone_id INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
)`)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("schemaCreate: %w", err)
	}
	return &Store{db: db}, nil
}

func (e *Store) Save(ctx context.Context, snapshot checkpoint.Snapshot) error {
	if e == nil || e.db == nil {
		return errors.New("checkpoint store unavailable")
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("snapshotMarshal: %w", err)
	}
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("snapshotBegin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
INSERT INTO zone_checkpoint (zone_id, revision, level, difficulty, payload, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(zone_id) DO UPDATE SET
    revision = excluded.revision,
    level = excluded.level,
    difficulty = excluded.difficulty,
    payload = excluded.payload,
    updated_at = excluded.updated_at
WHERE excluded.revision >= zone_checkpoint.revision`,
		snapshot.ZoneID, snapshot.Revision, snapshot.Level, snapshot.Difficulty,
		payload, snapshot.SavedAt.UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("snapshotUpsert: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("snapshotRows: %w", err)
	}
	if rowsAffected == 0 {
		return nil
	}
	_, err = tx.ExecContext(
		ctx, "DELETE FROM zone_checkpoint_member WHERE zone_id = ?", snapshot.ZoneID,
	)
	if err != nil {
		return fmt.Errorf("memberDelete: %w", err)
	}
	for index, member := range snapshot.Members {
		_, err = tx.ExecContext(ctx, `
INSERT INTO zone_checkpoint_member (user_id, zone_id, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(user_id) DO UPDATE SET
    zone_id = excluded.zone_id,
    updated_at = excluded.updated_at
WHERE excluded.updated_at >= zone_checkpoint_member.updated_at`,
			member.UserID, snapshot.ZoneID, snapshot.SavedAt.UnixMilli(),
		)
		if err != nil {
			return fmt.Errorf("memberUpsert[%d]: %w", index, err)
		}
	}
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("snapshotCommit: %w", err)
	}
	return nil
}

func (e *Store) Load(
	ctx context.Context, zoneID uint64,
) (checkpoint.Snapshot, bool, error) {
	if e == nil || e.db == nil {
		return checkpoint.Snapshot{}, false, errors.New("checkpoint store unavailable")
	}
	var payload []byte
	err := e.db.QueryRowContext(
		ctx, "SELECT payload FROM zone_checkpoint WHERE zone_id = ?", zoneID,
	).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return checkpoint.Snapshot{}, false, nil
	}
	if err != nil {
		return checkpoint.Snapshot{}, false, fmt.Errorf("snapshotSelect: %w", err)
	}
	var snapshot checkpoint.Snapshot
	err = json.Unmarshal(payload, &snapshot)
	if err != nil {
		return checkpoint.Snapshot{}, false, fmt.Errorf("snapshotDecode: %w", err)
	}
	return snapshot, true, nil
}

func (e *Store) LoadMember(
	ctx context.Context, userID uint64,
) (checkpoint.Snapshot, bool, error) {
	if e == nil || e.db == nil {
		return checkpoint.Snapshot{}, false, errors.New("checkpoint store unavailable")
	}
	var zoneID uint64
	err := e.db.QueryRowContext(
		ctx, "SELECT zone_id FROM zone_checkpoint_member WHERE user_id = ?", userID,
	).Scan(&zoneID)
	if errors.Is(err, sql.ErrNoRows) {
		return checkpoint.Snapshot{}, false, nil
	}
	if err != nil {
		return checkpoint.Snapshot{}, false, fmt.Errorf("memberSelect: %w", err)
	}
	snapshot, isFound, err := e.Load(ctx, zoneID)
	if err != nil {
		return checkpoint.Snapshot{}, false, fmt.Errorf("memberSnapshot: %w", err)
	}
	return snapshot, isFound, nil
}

func (e *Store) Delete(ctx context.Context, zoneID uint64) error {
	if e == nil || e.db == nil {
		return errors.New("checkpoint store unavailable")
	}
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("snapshotDeleteBegin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, "DELETE FROM zone_checkpoint_member WHERE zone_id = ?", zoneID)
	if err != nil {
		return fmt.Errorf("memberDelete: %w", err)
	}
	_, err = tx.ExecContext(ctx, "DELETE FROM zone_checkpoint WHERE zone_id = ?", zoneID)
	if err != nil {
		return fmt.Errorf("snapshotDelete: %w", err)
	}
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("snapshotDeleteCommit: %w", err)
	}
	return nil
}

func (e *Store) Close() error {
	if e == nil || e.db == nil {
		return nil
	}
	err := e.db.Close()
	if err != nil {
		return fmt.Errorf("databaseClose: %w", err)
	}
	return nil
}
