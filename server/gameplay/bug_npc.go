package gameplay

import (
	"sort"

	"github.com/darkspinnet/darkspin/server/chat"
	"github.com/darkspinnet/darkspin/server/game"
)

const maximumBugNearbyNPCCount = 16

func appendBugNPCs(result *chat.BugContext, peerSession gameplayPeerSession) {
	if peerSession.zone == nil || peerSession.zone.NPCs() == nil {
		return
	}
	position := game.Vec3(peerSession.playerPosition)
	npcs := peerSession.zone.NPCs().Snapshots()
	result.NPCCount = len(npcs)
	candidates := make([]chat.BugNPC, 0, len(npcs))
	for _, npc := range npcs {
		candidate := chat.BugNPC{
			ObjectID: npc.Plan.ObjectID, NounName: npc.Plan.NounName,
			LocusID: npc.Plan.LocusID, Position: vec3(npc.Plan.Position),
			Distance: npc.Plan.Position.Sub(position).Length(), HitPoint: npc.HitPoint,
			TargetObjectID: npc.TargetObjectID, IsPublished: npc.IsPublished,
			IsDefeated: npc.IsDefeated,
		}
		if candidate.ObjectID == peerSession.attackPose.targetObjectID {
			result.AttackTargetNPC = &candidate
		}
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].Distance != candidates[right].Distance {
			return candidates[left].Distance < candidates[right].Distance
		}
		return candidates[left].ObjectID < candidates[right].ObjectID
	})
	result.NearbyNPCs = candidates[:min(len(candidates), maximumBugNearbyNPCCount)]
}
