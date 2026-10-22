package gameplay

import (
	"sort"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/zone"
)

// The caller holds registry.mutex. Chunk 621 uses the native hostile creature
// predicate, not the AI acquisition predicate (which also hides stealth).
func (e campaignNPCActionRuntime) drainerDeathTargetLocked(world *zone.Zone, objectID uint32) (zone.NPCTarget, bool) {
	if world == nil {
		return zone.NPCTarget{}, false
	}
	director := world.DirectorDefinition()
	for _, member := range e.registry.sessions {
		if member.zone != world || member.isZoneTerminal() || member.deployedObjectID != objectID ||
			member.deployedCreatureIndex >= uint32(len(member.binding.Creatures)) ||
			!isDrainerOwnerConnected(world, member.binding.UserID, member.generation) {
			continue
		}
		nounID := member.binding.Creatures[member.deployedCreatureIndex].Noun
		actor, isFound := world.Hero().Snapshot(member.binding.UserID, member.generation)
		if !isFound || actor.ObjectID != objectID || actor.HitPoint <= 0 || director.NounTypeForAssetID(nounID) != game.NounTypeCreature {
			continue
		}
		// Current deployed heroes have no Intangible writer. Ghost Form adds
		// Incorporeal instead and remains eligible for its later dodge roll.
		return zone.NPCTarget{UserID: actor.UserID, PeerGeneration: actor.PeerGeneration,
			ObjectID: objectID, Position: actor.Position, LinearVelocity: actor.LinearVelocity,
			FootprintRadius: actor.FootprintRadius, ActorFootprintRadius: actor.FootprintRadius,
			HitPoint: actor.HitPoint, ManaPoint: actor.ManaPoint, IsHero: true}, true
	}
	actor, isFound := world.Companion().Snapshot(objectID)
	if !isFound || !actor.IsCombatant || actor.IsIntangible || actor.HitPoint <= 0 ||
		director.NounTypeForAssetID(actor.Noun) != game.NounTypeCreature {
		return zone.NPCTarget{}, false
	}
	for _, member := range e.registry.sessions {
		if member.zone == world && !member.isZoneTerminal() && member.binding.UserID == actor.UserID && member.generation == actor.PeerGeneration &&
			isDrainerOwnerConnected(world, actor.UserID, actor.PeerGeneration) {
			return zone.NPCTarget{UserID: actor.UserID, PeerGeneration: actor.PeerGeneration,
				ObjectID: objectID, Position: actor.PositionAt(e.now()), FootprintRadius: actor.FootprintRadius,
				ActorFootprintRadius: actor.FootprintRadius, HitPoint: actor.HitPoint}, true
		}
	}
	return zone.NPCTarget{}, false
}

func isDrainerOwnerConnected(world *zone.Zone, userID uint64, generation uint64) bool {
	for _, member := range world.Snapshot().Members {
		if member.UserID == userID && member.PeerGeneration == generation {
			return member.IsConnected
		}
	}
	return false
}

func (e campaignNPCActionRuntime) drainerDeathTargetsLocked(world *zone.Zone, position game.Vec3) []zone.NPCTarget {
	objectIDs := make([]uint32, 0)
	for _, actor := range world.Hero().Snapshots() {
		objectIDs = append(objectIDs, actor.ObjectID)
	}
	for _, actor := range world.Companion().Snapshots() {
		objectIDs = append(objectIDs, actor.ObjectID)
	}
	sort.Slice(objectIDs, func(left, right int) bool { return objectIDs[left] < objectIDs[right] })
	targets := make([]zone.NPCTarget, 0, len(objectIDs))
	for _, objectID := range objectIDs {
		target, isFound := e.drainerDeathTargetLocked(world, objectID)
		if isFound && target.Position.Sub(position).Length() <= 6 {
			targets = append(targets, target)
		}
	}
	return targets
}
