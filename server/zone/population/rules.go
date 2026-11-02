package population

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// ApplyRules consumes the mission stream only for accepted ordinary slots.
// Existing actors, including defeated actors, spend their section's agent cap.
func (e *Session) ApplyRules(director game.CampaignDirector, plans []zonenpc.SpawnPlan, snapshots []zonenpc.Snapshot) ([]zonenpc.SpawnPlan, error) {
	rules := director.EnemyPopulationRules.Resolved()
	if rules.AgentSelection == "authored_external" && rules.AgentsPerSection > 0 && len(director.OrdinaryAgentEntries) > 0 {
		countsBySection := make(map[sim.DirectorRouteSection]uint32)
		for _, snapshot := range snapshots {
			for _, entry := range director.OrdinaryAgentEntries {
				if strings.EqualFold(snapshot.Plan.NounName, entry.NounName) {
					countsBySection[e.sectionForLocus(snapshot.Plan.LocusID)]++
					break
				}
			}
		}
		for index := range plans {
			plan := &plans[index]
			section := e.sectionForLocus(plan.LocusID)
			if section == 0 || section == sim.DirectorRouteSectionAny || countsBySection[section] >= rules.AgentsPerSection || !isOrdinaryPolicyPlan(*plan) {
				continue
			}
			selectedIndex, err := e.Random().Index(uint32(len(director.OrdinaryAgentEntries)))
			if err != nil {
				return nil, fmt.Errorf("agentIndex: %w", err)
			}
			entry := director.OrdinaryAgentEntries[selectedIndex]
			plan.NounName, plan.NPCProfile = entry.NounName, entry.NPCProfile
			plan.ActionProfile = zonenpc.ActionProfile{}
			plan.IsActionKnown = false
			countsBySection[section]++
		}
	}
	if !rules.IsAmbientEliteEnabled {
		return plans, nil
	}
	for index := range plans {
		plan := &plans[index]
		if !isOrdinaryPolicyPlan(*plan) {
			continue
		}
		chance, minimum, maximum := ambientTuning(director, *plan)
		chance = min(max(chance*float32(rules.AmbientEliteChancePercent)/100, 0), 1)
		if chance <= 0 || maximum <= 0 {
			continue
		}
		affixes := eligibleAmbientAffixes(director, *plan)
		if len(affixes) == 0 || e.Random().Float64() >= float64(chance) {
			continue
		}
		minimum = max(minimum, 0)
		maximum = max(maximum, minimum)
		count := minimum
		if maximum > minimum {
			draw, err := e.Random().Index(uint32(maximum - minimum + 1))
			if err != nil {
				return nil, fmt.Errorf("eliteCount: %w", err)
			}
			count += int32(draw)
		}
		count = min(count, int32(game.MaxCampaignNPCAffixCount))
		identity := game.CampaignNPCIdentity{IsKnown: true, DisplayName: plan.NounName}
		if authored, isFound := director.NPCIdentitiesByNoun[strings.ToLower(plan.NounName)]; isFound && authored.DisplayName != "" {
			identity.DisplayName = authored.DisplayName
		}
		// Dark Spin compatibility order is authored-name order. Duplicate
		// modifiers and a parent/child pair cannot occupy two affix slots.
		selectedAffixes := make([]game.CampaignEliteAffix, 0, count)
		for _, affix := range affixes {
			isCompatible := true
			for _, selected := range selectedAffixes {
				if affix.ModifierID == selected.ModifierID || affix.ParentName == selected.Name || selected.ParentName == affix.Name || affix.ChildName == selected.Name || selected.ChildName == affix.Name {
					isCompatible = false
					break
				}
			}
			if !isCompatible {
				continue
			}
			slot := len(selectedAffixes)
			if slot >= int(count) {
				break
			}
			identity.NPCAffixNames[slot] = affix.Name
			identity.NPCAffixModifierNames[slot] = affix.ModifierName
			identity.NPCAffixModifierIDs[slot] = affix.ModifierID
			selectedAffixes = append(selectedAffixes, affix)
		}
		if len(selectedAffixes) == 0 || len(selectedAffixes) < int(minimum) {
			continue
		}
		bossIdentity, isValid := zonenpc.BossIdentityFromContent(identity)
		if !isValid {
			return nil, fmt.Errorf("eliteIdentity: invalid %q", plan.NounName)
		}
		plan.IsElite = true
		plan.BossIdentity = bossIdentity
		healthBonus := zonenpc.EliteBonusForType(plan.NPCProfile.NPCType).Health
		if bossIdentity.HasModifier("Tough_NPCAffixModifier") {
			healthBonus += zonenpc.ToughHealthBonusForType(plan.NPCProfile.NPCType)
		}
		plan.NPCProfile.HitPoint *= 1 + healthBonus
	}
	return plans, nil
}

