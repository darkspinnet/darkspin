package sim

import (
	"errors"
	"fmt"
	"time"
)

// TargetedAOEBehavior owns the authored activation, effect-object lifetime,
// pulse deadlines, and release boundary. The world adapter resolves live
// targets at each pulse rather than snapshotting them at cast time.
type TargetedAOEBehavior struct {
	simulator *Simulator
	scope     CancelScope
	input     TargetedAOEInput
	tasks     []TaskID
	isSpawned bool
	isDone    bool
}

type TargetedAOEInput struct {
	Definition AbilityDefinition
	ActorRole  Role
	EffectRole Role
	Position   Position
}

func StartTargetedAOE(
	simulator *Simulator, scope CancelScope, input TargetedAOEInput,
) (*TargetedAOEBehavior, error) {
	definition := input.Definition
	if simulator == nil {
		return nil, errors.New("nil simulator")
	}
	if !simulator.isScopeActive(scope) {
		return nil, errors.New("inactive scope")
	}
	if definition.Kind != AbilityKindTargetedAOE || input.ActorRole == "" || input.EffectRole == "" ||
		definition.Name == "" || definition.AnimationName == "" || len(definition.EffectNouns) != 1 ||
		definition.EffectNouns[0] == "" || definition.MuzzleEffectName == "" || definition.Cooldown <= 0 ||
		definition.Range <= 0 || definition.ManaCost < 0 || definition.Radius <= 0 ||
		definition.HitDelay < 0 || definition.ReleaseDelay < definition.HitDelay ||
		definition.TickDuration <= 0 || definition.NumberOfTicks == 0 ||
		definition.MinimumDamagePerTick < 0 ||
		definition.MaximumDamagePerTick < definition.MinimumDamagePerTick ||
		definition.MinimumHealingPerTick < 0 ||
		definition.MaximumHealingPerTick < definition.MinimumHealingPerTick ||
		definition.RootChance < 0 || definition.RootChance > 1 || !isFinitePosition(input.Position) {
		return nil, fmt.Errorf("targeted AOE input: %#v", input)
	}
	behavior := &TargetedAOEBehavior{simulator: simulator, scope: scope, input: input}
	for index, intent := range []Intent{
		AnimationIntent{Role: input.ActorRole, AnimationName: definition.AnimationName},
		CooldownIntent{Role: input.ActorRole, AbilityName: definition.Name, Duration: definition.Cooldown},
		ResourceChangeIntent{Role: input.ActorRole, Delta: -definition.ManaCost},
	} {
		err := behavior.emit(intent)
		if err != nil {
			return nil, fmt.Errorf("activationEmit[%d]: %w", index, err)
		}
	}
	err := behavior.schedule(definition.HitDelay, behavior.spawn)
	if err != nil {
		return nil, fmt.Errorf("spawnSchedule: %w", err)
	}
	err = behavior.schedule(definition.ReleaseDelay, behavior.release)
	if err != nil {
		return nil, fmt.Errorf("releaseSchedule: %w", err)
	}
	firstTick := uint32(1)
	if definition.IsInitialPulse {
		firstTick = 0
	}
	for tick := firstTick; tick <= definition.NumberOfTicks; tick++ {
		if definition.IsInitialPulse && tick == definition.NumberOfTicks {
			break
		}
		currentTick := tick
		deadline := definition.HitDelay +
			time.Duration(tick)*definition.TickDuration
		err = behavior.schedule(deadline, func() error { return behavior.pulse(currentTick) })
		if err != nil {
			return nil, fmt.Errorf("pulseSchedule[%d]: %w", tick, err)
		}
	}
	return behavior, nil
}

func (b *TargetedAOEBehavior) spawn() error {
	if b.isDone {
		return nil
	}
	definition := b.input.Definition
	for index, intent := range []Intent{
		PositionedEffectIntent{EffectName: definition.MuzzleEffectName, Position: b.input.Position},
		SpawnIntent{Role: b.input.EffectRole, NounName: definition.EffectNouns[0]},
		TeleportIntent{Role: b.input.EffectRole, Destination: b.input.Position},
	} {
		err := b.emit(intent)
		if err != nil {
			return fmt.Errorf("spawnEmit[%d]: %w", index, err)
		}
	}
	b.isSpawned = true
	return nil
}

func (b *TargetedAOEBehavior) pulse(tick uint32) error {
	if b.isDone || !b.isSpawned {
		return nil
	}
	definition := b.input.Definition
	minimumDamage := definition.MinimumDamagePerTick
	maximumDamage := definition.MaximumDamagePerTick
	if definition.IsInitialPulse && tick == 0 {
		minimumDamage = definition.MinimumDamage
		maximumDamage = definition.MaximumDamage
	}
	err := b.emit(AreaPulseIntent{
		Role: b.input.EffectRole, ActorRole: b.input.ActorRole,
		Position: b.input.Position, Radius: definition.Radius, Tick: tick,
		DamageMinimum: minimumDamage, DamageMaximum: maximumDamage,
		DamageCoefficient: definition.DamageCoefficient,
		HealingMinimum:    definition.MinimumHealingPerTick, HealingMaximum: definition.MaximumHealingPerTick,
		HealingCoefficient: definition.HealingCoefficient,
		RootModifierID:     definition.RootModifierID, RootChance: definition.RootChance,
		IsFirstTargetOnly: definition.IsInitialPulse && tick == 0 &&
			definition.IsInitialTargetOnly,
	})
	if err != nil {
		return fmt.Errorf("pulseEmit[%d]: %w", tick, err)
	}
	isFinalTick := tick == definition.NumberOfTicks
	if definition.IsInitialPulse {
		isFinalTick = tick+1 == definition.NumberOfTicks
	}
	if !isFinalTick {
		return nil
	}
	err = b.emit(DespawnIntent{Role: b.input.EffectRole})
	if err != nil {
		return fmt.Errorf("despawnEmit: %w", err)
	}
	b.isSpawned = false
	b.isDone = true
	return nil
}

func (b *TargetedAOEBehavior) release() error {
	if b.isDone {
		return nil
	}
	return b.emit(AbilityReleaseIntent{
		Role: b.input.ActorRole, AbilityName: b.input.Definition.Name,
	})
}

func (b *TargetedAOEBehavior) Cancel() error {
	if b == nil || b.simulator == nil || b.isDone {
		return nil
	}
	for _, taskID := range b.tasks {
		b.simulator.Cancel(taskID)
	}
	b.tasks = nil
	if b.isSpawned {
		err := b.emit(DespawnIntent{Role: b.input.EffectRole})
		if err != nil {
			return fmt.Errorf("cancelDespawn: %w", err)
		}
	}
	b.isSpawned = false
	b.isDone = true
	return nil
}

func (b *TargetedAOEBehavior) emit(intent Intent) error {
	err := b.simulator.EmitScoped(intent, b.input.Definition.Provenance, b.scope)
	if err != nil {
		return fmt.Errorf("emitScoped: %w", err)
	}
	return nil
}

func (b *TargetedAOEBehavior) schedule(delay time.Duration, callback func() error) error {
	taskID, err := b.simulator.Schedule(delay, b.scope, func(*Simulator) error {
		err := callback()
		if err != nil {
			return fmt.Errorf("targetedAOEContinue: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("simSchedule: %w", err)
	}
	b.tasks = append(b.tasks, taskID)
	return nil
}
