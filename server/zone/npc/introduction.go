package npc

// mapIntroductionProfile retains ordinary aggro reactions but omits arrival
// animations for actors that are already standing in the map.
func mapIntroductionProfile(plan SpawnPlan, profile ActionProfile) ActionProfile {
	if plan.Introduction != SpawnIntroductionDormant || plan.OwnerObjectID != 0 || plan.IsBoss {
		return profile
	}
	switch profile.FirstAggroAnimationName {
	case "gen_aggro_sp_beam_in", "zelem_flying_melee_beamin":
		profile.FirstAggroAnimationName = ""
		profile.FirstAggroDelay = 0
	}
	return profile
}
