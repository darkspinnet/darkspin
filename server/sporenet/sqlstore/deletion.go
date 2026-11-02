package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/jmoiron/sqlx"
)

// DeleteByLoginName hides a profile and releases its login without deleting children.
func (e *Repository) DeleteByLoginName(ctx context.Context, loginName string) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if loginName == "" {
		return fmt.Errorf("deleteIdentity: %w", sporenet.ErrUserNotFound)
	}
	tx, err := e.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("deleteBegin: %w", err)
	}
	defer rollback(tx)
	var row struct {
		ID          int64  `db:"id"`
		LoginName   string `db:"login_name"`
		DisplayName string `db:"display_name"`
	}
	query := `SELECT id, login_name, display_name FROM "user" WHERE LOWER(login_name) = LOWER(?) AND trmt_id = 0`
	if e.isPostgres {
		query += ` FOR UPDATE`
	}
	err = tx.GetContext(ctx, &row, tx.Rebind(query), loginName)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("deleteMissing: %w", sporenet.ErrUserNotFound)
	}
	if err != nil {
		return fmt.Errorf("deleteSelect: %w", err)
	}
	suffix := "_"
	for {
		var count int
		err = tx.GetContext(ctx, &count, tx.Rebind(`SELECT COUNT(*) FROM "user"
			WHERE LOWER(login_name) = LOWER(?) OR LOWER(display_name) = LOWER(?)`), row.LoginName+suffix, row.DisplayName+suffix)
		if err != nil {
			return fmt.Errorf("deleteCollision: %w", err)
		}
		if count == 0 {
			break
		}
		suffix += "_"
	}
	result, err := tx.ExecContext(ctx, tx.Rebind(`UPDATE "user"
		SET trmt_id = 1, trmt_dt = ?, login_name = ?, display_name = ?
		WHERE id = ? AND trmt_id = 0`), time.Now().UTC().Unix(), row.LoginName+suffix, row.DisplayName+suffix, row.ID)
	if err != nil {
		return fmt.Errorf("deleteUpdate: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("deleteAffected: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("deleteChanged: %w", sporenet.ErrUserNotFound)
	}
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("deleteCommit: %w", err)
	}
	return nil
}

