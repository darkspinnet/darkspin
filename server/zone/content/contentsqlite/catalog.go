package contentsqlite

import (
	"context"
	"errors"
	"fmt"
	"time"

	contentstore "github.com/darkspinnet/darkspin/content/sqlite"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonecontent "github.com/darkspinnet/darkspin/server/zone/content"
	zoneinteract "github.com/darkspinnet/darkspin/server/zone/interact"
	zonespawn "github.com/darkspinnet/darkspin/server/zone/spawn"
	zoneteleport "github.com/darkspinnet/darkspin/server/zone/teleport"
	zoneunlock "github.com/darkspinnet/darkspin/server/zone/unlock"
)

const quadraFirstAggroChunkID int64 = 502
const quadraFirstAggroSHA256 = "da18208fc597c67ce3251de30d12ad694ae4d644af0e1cb79b5383cc12ec6d8d"
const firstAggroTemplateModule = "Abilities!template_ability_firstaggro.lua"
const firstAggroTemplateChunkID int64 = 848
const firstAggroTemplateSHA256 = "a799a493587c70e6386be91aaa1dfbdb9cdf4cfd4562f0322c5776046df39671"
const introAbilitySecondChunkID = zoneunlock.AbilitySecondChunkID
const introAbilitySecondSHA256 = zoneunlock.AbilitySecondSHA256
const introAbilitySecondMarkerID uint32 = 1410140146
const introAbilitiesMarkerID uint32 = 32670756
const introAbilitySecondCallback = zoneunlock.AbilitySecondCallback
const introHealthAndPowerMarkerID uint32 = 3802061458
const introHealthAndPowerCallback = "nTutorial_IntroHealthAndPower.main"
const markerWaitPlayerRole sim.Role = "player"
const quadraRole sim.Role = "specialOne"

const introOverdriveMarkerID uint32 = 1359119906
const introOverdriveCallback = "nTutorial_IntroOverdriveActivate.main"
const introOverdriveChunkID int64 = 141
const introOverdriveSHA256 = "909a2ff8a3494f6b9bcfd26461cbe72538d8161778771cc8bc5e11792fcf1ffa"
const introSecondCreatureChunkID = zoneunlock.SecondCreatureChunkID
const introSecondCreatureSHA256 = zoneunlock.SecondCreatureSHA256
const introSecondCreatureMarkerID uint32 = 3912233898
const introSecondCreatureCallback = zoneunlock.SecondCreatureCallback
const invisibleBehaviorChunkID int64 = 865
const invisibleBehaviorSHA256 = "dcdab6384645e166b5dd88e23c15246cf421c72946d78b5eaba686976dd40ecf"
const supportHealerPassiveChunkID int64 = 270
const supportHealerPassiveSHA256 = "0883fc82711ac020a01b5423a5c0e98a8e028ab09fdcf33fe4e65219792275cb"
const lightspeedBasicName = "LightspeedTempestBasic"
const lightspeedChainModule = "Modifiers!modifier_chain_ability_counter.lua"
const lightspeedCounterChunkID int64 = 339
const lightspeedCounterSHA256 = "cdf28a8bdbc32813189acb780079c5aa2704dbc207742b85225b11188f68511d"
const instantCastTemplateModule = "Abilities!template_ability_instantcast.lua"
const instantCastTemplateChunkID int64 = 50
const instantCastTemplateSHA256 = "daf011a759c793297ba89f4eab1bf0b7a7ad140bf16ea0cb9065b8d850aef664"
const crystalPickupChunkID = zoneinteract.CrystalPickupChunkID
const crystalPickupSHA256 = zoneinteract.CrystalPickupSHA256
const defeatAllObjectiveSHA256 = "fda2e11e8da8f72c7415c061ee35b98d62a2d8ebbc71d0ce2ebaaf583a96f70a"
const finishObjectiveSHA256 = "cb9020a40265832803e62418ca1141fff829373d57b2f3ef8b644ee77bdc875c"
const damageObjectiveSHA256 = "8441e30aa93c5e61cb09cb2817bfc40a6a4fca856aea24645c673dd3d6721db4"
const obeliskObjectiveSHA256 = "e168a19cf64bddaff0b483b7d1b2546011d14cc4a72b5528331105c4b2133047"
const hugeDamageObjectiveSHA256 = "0031aa46c8b68375d6b8976a0655a5c41bc8e1dbef70d1bb4703e30fa1877bfc"
const healthObeliskObjectiveSHA256 = "ce065a01ba48775543cbbcb24807465398c2a3a8a1a94af8866c6f2dde99beda"
const enemyRegenObjectiveSHA256 = "c702ffcdae3ee116eb62f71a786b801a0b935cbd9c34813ecf5ab10823738b86"
const playerDrainObjectiveSHA256 = "3f56609d4ff248916f2d4f51ad98b937da7ce2c8c75b9ffad88bace200127673"
const destructibleObjectiveSHA256 = "543949cb5562798bbe07fa0c03fae425867bb22c39fadfb3ca1db537049121c7"
const stayAliveObjectiveSHA256 = "1547e9e5dcda912fc166202042e9d09ac04c54b73fc727e2de37ff7834d8a4ae"
const lootCrystalObjectiveSHA256 = "bb8cf84a351727e30a37596b334850862e9bb040df8cff0137e279585ab9741a"
const noSlowingObjectiveSHA256 = "42f1ae9e55c14a58fe5578400f0505d78723b45f8e05c6cc36e084e1f02aab0a"
const sameTypeObjectiveSHA256 = "8f15ed5adb0e0de1becb91218d36b19f515224eebe9e5dc8fbf2c971564ecec0"
const cryosLevelName = game.TutorialDirectorLevel
const build103CrystalDefinitionCount = 192
const build103CrystalLevelOffsetCount = 1

var luaJobPreloadedModules = []string{
	"Lua!GlobalDefinitions.lua",
	"Lua!TargetUtils.lua",
	"Lua!Vector.lua",
}

var nounPhysicsAssetNames = []string{
	"TutorialBasicPoison.Noun",
	"TutorialBasicDiseased.Noun",
	"TutorialBasicRanged.Noun",
	"TutorialSloth.Noun",
	"TutorialSpecialOne.Noun",
	"PC_EL_Rogue.Noun",
	"PC_LF_Mage.Noun",
	"HelperMelee.Noun",
	"Ability_Fireball.Noun",
	"HealthOrb.Noun",
	"manaorb.Noun",
	"HealthOrbPlaced.Noun",
	"ManaOrbPlaced.Noun",
}

