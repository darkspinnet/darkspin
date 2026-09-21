package npc

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

type ActionFamily uint8

const (
	ActionUnknown ActionFamily = iota
	ActionZelemRanged
	ActionZelemHaster
	ActionNomadSnipe
	ActionNomadDrone
	ActionProjectile
	ActionRetainedArea
	ActionPushPull
	ActionChannelDrain
	ActionResurrect
	ActionCharge
	ActionCone
	ActionLeap
	ActionMelee
	ActionDetonate
)

type ActionProfile struct {
	FirstAggroAbilityName            string
	IsFirstAggroFacingSuppressed     bool
	IsFacingSuppressed               bool
	IsFacingPolicyKnown              bool
	Family                           ActionFamily
	AbilityName                      string
	AnimationName                    string
	NearAnimationName                string
	FarAnimationName                 string
	LoopAnimationName                string
	EndAnimationName                 string
	RecoveryAnimationName            string
	PreAggroAnimationName            string
	FirstAggroAnimationName          string
	FirstAggroEffectName             string
	FirstAggroEffectDelay            time.Duration
	FirstAggroDelay                  time.Duration
	FirstAggroRevealDelay            time.Duration
	FirstAggroCinematicDuration      time.Duration
	FirstAggroCinematicRadius        float32
	HitDelay                         time.Duration
	ReleaseDelay                     time.Duration
	Cooldown                         time.Duration
	RecoveryDuration                 time.Duration
	RecoveryEveryActionCount         uint32
	Range                            float32
	MinimumRange                     float32
	MovementSpeed                    float32
	NonCombatMovementSpeed           float32
	MovementSpeedBuff                float32
	MinimumDamage                    float32
	MaximumDamage                    float32
	MinimumDamagePercent             float32
	DamageCoefficient                float32
	DescriptorMask                   uint32
	DamageType                       uint8
	DamageSource                     uint8
	ModifierName                     string
	ModifierID                       uint32
	ModifierDuration                 time.Duration
	ModifierChance                   uint32
	ModifierPhysicalHitCount         uint32
	ModifierPhysicalDamageIncrease   float32
	ModifierTickDuration             time.Duration
	ModifierTickDamage               float32
	ModifierMinimumTickDamage        float32
	ModifierMaximumTickDamage        float32
	ModifierTickDamageCoefficient    float32
	ModifierDamageBuff               float32
	ModifierDescriptorMask           uint32
	ModifierDamageType               uint8
	ModifierDamageSource             uint8
	IsModifierDamageProfileKnown     bool
	ModifierMaximumStack             uint32
	MaximumTargetCount               uint32
	ProjectileNoun                   string
	TrailEffectName                  string
	SecondaryTrailEffectName         string
	SourceEffectName                 string
	SecondarySourceEffectName        string
	GroundEffectName                 string
	ImpactEffectName                 string
	PassiveEffectName                string
	PassiveCreateEffectName          string
	RevealEffectName                 string
	PassiveEnergyDefense             float32
	PassivePhysicalDefense           float32
	PassiveDamageOverTimeReduction   float32
	PassiveAbsorptionShield          float32
	PassiveAbsorptionShieldRecharge  time.Duration
	PassivePhysicalDamageReduction   float32
	PassiveMeleeDamageReflection     float32
	SelfDamageReduction              float32
	ProjectileExitEffectName         string
	EmergeEffectName                 string
	EmergeDelay                      time.Duration
	TargetEffectName                 string
	TickDuration                     time.Duration
	EndAnimationDelay                time.Duration
	NumberOfTicks                    uint32
	LifeSteal                        float32
	ManaDrainFraction                float32
	MaximumChannelDistance           float32
	HealFraction                     float32
	MinimumHealing                   float32
	MaximumHealing                   float32
	HealingCoefficient               float32
	MissEffectName                   string
	ProjectileSpeed                  float32
	ProjectileDistance               float32
	ProjectileHeight                 float32
	ProjectileCloseRange             float32
	ProjectileCloseHeight            float32
	ProjectileCloseSpeed             float32
	ProjectileFlightDuration         time.Duration
	ProjectileShotInterval           time.Duration
	ProjectileShotCount              uint32
	ProjectileShotDeadlines          []time.Duration
	ProjectileShotAngles             []float32
	HomingDelay                      time.Duration
	Radius                           float32
	Angle                            float32
	ProjectileOffset                 game.Vec3
	ProjectileOffsets                []game.Vec3
	ProjectileCloseOffset            game.Vec3
	ProjectileSpreadAngle            float32
	ProjectileMaximumLeadAngle       float32
	ProjectileGravity                float32
	SubmunitionCount                 uint32
	SubmunitionMinimumDamage         float32
	SubmunitionMaximumDamage         float32
	SubmunitionRadius                float32
	SubmunitionMinimumDistance       float32
	SubmunitionMaximumDistance       float32
	SubmunitionProjectileSpeed       float32
	SubmunitionProjectileHeight      float32
	SubmunitionTrailEffectName       string
	SubmunitionImpactEffectName      string
	RetainedObjectNoun               string
	RetainedEffectName               string
	TeleportMinimumDistance          float32
	TeleportNormalDistance           float32
	TeleportMaximumDistance          float32
	TeleportAnimationName            string
	TeleportAnimationDelay           time.Duration
	TeleportReactionName             string
	TeleportEffectName               string
	ForcedMovementSpeed              float32
	ForcedMovementDistance           float32
	ForcedMovementDuration           time.Duration
	ForcedMovementStopDistance       float32
	ForcedMovementReactionName       string
	ForcedMovementEffectName         string
	StealthType                      game.StealthType
	IsPull                           bool
	IsSelfTargeted                   bool
	IsDamageProfileKnown             bool
	IsFirstAggroDurationKnown        bool
	IsProjectileParallelVolley       bool
	IsProjectileTrackingBetweenShots bool
	IsProjectileLeadingTarget        bool
	IsProjectilePiercing             bool
	IsProjectileCloseOffsetXFound    bool
	IsProjectileCloseOffsetYFound    bool
	IsProjectileCloseOffsetZFound    bool
	IsSpawnStealthed                 bool
}

func (e ActionProfile) Clone() ActionProfile {
	e.ProjectileShotDeadlines = append(
		[]time.Duration(nil), e.ProjectileShotDeadlines...,
	)
	e.ProjectileShotAngles = append(
		[]float32(nil), e.ProjectileShotAngles...,
	)
	e.ProjectileOffsets = append([]game.Vec3(nil), e.ProjectileOffsets...)
	return e
}

func (e ActionProfile) IsEqual(other ActionProfile) bool {
	return reflect.DeepEqual(e, other)
}

func (e ActionProfile) ModifierGUID() uint32 {
	if e.ModifierID != 0 {
		return e.ModifierID
	}
	return util.HashID(e.ModifierName)
}

type FirstActionCommand struct {
	ObjectID              uint32
	NounName              string
	SourcePosition        game.Vec3
	ActorFootprintRadius  float32
	TargetObjectID        uint32
	TargetPosition        game.Vec3
	TargetFootprintRadius float32
}

type FirstActionPlan struct {
	ObjectID         uint32
	TargetObjectID   uint32
	ActionGeneration uint64
	SourcePosition   game.Vec3
	TargetPosition   game.Vec3
	Profile          ActionProfile
	IsPursuitNeeded  bool
}

func PlanFirstAction(command FirstActionCommand) (FirstActionPlan, bool, error) {
	profile, isFound := ActionProfileForNoun(command.NounName)
	if !isFound {
		return FirstActionPlan{}, false, nil
	}
	action, err := PlanActionWithProfile(command, profile)
	return action, true, err
}

func ActionProfileForPlan(plan SpawnPlan) (ActionProfile, bool) {
	if plan.IsActionKnown {
		return plan.ActionProfile, true
	}
	profile, isFound := ActionProfileForNoun(plan.NounName)
	if isFound {
		return profile, true
	}
	if plan.IsFixture || plan.NounName == "" {
		return ActionProfile{}, false
	}
	return fallbackActionProfile(plan.NounName), true
}

func fallbackActionProfile(nounName string) ActionProfile {
	nashiraProfile, isNashiraFound := NashiraShadowTossProfile(nounName)
	if isNashiraFound {
		return nashiraProfile
	}
	orcusProfile, isOrcusFound := OrcusFallbackProfile(nounName)
	if isOrcusFound {
		return orcusProfile
	}
	if isFallbackRangedNoun(nounName) {
		return ZelemRangedShotProfile()
	}
	profile := NomadSnipeMeleeProfile(nounName)
	profile.Family = ActionNomadDrone
	return profile
}

func isFallbackRangedNoun(nounName string) bool {
	name := strings.ToLower(nounName)
	for _, token := range []string{
		"bomber", "burster", "dactyl", "drakon", "gunner", "homing",
		"leucopod", "ranged", "shooter", "snipe", "striker",
	} {
		if strings.Contains(name, token) {
			return true
		}
	}
	return false
}

func FirstActionTimelineKey(objectID uint32) string {
	if objectID == 0 {
		return ""
	}
	return fmt.Sprintf("npc-first-presentation:%d", objectID)
}

func ReturnTimelineKey(objectID uint32) string {
	if objectID == 0 {
		return ""
	}
	return fmt.Sprintf("npc-return:%d", objectID)
}

func PlanActionWithProfile(
	command FirstActionCommand, profile ActionProfile,
) (FirstActionPlan, error) {
	if command.ObjectID == 0 || command.TargetObjectID == 0 ||
		!zonegeometry.IsFinite(command.SourcePosition) ||
		!zonegeometry.IsFinite(command.TargetPosition) {
		return FirstActionPlan{}, errors.New("npc action: invalid")
	}
	targetObjectID := command.TargetObjectID
	targetPosition := command.TargetPosition
	if profile.IsSelfTargeted {
		targetObjectID = command.ObjectID
		targetPosition = command.SourcePosition
	} else {
		stopDistance, err := zoneaction.NPCStopDistance(
			profile.Range, command.ActorFootprintRadius,
			command.TargetFootprintRadius,
		)
		if err != nil {
			return FirstActionPlan{}, fmt.Errorf("npcActionRange: %w", err)
		}
		profile.Range = stopDistance
	}
	deltaX := command.SourcePosition.X - targetPosition.X
	deltaY := command.SourcePosition.Y - targetPosition.Y
	deltaZ := command.SourcePosition.Z - targetPosition.Z
	distanceSquared := deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ
	return FirstActionPlan{
		ObjectID: command.ObjectID, TargetObjectID: targetObjectID,
		SourcePosition: command.SourcePosition, TargetPosition: targetPosition, Profile: profile,
		IsPursuitNeeded: !profile.IsSelfTargeted && distanceSquared >= profile.Range*profile.Range,
	}, nil
}

func NomadSnipeMeleeProfile(nounName string) ActionProfile {
	movementSpeed := float32(7)
	nonCombatMovementSpeed := float32(5.5)
	switch strings.ToLower(nounName) {
	case "nomadsnipe_2.noun", "nomadsnipe_captain_2.noun":
		movementSpeed = 10.5
	case "nomadsnipe_3.noun", "nomadsnipe_captain_3.noun":
		movementSpeed = 14
	}
	return ActionProfile{
		Family: ActionNomadSnipe, AbilityName: "NomadSnipe_Melee",
		AnimationName: "nomad_lieu_sp_4_attack1", HitDelay: 466667 * time.Microsecond,
		ReleaseDelay: 1500 * time.Millisecond, Cooldown: 1500 * time.Millisecond,
		Range: 1.5, MovementSpeed: movementSpeed,
		NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage:          5, MaximumDamage: 8,
	}
}

func ZelemHasterAttackProfile() ActionProfile {
	return zelemHasterAttackProfile(5.5, 4)
}

func zelemHasterAttackProfile(
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionZelemHaster, AbilityName: "ZelemHasterAttack",
		AnimationName: "zlm_lieu_sp_03_attack1", HitDelay: 330 * time.Millisecond,
		ReleaseDelay: time.Second, Cooldown: 3 * time.Second, Range: 14,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage: 5, MaximumDamage: 10, DamageCoefficient: 0.05,
		DescriptorMask: 1<<7 | 1<<13, DamageType: 1, DamageSource: 1,
		ProjectileNoun:   "Ability_Fireball.Noun",
		TrailEffectName:  "spacetime_lieu_haster_shot_effect.ServerEventDef",
		ImpactEffectName: "spacetime_bite.ServerEventDef",
		MissEffectName:   "ineffective_common_small.ServerEventDef",
		ProjectileSpeed:  25, ProjectileDistance: 50,
		ProjectileOffset: game.Vec3{X: 0.5, Y: 2.2, Z: 0.7},
	}
}

func ZelemRangedShotProfile() ActionProfile {
	return ActionProfile{
		Family: ActionZelemRanged, AbilityName: "ZelemBasicRanged",
		AnimationName: "zlm_minn_sp_1_attack1", HitDelay: 666667 * time.Microsecond,
		ReleaseDelay: 1066667 * time.Microsecond, Cooldown: 5 * time.Second, Range: 12.5,
		MovementSpeed: 5,
		MinimumDamage: 2, MaximumDamage: 6, DamageCoefficient: 0.05,
		ProjectileNoun:   "Ability_Fireball.Noun",
		TrailEffectName:  "spacetime_shot_effect.ServerEventDef",
		ImpactEffectName: "spacetime_bite.ServerEventDef",
		MissEffectName:   "ineffective_common_small.ServerEventDef",
		ProjectileSpeed:  8, ProjectileDistance: 30,
		ProjectileOffset: game.Vec3{Z: 1},
	}
}

func zelemBasicPackMeleeProfile(
	cooldown time.Duration, movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "ZelemBasicPackMeleeAttack",
		AnimationName:          "zlm_minn_sp_01_attack1",
		HitDelay:               530 * time.Millisecond,
		ReleaseDelay:           1300 * time.Millisecond,
		Cooldown:               cooldown,
		Range:                  1.125,
		Radius:                 1.6,
		MovementSpeed:          movementSpeed,
		NonCombatMovementSpeed: nonCombatMovementSpeed,
		MovementSpeedBuff:      0,
		MinimumDamage:          6, MaximumDamage: 9,
		DamageCoefficient:         0.05,
		DescriptorMask:            1 | 1<<1 | 1<<6,
		DamageType:                2,
		DamageSource:              0,
		ImpactEffectName:          "spacetime_bite.ServerEventDef",
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func nocturnaSpecialLeechProfile(
	cooldown time.Duration, attackRange float32, projectileSpeed float32,
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "NocturnaSpecialLeech",
		AnimationName: "nomad_lieu_su_3_attack1",
		HitDelay:      430 * time.Millisecond, ReleaseDelay: 1400 * time.Millisecond,
		Cooldown: cooldown, Range: attackRange,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage: 2, MaximumDamage: 6, DamageCoefficient: 0.05,
		DescriptorMask: 8320, DamageType: 4, DamageSource: 1,
		ProjectileNoun:   "Ability_Fireball.Noun",
		TrailEffectName:  "shadow_leech_bullet_effect.ServerEventDef",
		ImpactEffectName: "shadow_bolt_impact.ServerEventDef",
		MissEffectName:   "ineffective_common_small.ServerEventDef",
		ProjectileSpeed:  projectileSpeed, ProjectileDistance: 30,
		IsDamageProfileKnown: true, IsFirstAggroDurationKnown: true,
	}
}

func nocturnaBasicRangedSilenceProfile(
	cooldown time.Duration, attackRange float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "NocturnaBasicRangedSilence",
		AnimationName:          "nct_minn_su_x3_attack1",
		HitDelay:               333333 * time.Microsecond,
		ReleaseDelay:           1600 * time.Millisecond,
		Cooldown:               cooldown,
		Range:                  attackRange,
		MovementSpeed:          5.5,
		NonCombatMovementSpeed: 4,
		MinimumDamage:          4, MaximumDamage: 8, DamageCoefficient: 0.05,
		DescriptorMask: 8320, DamageType: 4, DamageSource: 1,
		ModifierName:     "NocturnaBasicRanged_SilenceModifier",
		ModifierDuration: 2 * time.Second,
		ProjectileNoun:   "Ability_Fireball.Noun",
		TrailEffectName:  "shadow_silence_bullet_basic_effect.ServerEventDef",
		ImpactEffectName: "shadow_bolt_impact.ServerEventDef",
		MissEffectName:   "ineffective_common_small.ServerEventDef",
		ProjectileSpeed:  2, ProjectileDistance: 30,
		IsDamageProfileKnown: true, IsFirstAggroDurationKnown: true,
	}
}

func noctHopperJumpProfile(
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionLeap, AbilityName: "NoctHopperJump",
		AnimationName:          "nct_minn_su_04_attack_intro",
		LoopAnimationName:      "nct_minn_su_04_attack_in_air",
		EndAnimationName:       "nct_minn_su_04_attack_landing",
		HitDelay:               533333 * time.Microsecond,
		ReleaseDelay:           1900 * time.Millisecond,
		Cooldown:               1125 * time.Millisecond,
		Range:                  15,
		MinimumRange:           6,
		Radius:                 4,
		MovementSpeed:          movementSpeed,
		NonCombatMovementSpeed: nonCombatMovementSpeed,
		ForcedMovementSpeed:    24,
		MinimumDamage:          5,
		MaximumDamage:          8,
		DescriptorMask:         128,
		DamageType:             4,
		DamageSource:           1,
		EndAnimationDelay:      700 * time.Millisecond,
		IsDamageProfileKnown:   true, IsFirstAggroDurationKnown: true,
	}
}

func stealtherMeleeProfile(
	cooldown time.Duration, hitCount uint32, damageIncrease float32,
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "StealtherMelee",
		AnimationName:          "nct_lieu_su_stealther_attack2",
		HitDelay:               300 * time.Millisecond,
		ReleaseDelay:           1430 * time.Millisecond,
		Cooldown:               cooldown,
		Range:                  1.125,
		MovementSpeed:          movementSpeed,
		NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage:          7, MaximumDamage: 12,
		DescriptorMask:                 1 | 1<<1 | 1<<6,
		DamageType:                     4,
		DamageSource:                   0,
		ImpactEffectName:               "shadow_bolt_impact.ServerEventDef",
		ModifierName:                   "PhysicalVulnerabilityModifier",
		ModifierPhysicalHitCount:       hitCount,
		ModifierPhysicalDamageIncrease: damageIncrease,
		IsDamageProfileKnown:           true,
		IsFirstAggroDurationKnown:      true,
	}
}

func StealtherStealthProfile(nounName string) (ActionProfile, bool) {
	radius := float32(0)
	fearDuration := time.Duration(0)
	movementSpeed := float32(0)
	nonCombatMovementSpeed := float32(0)
	switch strings.ToLower(nounName) {
	case "nct_lieu_su_stealther.noun", "nct_lieu_su_stealther_captain.noun":
		radius = 5
		fearDuration = 3 * time.Second
		movementSpeed, nonCombatMovementSpeed = 6, 4
	case "nct_lieu_su_stealther_2.noun", "nct_lieu_su_stealther_captain_2.noun":
		radius = 6.5
		fearDuration = 4 * time.Second
		movementSpeed, nonCombatMovementSpeed = 9, 5
	case "nct_lieu_su_stealther_3.noun", "nct_lieu_su_stealther_captain_3.noun":
		radius = 8
		fearDuration = 5 * time.Second
		movementSpeed, nonCombatMovementSpeed = 12, 6
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionLeap, AbilityName: "StealthAttack",
		AnimationName:             "nct_lieu_su_stealther_attack3",
		EndAnimationName:          "nct_lieu_su_stealther_attack1",
		HitDelay:                  240 * time.Millisecond,
		ReleaseDelay:              1500 * time.Millisecond,
		Cooldown:                  10 * time.Second,
		Range:                     15,
		MinimumRange:              5,
		MovementSpeed:             movementSpeed,
		NonCombatMovementSpeed:    nonCombatMovementSpeed,
		MinimumDamage:             18,
		MaximumDamage:             24,
		DescriptorMask:            1 | 1<<1 | 1<<6,
		DamageType:                4,
		DamageSource:              0,
		Radius:                    radius,
		ModifierName:              "FearNova",
		ModifierDuration:          fearDuration,
		StealthType:               game.StealthType(2),
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}, true
}

