package sim

import (
	"errors"
	"fmt"
	"time"
)

// AreaHealingBehavior owns an authored healing object's cast, lifetime, pulse
// requests, and final despawn. The world port resolves the living allies at
// each pulse so reserve/deployed health is never snapshotted at cast time.
type AreaHealingBehavior struct {
	simulator *Simulator
	scope     CancelScope
	input     AreaHealingInput
	tasks     []TaskID
	isSpawned bool
	isDone    bool
}

type AreaHealingInput struct {
	Definition AbilityDefinition
	ActorRole  Role
	EffectRole Role
	Position   Position
}

func StartAreaHealing(
	simulator *Simulator, scope CancelScope, input AreaHealingInput,
) (*AreaHealingBehavior, error) {
	definition := input.Definition
	if simulator == nil {
		return nil, errors.New("nil simulator")
	}
	if !simulator.isScopeActive(scope) {
		return nil, errors.New("inactive scope")
	}
	if definition.Kind != AbilityKindAreaHealing || input.ActorRole == "" || input.EffectRole == "" ||
		definition.Name == "" || definition.AnimationName == "" || definition.SpawnNoun == "" ||
		definition.AbsorbEffectName == "" || len(definition.GrowthEffectNames) == 0 ||
		definition.GrowthEffectNames[0] == "" || definition.HealEffectName == "" ||
		definition.Cooldown <= 0 || definition.Range <= 0 || definition.ManaCost < 0 ||
		definition.Radius <= 0 || definition.HitDelay < 0 || definition.ReleaseDelay < definition.HitDelay ||
		definition.Duration <= 0 || definition.TickDuration <= 0 ||
		definition.Duration%definition.TickDuration != 0 || definition.MinimumHealingPerTick < 0 ||
		definition.MaximumHealingPerTick < definition.MinimumHealingPerTick ||
		definition.MinimumFinalHealing < 0 ||
		definition.MaximumFinalHealing < definition.MinimumFinalHealing || !isFinitePosition(input.Position) {
		return nil, fmt.Errorf("area healing input: %#v", input)
	}
	behavior := &AreaHealingBehavior{simulator: simulator, scope: scope, input: input}
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
	tickCount := uint32(definition.Duration / definition.TickDuration)
	for tick := uint32(1); tick <= tickCount; tick++ {
		currentTick := tick
		deadline := definition.HitDelay + time.Duration(tick)*definition.TickDuration
		err = behavior.schedule(deadline, func() error { return behavior.pulse(currentTick, tickCount) })
		if err != nil {
			return nil, fmt.Errorf("pulseSchedule[%d]: %w", tick, err)
		}
	}
	return behavior, nil
}

func (b *AreaHealingBehavior) spawn() error {
	if b.isDone {
		return nil
	}
	definition := b.input.Definition
	for index, intent := range []Intent{
		SpawnIntent{Role: b.input.EffectRole, NounName: definition.SpawnNoun},
		TeleportIntent{Role: b.input.EffectRole, Destination: b.input.Position},
		EffectIntent{Role: b.input.EffectRole, EffectName: definition.AbsorbEffectName, Slot: 1},
		EffectIntent{Role: b.input.EffectRole, EffectName: definition.GrowthEffectNames[0], Slot: 2},
	} {
		err := b.emit(intent)
		if err != nil {
			return fmt.Errorf("spawnEmit[%d]: %w", index, err)
		}
	}
	b.isSpawned = true
	return nil
}

func (b *AreaHealingBehavior) pulse(tick uint32, tickCount uint32) error {
	if b.isDone || !b.isSpawned {
		return nil
	}
	definition := b.input.Definition
	if tick < uint32(len(definition.GrowthEffectNames)) {
		growthName := definition.GrowthEffectNames[tick]
		if growthName == "" {
			return fmt.Errorf("growthEffect[%d]: empty", tick)
		}
		for index, intent := range []EffectIntent{
			{Role: b.input.EffectRole, Slot: 2, IsStopped: true},
			{Role: b.input.EffectRole, EffectName: growthName, Slot: 2},
		} {
			err := b.emit(intent)
			if err != nil {
				return fmt.Errorf("growthEmit[%d/%d]: %w", tick, index, err)
			}
		}
	}
	healingMinimum := definition.MinimumHealingPerTick
	healingMaximum := definition.MaximumHealingPerTick
	if tick == tickCount {
		healingMinimum += definition.MinimumFinalHealing
		healingMaximum += definition.MaximumFinalHealing
	}
	err := b.emit(AreaPulseIntent{
		Role: b.input.EffectRole, ActorRole: b.input.ActorRole,
		Position: b.input.Position, Radius: definition.Radius, Tick: tick,
		HealingMinimum: healingMinimum, HealingMaximum: healingMaximum,
		HealingCoefficient: definition.HealingCoefficient,
	})
	if err != nil {
		return fmt.Errorf("pulseEmit[%d]: %w", tick, err)
	}
	if tick != tickCount {
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

func (b *AreaHealingBehavior) release() error {
	if b.isDone {
		return nil
	}
	return b.emit(AbilityReleaseIntent{Role: b.input.ActorRole, AbilityName: b.input.Definition.Name})
}

func (b *AreaHealingBehavior) Cancel() error {
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

func (b *AreaHealingBehavior) emit(intent Intent) error {
	err := b.simulator.EmitScoped(intent, b.input.Definition.Provenance, b.scope)
	if err != nil {
		return fmt.Errorf("emitScoped: %w", err)
	}
	return nil
}

func (b *AreaHealingBehavior) schedule(delay time.Duration, callback func() error) error {
	taskID, err := b.simulator.Schedule(delay, b.scope, func(*Simulator) error {
		err := callback()
		if err != nil {
			return fmt.Errorf("areaHealingContinue: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("simSchedule: %w", err)
	}
	b.tasks = append(b.tasks, taskID)
	return nil
}
