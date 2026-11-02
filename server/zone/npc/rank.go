package npc

import (
	"strings"
	"time"
)

// EffectiveNPCRank follows build 103 GetNPCRank: the class rank is promoted
// only by the Elite.NPCAffix identity, not by an unrelated elite stat modifier.
func EffectiveNPCRank(plan SpawnPlan) int32 {
	rank := plan.NPCProfile.NPCRank
	if rank <= 0 {
		rank = 1
		stem := strings.TrimSuffix(strings.ToLower(plan.NounName), ".noun")
		if strings.HasSuffix(stem, "_3") {
			rank = 3
		} else if strings.HasSuffix(stem, "_2") {
			rank = 2
		}
	}
	if plan.EliteModifierAffixName() == EliteAffixName {
		rank++
	}
	return max(int32(1), min(int32(3), rank))
}

// RankedActionProfile resolves base properties before affix/haste timing.
// Keep the selected rank on the action rather than re-reading a later noun or
// modifier state during projectile, channel, or delayed-hit callbacks.
func RankedActionProfile(profile ActionProfile, plan SpawnPlan) ActionProfile {
	profile = AdditionalKitPassives(profile, plan)
	if effectName := PassiveAuraEffectName(plan); effectName != "" {
		profile.PassiveEffectName = effectName
	}
	if profile.AbilityRank > 0 {
		return profile
	}
	rank := EffectiveNPCRank(plan)
	profile.AbilityRank = rank
	index := rank - 1
	switch profile.AbilityName {
	case "NomadSpecialFour":
		profile.Cooldown = NomadSpecialFourCooldown(rank)
	case "PoisonSpit": // build-103 chunk395
		profile.Range = [...]float32{14, 16, 18}[index]
		profile.Cooldown = [...]time.Duration{3500 * time.Millisecond, 3 * time.Second, 2500 * time.Millisecond}[index]
		profile.ProjectileSpeed = [...]float32{6, 10, 20}[index]
		profile.IsProjectileLeadingTarget = index > 0
	case "StagnantNova": // build-103 chunk532, requested by Burrow981
		profile.Cooldown = [...]time.Duration{4 * time.Second, 3 * time.Second, 2 * time.Second}[index]
	case "ZelemBasicMeleeAttack", "NomadSnipe_Melee":
		profile.Cooldown = [...]time.Duration{1500 * time.Millisecond, 1250 * time.Millisecond, time.Second}[index]
	case "ZelemBasicRanged":
		profile.Cooldown = [...]time.Duration{5 * time.Second, 4 * time.Second, 3 * time.Second}[index]
		profile.Range = [...]float32{12.5, 15, 17.5}[index]
		profile.ProjectileSpeed = [...]float32{8, 10, 12}[index]
	case "BurstShot":
		profile.Range = [...]float32{13, 15, 17}[index]
		profile.ProjectileSpeed = [...]float32{25, 30, 35}[index]
		profile.IsProjectileTrackingBetweenShots = index > 0
	case "ZelemBasicRanged_Blink":
		profile.Cooldown = [...]time.Duration{7 * time.Second, 6 * time.Second, 5 * time.Second}[index]
		profile.HitDelay = [...]time.Duration{3200000047, 2200000047, 1200000047}[index]
		profile.AnimationName = [...]string{"zlm_minn_sp_1_blink_cast3", "zlm_minn_sp_1_blink_cast2", "zlm_minn_sp_1_blink_cast1"}[index]
	case "ZelemHasterAttack":
		profile.Cooldown = [...]time.Duration{3 * time.Second, 2500 * time.Millisecond, 2 * time.Second}[index]
	case "CastZelemHasteBuff":
		profile.Cooldown = [...]time.Duration{4 * time.Second, 3 * time.Second, 2 * time.Second}[index]
	case "NomadWithDronePunch":
		profile.Cooldown = [...]time.Duration{2400 * time.Millisecond, 2 * time.Second, 1600 * time.Millisecond}[index]
	case "NomadSnipe_Slow", "ZelemBasicRangedHomingProjectile":
		profile.Cooldown = [...]time.Duration{4 * time.Second, 3500 * time.Millisecond, 3 * time.Second}[index]
	case "NomadSpecialThree":
		profile.Cooldown = [...]time.Duration{2500 * time.Millisecond, 2 * time.Second, 1500 * time.Millisecond}[index]
	case "CitadelGrenadeRoll":
		profile.Cooldown = [...]time.Duration{4 * time.Second, 3500 * time.Millisecond, 3 * time.Second}[index]
		profile.Range = [...]float32{12, 10, 8}[index]
		profile.ProjectileSpeed = [...]float32{8, 12, 16}[index]
	case "ZelemBasicFlyingMeleeAttack", "TailZap":
		profile.Cooldown = [...]time.Duration{2 * time.Second, 1750 * time.Millisecond, 1500 * time.Millisecond}[index]
	case "ZelemChargeupStandardAttack":
		profile.Cooldown = [...]time.Duration{2 * time.Second, 1500 * time.Millisecond, time.Second}[index]
	case "ZelemChargeupBuildCharge":
		profile.Cooldown = [...]time.Duration{16 * time.Second, 14 * time.Second, 12 * time.Second}[index]
		profile.HitDelay = [...]time.Duration{700 * time.Millisecond, 700 * time.Millisecond, 300 * time.Millisecond}[index]
		profile.ReleaseDelay = [...]time.Duration{3500 * time.Millisecond, 2500 * time.Millisecond, 1500 * time.Millisecond}[index]
		profile.AnimationName = [...]string{"zlm_minn_sp_x4_chargeup_d1", "zlm_minn_sp_x4_chargeup_d2", "zlm_minn_sp_x4_chargeup_d3"}[index]
	case "Resurrect":
		profile.Cooldown = [...]time.Duration{15 * time.Second, 12 * time.Second, 8 * time.Second}[index]
	case "PlungeAttack":
		profile.Cooldown = [...]time.Duration{5 * time.Second, 4500 * time.Millisecond, 4 * time.Second}[index]
	case "Smash":
		profile.Cooldown = [...]time.Duration{1500 * time.Millisecond, 1250 * time.Millisecond, 1250 * time.Millisecond}[index]
	case "ZelemSP1_TeleportGun":
		profile.Cooldown = [...]time.Duration{3500 * time.Millisecond, 3 * time.Second, 2500 * time.Millisecond}[index]
		profile.Range = [...]float32{18, 20, 22}[index]
		profile.ProjectileSpeed = [...]float32{6, 10, 14}[index]
	case "NomadDroneLaser":
		profile.Cooldown = [...]time.Duration{700 * time.Millisecond, 600 * time.Millisecond, 500 * time.Millisecond}[index]
		profile.ProjectileSpeed = [...]float32{25, 30, 35}[index]
	case "ScaldronBasicDoppler_Clone":
		profile.Cooldown = [...]time.Duration{11 * time.Second, 9 * time.Second, 7 * time.Second}[index]
		// Chunk 180 compensates for an already promoted cast rank except at
		// the table cap, then promotes the newly created fake independently.
		childIndex := index
		if plan.IsEliteModifierApplied() && rank < 3 {
			childIndex = max(int32(0), childIndex-1)
		}
		profile.RetainedObjectNoun = [...]string{"ScaldronBasicDopplerFake.Noun", "ScaldronBasicDopplerFake_2.Noun", "ScaldronBasicDopplerFake_3.Noun"}[childIndex]
	case "NomadSpecialOneCleave":
		profile.Cooldown = [...]time.Duration{3 * time.Second, 2500 * time.Millisecond, 2 * time.Second}[index]
	case "NomadSpacetimeAgent_Blink":
		profile.Range = [...]float32{15, 20, 25}[index]
		profile.Cooldown = 300 * time.Second
	case "NocturnaSpecialHauntLockdown":
		profile.Cooldown = [...]time.Duration{12 * time.Second, 8 * time.Second, 4 * time.Second}[index]
	case "ChannelLightningCage":
		profile.Range = [...]float32{5, 6, 7}[index]
	case "FireStream":
		profile.MinimumDamage = [...]float32{5, 8, 10}[index]
		profile.MaximumDamage = [...]float32{10, 16, 20}[index]
	}
	return profile
}
