package ability

import (
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
		MinimumDamage: 8, MaximumDamage: 8, DamageCoefficient: 0.05,
		DescriptorMask: 128, DamageType: 3, DamageSource: 1,
		IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true,
		ManaCost: 20, ManaCoefficient: 0.1,
		ImpactEffectName: "lightningbolt_impact.ServerEventDef",
	}
}

func PlanPlasmaWreathHit(
	enemies *zonenpc.Session, sourceObjectID uint32, sourcePosition game.Vec3,
	creature game.GameplayCreature,
) (BasicPlan, error) {
	targetObjectID := NearestTarget(
		enemies, sourceObjectID, sourcePosition, PlasmaWreathRadius,
	)
	if targetObjectID == 0 {
		return BasicPlan{}, nil
	}
	return PlanProjected(
		enemies, sourceObjectID, targetObjectID, sourcePosition, creature,
		PlasmaWreathDefinition(), PlasmaWreathRadius,
	)
}
