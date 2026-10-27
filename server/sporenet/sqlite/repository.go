// Package sqlite opens the embedded player database.
package sqlite

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/sporenet/sqlstore"
)

type Options = sqlstore.Options
type Repository = sqlstore.Repository

func New(ctx context.Context, path string, options Options) (*Repository, error) {
	repository, err := sqlstore.New(ctx, path, options)
	if err != nil {
		return nil, fmt.Errorf("sqliteOpen: %w", err)
	}
	return repository, nil
}
