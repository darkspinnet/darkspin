package companion

import (
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/util"
)

// ConfigureNPCProfiles retains the imported class discriminator. Unknown nouns
// do not acquire an invented NPC class from their combat or owner flags.
func (e *Session) ConfigureNPCProfiles(profiles map[string]game.CampaignNPCProfile) {
	if e == nil {
		return
	}
	npcTypesByNoun := make(map[uint32]uint32, len(profiles))
	for nounName, profile := range profiles {
		if profile.IsClassKnown {
			npcTypesByNoun[util.HashID(nounName)] = profile.NPCType
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.npcTypesByNoun = npcTypesByNoun
}

// SecondAggroCinematicTargets includes player pets with an imported NPC class.
// Radius uses the existing actor-footprint projection of native spatial leaves.
func (e *Session) SecondAggroCinematicTargets(focus game.Vec3, at time.Time) []Actor {
	if e == nil || at.IsZero() || !isFinitePosition(focus) {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	targets := make([]Actor, 0)
	objectIDs := make(map[uint32]struct{}, len(e.actors)+len(e.retainedActors))
	for objectID := range e.actors {
		objectIDs[objectID] = struct{}{}
	}
	for objectID := range e.retainedActors {
		objectIDs[objectID] = struct{}{}
	}
	for objectID := range objectIDs {
		actor, isFound, isLive := e.cinematicActor(objectID)
		if !isFound {
			continue
		}
		// Both map sources are semantic actors during this explicit transfer.
		// The selected source is only needed by writes, not this projection.
		if !isLive && actor.lifeRevision != e.actorRevisions[objectID] {
			continue
		}
		npcType, isKnown := e.npcTypesByNoun[actor.Noun]
		if !isKnown || npcType == ^uint32(0) || actor.HitPoint <= 0 {
			continue
		}
		actor.Position = actor.PositionAt(at)
		if actor.Position.Sub(focus).Length() >= 110+actor.ActorFootprintRadius() {
			continue
		}
		targets = append(targets, actor)
	}
	sort.Slice(targets, func(left int, right int) bool {
		return targets[left].ObjectID < targets[right].ObjectID
	})
	return targets
}

// BeginCinematic owns only Immobilized. Attacks and unrelated cast holds keep
// their owners; invalidated arrivals cannot move the frozen actor afterward.
func (e *Session) BeginCinematic(target Actor, at time.Time, until time.Time) (Actor, bool) {
	if e == nil || at.IsZero() || !until.After(at) {
		return Actor{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound, isLive := e.cinematicActor(target.ObjectID)
	if !isFound || !actor.sameCinematicActor(target) ||
		actor.lifeRevision != target.lifeRevision ||
		actor.PeerGeneration != target.PeerGeneration || actor.HitPoint <= 0 {
		return Actor{}, false
	}
	actor.Position = actor.PositionAt(at)
	actor.clearFollow()
	actor.FollowRevision = e.nextFollowRevision()
	actor.clearPursuit()
	e.cinematicRevision++
	if e.cinematicRevision == 0 {
		e.cinematicRevision++
	}
	actor.cinematicRevision = e.cinematicRevision
	if actor.cinematicHolds == nil {
		actor.cinematicHolds = make(map[uint64]time.Time)
	}
	actor.cinematicHolds[actor.cinematicRevision] = until
	if until.After(actor.cinematicUntil) {
		actor.cinematicUntil = until
	}
	e.storeCinematicActor(actor, isLive)
	return actor, true
}

func (e *Session) EndCinematic(target Actor) bool {
	if e == nil || target.cinematicRevision == 0 {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound, isLive := e.cinematicActor(target.ObjectID)
	if !isFound || !actor.sameCinematicActor(target) {
		return false
	}
	deadline, isHeld := actor.cinematicHolds[target.cinematicRevision]
	if !isHeld || deadline.IsZero() {
		return false
	}
	delete(actor.cinematicHolds, target.cinematicRevision)
	actor.cinematicUntil = time.Time{}
	for _, deadline := range actor.cinematicHolds {
		if deadline.After(actor.cinematicUntil) {
			actor.cinematicUntil = deadline
		}
	}
	e.storeCinematicActor(actor, isLive)
	return true
}

func (e Actor) IsCinematicImmobilized(at time.Time) bool {
	return !at.IsZero() && at.Before(e.cinematicUntil)
}

func (e Actor) sameCinematicActor(target Actor) bool {
	return e.UserID == target.UserID && e.OwnerObjectID == target.OwnerObjectID &&
		e.Noun == target.Noun && e.DeathRevision == target.DeathRevision
}

func (e *Actor) clearCinematic() {
	e.cinematicUntil = time.Time{}
	e.cinematicRevision = 0
	e.cinematicHolds = nil
}

// Call with the session write lock held. Removal retains the latest token so
// an explicit transient restore can reject a subsequently replaced lifetime.
func (e *Session) nextLifeRevision() uint64 {
	e.lifeRevision++
	if e.lifeRevision == 0 {
		e.lifeRevision++
	}
	return e.lifeRevision
}
