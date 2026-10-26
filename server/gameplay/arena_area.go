package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
)

func (e campaignAreaHitStep) impactPresentation(
	plan zoneability.AreaPlan,
) ([][]byte, func(zoneability.AreaResult) ([]byte, error), bool, error) {
	prefix := make([][]byte, 0)
	var effect func(zoneability.AreaResult) ([]byte, error)
	isEffectAfterDamage := e.definition.Kind == sim.AbilityKindPointBlank
	if e.definition.Kind == sim.AbilityKindCursorArea {
		if !e.isEffectPresented {
			effectPacket, effectErr := abilityraknet.CursorAreaImpact(
				plan.Definition.HitEffectName,
				raknet.Vector3{X: plan.Center.X, Y: plan.Center.Y, Z: plan.Center.Z},
			)
			if effectErr != nil {
				return nil, nil, false, fmt.Errorf("areaBasicEffect: %w", effectErr)
			}
			prefix = append(prefix, effectPacket)
		}
	} else if plan.Definition.TimeLapse != nil {
		effect = marshalTimeLapseImpact
	} else if plan.Definition.ImpactEffectName != "" {
		impact := campaignPointBlankImpact{
			assetName:            plan.Definition.ImpactEffectName,
			largeTargetAssetName: plan.Definition.HitEffectName,
			attackerID:           e.command.Common.ObjectID,
			source: raknet.Vector3{
				X: e.sourcePosition.X, Y: e.sourcePosition.Y, Z: e.sourcePosition.Z,
			},
		}
		if plan.Definition.Name == binarySentinelSupportName {
			// The wave belongs to the caster; only large-target impacts are separate.
			impact.assetName = ""
		}
		effect = impact.marshal
	}

	return prefix, effect, isEffectAfterDamage, nil
}

func (e campaignAreaHitStep) publishArena(
	targets *arenaAbilityTargets, plan zoneability.AreaPlan, results []zoneability.AreaResult,
) ([][]byte, error) {
	prefix, effect, isEffectAfterDamage, err := e.impactPresentation(plan)
	if err != nil {
		return nil, fmt.Errorf("arenaAreaEffect: %w", err)
	}
	packets := prefix
	for _, result := range results {
		var effectPacket []byte
		if effect != nil {
			effectPacket, err = effect(result)
			if err != nil {
				return nil, fmt.Errorf("arenaAreaImpact: %w", err)
			}
		}
		if len(effectPacket) > 0 && !isEffectAfterDamage {
			packets = append(packets, effectPacket)
		}
		hitPackets, hitErr := targets.packets([]zoneability.AreaResult{result})
		if hitErr != nil {
			return nil, fmt.Errorf("arenaAreaHit: %w", hitErr)
		}
		packets = append(packets, hitPackets...)
		if len(effectPacket) > 0 && isEffectAfterDamage {
			packets = append(packets, effectPacket)
		}
	}
	return packets, nil
}
