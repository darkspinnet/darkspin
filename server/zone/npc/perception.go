package npc

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

// AcquisitionRange retains the server's ordinary aggro policy except for the
// stationary Herbipod turret family. Its authored alert and aggro entries both
// run FirstAggro_Turret; limiting acquisition to aggroRange stranded the turret
// at 5/7/9 units despite its 19/20/21-unit native perception circle.
// Using that recovered circle for automatic acquisition is a scoped
// compatibility rule, not a reconstruction of the native proximity updater.
func AcquisitionRange(plan SpawnPlan, fallback float32) float32 {
	profile := plan.NPCProfile
	if !profile.IsClassKnown || profile.IsPlayerPet {
		return EffectiveAggroRange(profile, fallback)
	}
	switch strings.ToLower(strings.TrimSpace(plan.NounName)) {
	case "verdanthbasicranged.noun", "verdanthbasicranged_2.noun", "verdanthbasicranged_3.noun":
		radius := EffectivePerceptionRange(&profile)
		if zonegeometry.IsFinitePositiveScalar(radius) {
			return radius
		}
	}
	return EffectiveAggroRange(profile, fallback)
}

type PerceptionQuery struct {
	ObjectID         uint32
	TargetPosition   game.Vec3
	TargetRadius     float32
	OwnerPosition    game.Vec3
	IsOwnerResolved  bool
	LevelYawOverride float32
	IsLevelResolved  bool
}

// InPerceptionCircle resolves the actor's current perception center. The only
// mutation is the pet's first successful level-derived offset; acquisition,
// alerts, spawn placement, and Wander do not consume this operation.
func (e *Session) InPerceptionCircle(req PerceptionQuery) (bool, error) {
	if e == nil || req.ObjectID == 0 ||
		!zonegeometry.IsFinite(req.TargetPosition) ||
		!zonegeometry.IsFiniteScalar(req.TargetRadius) {
		return false, errors.New("invalid perception query")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	npc, isFound := e.npcs[req.ObjectID]
	if !isFound {
		return false, fmt.Errorf("perceptionActor: %d unavailable", req.ObjectID)
	}
	center := npc.Plan.Position
	var classProfile *game.CampaignNPCProfile
	if npc.Plan.NPCProfile.IsClassKnown {
		classProfile = &npc.Plan.NPCProfile
	}
	if npc.Plan.NPCProfile.IsPlayerPet && req.IsOwnerResolved {
		if !zonegeometry.IsFinite(req.OwnerPosition) {
			return false, errors.New("invalid perception owner")
		}
		if !npc.IsPerceptionOffsetCached {
			if !req.IsLevelResolved {
				// Retain the uninitialized sentinel state. Do not invent a
				// default cached offset after an unsuccessful level lookup.
				return false, errors.New("perception level unavailable")
			}
			if !zonegeometry.IsFiniteScalar(req.LevelYawOverride) {
				return false, errors.New("invalid perception yaw")
			}
			npc.PerceptionOffset = petPerceptionOffset(req.LevelYawOverride)
			npc.IsPerceptionOffsetCached = true
			e.npcs[req.ObjectID] = npc
		}
		center = req.OwnerPosition.Add(npc.PerceptionOffset)
	}
	return IsInPerceptionCircle(classProfile, center, req.TargetPosition, req.TargetRadius), nil
}

// sub_9E8FC0 rotates (0,1,0) by a +Z quaternion, then scales by seven.
// Keep the stored angle, sine/cosine and sub_44E040 arithmetic boundaries;
// evaluating the equivalent -sin(angle)/cos(angle) changes float32 results.
func petPerceptionOffset(yawOverride float32) game.Vec3 {
	angle := float32(5.497787)
	if yawOverride != 0 {
		angle = float32(yawOverride * float32(0.017453292))
	}
	halfAngle := float64(angle) * 0.5
	sine := float32(math.Sin(halfAngle))
	cosine := float32(math.Cos(halfAngle))
	rotationX := float32(-float32(cosine*sine) * 2)
	rotationY := float32(1 - float32(float32(sine*sine)*2))
	return game.Vec3{X: float32(rotationX * 7), Y: float32(rotationY * 7)}
}
