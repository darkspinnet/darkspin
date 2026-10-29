package npc

import "github.com/darkspinnet/darkspin/server/game"

// NativePassiveAura returns the imported AI-owned passive independently of the
// selected attack. A known graph never borrows another noun's passive.
func NativePassiveAura(plan SpawnPlan) string {
	graph := plan.NPCProfile.AIGraph
	if graph != nil {
		if graph.PassiveAbility == nil {
			return ""
		}
		return *graph.PassiveAbility
	}
	switch nounSpecies(plan.NounName) {
	case "cryosspecialthree":
		return "ResurrectAlliesAuraMod"
	case "nocturnaspecialleech":
		return "NocturnaSpecialLeech_HealAura"
	case "citadelspecialtwo":
		return "CitadelSpecialTwo_SilenceAura"
	}
	return ""
}

func PassiveAuraEffectName(plan SpawnPlan) string {
	switch NativePassiveAura(plan) {
	case "ResurrectAlliesAuraMod":
		return "self_rez_aura_effect.ServerEventDef"
	case "NocturnaSpecialLeech_HealAura":
		return "life_healing_aura_effect.ServerEventDef"
	case "CitadelSpecialTwo_SilenceAura":
		return "cyber_silence_aura.ServerEventDef"
	}
	return ""
}

// Aura template540 grants child modifiers; it does not revoke them on range
// exit. Child57 checks its live initiator's distance11 when resurrection fires;
// child910 heals the live initiator on TookDamage without another range check.
func (e *Session) nativePassiveAurasLocked(target Snapshot, previousAuras []AffixAura) []AffixAura {
	if !isSwiftActorActive(target) || target.Plan.IsFixture || target.Plan.NPCProfile.NounType != game.NounTypeCreature {
		return nil
	}
	auras := make([]AffixAura, 0, 2)
	for _, modifierName := range []string{"ResurrectModifier", "NocturnaSpecialLeech_HealModifier"} {
		if modifierName == "ResurrectModifier" && target.IsAffixResurrectionTriggered {
			continue
		}
		previousSourceID := uint32(0)
		for _, aura := range previousAuras {
			if aura.ModifierName == modifierName {
				previousSourceID = aura.SourceObjectID
			}
		}
		selected := AffixAura{}
		for _, sourceID := range e.objectIDs {
			source := e.npcs[sourceID]
			if sourceID == target.Plan.ObjectID || !isSwiftActorActive(source) || source.Faction != target.Faction {
				continue
			}
			passiveName := NativePassiveAura(source.Plan)
			isResurrection := passiveName == "ResurrectAlliesAuraMod" && modifierName == "ResurrectModifier"
			isLeech := passiveName == "NocturnaSpecialLeech_HealAura" && modifierName == "NocturnaSpecialLeech_HealModifier"
			if !isResurrection && !isLeech {
				continue
			}
			rank := EffectivePassiveRank(source.Plan)
			radius := [...]float32{3, 5, 7}[rank-1]
			if isResurrection {
				radius = [...]float32{8, 12, 16}[rank-1]
			}
			if sourceID != previousSourceID && source.Plan.Position.Sub(target.Plan.Position).Length() > radius {
				continue
			}
			if selected.SourceObjectID == 0 || sourceID == previousSourceID {
				selected = AffixAura{ModifierName: modifierName, SourceObjectID: sourceID, Rank: rank}
			}
			if sourceID == previousSourceID {
				break
			}
		}
		if selected.SourceObjectID != 0 {
			auras = append(auras, selected)
		}
	}
	return auras
}

func hasAuraModifier(auras []AffixAura, modifierName string) bool {
	for _, aura := range auras {
		if aura.ModifierName == modifierName {
			return true
		}
	}
	return false
}
