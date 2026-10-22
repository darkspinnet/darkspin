package npc

const EliteAffixName = "Elite.NPCAffix"

// IsEliteModifierApplied describes the permanent modifier attached by the
// spawn adapter. The same attachment is retained for rejoin and death rewards.
func (e SpawnPlan) IsEliteModifierApplied() bool {
	return e.IsCaptain || e.IsElite || e.IsBoss ||
		e.BossIdentity.HasModifier(EliteModifierName)
}

// EliteModifierAffixName is the affix identity on that permanent modifier.
// Destructors retain its stat bonuses but publish their own nameplate instead.
func (e SpawnPlan) EliteModifierAffixName() string {
	if !e.IsEliteModifierApplied() || e.NPCProfile.NPCType == 2 {
		return ""
	}
	return EliteAffixName
}

// IsEliteRewardEligible follows the active modifier's affix identity, rather
// than treating all NPCs with an elite-looking name or rank as elite rewards.
func (e SpawnPlan) IsEliteRewardEligible() bool {
	if e.EliteModifierAffixName() == EliteAffixName {
		return true
	}
	for index, affixName := range e.BossIdentity.AffixNames {
		if affixName == EliteAffixName && e.BossIdentity.ModifierIDs[index] != 0 {
			return true
		}
	}
	return false
}
