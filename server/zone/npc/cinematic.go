package npc

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

const SecondAggroCinematicDuration = 6699999809 * time.Nanosecond
const SecondAggroCinematicRadius = float32(100)

// These ability-owned holds are independent of combat debuffs and shields.
// Their deadlines expire even when the transport that began the scene leaves.
type cinematicHold struct {
	generation       uint64
	immobilizations  map[cinematicOwner]time.Time
	focus            game.Vec3
	controller       ActionOwner
	isPending        bool
	intangibleUntil  time.Time
	immobilizedUntil time.Time
}

type cinematicOwner struct {
	objectID   uint32
	generation uint64
}

type CinematicLease struct {
	SourceObjectID uint32
	Generation     uint64
	Until          time.Time
	Targets        []uint32
}

func (e Snapshot) IsSecondAggroController(owner ActionOwner) bool {
	return e.cinematic.controller == owner && owner.UserID != 0 && owner.PeerGeneration != 0
}

func (e Snapshot) IsSecondAggroCinematic(generation uint64) bool {
	return generation != 0 && e.cinematic.generation == generation
}

func (e Snapshot) SecondAggroGeneration() uint64 {
	if !e.IsCorruptorStageTwo {
		return 0
	}
	return e.cinematic.generation
}

func (e *Session) ClaimSecondAggroController(objectID uint32, generation uint64, owner ActionOwner, at time.Time) bool {
	if e == nil || objectID == 0 || owner.UserID == 0 || owner.PeerGeneration == 0 || at.IsZero() {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	npc, isFound := e.npcs[objectID]
	if !isFound || npc.IsDefeated || !npc.IsCorruptorStageTwo || !npc.IsSecondAggroCinematic(generation) || at.Before(npc.cinematic.intangibleUntil) {
		return false
	}
	npc.cinematic.controller = owner
	npc.cinematic.isPending = false
	e.npcs[objectID] = npc
	return true
}

// Stage two has already been accepted by resurrection. Keep action admission
// pending while a rejected scene schedule is retried; source protection and
// rejoin focus belong to this acceptance and keep their original deadline.
func (e *Session) ReserveSecondAggroCinematic(objectID uint32, focus game.Vec3, until time.Time) (uint64, bool) {
	if e == nil || objectID == 0 || until.IsZero() || !zonegeometry.IsFinite(focus) {
		return 0, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	npc, isFound := e.npcs[objectID]
	if !isFound || npc.IsDefeated || !npc.IsCorruptorStageTwo {
		return 0, false
	}
	npc.cinematic.generation++
	if npc.cinematic.generation == 0 {
		npc.cinematic.generation = 1
	}
	npc.cinematic.isPending = true
	npc.cinematic.intangibleUntil = until
	npc.cinematic.focus = focus
	e.npcs[objectID] = npc
	return npc.cinematic.generation, true
}

func (e Snapshot) SecondAggroCinematic(now time.Time) (time.Duration, game.Vec3) {
	return max(time.Duration(0), e.cinematic.intangibleUntil.Sub(now)), e.cinematic.focus
}

func SecondAggroCinematicFocus(position game.Vec3) game.Vec3 {
	return position.Add(game.Vec3{X: 3 / math.Sqrt2, Y: 3 / math.Sqrt2})
}

// SecondAggroCinematicTargets projects the authored alive/non-NonNPC query.
// NonNPC is -1 in GlobalDefinitions; the class projection preserves its bits.
func (e *Session) SecondAggroCinematicTargets(sourceObjectID uint32, focus game.Vec3) ([]Snapshot, error) {
	if e == nil || sourceObjectID == 0 || !zonegeometry.IsFinite(focus) {
		return nil, errors.New("cinematic target context unavailable")
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	targets := make([]Snapshot, 0)
	for _, objectID := range e.objectIDs {
		target := e.npcs[objectID]
		if objectID == sourceObjectID || !target.IsPublished || target.IsDefeated || target.HitPoint <= 0 ||
			!target.Plan.NPCProfile.IsClassKnown || target.Plan.NPCProfile.NPCType == ^uint32(0) ||
			zonegeometry.Distance(target.Plan.Position, focus) >= SecondAggroCinematicRadius+10+target.Plan.ActorFootprintRadius() {
			continue
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func (e *Session) BeginSecondAggroCinematic(sourceObjectID uint32, generation uint64, targets []uint32, focus game.Vec3, until time.Time) (CinematicLease, error) {
	if e == nil || sourceObjectID == 0 || until.IsZero() || !zonegeometry.IsFinite(focus) {
		return CinematicLease{}, errors.New("cinematic hold context unavailable")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	source, isFound := e.npcs[sourceObjectID]
	if !isFound || source.IsDefeated || source.HitPoint <= 0 || !source.IsSecondAggroCinematic(generation) ||
		source.cinematic.intangibleUntil != until || source.cinematic.focus != focus {
		return CinematicLease{}, fmt.Errorf("cinematic source missing: %d", sourceObjectID)
	}
	lease := CinematicLease{SourceObjectID: sourceObjectID, Generation: source.cinematic.generation,
		Until: until, Targets: append([]uint32(nil), targets...)}
	owner := cinematicOwner{objectID: sourceObjectID, generation: lease.Generation}
	for _, objectID := range targets {
		target, isTargetFound := e.npcs[objectID]
		if !isTargetFound || target.IsDefeated || target.HitPoint <= 0 {
			continue
		}
		holds := make(map[cinematicOwner]time.Time, len(target.cinematic.immobilizations)+1)
		for key, deadline := range target.cinematic.immobilizations {
			holds[key] = deadline
		}
		holds[owner] = until
		target.cinematic.immobilizations = holds
		target.interruptZapCharge()
		if target.cinematic.immobilizedUntil.Before(until) {
			target.cinematic.immobilizedUntil = until
		}
		e.npcs[objectID] = target
	}
	return lease, nil
}

func (e *Session) EndSecondAggroCinematic(lease CinematicLease) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	// Source protection was accepted before transport admission. Rolling back
	// child presentation must not revoke it; its fixed deadline expires itself.
	owner := cinematicOwner{objectID: lease.SourceObjectID, generation: lease.Generation}
	for _, objectID := range lease.Targets {
		target, isTargetFound := e.npcs[objectID]
		if !isTargetFound || target.cinematic.immobilizations[owner] != lease.Until {
			continue
		}
		holds := make(map[cinematicOwner]time.Time, len(target.cinematic.immobilizations))
		target.cinematic.immobilizedUntil = time.Time{}
		for key, deadline := range target.cinematic.immobilizations {
			if key == owner {
				continue
			}
			holds[key] = deadline
			if deadline.After(target.cinematic.immobilizedUntil) {
				target.cinematic.immobilizedUntil = deadline
			}
		}
		target.cinematic.immobilizations = holds
		e.npcs[objectID] = target
	}
}