// recoveredHeroAbilityDefinitions contains rank-one contracts whose root Lua
// registration is blocked by an absent package alias or by a specialized shape
// that the constrained compiler cannot yet project. Every operand here is
// independently recovered from build-103 content; unresolved modifier effects
// remain intentionally absent or neutral.
func recoveredHeroAbilityDefinitions() map[string]sim.AbilityDefinition {
	definition := map[string]sim.AbilityDefinition{
		"LightningRogueSupport": zoneability.PlasmaWreathDefinition(),
		"CastEnrage": {
			Name: "CastEnrage", Kind: sim.AbilityKindTargetedAOE,
			Cooldown: 10 * time.Second, Range: 35,
			AnimationName: "lf_random_04", OutAnimationName: "lf_random_04_self",
			HitDelay: 333333340 * time.Nanosecond, ReleaseDelay: 600 * time.Millisecond,
			ManaCost: 14, ManaCoefficient: 0.07, Radius: 10,
			MuzzleEffectName: "bio_cast_enrage.ServerEventDef",
			HitEffectName:    "lf_enrage_cast.ServerEventDef",
			RootModifierID:   util.HashID("Enrage"),
		},
		"Ghostform": {
			Name: "Ghostform", Kind: sim.AbilityKindPointBlank,
			Cooldown: 24 * time.Second, Duration: 6 * time.Second,
			AnimationName: "su_ghostSentinel_support",
			HitDelay:      200 * time.Millisecond, ReleaseDelay: 400 * time.Millisecond,
			ManaCost: 12, ManaCoefficient: 0.06,
			MuzzleEffectName: "Ghostform_Enter.ServerEventDef",
			HitEffectName:    "shadow_ghostform_shader_effect.ServerEventDef",
			ImpactEffectName: zoneability.GhostFormPassthroughEffect,
			RootModifierID:   util.HashID("GhostformModifier"),
		},
		"EnergySentinelActive": {
			Name: "EnergySentinelActive", Kind: sim.AbilityKindMelee,
			Cooldown: 8 * time.Second, Range: 5, Radius: 5,
			AnimationName: "energysentinel_active",
			HitDelay:      130 * time.Millisecond, ReleaseDelay: 900 * time.Millisecond,
			MinimumDamage: 14, MaximumDamage: 18, DamageCoefficient: 0.05,
			ManaCost: 16, ManaCoefficient: 0.08,
			DescriptorMask: 129, DamageType: 0, DamageSource: 1,
			IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true,
			HitEffectName: "cyber_energy_arcweld_hit.ServerEventDef",
		},
		"EnergySentinelSupport": {
			Name: "EnergySentinelSupport", Kind: sim.AbilityKindMelee,
			Cooldown: 6 * time.Second, Range: 4,
			AnimationName: "energysentinel_support",
			HitDelay:      260 * time.Millisecond, ReleaseDelay: 800 * time.Millisecond,
			MinimumDamage: 10, MaximumDamage: 16, DamageCoefficient: 0.05,
			ManaCost: 16, ManaCoefficient: 0.08,
			DescriptorMask: 136, DamageType: 4, DamageSource: 1,
			IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true,
			HitEffectName: "cyber_cleave_hit.ServerEventDef", Radius: 6,
			RootModifierID: util.HashID("EnergySentinelStun"),
		},
		"BinarySentinelActive": {
			Name: "BinarySentinelActive", Kind: sim.AbilityKindCone,
			Cooldown: 6 * time.Second, Radius: 20, Angle: 90,
			AnimationName: "cast_binarysentinelactive",
			MinimumDamage: 20, MaximumDamage: 20, DamageCoefficient: 0.05,
			ManaCost: 14, ManaCoefficient: 0.07, MinimumDamagePercent: 0.25,
			RootModifierID: util.HashID("BinarySentinelPullModifier"),
		},
		"BinarySentinelSupport": {
			Name: "BinarySentinelSupport", Kind: sim.AbilityKindCone,
			Cooldown: 10 * time.Second, Radius: 10, Angle: 150,
			AnimationName: "cast_binarysentinelsupport",
			HitDelay:      230 * time.Millisecond, ReleaseDelay: time.Second,
			MinimumDamage: 30, MaximumDamage: 30, DamageCoefficient: 0.05,
			ManaCost: 18, ManaCoefficient: 0.09,
			MinimumDamagePercent: 0.5,
			DescriptorMask:       136, DamageType: 1, DamageSource: 1,
			IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true,
			ImpactEffectName: "spacetime_push_effect.ServerEventDef",
			HitEffectName:    "spacetime_knockback_vsLarge_effect.ServerEventDef",
		},
		"ArborealMight": {
			Name: "ArborealMight", Kind: sim.AbilityKindModifier,
			Cooldown:      18 * time.Second,
			AnimationName: "cast_arborealmight",
			HitDelay:      360 * time.Millisecond, ReleaseDelay: time.Second,
			Duration: 15 * time.Second,
			ManaCost: 8, ManaCoefficient: 0.04,
			RootModifierID:       0xa1935855,
			ActivationEffectName: "Arboreal_Might_Activate.ServerEventDef",
		},
		"FireTempestActive": {
			Name: "FireTempestActive", Kind: sim.AbilityKindSummonBuff,
			Cooldown:      8 * time.Second,
			AnimationName: "cast_firetempestactive",
			HitDelay:      300 * time.Millisecond, ReleaseDelay: 1300 * time.Millisecond,
			ManaCost:               20,
			ManaCoefficient:        0.10,
			Duration:               300 * time.Second,
			TickDuration:           time.Second,
			Radius:                 5,
			PlacementDistance:      5,
			MinimumDamage:          5,
			MaximumDamage:          6,
			DamageCoefficient:      0.05,
			DescriptorMask:         4104,
			DamageType:             1,
			DamageSource:           0,
			IsDescriptorFound:      true,
			IsDamageTypeFound:      true,
			IsDamageSourceFound:    true,
			SpawnNoun:              "RangedElementalPet.Noun",
			MuzzleEffectName:       "summoning_fire_elemental.ServerEventDef",
			ActivationEffectName:   "fire_elemental_enrage.ServerEventDef",
			ImpactEffectName:       "lightningbolt_impact.ServerEventDef",
			HitEffectName:          "fire_hands.ServerEventDef",
			OutAnimationName:       "firetempest_pet_beamout",
			RootModifierID:         0x1f5c7d49,
			SecondaryModifierID:    0x7be66b8d,
			SecondaryAnimationName: "cast_firetempest_enrage",
		},
		"GravityStorm": {
			Name: "GravityStorm", Kind: sim.AbilityKindChannelArea,
			Cooldown: 24 * time.Second, Range: 50, Radius: 5,
			AnimationName:          "cast_gravitystorm",
			SecondaryAnimationName: "slam_gravitystorm",
			HitDelay:               333333 * time.Microsecond,
			ReleaseDelay:           3533333 * time.Microsecond,
			ManaCost:               24, ManaCoefficient: 0.12,
			MinimumDamage: 16, MaximumDamage: 24, DamageCoefficient: 0.05,
			DescriptorMask: 8264, DamageType: 2, DamageSource: 0,
			IsDescriptorFound: true, IsDamageTypeFound: true,
			IsDamageSourceFound:  true,
			Duration:             2 * time.Second,
			TickDuration:         466667 * time.Microsecond,
			FinalWaitDelay:       1333333 * time.Microsecond,
			SpawnNoun:            "GravityStorm.Noun",
			ActivationEffectName: "spacetime_aoe_levitateSlam.ServerEventDef",
			RootModifierID:       util.HashID("GravityStormModifier"),
			SecondaryModifierID:  util.HashID("GravityStormEffectModifier"),
		},
		"FieldMedicActive": {
			Name: "FieldMedicActive", Kind: sim.AbilityKindModifierArea,
			Cooldown: 12 * time.Second, Radius: 10,
			AnimationName: "cast_fieldmedicactive",
			HitDelay:      230 * time.Millisecond, ReleaseDelay: 750 * time.Millisecond,
			ManaCost: 16, ManaCoefficient: 0.08,
			TimeToDestroyBuffs: 30 * time.Second,
			AllyEffectName:     "effect_syndromeshift_allyhit.ServerEventDef",
			EnemyEffectName:    "effect_syndromeshift_enemyhit.ServerEventDef",
		},
		"PlasmaSentinelActive": {
			Name: "PlasmaSentinelActive", Kind: sim.AbilityKindReactiveSummon,
			Cooldown: 30 * time.Second, ManaCost: 20, ManaCoefficient: 0.10,
			Duration: 10 * time.Second, SpawnNoun: "PlasmaSentinelPet.Noun",
			PlacementDistance: 5, RootModifierID: 0x95dcd059,
			SecondaryModifierID:  0x7be66b8d,
			ActivationEffectName: "el_plasmasentinel_active_pet_shield.ServerEventDef",
			HitEffectName:        "plasmasentinel_got_hit_rocks.ServerEventDef",
		},
		"QuantumState": {
			Name: "QuantumState", Kind: sim.AbilityKindModifier,
			Cooldown: 20 * time.Second, ManaCost: 16, ManaCoefficient: 0.08,
			Duration:             8 * time.Second,
			AnimationName:        "sp_quantumRavager_support",
			HitDelay:             200 * time.Millisecond,
			ReleaseDelay:         500 * time.Millisecond,
			RootModifierID:       util.HashID("QuantumStateBuff"),
			ActivationEffectName: "spacetime_quantumform_shader_effect.ServerEventDef",
		},
		"QuantumBlink": {
			Name: "QuantumBlink", Kind: sim.AbilityKindQuantumBlink,
			Cooldown: 12 * time.Second, Range: 35, Radius: 10,
			AnimationName: "sp_quantumRavager_active_start",
			AnimationNames: []string{
				"sp_quantumRavager_active_pose_1",
				"sp_quantumRavager_active_pose_2",
				"sp_quantumRavager_active_pose_3",
				"sp_quantumRavager_active_pose_4",
			},
			SecondaryAnimationName: "sp_quantumRavager_active_end",
			HitDelay:               100 * time.Millisecond, ReleaseDelay: 2100 * time.Millisecond,
			ManaCost: 18, ManaCoefficient: 0.09,
			MinimumDamage: 4, MaximumDamage: 16, DamageCoefficient: 0.05,
			DescriptorMask: 65, DamageType: 2, DamageSource: 0,
			IsDescriptorFound: true, IsDamageTypeFound: true,
			IsDamageSourceFound: true,
			ShotCount:           5, TickDuration: 200 * time.Millisecond,
			FinalWaitDelay: time.Second,
			HitEffectName:  "spacetime_large_hit_effect.ServerEventDef",
		},
		"SummonBeast": {
			Name: "SummonBeast", Kind: sim.AbilityKindSummonBuff,
			Cooldown: 12 * time.Second, Range: 50,
			AnimationName: "beastsentinel_support",
			ReleaseDelay:  750 * time.Millisecond,
			ManaCost:      28, ManaCoefficient: 0.14,
			SpawnNoun:            "BeastSentinelPet.Noun",
			PlacementDistance:    4,
			ActivationEffectName: "beastSentinel_summon_burrowIn.ServerEventDef",
		},
		"SummonSprite": {
			Name: "SummonSprite", Kind: sim.AbilityKindSummonBuff,
			Cooldown: 30 * time.Second, Range: 50,
			AnimationName: "lf_random_01", ReleaseDelay: 900 * time.Millisecond,
			ManaCost: 24, ManaCoefficient: 0.12,
			SpawnNoun: "SpritePet.Noun", PlacementDistance: 4,
			ActivationEffectName: "lf_pocketHealer_spawnIn.ServerEventDef",
			MuzzleEffectName:     "Pocket_Healer_muzzle.ServerEventDef",
			ImpactEffectName:     "lf_pocketHealer_spawnOut.ServerEventDef",
			HitEffectName:        "lf_pocketHealer_trail.ServerEventDef",
			HealEffectName:       "Pocket_Healer_muzzle.ServerEventDef",
			Duration:             30 * time.Second,
		},
		"LightningTempest_Active": {
			Name: "LightningTempest_Active", Kind: sim.AbilityKindProjectileBurst,
			Cooldown: 30 * time.Second, Range: 50, Distance: 18,
			Speed: 30, Radius: 1.5,
			AnimationName: "el_lightningtempest_active",
			ReleaseDelay:  2 * time.Second,
			MinimumDamage: 4, MaximumDamage: 10, DamageCoefficient: 0.05,
			ManaCost: 20, ManaCoefficient: 0.1,
			ProjectileNoun:   "Ability_LightningBall.Noun",
			ImpactEffectName: "lightning_tempest_active.ServerEventDef",
			ShotCount:        30, FiringRate: 265 * time.Millisecond,
			FiringRateRandomness: 100 * time.Millisecond,
			ShotHitChance:        0.3,
			BurstTargeting:       sim.ProjectileBurstTargetingRetargetChannel,
		},
		"MissileTempestActive": {
			Name: "MissileTempestActive", Kind: sim.AbilityKindProjectileBurst,
			Cooldown: 16 * time.Second, Range: 40, Distance: 16,
			Speed: 20, Radius: 2,
			AnimationName: "missiletempest_active_cast",
			MinimumDamage: 7, MaximumDamage: 11, DamageCoefficient: 0.05,
			ManaCost: 30, ManaCoefficient: 0.15,
			ProjectileNoun:   "Ability_Fireball.Noun",
			TrailEffectName:  "cyber_missile_barrage_projectile.ServerEventDef",
			ImpactEffectName: "cyber_missile_barrage_hit.ServerEventDef",
			ShotCount:        15, FiringRate: 300 * time.Millisecond,
			IsAlwaysUseCursorPosition: true,
			BurstTargeting:            sim.ProjectileBurstTargetingCursorArea,
		},
		"FireRavagerSupport": {
			Name: "FireRavagerSupport", Kind: sim.AbilityKindProjectileBurst,
			Cooldown: 3 * time.Second, Range: 20, Distance: 21,
			Speed: 40, Radius: 1.5,
			AnimationName: "el_fireravager_support_warmup",
			ReleaseDelay:  950 * time.Millisecond,
			MinimumDamage: 10, MaximumDamage: 16, DamageCoefficient: 0.05,
			ManaCost: 8, ManaCoefficient: 0.04,
			DescriptorMask: 72, DamageType: 3, DamageSource: 0,
			IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true,
			ProjectileNoun:       "Ability_Fireball.Noun",
			TrailEffectName:      "el_fireravager_support_projectile.ServerEventDef",
			ImpactEffectName:     "el_fireravager_support_impact.ServerEventDef",
			MissEffectName:       "ineffective_common_small.ServerEventDef",
			MuzzleEffectName:     "el_fireravager_support_muzzle.ServerEventDef",
			RootModifierID:       util.HashID("FireRavagerEnergyVulnerabilityModifier"),
			StatusDuration:       2 * time.Second,
			StatusEnergyIncrease: 0.5,
			ShotCount:            2,
			HitDelays: []time.Duration{
				200 * time.Millisecond,
				500 * time.Millisecond,
			},
			BurstTargeting: sim.ProjectileBurstTargetingRetargetChannel,
		},
		"PlasmaRandom": {
			Name: "PlasmaRandom", Kind: sim.AbilityKindCursorArea,
			Cooldown: 10 * time.Second, Range: 35, Radius: 3,
			AnimationName: "el_random01_meteor",
			HitDelay:      600 * time.Millisecond, ReleaseDelay: 600 * time.Millisecond,
			MinimumDamage: 12, MaximumDamage: 18, DamageCoefficient: 0.05,
			ManaCost: 16, ManaCoefficient: 0.08,
			DescriptorMask: 72, DamageType: 3, DamageSource: 0,
			IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true,
			IsAlwaysUseCursorPosition: true,
			MuzzleEffectName:          "Meteor_Strike_muzzle.ServerEventDef",
			HitEffectName:             "fire_summon_effect.ServerEventDef",
			RootModifierID:            util.HashID("PlasmaRandomModifier"),
			StatusKind:                sim.AbilityStatusKindStun,
			StatusDuration:            3 * time.Second,
		},
		"ClaymoreTrap": {
			Name: "ClaymoreTrap", Kind: sim.AbilityKindTrap,
			Cooldown: 12 * time.Second, Range: 2.5, Radius: 4,
			HitDelay: 200 * time.Millisecond, ReleaseDelay: 700 * time.Millisecond,
			Duration: 15 * time.Second, TickDuration: 200 * time.Millisecond,
			MinimumDamage: 15, MaximumDamage: 21, DamageCoefficient: 0.05,
			ManaCost: 12, ManaCoefficient: 0.06,
			DescriptorMask: 136, DamageType: 0, DamageSource: 1,
			IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true,
			SpawnNoun:           "ClaymoreTrap.Noun",
			RootModifierID:      util.HashID("ClaymoreTrap_Daze"),
			StatusKind:          sim.AbilityStatusKindSlow,
			StatusDuration:      6 * time.Second,
			StatusMovementScale: 0.5,
			StatusAttackScale:   1,
		},
		"TurretTrap": {
			Name: "TurretTrap", Kind: sim.AbilityKindTrap,
			Cooldown: 12 * time.Second, Range: 4,
			AnimationName: "cyber_trapper_autoturret",
			HitDelay:      300 * time.Millisecond,
			ReleaseDelay:  700 * time.Millisecond,
			Duration:      10 * time.Second,
			MinimumDamage: 3, MaximumDamage: 5, DamageCoefficient: 0.05,
			ManaCost: 18, ManaCoefficient: 0.09,
			DescriptorMask: 136, DamageType: 0, DamageSource: 1,
			IsDescriptorFound: true, IsDamageTypeFound: true,
			IsDamageSourceFound: true,
			SpawnNoun:           "TurretTrap.Noun",
			HitEffectName:       "cyber_trapper_trapBroken.ServerEventDef",
			ImpactEffectName:    "cyber_trapper_autoturret_repack.ServerEventDef",
		},
		"PipeBomb": {
			Name: "PipeBomb", Kind: sim.AbilityKindTrap,
			Range: 4, Radius: 8,
			AnimationName: "trapper_active_support",
			HitDelay:      300 * time.Millisecond,
			ReleaseDelay:  600 * time.Millisecond,
			Duration:      3 * time.Second,
			TickDuration:  300 * time.Millisecond,
			MinimumDamage: 9, MaximumDamage: 15, DamageCoefficient: 0.05,
			DescriptorMask: 136, DamageType: 0, DamageSource: 1,
			IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true,
			SpawnNoun:            "PipeBomb.Noun",
			IsForwardPlacement:   true,
			PlacementDistance:    3.5,
			ActivationEffectName: "cyber_trapper_placeTauntBomb.ServerEventDef",
			ImpactEffectName:     "cyber_trapper_tauntBombExplosion.ServerEventDef",
			HitEffectName:        "cyber_common_hit_bomb.ServerEventDef",
			RootModifierID:       util.HashID("PipeBombStunModifier"),
			StatusKind:           sim.AbilityStatusKindStun,
			StatusDuration:       2 * time.Second,
		},
		"NecroRandom": {
			Name: "NecroRandom", Kind: sim.AbilityKindChannelDrain,
			Cooldown: 12 * time.Second, Range: 12,
			AnimationName: "su_random01_loop", OutAnimationName: "su_random01_end",
			HitDelay:             300 * time.Millisecond,
			ReleaseDelay:         7066667 * time.Microsecond,
			MinimumDamagePerTick: 10, MaximumDamagePerTick: 10,
			MinimumHealingPerTick: 10, MaximumHealingPerTick: 10,
			DamageCoefficient: 0.05, HealingCoefficient: 0.05,
			ManaCost: 20, ManaCoefficient: 0.10,
			DescriptorMask: 148, DamageType: 2, DamageSource: 1,
			IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true,
			TickDuration: time.Second, NumberOfTicks: 6,
			StatusDamageReduction: 0.50,
			RootModifierID:        util.HashID("NecroRandomModifier"),
			MuzzleEffectName:      "shadow_random01_beam_effect.ServerEventDef",
			HitEffectName:         "shadow_random01_impact_sparks_effect.ServerEventDef",
		},
		"CastSoulLink": {
			Name: "CastSoulLink", Kind: sim.AbilityKindModifier,
			Cooldown:      24 * time.Second,
			AnimationName: "su_random02",
			HitDelay:      233333330 * time.Nanosecond,
			ReleaseDelay:  1300 * time.Millisecond,
			Duration:      12 * time.Second,
			ManaCost:      16, ManaCoefficient: 0.08,
			RootModifierID:       util.HashID("SoulLink"),
			ActivationEffectName: "soul_link_effect.ServerEventDef",
		},
		"RootingPlague": {
			Name: "RootingPlague", Kind: sim.AbilityKindInfection,
			Cooldown: 16 * time.Second, Range: 30, Radius: 5,
			AnimationName: "lf_random_02",
			ReleaseDelay:  1200 * time.Millisecond,
			Duration:      8 * time.Second,
			TickDuration:  time.Second, NumberOfTicks: 8,
			MinimumDamagePerTick: 4, MaximumDamagePerTick: 4,
			SecondaryMinimumDamage: 3, SecondaryMaximumDamage: 3,
			DamageCoefficient: 0.05,
			ManaCost:          24, ManaCoefficient: 0.12,
			DescriptorMask: 164, DamageType: 2, DamageSource: 1,
			IsDescriptorFound: true, IsDamageTypeFound: true,
			IsDamageSourceFound: true,
			RootModifierID:      0x0cf6e44d,
			SecondaryModifierID: util.HashID("BioEntangleModifier"),
			SpreadModifierID:    0x9f694998,
			MuzzleEffectName:    "Virulent_Vines_muzzle.ServerEventDef",
		},
		"Sporogenesis": {
			Name: "Sporogenesis", Kind: sim.AbilityKindHealingTicks,
			Cooldown: 8 * time.Second, Radius: 8,
			AnimationName: "lf_shroomtempest_active",
			HitDelay:      100 * time.Millisecond,
			ReleaseDelay:  700 * time.Millisecond,
			Duration:      4100 * time.Millisecond,
			TickDuration:  time.Second, NumberOfTicks: 5,
			MinimumHealingPerTick: 6, MaximumHealingPerTick: 6,
			HealingCoefficient: 0.05,
			ManaCost:           25, ManaCoefficient: 0.125,
			DescriptorMask: 4112, IsDescriptorFound: true,
			RootModifierID: util.HashID("SporogenesisHot"),
			HitEffectName:  "life_shroomtempest_sporogenesis_hit_friend.ServerEventDef",
			HealEffectName: "Shroom_Cloud.ServerEventDef",
		},
		"FieldMedicSupport": {
			Name: "FieldMedicSupport", Kind: sim.AbilityKindHealingTicks,
			Cooldown: 5 * time.Second, Range: 10, IsTargeted: true,
			AnimationName: "cast_fieldmedicsupport_self",
			HitDelay:      400 * time.Millisecond,
			ReleaseDelay:  3400 * time.Millisecond,
			Duration:      3400 * time.Millisecond,
			TickDuration:  500 * time.Millisecond, NumberOfTicks: 6,
			MinimumHealingPerTick: 10, MaximumHealingPerTick: 10,
			HealingCoefficient: 0.05,
			ManaCost:           32, ManaCoefficient: 0.16,
			DescriptorMask: 5120, IsDescriptorFound: true,
			RootModifierID:   util.HashID("FieldMedicHealthBuff"),
			StatusDuration:   15 * time.Second,
			MuzzleEffectName: "effect_fieldmedicsupport_beam.ServerEventDef",
			HitEffectName:    "effect_fieldmedicsupport_targetself.ServerEventDef",
			HealEffectName:   "effect_fieldmedicsupport_targetself.ServerEventDef",
		},
		"Psistorm": {
			Name: "Psistorm", Kind: sim.AbilityKindAuraArea,
			Cooldown: 15 * time.Second, Range: 35, Radius: 6,
			AnimationName: "cast_psistorm",
			HitDelay:      170 * time.Millisecond,
			ReleaseDelay:  time.Second,
			Duration:      5 * time.Second,
			TickDuration:  time.Second, NumberOfTicks: 6,
			MinimumDamage: 7, MaximumDamage: 7, DamageCoefficient: 0.05,
			ManaCost: 18, ManaCoefficient: 0.09,
			DescriptorMask: 140, DamageType: 4, DamageSource: 1,
			IsDescriptorFound: true, IsDamageTypeFound: true,
			IsDamageSourceFound: true,
			StatusKind:          sim.AbilityStatusKindSilence,
			RootModifierID:      util.HashID("PsistormDebuff"),
			SpawnNoun:           "shadow_spirit_pool_aoe.Noun",
			HitEffectName:       "status_silenced.ServerEventDef",
		},
		"SpacetimeRandom2": {
			Name: "SpacetimeRandom2", Kind: sim.AbilityKindAuraArea,
			Cooldown: 30 * time.Second, Radius: 8,
			AnimationName: "sp_random03_delaying_sphere",
			HitDelay:      233333 * time.Microsecond,
			ReleaseDelay:  time.Second,
			Duration:      12 * time.Second,
			ManaCost:      18, ManaCoefficient: 0.09,
			StatusKind:           sim.AbilityStatusKindSlow,
			RootModifierID:       util.HashID("DelayingSphereDrag"),
			SpawnNoun:            "TabulaRasa.Noun",
			MuzzleEffectName:     "Delaying_Sphere_Muzzle.ServerEventDef",
			ActivationEffectName: "spacetime_aoe_slow_globe_effect_lvl2.ServerEventDef",
			HitEffectName:        "spacetime_aoe_slow_globe_effect_enemy.ServerEventDef",
		},
		"TimeRavagerSupport": {
			Name: "TimeRavagerSupport", Kind: sim.AbilityKindTeleportArea,
			Cooldown: 14 * time.Second, Range: 35, Radius: 4,
			AnimationName:    "sp_timeRavager_support_start",
			OutAnimationName: "sp_timeRavager_support_end",
			HitDelay:         366667 * time.Microsecond,
			TeleportDelay:    266667 * time.Microsecond,
			ReleaseDelay:     1133334 * time.Microsecond,
			MinimumDamage:    8, MaximumDamage: 14, DamageCoefficient: 0.05,
			ManaCost: 16, ManaCoefficient: 0.08,
			DescriptorMask: 132, DamageType: 3, DamageSource: 1,
			IsDescriptorFound: true, IsDamageTypeFound: true,
			IsDamageSourceFound:       true,
			StatusKind:                sim.AbilityStatusKindStun,
			StatusDuration:            3 * time.Second,
			RootModifierID:            util.HashID("TimeRavagerSupportModifier"),
			IsAlwaysUseCursorPosition: true,
			MuzzleEffectName:          "wormhole_effect.ServerEventDef",
			ActivationEffectName:      "wormhole_exit_effect.ServerEventDef",
			HitEffectName:             "wormhole_exit_effect2.ServerEventDef",
			ImpactEffectName:          "wormhole_exit_effect2.ServerEventDef",
		},
		"RepulsionWave": {
			Name: "RepulsionWave", Kind: sim.AbilityKindRepulsion,
			Cooldown: 6 * time.Second, Radius: 8,
			AnimationName: "cast_repulsionwave",
			HitDelay:      466667 * time.Microsecond,
			ReleaseDelay:  833333 * time.Microsecond,
			ManaCost:      10, ManaCoefficient: 0.05,
			DescriptorMask: 132, DamageType: 3, DamageSource: 1,
			IsDescriptorFound: true, IsDamageTypeFound: true,
			IsDamageSourceFound: true,
			Speed:               11, Distance: 2,
			MuzzleEffectName:     "Repulsion_Wave_Muzzle.ServerEventDef",
			ActivationEffectName: "Repulsion_Wave_Circle.ServerEventDef",
			RootModifierID:       util.HashID("KnockbackModifier"),
		},
		"PlasmaRandom_WebbedLightning": {
			Name:     "PlasmaRandom_WebbedLightning",
			Kind:     sim.AbilityKindProjectileBurst,
			Cooldown: 16 * time.Second, Range: 15, Radius: 2,
			AnimationName:          "el_random03_lightning_charge",
			SecondaryAnimationName: "el_random03_lightning_cast",
			HitDelays: []time.Duration{
				time.Second, time.Second, time.Second, time.Second,
				time.Second, time.Second, time.Second, time.Second,
				time.Second, time.Second, time.Second, time.Second,
			},
			ReleaseDelay:  1800 * time.Millisecond,
			MinimumDamage: 6, MaximumDamage: 30, DamageCoefficient: 0.05,
			ManaCost: 24, ManaCoefficient: 0.12,
			DescriptorMask: 8332, DamageType: 3, DamageSource: 1,
			IsDescriptorFound: true, IsDamageTypeFound: true,
			IsDamageSourceFound: true,
			ProjectileNoun:      "Ability_Fireball.Noun",
			TrailEffectName:     "el_random_03_link_effect.ServerEventDef",
			ImpactEffectName:    "el_random_03_hit_effect.ServerEventDef",
			Speed:               50, Distance: 40,
			BurstTargeting: sim.ProjectileBurstTargetingArc,
			RootModifierID: util.HashID(
				"PlasmaRandom_WebbedLightning_ShockModifier",
			),
			StatusKind:     sim.AbilityStatusKindStun,
			StatusDuration: 3 * time.Second,
		},
		"SleepingCloud": {
			Name: "SleepingCloud", Kind: sim.AbilityKindAuraArea,
			Cooldown: 12 * time.Second, Radius: 7,
			AnimationName:    "lf_shroomtempest_support",
			OutAnimationName: "lf_shroomtempest_support_relax",
			HitDelay:         100 * time.Millisecond,
			ReleaseDelay:     6 * time.Second,
			Duration:         6 * time.Second,
			ManaCost:         24, ManaCoefficient: 0.12,
			DescriptorMask: 1064, IsDescriptorFound: true,
			StatusKind:           sim.AbilityStatusKindSleep,
			RootModifierID:       util.HashID("SleepingCloud_SleepModifier"),
			ActivationEffectName: "life_shroomtempest_sleeping_cloud_hit.ServerEventDef",
			HitEffectName:        "status_sleeping.ServerEventDef",
		},
		"SoulRavagerActive": {
			Name: "SoulRavagerActive", Kind: sim.AbilityKindProjectileBurst,
			Cooldown: 12 * time.Second, Range: 50, Radius: 3,
			AnimationName: "soulravager_support",
			HitDelays: []time.Duration{
				100 * time.Millisecond, 110 * time.Millisecond,
				120 * time.Millisecond, 130 * time.Millisecond,
				140 * time.Millisecond, 150 * time.Millisecond,
			},
			ReleaseDelay:  750 * time.Millisecond,
			MinimumDamage: 7, MaximumDamage: 14, DamageCoefficient: 0.05,
			ManaCost: 20, ManaCoefficient: 0.10,
			DescriptorMask: 8328, DamageType: 4, DamageSource: 1,
			IsDescriptorFound: true, IsDamageTypeFound: true,
			IsDamageSourceFound: true,
			ProjectileNoun:      "Ability_WideProjectile.Noun",
			TrailEffectName:     "shadow_soul_silence_spin_projectile.ServerEventDef",
			ImpactEffectName:    "shadow_soul_rapid_fire_aoe_effect.ServerEventDef",
			Speed:               20, Distance: 10,
			BurstTargeting: sim.ProjectileBurstTargetingRadial,
		},
		"SoulRavagerSupport": {
			Name: "SoulRavagerSupport", Kind: sim.AbilityKindProjectileBurst,
			Cooldown: 4 * time.Second, Range: 20,
			AnimationName: "soulravager_active",
			HitDelays: []time.Duration{
				300 * time.Millisecond, 700 * time.Millisecond,
				1100 * time.Millisecond,
			},
			ReleaseDelay:  2 * time.Second,
			MinimumDamage: 9, MaximumDamage: 15, DamageCoefficient: 0.05,
			ManaCost: 12, ManaCoefficient: 0.06, LifeSteal: 0.5,
			DescriptorMask: 8320, DamageType: 4, DamageSource: 1,
			IsDescriptorFound: true, IsDamageTypeFound: true,
			IsDamageSourceFound: true,
			ProjectileNoun:      "Ability_HomingProjectile.Noun",
			MuzzleEffectName:    "ghost_shot_muzzle_effect_serverEventDef.ServerEventDef",
			TrailEffectName:     "shadow_soul_rapid_fire_projectile.ServerEventDef",
			ImpactEffectName:    "shadow_soul_homing_bullet_impact_effect.ServerEventDef",
			Speed:               12, Distance: 25,
			BurstTargeting: sim.ProjectileBurstTargetingRetargetChannel,
		},
		"Terrify": {
			Name: "Terrify", Kind: sim.AbilityKindProjectileStatus,
			Cooldown: 8 * time.Second, Range: 35,
			AnimationName: "cast_terrifybolt",
			HitDelay:      170 * time.Millisecond,
			ReleaseDelay:  600 * time.Millisecond,
			HitDelays: []time.Duration{
				0, time.Second, 2 * time.Second, 3 * time.Second, 4 * time.Second,
			},
			StatusDuration:       4100 * time.Millisecond,
			MinimumDamagePerTick: 6, MaximumDamagePerTick: 6,
			DamageCoefficient: 0.05,
			ManaCost:          18, ManaCoefficient: 0.09,
			DescriptorMask: 164, DamageType: 4, DamageSource: 1,
			IsDescriptorFound: true, IsDamageTypeFound: true,
			IsDamageSourceFound: true,
			ProjectileNoun:      "Ability_Fireball.Noun",
			MuzzleEffectName:    "Terrifying_Curse_muzzle.ServerEventDef",
			TrailEffectName:     "status_terrified.ServerEventDef",
			ImpactEffectName:    "shadow_bolt_impact.ServerEventDef",
			MissEffectName:      "ineffective_common_small.ServerEventDef",
			Speed:               40, Distance: 50,
			StatusKind:     sim.AbilityStatusKindFear,
			RootModifierID: util.HashID("TerrifyDebuff"),
		},
		"AfflictionBolt": {
			Name: "AfflictionBolt", Kind: sim.AbilityKindProjectileStatus,
			Cooldown: 15 * time.Second, Range: 80,
			AnimationName: "su_random04",
			HitDelay:      180 * time.Millisecond,
			ReleaseDelay:  600 * time.Millisecond,
			HitDelays: []time.Duration{
				2 * time.Second, 4 * time.Second, 6 * time.Second, 8 * time.Second,
			},
			StatusDuration:       8250 * time.Millisecond,
			MinimumDamagePerTick: 5, MaximumDamagePerTick: 5,
			DamageCoefficient: 0.05,
			ManaCost:          24, ManaCoefficient: 0.12,
			DescriptorMask: 164, DamageType: 4, DamageSource: 1,
			IsDescriptorFound: true, IsDamageTypeFound: true,
			IsDamageSourceFound: true,
			ProjectileNoun:      "Ability_Fireball.Noun",
			MuzzleEffectName:    "shadow_random04_muzzle_effect.ServerEventDef",
			TrailEffectName:     "shadow_random_04_affliction_bolt_effect.ServerEventDef",
			ImpactEffectName:    "status_cursed.ServerEventDef",
			Speed:               12, Distance: 144, Radius: 3,
			StatusKind:     sim.AbilityStatusKindCurse,
			RootModifierID: util.HashID("AfflictionCurse"),
		},
		"BeastCharge": {
			Name: "BeastCharge", Kind: sim.AbilityKindCharge,
			Cooldown: 10 * time.Second, Range: 50, Radius: 4,
			AnimationName:    "lf_beastsentinel_active_charge_loop",
			OutAnimationName: "lf_beastsentinel_active_charge_end",
			HitDelay:         233333 * time.Microsecond, ReleaseDelay: 600 * time.Millisecond,
			MinimumDamage: 18, MaximumDamage: 28, DamageCoefficient: 0.05,
			ManaCost: 18, ManaCoefficient: 0.09,
			DescriptorMask: 65, DamageType: 2, DamageSource: 0,
			IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true,
			MovementSpeedMultiplier: 4,
			HitEffectName:           "beastSentinel_charge_hit.ServerEventDef",
			RootModifierID:          util.HashID("BeastChargeKnockup"),
			StatusDuration:          time.Second,
		},
		"BioRandom2": {
			Name: "BioRandom2", Kind: sim.AbilityKindCharge,
			Cooldown: 12 * time.Second, Range: 30, Radius: 8,
			AnimationName:    "entanglingrush_charge",
			OutAnimationName: "cast_arborealmight",
			HitDelay:         233333 * time.Microsecond,
			ReleaseDelay:     1290 * time.Millisecond,
			ManaCost:         15, ManaCoefficient: 0.075,
			MovementSpeedMultiplier: 2, Distance: 2,
			ActivationEffectName: "roar_of_derision_cast.ServerEventDef",
			RootModifierID:       util.HashID("RoarTaunt"),
			SecondaryModifierID:  util.HashID("RoarBuff"),
			TickDuration:         5 * time.Second,
			StatusDuration:       6 * time.Second,
		},
		"EntanglingRush": {
			Name: "EntanglingRush", Kind: sim.AbilityKindCharge,
			Cooldown: 8 * time.Second, Range: 25, Radius: 6,
			AnimationName:    "entanglingrush_loop",
			OutAnimationName: "entanglingrush_end",
			ReleaseDelay:     time.Second,
			MinimumDamage:    20, MaximumDamage: 30, DamageCoefficient: 0.05,
			ManaCost: 15, ManaCoefficient: 0.075,
			DescriptorMask: 65, DamageType: 2, DamageSource: 0,
			IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true,
			MovementSpeedMultiplier: 1.6, Distance: 2,
			MuzzleEffectName: "entangling_rush_cast.ServerEventDef",
			RootModifierID:   0x1b9912d5, StatusDuration: 6 * time.Second,
		},
		"PhantomCharge": {
			Name: "PhantomCharge", Kind: sim.AbilityKindCharge,
			Cooldown: 8 * time.Second, Range: 35, Radius: 4,
			AnimationName: "su_random03_loop",
			HitDelay:      100 * time.Millisecond, ReleaseDelay: 100 * time.Millisecond,
			MinimumDamage: 9, MaximumDamage: 13, DamageCoefficient: 0.05,
			ManaCost: 14, ManaCoefficient: 0.07,
			DescriptorMask: 136, DamageType: 4, DamageSource: 1,
			IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true,
			MovementSpeedMultiplier: 8,
			HitEffectName:           "ver_minn_lf_03_tailSting_hit.ServerEventDef",
			ActivationEffectName:    "shadow_ghostform_shader_effect.ServerEventDef",
			RootModifierID:          util.HashID("PhantomSilence"),
			StatusDuration:          2 * time.Second,
		},
		"PlasmaSentinelSupport": {
			Name: "PlasmaSentinelSupport", Kind: sim.AbilityKindTimedArea,
			Cooldown: 15 * time.Second, Radius: 5,
			AnimationName: "el_plasmasentinel_support",
			HitDelay:      200 * time.Millisecond,
			ReleaseDelay:  200 * time.Millisecond,
			Duration:      8 * time.Second,
			TickDuration:  500 * time.Millisecond,
			NumberOfTicks: 15,
			MinimumDamage: 3, MaximumDamage: 3, DamageCoefficient: 0.05,
			ManaCost: 18, ManaCoefficient: 0.09,
			DescriptorMask: 152, DamageType: 3, DamageSource: 1,
			IsDescriptorFound: true, IsDamageTypeFound: true,
			IsDamageSourceFound:  true,
			ActivationEffectName: "el_plasma_sentinel_support.ServerEventDef",
		},
		"SpacetimeRandom": {
			Name: "SpacetimeRandom", Kind: sim.AbilityKindStatusArea,
			Cooldown: 15 * time.Second, Range: 20, Radius: 6,
			AnimationName: "sp_random01_dimensional_rift",
			HitDelay:      170 * time.Millisecond,
			ReleaseDelay:  700 * time.Millisecond,
			Duration:      8 * time.Second,
			ManaCost:      16, ManaCoefficient: 0.08,
			StatusKind:           sim.AbilityStatusKindBanish,
			RootModifierID:       util.HashID("BanishedModifier"),
			ActivationEffectName: "spacetime_random01_effect.ServerEventDef",
			MuzzleEffectName:     "Dimensional_Rift_Muzzle.ServerEventDef",
		},
		"SpacetimeRandom3": {
			Name: "SpacetimeRandom3", Kind: sim.AbilityKindCursorArea,
			Cooldown: 8 * time.Second, Range: 35, Radius: 4,
			AnimationName: "sp_random04_comets",
			HitDelay:      100 * time.Millisecond,
			ReleaseDelay:  800 * time.Millisecond,
			MinimumDamage: 12, MaximumDamage: 20, DamageCoefficient: 0.05,
			ManaCost: 16, ManaCoefficient: 0.08,
			DescriptorMask: 72, DamageType: 1, DamageSource: 0,
			IsDescriptorFound: true, IsDamageTypeFound: true,
			IsDamageSourceFound:       true,
			IsAlwaysUseCursorPosition: true,
			MuzzleEffectName:          "Comets_Muzzle.ServerEventDef",
			HitEffectName:             "spacetime_random04_summon_effect.ServerEventDef",
			ImpactEffectName:          "spacetime_random04_summon_effect.ServerEventDef",
		},
		"VoodooTempestSupport": {
			Name: "VoodooTempestSupport", Kind: sim.AbilityKindStatusArea,
			Cooldown: 20 * time.Second, Range: 35, Radius: 8,
			AnimationName: "voodootempest_support",
			HitDelay:      170 * time.Millisecond,
			ReleaseDelay:  700 * time.Millisecond,
			Duration:      10 * time.Second,
			TickDuration:  time.Second,
			NumberOfTicks: 1,
			ManaCost:      16, ManaCoefficient: 0.08,
			StatusKind:             sim.AbilityStatusKindCurse,
			StatusDamageBuff:       -0.33,
			StatusPhysicalIncrease: 0.50,
			StatusEnergyIncrease:   0.50,
			RootModifierID:         util.HashID("VoodooTempestWeaken"),
			ActivationEffectName:   "shadow_voodoo_curse_aoe.ServerEventDef",
			MuzzleEffectName:       "Curse_of_Weakness_muzzle.ServerEventDef",
		},
		"TechRandom": {
			Name: "TechRandom", Kind: sim.AbilityKindCursorArea,
			Cooldown: 8 * time.Second, Range: 35, Distance: 35,
			AnimationName: "tc_random01_zetawatt_beam",
			HitDelay:      233333330 * time.Nanosecond, ReleaseDelay: 500 * time.Millisecond,
			MinimumDamage: 20, MaximumDamage: 30, DamageCoefficient: 0.05,
			ManaCost: 12, ManaCoefficient: 0.06,
			DescriptorMask: 136, DamageType: 4, DamageSource: 1,
			IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true,
			IsAlwaysUseCursorPosition: true,
			ImpactEffectName:          "cyber_randomAbility_1.ServerEventDef",
			HitEffectName:             "cyber_common_hit_large.ServerEventDef",
		},
		"TechRandom2": {
			Name: "TechRandom2", Kind: sim.AbilityKindModifier,
			Cooldown:      20 * time.Second,
			AnimationName: "tc_random03_omni_shield",
			HitDelay:      100 * time.Millisecond, ReleaseDelay: 800 * time.Millisecond,
			Duration: 4 * time.Second,
			ManaCost: 16, ManaCoefficient: 0.08,
			RootModifierID:       util.HashID("OmniShieldModifier"),
			ActivationEffectName: "cyber_omnishield.ServerEventDef",
		},
		"TechRandom3": {
			Name: "TechRandom3", Kind: sim.AbilityKindMelee,
			Cooldown: 10 * time.Second, Range: 2.5, Radius: 4, Angle: 90,
			AnimationName: "tc_random04_charged_fist",
			HitDelay:      300 * time.Millisecond, ReleaseDelay: 900 * time.Millisecond,
			MinimumDamage: 30, MaximumDamage: 50, DamageCoefficient: 0.05,
			ManaCost: 14, ManaCoefficient: 0.07,
			DescriptorMask: 65, DamageType: 0, DamageSource: 0,
			IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true,
			IsShouldPursue: true,
			HitEffectName:  "cyber_chargedFistHit.ServerEventDef",
			RootModifierID: util.HashID("ChargedFistTaunt"),
			StatusKind:     sim.AbilityStatusKindTaunt,
			StatusDuration: 6 * time.Second,
		},
		"LFPoisonRavager_Expunge": {
			Name: "LFPoisonRavager_Expunge", Kind: sim.AbilityKindMelee,
			Cooldown: 4 * time.Second, Range: 3,
			AnimationName: "lf_poisonravager_active",
			HitDelay:      200 * time.Millisecond, ReleaseDelay: 730 * time.Millisecond,
			MinimumDamage: 10, MaximumDamage: 16, DamageCoefficient: 0.05,
			ManaCost: 10, ManaCoefficient: 0.05,
			DescriptorMask: 193, DamageType: 2, DamageSource: 0,
			IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true,
			IsShouldPursue: true,
		},
		"LFPoisonRavager_PoisonNova": {
			Name: "LFPoisonRavager_PoisonNova", Kind: sim.AbilityKindPointBlank,
			Cooldown: 6 * time.Second, Radius: 6,
			AnimationName: "lf_poisonravager_support",
			HitDelay:      233333330 * time.Nanosecond,
			ReleaseDelay:  850 * time.Millisecond,
			MinimumDamage: 2, MaximumDamage: 6, DamageCoefficient: 0.05,
			ManaCost: 12, ManaCoefficient: 0.06,
			DescriptorMask: 72, DamageType: 2, DamageSource: 0,
			IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true,
			ImpactEffectName:     "Thornado_Spines.ServerEventDef",
			RootModifierID:       0x548abb1c,
			StatusDuration:       10 * time.Second,
			TickDuration:         2 * time.Second,
			NumberOfTicks:        5,
			MinimumDamagePerTick: 4, MaximumDamagePerTick: 4,
		},
		"TCShieldedSentinelActive": {
			Name: "TCShieldedSentinelActive", Kind: sim.AbilityKindPointBlank,
			Cooldown: 10 * time.Second, Radius: 8,
			AnimationName: "tc_shieldedsentinel_active",
			HitDelay:      500 * time.Millisecond,
			ReleaseDelay:  720 * time.Millisecond,
			MinimumDamage: 12, MaximumDamage: 20, DamageCoefficient: 0.05,
			ManaCost: 16, ManaCoefficient: 0.08,
			DescriptorMask: 136, DamageType: 0, DamageSource: 1,
			IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true,
			RootModifierID: 0x9a731b65, StatusKind: sim.AbilityStatusKindTaunt,
			StatusDuration: 5 * time.Second,
		},
		"TCShieldedSentinelSupport": {
			Name: "TCShieldedSentinelSupport", Kind: sim.AbilityKindCursorArea,
			Cooldown: 6 * time.Second, Range: 20, Radius: 4,
			AnimationName: "tc_shieldedsentinel_support",
			HitDelay:      170 * time.Millisecond,
			ReleaseDelay:  450 * time.Millisecond,
			MinimumDamage: 20, MaximumDamage: 30, DamageCoefficient: 0.05,
			SecondaryMinimumDamage: 10, SecondaryMaximumDamage: 16,
			SecondaryDamageCoefficient: 0.025,
			ManaCost:                   12, ManaCoefficient: 0.06,
			DescriptorMask: 136, DamageType: 4, DamageSource: 0,
			IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true,
			IsAlwaysUseCursorPosition: true,
			HitEffectName:             "cyber_shield_concussiveblast.ServerEventDef",
			MuzzleEffectName:          "Concussive_Blast_muzzle.ServerEventDef",
			RootModifierID:            util.HashID("TCShieldedSentinel_Support_Snare"),
			StatusKind:                sim.AbilityStatusKindSlow,
			StatusDuration:            5 * time.Second,
			StatusMovementScale:       0.5,
			StatusAttackScale:         1,
		},
		"FireTempestSupport": {
			Name: "FireTempestSupport", Kind: sim.AbilityKindTargetedAOE,
			Cooldown: 14 * time.Second, Range: 35, Radius: 6,
			AnimationName: "cast_sphereoftransfusion",
			HitDelay:      170 * time.Millisecond, ReleaseDelay: 800 * time.Millisecond,
			MinimumDamage: 9, MaximumDamage: 18,
			MinimumDamagePerTick: 4, MaximumDamagePerTick: 4,
			DamageCoefficient: 0.05,
			ManaCost:          20, ManaCoefficient: 0.10,
			DescriptorMask: 140, DamageType: 3, DamageSource: 1,
			IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true,
			TickDuration:        time.Second,
			NumberOfTicks:       4,
			IsInitialPulse:      true,
			IsInitialTargetOnly: true,
			EffectNouns:         []string{"FireTempestSupport.Noun"},
			MuzzleEffectName:    "Meteor_Strike_muzzle.ServerEventDef",
			RootModifierID:      util.HashID("FireTempestHealDebuff"),
			StatusDuration:      time.Second,
		},
		"MissileTempestSupport": {
			Name: "MissileTempestSupport", Kind: sim.AbilityKindCursorArea,
			Cooldown: time.Second, Range: 35, Radius: 5,
			AnimationName: "missiletempest_support",
			ReleaseDelay:  250 * time.Millisecond,
			MinimumDamage: 3, MaximumDamage: 6, DamageCoefficient: 0.05,
			DescriptorMask: 72, DamageType: 0, DamageSource: 0,
			IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true,
			IsAlwaysUseCursorPosition: true,
			MuzzleEffectName:          "Chaff_muzzle.ServerEventDef",
			HitEffectName:             "cyber_missile_chaff_hit.ServerEventDef",
			RootModifierID:            0x95917802,
			StatusKind:                sim.AbilityStatusKindSlow,
			StatusDuration:            6 * time.Second,
			StatusMovementScale:       0.50,
			StatusAttackScale:         1,
		},
		"ShadowRavagerSupport": {
			Name: "ShadowRavagerSupport", Kind: sim.AbilityKindModifier,
			AnimationName:  "su_shadowRavager_support",
			ReleaseDelay:   600 * time.Millisecond,
			Duration:       6 * time.Second,
			RootModifierID: util.HashID("ShadowRavagerStealthModifier"),
		},
		"LightspeedTempestActive": {
			Name: "LightspeedTempestActive", Kind: sim.AbilityKindCursorArea,
			Range: 35, Radius: 7,
			AnimationName: "lightspeedtempest_active",
			HitDelay:      100 * time.Millisecond,
			ReleaseDelay:  550 * time.Millisecond,
			MinimumDamage: 14, MaximumDamage: 21, DamageCoefficient: 0.05,
			DescriptorMask: 136, DamageType: 1, DamageSource: 1,
			IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true,
			IsAlwaysUseCursorPosition: true,
			HitEffectName:             "spacetime_AOE_slow_effect.ServerEventDef",
			RootModifierID:            util.HashID("LightSpeedSlow"),
			StatusKind:                sim.AbilityStatusKindSlow,
			StatusDuration:            5 * time.Second,
			StatusMovementScale:       0.6,
			StatusAttackScale:         1,
		},
	}
	for assetName, ability := range definition {
		if ability.Kind != sim.AbilityKindProjectileBurst ||
			ability.ShotCount == 0 || ability.FiringRate <= 0 {
			continue
		}
		ability.HitDelays = make([]time.Duration, ability.ShotCount)
		for shotIndex := range ability.HitDelays {
			ability.HitDelays[shotIndex] =
				time.Duration(shotIndex) * ability.FiringRate
		}
		lastLaunch := ability.HitDelays[len(ability.HitDelays)-1]
		ability.ReleaseDelay = max(
			ability.ReleaseDelay,
			lastLaunch+ability.FiringRate,
		)
		definition[assetName] = ability
	}
	return definition
}

