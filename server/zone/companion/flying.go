package companion

import (
	"errors"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

// FollowFlyingActor mirrors build 103's nBehavior_Follow_Owner_Flying:
// pathfind outside 16 units; otherwise steer a three-unit exact goal around
// the owner, with a preferred distance of four and at most 45 degrees per tick.
func (e *Session) FollowFlyingActor(userID uint64, generation uint64, ownerID uint32,
	objectID uint32, ownerPosition game.Vec3, at time.Time,
) ([]Follow, error) {
	if e == nil || userID == 0 || generation == 0 || ownerID == 0 || objectID == 0 ||
		!isFinitePosition(ownerPosition) || at.IsZero() {
		return nil, errors.New("flying follow invalid")
	}
	e.mu.Lock()
	actor, isFound := e.actors[objectID]
	if !isFound || actor.UserID != userID || actor.PeerGeneration != generation ||
		actor.OwnerObjectID != ownerID || actor.HitPoint <= 0 || actor.IsAbilityControlled ||
		actor.IsStationary || at.Before(actor.followHoldUntil) || actor.IsCinematicImmobilized(at) {
		e.mu.Unlock()
		return nil, nil
	}
	actor = e.refreshMovementModifiersLocked(actor, at)
	actor.Position = actor.followPosition(at)
	delta := ownerPosition.Sub(actor.Position)
	facing := actor.Facing
	if actor.IsFollowing {
		facing = actor.followPlan.Goal.Sub(actor.followPlan.Position)
	}
	if facing.Length() == 0 {
		facing = game.Vec3{X: 1}
	}
	facing = facing.Scale(1 / facing.Length())
	angle := math.Atan2(float64(facing.X*delta.Y-facing.Y*delta.X),
		float64(facing.X*delta.X+facing.Y*delta.Y)) * 180 / math.Pi
	if angle < 0 {
		angle += 360
	}
	turn := float64(0)
	if delta.Length() < 4 {
		if angle < 160 || angle > 200 {
			turn = -45
			if angle > 180 && angle+turn < 180 {
				turn = 180 - angle
			}
		}
	} else if angle > 20 && angle < 340 {
		turn = math.Min(45, 360-angle)
	}
	radian := turn * math.Pi / 180
	facing = game.Vec3{
		X: float32(float64(facing.X)*math.Cos(radian) - float64(facing.Y)*math.Sin(radian)),
		Y: float32(float64(facing.X)*math.Sin(radian) + float64(facing.Y)*math.Cos(radian)),
		Z: facing.Z,
	}
	speed := actor.MovementSpeed * movementScale(actor)
	if speed <= 0 {
		e.mu.Unlock()
		return nil, errors.New("flying speed unavailable")
	}
	goal := actor.Position.Add(facing.Scale(3))
	destination := goal
	stopDistance := float32(0)
	isExact := delta.Length() <= 16
	if !isExact {
		goal = ownerPosition
		stopDistance = 8
		destination = ownerPosition.Sub(delta.Scale(stopDistance / delta.Length()))
		facing = delta.Scale(1 / delta.Length())
	}
	duration := max(minimumPursuitDuration,
		time.Duration(float64(destination.Sub(actor.Position).Length()/speed)*float64(time.Second)))
	actor.FollowRevision = e.nextFollowRevision()
	plan := Follow{
		IsExact: isExact, ObjectID: objectID, OwnerObjectID: ownerID,
		Position: actor.Position, Goal: goal, Destination: destination, DesiredStopDistance: stopDistance,
		TravelDuration: duration, Deadline: at.Add(duration), Revision: actor.FollowRevision,
	}
	actor.Facing = facing
	actor.followPlan = plan
	actor.followStartedAt = at
	actor.followDeadline = plan.Deadline
	actor.IsFollowing = true
	actor.TargetObjectID = 0
	actor.clearPursuit()
	e.actors[objectID] = actor
	e.mu.Unlock()
	return []Follow{plan}, nil
}
