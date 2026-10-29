package npc

import (
	"fmt"
	"strings"
	"time"
)

// AdditionalKitState belongs to the authored direct-spawn actor, independent of
// any campaign admission policy. Source timestamps are rebuilt after restore.
type AdditionalKitState struct {
	ReadyTimestamps        [4]uint64
	HealThresholdMask      uint8
	IsAllyDeathPending     bool
	IsHitAnimationDisabled bool
}

func RestoreAdditionalKit(state AdditionalKitState) AdditionalKitState {
	return AdditionalKitState{HealThresholdMask: state.HealThresholdMask}
}

func AdditionalKitProfile(nounName string) (ActionProfile, bool) {
	profile := ActionProfile{IsDamageProfileKnown: true, IsFirstAggroDurationKnown: true}
	switch strings.ToLower(nounName) {
	case "babymortar.noun":
		profile = BabyTossProfile()
	case "creaturex.noun":
		profile.Family, profile.AbilityName = ActionMelee, "InstantNova"
		profile.Range, profile.MovementSpeed, profile.NonCombatMovementSpeed = 10, 2.5, 2
		profile.AnimationName, profile.ImpactEffectName = "cast_frostbite", "instantnova_hit.ServerEventDef"
		profile.FirstAggroAnimationName, profile.FirstAggroDelay = "first_aggro_roar", 1750*time.Millisecond
		profile.HitDelay, profile.ReleaseDelay = time.Second, 1700000048*time.Nanosecond
		profile.DescriptorMask, profile.DamageType, profile.DamageSource = 128, 3, 1
		profile.ModifierName, profile.ModifierDuration, profile.MovementSpeedBuff = "Chilled", 5*time.Second, -0.1
		profile.PassiveEffectName = "ScaldronBasicThorno_ThornsPassive.ServerEventDef"
		profile.PassivePhysicalDamageReduction, profile.PassiveMeleeDamageReflection = 0.2, 0.5
	case "stagnant.noun":
		profile = SlowNovaProfile()
	case "verdanthbosstest.noun":
		var isFound bool
		profile, isFound = OrcusDiseaseConeProfile("VerdanthBoss.Noun")
		if !isFound {
			return ActionProfile{}, false
		}
		profile.FirstAggroAnimationName, profile.FirstAggroEffectName = "", ""
		profile.FirstAggroDelay = 0
	default:
		return ActionProfile{}, false
	}
	return profile, true
}

// ThornBarkModifier is independent of the currently selected phase action.
// Chunk188 has four literal rank entries .5/.25/1/.25 and a constant .2
// physical reduction. NPC ranks use the first three entries at passive start.
func AdditionalKitPassives(profile ActionProfile, plan SpawnPlan) ActionProfile {
	if !strings.EqualFold(plan.NounName, "CreatureX.Noun") {
		return profile
	}
	profile.PassiveEffectName = "ScaldronBasicThorno_ThornsPassive.ServerEventDef"
	profile.PassivePhysicalDamageReduction = 0.2
	profile.PassiveMeleeDamageReflection = [...]float32{0.5, 0.25, 1}[EffectivePassiveRank(plan)-1]
	return profile
}

func BabyTossProfile() ActionProfile {
	return ActionProfile{Family: ActionProjectile, AbilityName: "BabyToss", AnimationName: "cast_babytoss", FarAnimationName: "cast_babytoss", Range: 20, Cooldown: 4 * time.Second, HitDelay: 300000012 * time.Nanosecond, ReleaseDelay: 500 * time.Millisecond, MovementSpeed: 5.5, NonCombatMovementSpeed: 5.5, MinimumDamage: 20, MaximumDamage: 30, DamageType: 4, DamageSource: 0, DescriptorMask: 64, Radius: 4, ProjectileNoun: "Ability_Epic_Fireball.Noun", TrailEffectName: "baby_toss_projectile.ServerEventDef", ImpactEffectName: "firetoss_impact.ServerEventDef", ProjectileHeight: 10, ProjectileSpeed: 15, ProjectileDistance: 20, IsDamageProfileKnown: true, IsFirstAggroDurationKnown: true}
}

