package gameplay

import (
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// Campaign bots recover inside a live Tree of Life before following their
// teammate again. A ranged attacker that is actually hurting the bot can
// interrupt this hold when the bot cannot return fire from the tree.
func (e gameplayPendingRuntime) playerAITreeGoalLocked(
	member gameplayPeerSession, now time.Time, attackReach float32,
) (raknet.Vector3, bool, playerAITarget) {
	if !member.binding.IsServerBot || member.binding.Mode != game.ModeChain ||
		member.zone == nil || member.zone.TreeOfLife() == nil {
		return raknet.Vector3{}, false, playerAITarget{}
	}
	maximum := member.characterHitPointMaximum(member.deployedCreatureIndex)
	if maximum <= 0 || member.deployedHitPoint() >= maximum {
		return raknet.Vector3{}, false, playerAITarget{}
	}
	center := raknet.Vector3{}
	radius := float32(0)
	closestDistance := float32(0)
	for _, tree := range member.zone.TreeOfLife().SnapshotsAt(member.zone.Elapsed(now)) {
		if tree.Team != abilityActorTeam(member) || tree.Radius <= 0 {
			continue
		}
		position := raknet.Vector3(tree.Position)
		distance := zonegeometry.Distance(game.Vec3(member.playerPosition), game.Vec3(position))
		if distance > tree.Radius+8 || (radius > 0 && distance >= closestDistance) {
			continue
		}
		center, radius, closestDistance = position, tree.Radius, distance
	}
	if radius <= 0 {
		return raknet.Vector3{}, false, playerAITarget{}
	}
	if member.playerAI.survival.recentLoss(now) > 0 {
		threat := playerAITreeRangedThreat(member, attackReach)
		if threat.objectID != 0 {
			return raknet.Vector3{}, false, threat
		}
	}
	// Leave a small margin so position correction does not put the bot just
	// outside the authored healing radius at the next pulse.
	if closestDistance <= max(float32(0.5), radius-1) {
		return member.playerPosition, true, playerAITarget{}
	}
	return center, true, playerAITarget{}
}

func playerAITreeRangedThreat(member gameplayPeerSession, attackReach float32) playerAITarget {
	if member.zone.NPCs() == nil {
		return playerAITarget{}
	}
	selected := playerAITarget{}
	closestDistance := float32(0)
	for _, npc := range member.zone.NPCs().Snapshots() {
		if !npc.IsPublished || npc.IsDefeated || npc.HitPoint <= 0 ||
			npc.TargetObjectID != member.deployedObjectID || npc.Faction != zonenpc.FactionNonPlayerAligned ||
			npc.IsSpawnStealthActive || !npc.Plan.NPCProfile.IsTargetable {
			continue
		}
		position := raknet.Vector3(npc.Plan.Position)
		radius := npc.Plan.ActorFootprintRadius()
		distance := zonegeometry.Distance(game.Vec3(member.playerPosition), game.Vec3(position))
		if distance <= attackReach+radius || (selected.objectID != 0 && distance >= closestDistance) {
			continue
		}
		profile, isFound := campaignNPCActionProfile(npc.Plan, game.Vec3(member.playerPosition),
			member.deployedCampaignActorFootprintRadius())
		if !isFound || profile.Range <= 5 || !isPlayerAITreeRangedProfile(profile.Family) {
			continue
		}
		selected = playerAITarget{
			objectID: npc.Plan.ObjectID, position: position, radius: radius,
			aggroTargetID: npc.TargetObjectID,
			healthRatio:   npc.HitPoint / max(float32(1), npc.Plan.NPCProfile.HitPoint),
			isPriority:    npc.Plan.IsElite || npc.Plan.IsCaptain || npc.Plan.IsBoss,
		}
		closestDistance = distance
	}
	return selected
}

func isPlayerAITreeRangedProfile(family zonenpc.ActionFamily) bool {
	switch family {
	case zonenpc.ActionProjectile, zonenpc.ActionZelemRanged, zonenpc.ActionNomadSnipe,
		zonenpc.ActionNomadDrone, zonenpc.ActionRetainedArea, zonenpc.ActionChannelDrain,
		zonenpc.ActionPushPull:
		return true
	default:
		return false
	}
}
