package companion

import (
	"errors"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

func (e Actor) PositionAt(at time.Time) game.Vec3 {
	return e.followPosition(at)
}

func (e *Session) RestoreCooldown(objectID uint32, expectedEnd time.Time, previousEnd time.Time) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound || !actor.CooldownEnd.Equal(expectedEnd) {
		return false
	}
	actor.CooldownEnd = previousEnd
	e.actors[objectID] = actor
	return true
}

// SetAttackReleaseEnd retains the accepted cast's release gate independently of
// its cooldown. Rewind can shorten admission without interrupting that cast.
func (e *Session) SetAttackReleaseEnd(objectID uint32, targetObjectID uint32, end time.Time) bool {
	if e == nil || end.IsZero() {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound || actor.TargetObjectID != targetObjectID {
		return false
	}
	actor.AttackReleaseEnd = end
	e.actors[objectID] = actor
	return true
}

// ScaleRemaining uses the native manager's integer millisecond delta and skips
// cooldowns already covered by the current action's release gate.
func (e *Session) ScaleRemaining(objectID uint32, at time.Time, scale float32) (Actor, bool, error) {
	if e == nil || objectID == 0 || at.IsZero() || scale < 0 ||
		math.IsNaN(float64(scale)) || math.IsInf(float64(scale), 0) {
		return Actor{}, false, errors.New("companion cooldown scale invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound || actor.HitPoint <= 0 || !actor.IsCombatant || !actor.IsTargetable {
		return Actor{}, false, nil
	}
	remaining := actor.CooldownEnd.Sub(at)
	if remaining <= max(time.Duration(0), actor.AttackReleaseEnd.Sub(at)) {
		return Actor{}, false, nil
	}
	delta := time.Duration(int64(float32(scale-1)*float32(remaining/time.Millisecond))) * time.Millisecond
	if delta == 0 {
		return Actor{}, false, nil
	}
	actor.CooldownEnd = actor.CooldownEnd.Add(delta)
	e.actors[objectID] = actor
	return actor, true, nil
}
