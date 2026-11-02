package npc

import (
	"errors"
	"math"
)

// SetSupportIdleMovementBuff owns the temporary additive movement attribute
// installed by the source's active Flee/FleeFacing behavior instance.
func (e *Session) SetSupportIdleMovementBuff(objectID uint32, movementBuff float32) (Snapshot, bool, error) {
	if e == nil || objectID == 0 || math.IsNaN(float64(movementBuff)) || math.IsInf(float64(movementBuff), 0) ||
		movementBuff <= -1 || movementBuff > 0 {
		return Snapshot{}, false, errors.New("support idle movement invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	source, isFound := e.npcs[objectID]
	if !isFound || source.IsDefeated || source.HitPoint <= 0 {
		return Snapshot{}, false, nil
	}
	source.Plan.SupportIdleMovementBuff = movementBuff
	e.npcs[objectID] = source
	return source, true, nil
}

func (e *Session) ClearSupportIdleMovementBuff(objectID uint32) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	source, isFound := e.npcs[objectID]
	if isFound {
		source.Plan.SupportIdleMovementBuff = 0
		e.npcs[objectID] = source
	}
}
