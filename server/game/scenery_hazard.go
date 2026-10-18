package game

import "strings"

// SceneryHazardAbility identifies the shared Citadel hazards independently of
// combatant class, map or smart-object variant. Bare vents are scenery; the
// prefab and boss variants carry the VentFlameCone passive.
func SceneryHazardAbility(nounName string) string {
	switch strings.ToLower(strings.TrimSpace(nounName)) {
	case "dest_citadel_plasma_pool.noun":
		return "CitadelPlasmaBurn"
	case "dest_citadel_factoryvent.noun", "dest_prefab_citadel_factoryvent.noun",
		"dest_citadel_factoryvent_boss.noun":
		return "VentFlameCone"
	default:
		return ""
	}
}