func nocturnaSpecialMunchMeleeProfile(
	cooldown time.Duration, movementSpeed float32,
	nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionCone, AbilityName: "NocturnaSpecialMunchMelee",
		AnimationName: "nct_lieu_su_03_attack1",
		HitDelay:      533333 * time.Microsecond,
		ReleaseDelay:  1800 * time.Millisecond,
		Cooldown:      cooldown,
		Range:         2.25,
		Radius:        3,
		Angle:         90,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage: 5, MaximumDamage: 8,
		DescriptorMask:            1 | 1<<1 | 1<<6,
		DamageType:                4,
		DamageSource:              0,
		ImpactEffectName:          "necro_common_hit_small_npc.ServerEventDef",
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func NocturnaSpecialMunchConsumeProfile(
	nounName string,
) (ActionProfile, bool) {
	var cooldown time.Duration
	var healing float32
	var damageBuff float32
	var movementSpeed float32
	var nonCombatMovementSpeed float32
	switch strings.ToLower(nounName) {
	case "nocturnaspecialmunch.noun", "nocturnaspecialmunch_captain.noun":
		cooldown, healing, damageBuff = 6*time.Second, 20, 0.25
		movementSpeed, nonCombatMovementSpeed = 6, 4.5
	case "nocturnaspecialmunch_2.noun", "nocturnaspecialmunch_captain_2.noun":
		cooldown, healing, damageBuff = 5*time.Second, 40, 0.4
		movementSpeed, nonCombatMovementSpeed = 9, 5.5
	case "nocturnaspecialmunch_3.noun", "nocturnaspecialmunch_captain_3.noun":
		cooldown, healing, damageBuff = 4*time.Second, 60, 0.5
		movementSpeed, nonCombatMovementSpeed = 12, 6.5
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		AbilityName:               "ConsumeCorpse",
		AnimationName:             "nct_lieu_su_03_consume",
		HitDelay:                  1400 * time.Millisecond,
		ReleaseDelay:              2133333 * time.Microsecond,
		Cooldown:                  cooldown,
		Range:                     8,
		MovementSpeed:             movementSpeed,
		NonCombatMovementSpeed:    nonCombatMovementSpeed,
		MinimumHealing:            healing,
		MaximumHealing:            healing,
		ModifierName:              "NocturnaSpecialMunchModifier",
		ModifierDuration:          30 * time.Second,
		ModifierMaximumStack:      5,
		ModifierDamageBuff:        damageBuff,
		TargetEffectName:          "status_enraged.ServerEventDef",
		IsFirstAggroDurationKnown: true,
	}, true
}

func nomadBioSpecialTwoSwipeProfile(
	cooldown time.Duration, movementSpeed float32,
	nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionCone, AbilityName: "NomadBioSpecialTwoSwipe",
		AnimationName: "nomad_lieu_lf_2_attack1",
		HitDelay:      433333 * time.Microsecond,
		ReleaseDelay:  1466667 * time.Microsecond,
		Cooldown:      cooldown,
		Range:         2.25,
		Radius:        3,
		Angle:         180,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage: 8, MaximumDamage: 16,
		DescriptorMask:            1 | 1<<1 | 1<<6,
		DamageType:                2,
		DamageSource:              0,
		MaximumTargetCount:        2,
		ImpactEffectName:          "nomad_lieu_lf_2_swipe_hit.ServerEventDef",
		PassiveEffectName:         "self_rez_aura_effect.ServerEventDef",
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func NomadBioSpecialTwoJumpProfile(nounName string) (ActionProfile, bool) {
	var cooldown time.Duration
	var attackRange float32
	var movementSpeed float32
	var nonCombatMovementSpeed float32
	switch strings.ToLower(nounName) {
	case "nomadbiospecialtwo.noun", "nomadbiospecialtwo_captain.noun":
		cooldown = 6 * time.Second
		attackRange = 20
		movementSpeed, nonCombatMovementSpeed = 7, 5
	case "nomadbiospecialtwo_2.noun", "nomadbiospecialtwo_captain_2.noun":
		cooldown = 5 * time.Second
		attackRange = 22.5
		movementSpeed, nonCombatMovementSpeed = 10.5, 6
	case "nomadbiospecialtwo_3.noun", "nomadbiospecialtwo_captain_3.noun":
		cooldown = 4 * time.Second
		attackRange = 25
		movementSpeed, nonCombatMovementSpeed = 14, 7
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionLeap, AbilityName: "NomadBioSpecialTwoJumpAttack",
		AnimationName:          "nomad_lieu_lf_2_attack1_intro",
		LoopAnimationName:      "nomad_lieu_lf_2_attack1_jump",
		EndAnimationName:       "nomad_lieu_lf_2_attack1_jump",
		HitDelay:               1900 * time.Millisecond,
		ReleaseDelay:           1533333 * time.Microsecond,
		Cooldown:               cooldown,
		Range:                  attackRange,
		Radius:                 3,
		MovementSpeed:          movementSpeed,
		NonCombatMovementSpeed: nonCombatMovementSpeed,
		ForcedMovementSpeed:    35,
		MinimumDamage:          12,
		MaximumDamage:          18,
		DescriptorMask:         1 | 1<<1 | 1<<6,
		DamageType:             2,
		DamageSource:           0,
		EndAnimationDelay:      1533333 * time.Microsecond,
		ImpactEffectName:       "nomad_lieu_lf_1_cleave_hit.ServerEventDef",
		IsDamageProfileKnown:   true, IsFirstAggroDurationKnown: true,
	}, true
}

func citadelBasicShieldMeleeProfile(
	cooldown time.Duration, shieldAmount float32, shieldRecharge time.Duration,
) ActionProfile {
	return ActionProfile{
		Family: ActionCone, AbilityName: "CitadelBasicShield_Melee",
		AnimationName:          "ctd_minn_tc_3_attack1",
		HitDelay:               730 * time.Millisecond,
		ReleaseDelay:           600 * time.Millisecond,
		Cooldown:               cooldown,
		Range:                  2.25,
		Radius:                 3,
		Angle:                  90,
		MovementSpeed:          4,
		NonCombatMovementSpeed: 2.5,
		MinimumDamage:          4, MaximumDamage: 7,
		DescriptorMask:                  1 | 1<<1 | 1<<6,
		DamageType:                      0,
		DamageSource:                    0,
		ImpactEffectName:                "ctd_minn_tc_3_hit.ServerEventDef",
		PassiveCreateEffectName:         "cyber_shield_generate_shield.ServerEventDef",
		PassiveEffectName:               "citadelBasicShield_Shield.ServerEventDef",
		PassiveAbsorptionShield:         shieldAmount,
		PassiveAbsorptionShieldRecharge: shieldRecharge,
		IsDamageProfileKnown:            true,
		IsFirstAggroDurationKnown:       true,
	}
}

func citadelBossTwinLaserProfile(nounName string) (ActionProfile, bool) {
	movementSpeed := float32(0)
	nonCombatMovementSpeed := float32(0)
	attackRange := float32(0)
	minimumDamage := float32(0)
	maximumDamage := float32(0)
	switch strings.ToLower(nounName) {
	case "citadelboss.noun":
		movementSpeed, nonCombatMovementSpeed = 7, 5.5
		attackRange = 15
		minimumDamage, maximumDamage = 3, 6
	case "citadelboss_2.noun":
		movementSpeed, nonCombatMovementSpeed = 10.5, 5.5
		attackRange = 17.5
		minimumDamage, maximumDamage = 4, 8
	case "citadelboss_3.noun":
		movementSpeed, nonCombatMovementSpeed = 14, 5.5
		attackRange = 20
		minimumDamage, maximumDamage = 6, 12
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionCone, AbilityName: "TwinLaser",
		AnimationName:     "ctd_boss_tc_attack2_part_1",
		LoopAnimationName: "ctd_boss_tc_attack2_part_2",
		EndAnimationName:  "ctd_boss_tc_attack2_part_3",
		HitDelay:          time.Second,
		// The cone scheduler adds the distance-dependent hold to this
		// wind-up plus end-animation duration; there is no extra idle cooldown.
		ReleaseDelay:              2900 * time.Millisecond,
		Cooldown:                  2900 * time.Millisecond,
		Range:                     attackRange,
		Radius:                    attackRange,
		Angle:                     30,
		MovementSpeed:             movementSpeed,
		NonCombatMovementSpeed:    nonCombatMovementSpeed,
		MinimumDamage:             minimumDamage,
		MaximumDamage:             maximumDamage,
		DescriptorMask:            1160,
		DamageType:                0,
		DamageSource:              1,
		TrailEffectName:           "citadel_boss_twin_laser_right.ServerEventDef",
		SecondaryTrailEffectName:  "citadel_boss_twin_laser_left.ServerEventDef",
		SourceEffectName:          "citadel_boss_twin_laser_muzzle_right.ServerEventDef",
		SecondarySourceEffectName: "citadel_boss_twin_laser_muzzle_left.ServerEventDef",
		GroundEffectName:          "citadel_boss_twin_laser_ground_hit.ServerEventDef",
		ImpactEffectName:          "citadel_boss_twin_laser_hit.ServerEventDef",
		TickDuration:              500 * time.Millisecond,
		NumberOfTicks:             4,
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}, true
}

func scaldronBossTwinLaserProfile(nounName string) (ActionProfile, bool) {
	rank := scaldronBossRank(nounName)
	if rank == 0 {
		return ActionProfile{}, false
	}
	cooldowns := [...]time.Duration{12 * time.Second, 10 * time.Second, 8 * time.Second}
	ranges := [...]float32{15, 17.5, 20}
	return ActionProfile{
		Family: ActionCone, AbilityName: "TwinLaser",
		AnimationName:             "sca_boss_attack_tc_part_1",
		LoopAnimationName:         "sca_boss_attack_tc_part_2",
		EndAnimationName:          "sca_boss_attack_tc_part_3",
		HitDelay:                  1260 * time.Millisecond,
		ReleaseDelay:              4760 * time.Millisecond,
		Cooldown:                  cooldowns[rank-1],
		Range:                     ranges[rank-1],
		Radius:                    ranges[rank-1],
		Angle:                     30,
		MovementSpeed:             5,
		NonCombatMovementSpeed:    3,
		MinimumDamage:             5,
		MaximumDamage:             5,
		DescriptorMask:            1160,
		DamageType:                0,
		DamageSource:              1,
		TrailEffectName:           "scaldron_boss_twin_laser_right.ServerEventDef",
		SecondaryTrailEffectName:  "scaldron_boss_twin_laser_left.ServerEventDef",
		SourceEffectName:          "scaldron_boss_twin_laser_muzzle_right.ServerEventDef",
		SecondarySourceEffectName: "scaldron_boss_twin_laser_muzzle_left.ServerEventDef",
		GroundEffectName:          "citadel_boss_twin_laser_ground_hit.ServerEventDef",
		ImpactEffectName:          "citadel_boss_twin_laser_hit.ServerEventDef",
		TickDuration:              500 * time.Millisecond,
		NumberOfTicks:             7,
		FirstAggroDelay:           10 * time.Second,
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}, true
}

func scaldronBossChainLightningProfile(nounName string) (ActionProfile, bool) {
	rank := scaldronBossRank(nounName)
	if rank == 0 {
		return ActionProfile{}, false
	}
	cooldowns := [...]time.Duration{10 * time.Second, 9 * time.Second, 8 * time.Second}
	ranges := [...]float32{5, 7.5, 10}
	minimumDamages := [...]float32{8, 12, 16}
	return ActionProfile{
		Family: ActionCone, AbilityName: "ChainLightningBolt",
		AnimationName:             "sca_boss_attack_el",
		HitDelay:                  2300 * time.Millisecond,
		ReleaseDelay:              4500 * time.Millisecond,
		Cooldown:                  cooldowns[rank-1],
		Range:                     ranges[rank-1],
		MovementSpeed:             5,
		NonCombatMovementSpeed:    3,
		MinimumDamage:             minimumDamages[rank-1],
		MaximumDamage:             30,
		DescriptorMask:            1 << 7,
		DamageType:                3,
		DamageSource:              1,
		ModifierName:              "CryosBossShock",
		ModifierDuration:          2 * time.Second,
		ModifierChance:            100,
		MaximumTargetCount:        5,
		TrailEffectName:           "cryos_boss_chain_lightning_attack_beam_effect.ServerEventDef",
		SecondaryTrailEffectName:  "cryos_boss_chain_lightning_link_effect.ServerEventDef",
		TargetEffectName:          "cryos_boss_chain_lightning_attack_beam_effect_target.ServerEventDef",
		ImpactEffectName:          "cryos_boss_electric_beam_hit_effect.ServerEventDef",
		Radius:                    10,
		Angle:                     360,
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}, true
}

func scaldronBossRank(nounName string) int {
	switch strings.ToLower(nounName) {
	case "scaldronboss.noun", "scaldronboss_stage2.noun":
		return 1
	case "scaldronboss_2.noun":
		return 2
	case "scaldronboss_3.noun":
		return 3
	default:
		return 0
	}
}

func citadelSpecificFourBoltProfile(
	cooldown time.Duration, projectileSpeed float32,
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "CitadelSpecificFour_Bolt",
		AnimationName:          "ctd_minn_tc_x4_attack1",
		HitDelay:               224999994 * time.Nanosecond,
		ReleaseDelay:           1500 * time.Millisecond,
		Cooldown:               cooldown,
		Range:                  23,
		MovementSpeed:          movementSpeed,
		NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage:          3,
		MaximumDamage:          6,
		DamageCoefficient:      0.05,
		DescriptorMask:         1<<7 | 1<<13,
		DamageType:             0,
		DamageSource:           1,
		ProjectileNoun:         "Ability_Fireball.Noun",
		TrailEffectName:        "ctd_minn_tc_x4_missile.ServerEventDef",
		ImpactEffectName:       "ctd_minn_tc_x4_missile_hit.ServerEventDef",
		MissEffectName:         "ineffective_common_small.ServerEventDef",
		ProjectileSpeed:        projectileSpeed,
		ProjectileDistance:     30,
		ProjectileOffset:       game.Vec3{Z: 1.7999999523162842},
		HomingDelay:            time.Millisecond,
		IsDamageProfileKnown:   true,
	}
}

func citadelSpecificTwoMeleeTauntProfile(
	cooldown time.Duration, minimumDamage float32, maximumDamage float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "CitadelSpecificTwo_MeleeTaunt",
		AnimationName:             "ctd_minn_tc_x2_attack1",
		HitDelay:                  500 * time.Millisecond,
		ReleaseDelay:              1300 * time.Millisecond,
		Cooldown:                  cooldown,
		Range:                     1,
		Radius:                    1.5,
		MovementSpeed:             5,
		NonCombatMovementSpeed:    5,
		MinimumDamage:             minimumDamage,
		MaximumDamage:             maximumDamage,
		DescriptorMask:            67,
		DamageType:                0,
		DamageSource:              0,
		ModifierID:                0x5ad04855,
		ImpactEffectName:          "ctd_minn_tc_x2_tauntHit.ServerEventDef",
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func zelemSpecialThreeProjectileProfile(
	cooldown time.Duration, attackRange float32, projectileSpeed float32,
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "ZelemSpecialThree",
		AnimationName:          "zlm_lieu_tc_3_attack2",
		HitDelay:               766667 * time.Microsecond,
		ReleaseDelay:           1600 * time.Millisecond,
		Cooldown:               cooldown,
		Range:                  attackRange,
		MovementSpeed:          movementSpeed,
		NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage:          6, MaximumDamage: 10,
		DamageCoefficient:         0.05,
		DescriptorMask:            1<<7 | 1<<13,
		DamageType:                0,
		DamageSource:              1,
		ProjectileNoun:            "Ability_PiercingLaser.Noun",
		TrailEffectName:           "cyber_zlm_lieu_blaster_projectile.ServerEventDef",
		ImpactEffectName:          "cyber_zlm_lieu_blaster_tunnel_in.ServerEventDef",
		ProjectileExitEffectName:  "cyber_zlm_lieu_blaster_tunnel_out.ServerEventDef",
		MissEffectName:            "ineffective_common_small.ServerEventDef",
		ProjectileSpeed:           projectileSpeed,
		ProjectileDistance:        50,
		IsProjectilePiercing:      true,
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func zelemBossPushProfile(forcedMovementDistance float32) ActionProfile {
	return ActionProfile{
		Family: ActionCone, AbilityName: "ZelemBossPush",
		AnimationName:               "zlm_boss_sp_attack3",
		FirstAggroAnimationName:     "zlm_boss_sp_beam_in",
		FirstAggroDelay:             13159999847 * time.Nanosecond,
		FirstAggroRevealDelay:       2 * time.Second,
		FirstAggroCinematicDuration: 13159999847 * time.Nanosecond,
		FirstAggroCinematicRadius:   100,
		HitDelay:                    1130 * time.Millisecond,
		ReleaseDelay:                2 * time.Second,
		Cooldown:                    12 * time.Second,
		Range:                       12,
		Radius:                      12,
		Angle:                       135,
		MovementSpeed:               5,
		MinimumDamage:               36, MaximumDamage: 36,
		MinimumDamagePercent:       0.5,
		DescriptorMask:             1<<7 | 1<<13,
		DamageType:                 0,
		DamageSource:               1,
		ForcedMovementSpeed:        25,
		ForcedMovementDistance:     forcedMovementDistance,
		ForcedMovementReactionName: "react_knockback",
		IsDamageProfileKnown:       true,
		IsFirstAggroDurationKnown:  true,
	}
}

func ZelemMarkSeekerProfile(nounName string) (ActionProfile, bool) {
	cooldown := time.Duration(0)
	projectileSpeed := float32(0)
	switch strings.ToLower(nounName) {
	case "zelemboss.noun":
		cooldown = 15 * time.Second
		projectileSpeed = 12
	case "zelemboss_2.noun":
		cooldown = 12 * time.Second
		projectileSpeed = 10
	case "zelemboss_3.noun":
		cooldown = 8 * time.Second
		projectileSpeed = 18
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "ZelemMarkSeeker",
		AnimationName: "zlm_boss_sp_attack2",
		HitDelay:      2100 * time.Millisecond,
		ReleaseDelay:  4900 * time.Millisecond,
		Cooldown:      cooldown,
		Range:         30,
		MovementSpeed: 5,
		MinimumDamage: 6, MaximumDamage: 10,
		DamageCoefficient:   0.05,
		DescriptorMask:      1<<7 | 1<<13,
		DamageType:          0,
		DamageSource:        1,
		ProjectileNoun:      "Ability_TallProjectile.Noun",
		TrailEffectName:     "spacetime_boss_shot_effect.ServerEventDef",
		ImpactEffectName:    "spacetime_lightspeed_hit_effect.ServerEventDef",
		MissEffectName:      "ineffective_common_small.ServerEventDef",
		ProjectileSpeed:     projectileSpeed,
		ProjectileDistance:  100,
		ProjectileShotCount: 6,
		ProjectileShotDeadlines: []time.Duration{
			0, 0, 2700 * time.Millisecond, 2700 * time.Millisecond,
			3260 * time.Millisecond, 3260 * time.Millisecond,
		},
		HomingDelay: 16 * time.Millisecond,
		ProjectileOffsets: []game.Vec3{
			{X: 2, Y: 1, Z: -1.5}, {X: -2, Y: 1, Z: -1.5},
			{X: 3.25, Y: 0.75, Z: -1.5}, {X: -3.25, Y: 0.75, Z: -1.5},
			{X: 6, Z: -1.5}, {X: -6, Z: -1.5},
		},
		IsDamageProfileKnown:       true,
		IsProjectileParallelVolley: true,
	}, true
}

func ZelemBossBlinkProfile(nounName string) (ActionProfile, bool) {
	cooldown := time.Duration(0)
	switch strings.ToLower(nounName) {
	case "zelemboss.noun":
		cooldown = 15 * time.Second
	case "zelemboss_2.noun":
		cooldown = 10 * time.Second
	case "zelemboss_3.noun":
		cooldown = 5 * time.Second
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionZelemRanged, AbilityName: "ZelemBossBlink",
		AnimationName:           "zlm_boss_sp_teleport_out",
		HitDelay:                1100 * time.Millisecond,
		ReleaseDelay:            2100 * time.Millisecond,
		Cooldown:                cooldown,
		Range:                   50,
		MovementSpeed:           5,
		TeleportMinimumDistance: 10,
		TeleportNormalDistance:  15,
		TeleportMaximumDistance: 20,
		TeleportAnimationName:   "zlm_boss_sp_teleport_in",
		TeleportAnimationDelay:  time.Second,
	}, true
}

func cryosPoisonMeleeProfile(
	modifierChance uint32, modifierDuration time.Duration,
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionCone, AbilityName: "CryosPoisonMelee",
		AnimationName:          "cry_minn_lf_poison_attack1",
		HitDelay:               430 * time.Millisecond,
		ReleaseDelay:           time.Second,
		Cooldown:               2 * time.Second,
		Range:                  0.75,
		Radius:                 1.25,
		Angle:                  90,
		MovementSpeed:          movementSpeed,
		NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage:          3, MaximumDamage: 6,
		DescriptorMask:                1 | 1<<1 | 1<<6,
		DamageType:                    2,
		DamageSource:                  0,
		ImpactEffectName:              "life_common_melee_hit.ServerEventDef",
		ModifierName:                  "Poison",
		ModifierDuration:              modifierDuration,
		ModifierChance:                modifierChance,
		ModifierTickDuration:          4 * time.Second,
		ModifierMinimumTickDamage:     3,
		ModifierMaximumTickDamage:     6,
		ModifierTickDamageCoefficient: 0.05,
		ModifierDescriptorMask:        1 | 1<<1 | 1<<6,
		ModifierDamageType:            2,
		ModifierDamageSource:          1,
		ModifierMaximumStack:          1,
		IsModifierDamageProfileKnown:  true,
		IsDamageProfileKnown:          true,
		IsFirstAggroDurationKnown:     true,
	}
}

func cryosBasicFieryMeleeProfile(
	cooldown time.Duration, modifierDuration time.Duration,
) ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "CryosBasicFieryMelee",
		AnimationName: "cry_minn_el_x3_attack",
		HitDelay:      266667 * time.Microsecond,
		ReleaseDelay:  1260 * time.Millisecond,
		Cooldown:      cooldown,
		Range:         1,
		MovementSpeed: 5,
		MinimumDamage: 2, MaximumDamage: 4,
		DescriptorMask:                1 | 1<<1 | 1<<6,
		DamageType:                    3,
		DamageSource:                  0,
		ImpactEffectName:              "plasma_common_fire_hit_small_effect.ServerEventDef",
		ModifierName:                  "CryosBasicFieryBurn",
		ModifierDuration:              modifierDuration,
		ModifierTickDuration:          time.Second,
		ModifierMinimumTickDamage:     2,
		ModifierMaximumTickDamage:     2,
		ModifierTickDamageCoefficient: 0.05,
		ModifierDescriptorMask:        1<<2 | 1<<5,
		ModifierDamageType:            3,
		ModifierDamageSource:          1,
		ModifierMaximumStack:          6,
		TargetEffectName:              "status_burning.ServerEventDef",
		IsModifierDamageProfileKnown:  true,
		IsDamageProfileKnown:          true,
		IsFirstAggroDurationKnown:     true,
	}
}

func cryosBasicRangedProfile(
	cooldown time.Duration, attackRange float32, projectileSpeed float32,
	modifierChance uint32, movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "PlasmaLightning",
		AnimationName: "cry_minn_el_ranged_attack1",
		HitDelay:      300 * time.Millisecond,
		ReleaseDelay:  450 * time.Millisecond,
		Cooldown:      cooldown,
		Range:         attackRange,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage: 1, MaximumDamage: 8,
		DamageCoefficient:         0.05,
		DescriptorMask:            1<<7 | 1<<13,
		DamageType:                3,
		DamageSource:              1,
		ProjectileNoun:            "Ability_Fireball.Noun",
		TrailEffectName:           "plasma_lightningbolt_projectile.ServerEventDef",
		ImpactEffectName:          "plasma_common_electric_hit_small_effect.ServerEventDef",
		MissEffectName:            "ineffective_common_small.ServerEventDef",
		ProjectileSpeed:           projectileSpeed,
		ProjectileDistance:        50,
		ModifierName:              "StalkerShock",
		ModifierDuration:          time.Second,
		ModifierChance:            modifierChance,
		ModifierDescriptorMask:    1 << 5,
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func cryosBasicLightningRangedProfile(
	cooldown time.Duration, attackRange float32, minimumRange float32,
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionCone, AbilityName: "CryosBasicLightningRanged",
		AnimationName:             "cry_minn_el_4_attack1",
		EndAnimationName:          "cry_minn_el_4_fizzle",
		HitDelay:                  333333343 * time.Nanosecond,
		ReleaseDelay:              949999988 * time.Nanosecond,
		Cooldown:                  cooldown,
		Range:                     attackRange,
		MinimumRange:              minimumRange,
		MovementSpeed:             movementSpeed,
		NonCombatMovementSpeed:    nonCombatMovementSpeed,
		MinimumDamage:             3,
		MaximumDamage:             9,
		DamageCoefficient:         0.05,
		DescriptorMask:            1 << 7,
		DamageType:                3,
		DamageSource:              1,
		TrailEffectName:           "ability_cryos_basic_lightning_ranged.ServerEventDef",
		ImpactEffectName:          "plasma_common_electric_hit_large_effect.ServerEventDef",
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func cryosBasicLightningMeleeProfile(
	cooldown time.Duration, movementSpeed float32, nonCombatMovementSpeed float32,
	isLightningMinion bool,
) ActionProfile {
	firstAggroAnimationName := ""
	firstAggroDelay := time.Duration(0)
	if isLightningMinion {
		firstAggroAnimationName = "cry_el_boss_minion_spawn"
		firstAggroDelay = 1800 * time.Millisecond
	}
	return ActionProfile{
		Family: ActionMelee, AbilityName: "CryosBasicLightningMelee",
		FirstAggroAnimationName: firstAggroAnimationName,
		FirstAggroDelay:         firstAggroDelay,
		AnimationName:           "cry_minn_el_3_attack1",
		HitDelay:                266666681 * time.Nanosecond,
		ReleaseDelay:            500 * time.Millisecond,
		Cooldown:                cooldown,
		Range:                   1.6875,
		Radius:                  2.25,
		MovementSpeed:           movementSpeed,
		NonCombatMovementSpeed:  nonCombatMovementSpeed,
		MovementSpeedBuff:       0,
		MinimumDamage:           1, MaximumDamage: 6,
		DamageCoefficient:         0.05,
		DescriptorMask:            1 | 1<<1 | 1<<7,
		DamageType:                3,
		DamageSource:              1,
		ModifierName:              "CryosBasicLightningDebuff",
		ModifierDamageBuff:        0.02,
		ModifierMaximumStack:      50,
		ImpactEffectName:          "plasma_common_electric_hit_small_effect.ServerEventDef",
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: isLightningMinion,
	}
}

func zelemSpecialHasterProfile(
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionZelemHaster, AbilityName: "CastZelemHasteBuff",
		AnimationName:          "zlm_lieu_sp_03_attack2",
		HitDelay:               266670 * time.Microsecond,
		ReleaseDelay:           1066670 * time.Microsecond,
		Cooldown:               4 * time.Second,
		Range:                  30,
		MovementSpeed:          movementSpeed,
		NonCombatMovementSpeed: nonCombatMovementSpeed,
		IsSelfTargeted:         true,
		ModifierName:           "ZelemHasteBuff", ModifierDuration: 20 * time.Second,
		IsFirstAggroDurationKnown: true,
	}
}

func nomadWithDroneProfile(
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionNomadDrone, AbilityName: "NomadWithDronePunch",
		AnimationName:           "nomad_lieu_tc_3_attack",
		PreAggroAnimationName:   "zlm_minn_tc_2_shutdown",
		FirstAggroAbilityName:   "FirstAggro_ActivateRobot",
		FirstAggroAnimationName: "zlm_minn_tc_2_aggro",
		FirstAggroDelay:         1300 * time.Millisecond,
		HitDelay:                515152 * time.Microsecond, ReleaseDelay: 1666667 * time.Microsecond,
		Cooldown: 2400 * time.Millisecond, Range: 2,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage: 5, MaximumDamage: 8, DamageCoefficient: 0.05,
		DescriptorMask: 1 | 1<<1 | 1<<6,
		DamageType:     4, DamageSource: 0,
		IsDamageProfileKnown: true, IsFirstAggroDurationKnown: true,
	}
}

func NomadWithDroneShieldDuration(nounName string) (time.Duration, bool) {
	switch strings.ToLower(nounName) {
	case "nomadwithdrone.noun", "nomadwithdrone_captain.noun":
		return 5 * time.Second, true
	case "nomadwithdrone_2.noun", "nomadwithdrone_captain_2.noun":
		return 6500 * time.Millisecond, true
	case "nomadwithdrone_3.noun", "nomadwithdrone_captain_3.noun":
		return 8 * time.Second, true
	default:
		return 0, false
	}
}

func NomadDroneLaserProfile(definition sim.AbilityDefinition) (ActionProfile, error) {
	if definition.Name != "SentryDroneLaser" ||
		definition.Kind != sim.AbilityKindProjectile || definition.Range <= 0 ||
		definition.HitDelay <= 0 || definition.Cooldown <= 0 ||
		definition.ProjectileNoun == "" || definition.Speed <= 0 ||
		definition.Distance <= 0 || definition.MinimumDamage <= 0 ||
		definition.MaximumDamage < definition.MinimumDamage ||
		definition.DamageType > 255 || definition.DamageSource > 255 {
		return ActionProfile{}, errors.New("nomad drone laser invalid")
	}
	releaseDelay := max(definition.ReleaseDelay, definition.HitDelay)
	return ActionProfile{
		Family: ActionProjectile, AbilityName: definition.Name,
		AnimationName: definition.AnimationName,
		HitDelay:      definition.HitDelay, ReleaseDelay: releaseDelay,
		Cooldown: definition.Cooldown, Range: definition.Range,
		MinimumDamage:      definition.MinimumDamage,
		MaximumDamage:      definition.MaximumDamage,
		DamageCoefficient:  definition.DamageCoefficient,
		DescriptorMask:     definition.DescriptorMask,
		DamageType:         uint8(definition.DamageType),
		DamageSource:       uint8(definition.DamageSource),
		ProjectileNoun:     definition.ProjectileNoun,
		TrailEffectName:    definition.TrailEffectName,
		ImpactEffectName:   definition.ImpactEffectName,
		MissEffectName:     definition.MissEffectName,
		ProjectileSpeed:    definition.Speed,
		ProjectileDistance: definition.Distance,
		IsDamageProfileKnown: definition.IsDescriptorFound &&
			definition.IsDamageTypeFound && definition.IsDamageSourceFound,
		IsFirstAggroDurationKnown: true,
	}, nil
}

func cryosSpecialOneBurstShotProfile(
	attackRange float32, projectileSpeed float32, isTrackingBetweenShots bool,
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "BurstShot",
		AnimationName: "cast_burstshot",
		HitDelay:      1060 * time.Millisecond,
		ReleaseDelay:  2900 * time.Millisecond,
		Cooldown:      time.Second,
		Range:         attackRange,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage: 2, MaximumDamage: 7,
		DamageCoefficient:                0.05,
		DescriptorMask:                   1<<7 | 1<<13,
		DamageType:                       3,
		DamageSource:                     1,
		ProjectileNoun:                   "Ability_Fireball.Noun",
		TrailEffectName:                  "lightning_bullet_projectile.ServerEventDef",
		ImpactEffectName:                 "plasma_common_electric_hit_medium_effect.ServerEventDef",
		MissEffectName:                   "ineffective_common_small.ServerEventDef",
		ProjectileSpeed:                  projectileSpeed,
		ProjectileDistance:               50,
		ProjectileShotInterval:           400 * time.Millisecond,
		ProjectileShotCount:              4,
		IsProjectileTrackingBetweenShots: isTrackingBetweenShots,
		IsDamageProfileKnown:             true,
		IsFirstAggroDurationKnown:        true,
	}
}

