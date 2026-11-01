package sqlstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

func (e *Repository) SwapProfileNames(ctx context.Context, req sporenet.ProfileNameSwapRequest) (sporenet.ProfileNameSwap, error) {
	if ctx == nil {
		return sporenet.ProfileNameSwap{}, errors.New("nil context")
	}
	if req.DeletedUserID <= 0 || req.ActiveUserID <= 0 || req.DeletedUserID == req.ActiveUserID {
		return sporenet.ProfileNameSwap{}, errors.New("swap requires two different positive account IDs")
	}
	tx, err := e.db.BeginTxx(ctx, nil)
	if err != nil {
		return sporenet.ProfileNameSwap{}, fmt.Errorf("swapBegin: %w", err)
	}
	defer rollback(tx)
	type profileNameRow struct {
		ID            int64  `db:"id"`
		TerminationID int64  `db:"trmt_id"`
		LoginName     string `db:"login_name"`
		DisplayName   string `db:"display_name"`
	}
	rows := make([]profileNameRow, 0, 2)
	query := `SELECT id, trmt_id, login_name, display_name FROM "user" WHERE id IN (?, ?) ORDER BY id`
	if e.isPostgres {
		query += ` FOR UPDATE`
	}
	err = tx.SelectContext(ctx, &rows, tx.Rebind(query), req.DeletedUserID, req.ActiveUserID)
	if err != nil {
		return sporenet.ProfileNameSwap{}, fmt.Errorf("swapSelect: %w", err)
	}
	if len(rows) != 2 {
		return sporenet.ProfileNameSwap{}, fmt.Errorf("swapMissing: %w", sporenet.ErrUserNotFound)
	}
	deleted, active := rows[0], rows[1]
	if deleted.ID != req.DeletedUserID {
		deleted, active = active, deleted
	}
	if deleted.TerminationID != 1 {
		return sporenet.ProfileNameSwap{}, errors.New("first account must be deleted")
	}
	if active.TerminationID != 0 {
		return sporenet.ProfileNameSwap{}, errors.New("second account must be active")
	}
	// A third, unused login lets immediate uniqueness constraints validate each
	// update. No intermediate identity is visible outside this transaction.
	temporaryLogin := deleted.LoginName + "_"
	for {
		var count int
		err = tx.GetContext(ctx, &count, tx.Rebind(`SELECT COUNT(*) FROM "user" WHERE LOWER(login_name) = LOWER(?)`), temporaryLogin)
		if err != nil {
			return sporenet.ProfileNameSwap{}, fmt.Errorf("swapTemporary: %w", err)
		}
		if count == 0 {
			break
		}
		temporaryLogin += "_"
	}
	result, err := tx.ExecContext(ctx, tx.Rebind(`UPDATE "user" SET login_name = ? WHERE id = ? AND trmt_id = 1`), temporaryLogin, deleted.ID)
	if err != nil {
		return sporenet.ProfileNameSwap{}, fmt.Errorf("swapStage: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return sporenet.ProfileNameSwap{}, fmt.Errorf("stageAffected: %w", err)
	}
	if affected != 1 {
		return sporenet.ProfileNameSwap{}, errors.New("deleted account changed during name swap")
	}
	result, err = tx.ExecContext(ctx, tx.Rebind(`UPDATE "user" SET login_name = ?, display_name = ? WHERE id = ? AND trmt_id = 0`), deleted.LoginName, deleted.DisplayName, active.ID)
	if err != nil {
		return sporenet.ProfileNameSwap{}, fmt.Errorf("swapActive: %w", err)
	}
	affected, err = result.RowsAffected()
	if err != nil {
		return sporenet.ProfileNameSwap{}, fmt.Errorf("activeAffected: %w", err)
	}
	if affected != 1 {
		return sporenet.ProfileNameSwap{}, errors.New("active account changed during name swap")
	}
	result, err = tx.ExecContext(ctx, tx.Rebind(`UPDATE "user"
		SET login_name = ?, display_name = ?
		WHERE id = ? AND trmt_id = 1`), active.LoginName, active.DisplayName, deleted.ID)
	if err != nil {
		return sporenet.ProfileNameSwap{}, fmt.Errorf("swapDeleted: %w", err)
	}
	affected, err = result.RowsAffected()
	if err != nil {
		return sporenet.ProfileNameSwap{}, fmt.Errorf("deletedAffected: %w", err)
	}
	if affected != 1 {
		return sporenet.ProfileNameSwap{}, errors.New("deleted account changed before name swap completed")
	}
	// Friend and ignore lists persist a display-name copy beside the stable ID.
	// Keep those labels attached to the account whose name just changed.
	result, err = tx.ExecContext(ctx, tx.Rebind(`UPDATE user_association
		SET name = CASE WHEN member_id = ? THEN ? ELSE ? END
		WHERE member_id IN (?, ?)`), deleted.ID, active.DisplayName, deleted.DisplayName, deleted.ID, active.ID)
	if err != nil {
		return sporenet.ProfileNameSwap{}, fmt.Errorf("swapAssociation: %w", err)
	}
	if result == nil {
		return sporenet.ProfileNameSwap{}, errors.New("association name swap returned no result")
	}
	err = tx.Commit()
	if err != nil {
		return sporenet.ProfileNameSwap{}, fmt.Errorf("swapCommit: %w", err)
	}
	return sporenet.ProfileNameSwap{
		DeletedUserID: deleted.ID, ActiveUserID: active.ID,
		DeletedLoginName: active.LoginName, ActiveLoginName: deleted.LoginName,
	}, nil
}
