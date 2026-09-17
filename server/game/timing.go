package game

import (
	"errors"
	"math"
	"time"
)

type AbilityTiming struct {
	Duration          time.Duration
	Descriptor        uint32
	IsDescriptorFound bool
}

type TimingProfile struct {
	AttackSpeed             float32
	CooldownReduction       float32
	ProjectileSpeedIncrease float32
	ChannelTimeDecrease     float32
}

// ModifierTiming describes the authored duration classifications consumed by
// build-103's modifier-duration path.
type ModifierTiming struct {
	Duration         time.Duration
	IsBuff           bool
	IsDebuff         bool
	IsDamageOverTime bool
	IsCrowdControl   bool
}

// ModifierTimingProfile contains the source and recipient duration attributes
// applied before the client's separate diminishing-return stage.
type ModifierTimingProfile struct {
	BuffDurationIncrease      float32
	DebuffDurationReduction   float32
	DamageOverTimeReduction   float32
	CrowdControlTimeReduction float32
}

// ResolveProjectileSpeed implements the build-103 WaitForProjectile path.
// Positive attacker attribute 26 is copied to projectile MovementSpeedBuff 48;
// locomotion advances by authored speed multiplied by 1 + that buff.
func ResolveProjectileSpeed(speed float32, profile TimingProfile) (float32, error) {
	number := []float32{speed, profile.ProjectileSpeedIncrease}
	for _, current := range number {
		if math.IsNaN(float64(current)) || math.IsInf(float64(current), 0) {
			return 0, errors.New("invalid projectile speed")
		}
	}
	if speed <= 0 {
		return 0, errors.New("invalid projectile speed")
	}
	increase := max(profile.ProjectileSpeedIncrease, 0)
	projected := speed * (1 + increase)
	if math.IsInf(float64(projected), 0) {
		return 0, errors.New("invalid projectile speed")
	}
	return projected, nil
}

// ResolveAbilityDuration implements the build-103 sub_438F00 timing branch.
// Basic attacks use attack speed with the native -0.9 floor; other abilities
// use cooldown reduction. Missing descriptor evidence is an identity operation.
func ResolveAbilityDuration(ability AbilityTiming, profile TimingProfile) (time.Duration, error) {
	if ability.Duration < 0 {
		return 0, errors.New("invalid ability duration")
	}
	profileNumbers := []float32{profile.AttackSpeed, profile.CooldownReduction}
	for _, number := range profileNumbers {
		if math.IsNaN(float64(number)) || math.IsInf(float64(number), 0) {
			return 0, errors.New("invalid timing profile")
		}
	}
	if !ability.IsDescriptorFound {
		return ability.Duration, nil
	}
	seconds := float64(ability.Duration) / float64(time.Second)
	if ability.Descriptor&2 != 0 {
		attackSpeed := max(profile.AttackSpeed, -0.9)
		seconds /= float64(1 + attackSpeed)
	} else {
		seconds *= float64(1 - profile.CooldownReduction)
	}
	seconds = max(0, seconds)
	return time.Duration(seconds * float64(time.Second)), nil
}

// ResolveChannelDuration implements build-103 sub_438810. Channel time is
// independent of the basic-attack and cooldown branches above.
func ResolveChannelDuration(duration time.Duration, profile TimingProfile) (time.Duration, error) {
	if duration < 0 {
		return 0, errors.New("invalid channel duration")
	}
	if math.IsNaN(float64(profile.ChannelTimeDecrease)) ||
		math.IsInf(float64(profile.ChannelTimeDecrease), 0) {
		return 0, errors.New("invalid channel timing profile")
	}
	seconds := float64(duration) / float64(time.Second)
	seconds *= float64(1 - profile.ChannelTimeDecrease)
	seconds = max(0, seconds)
	return time.Duration(seconds * float64(time.Second)), nil
}