func cryosSpecialTwoSpreadShotProfile(
	cooldown time.Duration, projectileSpeed float32, shotInterval time.Duration,
	shotAngles []float32, modifierDuration time.Duration,
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "SpreadShot",
		AnimationName:                 "cry_lieu_el_spread_attack1",
		FirstAggroAnimationName:       "cry_lieu_el_spread_burrow_out",
		FirstAggroDelay:               1630 * time.Millisecond,
		HitDelay:                      170 * time.Millisecond,
		ReleaseDelay:                  1800 * time.Millisecond,
		Cooldown:                      cooldown,
		Range:                         15,
		MovementSpeed:                 movementSpeed,
		NonCombatMovementSpeed:        nonCombatMovementSpeed,
		MinimumDamage:                 3,
		MaximumDamage:                 6,
		DamageCoefficient:             0.05,
		DescriptorMask:                8320,
		DamageType:                    3,
		DamageSource:                  1,
		ModifierName:                  "OnFire",
		ModifierDuration:              modifierDuration,
		ModifierChance:                25,
		ModifierTickDuration:          2 * time.Second,
		ModifierMinimumTickDamage:     6,
		ModifierMaximumTickDamage:     12,
		ModifierTickDamageCoefficient: 0.05,
		ModifierDescriptorMask:        1<<2 | 1<<5,
		ModifierDamageType:            3,
		ModifierDamageSource:          1,
		ModifierMaximumStack:          1,
		ProjectileNoun:                "Ability_Fireball.Noun",
		TrailEffectName:               "plasma_ball_projectile.ServerEventDef",
		ImpactEffectName:              "plasma_common_fire_hit_small_effect.ServerEventDef",
		MissEffectName:                "ineffective_common_small.ServerEventDef",
		ProjectileSpeed:               projectileSpeed,
		ProjectileDistance:            50,
		ProjectileShotInterval:        shotInterval,
		ProjectileShotCount:           uint32(len(shotAngles)),
		ProjectileShotAngles:          shotAngles,
		TargetEffectName:              "status_burning.ServerEventDef",
		IsModifierDamageProfileKnown:  true,
		IsDamageProfileKnown:          true,
		IsFirstAggroDurationKnown:     true,
	}
}

func verdanthBasicDiseasedPoisonCloudProfile(
	attackRange float32, projectileSpeed float32, modifierChance uint32,
) ActionProfile {
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "PoisonCloud",
		AnimationName: "ver_minn_lf_diseased_attack1",
		HitDelay:      170 * time.Millisecond,
		ReleaseDelay:  1860 * time.Millisecond,
		Cooldown:      3 * time.Second,
		Range:         attackRange,
		MovementSpeed: 5,
		MinimumDamage: 2, MaximumDamage: 4,
		DamageCoefficient:             0.05,
		DescriptorMask:                8328,
		DamageType:                    2,
		DamageSource:                  1,
		ModifierName:                  "LifePlagueSpread",
		ModifierID:                    0x11067057,
		ModifierDuration:              6 * time.Second,
		ModifierChance:                modifierChance,
		ModifierTickDuration:          time.Second,
		ModifierMinimumTickDamage:     3,
		ModifierMaximumTickDamage:     3,
		ModifierTickDamageCoefficient: 0.05,
		ModifierDescriptorMask:        1<<2 | 1<<5,
		ModifierDamageType:            2,
		ModifierDamageSource:          1,
		ModifierMaximumStack:          1,
		ProjectileNoun:                "Ability_Fireball.Noun",
		TrailEffectName:               "life_disease_spore_projectile.ServerEventDef",
		ImpactEffectName:              "life_disease_spore_cloud.ServerEventDef",
		MissEffectName:                "ineffective_common_small.ServerEventDef",
		ProjectileSpeed:               projectileSpeed,
		ProjectileDistance:            12,
		Radius:                        1,
		TargetEffectName:              "status_diseased.ServerEventDef",
		IsModifierDamageProfileKnown:  true,
		IsDamageProfileKnown:          true,
		IsFirstAggroDurationKnown:     true,
	}
}

func nomadSpecialThreeProjectileProfile(nounName string) (ActionProfile, bool) {
	energyDefense := float32(0)
	movementSpeed := float32(0)
	nonCombatMovementSpeed := float32(0)
	switch strings.ToLower(nounName) {
	case "nomadspecialthree.noun":
		energyDefense = 250
		movementSpeed, nonCombatMovementSpeed = 4.5, 3
	case "nomadspecialthree_2.noun":
		energyDefense = 500
		movementSpeed, nonCombatMovementSpeed = 6.5, 3
	case "nomadspecialthree_3.noun":
		energyDefense = 750
		movementSpeed, nonCombatMovementSpeed = 8.5, 3
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "NomadSpecialThree",
		AnimationName:          "nomad_lieu_lf_3_attack1",
		HitDelay:               433333 * time.Microsecond,
		ReleaseDelay:           880952 * time.Microsecond,
		Cooldown:               2500 * time.Millisecond,
		Range:                  15,
		MovementSpeed:          movementSpeed,
		NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage:          6, MaximumDamage: 10,
		DamageCoefficient:         0.05,
		ProjectileNoun:            "Ability_HealShot.Noun",
		TrailEffectName:           "nomad_lieu_lf_3_projectile.ServerEventDef",
		ImpactEffectName:          "nomad_lieu_lf_3_projectile_hit.ServerEventDef",
		MissEffectName:            "ineffective_common_small.ServerEventDef",
		ProjectileSpeed:           12,
		ProjectileDistance:        50,
		PassiveEffectName:         "creature_shield_effect.ServerEventDef",
		PassiveEnergyDefense:      energyDefense,
		IsFirstAggroDurationKnown: true,
	}, true
}

func NomadSpecialThreeTurtleProfile(nounName string) (ActionProfile, bool) {
	pulseCount := uint32(0)
	switch strings.ToLower(nounName) {
	case "nomadspecialthree.noun":
		pulseCount = 6
	case "nomadspecialthree_2.noun":
		pulseCount = 7
	case "nomadspecialthree_3.noun":
		pulseCount = 8
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		AbilityName:   "NomadSpecialThreeTurtle",
		Radius:        5,
		MinimumDamage: 8, MaximumDamage: 12,
		DamageCoefficient:             0.05,
		DescriptorMask:                1<<3 | 1<<7,
		DamageType:                    2,
		DamageSource:                  1,
		ImpactEffectName:              "nomad_lieu_lf_3_AOE_hit.ServerEventDef",
		ModifierName:                  "TurtlePoison",
		ModifierDuration:              time.Second,
		ModifierTickDuration:          500 * time.Millisecond,
		ModifierMinimumTickDamage:     8,
		ModifierMaximumTickDamage:     12,
		ModifierTickDamageCoefficient: 0.05,
		ModifierDescriptorMask:        1<<2 | 1<<5,
		ModifierDamageType:            2,
		ModifierDamageSource:          1,
		ModifierMaximumStack:          1,
		NumberOfTicks:                 pulseCount,
		TargetEffectName:              "status_poisoned.ServerEventDef",
		IsModifierDamageProfileKnown:  true,
		IsDamageProfileKnown:          true,
	}, true
}

func cryosElementalSpecialThreeProfile(
	cooldown time.Duration, attackRange float32, projectileSpeed float32,
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "CryosElementalSpecialThree",
		AnimationName: "cry_lieu_el_3_attack1",
		HitDelay:      366667 * time.Microsecond,
		ReleaseDelay:  time.Second,
		Cooldown:      cooldown,
		Range:         attackRange,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage: 4, MaximumDamage: 16,
		DamageCoefficient:         0.05,
		DescriptorMask:            1<<7 | 1<<13 | 1<<14,
		DamageType:                3,
		DamageSource:              1,
		ProjectileNoun:            "Ability_PiercingLaser.Noun",
		TrailEffectName:           "cry_lieu_el_3_electric_projectile.ServerEventDef",
		ImpactEffectName:          "plasma_common_electric_hit_small_effect.ServerEventDef",
		ProjectileExitEffectName:  "plasma_common_electric_hit_medium_effect.ServerEventDef",
		MissEffectName:            "ineffective_common_small.ServerEventDef",
		ProjectileSpeed:           projectileSpeed,
		ProjectileDistance:        50,
		IsProjectilePiercing:      true,
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func citadelBasicGunnerShotProfile(
	attackRange float32, isTrackingBetweenShots bool,
	maximumLeadAngle float32, movementSpeed float32, nonCombatMovementSpeed float32,
	energyDefense float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "CitadelBasicGunner_Shot",
		AnimationName: "ctd_minn_tc_4_attack1",
		HitDelay:      300 * time.Millisecond,
		ReleaseDelay:  1500 * time.Millisecond,
		Cooldown:      4 * time.Second,
		Range:         attackRange,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage: 1, MaximumDamage: 2,
		DamageCoefficient:                0.05,
		DescriptorMask:                   1 << 6,
		DamageType:                       0,
		DamageSource:                     0,
		ProjectileNoun:                   "Ability_Fireball.Noun",
		TrailEffectName:                  "ctd_minn_tc_4_bullet_projectile.ServerEventDef",
		ImpactEffectName:                 "ctd_minn_tc_4_bullet_hit.ServerEventDef",
		MissEffectName:                   "ineffective_common_small.ServerEventDef",
		ProjectileSpeed:                  60,
		ProjectileDistance:               25,
		ProjectileShotInterval:           100 * time.Millisecond,
		ProjectileShotCount:              5,
		ProjectileOffset:                 game.Vec3{Z: 1.2},
		ProjectileMaximumLeadAngle:       maximumLeadAngle,
		PassiveEnergyDefense:             energyDefense,
		IsProjectileTrackingBetweenShots: isTrackingBetweenShots,
		IsDamageProfileKnown:             true,
		IsFirstAggroDurationKnown:        true,
	}
}

func citadelBasicRangedBoltProfile(
	animationName string, hitDelay time.Duration, cooldown time.Duration,
	attackRange float32, projectileSpeed float32, maximumLeadAngle float32,
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "CitadelBasicRanged_Bolt",
		AnimationName: animationName,
		HitDelay:      hitDelay,
		ReleaseDelay:  3200 * time.Millisecond,
		Cooldown:      cooldown,
		Range:         attackRange,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage: 4, MaximumDamage: 8,
		DamageCoefficient:          0.05,
		DescriptorMask:             1<<7 | 1<<13,
		DamageType:                 0,
		DamageSource:               1,
		ProjectileNoun:             "Ability_Fireball.Noun",
		TrailEffectName:            "ctd_minn_tc_1_bolt.ServerEventDef",
		ImpactEffectName:           "ctd_minn_tc_1_bolt_tunnel_in.ServerEventDef",
		ProjectileExitEffectName:   "ctd_minn_tc_1_bolt_tunnel_out.ServerEventDef",
		MissEffectName:             "ineffective_common_small.ServerEventDef",
		ProjectileSpeed:            projectileSpeed,
		ProjectileDistance:         30,
		ProjectileOffset:           game.Vec3{Z: 1},
		ProjectileMaximumLeadAngle: maximumLeadAngle,
		IsProjectilePiercing:       true,
		IsDamageProfileKnown:       true,
	}
}

func citadelSpecialTwoHomingStunProfile(
	cooldown time.Duration, movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "CitadelSpecialTwo_HomingStun",
		AnimationName:             "ctd_lieu_tc_02_attack3",
		FirstAggroAnimationName:   "gen_aggro_tc_drop_in",
		FirstAggroDelay:           1500 * time.Millisecond,
		HitDelay:                  330 * time.Millisecond,
		ReleaseDelay:              time.Second,
		Cooldown:                  cooldown,
		Range:                     50,
		MovementSpeed:             movementSpeed,
		NonCombatMovementSpeed:    nonCombatMovementSpeed,
		MinimumDamage:             9,
		MaximumDamage:             15,
		DamageCoefficient:         0.05,
		DescriptorMask:            1 << 7,
		DamageType:                0,
		DamageSource:              1,
		ProjectileNoun:            "CitadelSpecialTwo_HomingProjectile.Noun",
		TrailEffectName:           "ctd_lieu_tc_2_stunMissile.ServerEventDef",
		ImpactEffectName:          "ctd_lieu_tc_2_stunMissile_hit.ServerEventDef",
		MissEffectName:            "ineffective_common_small.ServerEventDef",
		ProjectileSpeed:           16,
		ProjectileDistance:        50,
		ProjectileOffset:          game.Vec3{X: -1.5, Z: 2.1},
		HomingDelay:               time.Millisecond,
		ModifierName:              "StalkerShock",
		ModifierDuration:          3 * time.Second,
		ModifierChance:            100,
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func citadelSpecificOneDischargeProfile(
	cooldown time.Duration, attackRange float32,
	minimumDamage float32, maximumDamage float32,
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "CitadelDischarge",
		AnimationName:          "ctd_minn_tc_x1_attack3",
		HitDelay:               533333361 * time.Nanosecond,
		ReleaseDelay:           1100 * time.Millisecond,
		Cooldown:               cooldown,
		Range:                  attackRange,
		MovementSpeed:          movementSpeed,
		NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage:          minimumDamage,
		MaximumDamage:          maximumDamage,
		DamageCoefficient:      0.05,
		DescriptorMask:         1 << 7,
		DamageType:             0,
		DamageSource:           1,
		TrailEffectName:        "citadel_zap_discharge_4.ServerEventDef",
		ImpactEffectName:       "citadel_zap_hit_4.ServerEventDef",
		IsDamageProfileKnown:   true,
	}
}

func CitadelSpecificOneChargeFriendProfile(
	nounName string,
) (ActionProfile, bool) {
	cooldown := time.Duration(0)
	attackRange := float32(0)
	movementSpeed := float32(0)
	nonCombatMovementSpeed := float32(0)
	switch strings.ToLower(nounName) {
	case "citadelspecificone.noun":
		cooldown = 3 * time.Second
		attackRange, movementSpeed, nonCombatMovementSpeed = 8, 6, 4.5
	case "citadelspecificone_2.noun":
		cooldown = 2500 * time.Millisecond
		attackRange, movementSpeed, nonCombatMovementSpeed = 10, 9, 6
	case "citadelspecificone_3.noun":
		cooldown = 2 * time.Second
		attackRange, movementSpeed, nonCombatMovementSpeed = 12, 12, 7.5
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionMelee, AbilityName: "CitadelChargeFriend",
		AnimationName: "ctd_minn_tc_x1_attack1", HitDelay: 533333361 * time.Nanosecond,
		ReleaseDelay: 1100 * time.Millisecond, Cooldown: cooldown, Range: attackRange,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		TrailEffectName: "citadel_zap_charge_friend_1.ServerEventDef",
	}, true
}

func CitadelSpecialTwoMeleeProfile(nounName string) (ActionProfile, bool) {
	var cooldown time.Duration
	var movementSpeed float32
	var nonCombatMovementSpeed float32
	switch strings.ToLower(nounName) {
	case "citadelspecialtwo.noun", "citadelspecialtwo_captain.noun":
		cooldown = 3 * time.Second
		movementSpeed, nonCombatMovementSpeed = 5, 3.5
	case "citadelspecialtwo_2.noun", "citadelspecialtwo_captain_2.noun":
		cooldown = 2500 * time.Millisecond
		movementSpeed, nonCombatMovementSpeed = 7.5, 5
	case "citadelspecialtwo_3.noun", "citadelspecialtwo_captain_3.noun":
		cooldown = 2 * time.Second
		movementSpeed, nonCombatMovementSpeed = 10, 5
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionMelee, AbilityName: "CitadelSpecialTwo_Melee",
		AnimationName:          "ctd_lieu_tc_02_attack1",
		HitDelay:               360 * time.Millisecond,
		ReleaseDelay:           600 * time.Millisecond,
		Cooldown:               cooldown,
		Range:                  2.25,
		MovementSpeed:          movementSpeed,
		NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage:          8,
		MaximumDamage:          16,
		DescriptorMask:         1 | 1<<1 | 1<<6,
		DamageType:             0,
		DamageSource:           0,
		ImpactEffectName:       "ctd_lieu_tc_2_knockbackPunch_hit.ServerEventDef",
		IsDamageProfileKnown:   true,
	}, true
}

func citadelSpecialFourMovement(
	nounName string,
) (float32, float32, bool) {
	switch strings.ToLower(nounName) {
	case "citadelspecialfour.noun", "citadelspecialfour_captain.noun":
		return 5, 3.5, true
	case "citadelspecialfour_2.noun", "citadelspecialfour_captain_2.noun":
		return 7.5, 5, true
	case "citadelspecialfour_3.noun", "citadelspecialfour_captain_3.noun":
		return 10, 6.5, true
	default:
		return 0, 0, false
	}
}

func CitadelSpecialFourRoboRepairProfile(nounName string) (ActionProfile, bool) {
	cooldown := time.Duration(0)
	healing := float32(0)
	switch strings.ToLower(nounName) {
	case "citadelspecialfour.noun", "citadelspecialfour_captain.noun":
		cooldown, healing = 20*time.Second, 5
	case "citadelspecialfour_2.noun", "citadelspecialfour_captain_2.noun":
		cooldown, healing = 15*time.Second, 15
	case "citadelspecialfour_3.noun", "citadelspecialfour_captain_3.noun":
		cooldown, healing = 10*time.Second, 30
	default:
		return ActionProfile{}, false
	}
	movementSpeed, nonCombatMovementSpeed, isMovementFound := citadelSpecialFourMovement(nounName)
	if !isMovementFound {
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionUnknown, AbilityName: "RoboRepair",
		AnimationName: "citadel_robo_repair",
		Cooldown:      cooldown, TickDuration: time.Second, NumberOfTicks: 5,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumHealing: healing, MaximumHealing: healing,
		IsFirstAggroDurationKnown: true,
	}, true
}

func CitadelSpecialFourGroundSlamProfile(nounName string) (ActionProfile, bool) {
	cooldown := time.Duration(0)
	switch strings.ToLower(nounName) {
	case "citadelspecialfour.noun", "citadelspecialfour_captain.noun":
		cooldown = 14 * time.Second
	case "citadelspecialfour_2.noun", "citadelspecialfour_captain_2.noun":
		cooldown = 12 * time.Second
	case "citadelspecialfour_3.noun", "citadelspecialfour_captain_3.noun":
		cooldown = 10 * time.Second
	default:
		return ActionProfile{}, false
	}
	movementSpeed, nonCombatMovementSpeed, isMovementFound := citadelSpecialFourMovement(nounName)
	if !isMovementFound {
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionCone, AbilityName: "GroundSlam",
		AnimationName: "citadel_ground_slam",
		HitDelay:      460 * time.Millisecond, ReleaseDelay: 1600 * time.Millisecond,
		Cooldown: cooldown, Range: 2, Radius: 2, Angle: 360,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage: 6, MaximumDamage: 10,
		DescriptorMask: 1<<3 | 1<<7, DamageType: 0, DamageSource: 0,
		ImpactEffectName:           "ctd_lieu_tc_03_groundSlam.ServerEventDef",
		ModifierName:               "GroundSlamKnockup",
		ForcedMovementSpeed:        0.75,
		ForcedMovementDistance:     0.2,
		ForcedMovementReactionName: "react_knockup",
		IsDamageProfileKnown:       true,
		IsFirstAggroDurationKnown:  true,
	}, true
}

func CitadelSpecialFourPistonPunchProfile(nounName string) (ActionProfile, bool) {
	movementSpeed, nonCombatMovementSpeed, isFound := citadelSpecialFourMovement(nounName)
	if !isFound {
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionMelee, AbilityName: "PistonPunch",
		AnimationName: "citadel_piston_punch",
		HitDelay:      360 * time.Millisecond, ReleaseDelay: 1300 * time.Millisecond,
		Cooldown: time.Second, Range: 2.5, Radius: 3.5,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage: 10, MaximumDamage: 15,
		DescriptorMask: 1 | 1<<1 | 1<<6, DamageType: 0, DamageSource: 0,
		ImpactEffectName:          "ctd_lieu_tc_03_punch.ServerEventDef",
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}, true
}

func nomadRuptionMeleeProfile(
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "NomadRuptionMeleeAttack",
		AnimationName: "nomad_lieu_el_4_melee_attack",
		HitDelay:      533333 * time.Microsecond,
		ReleaseDelay:  1600 * time.Millisecond,
		Cooldown:      2 * time.Second,
		Range:         1,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage: 5, MaximumDamage: 8,
		DamageCoefficient:    0.05,
		DescriptorMask:       1 | 1<<1 | 1<<6,
		DamageType:           1,
		DamageSource:         0,
		ImpactEffectName:     "charge_impact_small_effect.ServerEventDef",
		IsDamageProfileKnown: true,
	}
}

func NomadRuptionHurlMagmaProfile(nounName string) (ActionProfile, bool) {
	attackRange := float32(0)
	cooldown := time.Duration(0)
	poolDamage := float32(0)
	burnDuration := time.Duration(0)
	movementSpeed := float32(0)
	nonCombatMovementSpeed := float32(0)
	switch strings.ToLower(nounName) {
	case "nomadruption.noun", "nomadruption_captain.noun":
		attackRange, cooldown, poolDamage, burnDuration = 15, 10*time.Second, 1, 4*time.Second
		movementSpeed, nonCombatMovementSpeed = 5, 3.5
	case "nomadruption_2.noun", "nomadruption_captain_2.noun":
		attackRange, cooldown, poolDamage, burnDuration = 18, 8*time.Second, 2, 6*time.Second
		movementSpeed, nonCombatMovementSpeed = 7.5, 3.5
	case "nomadruption_3.noun", "nomadruption_captain_3.noun":
		attackRange, cooldown, poolDamage, burnDuration = 21, 6*time.Second, 3, 8*time.Second
		movementSpeed, nonCombatMovementSpeed = 10, 3.5
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionRetainedArea, AbilityName: "HurlMagma",
		AnimationName: "nomad_lieu_el_4_magma_attack",
		HitDelay:      633333 * time.Microsecond, ReleaseDelay: 1800 * time.Millisecond,
		Cooldown: cooldown, Range: attackRange,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage: poolDamage, MaximumDamage: poolDamage,
		DescriptorMask: 1<<3 | 1<<7, DamageType: 3, DamageSource: 1,
		ModifierName: "RuptionBurn", ModifierDuration: burnDuration,
		ModifierTickDuration:      500 * time.Millisecond,
		ModifierMinimumTickDamage: 1, ModifierMaximumTickDamage: 3,
		ModifierDescriptorMask: 1<<2 | 1<<5 | 1<<7,
		ModifierDamageType:     3, ModifierDamageSource: 1,
		ModifierMaximumStack:     1,
		ProjectileNoun:           "Ability_Toss.Noun",
		TrailEffectName:          "nomad_lieu_el_4_magma_projectile_effect.ServerEventDef",
		ImpactEffectName:         "plasma_common_fire_hit_small_effect.ServerEventDef",
		TargetEffectName:         "status_burning.ServerEventDef",
		ProjectileHeight:         4,
		ProjectileFlightDuration: time.Second, Radius: 2,
		ProjectileOffset: game.Vec3{Y: 1, Z: 2},
		TickDuration:     time.Second, NumberOfTicks: 10,
		RetainedObjectNoun:           "NomadRuptionMagmaPool.Noun",
		RetainedEffectName:           "nomad_lieu_el_4_magma_impact_pool_effect.ServerEventDef",
		IsModifierDamageProfileKnown: true, IsDamageProfileKnown: true,
		IsFirstAggroDurationKnown: true,
	}, true
}

func NomadDragMeteorProfile(nounName string) (ActionProfile, bool) {
	cooldown := time.Duration(0)
	movementSpeed := float32(0)
	nonCombatMovementSpeed := float32(0)
	switch strings.ToLower(nounName) {
	case "nomaddrag.noun":
		cooldown = 6 * time.Second
		movementSpeed, nonCombatMovementSpeed = 5, 3.5
	case "nomaddrag_2.noun":
		cooldown = 5 * time.Second
		movementSpeed, nonCombatMovementSpeed = 7.5, 3.5
	case "nomaddrag_3.noun":
		cooldown = 4 * time.Second
		movementSpeed, nonCombatMovementSpeed = 10, 3.5
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionRetainedArea, AbilityName: "NomadDrag_Meteor",
		AnimationName: "nomad_lieu_sp_3_attack1",
		EmergeDelay:   1100 * time.Millisecond,
		HitDelay:      1600 * time.Millisecond,
		ReleaseDelay:  2460 * time.Millisecond,
		Cooldown:      cooldown,
		Range:         20,
		Radius:        3,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage: 8, MaximumDamage: 14,
		DamageCoefficient:    0.05,
		DescriptorMask:       1<<3 | 1<<6,
		DamageType:           1,
		DamageSource:         0,
		EmergeEffectName:     "spacetime_meteor_summon_effect.ServerEventDef",
		IsDamageProfileKnown: true,
	}, true
}

func nomadShielderBashProfile(
	attackRange float32, hitArcLength float32,
	minimumDamage float32, maximumDamage float32,
	forcedMovementSpeed float32, forcedMovementDistance float32,
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "NomadShielderBash",
		AnimationName: "nomad_lieu_tc_2_shield_bash",
		HitDelay:      233300 * time.Microsecond,
		ReleaseDelay:  1200 * time.Millisecond,
		Cooldown:      time.Second,
		Range:         attackRange,
		Radius:        hitArcLength,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage: minimumDamage, MaximumDamage: maximumDamage,
		DamageCoefficient:          0.05,
		DescriptorMask:             1 | 1<<1 | 1<<6,
		DamageType:                 0,
		DamageSource:               0,
		ModifierName:               "NomadShielderBashKnockback",
		ImpactEffectName:           "nomad_lieu_tc_2_shield_hit.ServerEventDef",
		ForcedMovementSpeed:        forcedMovementSpeed,
		ForcedMovementDistance:     forcedMovementDistance,
		ForcedMovementReactionName: "react_knockback",
		IsDamageProfileKnown:       true,
	}
}

