package npc

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

var ErrControlTargetOutOfRange = errors.New("npc control target out of range")

// Build 103's MagicNumbers resource stores this signed stat baseline at
// offset 52. sub_9E4E60 subtracts it before applying an ability coefficient.
const nonPlayerPrimaryAttributeBaseline = float32(8)

type AttackPlan struct {
	SourceObjectID   uint32
	TargetObjectID   uint32
	ActionGeneration uint64
	SourcePosition   game.Vec3
	TargetPosition   game.Vec3
	Profile          ActionProfile
	Damage           game.DamageRange
}

type AttackResult struct {
	Damage     float32
	IsCritical bool
}

type AttackTimeline struct {
	HitDelay  time.Duration
	NextDelay time.Duration
}

func TimelineForAttack(plan AttackPlan) (AttackTimeline, error) {
	profile := plan.Profile
	if profile.HitDelay < 0 || profile.ReleaseDelay < profile.HitDelay ||
		profile.Cooldown <= 0 {
		return AttackTimeline{}, errors.New("npc attack timeline invalid")
	}
	return AttackTimeline{
		HitDelay:  profile.HitDelay,
		NextDelay: max(profile.Cooldown, profile.ReleaseDelay),
	}, nil
}

func PlanAttack(
	npc Snapshot, targetObjectID uint32, targetPosition game.Vec3,
	targetFootprintRadius float32,
) (AttackPlan, error) {
	profile, isFound := ActionProfileForPlan(npc.Plan)
	if !isFound || profile.Family != ActionNomadDrone {
		return AttackPlan{}, errors.New("npc attack unsupported")
	}
	return PlanAttackWithProfile(
		npc, targetObjectID, targetPosition, profile, targetFootprintRadius,
	)
}

func PlanAttackWithProfile(
	npc Snapshot, targetObjectID uint32, targetPosition game.Vec3,
	profile ActionProfile, targetFootprintRadius float32,
) (AttackPlan, error) {
	plan, err := PlanControlWithProfile(
		npc, targetObjectID, targetPosition, profile, targetFootprintRadius,
	)
	if err != nil {
		return AttackPlan{}, fmt.Errorf("npcAttackControl: %w", err)
	}
	damage, err := resolveAttackDamage(npc, profile)
	if err != nil {
		return AttackPlan{}, fmt.Errorf("npcAttackDamage: %w", err)
	}
	plan.Damage = damage
	return plan, nil
}

// PlanAreaAttackWithProfile resolves damage for a target already admitted by
// an authored area or moving-trigger volume. It intentionally does not require
// the target to be the NPC's selected target or repeat a point-range check.
func PlanAreaAttackWithProfile(
	npc Snapshot, targetObjectID uint32, targetPosition game.Vec3,
	profile ActionProfile,
) (AttackPlan, error) {
	if npc.IsDefeated || npc.HitPoint <= 0 || targetObjectID == 0 {
		return AttackPlan{}, errors.New("npc area target unavailable")
	}
	damage, err := resolveAttackDamage(npc, profile)
	if err != nil {
		return AttackPlan{}, fmt.Errorf("npcAreaDamage: %w", err)
	}
	return AttackPlan{
		SourceObjectID:   npc.Plan.ObjectID,
		TargetObjectID:   targetObjectID,
		ActionGeneration: npc.ActionGeneration,
		SourcePosition:   npc.Plan.Position,
		TargetPosition:   targetPosition,
		Profile:          profile,
		Damage:           damage,
	}, nil
}

// PlanRetainedAreaAttackWithProfile resolves one pulse from a cast-time source
// snapshot. Retained world objects continue after their caster is defeated, so
// this path deliberately does not require a currently living source NPC.
func PlanRetainedAreaAttackWithProfile(
	source Snapshot, targetObjectID uint32, targetPosition game.Vec3,
	profile ActionProfile,
) (AttackPlan, error) {
	if source.Plan.ObjectID == 0 || targetObjectID == 0 {
		return AttackPlan{}, errors.New("npc retained area target unavailable")
	}
	damage, err := resolveAttackDamage(source, profile)
	if err != nil {
		return AttackPlan{}, fmt.Errorf("npcRetainedAreaDamage: %w", err)
	}
	return AttackPlan{
		SourceObjectID:   source.Plan.ObjectID,
		TargetObjectID:   targetObjectID,
		ActionGeneration: source.ActionGeneration,
		SourcePosition:   source.Plan.Position,
		TargetPosition:   targetPosition,
		Profile:          profile,
		Damage:           damage,
	}, nil
}

