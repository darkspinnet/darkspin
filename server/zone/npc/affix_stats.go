package npc

import (
	"math"
	"strings"
)

const (
	toughLieutenantNPCType = uint32(1)
	toughModifierName      = "Tough_NPCAffixModifier"
	accurateModifierName   = "Accurate_NPCAffixModifier"
	armoredModifierName    = "Armored_NPCAffixModifier"
	ghostlyModifierName    = "Ghostly_NPCAffixModifier"
	reflectiveModifierName = "Reflective_NPCAffixModifier"
)

// GlobalDefinitions binds nNPCType.Lieutenant to one. Tough's activation
// multiplies its healthPercent (0.5) by the corresponding Elite HP bonus and
// adds the result as a MaxHealth kPercentChange.
func ToughHealthBonusForType(npcType uint32) float32 {
	if npcType == toughLieutenantNPCType {
		return 0.5 * 0.75
	}
	return 0.5 * 4
}

// Accurate adds 45 to CriticalRating, the native non-player critical input.
func (e Snapshot) EffectiveCriticalRating() float32 {
	rating := e.Plan.NPCProfile.CriticalRating
	if e.HasAffixModifier(accurateModifierName) {
		rating += 45
	}
	return rating
}

func (e SpawnPlan) affixDefenseRating(name string) float32 {
	if !e.HasAffixModifier(name) {
		return 0
	}
	switch e.AffixRank(name) {
	case 1:
		return 250
	case 2:
		return 400
	default:
		return 600
	}
}

func (e SpawnPlan) AffixDefenseBonuses() (physical float32, energy float32) {
	return e.affixDefenseRating(ghostlyModifierName), e.affixDefenseRating(reflectiveModifierName)
}

func (e SpawnPlan) AffixFlatArmor() float32 {
	return e.flatArmor(e.AffixDifficulty)
}

// Armored and Sloth Passive independently add the same amount to physical and
// energy flat damage decrease. Native difficulty groups four minor levels per
// major level; their Lua uses major*10+minor as the exponent.
func (e SpawnPlan) flatArmor(difficulty uint32) float32 {
	if difficulty == 0 {
		return 0
	}
	baseArmor := float32(0)
	if e.HasAffixModifier(armoredModifierName) {
		baseArmor += 4
	}
	if e.NPCProfile.AIGraph != nil && e.NPCProfile.AIGraph.PassiveAbility != nil &&
		strings.EqualFold(strings.TrimSuffix(*e.NPCProfile.AIGraph.PassiveAbility, ".Ability"), "SlothPassive") {
		baseArmor += 2
	}
	if baseArmor == 0 {
		return 0
	}
	majorDifficulty := (difficulty-1)/4 + 1
	minorDifficulty := (difficulty-1)%4 + 1
	exponent := majorDifficulty*10 + minorDifficulty
	return baseArmor * float32(math.Pow(1.034999966621399, float64(exponent)))
}
