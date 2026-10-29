package game

import "strings"

// CryosGeyserHook accepts the exact imported source contracts, not a noun
// prefix or the presence of a positive health field on scenery.
func CryosGeyserHook(nounName string, profile CampaignNPCProfile) string {
	graph := profile.AIGraph
	if !profile.IsKnown || !profile.IsClassKnown || graph == nil || !graph.IsResolved {
		return ""
	}
	switch strings.ToLower(nounName) {
	case "cryos_ice_crack_blocked.noun":
		if graph.DefinitionID == 2483958835 && graph.DeathAbility != nil && *graph.DeathAbility == "CryosGeyserDeath" {
			return "CryosGeyserDeath"
		}
	case "dest_prefab_cryos_ice_crack1.noun":
		if graph.DefinitionID == 3722958495 && graph.PassiveAbility != nil && *graph.PassiveAbility == "CryosGeyserPassive" {
			return "CryosGeyserPassive"
		}
	case "dest_prefab_cryos_ice_crack2.noun":
		if graph.DefinitionID == 3722958492 && graph.PassiveAbility != nil && *graph.PassiveAbility == "CryosGeyserPassive" {
			return "CryosGeyserPassive"
		}
	case "dest_prefab_cryos2_ice_crack1.noun":
		if graph.DefinitionID == 1321691159 && graph.PassiveAbility != nil && *graph.PassiveAbility == "CryosGeyserPassive" {
			return "CryosGeyserPassive"
		}
	}
	return ""
}