func NomadShielderGrenadeProfile(nounName string) (ActionProfile, bool) {
	attackRange := float32(0)
	cooldown := time.Duration(0)
	submunitionCount := uint32(0)
	movementSpeed := float32(0)
	nonCombatMovementSpeed := float32(0)
	switch strings.ToLower(nounName) {
	case "nomadshielder.noun", "nomadshielder_captain.noun":
		attackRange, cooldown = 18, 3*time.Second
		movementSpeed, nonCombatMovementSpeed = 6, 5
	case "nomadshielder_2.noun", "nomadshielder_captain_2.noun":
		attackRange, cooldown, submunitionCount = 20, 2500*time.Millisecond, 3
		movementSpeed, nonCombatMovementSpeed = 9, 5
	case "nomadshielder_3.noun", "nomadshielder_captain_3.noun":
		attackRange, cooldown, submunitionCount = 22, 2*time.Second, 5
		movementSpeed, nonCombatMovementSpeed = 12, 5
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "NomadShielderGrenade",
		AnimationName: "nomad_lieu_tc_2_grenade_toss",
		HitDelay:      500 * time.Millisecond, ReleaseDelay: 1500 * time.Millisecond,
		Cooldown: cooldown, Range: attackRange,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage: 6, MaximumDamage: 12,
		DescriptorMask: 1<<3 | 1<<7, DamageType: 0, DamageSource: 1,
		ProjectileNoun:   "Ability_Epic_Fireball.Noun",
		TrailEffectName:  "nomad_lieu_tc_2_grenade.ServerEventDef",
		ImpactEffectName: "nomad_lieu_tc_2_grenade_explosion.ServerEventDef",
		ProjectileSpeed:  16, ProjectileHeight: 3,
		ProjectileOffset: game.Vec3{X: 1, Y: 1, Z: 1}, Radius: 2,
		SubmunitionCount:         submunitionCount,
		SubmunitionMinimumDamage: 3, SubmunitionMaximumDamage: 6,
		SubmunitionRadius:          1,
		SubmunitionMinimumDistance: 3.5, SubmunitionMaximumDistance: 7,
		SubmunitionProjectileSpeed: 8, SubmunitionProjectileHeight: 3,
		SubmunitionTrailEffectName:  "nomad_lieu_tc_2_subgrenade.ServerEventDef",
		SubmunitionImpactEffectName: "nomad_lieu_tc_2_subgrenade_explosion.ServerEventDef",
		IsDamageProfileKnown:        true,
		IsFirstAggroDurationKnown:   true,
	}, true
}

type NomadShielderShieldProfile struct {
	SetupAnimationName string
	IdleAnimationName  string
	StopAnimationName  string
	EffectName         string
	SetupWait          time.Duration
	SetupHitDelay      time.Duration
	SetupReleaseDelay  time.Duration
	StopReleaseDelay   time.Duration
	Range              float32
	MovementSpeed      float32
}

func NomadShielderDirectionalShieldProfile(
	nounName string,
) (NomadShielderShieldProfile, bool) {
	setupWait := time.Duration(0)
	switch strings.ToLower(nounName) {
	case "nomadshielder.noun", "nomadshielder_captain.noun":
		setupWait = 500 * time.Millisecond
	case "nomadshielder_2.noun", "nomadshielder_captain_2.noun":
		setupWait = 250 * time.Millisecond
	case "nomadshielder_3.noun", "nomadshielder_captain_3.noun":
		setupWait = 0
	default:
		return NomadShielderShieldProfile{}, false
	}
	return NomadShielderShieldProfile{
		SetupAnimationName: "nomad_lieu_tc_2_shield_start",
		IdleAnimationName:  "nomad_lieu_tc_2_shield_idle",
		StopAnimationName:  "nomad_lieu_tc_2_shield_stop",
		EffectName:         "nomad_lieu_tc_2_shield.ServerEventDef",
		SetupWait:          setupWait,
		SetupHitDelay:      600000023 * time.Nanosecond,
		SetupReleaseDelay:  766667008 * time.Nanosecond,
		StopReleaseDelay:   860000014 * time.Nanosecond,
		Range:              20,
		MovementSpeed:      2,
	}, true
}

func nocturnaSpecialHomerProfile(
	cooldown time.Duration, attackRange float32, projectileSpeed float32,
	fearDuration time.Duration,
) ActionProfile {
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "NocturnaSpecialHomer",
		AnimationName: "nomad_lieu_su_2_attack1",
		HitDelay:      1700 * time.Millisecond,
		ReleaseDelay:  2733334 * time.Microsecond,
		Cooldown:      cooldown,
		Range:         attackRange,
		MovementSpeed: 5,
		MinimumDamage: 2, MaximumDamage: 6,
		DamageCoefficient:    0.05,
		DescriptorMask:       1<<7 | 1<<13,
		DamageType:           4,
		DamageSource:         1,
		ModifierName:         "NocturnaSpecialHomerFear",
		ModifierDuration:     fearDuration,
		ProjectileNoun:       "Ability_HomingProjectile.Noun",
		TrailEffectName:      "shadow_homing_ghost_effect.ServerEventDef",
		ImpactEffectName:     "shadow_bolt_impact.ServerEventDef",
		MissEffectName:       "shadow_homing_ghost_fizzle_effect.ServerEventDef",
		ProjectileSpeed:      projectileSpeed,
		ProjectileDistance:   30,
		ProjectileOffset:     game.Vec3{Z: 5},
		HomingDelay:          time.Millisecond,
		IsDamageProfileKnown: true,
	}
}

func NocturnaSpecialHomerMeleeProfile(nounName string) (ActionProfile, bool) {
	cooldown := time.Duration(0)
	switch strings.ToLower(nounName) {
	case "nocturnaspecialhomer.noun":
		cooldown = 1500 * time.Millisecond
	case "nocturnaspecialhomer_2.noun":
		cooldown = 1250 * time.Millisecond
	case "nocturnaspecialhomer_3.noun":
		cooldown = time.Second
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionMelee, AbilityName: "NocturnaSpecialHomerMelee",
		AnimationName: "nomad_lieu_su_2_attack2",
		HitDelay:      566667 * time.Microsecond,
		ReleaseDelay:  1500 * time.Millisecond,
		Cooldown:      cooldown,
		Range:         2,
		Radius:        3,
		MovementSpeed: 5,
		MinimumDamage: 5, MaximumDamage: 8,
		DamageCoefficient:    0.05,
		DescriptorMask:       1 | 1<<1 | 1<<6,
		DamageType:           4,
		DamageSource:         0,
		ImpactEffectName:     "necro_common_hit_small.ServerEventDef",
		IsDamageProfileKnown: true,
	}, true
}

func NocturnaSpecialHomerCooldownScale(playerCount uint32) (float64, bool) {
	switch playerCount {
	case 1:
		return 1, true
	case 2:
		return 0.8, true
	case 3:
		return 0.67, true
	case 4:
		return 0.5, true
	default:
		return 0, false
	}
}

func ZelemBasicHybridMeleeProfile(nounName string) (ActionProfile, bool) {
	cooldown := time.Duration(0)
	switch strings.ToLower(nounName) {
	case "zelembasichybrid.noun":
		cooldown = 2500 * time.Millisecond
	case "zelembasichybrid_2.noun":
		cooldown = 2 * time.Second
	case "zelembasichybrid_3.noun":
		cooldown = 1500 * time.Millisecond
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionMelee, AbilityName: "ZelemBasicHybridMelee",
		AnimationName: "zlm_minn_sp_04_attack2",
		HitDelay:      259999990 * time.Nanosecond,
		ReleaseDelay:  1200000048 * time.Nanosecond,
		Cooldown:      cooldown,
		Range:         1.125,
		Radius:        1.75,
		MovementSpeed: 5,
		MinimumDamage: 2, MaximumDamage: 6,
		DamageCoefficient:         0.05,
		DescriptorMask:            1 | 1<<1 | 1<<7,
		DamageType:                1,
		DamageSource:              0,
		ImpactEffectName:          "spacetime_bite.ServerEventDef",
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}, true
}

func zelemBasicHybridProjectileProfile(
	cooldown time.Duration, attackRange float32, projectileSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "ZelemBasicHybridProjectile",
		AnimationName: "zlm_minn_sp_04_attack1",
		HitDelay:      466699988 * time.Nanosecond,
		ReleaseDelay:  1600000024 * time.Nanosecond,
		Cooldown:      cooldown,
		Range:         attackRange,
		MovementSpeed: 5,
		MinimumDamage: 7, MaximumDamage: 14,
		DamageCoefficient:         0.05,
		DescriptorMask:            1<<7 | 1<<13,
		DamageType:                1,
		DamageSource:              1,
		ProjectileNoun:            "Ability_Fireball.Noun",
		TrailEffectName:           "spacetime_lieuSP03_shot_effect.ServerEventDef",
		ImpactEffectName:          "spacetime_hybrid_hit_effect.ServerEventDef",
		MissEffectName:            "ineffective_common_small.ServerEventDef",
		ProjectileSpeed:           projectileSpeed,
		ProjectileDistance:        50,
		ProjectileOffset:          game.Vec3{X: 0.25, Z: 0.2},
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func verdanthBasicRootmobMeleeProfile(
	cooldown time.Duration, modifierChance uint32,
) ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "VerdanthBasicRootMobMelee",
		AnimationName:          "ver_minn_lf_x2_attack1",
		HitDelay:               400 * time.Millisecond,
		ReleaseDelay:           1200 * time.Millisecond,
		Cooldown:               cooldown,
		Range:                  1.25,
		MovementSpeed:          5.5,
		NonCombatMovementSpeed: 3,
		MinimumDamage:          4, MaximumDamage: 6,
		DescriptorMask:            1 | 1<<1 | 1<<6,
		DamageType:                2,
		DamageSource:              0,
		ImpactEffectName:          "ver_minn_lf_x2_rootAttack_hit.ServerEventDef",
		ModifierName:              "VerdanthBasicRootmobModifier",
		ModifierDuration:          time.Second,
		ModifierChance:            modifierChance,
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func noctBasicMeleeDogProfile(cooldown time.Duration) ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "ZelemBasicMeleeAttack",
		AnimationName:          "zlm_minn_sp_3_attack",
		HitDelay:               260 * time.Millisecond,
		ReleaseDelay:           600 * time.Millisecond,
		Cooldown:               cooldown,
		Range:                  0.75,
		MovementSpeed:          8,
		NonCombatMovementSpeed: 6.5,
		MinimumDamage:          4, MaximumDamage: 7,
		DescriptorMask:            1 | 1<<1 | 1<<6,
		DamageType:                3,
		DamageSource:              0,
		ImpactEffectName:          "spacetime_bite.ServerEventDef",
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func verdanthBasicMeleeProfile() ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "SlowingMelee",
		AnimationName:             "minion_sp_slower_attack1",
		FirstAggroAnimationName:   "first_aggro_roar",
		FirstAggroDelay:           1750 * time.Millisecond,
		HitDelay:                  270000011 * time.Nanosecond,
		ReleaseDelay:              1299999952 * time.Nanosecond,
		Cooldown:                  2 * time.Second,
		Range:                     0.75,
		Radius:                    1,
		MovementSpeed:             7,
		NonCombatMovementSpeed:    5,
		MinimumDamage:             6,
		MaximumDamage:             9,
		DamageCoefficient:         0.05,
		DescriptorMask:            1 | 1<<1 | 1<<6,
		DamageType:                1,
		DamageSource:              0,
		ModifierName:              "SlowingMeleeModifier",
		ModifierID:                0x83cd6cbc,
		ModifierDuration:          2 * time.Second,
		MovementSpeedBuff:         -0.5,
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func verdanthBasicRangedProfile(
	cooldown time.Duration, projectileSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family:                    ActionProjectile,
		AbilityName:               "HomingSwarm",
		AnimationName:             "minion_lf_homing_shoot",
		FirstAggroAnimationName:   "minion_lf_homing_aggro",
		FirstAggroEffectName:      "bee_turret_active_effect.ServerEventDef",
		FirstAggroDelay:           830 * time.Millisecond,
		HitDelay:                  300 * time.Millisecond,
		ReleaseDelay:              800 * time.Millisecond,
		Cooldown:                  cooldown,
		Range:                     35,
		MinimumDamage:             3,
		MaximumDamage:             6,
		DescriptorMask:            8320,
		DamageType:                2,
		DamageSource:              1,
		ProjectileNoun:            "Ability_Fireball.Noun",
		TrailEffectName:           "bee_swarm_effect.ServerEventDef",
		ImpactEffectName:          "bee_impact_effect.ServerEventDef",
		MissEffectName:            "ineffective_common_small.ServerEventDef",
		ProjectileSpeed:           projectileSpeed,
		ProjectileDistance:        40,
		HomingDelay:               time.Millisecond,
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func verdanthBasicHealerThornDartProfile(
	cooldown time.Duration, attackRange float32, projectileDistance float32,
	shotCount int, shotInterval time.Duration, isTrackingBetweenShots bool,
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "ThornDart",
		AnimationName: "ver_minn_lf_05_attack1",
		HitDelay:      500 * time.Millisecond,
		ReleaseDelay:  1400 * time.Millisecond,
		Cooldown:      cooldown,
		Range:         attackRange,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage: 2, MaximumDamage: 4,
		DamageCoefficient:                0.05,
		DescriptorMask:                   1<<6 | 1<<13,
		DamageType:                       2,
		DamageSource:                     0,
		ProjectileNoun:                   "Ability_Fireball.Noun",
		TrailEffectName:                  "ver_minn_lf_05_thornDart.ServerEventDef",
		ImpactEffectName:                 "ver_minn_lf_05_thornDart_hit.ServerEventDef",
		MissEffectName:                   "ineffective_common_small.ServerEventDef",
		ProjectileSpeed:                  30,
		ProjectileDistance:               projectileDistance,
		ProjectileShotInterval:           shotInterval,
		ProjectileShotCount:              uint32(shotCount),
		ProjectileSpreadAngle:            15,
		ProjectileOffset:                 game.Vec3{Z: 0.6},
		IsDamageProfileKnown:             true,
		IsFirstAggroDurationKnown:        true,
		IsProjectileTrackingBetweenShots: isTrackingBetweenShots,
	}
}

func VerdanthBasicHealerRootedHealProfile(
	nounName string,
) (ActionProfile, bool) {
	healing := float32(0)
	switch strings.ToLower(nounName) {
	case "verdanthbasichealer.noun", "verdanthbasichealer_captain.noun":
		healing = 4
	case "verdanthbasichealer_2.noun", "verdanthbasichealer_captain_2.noun":
		healing = 6
	case "verdanthbasichealer_3.noun", "verdanthbasichealer_captain_3.noun":
		healing = 8
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		AbilityName:       "RootedHeal",
		LoopAnimationName: "ver_minn_lf_05_heal_loop",
		Cooldown:          time.Second,
		Range:             35,
		Radius:            6,
		TickDuration:      2 * time.Second,
		MinimumHealing:    healing,
		TrailEffectName:   "ver_minn_lf_05_healingAura.ServerEventDef",
	}, true
}

func stagnantNovaAboveProfile(cooldown time.Duration) ActionProfile {
	return ActionProfile{
		Family: ActionCone, AbilityName: "StagnantNovaAbove",
		AnimationName:          "burrow_idle",
		EmergeDelay:            1500 * time.Millisecond,
		HitDelay:               2800 * time.Millisecond,
		ReleaseDelay:           3400 * time.Millisecond,
		Cooldown:               3400*time.Millisecond + cooldown,
		Range:                  8,
		Radius:                 8,
		Angle:                  360,
		MovementSpeed:          6,
		NonCombatMovementSpeed: 4.5,
		MinimumDamage:          2, MaximumDamage: 4,
		ModifierName:                 "Poison",
		ModifierDuration:             8 * time.Second,
		ModifierChance:               100,
		ModifierTickDuration:         4 * time.Second,
		ModifierMinimumTickDamage:    2,
		ModifierMaximumTickDamage:    4,
		ModifierDamageType:           3,
		ModifierDamageSource:         1,
		ModifierDescriptorMask:       1<<3 | 1<<7,
		ModifierMaximumStack:         1,
		IsModifierDamageProfileKnown: true,
		DescriptorMask:               1<<3 | 1<<7,
		DamageType:                   3,
		DamageSource:                 1,
		IsDamageProfileKnown:         true,
		IsFirstAggroDurationKnown:    true,
	}
}

func verdanthSpecialTwoEntangleProfile(
	attackRange float32, rootDuration time.Duration,
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "Entangle",
		AnimationName: "healer_root",
		HitDelay:      0,
		ReleaseDelay:  1200 * time.Millisecond,
		Cooldown:      16 * time.Second,
		Range:         attackRange,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage: 6, MaximumDamage: 12,
		DamageType:                2,
		DamageSource:              0,
		ModifierName:              "EntangleModifier",
		ModifierDuration:          rootDuration,
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func VerdanthSpecialTwoDirectHealProfile(nounName string) (ActionProfile, bool) {
	healing := float32(0)
	switch strings.ToLower(nounName) {
	case "verdanthspecialtwo.noun", "verdanthspecialtwo_captain.noun":
		healing = 30
	case "verdanthspecialtwo_2.noun", "verdanthspecialtwo_captain_2.noun":
		healing = 40
	case "verdanthspecialtwo_3.noun", "verdanthspecialtwo_captain_3.noun":
		healing = 50
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		AbilityName: "DirectHeal", AnimationName: "healer_heal",
		HitDelay: 500 * time.Millisecond, ReleaseDelay: time.Second,
		Cooldown: 10 * time.Second, Range: 20,
		MinimumHealing: healing, MaximumHealing: healing,
		TargetEffectName:          "life_heal_aura.ServerEventDef",
		ImpactEffectName:          "life_heal.ServerEventDef",
		IsFirstAggroDurationKnown: true,
	}, true
}

func nocturnaBasicHealthDrainProfile(
	attackRange float32, damage float32, lifeSteal float32,
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionChannelDrain, AbilityName: "NocturnaBasicHealthDrain",
		AnimationName:     "nct_minn_su_x4_attack1_start",
		LoopAnimationName: "nct_minn_su_x4_attack1_loop",
		EndAnimationName:  "nct_minn_su_x4_attack1_end",
		HitDelay:          466667 * time.Microsecond,
		ReleaseDelay:      60 * time.Second, Cooldown: 60 * time.Second,
		Range:         attackRange,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage: damage, MaximumDamage: damage,
		DescriptorMask: 128, DamageType: 4, DamageSource: 1,
		TargetEffectName:     "shadow_random01_impact_sparks_effect_grasper.ServerEventDef",
		TickDuration:         time.Second,
		EndAnimationDelay:    666667 * time.Microsecond,
		NumberOfTicks:        8,
		LifeSteal:            lifeSteal,
		IsDamageProfileKnown: true, IsFirstAggroDurationKnown: true,
	}
}

func nocturnaBasicStealthMeleeProfile(
	cooldown time.Duration, movementSpeed float32,
	nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionNomadDrone, AbilityName: "NocturnaBasicStealthMelee",
		AnimationName:          "nct_minn_su_x2_attack1",
		HitDelay:               466666669 * time.Nanosecond,
		ReleaseDelay:           1100000024 * time.Nanosecond,
		Cooldown:               cooldown,
		Range:                  1.125,
		Radius:                 1.5,
		MovementSpeed:          movementSpeed,
		NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage:          3, MaximumDamage: 7,
		DamageCoefficient:    0.05,
		DescriptorMask:       1 | 1<<1 | 1<<6,
		DamageType:           4,
		DamageSource:         0,
		ImpactEffectName:     "necro_common_hit_small.ServerEventDef",
		RevealEffectName:     "shadow_stealther_minion_smoke_effect.ServerEventDef",
		IsSpawnStealthed:     true,
		IsDamageProfileKnown: true,
	}
}