func applyRecoveredAreaRadiusPolicy(definition sim.AbilityDefinition) sim.AbilityDefinition {
	switch definition.Name {
	case "BinarySentinelSupport", "GravityStorm", "LFPoisonRavager_PoisonNova",
		"MissileTempestSupport", "PlasmaRandom", "PlasmaRandom_LightningBall",
		"Sprout", "TCShieldedSentinelActive", "TCShieldedSentinelBasic",
		"TCShieldedSentinelSupport":
		definition.IsAreaRadiusScaled = true
	}
	return definition
}

func applyRecoveredAreaDurationPolicy(definition sim.AbilityDefinition) sim.AbilityDefinition {
	switch definition.Name {
	case "FireTempestSupport", "LightningTempest_Active", "MissileTempestActive",
		"PlasmaSentinelSupport", "Psistorm", "VoodooTempestSupport":
		definition.IsAreaDurationScaled = true
	}
	return definition
}

func Load(store *contentstore.Store) (zonecontent.Programs, error) {
	if store == nil {
		return zonecontent.Programs{}, errors.New("nil content store")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	isAvailable, err := store.IsLuaCatalogAvailable(ctx)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("catalogCheck: %w", err)
	}
	if !isAvailable {
		return zonecontent.Programs{}, nil
	}
	chainLevel, err := store.ChainLevelReferences(ctx)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("chainLevel: %w", err)
	}
	arenaLevels, err := loadArenaLevels(ctx, store)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("arenaLevel: %w", err)
	}
	templates, err := store.CreatureTemplates(ctx)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("heroTemplate: %w", err)
	}
	playerBasicAbility, playerBasicUnsupported, err := loadPlayerBasicAbilities(ctx, store, templates)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("playerBasicAbility: %w", err)
	}
	recoveredHeroAbility := recoveredHeroAbilityDefinitions()
	heroKits, err := loadHeroKits(
		ctx, store, templates, playerBasicAbility, playerBasicUnsupported,
		recoveredHeroAbility,
	)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("heroKit: %w", err)
	}
	contentCritical, err := store.CriticalTuning(ctx)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("criticalTuning: %w", err)
	}
	critical := sim.CriticalTuning{
		DamageBonus:       contentCritical.DamageBonus,
		RatingConversions: append([]float32(nil), contentCritical.RatingConversions...),
	}
	contentDifficulty, err := store.DifficultyScaling(ctx)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("difficultyTuning: %w", err)
	}
	difficulty := sim.DifficultyCombatTuning{
		StarHealthBase:   contentDifficulty.StarHealthBase,
		StarDamageBase:   contentDifficulty.StarDamageBase,
		HealthMultiplier: make([]float32, len(contentDifficulty.Rows)),
		DamageMultiplier: make([]float32, len(contentDifficulty.Rows)),
	}
	for index := range contentDifficulty.Rows {
		difficulty.HealthMultiplier[index] = contentDifficulty.Rows[index].HealthMultiplier
		difficulty.DamageMultiplier[index] = contentDifficulty.Rows[index].DamageMultiplier
	}
	nonPlayerClasses, err := store.NonPlayerClasses(ctx)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("nonPlayerClass: %w", err)
	}
	nonPlayerHitPoint := make(map[uint32]float32, len(nonPlayerClasses))
	defenseProfiles, defenseErr := store.NonPlayerNounProfiles(ctx)
	if defenseErr != nil {
		return zonecontent.Programs{}, fmt.Errorf("defenseProfiles: %w", defenseErr)
	}
	nonPlayerDefenses := make(map[uint32]game.CampaignNPCProfile, len(defenseProfiles))
	for _, profile := range defenseProfiles {
		nonPlayerDefenses[util.HashID(profile.NounName)] = game.CampaignNPCProfile{
			DodgeRating: profile.DodgeRating, ResistRating: profile.ResistRating,
		}
	}
	nonPlayerCritical := make(map[uint32]sim.CriticalProfile, len(nonPlayerClasses))
	for _, class := range nonPlayerClasses {
		nonPlayerHitPoint[class.InstanceID] = class.HitPoint
		nonPlayerCritical[class.InstanceID] = sim.CriticalProfile{Rating: class.CriticalRating}
	}
	nounPhysicsCatalog, err := store.NounPhysicsCatalog(ctx)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("nounPhysics: %w", err)
	}
	npcDeathAnimations, err := store.NPCDeathAnimations(ctx)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("npcDeathAnimations: %w", err)
	}
	nounPhysics := make(map[string]zonecontent.NounPhysics, len(nounPhysicsCatalog))
	nounPhysicsByID := make(map[uint32]zonecontent.NounPhysics, len(nounPhysicsCatalog))
	for _, noun := range nounPhysicsCatalog {
		physics := zonecontent.NounPhysics{
			FootprintRadius: noun.FootprintRadius,
			CreatureType:    noun.CreatureType, IsCreatureTypeKnown: noun.IsCreatureTypeKnown,
			OrdinaryDeathAnimation: noun.OrdinaryDeathAnimation,
			DanceAnimation:         noun.DanceAnimation,
			Lifetime:               simulationContentDuration(noun.LifetimeSeconds),
			BoundMinimum: sim.Position{
				X: noun.BoundMinimumX, Y: noun.BoundMinimumY, Z: noun.BoundMinimumZ,
			},
			BoundMaximum: sim.Position{
				X: noun.BoundMaximumX, Y: noun.BoundMaximumY, Z: noun.BoundMaximumZ,
			},
		}
		nounPhysics[noun.AssetName] = physics
		nounPhysicsByID[util.HashID(noun.AssetName)] = physics
	}
	tutorialPhysicsAliases := map[string]string{
		"TutorialBasicPoisonNoOrbs.Noun": "TutorialBasicPoison.Noun",
		"TutorialSpecialOne_Intro.Noun":  "TutorialSpecialOne.Noun",
	}
	for aliasName, sourceName := range tutorialPhysicsAliases {
		physics, isFound := nounPhysics[sourceName]
		if !isFound {
			return zonecontent.Programs{}, fmt.Errorf(
				"nounPhysicsAlias[%s]: %s missing", aliasName, sourceName,
			)
		}
		nounPhysics[aliasName] = physics
		nounPhysicsByID[util.HashID(aliasName)] = physics
	}
	for _, assetName := range nounPhysicsAssetNames {
		if _, isFound := nounPhysics[assetName]; !isFound {
			return zonecontent.Programs{}, fmt.Errorf("nounPhysicsMissing: %s", assetName)
		}
	}
	shapes, err := store.NounPhysicsShapes(ctx)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("nounShape: %w", err)
	}
	var projectileHalfExtent sim.Position
	for _, shape := range shapes {
		if shape.AssetName == "Ability_Fireball.Noun" && shape.ShapeRole == "projectile_query" &&
			shape.ShapeKind == "box" {
			projectileHalfExtent = sim.Position{
				X: shape.DimensionX / 2, Y: shape.DimensionY / 2, Z: shape.DimensionZ / 2,
			}
		}
		if shape.ShapeRole == "pickup_trigger" && shape.ShapeKind == "box" {
			noun := nounPhysics[shape.AssetName]
			noun.PickupTriggerHalfExtent = sim.Position{
				X: shape.DimensionX / 2, Y: shape.DimensionY / 2, Z: shape.DimensionZ / 2,
			}
			nounPhysics[shape.AssetName] = noun
		}
	}
	if len(nounPhysics) < len(nounPhysicsAssetNames) || projectileHalfExtent.X <= 0 ||
		projectileHalfExtent.Y <= 0 || projectileHalfExtent.Z <= 0 {
		return zonecontent.Programs{}, errors.New("nounPhysics: incomplete")
	}
	orbLifetime := map[string]time.Duration{
		"HealthOrb.Noun": 30 * time.Second, "manaorb.Noun": 30 * time.Second,
		"HealthOrbPlaced.Noun": 0, "ManaOrbPlaced.Noun": 0,
	}
	for assetName, lifetime := range orbLifetime {
		noun := nounPhysics[assetName]
		if noun.Lifetime != lifetime || noun.PickupTriggerHalfExtent.X <= 0 ||
			noun.PickupTriggerHalfExtent.Y <= 0 || noun.PickupTriggerHalfExtent.Z <= 0 {
			return zonecontent.Programs{}, fmt.Errorf("orbPhysics[%s]: incomplete", assetName)
		}
	}
	contentCrystalDefinitions, err := store.CrystalDefinitions(ctx)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("crystalDefinition: %w", err)
	}
	crystalDefinitions := make([]sim.CrystalDefinition, 0, len(contentCrystalDefinitions))
	for _, definition := range contentCrystalDefinitions {
		if definition.MinimumLevel < 0 || definition.MaximumLevel < definition.MinimumLevel {
			return zonecontent.Programs{}, fmt.Errorf("crystalDefinition[%d]: invalid bounds", definition.Ordinal)
		}
		catalyst, isCatalyst := game.CatalystFromNounName(definition.NounReference)
		if !isCatalyst {
			return zonecontent.Programs{}, fmt.Errorf(
				"crystalDefinition[%d]: invalid noun %q",
				definition.Ordinal, definition.NounReference,
			)
		}
		crystalDefinitions = append(crystalDefinitions, sim.CrystalDefinition{
			NounName: definition.NounReference, CrystalType: int32(catalyst.Type),
			Rarity:       int32(catalyst.Rarity),
			MinimumLevel: uint32(definition.MinimumLevel),
			MaximumLevel: uint32(definition.MaximumLevel), Weight: definition.Weight,
		})
	}
	contentCrystalLevelOffsets, err := store.CrystalLevelOffsets(ctx)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("crystalLevelOffset: %w", err)
	}
	crystalLevelOffsets := make([]sim.CrystalLevelOffset, 0, len(contentCrystalLevelOffsets))
	for _, offset := range contentCrystalLevelOffsets {
		crystalLevelOffsets = append(crystalLevelOffsets, sim.CrystalLevelOffset{
			Offset: offset.Offset, Weight: offset.Weight,
		})
	}
	if len(crystalDefinitions) != build103CrystalDefinitionCount ||
		len(crystalLevelOffsets) != build103CrystalLevelOffsetCount {
		return zonecontent.Programs{}, fmt.Errorf("crystalTuning: incomplete definitions=%d offsets=%d",
			len(crystalDefinitions), len(crystalLevelOffsets))
	}
	root, err := store.LuaChunk(ctx, quadraFirstAggroChunkID)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("quadraChunk: %w", err)
	}
	if root.SHA256 != quadraFirstAggroSHA256 {
		return zonecontent.Programs{}, fmt.Errorf("quadraHash: got %s", root.SHA256)
	}
	storedModules, err := store.LuaModules(ctx, quadraFirstAggroChunkID)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("quadraModules: %w", err)
	}
	modules := make(map[string]sim.LuaBytecode, len(storedModules))
	for name, module := range storedModules {
		modules[name] = sim.LuaBytecode{
			ChunkID: module.ID, SHA256: module.SHA256, Contents: module.Bytecode,
		}
	}
	template, isTemplateFound := modules[firstAggroTemplateModule]
	if !isTemplateFound {
		return zonecontent.Programs{}, errors.New("quadra template missing")
	}
	if template.ChunkID != firstAggroTemplateChunkID || template.SHA256 != firstAggroTemplateSHA256 {
		return zonecontent.Programs{}, fmt.Errorf("quadraTemplateIdentity: got %d/%s",
			template.ChunkID, template.SHA256)
	}
	program, err := sim.CompileLuaAbility(sim.LuaAbilityInput{
		Root: sim.LuaBytecode{
			ChunkID: root.ID, SHA256: root.SHA256, Contents: root.Bytecode,
		},
		Modules: modules, Role: "specialOne",
		Position: sim.Position{
			X: 259.346, Y: 81.391, Z: 25.088,
		},
	})
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("quadraCompile: %w", err)
	}
	lightningBasic, err := loadLuaAbilityDefinition(ctx, store, "LightningRogueBasic")
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("lightningBasic: %w", err)
	}
	if lightningBasic.Kind != sim.AbilityKindMelee {
		return zonecontent.Programs{}, fmt.Errorf("lightningBasicKind: %s", lightningBasic.Kind)
	}
	electronSphere, err := loadLuaAbilityDefinition(ctx, store, "PlasmaRandom_LightningBall")
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("electronSphere: %w", err)
	}
	if electronSphere.Kind != sim.AbilityKindProjectile {
		return zonecontent.Programs{}, fmt.Errorf("electronSphereKind: %s", electronSphere.Kind)
	}
	supportHealerBasic, err := loadLuaAbilityDefinition(ctx, store, "SupportHealerBasic")
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("supportHealerBasic: %w", err)
	}
	if supportHealerBasic.Kind != sim.AbilityKindProjectile {
		return zonecontent.Programs{}, fmt.Errorf("supportHealerBasicKind: %s", supportHealerBasic.Kind)
	}
	supportHealerPassive, err := loadPinnedLuaBytecode(
		ctx, store, supportHealerPassiveChunkID, supportHealerPassiveSHA256, "supportHealerPassive",
	)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("soloSupportLoad: %w", err)
	}
	supportHealerPassiveDefinition, err := sim.CompileLuaSummonPassiveDefinition(sim.LuaAbilityInput{
		Root: supportHealerPassive, Role: "sage",
	})
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("supportHealerPassiveCompile: %w", err)
	}
	supportHealerPetBasic, err := loadLuaAbilityDefinition(ctx, store, "SupportHealerPetBasic")
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("supportHealerPetBasic: %w", err)
	}
	if supportHealerPetBasic.Kind != sim.AbilityKindMelee {
		return zonecontent.Programs{}, fmt.Errorf("supportHealerPetBasicKind: %s", supportHealerPetBasic.Kind)
	}
	sentryDroneLaser, err := loadLuaAbilityDefinition(ctx, store, "SentryDroneLaser")
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("sentryDroneLaser: %w", err)
	}
	if sentryDroneLaser.Kind != sim.AbilityKindProjectile {
		return zonecontent.Programs{}, fmt.Errorf("sentryDroneLaserKind: %s", sentryDroneLaser.Kind)
	}
	fireTempestPetBasic, err := loadLuaAbilityDefinition(ctx, store, "Fireball")
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("fireTempestPetBasic: %w", err)
	}
	if fireTempestPetBasic.Kind != sim.AbilityKindProjectile {
		return zonecontent.Programs{}, fmt.Errorf(
			"fireTempestPetBasicKind: %s", fireTempestPetBasic.Kind,
		)
	}
	beastPetBasic, err := loadLuaAbilityDefinition(ctx, store, "BeastSentinelPetAttack")
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("beastPetBasic: %w", err)
	}
	plasmaSentinelPetBasic, err := loadLuaAbilityDefinition(
		ctx, store, "PlasmaSentinelPetMelee",
	)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("plasmaSentinelPetBasic: %w", err)
	}
	if plasmaSentinelPetBasic.Kind != sim.AbilityKindMelee {
		return zonecontent.Programs{}, fmt.Errorf(
			"plasmaSentinelPetBasicKind: %s", plasmaSentinelPetBasic.Kind,
		)
	}
	if beastPetBasic.Kind != sim.AbilityKindMelee {
		return zonecontent.Programs{}, fmt.Errorf("beastPetBasicKind: %s", beastPetBasic.Kind)
	}
	poisonMelee, err := loadLuaAbilityDefinition(ctx, store, "TutorialPoisonMelee")
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("poisonMelee: %w", err)
	}
	if poisonMelee.Kind != sim.AbilityKindMelee {
		return zonecontent.Programs{}, fmt.Errorf("poisonMeleeKind: %s", poisonMelee.Kind)
	}
	poisonCloud, err := loadLuaAbilityDefinition(ctx, store, "TutorialPoisonCloud")
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("poisonCloud: %w", err)
	}
	if poisonCloud.Kind != sim.AbilityKindProjectile {
		return zonecontent.Programs{}, fmt.Errorf("poisonCloudKind: %s", poisonCloud.Kind)
	}
	plasmaLightning, err := loadLuaAbilityDefinition(ctx, store, "TutorialPlasmaLightning")
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("plasmaLightning: %w", err)
	}
	if plasmaLightning.Kind != sim.AbilityKindProjectile {
		return zonecontent.Programs{}, fmt.Errorf("plasmaLightningKind: %s", plasmaLightning.Kind)
	}
	tailZap, err := loadLuaAbilityDefinition(ctx, store, "TailZap")
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("tailZap: %w", err)
	}
	if tailZap.Kind != sim.AbilityKindMelee {
		return zonecontent.Programs{}, fmt.Errorf("tailZapKind: %s", tailZap.Kind)
	}
	burstShot, err := loadLuaAbilityDefinition(ctx, store, "BurstShot")
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("burstShot: %w", err)
	}
	if burstShot.Kind != sim.AbilityKindProjectile {
		return zonecontent.Programs{}, fmt.Errorf("burstShotKind: %s", burstShot.Kind)
	}
	interactWithObelisk, err := loadLuaAbilityDefinition(ctx, store, "InteractWithObelisk")
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("interactWithObelisk: %w", err)
	}
	if interactWithObelisk.Kind != sim.AbilityKindInteractable {
		return zonecontent.Programs{}, fmt.Errorf("interactWithObeliskKind: %s", interactWithObelisk.Kind)
	}
	interactHealthObelisk, err := loadLuaAbilityDefinition(ctx, store, "InteractHealthObelisk")
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("interactHealthObelisk: %w", err)
	}
	if interactHealthObelisk.Kind != sim.AbilityKindInteractable || !interactHealthObelisk.IsOrbDrop {
		return zonecontent.Programs{}, fmt.Errorf("interactHealthObeliskKind: %#v", interactHealthObelisk)
	}
	securityTeleporter, err := loadPinnedLuaBytecode(
		ctx, store, zoneteleport.SecurityChunkID, zoneteleport.SecuritySHA256,
		"securityTeleporter",
	)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("overdriveLoad: %w", err)
	}
	teleporterModifier, err := loadPinnedLuaBytecode(
		ctx, store, zoneteleport.ModifierChunkID, zoneteleport.ModifierSHA256, "teleporterModifier",
	)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("catalystLoad: %w", err)
	}
	invisibleBehavior, err := loadPinnedLuaBytecode(
		ctx, store, invisibleBehaviorChunkID, invisibleBehaviorSHA256, "invisibleBehavior",
	)
	if err != nil {
		return zonecontent.Programs{}, err
	}
	invisibleLifecycle, err := sim.CompileLuaBehaviorLifecycle(sim.LuaBehaviorInput{
		Root: invisibleBehavior, EntryGlobal: "nBehavior_Invisible", Role: "guard",
	}, []string{"Activate", "Deactivate"})
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("invisibleBehaviorCompile: %w", err)
	}
	spawnModifier, err := loadPinnedLuaBytecode(
		ctx, store, zonespawn.ChunkID, zonespawn.SHA256, "spawnModifier",
	)
	if err != nil {
		return zonecontent.Programs{}, err
	}
	spawnLifecycle, err := sim.CompileLuaModifierLifecycle(sim.LuaModifierInput{
		Root: spawnModifier, Role: zonespawn.NPCRole,
	}, []string{"Activate", "Tick", "Deactivate"})
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("spawnModifierCompile: %w", err)
	}
	bossTeleporter, err := zoneteleport.CompileSimulation(
		securityTeleporter,
		teleporterModifier,
		sim.Position{
			X: 260.83334, Y: 232.78407, Z: 20.16802,
		}, sim.Position{
			X: -347.57553, Y: -224.60829, Z: 10.08803,
		})
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("teleporterCompile: %w", err)
	}
	abilitySecondJob, err := store.MarkerLuaJob(ctx, cryosLevelName, introAbilitySecondMarkerID)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("abilitySecondMarker: %w", err)
	}
	if abilitySecondJob.CallbackName != introAbilitySecondCallback ||
		abilitySecondJob.LuaChunkID != introAbilitySecondChunkID {
		return zonecontent.Programs{}, fmt.Errorf("abilitySecondLink: got %s/%d",
			abilitySecondJob.CallbackName, abilitySecondJob.LuaChunkID)
	}
	if !abilitySecondJob.IsServerOnly || !abilitySecondJob.IsTriggerOnceOnly {
		return zonecontent.Programs{}, fmt.Errorf("abilitySecondPolicy: server=%t once=%t",
			abilitySecondJob.IsServerOnly, abilitySecondJob.IsTriggerOnceOnly)
	}
	abilitySecondChunk, err := store.LuaChunk(ctx, abilitySecondJob.LuaChunkID)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("abilitySecondChunk: %w", err)
	}
	if abilitySecondChunk.SHA256 != introAbilitySecondSHA256 {
		return zonecontent.Programs{}, fmt.Errorf("abilitySecondHash: got %s", abilitySecondChunk.SHA256)
	}
	abilitySecondProgram, err := sim.CompileLuaJob(sim.LuaJobInput{
		Root: sim.LuaBytecode{
			ChunkID: abilitySecondChunk.ID, SHA256: abilitySecondChunk.SHA256,
			Contents: abilitySecondChunk.Bytecode,
		},
		EntryGlobal: "nTutorial_IntroAbilitySecond", PlayerRoles: []sim.Role{"player"},
		PreloadedModules: luaJobPreloadedModules,
	})
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("abilitySecondCompile: %w", err)
	}
	healthAndPowerJob, err := store.MarkerLuaJob(ctx, cryosLevelName, introHealthAndPowerMarkerID)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("healthAndPowerMarker: %w", err)
	}
	if healthAndPowerJob.CallbackName != introHealthAndPowerCallback {
		return zonecontent.Programs{}, fmt.Errorf(
			"healthAndPowerLink: got %s", healthAndPowerJob.CallbackName,
		)
	}
	if healthAndPowerJob.IsServerOnly || healthAndPowerJob.IsTriggerOnceOnly {
		return zonecontent.Programs{}, fmt.Errorf(
			"healthAndPowerPolicy: server=%t once=%t",
			healthAndPowerJob.IsServerOnly, healthAndPowerJob.IsTriggerOnceOnly,
		)
	}
	overdriveJob, err := store.MarkerLuaJob(ctx, cryosLevelName, introOverdriveMarkerID)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("overdriveMarker: %w", err)
	}
	if overdriveJob.CallbackName != introOverdriveCallback ||
		overdriveJob.LuaChunkID != introOverdriveChunkID {
		return zonecontent.Programs{}, fmt.Errorf("overdriveLink: got %s/%d",
			overdriveJob.CallbackName, overdriveJob.LuaChunkID)
	}
	if !overdriveJob.IsServerOnly || overdriveJob.IsTriggerOnceOnly {
		return zonecontent.Programs{}, fmt.Errorf("overdrivePolicy: server=%t once=%t",
			overdriveJob.IsServerOnly, overdriveJob.IsTriggerOnceOnly)
	}
	overdriveChunk, err := store.LuaChunk(ctx, overdriveJob.LuaChunkID)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("overdriveChunk: %w", err)
	}
	if overdriveChunk.SHA256 != introOverdriveSHA256 {
		return zonecontent.Programs{}, fmt.Errorf("overdriveHash: got %s", overdriveChunk.SHA256)
	}
	overdriveProgram, err := sim.CompileLuaJob(sim.LuaJobInput{
		Root: sim.LuaBytecode{
			ChunkID: overdriveChunk.ID, SHA256: overdriveChunk.SHA256,
			Contents: overdriveChunk.Bytecode,
		},
		EntryGlobal: "nTutorial_IntroOverdriveActivate", PlayerRoles: []sim.Role{"player"},
		PreloadedModules: luaJobPreloadedModules,
	})
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("overdriveCompile: %w", err)
	}
	secondCreatureJob, err := store.MarkerLuaJob(ctx, cryosLevelName, introSecondCreatureMarkerID)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("secondCreatureMarker: %w", err)
	}
	if secondCreatureJob.CallbackName != introSecondCreatureCallback ||
		secondCreatureJob.LuaChunkID != introSecondCreatureChunkID {
		return zonecontent.Programs{}, fmt.Errorf("secondCreatureLink: got %s/%d",
			secondCreatureJob.CallbackName, secondCreatureJob.LuaChunkID)
	}
	if !secondCreatureJob.IsServerOnly || secondCreatureJob.IsTriggerOnceOnly {
		return zonecontent.Programs{}, fmt.Errorf("secondCreaturePolicy: server=%t once=%t",
			secondCreatureJob.IsServerOnly, secondCreatureJob.IsTriggerOnceOnly)
	}
	secondCreatureChunk, err := store.LuaChunk(ctx, secondCreatureJob.LuaChunkID)
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("secondCreatureChunk: %w", err)
	}
	if secondCreatureChunk.SHA256 != introSecondCreatureSHA256 {
		return zonecontent.Programs{}, fmt.Errorf("secondCreatureHash: got %s", secondCreatureChunk.SHA256)
	}
	secondCreatureProgram, err := sim.CompileLuaJob(sim.LuaJobInput{
		Root: sim.LuaBytecode{
			ChunkID: secondCreatureChunk.ID, SHA256: secondCreatureChunk.SHA256,
			Contents: secondCreatureChunk.Bytecode,
		},
		EntryGlobal: "nTutorial_IntroSecondCreatureUnlock", PlayerRoles: []sim.Role{"player"},
		PreloadedModules: luaJobPreloadedModules,
	})
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("secondCreatureCompile: %w", err)
	}
	soloSupportUnlock, err := loadPinnedPlayerLuaJob(
		ctx, store, zoneunlock.SoloSupportChunkID, zoneunlock.SoloSupportSHA256,
		"nTutorial_SoloSupportUnlock", "soloSupportUnlock",
	)
	if err != nil {
		return zonecontent.Programs{}, err
	}
	supportUnlock, err := loadPinnedPlayerLuaJob(
		ctx, store, zoneunlock.SupportChunkID, zoneunlock.SupportSHA256,
		"nTutorial_SupportUnlock", "supportUnlock",
	)
	if err != nil {
		return zonecontent.Programs{}, err
	}
	overdriveUnlock, err := loadPinnedPlayerLuaJob(
		ctx, store, zoneunlock.OverdriveChunkID, zoneunlock.OverdriveSHA256,
		"nTutorial_OverdriveUnlock", "overdriveUnlock",
	)
	if err != nil {
		return zonecontent.Programs{}, err
	}
	catalystUnlock, err := loadPinnedPlayerLuaJob(
		ctx, store, zoneunlock.CatalystChunkID, zoneunlock.CatalystSHA256,
		"nTutorial_CatalystUnlock", "catalystUnlock",
	)
	if err != nil {
		return zonecontent.Programs{}, err
	}
	crystalPickupBytecode, err := loadPinnedLuaBytecode(
		ctx, store, crystalPickupChunkID, crystalPickupSHA256, "crystalPickup",
	)
	if err != nil {
		return zonecontent.Programs{}, err
	}
	crystalPickup, err := sim.CompileLuaAbility(sim.LuaAbilityInput{
		Root: crystalPickupBytecode, Role: "playerAgent", PlayerRole: "player",
		PlayerIndex: 0, TargetRole: "crystal",
	})
	if err != nil {
		return zonecontent.Programs{}, fmt.Errorf("crystalPickupCompile: %w", err)
	}
	objectiveDefinitions := []struct {
		name   string
		sha256 string
		input  sim.LuaObjectiveInput
	}{
		{name: "TouchAllObelisks", sha256: obeliskObjectiveSHA256,
			input: sim.LuaObjectiveInput{ObjectiveID: 0x61c07561, InteractableCount: 3}},
		{name: "DontUseHealthObelisks", sha256: healthObeliskObjectiveSHA256,
			input: sim.LuaObjectiveInput{ObjectiveID: 0x79278139}},
		{name: "EnemyHealthRegen", sha256: enemyRegenObjectiveSHA256,
			input: sim.LuaObjectiveInput{ObjectiveID: 0x0f3e7324}},
		{name: "PlayerHealthDrain", sha256: playerDrainObjectiveSHA256,
			input: sim.LuaObjectiveInput{ObjectiveID: 0xec532a92}},
		{name: "DestroyAllDestructables", sha256: destructibleObjectiveSHA256,
			input: sim.LuaObjectiveInput{ObjectiveID: 0xc5980573}},
		{name: "DefeatAllMonsters", sha256: defeatAllObjectiveSHA256,
			input: sim.LuaObjectiveInput{ObjectiveID: 0xa28485cc}},
		{name: "StayAlive", sha256: stayAliveObjectiveSHA256,
			input: sim.LuaObjectiveInput{ObjectiveID: 0x6d26f97d}},
		{name: "LootCrystals", sha256: lootCrystalObjectiveSHA256,
			input: sim.LuaObjectiveInput{ObjectiveID: 0x53449566}},
		{name: "HugeDamage", sha256: hugeDamageObjectiveSHA256,
			input: sim.LuaObjectiveInput{ObjectiveID: 0x0478facb, HealthMultiplier: 1}},
		{name: "FinishLevelQuickly", sha256: finishObjectiveSHA256,
			input: sim.LuaObjectiveInput{ObjectiveID: 0xff9733ee}},
		{name: "NoSlowing", sha256: noSlowingObjectiveSHA256,
			input: sim.LuaObjectiveInput{ObjectiveID: 0xe2745fb5}},
		{name: "DefeatSameType", sha256: sameTypeObjectiveSHA256,
			input: sim.LuaObjectiveInput{ObjectiveID: 0xc0fbb81e}},
		{name: "DoDamageOften", sha256: damageObjectiveSHA256,
			input: sim.LuaObjectiveInput{ObjectiveID: 0xac4273f3}},
	}
	objectiveInitializers := make([]sim.Program, 0, len(objectiveDefinitions))
	objectiveInput := make([]sim.LuaObjectiveInput, 0, len(objectiveDefinitions))
	for index, definition := range objectiveDefinitions {
		objectiveInitializer, loadedInput, loadErr := loadLuaObjective(
			ctx, store, definition.name, definition.sha256, definition.input,
		)
		if loadErr != nil {
			return zonecontent.Programs{}, fmt.Errorf("objective[%d]: %w", index, loadErr)
		}
		objectiveInitializers = append(objectiveInitializers, objectiveInitializer)
		objectiveInput = append(objectiveInput, loadedInput)
	}
	return zonecontent.Programs{
		ChainLevel:             chainLevel,
		ArenaLevels:            arenaLevels,
		Critical:               critical,
		Difficulty:             difficulty,
		PlayerBasicAbility:     playerBasicAbility,
		PlayerBasicUnsupported: playerBasicUnsupported,
		HeroKits:               heroKits,
		NonPlayerHitPoint:      nonPlayerHitPoint,
		NonPlayerDefenses:      nonPlayerDefenses,
		NonPlayerCritical:      nonPlayerCritical,
		NounPhysics:            nounPhysics,
		NounPhysicsByID:        nounPhysicsByID,
		NPCDeathAnimations:     npcDeathAnimations,
		ProjectileHalfExtent:   projectileHalfExtent,
		QuadraFirstAggro:       program,
		LightningBasic:         lightningBasic,
		ElectronSphere:         electronSphere,
		SupportHealerBasic:     supportHealerBasic,
		SupportHealerPassive:   supportHealerPassiveDefinition,
		SupportHealerPetBasic:  supportHealerPetBasic,
		SentryDroneLaser:       sentryDroneLaser,
		FireTempestPetBasic:    fireTempestPetBasic,
		BeastPetBasic:          beastPetBasic,
		PlasmaSentinelPetBasic: plasmaSentinelPetBasic,
		PoisonMelee:            poisonMelee,
		PoisonCloud:            poisonCloud,
		PlasmaLightning:        plasmaLightning,
		TailZap:                tailZap,
		BurstShot:              burstShot,
		InteractWithObelisk:    interactWithObelisk,
		InteractHealthObelisk:  interactHealthObelisk,
		SecurityTeleporter:     securityTeleporter,
		TeleporterModifier:     teleporterModifier,
		BossTeleporter:         bossTeleporter,
		InvisibleBehavior:      invisibleLifecycle,
		SpawnModifier:          spawnLifecycle,
		SoloSupportUnlock:      soloSupportUnlock,
		SupportUnlock:          supportUnlock,
		OverdriveUnlock:        overdriveUnlock,
		CatalystUnlock:         catalystUnlock,
		CrystalPickup:          crystalPickup,
		ObjectiveInitializers:  objectiveInitializers,
		ObjectiveInput:         objectiveInput,
		CrystalDefinitions:     crystalDefinitions,
		CrystalLevelOffsets:    crystalLevelOffsets,
		IntroAbilitySecond: zonecontent.MarkerProgram{
			Program: abilitySecondProgram,
			Trigger: sim.MarkerTrigger{
				LevelID: abilitySecondJob.LevelID, MarkerID: abilitySecondJob.MarkerID,
				Center: sim.Position{
					X: abilitySecondJob.PositionX, Y: abilitySecondJob.PositionY, Z: abilitySecondJob.PositionZ,
				},
				Radius: abilitySecondJob.Radius, IsTriggerOnceOnly: abilitySecondJob.IsTriggerOnceOnly,
				IsServerOnly: abilitySecondJob.IsServerOnly,
			},
			CallbackName: abilitySecondJob.CallbackName,
		},
		IntroHealthAndPower: sim.MarkerTrigger{
			LevelID: healthAndPowerJob.LevelID, MarkerID: healthAndPowerJob.MarkerID,
			Center: sim.Position{
				X: healthAndPowerJob.PositionX, Y: healthAndPowerJob.PositionY,
				Z: healthAndPowerJob.PositionZ,
			},
			Radius:            healthAndPowerJob.Radius,
			IsTriggerOnceOnly: healthAndPowerJob.IsTriggerOnceOnly,
			IsServerOnly:      healthAndPowerJob.IsServerOnly,
		},
		IntroOverdrive: zonecontent.MarkerProgram{
			Program: overdriveProgram,
			Trigger: sim.MarkerTrigger{
				LevelID: overdriveJob.LevelID, MarkerID: overdriveJob.MarkerID,
				Center: sim.Position{
					X: overdriveJob.PositionX, Y: overdriveJob.PositionY, Z: overdriveJob.PositionZ,
				},
				Radius: overdriveJob.Radius, IsTriggerOnceOnly: overdriveJob.IsTriggerOnceOnly,
				IsServerOnly: overdriveJob.IsServerOnly,
			},
			CallbackName: overdriveJob.CallbackName,
		},
		IntroSecondCreature: zonecontent.MarkerProgram{
			Program: secondCreatureProgram,
			Trigger: sim.MarkerTrigger{
				LevelID: secondCreatureJob.LevelID, MarkerID: secondCreatureJob.MarkerID,
				Center: sim.Position{
					X: secondCreatureJob.PositionX, Y: secondCreatureJob.PositionY, Z: secondCreatureJob.PositionZ,
				},
				Radius:            secondCreatureJob.Radius,
				IsTriggerOnceOnly: secondCreatureJob.IsTriggerOnceOnly,
				IsServerOnly:      secondCreatureJob.IsServerOnly,
			},
			CallbackName: secondCreatureJob.CallbackName,
		},
	}, nil
}

