package sqlstore

import (
	"context"
	"fmt"
)

// CheckConnection observes database connectivity without writing player data.
func (e *Repository) CheckConnection(ctx context.Context) error {
	err := e.db.PingContext(ctx)
	if err != nil {
		return fmt.Errorf("profilePing: %w", err)
	}
	return nil
}