func noctFlyerLobProfile(
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "NoctFlyerLob",
		AnimationName:             "nct_minn_su_05_attack",
		HitDelay:                  606061 * time.Microsecond,
		ReleaseDelay:              1600 * time.Millisecond,
		Cooldown:                  3 * time.Second,
		Range:                     15,
		MovementSpeed:             movementSpeed,
		NonCombatMovementSpeed:    nonCombatMovementSpeed,
		MinimumDamage:             1,
		MaximumDamage:             1,
		DescriptorMask:            8320,
		DamageType:                4,
		DamageSource:              1,
		ModifierName:              "NoctFlyerPoison",
		ModifierDuration:          6 * time.Second,
		ModifierTickDuration:      2 * time.Second,
		ModifierTickDamage:        2,
		ModifierMaximumStack:      3,
		ProjectileNoun:            "Ability_Epic_Fireball.Noun",
		TrailEffectName:           "shadow_minion_lob_effect.ServerEventDef",
		ImpactEffectName:          "shadow_minion_lob_explosion.ServerEventDef",
		MissEffectName:            "ineffective_common_small.ServerEventDef",
		ProjectileSpeed:           10,
		ProjectileDistance:        15,
		ProjectileHeight:          2,
		Radius:                    0.005,
		ProjectileOffset:          game.Vec3{Y: 0.5, Z: 2.8},
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func zelemBasicPackflyAttackProfile(cooldown time.Duration) ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "PackflyAttack",
		AnimationName:          "zlm_minn_sp_x3_attack1",
		HitDelay:               360 * time.Millisecond,
		ReleaseDelay:           1130 * time.Millisecond,
		Cooldown:               cooldown,
		Range:                  1.5,
		MovementSpeed:          4,
		NonCombatMovementSpeed: 2.5,
		MinimumDamage:          3, MaximumDamage: 6,
		DamageCoefficient:         0,
		DescriptorMask:            1 | 1<<1 | 1<<6,
		DamageType:                2,
		DamageSource:              0,
		Radius:                    2.25,
		ImpactEffectName:          "spacetime_bite.ServerEventDef",
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func zelemBasicRepairArcWeldingProfile(
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionNomadDrone, AbilityName: "ArcWeldingMelee",
		AnimationName:             "zlm_minn_tc_2_attack1",
		HitDelay:                  533333361 * time.Nanosecond,
		ReleaseDelay:              1600 * time.Millisecond,
		Cooldown:                  1500 * time.Millisecond,
		Range:                     0.75,
		Radius:                    1.25,
		MovementSpeed:             movementSpeed,
		NonCombatMovementSpeed:    nonCombatMovementSpeed,
		MinimumDamage:             4,
		MaximumDamage:             7,
		DescriptorMask:            1 | 1<<1 | 1<<6,
		DamageType:                0,
		DamageSource:              0,
		ImpactEffectName:          "cyber_common_hit_small.ServerEventDef",
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func NoctMinionDrainerMeleeProfile(nounName string) (ActionProfile, bool) {
	var cooldown time.Duration
	var movementSpeed float32
	var nonCombatMovementSpeed float32
	switch strings.ToLower(nounName) {
	case "nct_minn_su_drainer.noun":
		cooldown = 2 * time.Second
		movementSpeed, nonCombatMovementSpeed = 4.5, 3.5
	case "nct_minn_su_drainer_2.noun":
		cooldown = 1500 * time.Millisecond
		movementSpeed, nonCombatMovementSpeed = 6.5, 4.5
	case "nct_minn_su_drainer_3.noun":
		cooldown = time.Second
		movementSpeed, nonCombatMovementSpeed = 8.5, 5.5
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionMelee, AbilityName: "DrainerMelee",
		AnimationName:          "nct_minn_su_drainer_attack2",
		HitDelay:               430 * time.Millisecond,
		ReleaseDelay:           1070 * time.Millisecond,
		Cooldown:               cooldown,
		Range:                  0.75,
		MovementSpeed:          movementSpeed,
		NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage:          3, MaximumDamage: 6,
		DamageCoefficient:         0,
		DescriptorMask:            1 | 1<<1 | 1<<6,
		DamageType:                4,
		DamageSource:              0,
		Radius:                    1.25,
		ImpactEffectName:          "shadow_bolt_impact.ServerEventDef",
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}, true
}

func noctMinionManaDrainProfile(nounName string) (ActionProfile, bool) {
	var drainFraction float32
	var movementSpeed float32
	var nonCombatMovementSpeed float32
	switch strings.ToLower(nounName) {
	case "nct_minn_su_drainer.noun":
		drainFraction = 0.01
		movementSpeed, nonCombatMovementSpeed = 4.5, 3.5
	case "nct_minn_su_drainer_2.noun":
		drainFraction = 0.02
		movementSpeed, nonCombatMovementSpeed = 6.5, 4.5
	case "nct_minn_su_drainer_3.noun":
		drainFraction = 0.03
		movementSpeed, nonCombatMovementSpeed = 8.5, 5.5
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionChannelDrain, AbilityName: "ManaDrain",
		AnimationName:             "nct_minn_su_drainer_attack1_start",
		LoopAnimationName:         "nct_minn_su_drainer_attack1_loop",
		EndAnimationName:          "nct_minn_su_drainer_attack1_end",
		HitDelay:                  330 * time.Millisecond,
		ReleaseDelay:              60 * time.Second,
		Cooldown:                  60 * time.Second,
		Range:                     5,
		MovementSpeed:             movementSpeed,
		NonCombatMovementSpeed:    nonCombatMovementSpeed,
		TargetEffectName:          "shadow_drainer_impact_sparks_effect.ServerEventDef",
		TrailEffectName:           "shadow_drainer_beam_effect.ServerEventDef",
		TickDuration:              time.Second,
		EndAnimationDelay:         730 * time.Millisecond,
		NumberOfTicks:             60,
		ManaDrainFraction:         drainFraction,
		MaximumChannelDistance:    7,
		IsFirstAggroDurationKnown: true,
	}, true
}

func verdanthBasicOozeMeleeProfile(cooldown time.Duration) ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "OozeMelee",
		AnimationName:             "ver_minn_lf_x4_attack1",
		FirstAggroAnimationName:   "first_aggro_roar",
		FirstAggroDelay:           1750 * time.Millisecond,
		HitDelay:                  460 * time.Millisecond,
		ReleaseDelay:              1433333 * time.Microsecond,
		Cooldown:                  cooldown,
		Range:                     1.125,
		MovementSpeed:             6.5,
		NonCombatMovementSpeed:    3,
		MinimumDamage:             2,
		MaximumDamage:             6,
		DescriptorMask:            1 | 1<<1 | 1<<6,
		DamageType:                3,
		DamageSource:              0,
		Radius:                    1.5,
		ImpactEffectName:          "ver_minn_lf_x4_oozeKiss_hit.ServerEventDef",
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func VerdanthBasicOozeGrowProfile(nounName string) (ActionProfile, bool) {
	var cooldown time.Duration
	switch strings.ToLower(nounName) {
	case "verdanthbasicooze.noun":
		cooldown = 9500 * time.Millisecond
	case "verdanthbasicooze_2.noun":
		cooldown = 7 * time.Second
	case "verdanthbasicooze_3.noun":
		cooldown = 4500 * time.Millisecond
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionMelee, AbilityName: "OozeGrow",
		AnimationName:             "ver_minn_lf_x4_grow",
		HitDelay:                  230 * time.Millisecond,
		ReleaseDelay:              1260 * time.Millisecond,
		Cooldown:                  cooldown,
		Range:                     50,
		MovementSpeed:             6.5,
		NonCombatMovementSpeed:    3,
		ModifierName:              "OozeGrowthBuff",
		ModifierMaximumStack:      4,
		ForcedMovementDistance:    3,
		IsFirstAggroDurationKnown: true,
	}, true
}

func citadelGrenadeRollProfile() ActionProfile {
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "CitadelGrenadeRoll",
		AnimationName:          "ctd_minn_tc_x3_attack1",
		HitDelay:               363636 * time.Microsecond,
		ReleaseDelay:           750 * time.Millisecond,
		Cooldown:               4 * time.Second,
		Range:                  12,
		MovementSpeed:          5.5,
		NonCombatMovementSpeed: 4,
		MinimumDamage:          4, MaximumDamage: 8,
		DamageCoefficient:         0.05,
		DescriptorMask:            136,
		DamageType:                0,
		DamageSource:              1,
		ProjectileNoun:            "CitadelRollingGrenade.Noun",
		ImpactEffectName:          "ctd_minn_tc_x3_grenade_explosion_AOE.ServerEventDef",
		ProjectileSpeed:           8,
		ProjectileDistance:        14,
		Radius:                    2,
		ProjectileOffset:          game.Vec3{X: 0.5, Y: 0.5},
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func zelemBasicFlyingMeleeProfile(
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "ZelemBasicFlyingMeleeAttack",
		AnimationName:             "zlm_minn_sp_02_attack1",
		FirstAggroAnimationName:   "zelem_flying_melee_beamin",
		FirstAggroDelay:           933300 * time.Microsecond,
		HitDelay:                  600 * time.Millisecond,
		ReleaseDelay:              1300 * time.Millisecond,
		Cooldown:                  2 * time.Second,
		Range:                     1.5,
		MovementSpeed:             movementSpeed,
		NonCombatMovementSpeed:    nonCombatMovementSpeed,
		MinimumDamage:             3,
		MaximumDamage:             6,
		DescriptorMask:            67,
		DamageType:                1,
		DamageSource:              0,
		ImpactEffectName:          "spacetime_bite.ServerEventDef",
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func zelemBasicRangedHomingProfile(
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "ZelemBasicRangedHomingProjectile",
		AnimationName: "zlm_minn_sp_05_attack",
		HitDelay:      366667 * time.Microsecond, ReleaseDelay: time.Second,
		Cooldown: 4 * time.Second, Range: 15, MovementSpeed: movementSpeed,
		NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage:          4, MaximumDamage: 8, DamageCoefficient: 0.05,
		ProjectileNoun:   "Ability_Fireball.Noun",
		TrailEffectName:  "spacetime_erratic_shot_effect.ServerEventDef",
		ImpactEffectName: "spacetime_bite.ServerEventDef",
		MissEffectName:   "ineffective_common_small.ServerEventDef",
		ProjectileSpeed:  6, ProjectileDistance: 18, HomingDelay: time.Second,
		ProjectileOffset: game.Vec3{X: 1, Y: 1},
	}
}

func nomadSnipeSlowProfile(
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionNomadSnipe, AbilityName: "NomadSnipe_Slow",
		AnimationName: "nomad_lieu_sp_4_attack2", FirstAggroAnimationName: "gen_aggro_sp_beam_in",
		FirstAggroDelay: 1230 * time.Millisecond, IsFirstAggroDurationKnown: true,
		HitDelay: 500 * time.Millisecond, ReleaseDelay: 1300 * time.Millisecond,
		Cooldown: 4 * time.Second, Range: 12, MovementSpeed: movementSpeed,
		NonCombatMovementSpeed: nonCombatMovementSpeed,
		ModifierName:           "NomadSnipe_SlowDebuff", ModifierDuration: 6 * time.Second,
	}
}

func zelemChargeupStandardProfile() ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "ZelemChargeupStandardAttack",
		AnimationName:          "zlm_minn_sp_x4_attack3",
		HitDelay:               300 * time.Millisecond,
		ReleaseDelay:           800 * time.Millisecond,
		Cooldown:               2 * time.Second,
		Range:                  1,
		MovementSpeed:          7,
		NonCombatMovementSpeed: 6,
		MinimumDamage:          3, MaximumDamage: 6,
		DescriptorMask:            67,
		DamageType:                1,
		DamageSource:              0,
		ImpactEffectName:          "spacetime_bite.ServerEventDef",
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func ZelemChargeupDischargeProfile() ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "ZelemChargeupDischargeAttack",
		AnimationName:          "zlm_minn_sp_x4_attack1",
		HitDelay:               533333 * time.Microsecond,
		ReleaseDelay:           1500 * time.Millisecond,
		Cooldown:               time.Second,
		Range:                  2,
		MovementSpeed:          7,
		NonCombatMovementSpeed: 6,
		MinimumDamage:          8, MaximumDamage: 16,
		DescriptorMask:             65,
		DamageType:                 1,
		DamageSource:               0,
		ModifierName:               "ZelemChargeupKnockback",
		ImpactEffectName:           "spacetime_bite.ServerEventDef",
		ForcedMovementSpeed:        12,
		ForcedMovementDistance:     6,
		ForcedMovementReactionName: "react_knockback",
		IsDamageProfileKnown:       true,
		IsFirstAggroDurationKnown:  true,
	}
}

func ZelemChargeupBuildProfile(nounName string) (ActionProfile, bool) {
	animationName := ""
	switch strings.ToLower(nounName) {
	case "zelembasicchargeup.noun", "zelembasicchargeup_captain.noun":
		animationName = "zlm_minn_sp_x4_chargeup_d1"
	case "zelembasicchargeup_2.noun", "zelembasicchargeup_captain_2.noun":
		animationName = "zlm_minn_sp_x4_chargeup_d2"
	case "zelembasicchargeup_3.noun", "zelembasicchargeup_captain_3.noun":
		animationName = "zlm_minn_sp_x4_chargeup_d3"
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionMelee, AbilityName: "ZelemChargeupBuildCharge",
		AnimationName:             animationName,
		HitDelay:                  700 * time.Millisecond,
		ReleaseDelay:              3500 * time.Millisecond,
		Cooldown:                  16 * time.Second,
		MovementSpeed:             7,
		NonCombatMovementSpeed:    6,
		TargetEffectName:          "zelem_chargeup_charge.ServerEventDef",
		IsFirstAggroDurationKnown: true,
	}, true
}

func rezzerResurrectionProfile(
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionResurrect, AbilityName: "Resurrect",
		AnimationName: "cast_entangle",
		HitDelay:      2 * time.Second, ReleaseDelay: 2 * time.Second,
		Cooldown: 15 * time.Second, Range: 16,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		HealFraction:              0.4,
		TrailEffectName:           "resurrect_cast.ServerEventDef",
		TargetEffectName:          "resurrect_target_chargeup.ServerEventDef",
		ImpactEffectName:          "resurrect_target.ServerEventDef",
		IsFirstAggroDurationKnown: true,
	}
}

func RezzerGhostlyBoltProfile(nounName string) (ActionProfile, bool) {
	cooldown := time.Duration(0)
	attackRange := float32(0)
	projectileSpeed := float32(0)
	movementSpeed := float32(0)
	nonCombatMovementSpeed := float32(0)
	switch strings.ToLower(nounName) {
	case "rezzer.noun":
		cooldown = 4 * time.Second
		attackRange = 14
		projectileSpeed = 10
		movementSpeed = 4.5
		nonCombatMovementSpeed = 3.5
	case "rezzer_captain.noun":
		cooldown = 4 * time.Second
		attackRange = 14
		projectileSpeed = 10
		movementSpeed = 9

		nonCombatMovementSpeed = 7
	case "rezzer_2.noun", "rezzer_captain_2.noun":
		cooldown = 3250 * time.Millisecond
		attackRange = 16
		projectileSpeed = 14
		movementSpeed = 6.5
		nonCombatMovementSpeed = 4.5
	case "rezzer_3.noun", "rezzer_captain_3.noun":
		cooldown = 2500 * time.Millisecond
		attackRange = 18
		projectileSpeed = 18
		movementSpeed = 8.5
		nonCombatMovementSpeed = 5.5
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "GhostlyBolt",
		AnimationName: "cast_ghostlybolt",
		HitDelay:      170 * time.Millisecond, ReleaseDelay: 170 * time.Millisecond,
		Cooldown: cooldown, Range: attackRange,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage: 8, MaximumDamage: 14, DamageCoefficient: 0.05,
		DescriptorMask: 8320, DamageType: 4, DamageSource: 1,
		ProjectileNoun:   "Ability_Fireball.Noun",
		TrailEffectName:  "ghostly_bolt.ServerEventDef",
		ImpactEffectName: "ghostly_bolt_impact.ServerEventDef",
		MissEffectName:   "ineffective_common_small.ServerEventDef",
		ProjectileSpeed:  projectileSpeed, ProjectileDistance: 40,
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}, true
}

func weaverResurrectionProfile() ActionProfile {
	profile := rezzerResurrectionProfile(9, 7)
	profile.PassivePhysicalDefense = 250
	profile.PassiveEffectName = "shadow_ghostform_shader_effect.ServerEventDef"
	return profile
}

func boomerChargeProfile(attackRange float32) ActionProfile {
	return ActionProfile{
		Family: ActionCharge, AbilityName: "BoomerCharge",
		AnimationName: "cast_boomercharge", EndAnimationName: "boomercharge_relax",
		HitDelay:     1366667 * time.Microsecond,
		ReleaseDelay: 1366667 * time.Microsecond,
		Cooldown:     12 * time.Second, Range: attackRange,
		MovementSpeed: 3, NonCombatMovementSpeed: 1.5,
		ForcedMovementSpeed: 8, ForcedMovementStopDistance: 2,
		EndAnimationDelay:         766667 * time.Microsecond,
		TrailEffectName:           "charging_trail_effect.ServerEventDef",
		IsFirstAggroDurationKnown: true,
	}
}

func verdanthBasicSkeetDartingProfile(
	cooldown time.Duration, movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionCharge, AbilityName: "DartingAttack",
		AnimationName:          "ver_minn_lf_03_attack1",
		HitDelay:               967 * time.Millisecond,
		ReleaseDelay:           100 * time.Second,
		Cooldown:               cooldown,
		Range:                  35,
		MovementSpeed:          movementSpeed,
		NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage:          4, MaximumDamage: 8,
		DamageCoefficient:          0.05,
		DescriptorMask:             1 | 1<<1 | 1<<7,
		DamageType:                 2,
		DamageSource:               0,
		ForcedMovementSpeed:        6,
		ForcedMovementStopDistance: 0.75,
		ImpactEffectName:           "ver_minn_lf_03_tailSting_hit.ServerEventDef",
		IsDamageProfileKnown:       true,
	}
}

func verdanthBasicPlungeProfile(
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionRetainedArea, AbilityName: "PlungeAttack",
		AnimationName:          "ver_minn_lf_04_attack1",
		HitDelay:               2500 * time.Millisecond,
		ReleaseDelay:           2866667 * time.Microsecond,
		Cooldown:               5 * time.Second,
		Range:                  8,
		Radius:                 2,
		MovementSpeed:          movementSpeed,
		NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage:          8, MaximumDamage: 12, DamageCoefficient: 0.05,
		DescriptorMask: 1<<3 | 1<<6, DamageType: 2, DamageSource: 0,
		EmergeEffectName:          "ver_minn_lf_04_thornBurrow_emerge.ServerEventDef",
		EmergeDelay:               time.Second,
		ImpactEffectName:          "ver_minn_lf_04_thornBurrow_hit.ServerEventDef",
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func VerdanthSpecialThreeFastSwipeProfile(
	nounName string,
) (ActionProfile, bool) {
	pullProfile, isFound := VerdanthSpecialThreePullProfile(nounName)
	if !isFound {
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionMelee, AbilityName: "FastSwipe",
		AnimationName:          "puller_melee",
		HitDelay:               0,
		ReleaseDelay:           1640 * time.Millisecond,
		Cooldown:               5 * time.Second,
		Range:                  2.5,
		Radius:                 2.5,
		MovementSpeed:          pullProfile.MovementSpeed,
		NonCombatMovementSpeed: pullProfile.NonCombatMovementSpeed,
		MinimumDamage:          1,
		MaximumDamage:          4,
		DamageCoefficient:      0,
		DescriptorMask:         1 | 1<<7,
		DamageType:             4,
		DamageSource:           0,
		ImpactEffectName:       "puller_melee_fast_hit.ServerEventDef",
		IsDamageProfileKnown:   true,
	}, true
}

func VerdanthSpecialThreePullProfile(nounName string) (ActionProfile, bool) {
	cooldown := time.Duration(0)
	projectileSpeed := float32(0)
	movementSpeed := float32(0)
	nonCombatMovementSpeed := float32(0)
	switch strings.ToLower(nounName) {
	case "verdanthspecialthree.noun", "verdanthspecialthree_captain.noun":
		cooldown, projectileSpeed = 10*time.Second, 12
		movementSpeed, nonCombatMovementSpeed = 4.5, 3
	case "verdanthspecialthree_2.noun", "verdanthspecialthree_captain_2.noun":
		cooldown, projectileSpeed = 8*time.Second, 16
		movementSpeed, nonCombatMovementSpeed = 6.5, 3.5
	case "verdanthspecialthree_3.noun", "verdanthspecialthree_captain_3.noun":
		cooldown, projectileSpeed = 6*time.Second, 20
		movementSpeed, nonCombatMovementSpeed = 8.5, 4
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "Puller",
		AnimationName:              "puller_pull",
		HitDelay:                   170000002 * time.Nanosecond,
		ReleaseDelay:               1250 * time.Millisecond,
		Cooldown:                   cooldown,
		Range:                      15,
		MovementSpeed:              movementSpeed,
		NonCombatMovementSpeed:     nonCombatMovementSpeed,
		ModifierName:               "PullModifier",
		ModifierID:                 0x54417823,
		ModifierDuration:           3 * time.Second,
		ProjectileNoun:             "Ability_Fireball.Noun",
		TrailEffectName:            "puller_beam_attack_beam_tip.ServerEventDef",
		ImpactEffectName:           "puller_beam_attack_hit.ServerEventDef",
		MissEffectName:             "puller_beam_attack_miss.ServerEventDef",
		ProjectileSpeed:            projectileSpeed,
		ProjectileDistance:         15,
		ForcedMovementSpeed:        10,
		ForcedMovementDuration:     time.Second,
		ForcedMovementReactionName: "react_pulled_short",
		ForcedMovementEffectName:   "puller_beam_attack_beam.ServerEventDef",
		IsPull:                     true,
		IsDamageProfileKnown:       true,
	}, true
}

func cryosBasicChargeProfile(
	cooldown time.Duration, attackRange float32,
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionCharge, AbilityName: "CryosBasicCharge",
		AnimationName:          "cry_minn_el_5_charge_start",
		LoopAnimationName:      "cry_minn_el_5_charge_loop",
		EndAnimationName:       "cry_minn_el_5_charge_end",
		HitDelay:               800 * time.Millisecond,
		ReleaseDelay:           100 * time.Second,
		Cooldown:               cooldown,
		Range:                  attackRange,
		MovementSpeed:          movementSpeed,
		NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage:          4, MaximumDamage: 8,
		DamageCoefficient:          0.05,
		DescriptorMask:             1 | 1<<7,
		DamageType:                 3,
		DamageSource:               0,
		ForcedMovementSpeed:        5,
		ForcedMovementStopDistance: 0.75,
		EndAnimationDelay:          time.Second,
		TrailEffectName:            "nomad_lieu_lf_1_charge.ServerEventDef",
		ImpactEffectName:           "charge_impact_effect.ServerEventDef",
		IsDamageProfileKnown:       true,
	}
}

func noctGhostChargeProfile(
	cooldown time.Duration, attackRange float32, movementSpeedBuff float32,
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionCharge, AbilityName: "NoctGhostCharge",
		AnimationName:              "nct_minn_su_03_charge_start",
		LoopAnimationName:          "nct_minn_su_03_charge_loop",
		EndAnimationName:           "nct_minn_su_03_charge_end",
		HitDelay:                   850000024 * time.Nanosecond,
		ReleaseDelay:               100 * time.Second,
		Cooldown:                   cooldown,
		Range:                      attackRange,
		MovementSpeed:              movementSpeed,
		NonCombatMovementSpeed:     nonCombatMovementSpeed,
		MinimumDamage:              6,
		MaximumDamage:              12,
		DescriptorMask:             1 << 7,
		DamageType:                 4,
		DamageSource:               1,
		MovementSpeedBuff:          movementSpeedBuff,
		ForcedMovementStopDistance: 0.1,
		EndAnimationDelay:          1033332944 * time.Nanosecond,
		ImpactEffectName:           "shadow_pass_thru_damage_effect.ServerEventDef",
		PassiveEffectName:          "shadow_ghostform_shader_effect.ServerEventDef",
		IsDamageProfileKnown:       true,
	}
}

func NoctGhostChargerPoseProfile() ActionProfile {
	return ActionProfile{
		Family: ActionUnknown, AbilityName: "NoctGhostChargerPose",
		AnimationName:          "nct_minn_su_03_pose",
		ReleaseDelay:           1700000048 * time.Nanosecond,
		Cooldown:               6 * time.Second,
		Range:                  32,
		MovementSpeed:          6.5,
		NonCombatMovementSpeed: 5,
	}
}

func CryosBasicChargeHeadbuttProfile(
	cooldown time.Duration, movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "CryosBasicChargeHeadbutt",
		AnimationName:          "cry_minn_el_5_attack1",
		HitDelay:               240 * time.Millisecond,
		ReleaseDelay:           600 * time.Millisecond,
		Cooldown:               cooldown,
		Range:                  0.75,
		Radius:                 1.25,
		MovementSpeed:          movementSpeed,
		NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage:          3, MaximumDamage: 6,
		DamageCoefficient:    0.05,
		DescriptorMask:       1 | 1<<1 | 1<<7,
		DamageType:           3,
		DamageSource:         0,
		ImpactEffectName:     "charge_impact_small_effect.ServerEventDef",
		IsDamageProfileKnown: true,
	}
}

func ShouldCryosBasicChargeHeadbutt(distance float32) bool {
	return distance >= 0 && distance <= 10 &&
		!math.IsNaN(float64(distance)) && !math.IsInf(float64(distance), 0)
}

func verdanthBasicPickyChargeProfile(cooldown time.Duration) ActionProfile {
	return ActionProfile{
		Family: ActionCharge, AbilityName: "VerdanthBasicPicky",
		AnimationName:              "ver_minn_lf_x3_charge_start",
		LoopAnimationName:          "ver_minn_lf_x3_charge_loop",
		EndAnimationName:           "ver_minn_lf_x3_charge_end_land",
		HitDelay:                   1366667 * time.Microsecond,
		ReleaseDelay:               100 * time.Second,
		Cooldown:                   cooldown,
		Range:                      25,
		MovementSpeed:              4.5,
		NonCombatMovementSpeed:     3,
		ForcedMovementSpeed:        3,
		ForcedMovementStopDistance: 4.5,
		TrailEffectName:            "nomad_lieu_lf_1_charge.ServerEventDef",
	}
}

func VerdanthBasicPickyChargingAttackProfile() ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "VerdanthBasicPickyChargingAttack",
		AnimationName:          "ver_minn_lf_x3_charge_end_melee",
		HitDelay:               100 * time.Millisecond,
		ReleaseDelay:           800 * time.Millisecond,
		Cooldown:               500 * time.Millisecond,
		Range:                  2,
		Radius:                 3,
		MovementSpeed:          4.5,
		NonCombatMovementSpeed: 3,
		MinimumDamage:          10, MaximumDamage: 18,
		DamageCoefficient:    0.05,
		DescriptorMask:       1 | 1<<7,
		DamageType:           2,
		DamageSource:         0,
		ImpactEffectName:     "charge_impact_small_effect.ServerEventDef",
		IsDamageProfileKnown: true,
	}
}

func VerdanthBasicPickyMeleeProfile() ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "VerdanthBasicPickyMelee",
		AnimationName:          "ver_minn_lf_x3_attack2",
		HitDelay:               366666675 * time.Nanosecond,
		ReleaseDelay:           1066666961 * time.Nanosecond,
		Cooldown:               2 * time.Second,
		Range:                  0.75,
		Radius:                 1,
		MovementSpeed:          4.5,
		NonCombatMovementSpeed: 3,
		MinimumDamage:          4, MaximumDamage: 6,
		DamageCoefficient:    0,
		DescriptorMask:       1 | 1<<1 | 1<<7,
		DamageType:           2,
		DamageSource:         0,
		ImpactEffectName:     "life_common_melee_hit.ServerEventDef",
		IsDamageProfileKnown: true,
	}
}

func ShouldVerdanthBasicPickyCharge(distance float32) bool {
	return distance > 10 &&
		!math.IsNaN(float64(distance)) && !math.IsInf(float64(distance), 0)
}

func BoomerSmashProfile() ActionProfile {
	return ActionProfile{
		Family: ActionUnknown, AbilityName: "Smash", AnimationName: "cast_swipe",
		HitDelay: 250 * time.Millisecond, ReleaseDelay: 1230 * time.Millisecond,
		Cooldown: 1500 * time.Millisecond, Range: 2,
		MovementSpeed: 3, NonCombatMovementSpeed: 1.5,
		MinimumDamage: 4, MaximumDamage: 6,
		DescriptorMask: 1 | 1<<1 | 1<<7, DamageType: 3, DamageSource: 1,
		ImpactEffectName:     "plasma_common_electric_hit_medium_effect.ServerEventDef",
		IsDamageProfileKnown: true, IsFirstAggroDurationKnown: true,
	}
}

func slothTailZapProfile(
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "TailZap",
		AnimationName: "cast_tailzap",
		HitDelay:      430000007 * time.Nanosecond,
		ReleaseDelay:  1200000048 * time.Nanosecond,
		Cooldown:      2 * time.Second,
		Range:         0.75,
		Radius:        1.25,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage: 5, MaximumDamage: 10,
		DescriptorMask:       67,
		DamageType:           3,
		DamageSource:         0,
		ImpactEffectName:     "charge_impact_small_effect.ServerEventDef",
		IsDamageProfileKnown: true,
	}
}

func nomadSpecialOneChargeProfile(
	cooldown time.Duration, attackRange float32,
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionCharge, AbilityName: "NomadSpecialOneCharge",
		AnimationName:              "nomad_lieu_lf_1_charge_start",
		LoopAnimationName:          "nomad_lieu_lf_1_charge_loop",
		EndAnimationName:           "nomad_lieu_lf_1_charge_end_land",
		HitDelay:                   1100 * time.Millisecond,
		ReleaseDelay:               1100 * time.Millisecond,
		Cooldown:                   cooldown,
		Range:                      attackRange,
		MovementSpeed:              movementSpeed,
		NonCombatMovementSpeed:     nonCombatMovementSpeed,
		ForcedMovementSpeed:        3,
		ForcedMovementStopDistance: 0.75,
		TrailEffectName:            "nomad_lieu_lf_1_charge.ServerEventDef",
		IsFirstAggroDurationKnown:  true,
	}
}

func nocturnaSpecialDriftChargeProfile(
	cooldown time.Duration, attackRange float32,
	silenceDuration time.Duration, movementSpeed float32,
	nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionCharge, AbilityName: "NocturnaSpecialDriftCharge",
		AnimationName:              "nomad_lieu_su_4_charge_start",
		LoopAnimationName:          "nomad_lieu_su_4_charge_loop",
		EndAnimationName:           "nomad_lieu_su_4_charge_end",
		HitDelay:                   866667 * time.Microsecond,
		ReleaseDelay:               1900 * time.Millisecond,
		Cooldown:                   cooldown,
		Range:                      attackRange,
		MovementSpeed:              movementSpeed,
		NonCombatMovementSpeed:     nonCombatMovementSpeed,
		MinimumDamage:              8,
		MaximumDamage:              14,
		DescriptorMask:             1<<3 | 1<<7 | 1<<14,
		DamageType:                 4,
		DamageSource:               1,
		ModifierName:               "SilenceModifier",
		ModifierDuration:           silenceDuration,
		ImpactEffectName:           "shadow_pass_thru_damage_effect.ServerEventDef",
		ForcedMovementSpeed:        10,
		ForcedMovementStopDistance: 0.1,
		EndAnimationDelay:          1033333 * time.Microsecond,
		IsDamageProfileKnown:       true,
		IsFirstAggroDurationKnown:  true,
	}
}

