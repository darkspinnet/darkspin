package ability

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type TossPlan struct {
	SourceObjectID uint32
	TargetObjectID uint32
	AbilityID      uint32
	Definition     sim.AbilityDefinition
	Cast           sim.TossCast
	LaunchPosition sim.Position
	Destination    sim.Position
	Lob            sim.CrystalLob
	Damage         game.DamageRange
}

func PlanToss(
	enemies *zonenpc.Session, sourceObjectID uint32, targetObjectID uint32,
	sourcePosition game.Vec3, creature game.GameplayCreature,
	definition sim.AbilityDefinition, simulationTime time.Duration,
) (TossPlan, error) {
	if enemies == nil || sourceObjectID == 0 || creature.Noun == 0 ||
		simulationTime < 0 {
		return TossPlan{}, errors.New("invalid toss identity")
	}
	if definition.Name == "" ||
		definition.Kind != sim.AbilityKindToss ||
		definition.Range <= 0 ||
		definition.Toss.ProjectileNoun == "" ||
		definition.Toss.ProjectileEffectName == "" ||
		definition.Toss.ImpactEffectName == "" ||
		definition.Toss.Radius <= 0 {
		return TossPlan{}, errors.New("invalid toss definition")
	}
	if !isFinitePosition(sourcePosition) {
		return TossPlan{}, errors.New("invalid toss source position")
	}
	admissionRange := game.AbilityAdmissionRange(
		definition.Range, creature.RangeIncrease, definition.DescriptorMask,
		definition.IsDescriptorFound,
	)
	if targetObjectID == 0 {
		targetObjectID = NearestTarget(
			enemies, sourceObjectID, sourcePosition, admissionRange,
		)
	}
	target, isFound := enemies.NPC(targetObjectID)
	if !isFound || target.IsDefeated || target.HitPoint <= 0 ||
		target.Faction != zonenpc.FactionNonPlayerAligned {
		return TossPlan{}, fmt.Errorf("targetUnavailable: %d", targetObjectID)
	}
	distance := Distance(sourcePosition, target.Plan.Position)
	if distance > admissionRange {
		return TossPlan{},
			fmt.Errorf("targetRange: got %g, want <= %g", distance, admissionRange)
	}
	projected, err := ProjectTiming(creature, definition)
	if err != nil {
		return TossPlan{}, fmt.Errorf("tossTiming: %w", err)
	}
	cast, err := sim.ResolveTossCast(projected, distance)
	if err != nil {
		return TossPlan{}, fmt.Errorf("tossCast: %w", err)
	}
	damage, err := projectTossDamage(creature, projected, false)
	if err != nil {
		return TossPlan{}, fmt.Errorf("tossDamage: %w", err)
	}
	launchPosition, err := TossLaunchPosition(
		sourcePosition, target.Plan.Position, cast.Offset,
	)
	if err != nil {
		return TossPlan{}, fmt.Errorf("tossLaunch: %w", err)
	}
	// Template 630 reads GetPosition on the target after the windup; the lob
	// receives a fixed ground-origin destination, not a homing center point.
	destination := sim.Position(target.Plan.Position)
	// The projectile thread selects its flight shape from the muzzle-to-goal
	// distance independently of the casting animation's actor-to-target range.
	flightCast, err := sim.ResolveTossCast(projected, Distance(
		game.Vec3(launchPosition), game.Vec3(destination),
	))
	if err != nil {
		return TossPlan{}, fmt.Errorf("flightCast: %w", err)
	}
	lob, err := sim.BuildTossLob(
		simulationTime+projected.HitDelay, launchPosition, destination,
		flightCast.Height, projected.Toss.FlightTime, flightCast.Speed,
		projected.Toss.BounceCount, flightCast.BounceRestitution,
		projected.Toss.IsStrikesGroundOnly,
		projected.Toss.IsStopBounceOnCreatures,
	)
	if err != nil {
		return TossPlan{}, fmt.Errorf("tossLob: %w", err)
	}
	return TossPlan{
		SourceObjectID: sourceObjectID,
		TargetObjectID: targetObjectID,
		AbilityID:      util.HashID(projected.Name),
		Definition:     projected,
		Cast:           cast,
		LaunchPosition: launchPosition,
		Destination:    destination,
		Lob:            lob,
		Damage:         damage,
	}, nil
}