func (e *Session) sectionForLocus(locusID uint32) sim.DirectorRouteSection {
	for _, candidate := range e.candidates {
		if candidate.locusID == locusID {
			return candidate.section
		}
	}
	return 0
}

func isOrdinaryPolicyPlan(plan zonenpc.SpawnPlan) bool {
	return !plan.IsCaptain && !plan.IsBoss && !plan.IsElite && !plan.IsFixture && plan.OwnerObjectID == 0 && !plan.IsEncounterAuxiliary
}

func ambientTuning(director game.CampaignDirector, plan zonenpc.SpawnPlan) (float32, int32, int32) {
	isSpecial, isMinion := false, false
	for _, pool := range director.Pools {
		for _, entry := range pool.Entries {
			if !strings.EqualFold(entry.NounName, plan.NounName) {
				continue
			}
			isSpecial = isSpecial || strings.EqualFold(pool.ConfigKind, "special")
			isMinion = isMinion || strings.EqualFold(pool.ConfigKind, "minion")
		}
	}
	if (!isSpecial && !isMinion) || (isSpecial && director.Difficulty < director.EliteSpecialMinimumStage) || (!isSpecial && director.Difficulty < director.EliteMinionMinimumStage) {
		return 0, 0, 0
	}
	for _, tuning := range director.EliteTunings {
		if tuning.Stage != director.Difficulty {
			continue
		}
		if isSpecial {
			return tuning.SpecialChance, tuning.SpecialMinimumAffixCount, tuning.SpecialMaximumAffixCount
		}
		return tuning.Chance, tuning.MinimumAffixCount, tuning.MaximumAffixCount
	}
	return 0, 0, 0
}

func eligibleAmbientAffixes(director game.CampaignDirector, plan zonenpc.SpawnPlan) []game.CampaignEliteAffix {
	affixes := make([]game.CampaignEliteAffix, 0)
	candidates := director.EliteAffixesByNoun[strings.ToLower(plan.NounName)]
	if len(candidates) == 0 {
		// Reconstructed Dark Spin policy: ordinary classes borrow the shared
		// build-103 captain catalog while retaining every stage interval.
		candidates = director.AmbientEliteAffixes
	}
	selectedNames := make(map[string]bool)
	for _, affix := range candidates {
		if int64(director.Difficulty) < int64(affix.MinimumDifficulty) || int64(director.Difficulty) > int64(affix.MaximumDifficulty) || affix.ModifierID == 0 || strings.Contains(strings.ToLower(affix.Name), "create") || affix.Name == zonenpc.EliteAffixName || math.IsNaN(float64(plan.NPCProfile.HitPoint)) {
			continue
		}
		normalizedName := strings.ToLower(affix.Name)
		if selectedNames[normalizedName] {
			continue
		}
		selectedNames[normalizedName] = true
		affixes = append(affixes, affix)
	}
	slices.SortStableFunc(affixes, func(a, b game.CampaignEliteAffix) int { return strings.Compare(a.Name, b.Name) })
	return affixes
}
