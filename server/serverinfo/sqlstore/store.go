// Package sqlstore persists the dedicated server identity in its profile database.
package sqlstore

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/darkspinnet/darkspin/server/serverinfo"
	"github.com/jmoiron/sqlx"
)

type Store struct {
	db *sqlx.DB
}

func New(db *sqlx.DB) *Store {
	return &Store{db: db}
}

// EnsureSchema is called only by dedicated server composition. A new server
// starts with empty identity fields and revision zero.
func (e *Store) EnsureSchema(ctx context.Context) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS server_identity (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			name TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			welcome TEXT NOT NULL DEFAULT '',
			rules TEXT NOT NULL DEFAULT '',
			contact_url TEXT NOT NULL DEFAULT '',
			revision BIGINT NOT NULL DEFAULT 0 CHECK (revision >= 0)
		)`,
		`INSERT INTO server_identity (id) VALUES (1) ON CONFLICT (id) DO NOTHING`,
	}
	for index, statement := range statements {
		result, err := e.db.ExecContext(ctx, statement)
		if err != nil {
			return fmt.Errorf("schemaExec[%d]: %w", index, err)
		}
		affectedCount, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("schemaAffected[%d]: %w", index, err)
		}
		if affectedCount < 0 {
			return fmt.Errorf("schemaCount[%d]: negative affected count", index)
		}
	}
	return nil
}

func (e *Store) Read(ctx context.Context) (serverinfo.Snapshot, error) {
	if ctx == nil {
		return serverinfo.Snapshot{}, errors.New("nil context")
	}
	var snapshot serverinfo.Snapshot
	var revision int64
	err := e.db.QueryRowxContext(ctx, `SELECT name, description, welcome, rules, contact_url, revision
		FROM server_identity WHERE id = 1`).Scan(
		&snapshot.Identity.Name, &snapshot.Identity.Description, &snapshot.Identity.Welcome,
		&snapshot.Identity.Rules, &snapshot.Identity.ContactURL, &revision,
	)
	if err != nil {
		return serverinfo.Snapshot{}, fmt.Errorf("identitySelect: %w", err)
	}
	snapshot.Revision = strconv.FormatInt(revision, 10)
	return snapshot, nil
}

func (e *Store) Save(ctx context.Context, expectedRevision string, identity serverinfo.Identity) (serverinfo.Snapshot, error) {
	if ctx == nil {
		return serverinfo.Snapshot{}, errors.New("nil context")
	}
	err := serverinfo.ValidateIdentity(identity)
	if err != nil {
		return serverinfo.Snapshot{}, fmt.Errorf("identityValidate: %w", err)
	}
	revision, err := strconv.ParseInt(expectedRevision, 10, 64)
	if err != nil {
		return serverinfo.Snapshot{}, fmt.Errorf("revisionParse: %w", serverinfo.ErrConflict)
	}
	if revision < 0 || revision == math.MaxInt64 || strconv.FormatInt(revision, 10) != expectedRevision {
		return serverinfo.Snapshot{}, fmt.Errorf("revisionRange: %w", serverinfo.ErrConflict)
	}
	result, err := e.db.ExecContext(ctx, e.db.Rebind(`UPDATE server_identity
		SET name = ?, description = ?, welcome = ?, rules = ?, contact_url = ?, revision = revision + 1
		WHERE id = 1 AND revision = ?`),
		identity.Name, identity.Description, identity.Welcome, identity.Rules, identity.ContactURL, revision,
	)
	if err != nil {
		return serverinfo.Snapshot{}, fmt.Errorf("identityUpdate: %w", err)
	}
	affectedCount, err := result.RowsAffected()
	if err != nil {
		return serverinfo.Snapshot{}, fmt.Errorf("identityAffected: %w", err)
	}
	if affectedCount == 0 {
		return serverinfo.Snapshot{}, fmt.Errorf("identityChanged: %w", serverinfo.ErrConflict)
	}
	if affectedCount != 1 {
		return serverinfo.Snapshot{}, errors.New("unexpected server identity affected count")
	}
	return serverinfo.Snapshot{Identity: identity, Revision: strconv.FormatInt(revision+1, 10)}, nil
}