func ResolveTossImpact(
	enemies *zonenpc.Session, plan TossPlan,
	creature game.GameplayCreature, at time.Time,
) (AreaPlan, bool, error) {
	landing, err := resolveTossLanding(enemies, plan)
	if err != nil {
		return AreaPlan{}, false, fmt.Errorf("tossLanding: %w", err)
	}
	isStrongImpact :=
		plan.Definition.Toss.Behavior == sim.TossAbilityBehaviorVoodoo &&
			enemies.HasCurseOrFear(plan.TargetObjectID, at)
	if !isStrongImpact {
		return landing, false, nil
	}
	landing.Damage, err = projectTossDamage(
		creature, plan.Definition, true,
	)
	if err != nil {
		return AreaPlan{}, false, fmt.Errorf("tossStrongDamage: %w", err)
	}
	return landing, true, nil
}

// RefreshTossLaunch samples the destination after windup without changing the
// accepted cast animation, muzzle, damage, or release. Once published, this
// plan remains fixed through landing (template 630's projectile continuation).
func RefreshTossLaunch(plan TossPlan, destination sim.Position, at time.Duration) (TossPlan, error) {
	if !isFinitePosition(game.Vec3(destination)) || at < 0 {
		return TossPlan{}, errors.New("invalid toss launch")
	}
	flightCast, err := sim.ResolveTossCast(plan.Definition, Distance(
		game.Vec3(plan.LaunchPosition), game.Vec3(destination),
	))
	if err != nil {
		return TossPlan{}, fmt.Errorf("launchCast: %w", err)
	}
	lob, err := sim.BuildTossLob(
		at, plan.LaunchPosition, destination,
		flightCast.Height, plan.Definition.Toss.FlightTime, flightCast.Speed,
		plan.Definition.Toss.BounceCount, flightCast.BounceRestitution,
		plan.Definition.Toss.IsStrikesGroundOnly,
		plan.Definition.Toss.IsStopBounceOnCreatures,
	)
	if err != nil {
		return TossPlan{}, fmt.Errorf("launchLob: %w", err)
	}
	plan.Destination = destination
	plan.Lob = lob
	return plan, nil
}

func resolveTossLanding(
	enemies *zonenpc.Session, plan TossPlan,
) (AreaPlan, error) {
	if enemies == nil || plan.SourceObjectID == 0 || plan.AbilityID == 0 ||
		plan.Definition.Kind != sim.AbilityKindToss ||
		plan.Definition.Toss.Radius <= 0 ||
		IsInvalidNumber(plan.Damage.Minimum) || plan.Damage.Minimum <= 0 ||
		IsInvalidNumber(plan.Damage.Maximum) ||
		plan.Damage.Maximum < plan.Damage.Minimum {
		return AreaPlan{}, errors.New("invalid toss landing")
	}
	target := make([]zonenpc.Snapshot, 0)
	center := game.Vec3{
		X: plan.Destination.X,
		Y: plan.Destination.Y,
		Z: plan.Destination.Z,
	}
	for _, enemy := range enemies.LiveSnapshots() {
		if enemy.Faction != zonenpc.FactionNonPlayerAligned ||
			enemy.IsDefeated || enemy.HitPoint <= 0 ||
			Distance(center, enemy.Plan.Position) >
				plan.Definition.Toss.Radius+enemy.Plan.NPCProfile.FootprintRadius {
			continue
		}
		target = append(target, enemy)
	}
	slices.SortFunc(
		target,
		func(first zonenpc.Snapshot, second zonenpc.Snapshot) int {
			firstDistance := Distance(center, first.Plan.Position)
			secondDistance := Distance(center, second.Plan.Position)
			if firstDistance < secondDistance {
				return -1
			}
			if firstDistance > secondDistance {
				return 1
			}
			if first.Plan.ObjectID < second.Plan.ObjectID {
				return -1
			}
			if first.Plan.ObjectID > second.Plan.ObjectID {
				return 1
			}
			return 0
		},
	)
	maximumTargetCount := uint32(1000)
	if plan.Definition.Toss.IsMaximumTargetCountFound {
		maximumTargetCount = plan.Definition.Toss.MaximumTargetCount
	}
	if uint32(len(target)) > maximumTargetCount {
		target = target[:maximumTargetCount]
	}
	return AreaPlan{
		SourceObjectID: plan.SourceObjectID,
		AbilityID:      plan.AbilityID,
		Definition:     plan.Definition,
		Damage:         plan.Damage,
		Center:         center,
		Target:         target,
	}, nil
}