func loadPlayerBasicAbilities(
	ctx context.Context, store *contentstore.Store, templates []contentstore.CreatureTemplate,
) (map[uint32]sim.AbilityDefinition, map[uint32]string, error) {
	if ctx == nil {
		return nil, nil, errors.New("nil context")
	}
	if store == nil {
		return nil, nil, errors.New("nil content store")
	}
	definitionByName := make(map[string]sim.AbilityDefinition, len(templates))
	compileErrorByName := make(map[string]error)
	definitionByNoun := make(map[uint32]sim.AbilityDefinition, len(templates))
	unsupportedByNoun := make(map[uint32]string)
	for index, template := range templates {
		if template.ID == 0 || template.AbilityBasicAsset == "" {
			return nil, nil, fmt.Errorf("templateIdentity[%d]: missing", index)
		}
		definition, isFound := definitionByName[template.AbilityBasicAsset]
		if !isFound {
			compiledDefinition, compileErr := loadLuaAbilityDefinition(ctx, store, template.AbilityBasicAsset)
			if compileErr != nil {
				compileErrorByName[template.AbilityBasicAsset] = compileErr
				definitionByName[template.AbilityBasicAsset] = sim.AbilityDefinition{}
			} else {
				definition = compiledDefinition
				definitionByName[template.AbilityBasicAsset] = definition
			}
		}
		if definition.Name == "" {
			compileErr := compileErrorByName[template.AbilityBasicAsset]
			if compileErr == nil {
				return nil, nil, fmt.Errorf("abilityMissing[%s]: no definition", template.AbilityBasicAsset)
			}
			unsupportedByNoun[template.ID] = compileErr.Error()
			continue
		}
		// Pummel is Wraith's authored arc melee basic. Some indexed Lua
		// variants omit the inherited hitArcLength key from the materialized
		// table, which otherwise leaves the recovered definition classified as
		// a point-blank special and disables index-zero attacks at runtime.
		if definition.Name == "Pummel" {
			definition.Kind = sim.AbilityKindMelee
			// The point-blank classification can also return before recovering
			// Pummel's authored hitEffect preload. Restore its impact event so
			// accepted hits produce the packaged visual and audio presentation.
			if definition.HitEffectName == "" {
				definition.HitEffectName = "necro_common_hit_large_player.ServerEventDef"
			}
		}
		// EnergySentinelBasic is Goliath's authored sword melee basic. Some
		// indexed Lua variants omit its inherited melee discriminator from the
		// materialized table, which otherwise rejects every basic attack before
		// targeting and damage as an unsupported runtime shape.
		if definition.Name == "EnergySentinelBasic" {
			definition.Kind = sim.AbilityKindMelee
			definition.HitEffectName = "cyber_energy_sword_hit.ServerEventDef"
			definition.AnimationAdditionalTargetCounts = []uint32{1, 1, 0}
			definition.AnimationDamageMultipliers = []float32{1, 1, 2}
			definition.AdditionalTargetRange = 6
			definition.AdditionalTargetAngle = 180
			definition.RootModifierID = util.HashID("EnergySentinelHealDebuff")
			definition.StatusDuration = 3 * time.Second
		}
		if definition.Name == "SplinteringCleave" {
			definition.Kind = sim.AbilityKindMelee
			definition.AnimationAdditionalTargetCounts = []uint32{1, 1, 1, 0}
			definition.AnimationDamageMultipliers = []float32{1, 1, 1, 2}
			definition.AdditionalTargetRange = 6
			definition.AdditionalTargetAngle = 180
		}
		if definition.Name == "BinarySentinelBasic" {
			definition.Kind = sim.AbilityKindMelee
			definition.AnimationDamageMultipliers = []float32{1, 1, 2}
		}
		if definition.Name == "LFPoisonRavager_Stab" {
			definition.Kind = sim.AbilityKindMelee
			definition.AnimationDamageMultipliers = []float32{1, 1, 1, 1, 1, 2}
		}
		if definition.Name == "QuantumStrike" {
			definition.Kind = sim.AbilityKindMelee
			definition.RandomMeleeDamagePolicies = []sim.MeleeDamagePolicy{
				{
					MinimumMultiplier: 0.33,
					MaximumMultiplier: 1,
					EffectName:        "spacetime_quantum_bite.ServerEventDef",
				},
				{
					MinimumMultiplier: 0.33,
					MaximumMultiplier: 0.67,
					EffectName:        "spacetime_large_slash_effect.ServerEventDef",
				},
				{
					MinimumMultiplier: 0.67,
					MaximumMultiplier: 1,
					EffectName:        "spacetime_lightspeed_quantum_hit_effect.ServerEventDef",
				},
			}
		}
		// TimeRavagerBasic is Vex's authored melee combo. Its nested fifth
		// animation preserves two hit timestamps, but the indexed materialized
		// table can omit the inherited melee discriminator and leave the whole
		// basic classified as an unsupported runtime shape.
		if definition.Name == "TimeRavagerBasic" {
			definition.Kind = sim.AbilityKindMelee
		}
		if definition.Name == lightspeedBasicName {
			definition.AnimationProjectileOffsets = [][]sim.Position{
				{{X: 0.65, Z: 0.4}},
				{{X: -0.65, Z: 0.4}},
				{{X: 0.65, Z: 0.4}, {X: -0.65, Z: 0.4}},
			}
		}
		// FireRavagerBasic authors three simultaneous muzzle launches with a
		// narrow spread. The indexed Lua decoder does not yet materialize the
		// parallel offset and angle arrays, so retain that recovered content at
		// the catalog boundary instead of collapsing Krel's basic to one shot.
		if definition.Name == "FireRavagerBasic" {
			projectileOffsets := []sim.Position{
				{X: 0.4, Y: -1, Z: 1},
				{X: -0.4, Y: -1, Z: 1},
				{X: 0, Y: -1, Z: 1.2},
			}
			projectileAngles := []float32{-15, 15, 0}
			definition.AnimationProjectileOffsets = make(
				[][]sim.Position, len(definition.AnimationNames),
			)
			definition.AnimationProjectileAngles = make(
				[][]float32, len(definition.AnimationNames),
			)
			for animationIndex := range definition.AnimationNames {
				definition.AnimationProjectileOffsets[animationIndex] = append(
					[]sim.Position(nil), projectileOffsets...,
				)
				definition.AnimationProjectileAngles[animationIndex] = append(
					[]float32(nil), projectileAngles...,
				)
			}
		}
		definition = applyRecoveredAreaRadiusPolicy(definition)
		definition = applyRecoveredAreaDurationPolicy(definition)
		// FieldMedicBasic resolves both damage bounds through GetWeaponDamage at
		// impact. Its projectile template contributes a nonzero inherited damage
		// table, so the ordinary missing-range fallback cannot detect that authored
		// weapon contract.
		if definition.Name == "FieldMedicBasic" ||
			(definition.MinimumDamage == 0 && definition.MaximumDamage == 0) {
			definition.MinimumDamage = float32(template.WeaponMinDamage)
			definition.MaximumDamage = float32(template.WeaponMaxDamage)
			definition.IsWeaponDamageRange = true
		}
		if _, isDuplicate := definitionByNoun[template.ID]; isDuplicate {
			return nil, nil, fmt.Errorf("templateDuplicate[%d]: %#x", index, template.ID)
		}
		definitionByNoun[template.ID] = definition
	}
	return definitionByNoun, unsupportedByNoun, nil
}

