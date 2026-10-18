package contentsqlite

import (
	"fmt"
	"strings"

	contentsqlite "github.com/darkspinnet/darkspin/content/sqlite"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/util"
)

// campaignNPCIdentityAtStage reconstructs authored captain eligibility from
// cEliteAffix's inclusive minDifficulty/maxDifficulty records. MakeElite is a
// native stub in both clients: this does not recover its random selector, infer
// extra affixes, or traverse parent/child links. Profiles without imported
// bounds retain their explicit list rather than interpreting zero as a range.
func campaignNPCIdentityAtStage(
	profile contentsqlite.NonPlayerNounProfile, affixesByName map[string]contentsqlite.NPCAffix,
	stage uint32,
) (game.CampaignNPCIdentity, error) {
	// Validate every authored mapping before filtering, so a broken catalog row
	// cannot remain undetected until a later campaign difficulty.
	identity, err := campaignNPCIdentity(profile, affixesByName)
	if err != nil {
		return game.CampaignNPCIdentity{}, fmt.Errorf("stageIdentity: %w", err)
	}
	if !profile.AreNPCAffixDifficultiesKnown {
		return identity, nil
	}
	selected := game.CampaignNPCIdentity{DisplayName: identity.DisplayName, IsKnown: identity.IsKnown}
	selectedIndex := 0
	for index, affixName := range identity.NPCAffixNames {
		if affixName == "" {
			continue
		}
		minimum := int64(profile.NPCAffixMinimumDifficulties[index])
		maximum := int64(profile.NPCAffixMaximumDifficulties[index])
		if minimum > maximum {
			return game.CampaignNPCIdentity{}, fmt.Errorf("stageAffixRange[%d]: inverted", index)
		}
		if int64(stage) < minimum || int64(stage) > maximum {
			continue
		}
		selected.NPCAffixNames[selectedIndex] = affixName
		selected.NPCAffixModifierNames[selectedIndex] = identity.NPCAffixModifierNames[index]
		selected.NPCAffixModifierIDs[selectedIndex] = identity.NPCAffixModifierIDs[index]
		selectedIndex++
	}
	return selected, nil
}

func campaignNPCIdentity(
	profile contentsqlite.NonPlayerNounProfile, affixesByName map[string]contentsqlite.NPCAffix,
) (game.CampaignNPCIdentity, error) {
	identity := game.CampaignNPCIdentity{
		DisplayName: profile.DisplayName, NPCAffixNames: profile.NPCAffixNames, IsKnown: true,
	}
	// Apply only the ordered class references. Parent/child links neither remove
	// an explicit reference nor add another modifier to the authored class list.
	for index, affixName := range profile.NPCAffixNames {
		if affixName == "" {
			continue
		}
		affix, isFound := affixesByName[strings.ToLower(affixName)]
		if !isFound {
			return game.CampaignNPCIdentity{}, fmt.Errorf("identityAffix[%d]: missing %q", index, affixName)
		}
		if affix.ModifierHash == 0 {
			return game.CampaignNPCIdentity{}, fmt.Errorf("identityModifier[%d]: missing ID", index)
		}
		identity.NPCAffixModifierIDs[index] = affix.ModifierHash
		if affix.ModifierName == nil {
			continue
		}
		if util.HashID(*affix.ModifierName) != affix.ModifierHash {
			return game.CampaignNPCIdentity{}, fmt.Errorf("identityModifier[%d]: name/ID mismatch", index)
		}
		identity.NPCAffixModifierNames[index] = *affix.ModifierName
	}
	return identity, nil
}