func NocturnaSpecialDriftAuraProfile(nounName string) (ActionProfile, bool) {
	damage := float32(0)
	switch strings.ToLower(nounName) {
	case "nocturnaspecialdrift.noun", "nocturnaspecialdrift_captain.noun":
		damage = 1
	case "nocturnaspecialdrift_2.noun", "nocturnaspecialdrift_captain_2.noun":
		damage = 2
	case "nocturnaspecialdrift_3.noun", "nocturnaspecialdrift_captain_3.noun":
		damage = 3
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionCone, AbilityName: "NocturnaSpecialDriftAura",
		Radius: 8, Angle: 360,
		MinimumDamage: damage, MaximumDamage: damage,
		DescriptorMask:       1<<3 | 1<<7 | 1<<13,
		DamageType:           4,
		DamageSource:         1,
		LifeSteal:            1,
		ImpactEffectName:     "shadow_random01_impact_sparks_effect_grasper.ServerEventDef",
		IsDamageProfileKnown: true,
	}, true
}

func NomadSpecialOneChargingAttackProfile() ActionProfile {
	return ActionProfile{
		Family: ActionUnknown, AbilityName: "NomadSpecialOneChargeChargingAttack",
		AnimationName: "nomad_lieu_lf_1_charge_end_melee",
		HitDelay:      0,
		ReleaseDelay:  766667 * time.Microsecond,
		Cooldown:      500 * time.Millisecond,
		Range:         0.75,
		MovementSpeed: 5,
		MinimumDamage: 10, MaximumDamage: 18,
		DescriptorMask:             1 | 1<<1 | 1<<6,
		DamageType:                 2,
		DamageSource:               0,
		ModifierName:               "NomadBioSpecialOneKnockbackModifier",
		ImpactEffectName:           "nomad_lieu_lf_1_charge_hit.ServerEventDef",
		ForcedMovementSpeed:        3,
		ForcedMovementDistance:     2,
		ForcedMovementReactionName: "react_knockup",
		IsDamageProfileKnown:       true,
		IsFirstAggroDurationKnown:  true,
	}
}

func cryosSpecialThreeSleepMushroomProfile(
	cooldown time.Duration, attackRange float32, sleepDuration time.Duration,
	movementSpeed float32, nonCombatMovementSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "SleepMushroom",
		AnimationName:             "cry_lieu_lf_sleepRez_attack1",
		HitDelay:                  150 * time.Millisecond,
		ReleaseDelay:              900 * time.Millisecond,
		Cooldown:                  cooldown,
		Range:                     attackRange,
		MovementSpeed:             movementSpeed,
		NonCombatMovementSpeed:    nonCombatMovementSpeed,
		ProjectileNoun:            "Ability_Fireball.Noun",
		TrailEffectName:           "life_sleep_spore_projectile.ServerEventDef",
		ImpactEffectName:          "life_sleep_spore_cloud.ServerEventDef",
		ProjectileSpeed:           1,
		ProjectileHeight:          8,
		ProjectileFlightDuration:  time.Second,
		ProjectileOffset:          game.Vec3{Z: 1.3},
		Radius:                    2,
		TickDuration:              2 * time.Second,
		NumberOfTicks:             3,
		ModifierName:              "SleepModifier",
		ModifierDuration:          sleepDuration,
		IsFirstAggroDurationKnown: true,
	}
}

func CryosSpecialThreeRezMeleeProfile(nounName string) (ActionProfile, bool) {
	cooldown := time.Duration(0)
	movementSpeed := float32(0)
	nonCombatMovementSpeed := float32(0)
	switch strings.ToLower(nounName) {
	case "cryosspecialthree.noun", "cryosspecialthree_captain.noun":
		cooldown = 2 * time.Second
		movementSpeed, nonCombatMovementSpeed = 3.5, 2
	case "cryosspecialthree_2.noun", "cryosspecialthree_captain_2.noun":
		cooldown = 1750 * time.Millisecond
		movementSpeed, nonCombatMovementSpeed = 5, 3
	case "cryosspecialthree_3.noun", "cryosspecialthree_captain_3.noun":
		cooldown = 1500 * time.Millisecond
		movementSpeed, nonCombatMovementSpeed = 6.5, 4
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		Family: ActionMelee, AbilityName: "RezMelee",
		AnimationName: "cry_lieu_lf_sleepRez_attack2",
		HitDelay:      250 * time.Millisecond,
		ReleaseDelay:  900 * time.Millisecond,
		Cooldown:      cooldown,
		Range:         1.5,
		MovementSpeed: movementSpeed, NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage: 8, MaximumDamage: 12,
		DescriptorMask:            1 | 1<<1 | 1<<6,
		DamageType:                2,
		DamageSource:              0,
		ImpactEffectName:          "life_common_melee_hit.ServerEventDef",
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}, true
}

