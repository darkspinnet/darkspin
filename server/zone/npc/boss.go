package npc

import (
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

const (
	EliteModifierName     = "EliteModifier"
	EliteModifierDuration = 1_000_000 * time.Second
	EliteHealthBonus      = float32(0.75)
	EliteDamageBonus      = float32(0.50)
	EliteBodyScaleBonus   = float32(0.25)
)

// BossIdentityFromContent converts the identity authored in ClassAttributes
// into the runtime modifiers used by zone simulation. Every elite-ranked
// identity receives the shared elite profile in addition to its authored
// affixes.
func BossIdentityFromContent(
	contentIdentity game.CampaignNPCIdentity,
) (BossIdentity, bool) {
	if !contentIdentity.IsKnown || strings.TrimSpace(contentIdentity.DisplayName) == "" {
		return BossIdentity{}, false
	}
	identity := BossIdentity{
		DisplayName: contentIdentity.DisplayName,
		IsKnown:     true,
	}
	modifierIndex := 0
	isAffixGap := false
	for affixIndex, affixName := range contentIdentity.NPCAffixNames {
		if affixName == "" {
			isAffixGap = true
			continue
		}
		if isAffixGap || !strings.HasSuffix(affixName, ".NPCAffix") {
			return BossIdentity{}, false
		}
		assetStem := strings.TrimSuffix(affixName, ".NPCAffix")
		if assetStem == "" {
			return BossIdentity{}, false
		}
		identity.AffixNames[affixIndex] = affixName
		identity.ModifierNames[modifierIndex] = npcAffixModifierName(assetStem)
		modifierIndex++
		if strings.HasPrefix(assetStem, "Aura_") {
			identity.AuraRadius = 12
		}
	}
	identity.ModifierNames[modifierIndex] = EliteModifierName
	return identity, true
}

func npcAffixModifierName(assetStem string) string {
	if strings.EqualFold(assetStem, "Spiky") {
		return "Aura_Spiky_NPCAffixModifier"
	}
	return assetStem + "_NPCAffixModifier"
}

func IsBossIdentityValid(identity BossIdentity) bool {
	if !identity.IsKnown {
		return false
	}
	contentIdentity := game.CampaignNPCIdentity{
		DisplayName:   identity.DisplayName,
		NPCAffixNames: identity.AffixNames,
		IsKnown:       true,
	}
	expected, isExpected := BossIdentityFromContent(contentIdentity)
	return isExpected && identity == expected
}

func (e BossIdentity) HasModifier(modifierName string) bool {
	for _, candidateName := range e.ModifierNames {
		if strings.EqualFold(candidateName, modifierName) {
			return true
		}
	}
	return false
}

func ApplyEliteProfile(profile game.CampaignNPCProfile) game.CampaignNPCProfile {
	profile.HitPoint *= 1 + EliteHealthBonus
	return profile
}
