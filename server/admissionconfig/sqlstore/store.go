package sqlstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/admissionconfig"
	"github.com/jmoiron/sqlx"
)

type Store struct{ db *sqlx.DB }

func New(db *sqlx.DB) *Store { return &Store{db: db} }

func (e *Store) EnsureSchema(ctx context.Context, isPostgres bool) error {
	result, err := e.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS admission_configuration (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		world_player_limit BIGINT NOT NULL CHECK (world_player_limit BETWEEN 1 AND 4294967295)
	)`)
	if err != nil {
		return fmt.Errorf("capacitySchema: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("schemaAffected: %w", err)
	}
	if count < 0 {
		return errors.New("negative schema affected count")
	}
	limit := uint32(25)
	if isPostgres {
		limit = 100
	}
	result, err = e.db.ExecContext(ctx, e.db.Rebind(`INSERT INTO admission_configuration (id, world_player_limit) VALUES (1, ?) ON CONFLICT (id) DO NOTHING`), limit)
	if err != nil {
		return fmt.Errorf("capacitySeed: %w", err)
	}
	count, err = result.RowsAffected()
	if err != nil {
		return fmt.Errorf("seedAffected: %w", err)
	}
	if count < 0 || count > 1 {
		return errors.New("unexpected capacity seed count")
	}
	return nil
}

func (e *Store) Read(ctx context.Context) (admissionconfig.Settings, error) {
	var settings admissionconfig.Settings
	err := e.db.QueryRowxContext(ctx, `SELECT world_player_limit FROM admission_configuration WHERE id = 1`).Scan(&settings.WorldPlayerLimit)
	if err != nil {
		return admissionconfig.Settings{}, fmt.Errorf("capacitySelect: %w", err)
	}
	return settings, nil
}

func (e *Store) Save(ctx context.Context, req admissionconfig.Settings) error {
	err := admissionconfig.Validate(req)
	if err != nil {
		return fmt.Errorf("capacityValidate: %w", err)
	}
	result, err := e.db.ExecContext(ctx, e.db.Rebind(`UPDATE admission_configuration SET world_player_limit = ? WHERE id = 1`), req.WorldPlayerLimit)
	if err != nil {
		return fmt.Errorf("capacityUpdate: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("capacityAffected: %w", err)
	}
	if count != 1 {
		return errors.New("capacity configuration singleton missing")
	}
	return nil
}
