package npc

import "fmt"

// SetNavigationCollision mirrors build-103 SetNavCollision's avoidance state.
// The navigation agent remains attached while other actors ignore its repulsion.
func (e *Session) SetNavigationCollision(objectID uint32, isEnabled bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[objectID]
	if !isFound {
		return fmt.Errorf("navigationCollisionMissing: %d", objectID)
	}
	actor.IsNavigationCollisionEnabled = isEnabled
	e.npcs[objectID] = actor
	return nil
}
