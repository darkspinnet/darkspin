package companion

import "errors"

// SetBodyScale retains the committed additive body-scale contribution, rather
// than changing the noun's ordinary creation scale.
func (e *Session) SetBodyScale(objectID uint32, bodyScale float32) error {
	if e == nil {
		return errors.New("nil companion session")
	}
	if objectID == 0 || !isFiniteNonNegative(bodyScale) {
		return errors.New("companion body scale invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound || actor.HitPoint <= 0 {
		return errors.New("living companion unavailable")
	}
	actor.BodyScale = bodyScale
	e.actors[objectID] = actor
	return nil
}

func (e *Session) ClearBodyScale(objectID uint32) {
	if e == nil || objectID == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound {
		return
	}
	actor.BodyScale = 0
	e.actors[objectID] = actor
}
