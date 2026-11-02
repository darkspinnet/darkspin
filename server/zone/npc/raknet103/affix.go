package raknet103

import (
	"fmt"
	"slices"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func affixAuraMessage(plan zonenpc.SpawnPlan, aura zonenpc.AffixAura) (raknet.ModifierCreatedMessage, error) {
	index, isFound := zonenpc.AffixAuraIndex(aura.ModifierName)
	if !isFound {
		return raknet.ModifierCreatedMessage{}, fmt.Errorf("auraModifier: %q", aura.ModifierName)
	}
	instanceID, err := zoneeffect.NounAuraModifierInstanceID(plan.ObjectID, index)
	if err != nil {
		return raknet.ModifierCreatedMessage{}, fmt.Errorf("auraInstance: %w", err)
	}
	return raknet.ModifierCreatedMessage{
		TargetID: plan.ObjectID, NPCAffixAssetID: 0,
		ModifierGUID: util.HashID(aura.ModifierName), InstanceID: instanceID,
		StartMilliseconds: permanentModifierStartMilliseconds, StackCount: 1,
		DurationMilliseconds: uint32(zonenpc.EliteModifierDuration.Milliseconds()),
	}, nil
}

// AffixChanges publishes unique aura enter/leave and timing changes to every
// observer. Instance handles and attachment slots are stable across rejoin.
func AffixChanges(changes []zonenpc.AffixChange) ([][]byte, error) {
	packets := make([][]byte, 0)
	for _, change := range changes {
		plan := change.Target.Plan
		messages := make([]raknet.ApplicationMessage, 0)
		for _, previous := range change.PreviousAuras {
			if slices.Contains(plan.AffixAuras, previous) {
				continue
			}
			index, isFound := zonenpc.AffixAuraIndex(previous.ModifierName)
			if !isFound {
				return nil, fmt.Errorf("auraPrevious: %q", previous.ModifierName)
			}
			instanceID, err := zoneeffect.NounAuraModifierInstanceID(plan.ObjectID, index)
			if err != nil {
				return nil, fmt.Errorf("auraDeleteInstance: %w", err)
			}
			messages = append(messages, raknet.ModifierDeletedMessage{TargetID: plan.ObjectID, InstanceID: instanceID})
		}
		for _, aura := range plan.AffixAuras {
			if slices.Contains(change.PreviousAuras, aura) {
				continue
			}
			message, err := affixAuraMessage(plan, aura)
			if err != nil {
				return nil, fmt.Errorf("auraCreate: %w", err)
			}
			messages = append(messages, message)
		}
		messages = append(messages, raknet.AttributeDataUpdateMessage{
			ObjectID: plan.ObjectID, Value: affixAttributes(change.Target),
		})
		if zonenpc.PackflyBoostEffectName(change.PreviousHelperCount) != zonenpc.PackflyBoostEffectName(plan.PackflyHelperCount) {
			assetName := zonenpc.PackflyBoostEffectName(plan.PackflyHelperCount)
			messages = append(messages, raknet.AttachedEffectMessage{
				Slot: 19, ObjectID: plan.ObjectID, IsForceAttached: true, IsRemovalRequested: assetName == "",
				Asset: util.HashID(assetName),
			})
		}
		for index, message := range messages {
			packet, err := raknet.MarshalApplication(message)
			if err != nil {
				return nil, fmt.Errorf("affixMarshal[%d]: %w", index, err)
			}
			packets = append(packets, packet)
		}
	}
	return packets, nil
}

func affixAttributes(npc zonenpc.Snapshot) map[uint8]float32 {
	plan := npc.Plan
	physical, energy := plan.AffixDefenseBonuses()
	profile, isFound := zonenpc.ActionProfileForPlan(plan)
	if isFound {
		physical += profile.PassivePhysicalDefense
		if !npc.IsTurtleActive {
			energy += profile.PassiveEnergyDefense
		}
	}
	armor := plan.AffixFlatArmor()
	attributes := map[uint8]float32{
		7: plan.NPCProfile.DodgeRating + physical, 9: plan.NPCProfile.ResistRating + energy,
		10: npc.EffectiveCriticalRating(), 23: zonenpc.EffectiveAttackSpeedBonus(plan),
		24: plan.AffixCooldownReductionBonus, 27: zonenpc.ZelemMeleeAOEResistance(plan), 48: zonenpc.EffectiveMovementSpeedBuff(plan),
		61: 0, 72: 0, 73: 0, 75: 0, 76: 0, 77: 0, 78: 0, 84: 0, 93: 0, 102: armor, 103: armor,
	}
	if plan.HasAffixModifier("Shifted_NPCAffixModifier") {
		attributes[27] = min(attributes[27]+0.75, 1)
	}
	if plan.IsCaptain {
		attributes[93] = 0.5
	}
	if plan.HasAffixModifier("Unstoppable_NPCAffixModifier") {
		for _, attributeID := range []uint8{73, 75, 76, 77, 78} {
			attributes[attributeID] = 1
		}
	}
	if plan.HasAffixModifier("Surefooted_NPCAffixModifier") {
		for _, attributeID := range []uint8{61, 72, 84} {
			attributes[attributeID] = 1
		}
	}
	return attributes
}