func loadHeroKits(
	ctx context.Context, store *contentstore.Store, templates []contentstore.CreatureTemplate,
	basicsByNoun map[uint32]sim.AbilityDefinition, basicErrorsByNoun map[uint32]string,
	recoveredDefinitions map[string]sim.AbilityDefinition,
) (map[uint32]zonecontent.HeroKit, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if store == nil {
		return nil, errors.New("nil content store")
	}
	kitsByNoun := make(map[uint32]zonecontent.HeroKit, len(templates))
	definitionsByAsset := make(map[string]sim.AbilityDefinition)
	compileErrorsByAsset := make(map[string]string)
	for index, template := range templates {
		if template.ID == 0 || template.Name == "" {
			return nil, fmt.Errorf("templateIdentity[%d]: missing", index)
		}
		if _, isDuplicate := kitsByNoun[template.ID]; isDuplicate {
			return nil, fmt.Errorf("templateDuplicate[%d]: %#x", index, template.ID)
		}
		kit := zonecontent.HeroKit{
			Noun: template.ID, Name: template.Name,
			Abilities: make(map[zonecontent.HeroAbilitySlot]zonecontent.HeroAbility, 5),
		}
		kit.Abilities[zonecontent.HeroAbilityPassive] = zonecontent.HeroAbility{
			ID: template.AbilityPassive, AssetName: template.AbilityPassiveAsset,
		}
		kit.Abilities[zonecontent.HeroAbilityBasic] = zonecontent.HeroAbility{
			ID: template.AbilityBasic, AssetName: template.AbilityBasicAsset,
			Definition: basicsByNoun[template.ID], CompileError: basicErrorsByNoun[template.ID],
		}
		activeSlots := []struct {
			slot      zonecontent.HeroAbilitySlot
			id        uint32
			assetName string
		}{
			{slot: zonecontent.HeroAbilityRandom, id: template.AbilityRandom, assetName: template.AbilityRandomAsset},
			{slot: zonecontent.HeroAbilitySpecialOne, id: template.AbilitySpecial1, assetName: template.AbilitySpecial1Asset},
			{slot: zonecontent.HeroAbilitySpecialTwo, id: template.AbilitySpecial2, assetName: template.AbilitySpecial2Asset},
		}
		for _, active := range activeSlots {
			ability := zonecontent.HeroAbility{ID: active.id, AssetName: active.assetName}
			if active.id == 0 || active.assetName == "" {
				ability.CompileError = "missing authored identity"
				kit.Abilities[active.slot] = ability
				continue
			}
			definition, isDefinitionKnown := definitionsByAsset[active.assetName]
			compileError, isErrorKnown := compileErrorsByAsset[active.assetName]
			if !isDefinitionKnown && !isErrorKnown {
				compiledDefinition, compileErr := loadLuaAbilityDefinition(ctx, store, active.assetName)
				if compileErr != nil {
					definition, isDefinitionKnown = recoveredDefinitions[active.assetName]
					if !isDefinitionKnown {
						compileError = compileErr.Error()
						compileErrorsByAsset[active.assetName] = compileError
					} else {
						definitionsByAsset[active.assetName] = definition
					}
				} else {
					definition = compiledDefinition
					definitionsByAsset[active.assetName] = definition
				}
			}
			// These custom callbacks expose a generic modifier-shaped registration,
			// but their actual payload is owned by a recovered specialized runtime.
			switch active.assetName {
			case "RootingPlague", "Sporogenesis", "FieldMedicSupport",
				"Psistorm", "SleepingCloud", "SoulRavagerActive", "SpacetimeRandom2",
				"Terrify", "TimeRavagerSupport", "TurretTrap", "RepulsionWave",
				"GravityStorm", "QuantumBlink", "QuantumState",
				"PlasmaRandom_WebbedLightning",
				"SoulRavagerSupport", "AfflictionBolt", "BeastCharge", "BioRandom2",
				"SummonSprite", "SpacetimeRandom3", "LightspeedTempestActive",
				"EntanglingRush", "PhantomCharge":
				recovered, isRecovered := recoveredDefinitions[active.assetName]
				if isRecovered {
					definition = recovered
					compileError = ""
					definitionsByAsset[active.assetName] = recovered
				}
			}
			if active.assetName == "PlasmaRandom" && definition.HitEffectName == "" {
				recovered, isRecovered := recoveredDefinitions[active.assetName]
				if isRecovered {
					definition = recovered
					compileError = ""
					definitionsByAsset[active.assetName] = recovered
				}
			}
			if active.assetName == "ShadowRavagerActive" {
				// Shadow Sting inherits a target modifier alongside its melee arc. The
				// generic Lua modifier discriminator sees that field first, so restore
				// the authored strike shape and impact event at the catalog boundary.
				definition.Kind = sim.AbilityKindMelee
				definition.HitEffectName =
					"shadow_sting_explode_effect.ServerEventDef"
				definition.StatusKind = sim.AbilityStatusKindPhysicalVulnerability
				definition.StatusDuration = 6 * time.Second
				definition.StatusPhysicalIncrease = 0.50
				definition.RootModifierID = util.HashID(
					"ShadowRavagerPhysicalVulnerabilityModifier",
				)
			}
			definition = applyRecoveredAreaRadiusPolicy(definition)
			definition = applyRecoveredAreaDurationPolicy(definition)
			definitionsByAsset[active.assetName] = definition
			ability.Definition = definition
			ability.CompileError = compileError
			kit.Abilities[active.slot] = ability
		}
		kitsByNoun[template.ID] = kit
	}
	return kitsByNoun, nil
}

