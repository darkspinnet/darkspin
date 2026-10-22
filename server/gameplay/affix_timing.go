package gameplay

import zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"

// Special action selectors replace the already adjusted standard profile.
// Apply the current affix attributes to the replacement exactly once.
func campaignAffixActionProfile(
	plan zonenpc.SpawnPlan, profile zonenpc.ActionProfile, isFound bool,
) (zonenpc.ActionProfile, bool) {
	if !isFound {
		return profile, false
	}
	return zonenpc.ApplyAffixTiming(profile, plan), true
}