func SlowNovaProfile() ActionProfile {
	return ActionProfile{Family: ActionCone, AbilityName: "SlowNova", AnimationName: "cast_slownova", Range: 52.5, Radius: 10, Angle: 360, Cooldown: 5500 * time.Millisecond, HitDelay: 500 * time.Millisecond, ReleaseDelay: 500 * time.Millisecond, MovementSpeed: 6, NonCombatMovementSpeed: 3, MinimumDamage: 2, MaximumDamage: 4, DamageType: 3, DamageSource: 1, DescriptorMask: 128, ModifierName: "SlowNovaModifier", ModifierDuration: 5 * time.Second, MovementSpeedBuff: -0.6, TrailEffectName: "slownova_rings.ServerEventDef", IsDamageProfileKnown: true, IsFirstAggroDurationKnown: true}
}

func AdditionalSelfHealProfile() ActionProfile {
	return ActionProfile{Family: ActionMelee, AbilityName: "SelfHeal", AnimationName: "cast_fireball", Range: 55, HitDelay: 170000002 * time.Nanosecond, ReleaseDelay: 170000002 * time.Nanosecond, MinimumHealing: 1000, MaximumHealing: 1000, MovementSpeed: 7, NonCombatMovementSpeed: 6, IsDamageProfileKnown: true, IsFirstAggroDurationKnown: true}
}

func AdditionalBurrowNovaProfile() ActionProfile {
	profile := stagnantNovaAboveProfile(4 * time.Second)
	profile.AbilityName = "StagnantNova"
	profile.AnimationName = "burrow_attack1"
	profile.HitDelay = 1299999952 * time.Nanosecond
	profile.ReleaseDelay = 1299999952 * time.Nanosecond
	profile.NonCombatMovementSpeed = 3
	return profile
}

// SelectAdditionalKitAction evaluates the named graph's ordered gambits. The
// caller supplies current target distance; cooldown is paid only after admission.
func (e *Session) SelectAdditionalKitAction(objectID uint32, timestamp uint64, at time.Time, distance float32) (ActionProfile, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[objectID]
	if !isFound || actor.IsDefeated {
		return ActionProfile{}, false, nil
	}
	profile, isKnown := AdditionalKitProfile(actor.Plan.NounName)
	if !isKnown {
		return ActionProfile{}, false, nil
	}
	state := actor.AdditionalKit
	slot := 0
	switch strings.ToLower(actor.Plan.NounName) {
	case "babymortar.noun":
		if distance <= 10 {
			profile = ActionProfile{Family: ActionMelee, AbilityName: "Bite", AnimationName: "cast_bite", Range: 0.75, Radius: 1, Cooldown: 500 * time.Millisecond, HitDelay: 430000007 * time.Nanosecond, ReleaseDelay: 1429999948 * time.Nanosecond, MovementSpeed: 5.5, NonCombatMovementSpeed: 5.5, MinimumDamage: 8, MaximumDamage: 12, DamageType: 3, DescriptorMask: 67, IsDamageProfileKnown: true, IsFirstAggroDurationKnown: true}
			slot = 1
		}
	case "creaturex.noun":
		profile.MovementSpeedBuff = -[...]float32{0.1, 0.15, 0.2}[EffectiveNPCRank(actor.Plan)-1]
		if state.IsAllyDeathPending {
			profile.AbilityName = "Flee"
			profile.Range = 55
			state.IsAllyDeathPending = false
		}
	case "stagnant.noun":
		if distance > 8 && timestamp >= state.ReadyTimestamps[1] {
			profile.AbilityName = "Burrow"
			profile.Range = 55
			profile.Cooldown = [...]time.Duration{6 * time.Second, 5 * time.Second, 4 * time.Second}[EffectiveNPCRank(actor.Plan)-1]
			slot = 1
		} else if timestamp < state.ReadyTimestamps[0] {
			profile = shooterPoisonSpitProfile(3500*time.Millisecond, 14, 6, 6, 3, false)
			profile.FirstAggroAnimationName, profile.FirstAggroEffectName = "", ""
			profile.FirstAggroDelay = 0
			slot = 2
		}
	case "verdanthbosstest.noun":
		for index, threshold := range [...]float32{0.75, 0.5, 0.25} {
			mask := uint8(1 << index)
			if state.HealThresholdMask&mask == 0 && actor.HitPoint <= actor.Plan.NPCProfile.HitPoint*threshold {
				profile = AdditionalSelfHealProfile()
				state.HealThresholdMask |= mask
				break
			}
		}
	}
	// Freeze the selected action's native rank before a deferred pursuit/cast.
	// ApplyAffixTiming later consumes this profile without resolving rank twice.
	profile = RankedActionProfile(profile, actor.Plan)
	if timestamp < state.ReadyTimestamps[slot] && profile.AbilityName != "SelfHeal" && profile.AbilityName != "Flee" {
		return ActionProfile{}, true, nil
	}
	if at.IsZero() {
		return ActionProfile{}, false, fmt.Errorf("additionalClock: unavailable")
	}
	actor.AdditionalKit = state
	e.npcs[objectID] = actor
	return profile, true, nil
}

