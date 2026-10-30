package gameplay

import (
	"math/rand/v2"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const playerAIEngagementDistance = float32(20)
const playerAIAllyLeash = float32(30)
const playerAICampaignBotEngagementDistance = float32(24)
const playerAITankScanDistance = float32(20)
const playerAITankPursuitDistance = float32(28)

type playerAITarget struct {
	objectID      uint32
	position      raknet.Vector3
	radius        float32
	aggroTargetID uint32
	healthRatio   float32
	isPriority    bool
}

type playerAITargetChoice struct {
	target   playerAITarget
	score    float32
	distance float32
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
	member *gameplayPeerSession, ally gameplayPeerSession, isAllyFound bool, now time.Time,
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
				position: candidate.playerPosition, radius: candidate.deployedCampaignActorFootprintRadius(),
				healthRatio: candidate.deployedHitPoint() / max(float32(1), candidate.characterHitPointMaximum(candidate.deployedCreatureIndex))})
		}
	} else if member.zone != nil && member.zone.NPCs() != nil {
		for _, npc := range member.zone.NPCs().Snapshots() {
			if !npc.IsPublished || npc.IsDefeated || npc.HitPoint <= 0 ||
				npc.Faction != zonenpc.FactionNonPlayerAligned || npc.IsSpawnStealthActive ||
				!npc.Plan.NPCProfile.IsTargetable {
				continue
			}
			isDestructible := npc.Plan.IsFixture ||
				npc.Plan.NPCProfile.NounType == game.NounTypeDestructibleOrnament
			if member.binding.IsServerBot && isDestructible &&
				(npc.TargetObjectID != member.deployedObjectID || member.playerAI.survival.recentLoss(now) <= 0) {
				continue
			}
			radius := npc.Plan.ActorFootprintRadius()
			targets = append(targets, playerAITarget{objectID: npc.Plan.ObjectID,
				position: raknet.Vector3(npc.Plan.Position), radius: radius, aggroTargetID: npc.TargetObjectID,
				healthRatio: npc.HitPoint / max(float32(1), npc.Plan.NPCProfile.HitPoint),
				isPriority:  npc.Plan.IsElite || npc.Plan.IsCaptain || npc.Plan.IsBoss})
		}
	}
	selected := playerAITarget{}
	bestScore := float32(0)
	bestDistance := float32(0)
	isBestUrgent := false
	choices := make([]playerAITargetChoice, 0, len(targets))
	isTankBot := member.binding.IsServerBot && member.binding.Mode == game.ModeChain && isAllyFound &&
		strings.EqualFold(member.binding.Creatures[member.deployedCreatureIndex].ClassType, "sentinel")
	isCampaignBot := member.binding.IsServerBot && member.binding.Mode == game.ModeChain
	engagementDistance := playerAIEngagementDistance
	if isCampaignBot {
		engagementDistance = playerAICampaignBotEngagementDistance
	}
	for _, target := range targets {
		distance := zonegeometry.Distance(game.Vec3(member.playerPosition), game.Vec3(target.position))
		allyDistance := float32(0)
		if isAllyFound {
			allyDistance = zonegeometry.Distance(game.Vec3(ally.playerPosition), game.Vec3(target.position))
		}
		isTankScanTarget := isTankBot && allyDistance <= playerAITankScanDistance &&
			distance <= playerAITankPursuitDistance
		if member.binding.Mode != game.ModeArena && distance > engagementDistance && !isTankScanTarget {
			continue
		}
		if member.binding.Mode != game.ModeArena && isAllyFound && !isCampaignBot &&
			allyDistance > playerAIAllyLeash {
			continue
		}
		score := distance
		if isTankBot {
			score = allyDistance + distance*0.25
		}
		if target.healthRatio <= 0.20 && distance <= 10 {
			score -= 15
		}
		isUrgent := isAllyFound && target.aggroTargetID == ally.deployedObjectID &&
			ally.deployedHitPoint() < ally.characterHitPointMaximum(ally.deployedCreatureIndex)*0.40
		if isUrgent {
			score -= 100
		}
		if isAllyFound && ally.assistTargetObjectID == target.objectID && now.Sub(ally.assistTargetAt) < 8*time.Second {
			score -= 100
		} else if isAllyFound && target.aggroTargetID == ally.deployedObjectID {
			score -= 50
		} else if member.assistTargetObjectID == target.objectID {
			score -= 10
		}
		choices = append(choices, playerAITargetChoice{target: target, score: score, distance: distance})
		if selected.objectID == 0 || score < bestScore ||
			(score == bestScore && target.objectID < selected.objectID) {
			selected, bestScore, bestDistance, isBestUrgent = target, score, distance, isUrgent
		}
	}
	if !isCampaignBot || selected.objectID == 0 {
		return selected, selected.objectID != 0
	}
	botCount, _ := e.campaignBotFormationLocked(*member)
	if botCount < 2 {
		return selected, true
	}
	// A recent choice stays put briefly, unless a much better nearby threat appears.
	if now.Before(member.playerAI.targetUntil) {
		for _, choice := range choices {
			if choice.target.objectID == member.playerAI.targetObjectID &&
				(choice.score <= bestScore+3 || (!isBestUrgent && choice.distance <= bestDistance+3)) {
				return choice.target, true
			}
		}
	}
	nearChoices := make([]playerAITargetChoice, 0, len(choices))
	for _, choice := range choices {
		if choice.target.objectID != selected.objectID &&
			(choice.score <= bestScore+3 || (!isBestUrgent && choice.distance <= bestDistance+3)) {
			nearChoices = append(nearChoices, choice)
		}
	}
	if len(nearChoices) > 0 && rand.Float64() < 0.4 {
		selected = nearChoices[rand.IntN(len(nearChoices))].target
	}
	member.playerAI.targetObjectID = selected.objectID
	member.playerAI.targetUntil = now.Add(2 * time.Second)
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
