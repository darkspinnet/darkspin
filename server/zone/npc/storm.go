package npc

import "time"

const MerakStormReleaseDelay = 6633334160 * time.Nanosecond

// MerakStormProfile is the independent Apocalypse gambit, not the passive geyser.
// Build-103 chunks 594/324 release the agent before the eight delayed hits.
func MerakStormProfile(plan SpawnPlan) (ActionProfile, bool) {
	if cryosBossRank(plan.NounName) != 3 {
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionRetainedArea, AbilityName: "LightningStorm",
		AnimationName: "cry_el_boss_summon_lightning_storm",
		HitDelay:      8 * time.Second, ReleaseDelay: MerakStormReleaseDelay,
		Cooldown: 20 * time.Second, Range: 10, Radius: 10, Angle: 360,
		MinimumDamage: 4, MaximumDamage: 12,
		DescriptorMask: 1<<3 | 1<<7, DamageType: 3, DamageSource: 1,
		GroundEffectName: "lightning_storm_boss_meteor_shower.ServerEventDef",
		MovementSpeed:    5, NonCombatMovementSpeed: 3,
		IsDamageProfileKnown: true, IsFirstAggroDurationKnown: true,
	}, true
}