func loadLuaObjective(
	ctx context.Context, store *contentstore.Store, name string, sha256 string,
	input sim.LuaObjectiveInput,
) (sim.Program, sim.LuaObjectiveInput, error) {
	roots, err := store.LuaChunksByStringConstant(ctx, name)
	if err != nil {
		return sim.Program{}, sim.LuaObjectiveInput{}, fmt.Errorf("objectiveChunk[%s]: %w", name, err)
	}
	var root *contentstore.LuaChunk
	for _, candidate := range roots {
		if candidate.SHA256 != sha256 {
			continue
		}
		if root != nil {
			return sim.Program{}, sim.LuaObjectiveInput{},
				fmt.Errorf("objectiveHash[%s]: duplicate %s", name, sha256)
		}
		root = candidate
	}
	if root == nil {
		return sim.Program{}, sim.LuaObjectiveInput{},
			fmt.Errorf("objectiveHash[%s]: missing %s", name, sha256)
	}
	storedModules, err := store.LuaModules(ctx, root.ID)
	if err != nil {
		return sim.Program{}, sim.LuaObjectiveInput{},
			fmt.Errorf("objectiveModules[%s]: %w", name, err)
	}
	input.Modules = make(map[string]sim.LuaBytecode, len(storedModules))
	for moduleName, module := range storedModules {
		input.Modules[moduleName] = sim.LuaBytecode{
			ChunkID: module.ID, SHA256: module.SHA256, Contents: module.Bytecode,
		}
	}
	input.Root = sim.LuaBytecode{ChunkID: root.ID, SHA256: root.SHA256, Contents: root.Bytecode}
	program, err := sim.CompileLuaObjective(input)
	if err != nil {
		return sim.Program{}, sim.LuaObjectiveInput{},
			fmt.Errorf("objectiveCompile[%s]: %w", name, err)
	}
	return program, input, nil
}

