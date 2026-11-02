package gameplay

import (
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/navigation"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
)

const campaignBotIdleLeash = float32(8)
const campaignBotTankLeadDistance = float32(3.5)
const campaignBotSupportTrailDistance = float32(2.5)
const campaignBotSpacingDistance = float32(1.75)

func (e gameplayPendingRuntime) campaignBotFormationLocked(member gameplayPeerSession) (int, int) {
	if !member.binding.IsServerBot || member.binding.Mode != game.ModeChain {
		return 0, 0
	}
	count, rank := 0, 0
	for _, candidate := range e.registry.sessions {
		if !candidate.binding.IsServerBot || candidate.binding.Mode != game.ModeChain ||
			candidate.binding.GameID != member.binding.GameID || candidate.zone != member.zone ||
			!isPlayerAISession(candidate) || candidate.deployedHitPoint() <= 0 {
			continue
		}
		count++
		if candidate.binding.Slot < member.binding.Slot {
			rank++
		}
	}
	return count, rank
}

func (e gameplayPendingRuntime) isCampaignBotGoalCrowdedLocked(member gameplayPeerSession, goal raknet.Vector3) bool {
	for _, candidate := range e.registry.sessions {
		if candidate.binding.UserID == member.binding.UserID || !candidate.binding.IsServerBot ||
			candidate.binding.Mode != game.ModeChain || candidate.binding.GameID != member.binding.GameID ||
			candidate.zone != member.zone || !isPlayerAISession(candidate) || candidate.deployedHitPoint() <= 0 {
			continue
		}
		if zonegeometry.Distance(game.Vec3(candidate.playerMovementGoal), game.Vec3(goal)) < 2.5 ||
			zonegeometry.Distance(game.Vec3(candidate.playerPosition), game.Vec3(goal)) < 2.5 {
			return true
		}
	}
	return false
}

func isCampaignSupportBot(member gameplayPeerSession) bool {
	return member.binding.IsServerBot && member.binding.Mode == game.ModeChain &&
		isCampaignSupportHero(member.binding.Creatures[member.deployedCreatureIndex])
}

func isCampaignSupportHero(creature game.GameplayCreature) bool {
	return strings.EqualFold(creature.ClassType, "tempest")
}

// Idle campaign bots make occasional short moves near their human teammate.
// Combat planning takes priority and clears any unfinished wander.
func (e gameplayPendingRuntime) playerAICampaignIdleLocked(
	member *gameplayPeerSession, command raknet.ActionCommandData,
	ally gameplayPeerSession, now time.Time,
) (raknet.ActionCommandData, bool, error) {
	state := &member.playerAI
	allyPosition := ally.playerPosition
	allyDistance := zonegeometry.Distance(game.Vec3(member.playerPosition), game.Vec3(allyPosition))
	if allyDistance > campaignBotIdleLeash {
		state.isIdleWandering = false
		state.nextIdleWanderAt = time.Time{}
		return e.playerAIMoveLocked(member, command, playerFollowGoal(member.playerPosition, allyPosition))
	}
	creature := member.binding.Creatures[member.deployedCreatureIndex]
	if isCampaignSupportBot(*member) {
		trailDistance := float32(1.5)
		allyCreature := ally.binding.Creatures[ally.deployedCreatureIndex]
		if !isCampaignSupportHero(allyCreature) {
			trailDistance = campaignBotSupportTrailDistance
		}
		trailGoal, isAllyMoving := campaignBotSupportTrailGoal(allyPosition, ally.playerMovementGoal, trailDistance)
		if isAllyMoving {
			state.isIdleWandering = false
			state.nextIdleWanderAt = time.Time{}
			if zonegeometry.Distance(game.Vec3(member.playerPosition), game.Vec3(trailGoal)) <= 1 {
				return e.playerAIMoveLocked(member, command, member.playerPosition)
			}
			return e.playerAIMoveLocked(member, command, trailGoal)
		}
	}
	if strings.EqualFold(creature.ClassType, "sentinel") {
		leadGoal, isAllyMoving := campaignBotTankLeadGoal(allyPosition, ally.playerMovementGoal)
		if isAllyMoving {
			state.isIdleWandering = false
			state.nextIdleWanderAt = time.Time{}
			if zonegeometry.Distance(game.Vec3(member.playerPosition), game.Vec3(leadGoal)) <= 1 {
				return e.playerAIMoveLocked(member, command, member.playerPosition)
			}
			return e.playerAIMoveLocked(member, command, leadGoal)
		}
	}
	if state.isIdleWandering {
		goalDistance := zonegeometry.Distance(game.Vec3(member.playerPosition), game.Vec3(state.idleWanderGoal))
		if goalDistance > 0.75 && now.Before(state.idleWanderUntil) {
			return e.playerAIMoveLocked(member, command, state.idleWanderGoal)
		}
		state.isIdleWandering = false
		state.nextIdleWanderAt = now.Add(time.Duration(1+rand.IntN(7)) * time.Second)
		return e.playerAIMoveLocked(member, command, member.playerPosition)
	}
	if state.nextIdleWanderAt.IsZero() {
		state.nextIdleWanderAt = now.Add(time.Duration(1+rand.IntN(7)) * time.Second)
	}
	if now.Before(state.nextIdleWanderAt) {
		return e.playerAIMoveLocked(member, command, member.playerPosition)
	}
	angle := rand.Float64() * 2 * math.Pi
	radius := float32(2 + rand.Float64()*2)
	goal := raknet.Vector3{X: allyPosition.X + radius*float32(math.Cos(angle)),
		Y: allyPosition.Y + radius*float32(math.Sin(angle)), Z: allyPosition.Z}
	if zonegeometry.Distance(game.Vec3(member.playerPosition), game.Vec3(goal)) < 1 {
		goal.X = allyPosition.X - radius*float32(math.Cos(angle))
		goal.Y = allyPosition.Y - radius*float32(math.Sin(angle))
	}
	state.idleWanderGoal = goal
	state.idleWanderUntil = now.Add(4 * time.Second)
	state.isIdleWandering = true
	return e.playerAIMoveLocked(member, command, goal)
}

