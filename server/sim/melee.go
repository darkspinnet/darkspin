package sim

import (
	"errors"
	"fmt"
	"time"
)

// TimedMeleeInput describes the common authored melee boundary while retaining
// the ability-specific timing and presentation assets supplied by content.
type TimedMeleeInput struct {
	ActorRole          Role
	TargetRole         Role
	AbilityName        string
	AnimationName      string
	HitEffectName      string
	TargetPosition     Position
	TargetFacing       Position
	HitDelay           time.Duration
	HitDelays          []time.Duration
	ReleaseDelay       time.Duration
	Cooldown           time.Duration
	Damage             float32
	TargetHitPoint     float32
	IsTargetValidAtHit bool
	IsCritical         bool
	Provenance         Provenance
}

// TimedMeleeBehavior owns one deterministic activation, hit, and release.
type TimedMeleeBehavior struct {
	simulator      *Simulator
	scope          CancelScope
	input          TimedMeleeInput
	tasks          []TaskID
	areHitsStarted []bool
	isCanceled     bool
}

func StartTimedMelee(
	simulator *Simulator, scope CancelScope, input TimedMeleeInput,
) (*TimedMeleeBehavior, error) {
	if simulator == nil {
		return nil, errors.New("nil simulator")
	}
	hitDelays := append([]time.Duration(nil), input.HitDelays...)
	if len(hitDelays) == 0 {
		hitDelays = []time.Duration{input.HitDelay}
	}
	if input.ActorRole == "" || input.TargetRole == "" || input.AbilityName == "" ||
		input.AnimationName == "" || input.Damage <= 0 ||
		input.TargetHitPoint < 0 || input.HitDelay < 0 || input.ReleaseDelay < hitDelays[len(hitDelays)-1] ||
		input.Cooldown < 0 {
		return nil, fmt.Errorf("melee input: %#v", input)
	}
	for index, hitDelay := range hitDelays {
		if hitDelay < 0 || (index != 0 && hitDelay <= hitDelays[index-1]) {
			return nil, fmt.Errorf("melee hit schedule: %#v", hitDelays)
		}
	}
	input.HitDelays = hitDelays
	if !isFinitePosition(input.TargetPosition) || !isFinitePosition(input.TargetFacing) {
		return nil, errors.New("non-finite melee input")
	}
	if !simulator.isScopeActive(scope) {
		return nil, errors.New("inactive scope")
	}
	behavior := &TimedMeleeBehavior{
		simulator: simulator, scope: scope, input: input, areHitsStarted: make([]bool, len(hitDelays)),
	}
	err := behavior.emit(LocomotionStopIntent{
		Role: input.ActorRole, Facing: input.TargetFacing,
		TargetPosition: input.TargetPosition, IsTurn: true,
	})
	if err != nil {
		return nil, fmt.Errorf("stopEmit: %w", err)
	}
	err = behavior.emit(AnimationIntent{Role: input.ActorRole, AnimationName: input.AnimationName})
	if err != nil {
		return nil, fmt.Errorf("animationEmit: %w", err)
	}
	err = behavior.emit(TargetValidationIntent{
		ActorRole: input.ActorRole, TargetRole: input.TargetRole, Stage: "activation", IsValid: true,
	})
	if err != nil {
		return nil, fmt.Errorf("activationValidate: %w", err)
	}
	for hitIndex, hitDelay := range hitDelays {
		index := hitIndex
		err = behavior.schedule(hitDelay, func() error { return behavior.hit(index) })
		if err != nil {
			return nil, fmt.Errorf("hitSchedule[%d]: %w", hitIndex, err)
		}
	}
	err = behavior.schedule(input.ReleaseDelay, behavior.release)
	if err != nil {
		return nil, fmt.Errorf("releaseSchedule: %w", err)
	}
	return behavior, nil
}

