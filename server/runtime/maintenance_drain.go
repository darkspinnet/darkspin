package runtime

import (
	"context"
	"errors"
	"fmt"
)

// ForceDrain uses existing session owners when a maintenance hold reaches its
// deadline. It keeps the server and panel available for administration.
func (e *panelMaintenance) ForceDrain(ctx context.Context) error {
	if e.server == nil || e.connectionControl == nil {
		return errors.New("maintenance drain unavailable")
	}
	failures := make([]error, 0)
	users := e.server.userManager.Users()
	for _, user := range users {
		if user.CurrentGameID() == 0 {
			continue
		}
		userID := user.View().Account.ID
		err := e.connectionControl.DisconnectAccount(ctx, userID)
		if err != nil {
			failures = append(failures, fmt.Errorf("drainAccount[%d]: %w", userID, err))
		}
	}
	if len(failures) != 0 {
		return fmt.Errorf("maintenanceDrain: %w", errors.Join(failures...))
	}
	return nil
}
