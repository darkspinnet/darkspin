//go:build scenario

package interact

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ScenarioEquipmentCount observes available, unexpired equipment registrations
// without advancing drop flight or copying item payloads.
func (e *PickupRegistry) ScenarioEquipmentCount(ctx context.Context, elapsed time.Duration) (int, error) {
	if e == nil || ctx == nil {
		return 0, errors.New("equipment observation requires a registry and context")
	}
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for !e.mu.TryLock() {
		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("equipmentLock: %w", ctx.Err())
		case <-ticker.C:
		}
	}
	defer e.mu.Unlock()
	count := 0
	for objectID, pickup := range e.pickups {
		if pickup.Kind != PickupEquipment || (pickup.ExpiresAt > 0 && elapsed >= pickup.ExpiresAt) || !e.set.IsAvailable(objectID) {
			continue
		}
		count++
	}
	return count, nil
}
