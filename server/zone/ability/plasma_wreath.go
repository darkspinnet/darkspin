package ability

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const (
	PlasmaWreathOrbCount      = uint32(6)
	PlasmaWreathRadius        = float32(5)
	PlasmaWreathCheckInterval = 2 * time.Second
)

func PlasmaWreathDefinition() sim.AbilityDefinition {
	return sim.AbilityDefinition{
		Name: "LightningRogueSupport", Kind: sim.AbilityKindPointBlank,
		Cooldown: 15 * time.Second, Range: PlasmaWreathRadius,
		AnimationName: "el_lightningravager_support",
		ReleaseDelay:  450 * time.Millisecond,
		MinimumDamage: 8, MaximumDamage: 20, DamageCoefficient: 0.05,
		DescriptorMask: 128, DamageType: 3, DamageSource: 1,
		IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true,
		ManaCost: 20, ManaCoefficient: 0.1,
		ImpactEffectName: "lightningbolt_impact.ServerEventDef",
	}
}

// PlasmaWreathRankProfile retains the installed modifier's ranked operands.
type PlasmaWreathRankProfile struct {
	Definition          sim.AbilityDefinition
	AttackSpeedIncrease float32
}

func PlasmaWreathProfileForRank(rank int32) PlasmaWreathRankProfile {
	definition := PlasmaWreathDefinition()
	definition.ShotCount = PlasmaWreathOrbCount
	if rank >= 1 && rank <= 8 && rank%2 == 0 {
		definition.ShotCount = 4
		definition.MinimumDamage = 6
		definition.MaximumDamage = 12
	}
	profile := PlasmaWreathRankProfile{Definition: definition}
	if rank == 7 || rank == 8 {
		profile.Definition.Duration = 10 * time.Second
		profile.AttackSpeedIncrease = 0.25
	}
	return profile
}

func PlanPlasmaWreathHit(
	enemies *zonenpc.Session, sourceObjectID uint32, sourcePosition game.Vec3,
	creature game.GameplayCreature,
) (BasicPlan, error) {
	plan, err := PlanRankedPlasmaWreathHit(enemies, sourceObjectID, sourcePosition, creature, PlasmaWreathProfileForRank(1).Definition)
	if err != nil {
		return BasicPlan{}, fmt.Errorf("wreathPlan: %w", err)
	}
	return plan, nil
}

func PlanRankedPlasmaWreathHit(
	enemies *zonenpc.Session, sourceObjectID uint32, sourcePosition game.Vec3,
	creature game.GameplayCreature, definition sim.AbilityDefinition,
) (BasicPlan, error) {
	targetObjectID := uint32(0)
	if enemies != nil {
		for _, target := range enemies.LiveSnapshots() {
			if !target.IsPublished || target.HitPoint <= 0 ||
				target.Faction != zonenpc.FactionNonPlayerAligned ||
				positionDistance(sourcePosition, target.Plan.Position) > PlasmaWreathRadius {
				continue
			}
			targetObjectID = target.Plan.ObjectID
			break
		}
	}
	if targetObjectID == 0 {
		return BasicPlan{}, nil
	}
	plan, err := PlanProjected(
		enemies, sourceObjectID, targetObjectID, sourcePosition, creature,
		definition, PlasmaWreathRadius,
	)
	if err != nil {
		return BasicPlan{}, fmt.Errorf("wreathTarget: %w", err)
	}
	return plan, nil
}
