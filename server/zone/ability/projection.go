package ability

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
)

func ProjectDamage(
	creature game.GameplayCreature, definition sim.AbilityDefinition,
	minimum float32, maximum float32,
) (game.DamageRange, error) {
	if definition.IsWeaponDamageRange {
		minimum = creature.MinimumWeaponDamage
		maximum = creature.MaximumWeaponDamage
	}
	if definition.IsDamageTypeFound && definition.DamageType > uint32(^uint8(0)) {
		return game.DamageRange{}, errors.New("damage type overflow")
	}
	if definition.IsDamageSourceFound && definition.DamageSource > uint32(^uint8(0)) {
		return game.DamageRange{}, errors.New("damage source overflow")
	}
	damage, err := game.ResolveAbilityDamageRange(game.AbilityDamage{
		Minimum: minimum, Maximum: maximum,
		Coefficient:         definition.DamageCoefficient,
		Descriptor:          definition.DescriptorMask,
		DamageType:          uint8(definition.DamageType),
		DamageSource:        uint8(definition.DamageSource),
		IsDescriptorFound:   definition.IsDescriptorFound,
		IsDamageTypeFound:   definition.IsDamageTypeFound,
		IsDamageSourceFound: definition.IsDamageSourceFound,
	}, creature.DamageProfile)
	if err != nil {
		return game.DamageRange{}, fmt.Errorf("damageProject: %w", err)
	}
	return damage, nil
}

func ProjectCooldown(
	creature game.GameplayCreature, definition sim.AbilityDefinition,
) (time.Duration, error) {
	duration, err := game.ResolveAbilityDuration(game.AbilityTiming{
		Duration:          definition.Cooldown,
		Descriptor:        definition.DescriptorMask,
		IsDescriptorFound: definition.IsDescriptorFound,
	}, creature.TimingProfile)
	if err != nil {
		return 0, fmt.Errorf("cooldownProject: %w", err)
	}
	return duration, nil
}

func ProjectTiming(
	creature game.GameplayCreature, definition sim.AbilityDefinition,
) (sim.AbilityDefinition, error) {
	projected := definition
	var err error
	projected.Cooldown, err = ProjectCooldown(creature, definition)
	if err != nil {
		return sim.AbilityDefinition{}, fmt.Errorf("abilityCooldown: %w", err)
	}
	projected.HitDelay, err = ProjectDuration(
		creature, definition, definition.HitDelay,
	)
	if err != nil {
		return sim.AbilityDefinition{}, fmt.Errorf("abilityHit: %w", err)
	}
	projected.ReleaseDelay, err = ProjectDuration(
		creature, definition, definition.ReleaseDelay,
	)
	if err != nil {
		return sim.AbilityDefinition{}, fmt.Errorf("abilityRelease: %w", err)
	}
	projected.HitDelays = make([]time.Duration, len(definition.HitDelays))
	for index, duration := range definition.HitDelays {
		projected.HitDelays[index], err = ProjectDuration(
			creature, definition, duration,
		)
		if err != nil {
			return sim.AbilityDefinition{}, fmt.Errorf("abilityHit[%d]: %w", index, err)
		}
	}
	projected.AnimationHitDelays = make(
		[][]time.Duration, len(definition.AnimationHitDelays),
	)
	for animationIndex, hitDelay := range definition.AnimationHitDelays {
		projected.AnimationHitDelays[animationIndex] = make(
			[]time.Duration, len(hitDelay),
		)
		for hitIndex, duration := range hitDelay {
			projected.AnimationHitDelays[animationIndex][hitIndex], err =
				ProjectDuration(creature, definition, duration)
			if err != nil {
				return sim.AbilityDefinition{}, fmt.Errorf(
					"abilityAnimationHit[%d:%d]: %w",
					animationIndex, hitIndex, err,
				)
			}
		}
	}
	projected.ReleaseDelays = make(
		[]time.Duration, len(definition.ReleaseDelays),
	)
	for index, duration := range definition.ReleaseDelays {
		projected.ReleaseDelays[index], err = ProjectDuration(
			creature, definition, duration,
		)
		if err != nil {
			return sim.AbilityDefinition{}, fmt.Errorf(
				"abilityRelease[%d]: %w", index, err,
			)
		}
	}
	return projected, nil
}

func ProjectDuration(
	creature game.GameplayCreature, definition sim.AbilityDefinition,
	duration time.Duration,
) (time.Duration, error) {
	projected, err := game.ResolveAbilityDuration(game.AbilityTiming{
		Duration:          duration,
		Descriptor:        definition.DescriptorMask,
		IsDescriptorFound: definition.IsDescriptorFound,
	}, creature.TimingProfile)
	if err != nil {
		return 0, fmt.Errorf("durationProject: %w", err)
	}
	return projected, nil
}

func ProjectProjectileSpeed(
	creature game.GameplayCreature, definition sim.AbilityDefinition,
) (float32, error) {
	speed, err := game.ResolveProjectileSpeed(
		definition.Speed, creature.TimingProfile,
	)
	if err != nil {
		return 0, fmt.Errorf("projectileSpeedProject: %w", err)
	}
	return speed, nil
}

func ProjectileTravelDuration(
	distance float32, speed float32, acceleration float32,
) (time.Duration, error) {
	if distance < 0 || speed < 0 || acceleration < 0 ||
		math.IsNaN(float64(distance)) || math.IsInf(float64(distance), 0) ||
		math.IsNaN(float64(speed)) || math.IsInf(float64(speed), 0) ||
		math.IsNaN(float64(acceleration)) || math.IsInf(float64(acceleration), 0) {
		return 0, errors.New("invalid projectile travel")
	}
	if distance == 0 {
		return 0, nil
	}
	if acceleration == 0 {
		if speed == 0 {
			return 0, errors.New("stationary projectile")
		}
		return time.Duration(float64(distance/speed) * float64(time.Second)), nil
	}
	distance64 := float64(distance)
	speed64 := float64(speed)
	acceleration64 := float64(acceleration)
	discriminant := speed64*speed64 + 2*acceleration64*distance64
	seconds := (math.Sqrt(discriminant) - speed64) / acceleration64
	if seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return 0, errors.New("invalid projectile duration")
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

func ProjectileSpeedAfter(
	speed float32, acceleration float32, duration time.Duration,
) (float32, error) {
	if speed < 0 || acceleration < 0 || duration < 0 ||
		math.IsNaN(float64(speed)) || math.IsInf(float64(speed), 0) ||
		math.IsNaN(float64(acceleration)) || math.IsInf(float64(acceleration), 0) {
		return 0, errors.New("invalid projectile speed")
	}
	return speed + acceleration*float32(duration.Seconds()), nil
}

func ProjectHealing(
	creature game.GameplayCreature, definition sim.AbilityDefinition,
	amount float32,
) (float32, error) {
	healing, err := game.ResolveAbilityHealing(game.AbilityHealing{
		Amount:            amount,
		Coefficient:       definition.HealingCoefficient,
		Descriptor:        definition.DescriptorMask,
		IsDescriptorFound: definition.IsDescriptorFound,
	}, creature.HealingProfile)
	if err != nil {
		return 0, fmt.Errorf("healingProject: %w", err)
	}
	return healing, nil
}