// ResolveAreaDurationCount reproduces the positive numeric-for-loop bounds used
// by recovered abilities that multiply their authored iteration or shot count
// by 1 + AoEDurationIncrease. Lua admits only each whole positive iteration.
func ResolveAreaDurationCount(count uint32, increase float32) (uint32, error) {
	if count == 0 || math.IsNaN(float64(increase)) || math.IsInf(float64(increase), 0) {
		return 0, errors.New("invalid area duration count")
	}
	projected := float64(count) * (1 + float64(max(increase, 0)))
	if math.IsInf(projected, 0) || projected > float64(^uint32(0)) {
		return 0, errors.New("invalid area duration count")
	}
	return max(uint32(1), uint32(projected)), nil
}

// ResolveAreaDuration reproduces recovered continuous AoE lifetime scaling.
func ResolveAreaDuration(duration time.Duration, increase float32) (time.Duration, error) {
	if duration <= 0 || math.IsNaN(float64(increase)) || math.IsInf(float64(increase), 0) {
		return 0, errors.New("invalid area duration")
	}
	projected := float64(duration) * (1 + float64(max(increase, 0)))
	if math.IsInf(projected, 0) || projected > float64(math.MaxInt64) {
		return 0, errors.New("invalid area duration")
	}
	return time.Duration(projected), nil
}

// ResolveOverdriveDuration reproduces build-103's Overdrive meter path. The
// equipped duration attribute reduces the authored energy drain rate rather
// than multiplying the final duration directly.
func ResolveOverdriveDuration(
	maximumEnergy float64, drainPerSecond float64, increase float32,
) (time.Duration, error) {
	numbers := []float64{maximumEnergy, drainPerSecond, float64(increase)}
	for _, number := range numbers {
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return 0, errors.New("invalid overdrive duration")
		}
	}
	if maximumEnergy <= 0 || drainPerSecond <= 0 || increase < 0 {
		return 0, errors.New("invalid overdrive duration")
	}
	projectedDrain := drainPerSecond * (1 - float64(increase))
	if projectedDrain <= 0 {
		return 0, errors.New("invalid overdrive drain")
	}
	seconds := maximumEnergy / projectedDrain
	if math.IsInf(seconds, 0) || seconds > float64(math.MaxInt64)/float64(time.Second) {
		return 0, errors.New("invalid overdrive duration")
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

// ResolveModifierAttributeDuration reproduces the attribute-only portion of
// build-103 sub_9DE220. Buff/debuff source scaling is followed by recipient
// DoT or crowd-control reduction. The caller remains responsible for the
// client's separate diminishing-return and modifier-specific stages.
func ResolveModifierAttributeDuration(modifier ModifierTiming, profile ModifierTimingProfile) (time.Duration, error) {
	if modifier.Duration < 0 {
		return 0, errors.New("invalid modifier duration")
	}
	profileNumbers := []float32{
		profile.BuffDurationIncrease,
		profile.DebuffDurationReduction,
		profile.DamageOverTimeReduction,
		profile.CrowdControlTimeReduction,
	}
	for _, number := range profileNumbers {
		if math.IsNaN(float64(number)) || math.IsInf(float64(number), 0) {
			return 0, errors.New("invalid modifier timing profile")
		}
	}
	seconds := float64(modifier.Duration) / float64(time.Second)
	if modifier.IsBuff {
		seconds *= float64(1 + profile.BuffDurationIncrease)
	} else if modifier.IsDebuff {
		seconds *= float64(1 - profile.DebuffDurationReduction)
	}
	if modifier.IsDamageOverTime {
		seconds *= float64(1 - profile.DamageOverTimeReduction)
	} else if modifier.IsCrowdControl {
		seconds *= float64(1 - profile.CrowdControlTimeReduction)
	}
	seconds = max(0, seconds)
	return time.Duration(seconds * float64(time.Second)), nil
}

func timingProfile(attribute [partAttributeCount]float32) TimingProfile {
	return TimingProfile{
		AttackSpeed: attribute[23], CooldownReduction: attribute[24],
		ProjectileSpeedIncrease: attribute[26], ChannelTimeDecrease: attribute[92],
	}
}
