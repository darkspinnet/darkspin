package companion

import (
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

// MotionSnapshot samples an existing owner-follow reservation without changing
// its revision, destination or original arrival deadline.
type MotionSnapshot struct {
	Actor         Actor
	Follow        Follow
	IsFollowing   bool
	MovementSpeed float32
}

func (e *Session) MotionRevision() uint64 {
	if e == nil {
		return 0
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.motionRevision
}

func (e *Session) MotionSnapshots(at time.Time) []MotionSnapshot {
	if e == nil || at.IsZero() {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	snapshots := make([]MotionSnapshot, 0, len(e.actors))
	for _, actor := range e.actors {
		actor.Position = actor.followPosition(at)
		snapshot := MotionSnapshot{Actor: actor}
		if actor.IsFollowing && at.Before(actor.followDeadline) {
			snapshot.MovementSpeed = actor.followPlan.Destination.Sub(actor.followPlan.Position).Length() /
				float32(actor.followPlan.TravelDuration.Seconds())
			snapshot.Follow = actor.followPlan
			snapshot.Follow.Position = actor.Position
			snapshot.Follow.TravelDuration = actor.followDeadline.Sub(at)
			snapshot.IsFollowing = true
		}
		snapshots = append(snapshots, snapshot)
	}
	sort.Slice(snapshots, func(left int, right int) bool {
		return snapshots[left].Actor.ObjectID < snapshots[right].Actor.ObjectID
	})
	return snapshots
}

func (e *Actor) followPosition(at time.Time) game.Vec3 {
	if !e.IsFollowing || e.followStartedAt.IsZero() || e.followDeadline.IsZero() {
		return e.Position
	}
	if !at.After(e.followStartedAt) {
		return e.followPlan.Position
	}
	if !at.Before(e.followDeadline) {
		return e.followPlan.Destination
	}
	fraction := float32(float64(at.Sub(e.followStartedAt)) /
		float64(e.followDeadline.Sub(e.followStartedAt)))
	return e.followPlan.Position.Add(
		e.followPlan.Destination.Sub(e.followPlan.Position).Scale(fraction),
	)
}

func (e *Actor) clearFollow() {
	e.IsFollowing = false
	e.followPlan = Follow{}
	e.followStartedAt = time.Time{}
	e.followDeadline = time.Time{}
}

// A session sequence fences replacement actors against an older object's job.
// Call with the session write lock held.
func (e *Session) nextFollowRevision() uint64 {
	e.followRevision++
	if e.followRevision == 0 {
		e.followRevision++
	}
	e.motionRevision++
	return e.followRevision
}
