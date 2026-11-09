// Package sqlstore persists host configuration in the local profile database.
package sqlstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/hostconfig"
	"github.com/jmoiron/sqlx"
)

type Store struct{ db *sqlx.DB }

func New(db *sqlx.DB) *Store { return &Store{db: db} }

// document is private storage data; it is never a protocol response.
type document struct {
	Port                uint16 `json:"port"`
	BindHost            string `json:"bind_host"`
	AdvertisedHost      string `json:"advertised_host"`
	IsVersionLocked     bool   `json:"is_version_locked"`
	IsSingleplayerOnly  bool   `json:"is_singleplayer_only"`
	IsChatStdoutEnabled bool   `json:"is_chat_stdout_enabled"`
	JWTSecret           string `json:"jwt_secret"`
	JWTIssuer           string `json:"jwt_issuer"`
	JWTAudience         string `json:"jwt_audience"`
	ContentPath         string `json:"content_path"`
}

func encode(req hostconfig.Settings) ([]byte, error) {
	err := hostconfig.Validate(req)
	if err != nil {
		return nil, fmt.Errorf("settingsValidate: %w", err)
	}
	payload, err := json.Marshal(document(req))
	if err != nil {
		return nil, fmt.Errorf("settingsEncode: %w", err)
	}
	return payload, nil
}

func (e *Store) EnsureSchema(ctx context.Context, defaults hostconfig.Settings) error {
	payload, err := encode(defaults)
	if err != nil {
		return fmt.Errorf("defaultsEncode: %w", err)
	}
	result, err := e.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS host_configuration (id INTEGER PRIMARY KEY CHECK (id = 1), configuration TEXT NOT NULL)`)
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
	result, err = e.db.ExecContext(ctx, e.db.Rebind(`INSERT INTO host_configuration (id, configuration) VALUES (1, ?) ON CONFLICT (id) DO NOTHING`), string(payload))
	if err != nil {
		return fmt.Errorf("defaultsInsert: %w", err)
	}
	count, err = result.RowsAffected()
	if err != nil {
		return fmt.Errorf("defaultsAffected: %w", err)
	}
	if count < 0 || count > 1 {
		return errors.New("unexpected host configuration initialization count")
	}
	return nil
}

func (e *Store) Read(ctx context.Context) (hostconfig.Settings, error) {
	var payload string
	err := e.db.QueryRowxContext(ctx, `SELECT configuration FROM host_configuration WHERE id = 1`).Scan(&payload)
	if err != nil {
		return hostconfig.Settings{}, fmt.Errorf("settingsSelect: %w", err)
	}
	var stored document
	err = json.Unmarshal([]byte(payload), &stored)
	if err != nil {
		return hostconfig.Settings{}, fmt.Errorf("settingsDecode: %w", err)
	}
	settings := hostconfig.Settings(stored)
	err = hostconfig.Validate(settings)
	if err != nil {
		return hostconfig.Settings{}, fmt.Errorf("settingsValidate: %w", err)
	}
	return settings, nil
}

func (e *Store) Save(ctx context.Context, req hostconfig.Settings) error {
	payload, err := encode(req)
	if err != nil {
		return fmt.Errorf("settingsEncode: %w", err)
	}
	result, err := e.db.ExecContext(ctx, e.db.Rebind(`UPDATE host_configuration SET configuration = ? WHERE id = 1`), string(payload))
	if err != nil {
		return fmt.Errorf("settingsUpdate: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("settingsAffected: %w", err)
	}
	if count != 1 {
		return errors.New("host configuration singleton missing")
	}
	return nil
}