func resolveAttackDamage(
	npc Snapshot, profile ActionProfile,
) (game.DamageRange, error) {
	if profile.MinimumDamage <= 0 ||
		profile.MaximumDamage < profile.MinimumDamage {
		return game.DamageRange{}, errors.New("npc attack profile invalid")
	}
	damageProfile := game.DamageProfile{
		PrimaryAttribute:                npc.Plan.NPCProfile.Mind,
		IsPrimaryAttributeFound:         true,
		PrimaryAttributeBaseline:        nonPlayerPrimaryAttributeBaseline,
		IsPrimaryAttributeBaselineFound: true,
	}
	at := time.Now()
	if at.Before(npc.status.curseExpiresAt) {
		damageProfile.DamageBuff = npc.status.curseDamage.DamageBuff
		damageProfile.PhysicalAbilityDamageIncrease =
			npc.status.curseDamage.PhysicalIncrease
		damageProfile.EnergyAbilityDamageIncrease =
			npc.status.curseDamage.EnergyIncrease
	}
	if at.Before(npc.status.energyBuffExpiresAt) {
		damageProfile.EnergyDamageBuff += npc.status.energyDamageIncrease
	}
	if at.Before(npc.status.munchExpiresAt) {
		damageProfile.DamageBuff += npc.status.munchDamageIncrease
	}
	damageProfile.DamageBuff += npc.status.passiveDamageIncrease
	damageProfile.DamageBuff += npc.status.oozeGrowthDamageIncrease
	if npc.Plan.IsCaptain || npc.Plan.IsElite || npc.Plan.IsBoss ||
		npc.Plan.BossIdentity.HasModifier(EliteModifierName) {
		damageProfile.DamageBuff += EliteDamageBonus
	}
	descriptorMask := uint32(1 | 1<<6)
	damageType := uint8(0)
	damageSource := uint8(0)
	if profile.IsDamageProfileKnown {
		descriptorMask = profile.DescriptorMask
		damageType = profile.DamageType
		damageSource = profile.DamageSource
	}
	damage, err := game.ResolveAbilityDamageRange(game.AbilityDamage{
		Minimum:             profile.MinimumDamage,
		Maximum:             profile.MaximumDamage,
		Coefficient:         profile.DamageCoefficient,
		Descriptor:          descriptorMask,
		DamageType:          damageType,
		DamageSource:        damageSource,
		IsDescriptorFound:   true,
		IsDamageTypeFound:   true,
		IsDamageSourceFound: true,
	}, damageProfile)
	if err != nil {
		return game.DamageRange{}, fmt.Errorf("npcDamage: %w", err)
	}
	difficultyDamageMultiplier :=
		npc.Plan.NPCProfile.DifficultyDamageMultiplier
	if difficultyDamageMultiplier == 0 {
		difficultyDamageMultiplier = 1
	}
	damage.Minimum = float32(math.Floor(
		float64(damage.Minimum * difficultyDamageMultiplier),
	))
	damage.Maximum = float32(math.Ceil(
		float64(damage.Maximum * difficultyDamageMultiplier),
	))
	return damage, nil
}

func PlanControlWithProfile(
	npc Snapshot, targetObjectID uint32, targetPosition game.Vec3,
	profile ActionProfile, targetFootprintRadius float32,
) (AttackPlan, error) {
	isAnimationSupported := profile.AnimationName != "" ||
		profile.AbilityName == "SentryDroneLaser"
	if !isAnimationSupported || profile.Range <= 0 {
		return AttackPlan{}, errors.New("npc control profile invalid")
	}
	if npc.IsDefeated || npc.HitPoint <= 0 || targetObjectID == 0 ||
		npc.TargetObjectID != targetObjectID {
		return AttackPlan{}, errors.New("npc control target unavailable")
	}
	stopDistance, err := zoneaction.NPCStopDistance(
		profile.Range, npc.Plan.NPCProfile.FootprintRadius,
		targetFootprintRadius,
	)
	if err != nil {
		return AttackPlan{}, fmt.Errorf("npcControlRange: %w", err)
	}
	profile.Range = stopDistance
	if zonegeometry.Distance(npc.Plan.Position, targetPosition) >= profile.Range {
		return AttackPlan{}, ErrControlTargetOutOfRange
	}
	return AttackPlan{
		SourceObjectID:   npc.Plan.ObjectID,
		TargetObjectID:   targetObjectID,
		ActionGeneration: npc.ActionGeneration,
		SourcePosition:   npc.Plan.Position,
		TargetPosition:   targetPosition,
		Profile:          profile,
	}, nil
}

func CommitAttack(
	random *sim.SimulatorRandom, plan AttackPlan, criticalRating float32,
	tuning sim.CriticalTuning,
) (AttackResult, error) {
	selectedDamage, err := sim.SelectRankDamage(random, sim.DamageRange{
		Minimum: plan.Damage.Minimum,
		Maximum: plan.Damage.Maximum,
	})
	if err != nil {
		return AttackResult{}, fmt.Errorf("npcDamageSelect: %w", err)
	}
	critical := sim.CriticalDamageResult{Damage: selectedDamage}
	if tuning.DamageBonus != 0 || len(tuning.RatingConversions) != 0 {
		critical, err = sim.ResolveNonPlayerCriticalDamage(
			random, selectedDamage,
			sim.CriticalProfile{Rating: criticalRating}, tuning,
		)
		if err != nil {
			return AttackResult{}, fmt.Errorf("npcCritical: %w", err)
		}
	}
	return AttackResult{
		Damage:     critical.Damage,
		IsCritical: critical.IsCritical,
	}, nil
}

func ResolveModifierTickDamage(
	npc Snapshot, profile ActionProfile,
) (game.DamageRange, error) {
	if profile.ModifierMinimumTickDamage <= 0 ||
		profile.ModifierMaximumTickDamage < profile.ModifierMinimumTickDamage ||
		!profile.IsModifierDamageProfileKnown {
		return game.DamageRange{}, errors.New("npc modifier damage unavailable")
	}
	tickProfile := ActionProfile{
		MinimumDamage:        profile.ModifierMinimumTickDamage,
		MaximumDamage:        profile.ModifierMaximumTickDamage,
		DamageCoefficient:    profile.ModifierTickDamageCoefficient,
		DescriptorMask:       profile.ModifierDescriptorMask,
		DamageType:           profile.ModifierDamageType,
		DamageSource:         profile.ModifierDamageSource,
		IsDamageProfileKnown: true,
	}
	damage, err := resolveAttackDamage(npc, tickProfile)
	if err != nil {
		return game.DamageRange{}, fmt.Errorf("npcModifierDamage: %w", err)
	}
	return damage, nil
}