func (b *TimedMeleeBehavior) hit(hitIndex int) error {
	b.areHitsStarted[hitIndex] = true
	err := b.emit(TargetValidationIntent{
		ActorRole: b.input.ActorRole, TargetRole: b.input.TargetRole,
		Stage: "hit", IsValid: b.input.IsTargetValidAtHit,
	})
	if err != nil {
		return fmt.Errorf("hitValidate: %w", err)
	}
	if hitIndex == 0 {
		err = b.emit(CooldownIntent{
			Role: b.input.ActorRole, AbilityName: b.input.AbilityName, Duration: b.input.Cooldown,
		})
		if err != nil {
			return fmt.Errorf("cooldownEmit: %w", err)
		}
	}
	if !b.input.IsTargetValidAtHit {
		return nil
	}
	intents := []Intent{
		DamageIntent{
			ActorRole: b.input.ActorRole, TargetRole: b.input.TargetRole,
			DeltaHealth: b.input.Damage, IntegerHitPointChange: -int32(b.input.Damage),
			IsCritical: b.input.IsCritical,
		},
	}
	if b.input.HitEffectName != "" {
		intents = append(intents, EffectIntent{
			Role: b.input.TargetRole, ActorRole: b.input.ActorRole,
			EffectName: b.input.HitEffectName, Facing: b.input.TargetFacing,
		})
	}
	intents = append(intents, HitPointIntent{
		Role: b.input.TargetRole, HitPoints: max(0, b.input.TargetHitPoint-b.input.Damage),
	})
	for index, intent := range intents {
		err = b.emit(intent)
		if err != nil {
			return fmt.Errorf("hitEmit[%d]: %w", index, err)
		}
	}
	return nil
}

// PrepareHit updates the authoritative target snapshot immediately before the
// scheduled hit continuation. It permits movement, death, and earlier
// same-deadline attackers to affect validation and resulting HP deterministically.
func (b *TimedMeleeBehavior) PrepareHit(
	isTargetValid bool, targetHitPoint float32, damage float32, isCritical bool,
	targetPosition Position, targetFacing Position,
) error {
	return b.PrepareHitAt(0, isTargetValid, targetHitPoint, damage, isCritical, targetPosition, targetFacing)
}

// PrepareHitAt updates one authored hit in a multi-hit animation.
func (b *TimedMeleeBehavior) PrepareHitAt(
	hitIndex int, isTargetValid bool, targetHitPoint float32, damage float32, isCritical bool,
	targetPosition Position, targetFacing Position,
) error {
	if b == nil || b.simulator == nil {
		return errors.New("nil melee behavior")
	}
	if hitIndex < 0 || hitIndex >= len(b.input.HitDelays) {
		return fmt.Errorf("melee hit index: %d", hitIndex)
	}
	if b.isCanceled || b.areHitsStarted[hitIndex] || b.simulator.Now() >= b.input.HitDelays[hitIndex] {
		return errors.New("melee hit already started")
	}
	if targetHitPoint < 0 || damage <= 0 || !isFinitePosition(targetPosition) || !isFinitePosition(targetFacing) {
		return errors.New("invalid melee hit state")
	}
	b.input.IsTargetValidAtHit = isTargetValid
	b.input.TargetHitPoint = targetHitPoint
	b.input.Damage = damage
	b.input.IsCritical = isCritical
	b.input.TargetPosition = targetPosition
	b.input.TargetFacing = targetFacing
	return nil
}

func (b *TimedMeleeBehavior) release() error {
	return b.emit(AbilityReleaseIntent{Role: b.input.ActorRole, AbilityName: b.input.AbilityName})
}

func (b *TimedMeleeBehavior) Cancel() {
	if b == nil || b.simulator == nil || b.isCanceled {
		return
	}
	for _, taskID := range b.tasks {
		b.simulator.Cancel(taskID)
	}
	b.tasks = nil
	b.isCanceled = true
}

func (b *TimedMeleeBehavior) emit(intent Intent) error {
	err := b.simulator.EmitScoped(intent, b.input.Provenance, b.scope)
	if err != nil {
		return fmt.Errorf("simulatorEmit: %w", err)
	}
	return nil
}

func (b *TimedMeleeBehavior) schedule(delay time.Duration, callback func() error) error {
	taskID, err := b.simulator.Schedule(delay, b.scope, func(*Simulator) error {
		if b.isCanceled {
			return nil
		}
		err := callback()
		if err != nil {
			return fmt.Errorf("callback: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("simulatorSchedule: %w", err)
	}
	b.tasks = append(b.tasks, taskID)
	return nil
}