func citadelFireBreathProfile(
	minimumDamage float32, maximumDamage float32, radius float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionCone, AbilityName: "CitadelMinionMelee_Flamethrower",
		AnimationName:             "cry_minn_el_1_attack1",
		FirstAggroAnimationName:   "cry_minn_el_1_burrow_out",
		FirstAggroDelay:           1630 * time.Millisecond,
		HitDelay:                  430 * time.Millisecond,
		ReleaseDelay:              1200 * time.Millisecond,
		Cooldown:                  3 * time.Second,
		Range:                     radius,
		Radius:                    radius,
		Angle:                     120,
		MovementSpeed:             6.5,
		NonCombatMovementSpeed:    5,
		MinimumDamage:             minimumDamage,
		MaximumDamage:             maximumDamage,
		DescriptorMask:            128,
		DamageType:                3,
		DamageSource:              1,
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func ZelemSpecialTwoPushProfile() ActionProfile {
	return ActionProfile{
		Family: ActionPushPull, AbilityName: "ZelemSpecialTwo_Push",
		AnimationName: "zlm_lieu_sp_2_attack2",
		HitDelay:      560 * time.Millisecond, ReleaseDelay: 1866600 * time.Microsecond,
		Cooldown: 8 * time.Second, Range: 7, MovementSpeed: 4.5,
		NonCombatMovementSpeed: 3,
		Radius:                 7,
		MinimumDamage:          10, MaximumDamage: 16,
		ForcedMovementSpeed: 12, ForcedMovementDistance: 6,
		ForcedMovementReactionName: "react_knockback",
	}
}

func ZelemSpecialTwoPullProfile() ActionProfile {
	return ActionProfile{
		Family: ActionPushPull, AbilityName: "ZelemSpecialTwo_Pull",
		AnimationName: "zlm_lieu_sp_2_attack1",
		HitDelay:      330 * time.Millisecond, ReleaseDelay: 1730 * time.Millisecond,
		Cooldown: 12 * time.Second, Range: 10, MovementSpeed: 4.5,
		NonCombatMovementSpeed: 3,
		ForcedMovementSpeed:    25, ForcedMovementStopDistance: 1,
		ForcedMovementEffectName: "spacetime_lieu_pull_affectedEnemy_effect.ServerEventDef",
		IsPull:                   true,
	}
}

func ZelemSpecialTwoActionProfile(distance float32) (ActionProfile, error) {
	if distance < 0 || math.IsNaN(float64(distance)) || math.IsInf(float64(distance), 0) {
		return ActionProfile{}, errors.New("zelem special two distance invalid")
	}
	if distance <= 5 {
		return ZelemSpecialTwoPushProfile(), nil
	}
	return ZelemSpecialTwoPullProfile(), nil
}

func ActionProfileFromAbility(
	family ActionFamily,
	definition sim.AbilityDefinition,
	movementSpeed float32,
	firstAggroDelay time.Duration,
) (ActionProfile, error) {
	if family == ActionUnknown || definition.Name == "" ||
		definition.AnimationName == "" || definition.Range <= 0 ||
		definition.HitDelay < 0 ||
		definition.ReleaseDelay < definition.HitDelay ||
		definition.Cooldown <= 0 || movementSpeed <= 0 ||
		firstAggroDelay < 0 || definition.DamageType > 255 ||
		definition.DamageSource > 255 {
		return ActionProfile{}, errors.New("npc ability profile invalid")
	}
	if definition.Kind != sim.AbilityKindMelee &&
		definition.Kind != sim.AbilityKindProjectile {
		return ActionProfile{}, fmt.Errorf(
			"npcAbilityKind: %s",
			definition.Kind,
		)
	}
	projectileShotCount := definition.ShotCount
	projectileShotInterval := definition.FiringRate
	if definition.Kind == sim.AbilityKindProjectile &&
		projectileShotCount == 0 && len(definition.HitDelays) > 1 {
		projectileShotCount = uint32(len(definition.HitDelays))
		projectileShotInterval = definition.HitDelays[1] - definition.HitDelays[0]
		if projectileShotInterval <= 0 {
			return ActionProfile{}, errors.New("npc ability shot cadence invalid")
		}
		for shotIndex := 2; shotIndex < len(definition.HitDelays); shotIndex++ {
			interval := definition.HitDelays[shotIndex] -
				definition.HitDelays[shotIndex-1]
			if interval != projectileShotInterval {
				return ActionProfile{}, fmt.Errorf(
					"npcAbilityShotCadence[%d]: %s", shotIndex, interval,
				)
			}
		}
	}
	if projectileShotCount > 1 && projectileShotInterval <= 0 {
		return ActionProfile{}, errors.New("npc ability shot interval missing")
	}
	profile := ActionProfile{
		Family:                           family,
		AbilityName:                      definition.Name,
		IsFacingSuppressed:               definition.IsFacingSuppressed,
		IsFacingPolicyKnown:              definition.IsFacingPolicyKnown,
		AnimationName:                    definition.AnimationName,
		FirstAggroDelay:                  firstAggroDelay,
		HitDelay:                         definition.HitDelay,
		ReleaseDelay:                     definition.ReleaseDelay,
		Cooldown:                         definition.Cooldown,
		Range:                            definition.Range,
		MovementSpeed:                    movementSpeed,
		MinimumDamage:                    definition.MinimumDamage,
		MaximumDamage:                    definition.MaximumDamage,
		DamageCoefficient:                definition.DamageCoefficient,
		DescriptorMask:                   definition.DescriptorMask,
		DamageType:                       uint8(definition.DamageType),
		DamageSource:                     uint8(definition.DamageSource),
		Radius:                           definition.Radius,
		Angle:                            definition.Angle,
		ProjectileNoun:                   definition.ProjectileNoun,
		TrailEffectName:                  definition.TrailEffectName,
		ImpactEffectName:                 definition.ImpactEffectName,
		MissEffectName:                   definition.MissEffectName,
		ProjectileSpeed:                  definition.Speed,
		ProjectileDistance:               definition.Distance,
		ProjectileShotInterval:           projectileShotInterval,
		ProjectileShotCount:              projectileShotCount,
		IsProjectileTrackingBetweenShots: definition.IsTrackBetweenShots,
		IsDamageProfileKnown: definition.IsDescriptorFound &&
			definition.IsDamageTypeFound && definition.IsDamageSourceFound,
		IsFirstAggroDurationKnown: true,
	}
	if profile.ImpactEffectName == "" {
		profile.ImpactEffectName = definition.HitEffectName
	}
	return profile, nil
}

func ActionProfileForNoun(nounName string) (ActionProfile, bool) {
	operative, isOperative := OperativeProfile(nounName)
	if isOperative {
		return operative, true
	}
	fiendProfile, isFiendFound := NashiraFiendProfile(nounName)
	if isFiendFound {
		return fiendProfile, true
	}
	switch strings.ToLower(nounName) {
	case "shadowboss.noun", "shadowboss_2.noun", "shadowboss_3.noun":
		return NashiraShadowTossProfile(nounName)
	case "cryosboss.noun", "cryosboss_2.noun", "cryosboss_3.noun":
		return CryosBossChainLightningProfile(nounName)
	case "cryosplasmaadd.noun":
		profile := cryosBasicFieryMeleeProfile(
			2500*time.Millisecond, 3*time.Second,
		)
		profile.PassiveEffectName =
			"fire_wall_minion_onfire_effect.ServerEventDef"
		return profile, true
	case "citadelboss.noun", "citadelboss_2.noun", "citadelboss_3.noun":
		return citadelBossTwinLaserProfile(nounName)
	case "scaldronboss.noun", "scaldronboss_stage2.noun",
		"scaldronboss_2.noun", "scaldronboss_3.noun":
		return scaldronBossTwinLaserProfile(nounName)
	case "citadelspecificfour.noun":
		return citadelSpecificFourBoltProfile(
			3*time.Second, 12, 4, 2.5,
		), true
	case "citadelspecificfour_2.noun":
		return citadelSpecificFourBoltProfile(
			2500*time.Millisecond, 16, 6, 3,
		), true
	case "citadelspecificfour_3.noun":
		return citadelSpecificFourBoltProfile(
			2*time.Second, 20, 8, 3,
		), true
	case "citadelspecifictwo.noun":
		return citadelSpecificTwoMeleeTauntProfile(
			3500*time.Millisecond, 4, 8,
		), true
	case "citadelspecifictwo_2.noun":
		return citadelSpecificTwoMeleeTauntProfile(
			2500*time.Millisecond, 5, 10,
		), true
	case "citadelspecifictwo_3.noun":
		return citadelSpecificTwoMeleeTauntProfile(
			1500*time.Millisecond, 6, 12,
		), true
	case "citadelbasicsuicide.noun":
		return citadelBasicSuicideProfile(
			"NomadSnipe_SlowDebuff", 4*time.Second,
		), true
	case "citadelbasicsuicide_2.noun", "citadelbasicsuicide_3.noun":
		return citadelBasicSuicideProfile(
			"StalkerShock", 3*time.Second,
		), true
	case "scaldronbasicmonk.noun":
		return scaldronBasicMonkFirebombProfile(2), true
	case "scaldronbasicmonk_2.noun":
		return scaldronBasicMonkFirebombProfile(3), true
	case "scaldronbasicmonk_3.noun":
		return scaldronBasicMonkFirebombProfile(4), true
	case "scaldronbasiccopter.noun":
		return scaldronBasicCopterPassiveProfile(3, 4), true
	case "scaldronbasiccopter_2.noun":
		return scaldronBasicCopterPassiveProfile(4, 5), true
	case "scaldronbasiccopter_3.noun":
		return scaldronBasicCopterPassiveProfile(5, 6), true
	case "scaldronbasicmaser.noun":
		return scaldronBasicMaserShotProfile(2 * time.Second), true
	case "scaldronbasicmaser_2.noun":
		return scaldronBasicMaserShotProfile(1500 * time.Millisecond), true
	case "scaldronbasicmaser_3.noun":
		return scaldronBasicMaserShotProfile(time.Second), true
	case "scaldronbasicdoppler.noun":
		return scaldronBasicDopplerShotProfile(
			3500*time.Millisecond, 14, 7,
		), true
	case "scaldronbasicdoppler_2.noun":
		return scaldronBasicDopplerShotProfile(
			3*time.Second, 16, 10,
		), true
	case "scaldronbasicdoppler_3.noun":
		return scaldronBasicDopplerShotProfile(
			2500*time.Millisecond, 18, 13,
		), true
	case "scaldronbasicdopplerfake.noun":
		return scaldronBasicDopplerShotProfile(
			3500*time.Millisecond, 14, 7,
		), true
	case "scaldronbasicdopplerfake_2.noun":
		return scaldronBasicDopplerShotProfile(
			3*time.Second, 16, 10,
		), true
	case "scaldronbasicdopplerfake_3.noun":
		return scaldronBasicDopplerShotProfile(
			2500*time.Millisecond, 18, 13,
		), true
	case "scaldronbasicdog.noun":
		return scaldronBasicDogMeleeProfile(2 * time.Second), true
	case "scaldronbasicdog_2.noun":
		return scaldronBasicDogMeleeProfile(1500 * time.Millisecond), true
	case "scaldronbasicdog_3.noun":
		return scaldronBasicDogMeleeProfile(time.Second), true
	case "scaldronbasicnestle.noun":
		return scaldronBasicNestleMeleeProfile(2500 * time.Millisecond), true
	case "scaldronbasicnestle_2.noun":
		return scaldronBasicNestleMeleeProfile(2250 * time.Millisecond), true
	case "scaldronbasicnestle_3.noun":
		return scaldronBasicNestleMeleeProfile(2 * time.Second), true
	case "scaldronbasicmines.noun", "scaldronbasicmines_2.noun",
		"scaldronbasicmines_3.noun":
		return scaldronBasicMinesMeleeProfile(), true
	case "scaldronbasicthorno.noun":
		return scaldronBasicThornoNovaProfile(
			4, 5, 8, 3, 6, 0.2, 0.2,
		), true
	case "scaldronbasicthorno_2.noun":
		return scaldronBasicThornoNovaProfile(
			6, 7, 12, 3, 5, 0.333, 0.5,
		), true
	case "scaldronbasicthorno_3.noun":
		return scaldronBasicThornoNovaProfile(
			8, 9, 16, 2, 4, 0.5, 1,
		), true
	case "scaldronbasicblink.noun":
		return scaldronBasicBlinkAttackProfile(
			8*time.Second, 15, 133*time.Millisecond, 266*time.Millisecond,
		), true
	case "scaldronbasicblink_2.noun":
		return scaldronBasicBlinkAttackProfile(
			7*time.Second, 18, 266*time.Millisecond, 532*time.Millisecond,
		), true
	case "scaldronbasicblink_3.noun":
		return scaldronBasicBlinkAttackProfile(
			6*time.Second, 21, 400*time.Millisecond, 800*time.Millisecond,
		), true
	case "scaldronbasicsinkhole.noun":
		return scaldronBasicSinkholeProjectileProfile(
			2500*time.Millisecond, 14, 6,
		), true
	case "scaldronbasicsinkhole_2.noun":
		return scaldronBasicSinkholeProjectileProfile(
			2250*time.Millisecond, 16, 9,
		), true
	case "scaldronbasicsinkhole_3.noun":
		return scaldronBasicSinkholeProjectileProfile(
			2*time.Second, 18, 12,
		), true
	case "citadelspecialthree.noun":
		return citadelSpecialThreeLaserZoneProfile(6*time.Second, 250, 3), true
	case "citadelspecialthree_captain.noun":
		return mgpLaserZoneProfile(), true
	case "citadelspecialthree_2.noun", "citadelspecialthree_captain_2.noun":
		return citadelSpecialThreeLaserZoneProfile(4*time.Second, 500, 4), true
	case "citadelspecialthree_3.noun", "citadelspecialthree_captain_3.noun":
		return citadelSpecialThreeLaserZoneProfile(2*time.Second, 750, 4), true
	case "citadelspecialtwo.noun", "citadelspecialtwo_captain.noun":
		return citadelSpecialTwoHomingStunProfile(12*time.Second, 5, 3.5), true
	case "citadelspecialtwo_2.noun", "citadelspecialtwo_captain_2.noun":
		return citadelSpecialTwoHomingStunProfile(10*time.Second, 7.5, 5), true
	case "citadelspecialtwo_3.noun", "citadelspecialtwo_captain_3.noun":
		return citadelSpecialTwoHomingStunProfile(8*time.Second, 10, 5), true
	case "citadelspecificone.noun":
		return citadelSpecificOneDischargeProfile(
			3*time.Second, 12, 39, 41, 6, 4.5,
		), true
	case "citadelspecificone_2.noun":
		return citadelSpecificOneDischargeProfile(
			2500*time.Millisecond, 15, 40, 42, 9, 6,
		), true
	case "citadelspecificone_3.noun":
		return citadelSpecificOneDischargeProfile(
			2*time.Second, 18, 41, 43, 12, 7.5,
		), true
	case "nomadscope.noun", "nomadscope_captain.noun":
		return nomadScopeBurningDebuffProfile(4 * time.Second), true
	case "nomadscope_2.noun", "nomadscope_captain_2.noun":
		return nomadScopeBurningDebuffProfile(3 * time.Second), true
	case "nomadscope_3.noun", "nomadscope_captain_3.noun":
		return nomadScopeBurningDebuffProfile(2 * time.Second), true
	case "citadelspecialfour.noun", "citadelspecialfour_2.noun",
		"citadelspecialfour_3.noun", "citadelspecialfour_captain.noun",
		"citadelspecialfour_captain_2.noun", "citadelspecialfour_captain_3.noun":
		return CitadelSpecialFourGroundSlamProfile(nounName)
	case "zelemboss.noun":
		return zelemBossPushProfile(10), true
	case "zelemboss_2.noun":
		return zelemBossPushProfile(12), true
	case "zelemboss_3.noun":
		return zelemBossPushProfile(15), true
	case "verdanthbasichealer.noun", "verdanthbasichealer_captain.noun":
		return verdanthBasicHealerThornDartProfile(
			4*time.Second, 10, 11, 4, 133333*time.Microsecond, false, 5.5, 3,
		), true
	case "verdanthbasichealer_2.noun", "verdanthbasichealer_captain_2.noun":
		return verdanthBasicHealerThornDartProfile(
			3500*time.Millisecond, 12, 13, 5, 100*time.Millisecond, true, 8, 4,
		), true
	case "verdanthbasichealer_3.noun", "verdanthbasichealer_captain_3.noun":
		return verdanthBasicHealerThornDartProfile(
			3*time.Second, 14, 15, 7, 66667*time.Microsecond, true, 10.5, 5,
		), true
	case "noctbasicmeleedog.noun", "noctbasicmeleedog_captain.noun":
		return noctBasicMeleeDogProfile(1500 * time.Millisecond), true
	case "noctbasicmeleedog_2.noun", "noctbasicmeleedog_captain_2.noun":
		return noctBasicMeleeDogProfile(1250 * time.Millisecond), true
	case "noctbasicmeleedog_3.noun", "noctbasicmeleedog_captain_3.noun":
		return noctBasicMeleeDogProfile(time.Second), true
	case "verdanthbasicrootmob.noun", "verdanthbasicrootmob_captain.noun":
		return verdanthBasicRootmobMeleeProfile(2500*time.Millisecond, 25), true
	case "verdanthbasicrootmob_2.noun", "verdanthbasicrootmob_captain_2.noun":
		return verdanthBasicRootmobMeleeProfile(2*time.Second, 50), true
	case "verdanthbasicrootmob_3.noun", "verdanthbasicrootmob_captain_3.noun":
		return verdanthBasicRootmobMeleeProfile(1500*time.Millisecond, 100), true
	case "verdanthbasicmelee.noun", "verdanthbasicmelee_2.noun",
		"verdanthbasicmelee_3.noun":
		return verdanthBasicMeleeProfile(), true
	case "verdanthspecialtwo.noun", "verdanthspecialtwo_captain.noun":
		return verdanthSpecialTwoEntangleProfile(12, 3*time.Second, 6, 4), true
	case "verdanthspecialtwo_2.noun", "verdanthspecialtwo_captain_2.noun":
		return verdanthSpecialTwoEntangleProfile(14, 4500*time.Millisecond, 9, 5), true
	case "verdanthspecialtwo_3.noun", "verdanthspecialtwo_captain_3.noun":
		return verdanthSpecialTwoEntangleProfile(16, 6*time.Second, 12, 6), true
	case "verdanthspecialone.noun", "verdanthspecialone_captain.noun":
		return stagnantNovaAboveProfile(4 * time.Second), true
	case "verdanthspecialone_2.noun", "verdanthspecialone_captain_2.noun":
		return stagnantNovaAboveProfile(3 * time.Second), true
	case "verdanthspecialone_3.noun", "verdanthspecialone_captain_3.noun":
		return stagnantNovaAboveProfile(2 * time.Second), true
	case "verdanthspecialthree.noun", "verdanthspecialthree_captain.noun",
		"verdanthspecialthree_2.noun", "verdanthspecialthree_captain_2.noun",
		"verdanthspecialthree_3.noun", "verdanthspecialthree_captain_3.noun":
		return VerdanthSpecialThreePullProfile(nounName)
	case "cryosbasicpoison.noun", "cryosbasicpoison_captain.noun":
		return cryosPoisonMeleeProfile(10, 12*time.Second, 6, 4.5), true
	case "cryosbasicpoison_2.noun", "cryosbasicpoison_captain_2.noun":
		return cryosPoisonMeleeProfile(30, 16*time.Second, 9, 6), true
	case "cryosbasicpoison_3.noun", "cryosbasicpoison_captain_3.noun":
		return cryosPoisonMeleeProfile(50, 20*time.Second, 12, 7.5), true
	case "cryosbasicfiery.noun":
		return cryosBasicFieryMeleeProfile(2500*time.Millisecond, 3*time.Second), true
	case "cryosbasicfiery_2.noun":
		return cryosBasicFieryMeleeProfile(2*time.Second, 6*time.Second), true
	case "cryosbasicfiery_3.noun":
		return cryosBasicFieryMeleeProfile(1500*time.Millisecond, 9*time.Second), true
	case "cryosbasicranged.noun":
		return cryosBasicRangedProfile(3*time.Second, 14, 10, 10, 4.5, 3), true
	case "cryosbasicranged_2.noun":
		return cryosBasicRangedProfile(2500*time.Millisecond, 16, 16, 15, 6.5, 4), true
	case "cryosbasicranged_3.noun":
		return cryosBasicRangedProfile(2*time.Second, 18, 24, 20, 8.5, 5), true
	case "cryosbasiclightningranged.noun":
		return cryosBasicLightningRangedProfile(5*time.Second, 15, 8, 4.5, 3), true
	case "cryosbasiclightningranged_2.noun":
		return cryosBasicLightningRangedProfile(4*time.Second, 18, 7, 6.5, 3.5), true
	case "cryosbasiclightningranged_3.noun":
		return cryosBasicLightningRangedProfile(3*time.Second, 21, 6, 8.5, 3.5), true
	case "cryosbasicfirewave.noun":
		return cryosBasicFireWaveProfile(
			4*time.Second, 8, 8, 10, 5.5, 4, false,
		), true
	case "cryosbasicfirewave_2.noun":
		return cryosBasicFireWaveProfile(
			3500*time.Millisecond, 10, 12, 12, 8, 5.25, true,
		), true
	case "cryosbasicfirewave_3.noun":
		return cryosBasicFireWaveProfile(
			3*time.Second, 12, 16, 14, 10.5, 6.5, true,
		), true
	case "cryosbasicmelee.noun":
		return cryosBasicLightningMeleeProfile(1500*time.Millisecond, 7.5, 6, false), true
	case "cryosbasicmelee_2.noun":
		return cryosBasicLightningMeleeProfile(1250*time.Millisecond, 7.5, 6, false), true
	case "cryosbasicmelee_3.noun":
		return cryosBasicLightningMeleeProfile(time.Second, 7.5, 6, false), true
	case "cryosbasiclightningmelee.noun":
		return cryosBasicLightningMeleeProfile(1500*time.Millisecond, 7, 5.5, true), true
	case "cryosbasiclightningmelee_2.noun":
		return cryosBasicLightningMeleeProfile(1250*time.Millisecond, 10.5, 6.5, true), true
	case "cryosbasiclightningmelee_3.noun":
		return cryosBasicLightningMeleeProfile(time.Second, 14, 7.5, true), true
	case "cryosspecialone.noun", "cryosspecialone_captain.noun":
		return cryosSpecialOneBurstShotProfile(13, 25, false, 5.5, 4), true
	case "cryosspecialone_2.noun", "cryosspecialone_captain_2.noun":
		return cryosSpecialOneBurstShotProfile(15, 30, true, 8, 5), true
	case "cryosspecialone_3.noun", "cryosspecialone_captain_3.noun":
		return cryosSpecialOneBurstShotProfile(17, 35, true, 10.5, 6), true
	case "cryosspecialtwo.noun", "cryosspecialtwo_captain.noun":
		return cryosSpecialTwoSpreadShotProfile(
			4*time.Second, 10, 150*time.Millisecond,
			[]float32{25, 0, -25}, 6*time.Second, 5, 3.5,
		), true
	case "cryosspecialtwo_2.noun", "cryosspecialtwo_captain_2.noun":
		return cryosSpecialTwoSpreadShotProfile(
			3500*time.Millisecond, 16, 100*time.Millisecond,
			[]float32{30, 5, -5, -30}, 8*time.Second, 7.5, 4.5,
		), true
	case "cryosspecialtwo_3.noun", "cryosspecialtwo_captain_3.noun":
		return cryosSpecialTwoSpreadShotProfile(
			3*time.Second, 22, 60*time.Millisecond,
			[]float32{40, 20, 5, 0, -5, -20, -40}, 10*time.Second, 10, 5.5,
		), true
	case "verdanthbasicdiseased.noun":
		return verdanthBasicDiseasedPoisonCloudProfile(8, 6, 20), true
	case "verdanthbasicdiseased_2.noun":
		return verdanthBasicDiseasedPoisonCloudProfile(12, 8, 50), true
	case "verdanthbasicdiseased_3.noun":
		return verdanthBasicDiseasedPoisonCloudProfile(16, 10, 100), true
	case "cryoselementalspecialthree.noun", "cryoselementalspecialthree_captain.noun":
		return cryosElementalSpecialThreeProfile(4*time.Second, 12, 40, 5.5, 4), true
	case "cryoselementalspecialthree_2.noun", "cryoselementalspecialthree_captain_2.noun":
		return cryosElementalSpecialThreeProfile(3500*time.Millisecond, 15, 55, 8, 5), true
	case "cryoselementalspecialthree_3.noun", "cryoselementalspecialthree_captain_3.noun":
		return cryosElementalSpecialThreeProfile(3*time.Second, 18, 70, 10.5, 6), true
	case "zelemspecialthree.noun", "zelemspecialthree_captain.noun":
		return zelemSpecialThreeProjectileProfile(5*time.Second, 14, 35, 5.5, 4), true
	case "zelemspecialthree_2.noun", "zelemspecialthree_captain_2.noun":
		return zelemSpecialThreeProjectileProfile(4*time.Second, 16, 45, 8, 4.5), true
	case "zelemspecialthree_3.noun", "zelemspecialthree_captain_3.noun":
		return zelemSpecialThreeProjectileProfile(3*time.Second, 18, 55, 10.5, 5), true
	case "citadelbasicshield.noun", "citadelbasicshield_captain.noun":
		return citadelBasicShieldMeleeProfile(3*time.Second, 10, 12*time.Second), true
	case "citadelbasicshield_2.noun", "citadelbasicshield_captain_2.noun":
		return citadelBasicShieldMeleeProfile(2500*time.Millisecond, 20, 10*time.Second), true
	case "citadelbasicshield_3.noun", "citadelbasicshield_captain_3.noun":
		return citadelBasicShieldMeleeProfile(2*time.Second, 30, 8*time.Second), true
	case "nocturnaspecialmunch.noun", "nocturnaspecialmunch_captain.noun":
		return nocturnaSpecialMunchMeleeProfile(2800*time.Millisecond, 6, 4.5), true
	case "nocturnaspecialmunch_2.noun", "nocturnaspecialmunch_captain_2.noun":
		return nocturnaSpecialMunchMeleeProfile(2300*time.Millisecond, 9, 5.5), true
	case "nocturnaspecialmunch_3.noun", "nocturnaspecialmunch_captain_3.noun":
		return nocturnaSpecialMunchMeleeProfile(1800*time.Millisecond, 12, 6.5), true
	case "nomadbiospecialtwo.noun", "nomadbiospecialtwo_captain.noun":
		return nomadBioSpecialTwoSwipeProfile(5*time.Second, 7, 5), true
	case "nomadbiospecialtwo_2.noun", "nomadbiospecialtwo_captain_2.noun":
		return nomadBioSpecialTwoSwipeProfile(4*time.Second, 10.5, 6), true
	case "nomadbiospecialtwo_3.noun", "nomadbiospecialtwo_captain_3.noun":
		return nomadBioSpecialTwoSwipeProfile(3*time.Second, 14, 7), true
	case "nct_lieu_su_stealther.noun", "nct_lieu_su_stealther_captain.noun":
		return stealtherMeleeProfile(2*time.Second, 3, 0.5, 6, 4), true
	case "nct_lieu_su_stealther_2.noun", "nct_lieu_su_stealther_captain_2.noun":
		return stealtherMeleeProfile(1750*time.Millisecond, 4, 0.75, 9, 5), true
	case "nct_lieu_su_stealther_3.noun", "nct_lieu_su_stealther_captain_3.noun":
		return stealtherMeleeProfile(1500*time.Millisecond, 5, 1, 12, 6), true
	case "noctbasichopper.noun":
		return noctHopperJumpProfile(4, 3), true
	case "noctbasichopper_2.noun":
		return noctHopperJumpProfile(6, 4), true
	case "noctbasichopper_3.noun":
		return noctHopperJumpProfile(8, 5), true
	case "sloth.noun":
		return slothTailZapProfile(3.5, 2), true
	case "sloth_2.noun":
		return slothTailZapProfile(5, 3), true
	case "sloth_3.noun":
		return slothTailZapProfile(6.5, 4), true
	case "nocturnabasicrangedsilence.noun":
		return nocturnaBasicRangedSilenceProfile(6*time.Second, 12.5), true
	case "nocturnabasicrangedsilence_2.noun":
		return nocturnaBasicRangedSilenceProfile(5*time.Second, 15), true
	case "nocturnabasicrangedsilence_3.noun":
		return nocturnaBasicRangedSilenceProfile(4*time.Second, 17.5), true
	case "citadelbasicmelee.noun":
		return citadelFireBreathProfile(24, 36, 10), true
	case "citadelbasicmelee_2.noun":
		return citadelFireBreathProfile(28, 42, 12), true
	case "citadelbasicmelee_3.noun":
		return citadelFireBreathProfile(32, 48, 14), true
	case "citadelbasicgunner.noun":
		return citadelBasicGunnerShotProfile(8, false, 0, 5, 3.5, 250), true
	case "citadelbasicgunner_2.noun":
		return citadelBasicGunnerShotProfile(10, true, 0, 7.5, 4.5, 500), true
	case "citadelbasicgunner_3.noun":
		return citadelBasicGunnerShotProfile(12, true, 10, 10, 5.5, 750), true
	case "citadelbasicranged.noun":
		return citadelBasicRangedBoltProfile(
			"ctd_minn_tc_1_chargeup_d1", 2*time.Second, 5*time.Second, 12, 24, 0, 4, 2.5,
		), true
	case "citadelbasicranged_2.noun":
		return citadelBasicRangedBoltProfile(
			"ctd_minn_tc_1_chargeup_d2", 1500*time.Millisecond, 4*time.Second, 14, 36, 15, 6, 3.5,
		), true
	case "citadelbasicranged_3.noun":
		return citadelBasicRangedBoltProfile(
			"ctd_minn_tc_1_chargeup_d3", time.Second, 3*time.Second, 16, 48, 15, 8, 4.5,
		), true
	case "nomadruption.noun", "nomadruption_captain.noun":
		return nomadRuptionMeleeProfile(5, 3.5), true
	case "nomadruption_2.noun", "nomadruption_captain_2.noun":
		return nomadRuptionMeleeProfile(7.5, 3.5), true
	case "nomadruption_3.noun", "nomadruption_captain_3.noun":
		return nomadRuptionMeleeProfile(10, 3.5), true
	case "nomaddrag.noun", "nomaddrag_2.noun", "nomaddrag_3.noun":
		return NomadDragMeteorProfile(nounName)
	case "nomadshielder.noun", "nomadshielder_captain.noun":
		return nomadShielderBashProfile(2.5, 3, 3, 6, 12, 6, 6, 5), true
	case "nomadshielder_2.noun", "nomadshielder_captain_2.noun":
		return nomadShielderBashProfile(3, 3.5, 4, 8, 16, 8, 9, 5), true
	case "nomadshielder_3.noun", "nomadshielder_captain_3.noun":
		return nomadShielderBashProfile(3.5, 4, 6, 12, 20, 10, 12, 5), true
	case "nocturnaspecialhomer.noun":
		return nocturnaSpecialHomerProfile(16*time.Second, 12.5, 6, 1500*time.Millisecond), true
	case "nocturnaspecialhomer_2.noun":
		return nocturnaSpecialHomerProfile(13*time.Second, 15, 9, 2*time.Second), true
	case "nocturnaspecialhomer_3.noun":
		return nocturnaSpecialHomerProfile(10*time.Second, 17.5, 12, 2500*time.Millisecond), true
	case "boomer.noun", "boomer_captain.noun":
		return boomerChargeProfile(20), true
	case "boomer_2.noun", "boomer_captain_2.noun":
		return boomerChargeProfile(25), true
	case "boomer_3.noun", "boomer_captain_3.noun":
		return boomerChargeProfile(30), true
	case "nomadspecialone.noun", "nomadspecialone_captain.noun":
		return nomadSpecialOneChargeProfile(6*time.Second, 20, 4, 2.5), true
	case "nomadspecialone_2.noun", "nomadspecialone_captain_2.noun":
		return nomadSpecialOneChargeProfile(5*time.Second, 25, 6, 3), true
	case "nomadspecialone_3.noun", "nomadspecialone_captain_3.noun":
		return nomadSpecialOneChargeProfile(4*time.Second, 30, 8, 3.5), true
	case "nocturnaspecialdrift.noun", "nocturnaspecialdrift_captain.noun":
		return nocturnaSpecialDriftChargeProfile(
			7*time.Second, 14, 500*time.Millisecond, 5, 3.5,
		), true
	case "nocturnaspecialdrift_2.noun", "nocturnaspecialdrift_captain_2.noun":
		return nocturnaSpecialDriftChargeProfile(
			6*time.Second, 16, time.Second, 7.5, 4.5,
		), true
	case "nocturnaspecialdrift_3.noun", "nocturnaspecialdrift_captain_3.noun":
		return nocturnaSpecialDriftChargeProfile(
			5*time.Second, 18, 1500*time.Millisecond, 10, 5.5,
		), true
	case "cryosspecialthree.noun", "cryosspecialthree_captain.noun":
		return cryosSpecialThreeSleepMushroomProfile(
			14*time.Second, 14, 2*time.Second, 3.5, 2,
		), true
	case "cryosspecialthree_2.noun", "cryosspecialthree_captain_2.noun":
		return cryosSpecialThreeSleepMushroomProfile(
			12*time.Second, 16, 3*time.Second, 5, 3,
		), true
	case "cryosspecialthree_3.noun", "cryosspecialthree_captain_3.noun":
		return cryosSpecialThreeSleepMushroomProfile(
			10*time.Second, 18, 4*time.Second, 6.5, 4,
		), true
	case "rezzer.noun":
		return rezzerResurrectionProfile(4.5, 3.5), true
	case "rezzer_captain.noun":
		return weaverResurrectionProfile(), true
	case "rezzer_2.noun", "rezzer_captain_2.noun":
		return rezzerResurrectionProfile(6.5, 4.5), true
	case "rezzer_3.noun", "rezzer_captain_3.noun":
		return rezzerResurrectionProfile(8.5, 5.5), true
	case "shooter.noun":
		return shooterPoisonSpitProfile(
			3500*time.Millisecond, 14, 6, 5.5, 4, false,
		), true
	case "shooter_2.noun":
		return shooterPoisonSpitProfile(
			3*time.Second, 16, 10, 8, 4.5, true,
		), true
	case "shooter_3.noun":
		return shooterPoisonSpitProfile(
			2500*time.Millisecond, 18, 20, 10.5, 5.5, true,
		), true
	case "nocturnabasichealthdrain.noun":
		return nocturnaBasicHealthDrainProfile(2, 3, 0.5, 5.5, 4), true
	case "nocturnabasichealthdrain_2.noun":
		return nocturnaBasicHealthDrainProfile(3, 4, 0.75, 8, 5), true
	case "nocturnabasichealthdrain_3.noun":
		return nocturnaBasicHealthDrainProfile(4, 4, 1, 10.5, 6), true
	case "nocturnabasicstealth.noun":
		return nocturnaBasicStealthMeleeProfile(2*time.Second, 7, 5.5), true
	case "nocturnabasicstealth_2.noun":
		return nocturnaBasicStealthMeleeProfile(1750*time.Millisecond, 10.5, 6.5), true
	case "nocturnabasicstealth_3.noun":
		return nocturnaBasicStealthMeleeProfile(1500*time.Millisecond, 14, 7.5), true
	case "noctbasicflyer.noun":
		return noctFlyerLobProfile(6, 4.5), true
	case "noctbasicflyer_2.noun":
		return noctFlyerLobProfile(9, 5.5), true
	case "noctbasicflyer_3.noun":
		return noctFlyerLobProfile(12, 6.5), true
	case "verdanthbasicranged.noun":
		return verdanthBasicRangedProfile(5*time.Second, 8), true
	case "verdanthbasicranged_2.noun":
		return verdanthBasicRangedProfile(4*time.Second, 10), true
	case "verdanthbasicranged_3.noun":
		return verdanthBasicRangedProfile(3*time.Second, 12), true
	case "verdanthbasicooze.noun":
		return verdanthBasicOozeMeleeProfile(4 * time.Second), true
	case "verdanthbasicooze_2.noun":
		return verdanthBasicOozeMeleeProfile(3 * time.Second), true
	case "verdanthbasicooze_3.noun":
		return verdanthBasicOozeMeleeProfile(2 * time.Second), true
	case "zelembasicpackfly.noun":
		return zelemBasicPackflyAttackProfile(3 * time.Second), true
	case "zelembasicpackfly_2.noun":
		return zelemBasicPackflyAttackProfile(2500 * time.Millisecond), true
	case "zelembasicpackfly_3.noun":
		return zelemBasicPackflyAttackProfile(2 * time.Second), true
	case "nct_minn_su_drainer.noun", "nct_minn_su_drainer_2.noun",
		"nct_minn_su_drainer_3.noun":
		return noctMinionManaDrainProfile(nounName)
	case "citadelspecificthree.noun", "citadelspecificthree_2.noun",
		"citadelspecificthree_3.noun", "citadelspecificthree_captain.noun",
		"citadelspecificthree_captain_2.noun",
		"citadelspecificthree_captain_3.noun":
		return citadelGrenadeRollProfile(), true
	case "zelembasicflyingmelee.noun", "zelembasicflyingmelee_captain.noun":
		return zelemBasicFlyingMeleeProfile(10, 8), true
	case "zelembasicflyingmelee_2.noun", "zelembasicflyingmelee_captain_2.noun":
		return zelemBasicFlyingMeleeProfile(14, 3), true
	case "zelembasicflyingmelee_3.noun", "zelembasicflyingmelee_captain_3.noun":
		return zelemBasicFlyingMeleeProfile(18, 3), true
	case "zelembasicpackmelee.noun":
		return zelemBasicPackMeleeProfile(2*time.Second, 6.5, 4), true
	case "zelembasicpackmelee_2.noun":
		return zelemBasicPackMeleeProfile(1500*time.Millisecond, 9.5, 5), true
	case "zelembasicpackmelee_3.noun":
		return zelemBasicPackMeleeProfile(time.Second, 12.5, 6), true
	case "verdanthbasicskeet.noun":
		return verdanthBasicSkeetDartingProfile(4*time.Second, 7, 5), true
	case "verdanthbasicskeet_2.noun":
		return verdanthBasicSkeetDartingProfile(3*time.Second, 10.5, 6.5), true
	case "verdanthbasicskeet_3.noun":
		return verdanthBasicSkeetDartingProfile(2*time.Second, 14, 8), true
	case "cryosbasiccharge.noun":
		return cryosBasicChargeProfile(10*time.Second, 16, 4.5, 3), true
	case "cryosbasiccharge_2.noun":
		return cryosBasicChargeProfile(8*time.Second, 20, 6.5, 4), true
	case "cryosbasiccharge_3.noun":
		return cryosBasicChargeProfile(6*time.Second, 24, 8.5, 5), true
	case "noctbasicghostcharger.noun":
		return noctGhostChargeProfile(5*time.Second, 15, 0.4, 6.5, 5), true
	case "noctbasicghostcharger_2.noun":
		return noctGhostChargeProfile(4*time.Second, 17.5, 0.6, 9.5, 6.5), true
	case "noctbasicghostcharger_3.noun":
		return noctGhostChargeProfile(3*time.Second, 20, 0.8, 12.5, 8), true
	case "verdanthbasicpicky.noun":
		return verdanthBasicPickyChargeProfile(12 * time.Second), true
	case "verdanthbasicpicky_2.noun":
		return verdanthBasicPickyChargeProfile(9 * time.Second), true
	case "verdanthbasicpicky_3.noun":
		return verdanthBasicPickyChargeProfile(6 * time.Second), true
	case "zelembasicchargeup.noun", "zelembasicchargeup_2.noun",
		"zelembasicchargeup_3.noun", "zelembasicchargeup_captain.noun",
		"zelembasicchargeup_captain_2.noun",
		"zelembasicchargeup_captain_3.noun":
		return zelemChargeupStandardProfile(), true
	case "nocturnaspecialleech.noun", "nocturnaspecialleech_captain.noun":
		return nocturnaSpecialLeechProfile(8*time.Second, 12.5, 10, 7, 5.5), true
	case "nocturnaspecialleech_2.noun", "nocturnaspecialleech_captain_2.noun":
		return nocturnaSpecialLeechProfile(7*time.Second, 15, 12.5, 10.5, 6.5), true
	case "nocturnaspecialleech_3.noun", "nocturnaspecialleech_captain_3.noun":
		return nocturnaSpecialLeechProfile(6*time.Second, 17.5, 15, 14, 7.5), true
	case "zelembasicrangedhoming.noun":
		return zelemBasicRangedHomingProfile(6, 4), true
	case "zelembasicrangedhoming_2.noun":
		return zelemBasicRangedHomingProfile(9, 4.5), true
	case "zelembasicrangedhoming_3.noun":
		return zelemBasicRangedHomingProfile(12, 5), true
	case "zelemspecialone.noun", "zelemspecialone_2.noun", "zelemspecialone_3.noun",
		"zelemspecialone_captain.noun", "zelemspecialone_captain_2.noun",
		"zelemspecialone_captain_3.noun":
		return ActionProfile{
			Family: ActionProjectile, AbilityName: "ZelemSP1_TeleportGun",
			AnimationName: "zlm_lieu_sp_1_attack1",
			HitDelay:      1350 * time.Millisecond, ReleaseDelay: 1700 * time.Millisecond,
			Cooldown: 3500 * time.Millisecond, Range: 18, MovementSpeed: 5,
			MinimumDamage: 10, MaximumDamage: 16, DamageCoefficient: 0.05,
			ModifierName: "RandomTeleport", ModifierDuration: 200 * time.Millisecond,
			ProjectileNoun:   "Ability_Fireball.Noun",
			TrailEffectName:  "spacetime_lieu_shot_projectile.ServerEventDef",
			ImpactEffectName: "warp_impact_effect.ServerEventDef",
			MissEffectName:   "ineffective_common_small.ServerEventDef",
			ProjectileSpeed:  6, ProjectileDistance: 50,
			TeleportMinimumDistance: 2, TeleportNormalDistance: 8,
			TeleportMaximumDistance: 10,
			TeleportReactionName:    "react_teleported",
			TeleportEffectName:      "warp_impact_exit_effect.ServerEventDef",
		}, true
	case "zelemspecialtwo.noun", "zelemspecialtwo_2.noun", "zelemspecialtwo_3.noun",
		"zelemspecialtwo_captain.noun", "zelemspecialtwo_captain_2.noun",
		"zelemspecialtwo_captain_3.noun":
		return ActionProfile{
			Family: ActionPushPull, AbilityName: "ZelemSpecialTwo",
			FirstAggroAnimationName: "gen_aggro_sp_beam_in",
			FirstAggroDelay:         1230 * time.Millisecond,
			Range:                   10, MovementSpeed: 4.5,
			NonCombatMovementSpeed:    3,
			IsFirstAggroDurationKnown: true,
		}, true
	case "zelembasicranged.noun":
		return ActionProfile{
			Family: ActionZelemRanged, AbilityName: "ZelemBasicRanged_Blink",
			AnimationName: "zlm_minn_sp_1_blink_cast3",
			HitDelay:      3200 * time.Millisecond, Cooldown: 7 * time.Second, Range: 50,
			MovementSpeed:           5,
			TeleportMinimumDistance: 2, TeleportNormalDistance: 8,
			TeleportMaximumDistance: 10, TeleportAnimationName: "zlm_minn_sp_1_warp_in",
			TeleportAnimationDelay:    1066667 * time.Microsecond,
			IsFirstAggroDurationKnown: true,
		}, true
	case "zelembasichybrid.noun":
		return zelemBasicHybridProjectileProfile(2500*time.Millisecond, 15, 4), true
	case "zelembasichybrid_2.noun":
		return zelemBasicHybridProjectileProfile(2*time.Second, 18, 6), true
	case "zelembasichybrid_3.noun":
		return zelemBasicHybridProjectileProfile(1500*time.Millisecond, 21, 8), true
	case "zelembasicrepair.noun":
		return zelemBasicRepairArcWeldingProfile(5, 3), true
	case "zelembasicrepair_2.noun":
		return zelemBasicRepairArcWeldingProfile(7.5, 4), true
	case "zelembasicrepair_3.noun":
		return zelemBasicRepairArcWeldingProfile(10, 5), true
	case "zelembasicmelee.noun", "zelembasicmelee_2.noun",
		"zelembasicmelee_3.noun", "zelembasicmelee_captain.noun",
		"zelembasicmelee_captain_2.noun", "zelembasicmelee_captain_3.noun":
		return ActionProfile{
			Family: ActionNomadDrone, AbilityName: "ZelemBasicMeleeAttack",
			AnimationName: "zlm_minn_sp_3_attack",
			HitDelay:      260 * time.Millisecond, ReleaseDelay: 600 * time.Millisecond,
			Cooldown: 1500 * time.Millisecond, Range: 0.75, MovementSpeed: 8,
			MinimumDamage: 4, MaximumDamage: 7, IsFirstAggroDurationKnown: true,
		}, true
	case "zelemspecialhaster.noun", "zelemspecialhaster_captain.noun":
		return zelemSpecialHasterProfile(5.5, 4), true
	case "zelemspecialhaster_2.noun", "zelemspecialhaster_captain_2.noun":
		return zelemSpecialHasterProfile(7.5, 5), true
	case "zelemspecialhaster_3.noun", "zelemspecialhaster_captain_3.noun":
		return zelemSpecialHasterProfile(9.5, 6), true
	case "nomadsnipe.noun", "nomadsnipe_captain.noun":
		return nomadSnipeSlowProfile(7, 5.5), true
	case "nomadsnipe_2.noun", "nomadsnipe_captain_2.noun":
		return nomadSnipeSlowProfile(10.5, 5.5), true
	case "nomadsnipe_3.noun", "nomadsnipe_captain_3.noun":
		return nomadSnipeSlowProfile(14, 5.5), true
	case "nomadwithdrone.noun", "nomadwithdrone_captain.noun":
		return nomadWithDroneProfile(6.5, 5), true
	case "nomadwithdrone_2.noun", "nomadwithdrone_captain_2.noun":
		return nomadWithDroneProfile(9.5, 5), true
	case "nomadwithdrone_3.noun", "nomadwithdrone_captain_3.noun":
		return nomadWithDroneProfile(12.5, 5), true
	case "nomadspecialthree.noun", "nomadspecialthree_2.noun", "nomadspecialthree_3.noun":
		return nomadSpecialThreeProjectileProfile(nounName)
	case "verdanthbasicplunge.noun":
		return verdanthBasicPlungeProfile(6, 4), true
	case "verdanthbasicplunge_2.noun":
		return verdanthBasicPlungeProfile(9, 5), true
	case "verdanthbasicplunge_3.noun":
		return verdanthBasicPlungeProfile(12, 6), true
	default:
		return ActionProfile{}, false
	}
}

func scaldronBasicCopterPassiveProfile(
	minimumDamage float32, maximumDamage float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "ScaldronBasicCopter_Passive",
		HitDelay:                  500 * time.Millisecond,
		Cooldown:                  500 * time.Millisecond,
		Range:                     7,
		MinimumRange:              4,
		MovementSpeed:             7,
		NonCombatMovementSpeed:    5.5,
		MinimumDamage:             minimumDamage,
		MaximumDamage:             maximumDamage,
		DamageCoefficient:         0.05,
		DescriptorMask:            0,
		DamageType:                0,
		DamageSource:              0,
		TrailEffectName:           "sca_minn_tc_01_laserWall.ServerEventDef",
		ImpactEffectName:          "sca_minn_tc_01_laserWallHit.ServerEventDef",
		MaximumTargetCount:        3,
		Radius:                    15,
		ProjectileOffset:          game.Vec3{Z: 0.5},
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func scaldronBasicMaserShotProfile(cooldown time.Duration) ActionProfile {
	return ActionProfile{
		Family: ActionCone, AbilityName: "ScaldronBasicMaser_Shot",
		AnimationName:             "sca_minn_tc_02_attack1",
		HitDelay:                  330 * time.Millisecond,
		ReleaseDelay:              500 * time.Millisecond,
		Cooldown:                  cooldown,
		Range:                     2.5,
		Radius:                    4,
		MovementSpeed:             7,
		NonCombatMovementSpeed:    5.5,
		MinimumDamage:             4,
		MaximumDamage:             5,
		DamageCoefficient:         0.05,
		DescriptorMask:            1<<3 | 1<<7,
		DamageType:                0,
		DamageSource:              1,
		TrailEffectName:           "sca_minn_tc_02_laserShot.ServerEventDef",
		ImpactEffectName:          "sca_minn_tc_02_laserShotHit.ServerEventDef",
		ProjectileDistance:        4,
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func ScaldronBasicMaserCleanseProfile(
	nounName string,
) (ActionProfile, bool) {
	cooldown := time.Duration(0)
	switch strings.ToLower(strings.TrimSpace(nounName)) {
	case "scaldronbasicmaser.noun":
		cooldown = 8 * time.Second
	case "scaldronbasicmaser_2.noun":
		cooldown = 6 * time.Second
	case "scaldronbasicmaser_3.noun":
		cooldown = 4 * time.Second
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		AbilityName:   "ScaldronBasicMaser_Cleanse",
		AnimationName: "sca_minn_tc_02_attack2",
		HitDelay:      200 * time.Millisecond, ReleaseDelay: 750 * time.Millisecond,
		Cooldown: cooldown, Radius: 5,
		MovementSpeed: 7, NonCombatMovementSpeed: 5.5,
		TrailEffectName:  "sca_minn_tc_02_cleansingPulse.ServerEventDef",
		ImpactEffectName: "sca_minn_tc_02_cleansingPulseHit.ServerEventDef",
		ProjectileSpeed:  5,
	}, true
}

func scaldronBasicDopplerShotProfile(
	cooldown time.Duration, attackRange float32, projectileSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "ScaldronBasicDoppler_Shot",
		AnimationName: "sca_minn_su_01_attack2",
		HitDelay:      260 * time.Millisecond, ReleaseDelay: 1900 * time.Millisecond,
		Cooldown: cooldown, Range: attackRange,
		MovementSpeed: 7, NonCombatMovementSpeed: 5.5,
		MinimumDamage: 4, MaximumDamage: 8, DamageCoefficient: 0.05,
		DescriptorMask: 1<<7 | 1<<13, DamageType: 4, DamageSource: 1,
		ProjectileNoun:   "Ability_Fireball.Noun",
		TrailEffectName:  "shadow_scarab_shot_effect.ServerEventDef",
		ImpactEffectName: "shadow_bolt_impact.ServerEventDef",
		MissEffectName:   "ineffective_common_small.ServerEventDef",
		ProjectileSpeed:  projectileSpeed, ProjectileDistance: 24,
		IsDamageProfileKnown: true, IsFirstAggroDurationKnown: true,
	}
}

func ScaldronBasicDopplerCloneProfile(
	nounName string,
) (ActionProfile, bool) {
	cooldown := time.Duration(0)
	retainedObjectNoun := ""
	switch strings.ToLower(strings.TrimSpace(nounName)) {
	case "scaldronbasicdoppler.noun":
		cooldown = 11 * time.Second
		retainedObjectNoun = "ScaldronBasicDopplerFake.Noun"
	case "scaldronbasicdoppler_2.noun":
		cooldown = 9 * time.Second
		retainedObjectNoun = "ScaldronBasicDopplerFake_2.Noun"
	case "scaldronbasicdoppler_3.noun":
		cooldown = 7 * time.Second
		retainedObjectNoun = "ScaldronBasicDopplerFake_3.Noun"
	default:
		return ActionProfile{}, false
	}
	return ActionProfile{
		AbilityName:   "ScaldronBasicDoppler_Clone",
		AnimationName: "sca_minn_su_01_split",
		HitDelay:      700 * time.Millisecond, Cooldown: cooldown,
		MinimumRange: 6, Range: 10,
		MovementSpeed: 7, NonCombatMovementSpeed: 5.5,
		RetainedObjectNoun: retainedObjectNoun,
		RetainedEffectName: "shadow_doppler_split_shader_effect.ServerEventDef",
		ImpactEffectName:   "shadow_doppler_split_effect.ServerEventDef",
	}, true
}

func cryosBasicFireWaveProfile(
	cooldown time.Duration, attackRange float32, projectileSpeed float32,
	projectileDistance float32, movementSpeed float32,
	nonCombatMovementSpeed float32, isLeadingTarget bool,
) ActionProfile {
	maximumLeadAngle := float32(0)
	if isLeadingTarget {
		maximumLeadAngle = 20
	}
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "CryosBasicFireWave",
		AnimationName:          "cry_minn_el_x4_shoot",
		HitDelay:               166667 * time.Microsecond,
		ReleaseDelay:           time.Second,
		Cooldown:               cooldown,
		Range:                  attackRange,
		MovementSpeed:          movementSpeed,
		NonCombatMovementSpeed: nonCombatMovementSpeed,
		MinimumDamage:          6, MaximumDamage: 12,
		MinimumDamagePercent:       0.25,
		DamageCoefficient:          0.05,
		DescriptorMask:             8320,
		DamageType:                 3,
		DamageSource:               1,
		ProjectileNoun:             "Ability_WidePiercingProjectile.Noun",
		TrailEffectName:            "cry_minn_el_x4_flamethrower_effect.ServerEventDef",
		ImpactEffectName:           "plasma_common_fire_hit_small_effect.ServerEventDef",
		ProjectileExitEffectName:   "cry_minn_el_x4_passthrough_effect.ServerEventDef",
		MissEffectName:             "ineffective_common_small.ServerEventDef",
		ProjectileSpeed:            projectileSpeed,
		ProjectileDistance:         projectileDistance,
		ProjectileMaximumLeadAngle: maximumLeadAngle,
		IsProjectileLeadingTarget:  isLeadingTarget,
		IsProjectilePiercing:       true,
		IsDamageProfileKnown:       true,
		IsFirstAggroDurationKnown:  true,
	}
}

func shooterPoisonSpitProfile(
	cooldown time.Duration, attackRange float32, projectileSpeed float32,
	movementSpeed float32, nonCombatMovementSpeed float32,
	isLeadingTarget bool,
) ActionProfile {
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "PoisonSpit",
		AnimationName:             "cast_poisonspit",
		FirstAggroAnimationName:   "gen_aggro_su_voidzone",
		FirstAggroEffectName:      "shadow_doppler_void_zone_effect.ServerEventDef",
		FirstAggroDelay:           1060 * time.Millisecond,
		HitDelay:                  170 * time.Millisecond,
		ReleaseDelay:              1900 * time.Millisecond,
		Cooldown:                  cooldown,
		Range:                     attackRange,
		MovementSpeed:             movementSpeed,
		NonCombatMovementSpeed:    nonCombatMovementSpeed,
		MinimumDamage:             4,
		MaximumDamage:             8,
		DamageCoefficient:         0.05,
		DescriptorMask:            8320,
		DamageType:                4,
		DamageSource:              1,
		ProjectileNoun:            "Ability_Fireball.Noun",
		TrailEffectName:           "shadow_bolt_effect.ServerEventDef",
		ImpactEffectName:          "shadow_bolt_impact.ServerEventDef",
		MissEffectName:            "ineffective_common_small.ServerEventDef",
		ProjectileSpeed:           projectileSpeed,
		ProjectileDistance:        24,
		IsProjectileLeadingTarget: isLeadingTarget,
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func citadelBasicSuicideProfile(
	modifierName string, modifierDuration time.Duration,
) ActionProfile {
	return ActionProfile{
		Family: ActionDetonate, AbilityName: "CitadelMinionSuicide",
		AnimationName:             "zlm_minn_tc_2_shutdown",
		PreAggroAnimationName:     "zlm_minn_tc_2_shutdown",
		FirstAggroAbilityName:     "FirstAggro_ActivateRobot",
		FirstAggroAnimationName:   "zlm_minn_tc_2_aggro",
		FirstAggroDelay:           1300 * time.Millisecond,
		HitDelay:                  400 * time.Millisecond,
		ReleaseDelay:              time.Second,
		Cooldown:                  time.Second,
		Range:                     0.75,
		Radius:                    4,
		MovementSpeed:             4,
		NonCombatMovementSpeed:    2.5,
		MinimumDamage:             5,
		MaximumDamage:             8,
		DamageCoefficient:         0.05,
		DescriptorMask:            1<<3 | 1<<7,
		DamageType:                0,
		DamageSource:              1,
		ModifierName:              modifierName,
		ModifierDuration:          modifierDuration,
		ImpactEffectName:          "cyber_trapper_tauntBombExplosion.ServerEventDef",
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func scaldronBasicMonkFirebombProfile(tickCount uint32) ActionProfile {
	return ActionProfile{
		Family: ActionDetonate, AbilityName: "ScaldronBasicMonk_Firebomb",
		AnimationName:                 "sca_minn_el_02_explode",
		HitDelay:                      100 * time.Millisecond,
		ReleaseDelay:                  100 * time.Millisecond,
		Cooldown:                      2 * time.Second,
		Range:                         0.75,
		Radius:                        4,
		MovementSpeed:                 6,
		NonCombatMovementSpeed:        4,
		MinimumDamage:                 1,
		MaximumDamage:                 1,
		ModifierName:                  "ScaldronBasicFirebomb_FirebombDot",
		ModifierDuration:              time.Duration(tickCount) * 2 * time.Second,
		ModifierTickDuration:          2 * time.Second,
		ModifierMinimumTickDamage:     8,
		ModifierMaximumTickDamage:     12,
		ModifierTickDamageCoefficient: 0.05,
		ModifierDescriptorMask:        36,
		ModifierDamageType:            3,
		ModifierDamageSource:          1,
		IsModifierDamageProfileKnown:  true,
		ModifierMaximumStack:          3,
		TargetEffectName:              "ctd_minn_tc_2_countdownFlash.ServerEventDef",
		EmergeDelay:                   1750 * time.Millisecond,
		IsFirstAggroDurationKnown:     true,
	}
}

func scaldronBasicDogMeleeProfile(cooldown time.Duration) ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "ScaldronBasicDog_Attack",
		AnimationName:                  "sca_minn_su_02_attack1",
		HitDelay:                       303030 * time.Microsecond,
		ReleaseDelay:                   1166667 * time.Microsecond,
		Cooldown:                       cooldown,
		Range:                          1,
		Radius:                         1.5,
		MovementSpeed:                  7,
		NonCombatMovementSpeed:         5.5,
		MinimumDamage:                  4,
		MaximumDamage:                  7,
		DamageCoefficient:              0.05,
		DescriptorMask:                 1 | 1<<1 | 1<<6,
		DamageType:                     4,
		DamageSource:                   0,
		ModifierName:                   "ScaldronBasicDog_VulnerabilityModifier",
		ModifierDuration:               5 * time.Second,
		ModifierChance:                 100,
		ModifierPhysicalDamageIncrease: 0.5,
		ImpactEffectName:               "necro_common_hit_small.ServerEventDef",
		IsDamageProfileKnown:           true, IsFirstAggroDurationKnown: true,
	}
}

func scaldronBasicNestleMeleeProfile(cooldown time.Duration) ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "ScaldronBasicNestle_Melee",
		AnimationName:             "sca_minn_lf_02_attack",
		HitDelay:                  330 * time.Millisecond,
		ReleaseDelay:              700 * time.Millisecond,
		Cooldown:                  cooldown,
		Range:                     1.25,
		Radius:                    2.25,
		MovementSpeed:             7,
		NonCombatMovementSpeed:    5.5,
		MinimumDamage:             4,
		MaximumDamage:             8,
		DamageCoefficient:         0.05,
		DescriptorMask:            1 | 1<<1 | 1<<6,
		DamageType:                2,
		DamageSource:              0,
		ImpactEffectName:          "ScaldronBasicNestle_MeleeImpact.ServerEventDef",
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func ScaldronBasicNestleWanderProfile(nounName string) (ActionProfile, bool) {
	switch strings.ToLower(strings.TrimSpace(nounName)) {
	case "scaldronbasicnestle.noun", "scaldronbasicnestle_2.noun",
		"scaldronbasicnestle_3.noun":
		return ActionProfile{
			AbilityName:            "ScaldronBasicNestle_Wander",
			HitDelay:               time.Second,
			ReleaseDelay:           time.Second,
			Cooldown:               8 * time.Second,
			MinimumRange:           3,
			Range:                  5,
			ModifierDuration:       3 * time.Second,
			MovementSpeed:          7,
			NonCombatMovementSpeed: 5.5,
		}, true
	default:
		return ActionProfile{}, false
	}
}

func scaldronBasicMinesMeleeProfile() ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "ScaldronBasicMines_Melee",
		AnimationName:             "sca_minn_el_01_attack",
		HitDelay:                  330 * time.Millisecond,
		ReleaseDelay:              1430 * time.Millisecond,
		Cooldown:                  500 * time.Millisecond,
		Range:                     1.25,
		Radius:                    2.5,
		MovementSpeed:             7,
		NonCombatMovementSpeed:    5.5,
		MinimumDamage:             4,
		MaximumDamage:             8,
		DamageCoefficient:         0.05,
		DescriptorMask:            1 | 1<<1 | 1<<6,
		DamageType:                3,
		DamageSource:              0,
		ImpactEffectName:          "charge_impact_small_effect.ServerEventDef",
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func ScaldronBasicMinesDeathProfile(nounName string) (ActionProfile, bool) {
	switch strings.ToLower(strings.TrimSpace(nounName)) {
	case "scaldronbasicmines.noun":
		return scaldronBasicMinesDeathProfile(4, 2*time.Second), true
	case "scaldronbasicmines_2.noun":
		return scaldronBasicMinesDeathProfile(6, time.Second), true
	case "scaldronbasicmines_3.noun":
		return scaldronBasicMinesDeathProfile(8, time.Second), true
	default:
		return ActionProfile{}, false
	}
}

func scaldronBasicMinesDeathProfile(
	mineCount uint32, warmup time.Duration,
) ActionProfile {
	return ActionProfile{
		Family: ActionRetainedArea, AbilityName: "ScaldronBasicMines_OnDeath",
		AnimationName:            "sca_minn_el_01_death",
		HitDelay:                 warmup,
		ModifierDuration:         5 * time.Second,
		MinimumDamage:            8,
		MaximumDamage:            16,
		DamageCoefficient:        0.05,
		DescriptorMask:           1<<3 | 1<<7,
		DamageType:               3,
		DamageSource:             1,
		ProjectileShotCount:      mineCount,
		ProjectileNoun:           "Ability_Fireball.Noun",
		TrailEffectName:          "ScaldronBasicMines_MineProjectileTrail.ServerEventDef",
		ProjectileExitEffectName: "ScaldronBasicMines_MineProjectileImpact.ServerEventDef",
		ImpactEffectName:         "plasma_common_fire_hit_small_effect.ServerEventDef",
		RetainedObjectNoun:       "Ability_Fireball.Noun",
		RetainedEffectName:       "ScaldronBasicMines_MineExplosion.ServerEventDef",
		MinimumRange:             1,
		Radius:                   16,
		ProjectileHeight:         2,
		IsDamageProfileKnown:     true,
	}
}

func BoomerDeathDetonationProfile(nounName string) (ActionProfile, bool) {
	switch strings.ToLower(strings.TrimSpace(nounName)) {
	case "boomer.noun", "boomer_captain.noun":
		return boomerDeathDetonationProfile(8, 16), true
	case "boomer_2.noun", "boomer_captain_2.noun":
		return boomerDeathDetonationProfile(12, 24), true
	case "boomer_3.noun", "boomer_captain_3.noun":
		return boomerDeathDetonationProfile(16, 32), true
	default:
		return ActionProfile{}, false
	}
}

func boomerDeathDetonationProfile(
	minimumDamage float32, maximumDamage float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionRetainedArea, AbilityName: "DeathDetonate",
		AnimationName:        "cast_explode",
		HitDelay:             1300 * time.Millisecond,
		MinimumDamage:        minimumDamage,
		MaximumDamage:        maximumDamage,
		DamageCoefficient:    0.05,
		Radius:               10,
		DescriptorMask:       1<<3 | 1<<7,
		DamageType:           3,
		DamageSource:         1,
		ImpactEffectName:     "plasma_common_electric_hit_medium_effect.ServerEventDef",
		IsDamageProfileKnown: true,
	}
}

func scaldronBasicThornoNovaProfile(
	attackRange float32, radius float32, maximumTargetCount uint32,
	minimumDamage float32, maximumDamage float32,
	physicalDamageReduction float32, meleeDamageReflection float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionCone, AbilityName: "ScaldronBasicThorno_ThornNova",
		AnimationName:                  "sca_minn_lf_01_attack1",
		HitDelay:                       330 * time.Millisecond,
		ReleaseDelay:                   time.Second,
		Cooldown:                       6 * time.Second,
		Range:                          attackRange,
		Radius:                         radius,
		Angle:                          360,
		MovementSpeed:                  7,
		NonCombatMovementSpeed:         5.5,
		MinimumDamage:                  minimumDamage,
		MaximumDamage:                  maximumDamage,
		DamageCoefficient:              0.05,
		DescriptorMask:                 1 << 6,
		DamageType:                     2,
		DamageSource:                   0,
		MaximumTargetCount:             maximumTargetCount,
		ImpactEffectName:               "life_common_melee_hit.ServerEventDef",
		PassiveEffectName:              "ScaldronBasicThorno_ThornsPassive.ServerEventDef",
		PassivePhysicalDamageReduction: physicalDamageReduction,
		PassiveMeleeDamageReflection:   meleeDamageReflection,
		IsDamageProfileKnown:           true,
		IsFirstAggroDurationKnown:      true,
	}
}

func scaldronBasicBlinkAttackProfile(
	cooldown time.Duration, attackRange float32,
	hitDelay time.Duration, releaseDelay time.Duration,
) ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "ScaldronBasicBlink_AttackBlink",
		AnimationName:             "sca_minn_sp_02_attack1",
		EndAnimationName:          "sca_minn_sp_02_attack1_end",
		HitDelay:                  hitDelay,
		ReleaseDelay:              releaseDelay,
		Cooldown:                  cooldown,
		Range:                     attackRange,
		Radius:                    3,
		MovementSpeed:             7,
		NonCombatMovementSpeed:    5.5,
		MinimumDamage:             1,
		MaximumDamage:             2,
		DamageCoefficient:         0.05,
		DescriptorMask:            1<<1 | 1<<6,
		DamageType:                1,
		DamageSource:              0,
		ImpactEffectName:          "spacetime_bite_effect.ServerEventDef",
		TeleportMinimumDistance:   10,
		TeleportNormalDistance:    11.5,
		TeleportMaximumDistance:   13,
		TeleportAnimationName:     "sca_minn_sp_02_blink_in",
		TeleportAnimationDelay:    250 * time.Millisecond,
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func scaldronBasicSinkholeProjectileProfile(
	cooldown time.Duration, attackRange float32, projectileSpeed float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionProjectile, AbilityName: "ScaldronBasicSinkhole_Projectile",
		AnimationName:             "sca_minn_sp_01_attack1",
		HitDelay:                  466700 * time.Microsecond,
		ReleaseDelay:              1600 * time.Millisecond,
		Cooldown:                  cooldown,
		Range:                     attackRange,
		MovementSpeed:             7,
		NonCombatMovementSpeed:    5.5,
		MinimumDamage:             5,
		MaximumDamage:             8,
		DamageCoefficient:         0.05,
		DescriptorMask:            1<<7 | 1<<13,
		DamageType:                1,
		DamageSource:              1,
		ProjectileNoun:            "Ability_Fireball.Noun",
		TrailEffectName:           "spacetime_sinkhole_shot_effect.ServerEventDef",
		ImpactEffectName:          "forcepulse_impact.ServerEventDef",
		MissEffectName:            "ineffective_common_small.ServerEventDef",
		ProjectileSpeed:           projectileSpeed,
		ProjectileDistance:        30,
		ProjectileOffset:          game.Vec3{X: 0.25, Y: 1.5, Z: 0.2},
		IsDamageProfileKnown:      true,
		IsFirstAggroDurationKnown: true,
	}
}