// RestoreDeletedProfile restores exactly one archived ID, rejecting occupied logins.
// An empty login restores the name preceding the deletion's underscore suffix.
func (e *Repository) RestoreDeletedProfile(ctx context.Context, id int64, loginName string) (string, error) {
	if ctx == nil {
		return "", errors.New("nil context")
	}
	if id <= 0 {
		return "", fmt.Errorf("restoreID: %w", sporenet.ErrUserNotFound)
	}
	tx, err := e.db.BeginTxx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("restoreBegin: %w", err)
	}
	defer rollback(tx)
	var row struct {
		LoginName   string `db:"login_name"`
		DisplayName string `db:"display_name"`
	}
	query := `SELECT login_name, display_name FROM "user" WHERE id = ? AND trmt_id = 1`
	if e.isPostgres {
		query += ` FOR UPDATE`
	}
	err = tx.GetContext(ctx, &row, tx.Rebind(query), id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("restoreMissing: %w", sporenet.ErrUserNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("restoreSelect: %w", err)
	}
	row.LoginName = strings.TrimRight(row.LoginName, "_")
	row.DisplayName = strings.TrimRight(row.DisplayName, "_")
	if loginName == "" {
		loginName = row.LoginName
	}
	if strings.TrimSpace(loginName) == "" {
		return "", fmt.Errorf("restoreLogin: %w", sporenet.ErrInvalidUser)
	}
	var count int
	err = tx.GetContext(ctx, &count, tx.Rebind(`SELECT COUNT(*) FROM "user" WHERE LOWER(login_name) = LOWER(?) AND id <> ?`), loginName, id)
	if err != nil {
		return "", fmt.Errorf("restoreCollision: %w", err)
	}
	if count != 0 {
		return "", fmt.Errorf("restoreOccupied: %w", sporenet.ErrUserExists)
	}
	displayName := row.DisplayName
	if strings.EqualFold(displayName, row.LoginName) && !strings.EqualFold(loginName, row.LoginName) {
		displayName = loginName
	}
	result, err := tx.ExecContext(ctx, tx.Rebind(`UPDATE "user"
		SET login_name = ?, display_name = ?, trmt_id = 0
		WHERE id = ? AND trmt_id = 1`), loginName, displayName, id)
	if err != nil {
		return "", fmt.Errorf("restoreUpdate: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("restoreAffected: %w", err)
	}
	if affected != 1 {
		return "", fmt.Errorf("restoreChanged: %w", sporenet.ErrUserNotFound)
	}
	err = tx.Commit()
	if err != nil {
		return "", fmt.Errorf("restoreCommit: %w", err)
	}
	return loginName, nil
}

// PurgeDeletedProfiles permanently removes archived profiles at or before cutoff.
// Child rows are removed by the schema's foreign-key cascades.
func (e *Repository) PurgeDeletedProfiles(ctx context.Context, cutoff time.Time) (int64, error) {
	if ctx == nil {
		return 0, errors.New("nil context")
	}
	if cutoff.IsZero() {
		return 0, errors.New("empty deletion cutoff")
	}
	result, err := e.db.ExecContext(ctx, e.db.Rebind(`DELETE FROM "user"
		WHERE trmt_id = 1 AND trmt_dt > 0 AND trmt_dt <= ?`), cutoff.UTC().Unix())
	if err != nil {
		return 0, fmt.Errorf("purgeDelete: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("purgeAffected: %w", err)
	}
	return affected, nil
}

// LatestProfileDeletion derives the released login from its underscore suffix.
func (e *Repository) LatestProfileDeletion(ctx context.Context, loginName string) (time.Time, error) {
	if ctx == nil {
		return time.Time{}, errors.New("nil context")
	}
	var terminationDT int64
	err := e.db.GetContext(ctx, &terminationDT, e.db.Rebind(`
		SELECT COALESCE(MAX(trmt_dt), 0) FROM "user"
		WHERE trmt_dt > 0 AND LOWER(RTRIM(login_name, '_')) = LOWER(?)`), loginName)
	if err != nil {
		return time.Time{}, fmt.Errorf("deletionLatest: %w", err)
	}
	if terminationDT <= 0 {
		return time.Time{}, nil
	}
	return time.Unix(terminationDT, 0).UTC(), nil
}

func ensureProfileDeletionColumns(ctx context.Context, tx *sqlx.Tx, isPostgres bool) error {
	columns := []struct {
		name       string
		definition string
	}{
		{name: "trmt_id", definition: "INTEGER NOT NULL DEFAULT 0"},
		{name: "trmt_dt", definition: "BIGINT NOT NULL DEFAULT 0"},
	}
	for _, column := range columns {
		if !isPostgres {
			var count int
			err := tx.GetContext(ctx, &count, tx.Rebind(`SELECT COUNT(*) FROM pragma_table_info('user') WHERE name = ?`), column.name)
			if err != nil {
				return fmt.Errorf("columnCheck[%s]: %w", column.name, err)
			}
			if count != 0 {
				continue
			}
		}
		query := `ALTER TABLE "user" ADD COLUMN `
		if isPostgres {
			query += `IF NOT EXISTS `
		}
		result, err := tx.ExecContext(ctx, query+column.name+" "+column.definition)
		if err != nil {
			return fmt.Errorf("columnAdd[%s]: %w", column.name, err)
		}
		if result == nil {
			return fmt.Errorf("columnResult[%s]: missing result", column.name)
		}
	}
	err := migrateProfileTerminationDate(ctx, tx, isPostgres)
	if err != nil {
		return fmt.Errorf("terminationMigrate: %w", err)
	}
	// Drop redundant original-name columns before SELECT * rows are decoded.
	for _, column := range []string{"deleted_login_name", "deleted_display_name"} {
		if !isPostgres {
			var count int
			err := tx.GetContext(ctx, &count, tx.Rebind(`SELECT COUNT(*) FROM PRAGMA_TABLE_INFO('user') WHERE name = ?`), column)
			if err != nil {
				return fmt.Errorf("obsoleteCheck[%s]: %w", column, err)
			}
			if count == 0 {
				continue
			}
		}
		query := `ALTER TABLE "user" DROP COLUMN `
		if isPostgres {
			query += `IF EXISTS `
		}
		result, err := tx.ExecContext(ctx, query+column)
		if err != nil {
			return fmt.Errorf("obsoleteDrop[%s]: %w", column, err)
		}
		if result == nil {
			return fmt.Errorf("obsoleteResult[%s]: missing result", column)
		}
	}
	return nil
}

func migrateProfileTerminationDate(ctx context.Context, tx *sqlx.Tx, isPostgres bool) error {
	query := `SELECT COUNT(*) FROM PRAGMA_TABLE_INFO('user') WHERE name = 'terminated_at_unix'`
	if isPostgres {
		query = `SELECT COUNT(*) FROM information_schema.columns
			WHERE table_schema = CURRENT_SCHEMA() AND table_name = 'user' AND column_name = 'terminated_at_unix'`
	}
	var count int
	err := tx.GetContext(ctx, &count, query)
	if err != nil {
		return fmt.Errorf("dateColumn: %w", err)
	}
	if count == 0 {
		return nil
	}
	result, err := tx.ExecContext(ctx, `UPDATE "user" SET trmt_dt = terminated_at_unix
		WHERE trmt_dt = 0 AND terminated_at_unix <> 0`)
	if err != nil {
		return fmt.Errorf("dateCopy: %w", err)
	}
	if result == nil {
		return errors.New("termination date migration returned no result")
	}
	result, err = tx.ExecContext(ctx, `ALTER TABLE "user" DROP COLUMN terminated_at_unix`)
	if err != nil {
		return fmt.Errorf("dateDrop: %w", err)
	}
	if result == nil {
		return errors.New("termination date column removal returned no result")
	}
	return nil
}
