package npc

import "time"

// These ordinary entrance animations/times come from the indexed client Lua
// FirstAggro scripts, not the attack chosen by a noun's combat profile. Boss
// cinematics and SetAnimationStateToAggro keep their existing dedicated paths.
func authoredInvisibleEntrance(plan SpawnPlan, profile ActionProfile) ActionProfile {
	graph := plan.NPCProfile.AIGraph
	if graph == nil || graph.FirstAggroAbility == nil || plan.IsSecondaryStart ||
		profile.FirstAggroCinematicDuration > 0 || profile.FirstAggroRevealDelay > 0 {
		return profile
	}
	animation := ""
	duration := time.Duration(0)
	switch *graph.FirstAggroAbility {
	case "FirstAggro_BeamIn": // chunk 572
		animation, duration = "gen_aggro_sp_beam_in", 1230*time.Millisecond
	case "FirstAggro_BeamIn_Agent": // chunk 537
		animation, duration = "gen_aggro_agent_beam_in", 2100*time.Millisecond
	case "FirstAggro_BeamIn_Tutorial": // chunk 509
		animation, duration = "character_teleport_in", 1291666985*time.Nanosecond
	case "FirstAggro_CitadelBasicMeleeUnburrow": // chunk 379
		animation, duration = "cry_minn_el_1_burrow_out", 1630*time.Millisecond
	case "FirstAggro_CitadelBossMinion": // chunk 879
		animation, duration = "ctd_boss_tc_minion_drop_in", 900*time.Millisecond
	case "FirstAggro_CryosBoss_Minion": // chunk 838
		animation, duration = "cry_el_boss_minion_spawn", 1800*time.Millisecond
	case "FirstAggro_CyberDropIn": // chunk 871
		animation, duration = "gen_aggro_tc_drop_in", 1500*time.Millisecond
	case "FirstAggro_FlyIn", "FirstAggro_FlyIn_Zlm": // chunks 244, 593
		animation, duration = "gen_aggro_su_fly_in", 1500*time.Millisecond
	case "FirstAggro_ShadowSpawn": // chunk 158
		animation, duration = "gen_aggro_su_voidzone", 1060*time.Millisecond
	case "FirstAggro_ShadowSpawnLeech": // chunk 641
		animation, duration = "nomad_lieu_su_3_aggro", 1060*time.Millisecond
	case "FirstAggro_SpawnEater_Minion": // chunk 547
		animation, duration = "ver_minn_lf_spawneater_minion_spawn", 1630*time.Millisecond
		profile.IsFirstAggroFacingSuppressed = true
	case "FirstAggro_SpreadUnburrow": // chunk 488
		animation, duration = "cry_lieu_el_spread_burrow_out", 1630*time.Millisecond
	case "FirstAggro_Unburrow": // chunk 500; 744 registers a different ability
		animation, duration = "cast_burrow_out", 1900*time.Millisecond
	default:
		return profile
	}
	profile.FirstAggroAnimationName = animation
	profile.FirstAggroDelay = duration
	profile.FirstAggroRevealDelay = 0
	profile.IsFirstAggroDurationKnown = true
	return profile
}
