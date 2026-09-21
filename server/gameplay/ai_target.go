package gameplay

import (
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const playerAIEngagementDistance = float32(20)
const playerAIAllyLeash = float32(30)

type playerAITarget struct {
	objectID      uint32
	position      raknet.Vector3
	radius        float32
	aggroTargetID uint32
}

func (e gameplayPendingRuntime) playerAIAllyLocked(member gameplayPeerSession) (gameplayPeerSession, bool) {
	ally := gameplayPeerSession{}
	isFound := false
	closestDistance := float32(0)
	for _, candidate := range e.registry.sessions {
		if candidate.binding.GameID != member.binding.GameID ||
			candidate.binding.UserID == member.binding.UserID ||
			candidate.binding.Mode != member.binding.Mode || !isPlayerAISession(candidate) ||
			candidate.deployedHitPoint() <= 0 || candidate.playerAI.isEnabled ||
			(member.binding.Mode == game.ModeArena && candidate.binding.Team != member.binding.Team) ||
			(member.binding.Mode == game.ModeChain && candidate.zone != member.zone) {
			continue
		}
		distance := zonegeometry.Distance(game.Vec3(member.playerPosition), game.Vec3(candidate.playerPosition))
		if !isFound || distance < closestDistance ||
			(distance == closestDistance && candidate.binding.UserID < ally.binding.UserID) {
			ally, isFound, closestDistance = candidate, true, distance
		}
	}
	return ally, isFound
}

func (e gameplayPendingRuntime) playerAITargetLocked(
	member gameplayPeerSession, ally gameplayPeerSession, isAllyFound bool, now time.Time,
) (playerAITarget, bool) {
	targets := make([]playerAITarget, 0)
	if member.binding.Mode == game.ModeArena {
		for _, candidate := range e.registry.sessions {
			if candidate.binding.GameID != member.binding.GameID ||
				candidate.binding.Mode != game.ModeArena || candidate.binding.Team == member.binding.Team ||
				!isPlayerAISession(candidate) || candidate.deployedHitPoint() <= 0 ||
				!candidate.isArenaCombatActive || candidate.isTrapperStealthed {
				continue
			}
			targets = append(targets, playerAITarget{objectID: candidate.deployedObjectID,
				position: candidate.playerPosition, radius: candidate.deployedCampaignFootprintRadius()})
		}
	} else if member.zone != nil && member.zone.NPCs() != nil {
		for _, npc := range member.zone.NPCs().Snapshots() {
			if !npc.IsPublished || npc.IsDefeated || npc.HitPoint <= 0 ||
				npc.Faction != zonenpc.FactionNonPlayerAligned || npc.IsSpawnStealthActive ||
				!npc.Plan.NPCProfile.IsTargetable {
				continue
			}
			radius, radiusErr := e.action.ability.program.FootprintRadius(npc.Plan.NounName)
			if radiusErr != nil {
				radius = 0
			}
			targets = append(targets, playerAITarget{objectID: npc.Plan.ObjectID,
				position: raknet.Vector3(npc.Plan.Position), radius: radius, aggroTargetID: npc.TargetObjectID})
		}
	}
	selected := playerAITarget{}
	bestScore := float32(0)
	for _, target := range targets {
		distance := zonegeometry.Distance(game.Vec3(member.playerPosition), game.Vec3(target.position))
		if distance > playerAIEngagementDistance {
			continue
		}
		if isAllyFound && zonegeometry.Distance(game.Vec3(ally.playerPosition), game.Vec3(target.position)) > playerAIAllyLeash {
			continue
		}
		score := distance
		if isAllyFound && ally.assistTargetObjectID == target.objectID && now.Sub(ally.assistTargetAt) < 8*time.Second {
			score -= 100
		} else if isAllyFound && target.aggroTargetID == ally.deployedObjectID {
			score -= 50
		} else if member.assistTargetObjectID == target.objectID {
			score -= 10
		}
		if selected.objectID == 0 || score < bestScore ||
			(score == bestScore && target.objectID < selected.objectID) {
			selected, bestScore = target, score
		}
	}
	return selected, selected.objectID != 0
}

func playerAIApproach(source raknet.Vector3, target raknet.Vector3, stopDistance float32) raknet.Vector3 {
	distance := zonegeometry.Distance(game.Vec3(source), game.Vec3(target))
	if distance <= stopDistance {
		return source
	}
	ratio := stopDistance / distance
	return raknet.Vector3{X: target.X + (source.X-target.X)*ratio,
		Y: target.Y + (source.Y-target.Y)*ratio, Z: target.Z + (source.Z-target.Z)*ratio}
}
