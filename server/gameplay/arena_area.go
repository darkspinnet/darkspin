package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	geometryraknet "github.com/darkspinnet/darkspin/server/zone/geometry/raknet103"
)

func (e campaignAreaHitStep) impactPresentation(
	plan zoneability.AreaPlan,
) ([][]byte, func(zoneability.AreaResult) ([]byte, error), bool, error) {
	prefix := make([][]byte, 0)
	var effect func(zoneability.AreaResult) ([]byte, error)
	isEffectAfterDamage := e.definition.Kind == sim.AbilityKindPointBlank
	if plan.Definition.Name == "TechRandom" {
		beam, err := e.runtime.zetawattBeamPresentation(e.command.Common.ObjectID, e.sourcePosition, e.creature, plan)
		if err != nil {
			return nil, nil, false, fmt.Errorf("areaBeam: %w", err)
		}
		impact := areaSpecialEffect{assetID: util.HashID(plan.Definition.HitEffectName), isObjectOnly: true}
		return [][]byte{beam}, impact.produce, false, nil
	}
	if e.definition.Kind == sim.AbilityKindCursorArea {
		if plan.Definition.Name == "TCShieldedSentinelSupport" ||
			(plan.Definition.Name == "MissileTempestSupport" && !e.isRetained) {
			muzzle, err := raknet.MarshalApplication(raknet.ObjectPresentationMessage{
				Asset: util.HashID(plan.Definition.MuzzleEffectName), ObjectID: e.command.Common.ObjectID,
			})
			if err != nil {
				return nil, nil, false, fmt.Errorf("areaHitMuzzle: %w", err)
			}
			prefix = append(prefix, muzzle)
		}
		if !e.isEffectPresented {
			effectPacket, effectErr := e.cursorGroundEffect(plan)
			if effectErr != nil {
				return nil, nil, false, fmt.Errorf("areaBasicEffect: %w", effectErr)
			}
			prefix = append(prefix, effectPacket)
		}
		if plan.Definition.ImpactEffectName != "" {
			impact := cursorAreaHitEffect{assetName: plan.Definition.ImpactEffectName}
			effect = impact.marshal
			isEffectAfterDamage = true
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

func (e campaignAreaHitStep) cursorGroundEffect(plan zoneability.AreaPlan) ([]byte, error) {
	position := raknet.Vector3{X: plan.Center.X, Y: plan.Center.Y, Z: plan.Center.Z}
	if plan.Definition.Name != "MissileTempestSupport" {
		packet, err := abilityraknet.CursorAreaImpact(plan.Definition.HitEffectName, position)
		if err != nil {
			return nil, fmt.Errorf("cursorPosition: %w", err)
		}
		return packet, nil
	}
	facing, err := geometryraknet.Forward(e.command.Common.Orientation)
	if err != nil {
		return nil, fmt.Errorf("flakFacing: %w", err)
	}
	packet, err := raknet.MarshalApplication(raknet.PositionedEffectMessage{
		Asset: util.HashID(plan.Definition.HitEffectName), Position: position, Facing: facing,
	})
	if err != nil {
		return nil, fmt.Errorf("flakGround: %w", err)
	}
	return packet, nil
}

type cursorAreaHitEffect struct{ assetName string }

func (e cursorAreaHitEffect) marshal(result zoneability.AreaResult) ([]byte, error) {
	if result.Damage.IsDamageImmune || result.Damage.Damage <= 0 {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectPresentationMessage{
		Asset: util.HashID(e.assetName), ObjectID: result.Damage.ObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("cursorHitEffect: %w", err)
	}
	return packet, nil
}

func (e campaignAreaHitStep) publishArena(
	targets *arenaAbilityTargets, plan zoneability.AreaPlan, results []zoneability.AreaResult,
) ([][]byte, error) {
	var prefix [][]byte
	var effect func(zoneability.AreaResult) ([]byte, error)
	var err error
	isEffectAfterDamage := false
	if plan.Definition.Name != "MissileTempestSupport" {
		prefix, effect, isEffectAfterDamage, err = e.impactPresentation(plan)
		if err != nil {
			return nil, fmt.Errorf("arenaAreaEffect: %w", err)
		}
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
	if plan.Definition.Name == "MissileTempestSupport" {
		// Its slow is requested before TakeDamage, even on defended hits.
		return packets, nil
	}
	statusPackets, statusErr := e.runtime.applyAcceptedHitStatus(e.packet, e.sessionKey, e.generation,
		e.command.Common.ObjectID, e.hitTimestamp, plan, results)
	if statusErr != nil {
		// Damage has committed. Preserve its result if optional presentation fails.
		if e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena accepted-hit status failed source=%d: %v", e.command.Common.ObjectID, statusErr)
		}
	} else {
		packets = append(packets, statusPackets...)
	}
	return packets, nil
}
