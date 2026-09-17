package ability

import (
	"errors"
	"fmt"
	"slices"
	"time"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
)

const AreaRadiusAttribute = 62

// ProjectAreaRadius applies the native AoERadius multiplier at an ability
// boundary whose recovered Lua actually consumes the scaled radius.
func ProjectAreaRadius(creature game.GameplayCreature, baseRadius float32) (float32, error) {
	if baseRadius <= 0 {
		return 0, errors.New("invalid area radius")
	}
	radiusScale := 1 + creature.PartAttribute[AreaRadiusAttribute]
	if radiusScale <= 0 {
		return 0, errors.New("invalid area radius scale")
	}
	return baseRadius * radiusScale, nil
}

func PlanElectronSphereImpact(
	enemies *zonenpc.Session, sourceObjectID uint32, directTargetObjectID uint32,
	impactPosition game.Vec3, creature game.GameplayCreature, plan BasicPlan,
) (AreaPlan, error) {
	if enemies == nil || sourceObjectID == 0 || creature.Noun == 0 ||
		plan.AbilityID == 0 ||
		plan.Definition.Name != "PlasmaRandom_LightningBall" ||
		plan.Definition.Kind != sim.AbilityKindProjectile ||
		plan.Definition.Radius <= 0 || plan.Definition.HitEffectName == "" ||
		plan.Damage.Minimum <= 0 || plan.Damage.Maximum < plan.Damage.Minimum ||
		!isFinitePosition(impactPosition) {
		return AreaPlan{}, errors.New("invalid Electron Sphere impact")
	}
	radius, err := ProjectAreaRadius(creature, plan.Definition.Radius)
	if err != nil {
		return AreaPlan{}, fmt.Errorf("electronImpactRadius: %w", err)
	}
	targets := make([]zonenpc.Snapshot, 0)
	for _, enemy := range enemies.LiveSnapshots() {
		if positionDistance(impactPosition, enemy.Plan.Position) > radius {
			continue
		}
		targets = append(targets, enemy)
	}
	slices.SortFunc(targets, func(left, right zonenpc.Snapshot) int {
		if left.Plan.ObjectID == directTargetObjectID &&
			right.Plan.ObjectID != directTargetObjectID {
			return -1
		}
		if right.Plan.ObjectID == directTargetObjectID &&
			left.Plan.ObjectID != directTargetObjectID {
			return 1
		}
		leftDistance := positionDistance(impactPosition, left.Plan.Position)
		rightDistance := positionDistance(impactPosition, right.Plan.Position)
		if leftDistance < rightDistance {
			return -1
		}
		if leftDistance > rightDistance {
			return 1
		}
		if left.Plan.ObjectID < right.Plan.ObjectID {
			return -1
		}
		if left.Plan.ObjectID > right.Plan.ObjectID {
			return 1
		}
		return 0
	})
	return AreaPlan{
		SourceObjectID: sourceObjectID,
		AbilityID:      plan.AbilityID,
		Definition:     plan.Definition,
		Damage:         plan.Damage,
		Center:         impactPosition,
		Target:         targets,
	}, nil
}

func ElectronSphereSecondaryDelay(
	random *sim.SimulatorRandom, secondary sim.LightningSecondaryAbilityDefinition,
) (time.Duration, error) {
	if random == nil || secondary.BaseDelay <= 0 || secondary.RandomDelay < 0 {
		return 0, errors.New("invalid Electron Sphere secondary delay")
	}
	randomDelay := time.Duration(float64(secondary.RandomDelay) * random.Float64())
	return secondary.BaseDelay + randomDelay, nil
}

func PlanElectronSphereSecondary(
	enemies *zonenpc.Session, sourceObjectID uint32, projectilePosition game.Vec3,
	creature game.GameplayCreature, definition sim.AbilityDefinition,
) (AreaPlan, error) {
	secondary := definition.LightningSecondary
	if enemies == nil || sourceObjectID == 0 || creature.Noun == 0 ||
		definition.Name != "PlasmaRandom_LightningBall" ||
		definition.Kind != sim.AbilityKindProjectile ||
		secondary.MinimumDamage <= 0 ||
		secondary.MaximumDamage < secondary.MinimumDamage ||
		secondary.DamageCoefficient < 0 || secondary.Radius <= 0 ||
		secondary.BaseDelay <= 0 || secondary.RandomDelay < 0 ||
		secondary.Chance < 0 || secondary.Chance > 1 ||
		secondary.MaximumCandidates == 0 ||
		secondary.EffectName == "" || secondary.HitEffectName == "" ||
		!isFinitePosition(projectilePosition) {
		return AreaPlan{}, errors.New("invalid Electron Sphere secondary")
	}
	radius, err := ProjectAreaRadius(creature, secondary.Radius)
	if err != nil {
		return AreaPlan{}, fmt.Errorf("electronSecondaryRadius: %w", err)
	}
	projected := definition
	projected.DamageCoefficient = secondary.DamageCoefficient
	damage, err := ProjectDamage(
		creature, projected, secondary.MinimumDamage, secondary.MaximumDamage,
	)
	if err != nil {
		return AreaPlan{}, fmt.Errorf("electronSecondaryDamage: %w", err)
	}
	targets := make([]zonenpc.Snapshot, 0, secondary.MaximumCandidates)
	for _, enemy := range enemies.LiveSnapshots() {
		if positionDistance(projectilePosition, enemy.Plan.Position) > radius {
			continue
		}
		targets = append(targets, enemy)
	}
	slices.SortFunc(targets, func(left, right zonenpc.Snapshot) int {
		leftDistance := positionDistance(projectilePosition, left.Plan.Position)
		rightDistance := positionDistance(projectilePosition, right.Plan.Position)
		if leftDistance < rightDistance {
			return -1
		}
		if leftDistance > rightDistance {
			return 1
		}
		if left.Plan.ObjectID < right.Plan.ObjectID {
			return -1
		}
		if left.Plan.ObjectID > right.Plan.ObjectID {
			return 1
		}
		return 0
	})
	if len(targets) > int(secondary.MaximumCandidates) {
		targets = targets[:int(secondary.MaximumCandidates)]
	}
	return AreaPlan{
		SourceObjectID: sourceObjectID,
		AbilityID:      util.HashID(definition.Name),
		Definition:     projected,
		Damage:         damage,
		Center:         projectilePosition,
		Target:         targets,
	}, nil
}

func CommitElectronSphereSecondary(
	random *sim.SimulatorRandom, enemies *zonenpc.Session,
	plan AreaPlan, creature game.GameplayCreature,
	difficulty uint32, tuning sim.CriticalTuning,
) ([]AreaResult, error) {
	if random == nil || plan.Definition.Name != "PlasmaRandom_LightningBall" {
		return nil, errors.New("invalid Electron Sphere secondary commit")
	}
	chance := plan.Definition.LightningSecondary.Chance
	results := make([]AreaResult, 0, len(plan.Target))
	for _, target := range plan.Target {
		if float32(random.Float64()) >= chance {
			continue
		}
		targetPlan := plan
		targetPlan.Target = []zonenpc.Snapshot{target}
		targetResults, err := CommitArea(
			random, enemies, targetPlan, creature, difficulty, tuning,
		)
		if err != nil {
			return nil, fmt.Errorf("electronSecondaryTarget[%d]: %w",
				target.Plan.ObjectID, err)
		}
		results = append(results, targetResults...)
	}
	return results, nil
}
