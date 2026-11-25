package sqlstore

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/accountaccess"
	"github.com/jmoiron/sqlx"
)

// InvalidateUser joins the owning account operation's transaction. Deleting an
// account must revoke its panel sessions even if an operator later restores it.
func (e *Store) InvalidateUser(ctx context.Context, tx *sqlx.Tx, userID int64) error {
	if ctx == nil || tx == nil || userID <= 0 {
		return errors.New("invalid credential invalidation")
	}
	result, err := tx.ExecContext(ctx, tx.Rebind(`UPDATE account_access
		SET revision = revision + 1 WHERE user_id = ? AND revision < ?`), userID, int64(math.MaxInt64))
	if err != nil {
		return fmt.Errorf("revisionUpdate: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("revisionAffected: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("revisionState: %w", accountaccess.ErrAccessDenied)
	}
	return nil
}
