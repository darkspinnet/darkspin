package ability

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// ApplyTimeLapse repeats each nearby enemy's recent damage, capped separately
// for each victim. An undamaged enemy still receives the base energy hit.
func ApplyTimeLapse(plan *AreaPlan, enemies RecentDamageReader, creature game.GameplayCreature, now time.Time) error {
	err := ApplyTimeLapseWithDexterity(plan, enemies, creature, creature.Dexterity, now)
	if err != nil {
		return fmt.Errorf("timeLapsePlan: %w", err)
	}
	return nil
}

// ApplyTimeLapseWithDexterity keeps the accepted damage snapshot separate from
// the live Dexterity operand read after the windup in ability525.
func ApplyTimeLapseWithDexterity(
	plan *AreaPlan, enemies RecentDamageReader, creature game.GameplayCreature,
	dexterity float32, now time.Time,
) error {
	policy := plan.Definition.TimeLapse
	if policy == nil {
		return nil
	}
	cap := policy.DamageCap + policy.CapCoefficient*dexterity
	definition := plan.Definition
	baseScale := 1 + (dexterity-8)*definition.DamageCoefficient
	definition.DamageCoefficient = 0
	plan.TargetDamage = make(map[uint32]game.DamageRange, len(plan.Target))
	plan.TargetEffects = make(map[uint32]string, len(plan.Target))
	targets := make([]zonenpc.Snapshot, 0, len(plan.Target))
	for _, target := range plan.Target {
		if target.Plan.IsFixture {
			continue
		}
		targets = append(targets, target)
		objectID := target.Plan.ObjectID
		recent := enemies.RecentDamage(objectID, now)
		damage := game.DamageRange{
			Minimum: definition.MinimumDamage * baseScale,
			Maximum: definition.MaximumDamage * baseScale,
		}
		effect := policy.SmallHitEffectName
		if recent > 0 {
			damage.Minimum = min(damage.Minimum+recent, cap)
			damage.Maximum = min(damage.Maximum+recent, cap)
			effect = plan.Definition.HitEffectName
		}
		projected, err := ProjectDamage(creature, definition, damage.Minimum, damage.Maximum)
		if err != nil {
			return fmt.Errorf("timeLapseDamage: %w", err)
		}
		plan.TargetDamage[objectID] = projected
		plan.TargetEffects[objectID] = effect
	}
	plan.Target = targets
	return nil
}
