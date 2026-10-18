package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/jmoiron/sqlx"
)

type partShopRow struct {
	UserID     int64  `db:"user_id"`
	ExpiresAt  int64  `db:"expires_at"`
	NextPartID uint64 `db:"next_part_id"`
	ItemLevel  uint16 `db:"item_level"`
}

func writePartShop(ctx context.Context, tx *sqlx.Tx, record sporenet.UserRecord) error {
	result, err := tx.NamedExecContext(ctx, `INSERT INTO part_shop
		(user_id, expires_at, next_part_id, item_level)
		VALUES (:user_id, :expires_at, :next_part_id, :item_level)`, partShopRow{
		UserID: record.Account.ID, ExpiresAt: record.PartShop.ExpiresAt,
		NextPartID: record.PartShop.NextPartID, ItemLevel: record.PartShop.ItemLevel,
	})
	if err != nil {
		return fmt.Errorf("shopInsert: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("shopCount: %w", err)
	}
	if count != 1 {
		return errors.New("part shop row was not saved")
	}
	return nil
}

func loadPartShop(ctx context.Context, tx *sqlx.Tx, userID int64) (sporenet.PartShopState, error) {
	var row partShopRow
	err := tx.GetContext(ctx, &row, `SELECT * FROM part_shop WHERE user_id = ?`, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return sporenet.PartShopState{}, nil
	}
	if err != nil {
		return sporenet.PartShopState{}, fmt.Errorf("shopSelect: %w", err)
	}
	return sporenet.PartShopState{ExpiresAt: row.ExpiresAt,
		NextPartID: row.NextPartID, ItemLevel: row.ItemLevel}, nil
}
