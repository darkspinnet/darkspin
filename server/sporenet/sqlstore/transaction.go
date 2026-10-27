package sqlstore

import (
	"database/sql"
	"errors"
	"log"

	"github.com/jmoiron/sqlx"
)

func rollback(tx *sqlx.Tx) {
	err := tx.Rollback()
	if err != nil && !errors.Is(err, sql.ErrTxDone) {
		log.Printf("Player transaction rollback failed: %v", err)
	}
}
