package npc

import (
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

type OrcusSpawnDefinition struct {
	ServantNoun         string
	MaximumServant      int
	ServantCountPerCast int
	FirstSpawnDelay     time.Duration
	SpawnInterval       time.Duration
	Cooldown            time.Duration
	AnimationName       string
	ServantProfile      game.CampaignNPCProfile
	ServantAction       ActionProfile
}

func OrcusSpawnProfile(nounName string) (OrcusSpawnDefinition, bool) {
	servantNoun := ""
	maximumServant := 0
	hitPoint := float32(0)
	movementSpeed := float32(0)
	nonCombatMovementSpeed := float32(0)
	switch strings.ToLower(nounName) {
	case "verdanthboss.noun":
		servantNoun, maximumServant = "Verdanth_Boss_Spawn.Noun", 10
		hitPoint, movementSpeed, nonCombatMovementSpeed = 20, 6, 3
	case "verdanthboss_2.noun":
		servantNoun, maximumServant = "Verdanth_Boss_Spawn2.Noun", 15
		hitPoint, movementSpeed, nonCombatMovementSpeed = 30, 9, 4
	case "verdanthboss_3.noun":
		servantNoun, maximumServant = "Verdanth_Boss_Spawn3.Noun", 20
		hitPoint, movementSpeed, nonCombatMovementSpeed = 40, 12, 5
	default:
		return OrcusSpawnDefinition{}, false
	}
	return OrcusSpawnDefinition{
		ServantNoun: servantNoun, MaximumServant: maximumServant,
		ServantCountPerCast: 3, FirstSpawnDelay: 769999981 * time.Nanosecond,
		SpawnInterval: 1370000005 * time.Nanosecond, Cooldown: 18 * time.Second,
		AnimationName: "boss_lf_spawneater_attack1",
		ServantProfile: game.CampaignNPCProfile{
			IsTargetable: true, HitPoint: hitPoint, PowerPoint: 75,
			Strength: 10, Dexterity: 10, Mind: 10, CriticalRating: 5,
			GraphicsScale: 1, FootprintRadius: 0.75, IsKnown: true,
		},
		ServantAction: ActionProfile{
			Family: ActionMelee, AbilityName: "VerdanthBossMinionMelee",
			AnimationName:           "cast_tailzap",
			FirstAggroAnimationName: "ver_minn_lf_spawneater_minion_spawn",
			FirstAggroEffectName:    "burrower_burrowIn.ServerEventDef",
			FirstAggroDelay:         1629999995 * time.Nanosecond,
			HitDelay:                430000007 * time.Nanosecond,
			ReleaseDelay:            1429999948 * time.Nanosecond,
			Cooldown:                2 * time.Second,
			Range:                   0.75,
			MovementSpeed:           movementSpeed,
			NonCombatMovementSpeed:  nonCombatMovementSpeed,
			MinimumDamage:           2, MaximumDamage: 5,
			DescriptorMask: 67, DamageType: 2, DamageSource: 0,
			ImpactEffectName:          "life_common_melee_hit.ServerEventDef",
			IsDamageProfileKnown:      true,
			IsFirstAggroDurationKnown: true,
		},
	}, true
}

func OrcusFallbackProfile(nounName string) (ActionProfile, bool) {
	movementSpeed := float32(0)
	nonCombatMovementSpeed := float32(0)
	switch strings.ToLower(nounName) {
	case "verdanthboss.noun":
		movementSpeed, nonCombatMovementSpeed = 7, 5.5
	case "verdanthboss_2.noun":
		movementSpeed, nonCombatMovementSpeed = 10.5, 5.5
	case "verdanthboss_3.noun":
		movementSpeed, nonCombatMovementSpeed = 14, 5.5
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionNomadDrone, AbilityName: "VerdanthBossFallbackMelee",
		AnimationName: "attack", HitDelay: 466667 * time.Microsecond,
		ReleaseDelay: 1500 * time.Millisecond, Cooldown: 1500 * time.Millisecond,
		Range: 2.25, MovementSpeed: movementSpeed,
		NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage:          5, MaximumDamage: 8,
		FirstAggroAnimationName:     "boss_lf_spawneater_aggro",
		FirstAggroDelay:             9199999929 * time.Nanosecond,
		FirstAggroCinematicDuration: 9199999929 * time.Nanosecond,
		FirstAggroCinematicRadius:   100,
		IsFirstAggroDurationKnown:   true,
	}, true
}

func OrcusGroundSlamProfile(nounName string) (ActionProfile, bool) {
	fallback, isFound := OrcusFallbackProfile(nounName)
	if !isFound {
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionCone, AbilityName: "VerdanthBoss_GroundSlam",
		AnimationName: "boss_lf_groundslam",
		HitDelay:      2450 * time.Millisecond, ReleaseDelay: 6 * time.Second,
		Cooldown: 25 * time.Second, Range: 5,
		MovementSpeed:          fallback.MovementSpeed,
		NonCombatMovementSpeed: fallback.NonCombatMovementSpeed,
		MinimumDamage:          15, MaximumDamage: 20,
		DescriptorMask: 72, DamageType: 2, DamageSource: 0,
		ImpactEffectName: "life_common_melee_hit_pc.ServerEventDef",
		Radius:           10, Angle: 360, IsDamageProfileKnown: true,
	}, true
}

func OrcusDiseaseConeProfile(nounName string) (ActionProfile, bool) {
	fallback, isFound := OrcusFallbackProfile(nounName)
	if !isFound {
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionCone, AbilityName: "VerdanthBossDiseaseCone",
		AnimationName: "boss_lf_spawneater_attack2",
		HitDelay:      2 * time.Second, ReleaseDelay: 4470 * time.Millisecond,
		Cooldown: 15 * time.Second, Range: 5,
		MovementSpeed:          fallback.MovementSpeed,
		NonCombatMovementSpeed: fallback.NonCombatMovementSpeed,
		ModifierName:           "VerdanthBossPrimaryPlague",
		ModifierDuration:       6 * time.Second, ModifierChance: 100,
		ModifierTickDuration:      time.Second,
		ModifierMinimumTickDamage: 8, ModifierMaximumTickDamage: 8,
		ModifierTickDamageCoefficient: 0.05,
		ModifierDescriptorMask:        36, ModifierDamageType: 2,
		ModifierDamageSource: 1, ModifierMaximumStack: 1,
		TargetEffectName: "status_diseased.ServerEventDef",
		Radius:           10, Angle: 55, IsModifierDamageProfileKnown: true,
	}, true
}