func ShouldDetonate(
	definition sim.AbilityDefinition, elapsed time.Duration, targetCount int,
) (bool, error) {
	if definition.Kind != sim.AbilityKindToss ||
		elapsed < 0 || targetCount < 0 {
		return false, errors.New("invalid toss landing state")
	}
	switch definition.Toss.Behavior {
	case sim.TossAbilityBehaviorVoodoo:
		return true, nil
	case sim.TossAbilityBehaviorTrapper:
		if definition.Toss.GrenadeTimer <= 0 {
			return false, errors.New("invalid toss grenade timer")
		}
		return targetCount > 0 ||
			elapsed >= definition.Toss.GrenadeTimer, nil
	default:
		return false,
			fmt.Errorf("unsupported toss behavior: %s", definition.Toss.Behavior)
	}
}

func projectTossDamage(
	creature game.GameplayCreature, definition sim.AbilityDefinition,
	isCurseOrFear bool,
) (game.DamageRange, error) {
	if definition.Kind != sim.AbilityKindToss {
		return game.DamageRange{}, errors.New("invalid toss damage definition")
	}
	minimumDamage := definition.MinimumDamage
	maximumDamage := definition.MaximumDamage
	if minimumDamage == 0 && maximumDamage == 0 {
		minimumDamage = creature.MinimumWeaponDamage
		maximumDamage = creature.MaximumWeaponDamage
	}
	switch definition.Toss.Behavior {
	case sim.TossAbilityBehaviorTrapper:
		minimumDamage *= definition.Toss.DamageMultiplier
		maximumDamage *= definition.Toss.DamageMultiplier
	case sim.TossAbilityBehaviorVoodoo:
		if isCurseOrFear {
			minimumDamage *= definition.Toss.CurseFearDamageMultiplier
			maximumDamage *= definition.Toss.CurseFearDamageMultiplier
		}
	default:
		return game.DamageRange{},
			fmt.Errorf("unsupported toss damage behavior: %s", definition.Toss.Behavior)
	}
	damage, err := ProjectDamage(
		creature, definition, minimumDamage, maximumDamage,
	)
	if err != nil {
		return game.DamageRange{}, fmt.Errorf("tossDamageProject: %w", err)
	}
	if damage.Minimum <= 0 || damage.Maximum < damage.Minimum {
		return game.DamageRange{}, errors.New("invalid toss projected damage")
	}
	return damage, nil
}

// TossLaunchPosition is the server fallback for the unrecovered local-offset
// axis convention: X is right, Y is forward, and Z is up.
func TossLaunchPosition(
	source game.Vec3, target game.Vec3, offset sim.Position,
) (sim.Position, error) {
	deltaX := float64(target.X - source.X)
	deltaY := float64(target.Y - source.Y)
	length := math.Hypot(deltaX, deltaY)
	if length <= 0 || math.IsNaN(length) || math.IsInf(length, 0) {
		return sim.Position{}, errors.New("invalid toss facing")
	}
	forwardX := deltaX / length
	forwardY := deltaY / length
	rightX := forwardY
	rightY := -forwardX
	return sim.Position{
		X: source.X + float32(rightX)*offset.X +
			float32(forwardX)*offset.Y,
		Y: source.Y + float32(rightY)*offset.X +
			float32(forwardY)*offset.Y,
		Z: source.Z + offset.Z,
	}, nil
}
