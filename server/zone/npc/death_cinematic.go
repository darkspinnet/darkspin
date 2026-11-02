package npc

import (
	"errors"
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

const MinionCinematicDeathDuration = 100000000 * time.Second

// Final Corruptor death owns the radius-100 teardown after four seconds.
// The first-stage resurrection and every other parent's cleanup stay separate.
func preservesCorruptorDeathChildren(actor Snapshot, isStageTwoStarted bool) bool {
	return !isStageTwoStarted && actor.IsCorruptorStageTwo &&
		actor.Plan.OwnerObjectID == 0 && IsCorruptorNoun(actor.Plan.NounName)
}

// DeathCinematicTargets preserves chunk446's first query: all living NPC
// classes, without a team filter. The caller separately builds its friendly
// kill queue. Spatial leaves use the same native padding as the entrance query.
func (e *Session) DeathCinematicTargets(sourceID uint32, position game.Vec3) ([]Snapshot, error) {
	if e == nil || sourceID == 0 || !zonegeometry.IsFinite(position) {
		return nil, errors.New("death cinematic query unavailable")
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	targets := make([]Snapshot, 0)
	for _, objectID := range e.objectIDs {
		target := e.npcs[objectID]
		if objectID == sourceID || !target.IsPublished || target.IsBirthPending ||
			target.IsDefeated || target.HitPoint <= 0 || !target.Plan.NPCProfile.IsClassKnown ||
			target.Plan.NPCProfile.NPCType == ^uint32(0) ||
			zonegeometry.Distance(position, target.Plan.Position) >= 110+target.Plan.ActorFootprintRadius() {
			continue
		}
		targets = append(targets, target)
	}
	sort.SliceStable(targets, func(left, right int) bool {
		return zonegeometry.Distance(position, targets[left].Plan.Position) <
			zonegeometry.Distance(position, targets[right].Plan.Position)
	})
	return targets, nil
}

// HoldDeathCinematic adds an independent Immobilized contribution. It neither
// depends on a living boss nor borrows the second-aggro reservation token.
func (e *Session) HoldDeathCinematic(sourceID, instanceID, targetID uint32, until time.Time) bool {
	if e == nil || sourceID == 0 || instanceID == 0 || until.IsZero() {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	target, isFound := e.npcs[targetID]
	if !isFound || target.IsDefeated || !target.IsPublished || target.HitPoint <= 0 {
		return false
	}
	if target.cinematic.immobilizations == nil {
		target.cinematic.immobilizations = make(map[cinematicOwner]time.Time)
	}
	owner := cinematicOwner{objectID: sourceID, generation: uint64(instanceID)}
	target.cinematic.immobilizations[owner] = until
	if until.After(target.cinematic.immobilizedUntil) {
		target.cinematic.immobilizedUntil = until
	}
	target.interruptZapCharge()
	e.npcs[targetID] = target
	return true
}

func (e *Session) ReleaseDeathCinematic(sourceID, instanceID, targetID uint32) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	target, isFound := e.npcs[targetID]
	if !isFound {
		return
	}
	delete(target.cinematic.immobilizations, cinematicOwner{objectID: sourceID, generation: uint64(instanceID)})
	target.cinematic.immobilizedUntil = time.Time{}
	for _, until := range target.cinematic.immobilizations {
		if until.After(target.cinematic.immobilizedUntil) {
			target.cinematic.immobilizedUntil = until
		}
	}
	e.npcs[targetID] = target
}
