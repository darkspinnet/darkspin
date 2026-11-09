// Package sqlstore persists matchmaking preferences in the profile database.
package sqlstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/matchmaking"
	"github.com/jmoiron/sqlx"
)

type Store struct{ db *sqlx.DB }

func New(db *sqlx.DB) *Store { return &Store{db: db} }

// EnsureSchema initializes missing settings with defaults, preserving database edits.
func (e *Store) EnsureSchema(ctx context.Context) error {
	seed := matchmaking.Default()
	result, err := e.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS matchmaking_configuration (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		campaign_matchmaking_bot_size INTEGER NOT NULL CHECK (campaign_matchmaking_bot_size BETWEEN 1 AND 3),
		is_campaign_bot_squad_mirroring_enabled INTEGER NOT NULL CHECK (is_campaign_bot_squad_mirroring_enabled IN (0, 1))
	)`)
	if err != nil {
		return fmt.Errorf("schemaCreate: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("schemaAffected: %w", err)
	}
	if count < 0 {
		return errors.New("negative schema affected count")
	}
	result, err = e.db.ExecContext(ctx, e.db.Rebind(`INSERT INTO matchmaking_configuration
		(id, campaign_matchmaking_bot_size, is_campaign_bot_squad_mirroring_enabled)
		VALUES (1, ?, ?) ON CONFLICT (id) DO NOTHING`), seed.CampaignMatchmakingBotSize, booleanInteger(seed.IsCampaignBotSquadMirroringEnabled))
	if err != nil {
		return fmt.Errorf("seedInsert: %w", err)
	}
	count, err = result.RowsAffected()
	if err != nil {
		return fmt.Errorf("seedAffected: %w", err)
	}
	if count < 0 || count > 1 {
		return errors.New("unexpected matchmaking seed count")
	}
	return nil
}

func (e *Store) Read(ctx context.Context) (matchmaking.Settings, error) {
	var settings matchmaking.Settings
	var isMirroringEnabled int
	err := e.db.QueryRowxContext(ctx, `SELECT campaign_matchmaking_bot_size, is_campaign_bot_squad_mirroring_enabled FROM matchmaking_configuration WHERE id = 1`).Scan(&settings.CampaignMatchmakingBotSize, &isMirroringEnabled)
	if err != nil {
		return matchmaking.Settings{}, fmt.Errorf("settingsSelect: %w", err)
	}
	settings.IsCampaignBotSquadMirroringEnabled = isMirroringEnabled != 0
	return settings, nil
}

func (e *Store) Save(ctx context.Context, req matchmaking.Settings) error {
	err := matchmaking.Validate(req)
	if err != nil {
		return fmt.Errorf("settingsValidate: %w", err)
	}
	result, err := e.db.ExecContext(ctx, e.db.Rebind(`UPDATE matchmaking_configuration SET campaign_matchmaking_bot_size = ?, is_campaign_bot_squad_mirroring_enabled = ? WHERE id = 1`), req.CampaignMatchmakingBotSize, booleanInteger(req.IsCampaignBotSquadMirroringEnabled))
	if err != nil {
		return fmt.Errorf("settingsUpdate: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("settingsAffected: %w", err)
	}
	if count != 1 {
		return errors.New("matchmaking configuration singleton missing")
	}
	return nil
}

func booleanInteger(isEnabled bool) int {
	if isEnabled {
		return 1
	}
	return 0
}
