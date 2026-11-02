package npc

import (
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

// Build-103 chunk467 is a self-owned movement/line controller, not a melee
// attack. Its zero range must not ask the ordinary first action to chase.
func PlasmaWallProfile() ActionProfile {
	return ActionProfile{Family: ActionMelee, AbilityName: "PlasmaWall",
		IsSelfTargeted: true, MovementSpeed: 5, NonCombatMovementSpeed: 5,
		PassiveEffectName:         "fire_wall_minion_onfire_effect.ServerEventDef",
		IsFirstAggroDurationKnown: true}
}

// Chunk503 stacks one fixed point of energy burn per second, resetting its
// seven-second duration on each application. All three ranked rows are equal.
func PlasmaWallBurnProfile(rank int32) ActionProfile {
	return ActionProfile{AbilityName: "PlasmaWall", AbilityRank: max(int32(1), min(int32(3), rank)),
		ModifierName: "CryosBossAddFire", ModifierDuration: 7 * time.Second,
		ModifierTickDuration: time.Second, ModifierTickDamage: 1,
		ModifierMinimumTickDamage: 1, ModifierMaximumTickDamage: 1,
		ModifierMaximumStack: 10, ModifierDescriptorMask: 1<<2 | 1<<5,
		ModifierDamageType: 3, ModifierDamageSource: 1,
		IsModifierDamageProfileKnown: true, IsRetainedVolumeDamage: true}
}

// Chunk467 rotates the direction away from the target by [0, 2*pi] at five
// units, reducing the maximum angle linearly to zero at fifteen units.
func PlasmaWallDestination(source game.Vec3, target game.Vec3, randomFraction float64) game.Vec3 {
	direction := source.Sub(target)
	distance := direction.Length()
	if distance == 0 {
		return target
	}
	direction = direction.Scale(1 / distance)
	angleLimit := 2 * math.Pi * float64(max(float32(0), min(float32(1), (15-distance)/10)))
	angle := angleLimit * randomFraction
	direction = game.Vec3{X: direction.X*float32(math.Cos(angle)) - direction.Y*float32(math.Sin(angle)),
		Y: direction.X*float32(math.Sin(angle)) + direction.Y*float32(math.Cos(angle)), Z: direction.Z}
	return source.Add(direction.Scale(20))
}
