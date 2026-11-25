package game

import "strings"

// These are Dark Spin's provisional map budgets, not recovered retail counts.
// Keep existing density until each map is tuned independently. Authored spawn
// positions still bound how much of a map's target can actually be admitted.
var campaignPopulationTargets = map[string]uint32{
	"zelems_1": 15, "zelems_2": 15, "zelems_3": 15, "zelems_4": 15,
	"nocturna_1": 15, "nocturna_2": 15, "nocturna_3": 15, "nocturna_4": 15,
	"verdanth_1": 15, "verdanth_2": 15, "verdanth_3": 15, "verdanth_4": 15,
	"cryos_1": 15, "cryos_2": 15, "cryos_3": 15, "cryos_4": 15,
	"infinity_1": 15, "infinity_2": 15, "infinity_3": 15, "infinity_4": 15,
	"scaldron_1": 15, "scaldron_2": 15, "scaldron_3": 15, "scaldron_4": 15,
}

// CampaignPopulationTarget returns the destination map's ordinary-enemy target
// per route section, including when a replay randomizes the destination.
func CampaignPopulationTarget(level string) uint32 {
	target, isFound := campaignPopulationTargets[strings.ToLower(strings.TrimSpace(level))]
	if !isFound {
		// Unlisted scenario maps retain the previous baseline.
		return 15
	}
	return target
}
