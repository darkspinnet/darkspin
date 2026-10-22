package zone

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// MutationTargets is the Lua592 hostile-combatant view, independent of AI
// acquisition. Current deployed heroes are combatants and have no Intangible
// writer; Ghost Form's Incorporeal is deliberately not treated as Intangible.
func (e *Zone) MutationTargets(faction zonenpc.Faction, at time.Time) []NPCTarget {
	if e == nil || at.IsZero() {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.state != StateActive || faction != zonenpc.FactionNonPlayerAligned {
		return nil
	}
	objectIDs := make([]uint32, 0)
	for _, actor := range e.info.Hero.Snapshots() {
		objectIDs = append(objectIDs, actor.ObjectID)
	}
	for _, actor := range e.info.Companion.Snapshots() {
		objectIDs = append(objectIDs, actor.ObjectID)
	}
	sort.Slice(objectIDs, func(left, right int) bool { return objectIDs[left] < objectIDs[right] })
	targets := make([]NPCTarget, 0, len(objectIDs))
	for _, objectID := range objectIDs {
		target, isFound := e.mutationTargetLocked(faction, objectID, at)
		if isFound {
			targets = append(targets, target)
		}
	}
	return targets
}

func (e *Zone) MutationTarget(faction zonenpc.Faction, objectID uint32, at time.Time) (NPCTarget, bool) {
	if e == nil || at.IsZero() || objectID == 0 {
		return NPCTarget{}, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.mutationTargetLocked(faction, objectID, at)
}

func (e *Zone) mutationTargetLocked(faction zonenpc.Faction, objectID uint32, at time.Time) (NPCTarget, bool) {
	// The installed NPC aggregate has no player-aligned NPC writer. All
	// current hostile recipients of this non-player agent are heroes/pets.
	if e.state != StateActive || faction != zonenpc.FactionNonPlayerAligned {
		return NPCTarget{}, false
	}
	actor, isHeroFound := e.info.Hero.SnapshotByObjectID(objectID)
	if isHeroFound {
		member, isMemberFound := e.members[actor.UserID]
		if !isMemberFound || !member.IsConnected || member.PeerGeneration != actor.PeerGeneration ||
			actor.HitPoint <= 0 || actor.CreatureIndex >= uint32(len(member.Roster.Creatures)) {
			return NPCTarget{}, false
		}
		nounID := member.Roster.Creatures[actor.CreatureIndex].Noun
		return NPCTarget{UserID: actor.UserID, PeerGeneration: actor.PeerGeneration,
			ObjectID: actor.ObjectID, Position: actor.Position, LinearVelocity: actor.LinearVelocity,
			ActorFootprintRadius: e.actorFootprintRadius(nounID, actor.FootprintRadius),
			FootprintRadius:      actor.FootprintRadius, HitPoint: actor.HitPoint,
			ManaPoint: actor.ManaPoint, IsHero: true}, true
	}
	companion, isCompanionFound := e.info.Companion.Snapshot(objectID)
	if !isCompanionFound || !companion.IsCombatant || companion.IsIntangible || companion.HitPoint <= 0 {
		return NPCTarget{}, false
	}
	member, isMemberFound := e.members[companion.UserID]
	if !isMemberFound || !member.IsConnected || member.PeerGeneration != companion.PeerGeneration {
		return NPCTarget{}, false
	}
	return NPCTarget{UserID: companion.UserID, PeerGeneration: companion.PeerGeneration,
		ObjectID: companion.ObjectID, Position: companion.PositionAt(at),
		ActorFootprintRadius: companion.ActorFootprintRadius(), FootprintRadius: companion.FootprintRadius,
		HitPoint: companion.HitPoint}, true
}

// ApplyMutationAuraDamage commits one previously prepared event target. The
// event survives source destruction only after the damage owner claimed it.
func (e *Zone) ApplyMutationAuraDamage(owner zonenpc.ActionOwner, eventID zonenpc.MutationEventID,
	targetObjectID uint32, damage float32, faction zonenpc.Faction, at time.Time,
) (NPCTargetDamage, bool, error) {
	if e == nil || owner.UserID == 0 || owner.PeerGeneration == 0 || targetObjectID == 0 ||
		damage <= 0 || math.IsNaN(float64(damage)) || math.IsInf(float64(damage), 0) || at.IsZero() {
		return NPCTargetDamage{}, false, errors.New("mutation damage invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	member, isMemberFound := e.members[owner.UserID]
	if !isMemberFound || !member.IsConnected || member.PeerGeneration != owner.PeerGeneration {
		return NPCTargetDamage{}, false, nil
	}
	work, isEventFound := e.info.NPCs.MutationDamageWork(eventID)
	if !isEventFound || !work.IsPrepared {
		return NPCTargetDamage{}, false, nil
	}
	target, isTargetFound := e.mutationTargetLocked(faction, targetObjectID, at)
	if !isTargetFound {
		return NPCTargetDamage{}, false, nil
	}
	deathRevision := uint64(0)
	if target.IsHero {
		actor, isFound := e.info.Hero.SnapshotByObjectID(targetObjectID)
		if !isFound {
			return NPCTargetDamage{}, false, nil
		}
		deathRevision = actor.DeathRevision
	} else {
		actor, isFound := e.info.Companion.Snapshot(targetObjectID)
		if !isFound {
			return NPCTargetDamage{}, false, nil
		}
		deathRevision = actor.DeathRevision
	}
	isPrepared := false
	for index := uint32(0); index < work.TargetCount; index++ {
		candidate := work.Targets[index]
		if candidate.ObjectID == targetObjectID && candidate.UserID == target.UserID &&
			candidate.PeerGeneration == target.PeerGeneration && candidate.DeathRevision == deathRevision && !candidate.IsCommitted && !candidate.IsComplete {
			isPrepared = true
			break
		}
	}
	if !isPrepared {
		return NPCTargetDamage{}, false, nil
	}
	var result NPCTargetDamage
	var isApplied bool
	var err error
	if target.IsHero {
		result, isApplied, err = e.applyNPCTargetDamage(owner, eventID.ObjectID, targetObjectID, damage)
	} else {
		// The retained mutation receipt publishes combat/death feedback once.
		// Ordinary projection would duplicate it and delete delayed pet deaths.
		result, isApplied, err = e.applyNPCCompanionDamageState(owner, eventID.ObjectID, targetObjectID, damage, false, false)
	}
	if err != nil {
		return result, isApplied, fmt.Errorf("mutationCommit: %w", err)
	}
	return result, isApplied, nil
}
