package sqlstore

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/activity"
	activitysql "github.com/darkspinnet/darkspin/server/activity/sqlstore"
)

// ActivityStore shares the durable profile database without loading profiles.
func (e *Repository) ActivityStore() activity.Store {
	return activitysql.New(e.db, e.isPostgres)
}

func (e *Repository) ensureActivitySchema(ctx context.Context) error {
	err := activitysql.New(e.db, e.isPostgres).EnsureSchema(ctx)
	if err != nil {
		return fmt.Errorf("activityEnsure: %w", err)
	}
	return nil
}