func campaignBotTankLeadGoal(position, movementGoal raknet.Vector3) (raknet.Vector3, bool) {
	deltaX := movementGoal.X - position.X
	deltaY := movementGoal.Y - position.Y
	distance := float32(math.Hypot(float64(deltaX), float64(deltaY)))
	if distance < 1.5 {
		return raknet.Vector3{}, false
	}
	leadDistance := min(campaignBotTankLeadDistance, distance)
	return raknet.Vector3{X: position.X + deltaX/distance*leadDistance,
		Y: position.Y + deltaY/distance*leadDistance, Z: position.Z}, true
}

func campaignBotSupportTrailGoal(position, movementGoal raknet.Vector3, trailDistance float32) (raknet.Vector3, bool) {
	deltaX := movementGoal.X - position.X
	deltaY := movementGoal.Y - position.Y
	distance := float32(math.Hypot(float64(deltaX), float64(deltaY)))
	if distance < 1.5 {
		return raknet.Vector3{}, false
	}
	return raknet.Vector3{X: position.X - deltaX/distance*trailDistance,
		Y: position.Y - deltaY/distance*trailDistance, Z: position.Z}, true
}

// AI goals are generated geometry, not observed clicks. Check them against
// the ordinary movement path contract before issuing a native action, so an
// off-map retreat does not repeatedly enter rejected-prediction correction.
func (e gameplayPendingRuntime) playerAIMoveLocked(
	member *gameplayPeerSession, req raknet.ActionCommandData, goal raknet.Vector3,
) (raknet.ActionCommandData, bool, error) {
	var mesh *navigation.Mesh
	if member.binding.Mode == game.ModeArena {
		match := e.registry.arenaMatches[member.binding.GameID]
		if match != nil {
			mesh = match.navigation
		}
	} else if member.zone != nil {
		mesh = member.zone.Navigation()
	}
	if mesh == nil || goal == member.playerPosition {
		return playerAIMove(member, req, goal)
	}
	planLayer, isLayerFound := mesh.SelectFootprintLayer(
		member.deployedCampaignNavigationRadius(), uint8(member.binding.Mode),
	)
	if isLayerFound {
		botCount, rank := e.campaignBotFormationLocked(*member)
		if botCount > 1 && !member.playerAI.isTreeHealing &&
			zonegeometry.Distance(game.Vec3(member.playerPosition), game.Vec3(goal)) > 0.75 &&
			e.isCampaignBotGoalCrowdedLocked(*member, goal) {
			angle := 2 * math.Pi * float64(rank) / float64(botCount)
			spreadGoal := goal
			spreadGoal.X += campaignBotSpacingDistance * float32(math.Cos(angle))
			spreadGoal.Y += campaignBotSpacingDistance * float32(math.Sin(angle))
			path, err := mesh.CreatePath(navigation.Vec3(member.playerPosition), navigation.Vec3(spreadGoal),
				navigation.PathOptions{
					ProjectionOptions: navigation.ProjectionOptions{
						PlanLayer: planLayer, MaxDistance: zonenavigation.ProjectionDistance,
					},
					MaxVisitedPolygon: 2048,
				})
			if err == nil && len(path.Points) > 0 {
				return playerAIMove(member, req, raknet.Vector3(path.Points[len(path.Points)-1]))
			}
		}
		path, err := mesh.CreatePath(navigation.Vec3(member.playerPosition), navigation.Vec3(goal),
			navigation.PathOptions{
				ProjectionOptions: navigation.ProjectionOptions{
					PlanLayer: planLayer, MaxDistance: zonenavigation.ProjectionDistance,
				},
				MaxVisitedPolygon: 2048,
			})
		if err == nil && len(path.Points) > 0 {
			return playerAIMove(member, req, raknet.Vector3(path.Points[len(path.Points)-1]))
		}
		if err != nil {
			// An unavailable generated destination is an ordinary planning miss.
			// Keep the current reachable pose instead of submitting this command.
			return stopPlayerAIForReplan(member, req, e.now())
		}
	}
	return stopPlayerAIForReplan(member, req, e.now())
}

func stopPlayerAIForReplan(member *gameplayPeerSession, req raknet.ActionCommandData, at time.Time) (raknet.ActionCommandData, bool, error) {
	// Stop once if already walking, then reconsider attacks and other plans.
	// In particular, do not spend every ranged decision retreating into a wall.
	member.playerAI.moveUntil = time.Time{}
	member.playerAI.nextRepositionAt = at.Add(time.Second)
	return playerAIMove(member, req, member.playerPosition)
}