func loadPinnedPlayerLuaJob(
	ctx context.Context, store *contentstore.Store, chunkID int64, sha256 string,
	entryGlobal string, operation string,
) (sim.Program, error) {
	root, err := store.LuaChunk(ctx, chunkID)
	if err != nil {
		return sim.Program{}, fmt.Errorf("%sChunk: %w", operation, err)
	}
	if root.SHA256 != sha256 {
		return sim.Program{}, fmt.Errorf("%sHash: got %s", operation, root.SHA256)
	}
	program, err := sim.CompileLuaJob(sim.LuaJobInput{
		Root: sim.LuaBytecode{
			ChunkID: root.ID, SHA256: root.SHA256, Contents: root.Bytecode,
		},
		EntryGlobal: entryGlobal, PlayerRoles: []sim.Role{zoneunlock.PlayerRole},
		PreloadedModules: luaJobPreloadedModules, SourceRole: "directorSource",
		InitiatingRole: zoneunlock.PlayerRole, InitiatingPlayerIndex: 0,
		IsLevelBeaten: []bool{false},
	})
	if err != nil {
		return sim.Program{}, fmt.Errorf("%sCompile: %w", operation, err)
	}
	return program, nil
}

func simulationContentDuration(seconds float32) time.Duration {
	return time.Duration(float64(seconds) * float64(time.Second))
}

