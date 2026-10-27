// Package sqlite opens the embedded checkpoint database.
package sqlite

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/zone/checkpoint/sqlstore"
)

type Options = sqlstore.Options
type Store = sqlstore.Store

func New(ctx context.Context, path string, options Options) (*Store, error) {
	store, err := sqlstore.New(ctx, path, options)
	if err != nil {
		return nil, fmt.Errorf("sqliteOpen: %w", err)
	}
	return store, nil
}
