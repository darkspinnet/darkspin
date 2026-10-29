package companion

import "time"

// MovementModifierSource is a read projection of shared live effects. The
// companion owns movement while the effect owner owns creation and retirement.
type MovementModifierSource interface {
	MovementDebuff(objectID uint32, at time.Time) (float32, time.Time)
}

func (e *Session) BindMovementModifiers(source MovementModifierSource) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.movementModifierSource = source
}

func (e *Session) refreshMovementModifiersLocked(actor Actor, at time.Time) Actor {
	if e.movementModifierSource == nil || at.IsZero() {
		return actor
	}
	for !actor.movementModifierExpiry.IsZero() && !at.Before(actor.movementModifierExpiry) {
		changeAt := actor.movementModifierExpiry
		buff, nextExpiry := e.movementModifierSource.MovementDebuff(actor.ObjectID, changeAt)
		actor = e.rebaseMovementModifierLocked(actor, changeAt, buff)
		actor.movementModifierExpiry = nextExpiry
	}
	buff, nextExpiry := e.movementModifierSource.MovementDebuff(actor.ObjectID, at)
	actor = e.rebaseMovementModifierLocked(actor, at, buff)
	actor.movementModifierExpiry = nextExpiry
	return actor
}

func (e *Session) rebaseMovementModifierLocked(actor Actor, at time.Time, buff float32) Actor {
	if actor.movementModifierBuff == buff {
		return actor
	}
	previousScale := max(float32(.1), 1+actor.MovementSpeedBuff+actor.movementModifierBuff)
	scale := max(float32(.1), 1+actor.MovementSpeedBuff+buff)
	position := actor.PositionAt(at)
	if actor.IsFollowing && at.Before(actor.followDeadline) {
		remaining := actor.followDeadline.Sub(at)
		remaining = time.Duration(float64(remaining) * float64(previousScale/scale))
		actor.followPlan.Position = position
		actor.followPlan.TravelDuration = remaining
		actor.followPlan.Deadline = at.Add(remaining)
		actor.followStartedAt = at
		actor.followDeadline = at.Add(remaining)
	}
	if actor.PursuitObjectID != 0 && !actor.isForcedPursuit && at.Before(actor.pursuitDeadline) {
		remaining := actor.pursuitDeadline.Sub(at)
		remaining = time.Duration(float64(remaining) * float64(previousScale/scale))
		actor.pursuitSource = position
		actor.pursuitStartedAt = at
		actor.pursuitDeadline = at.Add(remaining)
	}
	actor.Position = position
	actor.movementModifierBuff = buff
	e.motionRevision++
	return actor
}

func movementScale(actor Actor) float32 {
	return max(float32(.1), 1+actor.MovementSpeedBuff+actor.movementModifierBuff)
}

// RefreshMovement retains motion revisions, so original arrival jobs can query
// the changed remaining duration instead of completing an old faster plan.
func (e *Session) RefreshMovement(objectID uint32, at time.Time) (Actor, bool) {
	if e == nil {
		return Actor{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.actors[objectID]
	if !isFound {
		return Actor{}, false
	}
	actor = e.refreshMovementModifiersLocked(actor, at)
	e.actors[objectID] = actor
	actor.Position = actor.PositionAt(at)
	return actor, true
}

func (e *Session) MovementSpeedBuff(objectID uint32, at time.Time) (float32, bool) {
	actor, isFound := e.RefreshMovement(objectID, at)
	if !isFound {
		return 0, false
	}
	return movementScale(actor) - 1, true
}

func (e *Session) FollowRemaining(objectID uint32, revision uint64, at time.Time) (time.Duration, bool) {
	actor, isFound := e.RefreshMovement(objectID, at)
	if !isFound || !actor.IsFollowing || actor.FollowRevision != revision {
		return 0, false
	}
	return movementCheckDelay(actor, actor.followDeadline, at), true
}

func (e *Session) PursuitRemaining(objectID uint32, revision uint64, at time.Time) (time.Duration, bool) {
	actor, isFound := e.RefreshMovement(objectID, at)
	if !isFound || actor.PursuitRevision != revision || actor.PursuitObjectID == 0 {
		return 0, false
	}
	return movementCheckDelay(actor, actor.pursuitDeadline, at), true
}

// An expiry can accelerate arrival, so its boundary must be checked before a
// previously computed slower goal deadline. Zero means the actual goal is due.
func movementCheckDelay(actor Actor, deadline time.Time, at time.Time) time.Duration {
	remaining := max(time.Duration(0), deadline.Sub(at))
	if actor.movementModifierBuff < 0 && !actor.isForcedPursuit {
		remaining = min(remaining, minimumPursuitDuration)
	}
	if actor.movementModifierExpiry.After(at) {
		remaining = min(remaining, actor.movementModifierExpiry.Sub(at))
	}
	return remaining
}