func loadLuaAbilityDefinition(
	ctx context.Context, store *contentstore.Store, abilityName string,
) (sim.AbilityDefinition, error) {
	roots, err := store.LuaChunksByStringConstant(ctx, abilityName)
	if err != nil {
		return sim.AbilityDefinition{}, fmt.Errorf("rootLookup: %w", err)
	}
	var definition sim.AbilityDefinition
	matchCount := 0
	compileError := make([]error, 0)
	for index, root := range roots {
		storedModules, loadErr := store.LuaModules(ctx, root.ID)
		if loadErr != nil {
			return sim.AbilityDefinition{}, fmt.Errorf("moduleLoad[%d]: %w", index, loadErr)
		}
		loadErr = addAbilityDefinitionFallbacks(ctx, store, abilityName, storedModules)
		if loadErr != nil {
			compileError = append(compileError,
				fmt.Errorf("candidateFallback[%d/%d]: %w", index, root.ID, loadErr))
			continue
		}
		if abilityName == lightspeedBasicName {
			loadErr = addLightspeedDefinitionFallback(ctx, store, storedModules)
			if loadErr != nil {
				compileError = append(compileError,
					fmt.Errorf("candidateFallback[%d/%d]: %w", index, root.ID, loadErr))
				continue
			}
		}
		modules := make(map[string]sim.LuaBytecode, len(storedModules))
		for name, module := range storedModules {
			modules[name] = sim.LuaBytecode{
				ChunkID: module.ID, SHA256: module.SHA256, Contents: module.Bytecode,
			}
		}
		candidate, compileErr := sim.CompileLuaAbilityDefinition(sim.LuaAbilityInput{
			Root: sim.LuaBytecode{
				ChunkID: root.ID, SHA256: root.SHA256, Contents: root.Bytecode,
			},
			Modules: modules, Role: "contentActor",
		})
		if compileErr != nil {
			compileError = append(compileError, fmt.Errorf("candidateCompile[%d/%d]: %w", index, root.ID, compileErr))
			continue
		}
		if candidate.Name != abilityName {
			continue
		}
		definition = candidate
		matchCount++
	}
	if matchCount == 0 && len(compileError) != 0 {
		return sim.AbilityDefinition{}, fmt.Errorf("definitionMissing[%s]: %w", abilityName, errors.Join(compileError...))
	}
	if matchCount == 0 {
		return sim.AbilityDefinition{}, fmt.Errorf("definitionMissing: %s", abilityName)
	}
	if matchCount != 1 {
		return sim.AbilityDefinition{}, fmt.Errorf("definitionAmbiguous[%s]: %d", abilityName, matchCount)
	}
	return definition, nil
}

func addAbilityDefinitionFallbacks(
	ctx context.Context, store *contentstore.Store, abilityName string,
	modules map[string]contentstore.LuaChunk,
) error {
	switch abilityName {
	case "CastEnrage", "RootingPlague", "Sporogenesis", "Terrify":
		err := addPinnedDefinitionModule(
			ctx, store, modules, instantCastTemplateModule,
			instantCastTemplateChunkID, instantCastTemplateSHA256,
		)
		if err != nil {
			return fmt.Errorf("instantCast: %w", err)
		}
	}
	return nil
}

func addPinnedDefinitionModule(
	ctx context.Context, store *contentstore.Store,
	modules map[string]contentstore.LuaChunk, moduleName string,
	chunkID int64, sha256 string,
) error {
	if _, isFound := modules[moduleName]; isFound {
		return nil
	}
	module, err := store.LuaChunk(ctx, chunkID)
	if err != nil {
		return fmt.Errorf("moduleChunk: %w", err)
	}
	if module.SHA256 != sha256 {
		return fmt.Errorf("moduleHash: got %s", module.SHA256)
	}
	dependencies, err := store.LuaModules(ctx, module.ID)
	if err != nil {
		return fmt.Errorf("moduleDependencies: %w", err)
	}
	for dependencyName, dependency := range dependencies {
		existing, isExisting := modules[dependencyName]
		if isExisting && existing.ID != dependency.ID {
			return fmt.Errorf(
				"moduleConflict[%s]: %d/%d", dependencyName, existing.ID, dependency.ID,
			)
		}
		modules[dependencyName] = dependency
	}
	modules[moduleName] = *module
	return nil
}

// addLightspeedDefinitionFallback supplies the one absent build-103 aggregator
// only while compiling Orion's basic definition. The concrete Orion counter
// and its generic template are shipped content; keeping this substitution out
// of content.db prevents the unrelated Citadel require from inheriting it.
func addLightspeedDefinitionFallback(
	ctx context.Context, store *contentstore.Store, modules map[string]contentstore.LuaChunk,
) error {
	counter, err := store.LuaChunk(ctx, lightspeedCounterChunkID)
	if err != nil {
		return fmt.Errorf("counterChunk: %w", err)
	}
	if counter.SHA256 != lightspeedCounterSHA256 {
		return fmt.Errorf("counterHash: got %s", counter.SHA256)
	}
	counterModules, err := store.LuaModules(ctx, counter.ID)
	if err != nil {
		return fmt.Errorf("counterModules: %w", err)
	}
	for name, module := range counterModules {
		existing, isExisting := modules[name]
		if isExisting && existing.ID != module.ID {
			return fmt.Errorf("moduleConflict[%s]: %d/%d", name, existing.ID, module.ID)
		}
		modules[name] = module
	}
	modules[lightspeedChainModule] = *counter
	return nil
}

func loadPinnedLuaBytecode(
	ctx context.Context, store *contentstore.Store, chunkID int64, sha256 string, operation string,
) (sim.LuaBytecode, error) {
	chunk, err := store.LuaChunk(ctx, chunkID)
	if err != nil {
		return sim.LuaBytecode{}, fmt.Errorf("%sChunk: %w", operation, err)
	}
	if chunk.SHA256 != sha256 {
		return sim.LuaBytecode{}, fmt.Errorf("%sHash: got %s", operation, chunk.SHA256)
	}
	return sim.LuaBytecode{
		ChunkID: chunk.ID, SHA256: chunk.SHA256, Contents: chunk.Bytecode,
	}, nil
}