func ScaldronBasicSinkholeWellProfile(nounName string) (ActionProfile, bool) {
	switch strings.ToLower(strings.TrimSpace(nounName)) {
	case "scaldronbasicsinkhole.noun":
		return scaldronBasicSinkholeWellProfile(15*time.Second, 2, 200, 0.6), true
	case "scaldronbasicsinkhole_2.noun":
		return scaldronBasicSinkholeWellProfile(12*time.Second, 2.5, 250, 0.7), true
	case "scaldronbasicsinkhole_3.noun":
		return scaldronBasicSinkholeWellProfile(9*time.Second, 3, 300, 0.8), true
	default:
		return ActionProfile{}, false
	}
}

func scaldronBasicSinkholeWellProfile(
	cooldown time.Duration, pullSpeed float32, projectileGravity float32,
	damageReduction float32,
) ActionProfile {
	return ActionProfile{
		Family: ActionPushPull, AbilityName: "ScaldronBasicSinkhole_Sinkhole",
		AnimationName:             "sca_minn_sp_01_attack2",
		EndAnimationName:          "sca_minn_sp_01_attack2_end",
		HitDelay:                  time.Second,
		ReleaseDelay:              10 * time.Second,
		Cooldown:                  cooldown,
		Range:                     25,
		Radius:                    10,
		MovementSpeed:             7,
		NonCombatMovementSpeed:    5.5,
		ForcedMovementSpeed:       pullSpeed,
		ForcedMovementDuration:    500 * time.Millisecond,
		ForcedMovementEffectName:  "gravity_orb_wind.ServerEventDef",
		ProjectileGravity:         projectileGravity,
		SelfDamageReduction:       damageReduction,
		IsPull:                    true,
		IsFirstAggroDurationKnown: true,
	}
}

func citadelSpecialThreeLaserZoneProfile(
	cooldown time.Duration, energyDefense float32, maximumTargetCount uint32,
) ActionProfile {
	return ActionProfile{
		Family: ActionCone, AbilityName: "CitadelSpecialThree_LaserZone",
		AnimationName:                  "ctd_lieu_tc_01_attack1",
		PreAggroAnimationName:          "zlm_minn_tc_2_shutdown",
		FirstAggroAbilityName:          "FirstAggro_ActivateRobot",
		FirstAggroAnimationName:        "zlm_minn_tc_2_aggro",
		FirstAggroDelay:                1300 * time.Millisecond,
		HitDelay:                       433333337 * time.Nanosecond,
		ReleaseDelay:                   time.Second,
		Cooldown:                       cooldown,
		Range:                          20,
		Radius:                         50,
		Angle:                          35,
		MovementSpeed:                  5,
		NonCombatMovementSpeed:         3.5,
		MinimumDamage:                  3,
		MaximumDamage:                  6,
		DamageCoefficient:              0.05,
		DescriptorMask:                 136,
		DamageType:                     0,
		DamageSource:                   1,
		MaximumTargetCount:             maximumTargetCount,
		TrailEffectName:                "ctd_lieu_tc_1_beam.ServerEventDef",
		ImpactEffectName:               "ctd_lieu_tc_1_beam_hit.ServerEventDef",
		PassiveEnergyDefense:           energyDefense,
		PassiveDamageOverTimeReduction: 0.75,
		IsDamageProfileKnown:           true,
		IsFirstAggroDurationKnown:      true,
	}
}

func mgpLaserZoneProfile() ActionProfile {
	profile := citadelSpecialThreeLaserZoneProfile(6*time.Second, 250, 4)
	profile.PassiveCreateEffectName = "cyber_shield_generate_shield.ServerEventDef"
	profile.PassiveEffectName = "citadelBasicShield_Shield.ServerEventDef"
	return profile
}

func nomadScopeBurningDebuffProfile(cooldown time.Duration) ActionProfile {
	return ActionProfile{
		Family: ActionMelee, AbilityName: "NomadScope_BurningDebuff",
		AnimationName:                 "nomad_lieu_el_2_burningDOT_cast",
		FirstAggroAnimationName:       "first_aggro_roar",
		FirstAggroDelay:               1750 * time.Millisecond,
		HitDelay:                      600 * time.Millisecond,
		ReleaseDelay:                  1866667 * time.Microsecond,
		Cooldown:                      cooldown,
		Range:                         12,
		MovementSpeed:                 8.5,
		NonCombatMovementSpeed:        8.5,
		MinimumDamage:                 2,
		MaximumDamage:                 4,
		DamageCoefficient:             0.05,
		DescriptorMask:                1 | 1<<7,
		DamageType:                    3,
		DamageSource:                  1,
		ModifierName:                  "OnFire",
		ModifierDuration:              6 * time.Second,
		ModifierTickDuration:          2 * time.Second,
		ModifierMinimumTickDamage:     2,
		ModifierMaximumTickDamage:     4,
		ModifierTickDamageCoefficient: 0.05,
		ModifierDescriptorMask:        36,
		ModifierDamageType:            3,
		ModifierDamageSource:          1,
		IsModifierDamageProfileKnown:  true,
		ModifierMaximumStack:          1,
		ImpactEffectName:              "nomad_lieu_el_2_scope_fire_burningDOT_hit_effect.ServerEventDef",
		IsDamageProfileKnown:          true,
		IsFirstAggroDurationKnown:     true,
	}
}
