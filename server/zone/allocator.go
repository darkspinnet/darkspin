package zone

import (
	"errors"
	"fmt"
)

func (e *Zone) ReserveObjectIDs(count uint32) (uint32, error) {
	if e == nil || count == 0 {
		return 0, errors.New("zone object id reservation invalid")
	}
	e.mu.RLock()
	objectID := e.info.ObjectID
	e.mu.RUnlock()
	if objectID == nil {
		return 0, errors.New("zone object id allocator unavailable")
	}
	firstObjectID, err := objectID.Reserve(count)
	if err != nil {
		return 0, fmt.Errorf("objectIDReserve: %w", err)
	}
	return firstObjectID, nil
}

func (e *Zone) ReserveProjectileIDs(
	count uint32,
) (uint32, uint32, error) {
	if e == nil || count == 0 {
		return 0, 0, errors.New("zone projectile id reservation invalid")
	}
	e.mu.RLock()
	projectileID := e.info.ProjectileID
	e.mu.RUnlock()
	if projectileID == nil {
		return 0, 0, errors.New("zone projectile id allocator unavailable")
	}
	firstObjectID, err := projectileID.Reserve(count)
	if err != nil {
		return 0, 0, fmt.Errorf("projectileIDReserve: %w", err)
	}
	return firstObjectID, firstObjectID + count, nil
}