// Native AllyDied is a stimulus bit (chunk317). Dark Spin delivers the death
// event once to published allies using the existing Server Rules ally-alert
// enable/range controls. Native propagation/decay is not retained in the Lua.
func (e *Session) RecordAdditionalKitAllyDeath(sourceID uint32, rules AllyAlertRules) error {
	err := rules.Validate()
	if err != nil {
		return fmt.Errorf("deathRules: %w", err)
	}
	if !rules.IsEnabled {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	source, isFound := e.npcs[sourceID]
	if !isFound {
		return nil
	}
	for _, objectID := range e.objectIDs {
		actor := e.npcs[objectID]
		if actor.IsDefeated || !actor.IsPublished || !strings.EqualFold(actor.Plan.NounName, "CreatureX.Noun") || actor.Faction != source.Faction {
			continue
		}
		radius := actor.Plan.NPCProfile.AlertRange
		if rules.RangeOwner == "source" {
			radius = source.Plan.NPCProfile.AlertRange
		}
		radius *= float32(rules.RangePercent) / 100
		if actor.Plan.Position.Sub(source.Plan.Position).Length() > radius {
			continue
		}
		actor.AdditionalKit.IsAllyDeathPending = true
		e.npcs[objectID] = actor
	}
	return nil
}

func (e *Session) PayAdditionalKitCooldown(objectID uint32, abilityName string, readyTimestamp uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[objectID]
	if !isFound {
		return
	}
	slot := 0
	switch abilityName {
	case "Bite", "Burrow":
		slot = 1
	case "PoisonSpit":
		slot = 2
	case "StagnantNova":
		slot = 3
	}
	actor.AdditionalKit.ReadyTimestamps[slot] = readyTimestamp
	e.npcs[objectID] = actor
}

func (e *Session) SetAdditionalKitHitAnimation(objectID uint32, isDisabled bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[objectID]
	if !isFound {
		return
	}
	actor.AdditionalKit.IsHitAnimationDisabled = isDisabled
	e.npcs[objectID] = actor
}

// Retain the admitted action lease while the named phase selects its cast.
func (e *Session) SetAdditionalKitProfile(objectID uint32, profile ActionProfile) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	actor, isFound := e.npcs[objectID]
	if !isFound || actor.IsDefeated {
		return fmt.Errorf("additionalActor: missing %d", objectID)
	}
	actor.Plan.ActionProfile = profile.Clone()
	actor.Plan.IsActionKnown = true
	e.npcs[objectID] = actor
	return nil
}
