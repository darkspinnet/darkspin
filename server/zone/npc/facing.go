package npc

import (
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

// IsFacingTarget keeps native activation policy separate from animation names.
// Named overrides cover server-authored profiles that do not use the Lua loader.
func (e ActionProfile) IsFacingTarget() bool {
	if e.IsFacingPolicyKnown || e.IsFacingSuppressed {
		return !e.IsFacingSuppressed
	}
	switch e.AbilityName {
	case "ZelemBasicPackMeleeCower", "NomadShielderPositionForAttack",
		"CitadelSpecificFour_Position", "ScaldronBasicDoppler_Clone",
		"NomadShielderGrenade", "NomadBioSpecialJumpAttackMelee",
		"CitadelSpecialThree_LaserZone", "NomadSpecialFourVomit",
		"CitadelSpecificFour_Bolt", "NoctGhostChargerPose",
		"FirstAggro_SpawnEater_Minion", "ScaldronBasicMaser_Shot",
		"ScaldronBasicSinkhole_Sinkhole", "FirstAggro_Anim_NonFacing",
		"FirstAggro_ActivateRobot", "SubsequentAggro_ActivateRobot",
		"NomadShielderBash", "FirstAggro_VerdanthBasicHealer",
		"SubsequentAggro_ActivateSpecialRobot", "MutationAgent_Shot",
		"HurlMagma", "FirstAggro_ActivateSpecialRobot", "ShadowBossDuplicate":
		return false
	}
	return true
}

func (e AttackPlan) IsFacingTarget() bool {
	return e.SourceObjectID != e.TargetObjectID && e.TargetObjectID != 0 &&
		e.Profile.IsFacingTarget() && zonegeometry.IsFinite(e.SourcePosition) &&
		zonegeometry.IsFinite(e.TargetPosition) &&
		zonegeometry.Distance(e.SourcePosition, e.TargetPosition) > 0.001
}

// CommitFacing rejects retired action plans. Position corrections never imply
// a turn; only the accepted action or locomotion operation owns its heading.
func (e *Session) CommitFacing(plan AttackPlan) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	npc, isFound := e.npcs[plan.SourceObjectID]
	if !isFound || npc.IsDefeated || !npc.IsPublished || !npc.IsActionStarted ||
		plan.ActionGeneration != npc.ActionGeneration {
		return false
	}
	if plan.IsFacingTarget() {
		npc.Facing = directionTo(plan.SourcePosition, plan.TargetPosition)
		e.npcs[plan.SourceObjectID] = npc
	}
	return true
}

// FacingSpawnPlan restores the current heading in the create prefix itself,
// before any visibility or animation updates can expose the actor.
func (e Snapshot) FacingSpawnPlan() SpawnPlan {
	plan := e.Plan.Clone()
	length := e.Facing.Length()
	if length <= 0 || !zonegeometry.IsFinite(e.Facing) {
		return plan
	}
	facing := e.Facing.Scale(1 / length)
	plan.Rotation = game.Vec3{
		X: float32(math.Asin(max(-1, min(1, float64(facing.Z)))) * 180 / math.Pi),
		Z: float32(math.Atan2(-float64(facing.X), float64(facing.Y)) * 180 / math.Pi),
	}
	return plan
}

// InitialFacing projects the authored rotation's forward (+Y). Campaign
// actors without an authored transform use the same valid zero-Euler heading.
func (e SpawnPlan) InitialFacing() game.Vec3 {
	sx, cx := math.Sincos(float64(e.Rotation.X) * math.Pi / 360)
	sy, cy := math.Sincos(float64(e.Rotation.Y) * math.Pi / 360)
	sz, cz := math.Sincos(float64(e.Rotation.Z) * math.Pi / 360)
	// Build 103 sub_5389B0, followed by quaternion rotation of +Y.
	x := cz*sx*cy - sz*cx*sy
	y := sz*sx*cy + cz*cx*sy
	z := sz*cx*cy - cz*sx*sy
	w := sz*sx*sy + cz*cx*cy
	return game.Vec3{
		X: float32(2 * (x*y - w*z)), Y: float32(1 - 2*(x*x+z*z)),
		Z: float32(2 * (y*z + w*x)),
	}
}

// FirstAggroFacingPlan represents activation even when its Lua Tick only yields
// and has no animation. Scripted entrances and dormant activation retain pose.
func (e FirstActionPlan) FirstAggroFacingPlan() AttackPlan {
	profile := ActionProfile{AbilityName: e.Profile.FirstAggroAbilityName}
	profile.IsFacingSuppressed = e.Profile.IsFirstAggroFacingSuppressed ||
		e.Profile.PreAggroAnimationName != "" || e.Profile.FirstAggroRevealDelay > 0
	return AttackPlan{
		SourceObjectID: e.ObjectID, TargetObjectID: e.TargetObjectID,
		ActionGeneration: e.ActionGeneration, SourcePosition: e.SourcePosition,
		TargetPosition: e.TargetPosition, Profile: profile,
	}
}

func (e Snapshot) IsIdleTurnNeeded(target game.Vec3) bool {
	direction := directionTo(e.Plan.Position, target)
	length := e.Facing.Length() * direction.Length()
	if length <= 0 || e.IsDefeated || e.IsTurtleActive {
		return false
	}
	dot := (e.Facing.X*direction.X + e.Facing.Y*direction.Y +
		e.Facing.Z*direction.Z) / length
	return dot < float32(math.Cos(math.Pi/6))
}
