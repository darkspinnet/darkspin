package npc

import (
	"slices"
	"time"
)

// AffixAura records one supplying source for a UniqueIrreplaceable recipient
// modifier. Proximity refresh replaces the collection without mutating aliases.
type AffixAura struct {
	ModifierName   string
	SourceObjectID uint32
	Rank           int32
}

func (e SpawnPlan) HasAffixModifier(name string) bool {
	if e.BossIdentity.HasModifier(name) {
		return true
	}
	for _, aura := range e.AffixAuras {
		if aura.ModifierName == name {
			return true
		}
	}
	return false
}

func (e Snapshot) HasAffixModifier(name string) bool {
	return e.Plan.HasAffixModifier(name)
}

func (e SpawnPlan) AffixRank(name string) int32 {
	if e.BossIdentity.HasModifier(name) {
		// Native affix application passes rank zero; GetRankedValue clamps it
		// to the first slot. NPC class rank is a separate field.
		return 1
	}
	for _, aura := range e.AffixAuras {
		if aura.ModifierName == name {
			return max(int32(1), min(int32(3), aura.Rank))
		}
	}
	return 1
}

type affixAuraDefinition struct {
	SourceModifierName string
	ModifierName       string
}

// Chunk 283 supplies these unique child modifiers; chunk 43's Persistent aura
// instead supplies ResurrectModifier and refuses already resurrected actors.
var affixAuraDefinitions = [...]affixAuraDefinition{
	{"Aura_Spiky_NPCAffixModifier", "Thorns_NPCAffixModifier"},
	{"Aura_Ghostly_NPCAffixModifier", "Ghostly_NPCAffixModifier"},
	{"Aura_Reflective_NPCAffixModifier", "Reflective_NPCAffixModifier"},
	{"Aura_Shifted_NPCAffixModifier", "Shifted_NPCAffixModifier"},
	{"Aura_Surefooted_NPCAffixModifier", "Surefooted_NPCAffixModifier"},
	{"Aura_Frenzied_NPCAffixModifier", "Frenzied_NPCAffixModifier"},
	{"Aura_Unstoppable_NPCAffixModifier", "Unstoppable_NPCAffixModifier"},
	{"Aura_Persistent_NpcAffix", "ResurrectModifier"},
}

func AffixAuraIndex(name string) (int, bool) {
	for index, definition := range affixAuraDefinitions {
		if definition.ModifierName == name {
			return index, true
		}
	}
	return 0, false
}

type AffixChange struct {
	Target              Snapshot
	PreviousAuras       []AffixAura
	PreviousHelperCount uint32
}

// RefreshAffixEffects re-evaluates friendly proximity effects and cached timing
// attributes. A surviving supplying source retains its unique child instance.
func (e *Session) RefreshAffixEffects(playerCount uint32) []AffixChange {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	at := time.Now()
	if at.Before(e.affixRefreshAt) && e.affixPlayerCount == max(uint32(1), playerCount) {
		return nil
	}
	e.affixRefreshAt = at.Add(200 * time.Millisecond)
	e.affixPlayerCount = max(uint32(1), playerCount)
	sourcesByModifier := make(map[string][]Snapshot, len(affixAuraDefinitions))
	for _, objectID := range e.objectIDs {
		source := e.npcs[objectID]
		if !isSwiftActorActive(source) {
			continue
		}
		for _, definition := range affixAuraDefinitions {
			if source.Plan.BossIdentity.HasModifier(definition.SourceModifierName) {
				sourcesByModifier[definition.SourceModifierName] = append(sourcesByModifier[definition.SourceModifierName], source)
			}
		}
	}
	changes := make([]AffixChange, 0)
	for _, objectID := range e.objectIDs {
		target := e.npcs[objectID]
		previousPlan := target.Plan
		auras := make([]AffixAura, 0)
		if isSwiftActorActive(target) && !target.Plan.IsFixture && target.Plan.NPCProfile.NPCType != 3 {
			for _, definition := range affixAuraDefinitions {
				if target.Plan.BossIdentity.HasModifier(definition.ModifierName) ||
					(definition.ModifierName == "ResurrectModifier" && target.IsAffixResurrectionTriggered) {
					continue
				}
				previousSourceID := uint32(0)
				for _, aura := range previousPlan.AffixAuras {
					if aura.ModifierName == definition.ModifierName {
						previousSourceID = aura.SourceObjectID
					}
				}
				selected := AffixAura{}
				for _, source := range sourcesByModifier[definition.SourceModifierName] {
					sourceID := source.Plan.ObjectID
					if source.Faction != target.Faction {
						continue
					}
					rank := source.Plan.AffixRank(definition.SourceModifierName)
					radius := float32(8 + 4*rank)
					if target.Plan.Position.Sub(source.Plan.Position).Length() > radius {
						continue
					}
					if selected.SourceObjectID == 0 || sourceID == previousSourceID {
						selected = AffixAura{ModifierName: definition.ModifierName, SourceObjectID: sourceID, Rank: rank}
					}
					if sourceID == previousSourceID {
						break
					}
				}
				if selected.SourceObjectID != 0 {
					auras = append(auras, selected)
				}
			}
		}
		target.Plan.AffixAuras = auras
		target.Plan.AffixDifficulty = e.affixDifficulty
		helperCount := uint32(0)
		if isSwiftActorActive(target) && isPackfly(target.Plan.NounName) {
			helperCount = e.sameSpeciesAllyCountLocked(objectID, packflyHelperRadius, packflyMaximumHelperCount)
		}
		target.Plan.PackflyHelperCount = helperCount
		target.Plan.AffixAttackSpeedBonus, target.Plan.AffixCooldownReductionBonus,
			target.Plan.AffixMovementSpeedBuff = AffixTimingBonuses(target.Plan, e.affixPlayerCount, helperCount)
		if slices.Equal(previousPlan.AffixAuras, target.Plan.AffixAuras) &&
			previousPlan.PackflyHelperCount == helperCount &&
			previousPlan.AffixAttackSpeedBonus == target.Plan.AffixAttackSpeedBonus &&
			previousPlan.AffixCooldownReductionBonus == target.Plan.AffixCooldownReductionBonus &&
			previousPlan.AffixMovementSpeedBuff == target.Plan.AffixMovementSpeedBuff {
			continue
		}
		e.npcs[objectID] = target
		changes = append(changes, AffixChange{Target: target, PreviousAuras: previousPlan.AffixAuras,
			PreviousHelperCount: previousPlan.PackflyHelperCount})
	}
	return changes
}

func (e *Session) PrepareAffixPlans(plans []SpawnPlan) []SpawnPlan {
	if e == nil {
		return plans
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	preparedPlans := make([]SpawnPlan, len(plans))
	for index, plan := range plans {
		preparedPlans[index] = e.prepareAffixPlan(plan)
	}
	return preparedPlans
}

func (e *Session) prepareAffixPlan(plan SpawnPlan) SpawnPlan {
	if isPackfly(plan.NounName) && plan.PackflyPassiveRank == 0 {
		plan.PackflyPassiveRank = packflyRank(plan)
	}
	plan.AffixDifficulty = e.affixDifficulty
	plan.AffixAttackSpeedBonus, plan.AffixCooldownReductionBonus, plan.AffixMovementSpeedBuff =
		AffixTimingBonuses(plan, max(uint32(1), e.affixPlayerCount), plan.PackflyHelperCount)
	return plan
}

func (e *Session) ConfigureAffixContext(difficulty, playerCount uint32) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.affixDifficulty = difficulty
	e.affixPlayerCount = max(uint32(1), playerCount)
	e.affixRefreshAt = time.Time{}
}
