package ability

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
)

const (
	PlasmaShockDuration = time.Second
	PlasmaBurnDuration  = 5 * time.Second
	PlasmaBurnTick      = time.Second
)

type PlasmaModifierKind uint8

const (
	PlasmaModifierShock PlasmaModifierKind = iota + 1
	PlasmaModifierBurn
)

type PlasmaModifierPlan struct {
	Kind       PlasmaModifierKind
	ModifierID uint32
	Duration   time.Duration
	Tick       time.Duration
	Damage     game.DamageRange
}

func PlanPlasmaModifier(
	random *sim.SimulatorRandom, policy []sim.MeleeHitPolicy,
	animationIndex int, creature game.GameplayCreature,
) (PlasmaModifierPlan, bool, error) {
	if random == nil {
		return PlasmaModifierPlan{}, false, errors.New("plasma modifier random missing")
	}
	if len(policy) != 2 || animationIndex < 0 {
		return PlasmaModifierPlan{}, false, errors.New("plasma modifier policy invalid")
	}
	selected := policy[animationIndex%len(policy)]
	if selected.ModifierID == 0 || selected.ModifierChance > 100 {
		return PlasmaModifierPlan{}, false, errors.New("plasma modifier entry invalid")
	}
	draw, err := random.Index(100)
	if err != nil {
		return PlasmaModifierPlan{}, false, fmt.Errorf("plasmaModifierDraw: %w", err)
	}
	if draw+1 > selected.ModifierChance {
		return PlasmaModifierPlan{}, false, nil
	}
	switch selected.ModifierID {
	case util.HashID("PlasmaSentinelShock"):
		return PlasmaModifierPlan{
			Kind: PlasmaModifierShock, ModifierID: selected.ModifierID,
			Duration: PlasmaShockDuration,
		}, true, nil
	case util.HashID("PlasmaSentinelBurn"):
		definition := sim.AbilityDefinition{
			Name:                "PlasmaSentinelBurn",
			MinimumDamage:       1,
			MaximumDamage:       1,
			DamageCoefficient:   0.05,
			DescriptorMask:      36,
			DamageType:          3,
			DamageSource:        1,
			IsDescriptorFound:   true,
			IsDamageTypeFound:   true,
			IsDamageSourceFound: true,
		}
		damage, err := ProjectDamage(creature, definition, 1, 1)
		if err != nil {
			return PlasmaModifierPlan{}, false, fmt.Errorf("plasmaBurnDamage: %w", err)
		}
		return PlasmaModifierPlan{
			Kind: PlasmaModifierBurn, ModifierID: selected.ModifierID,
			Duration: PlasmaBurnDuration, Tick: PlasmaBurnTick, Damage: damage,
		}, true, nil
	default:
		return PlasmaModifierPlan{}, false,
			fmt.Errorf("plasma modifier unsupported: %#x", selected.ModifierID)
	}
}
