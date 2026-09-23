package sim

import (
	"errors"
	"fmt"
	"time"
)

const ordinaryDeathRevivalWindow = 10 * time.Second
const ordinaryDeathFadeDuration = 5 * time.Second
const criticalDeathSettleDelay = 100 * time.Millisecond
const criticalDeathDeleteDelay = 3 * time.Second

// DeathBehaviorInput binds the recovered shared death behavior to stable roles
// and descriptor-selected presentation assets.
type DeathBehaviorInput struct {
	ActorRole            Role
	TargetRole           Role
	Damage               float32
	AnimationName        string
	GraphicsState        uint32
	PositionedEffectName string
	Position             Position
	FadeEffectName       string
	IsCritical           bool
	IsFastCriticalDeath  bool
	IsPlayerControlled   bool
	IsBoss               bool
	IsRemnantRetained    bool
	CorpseFadeDelay      time.Duration
	DeleteDelay          time.Duration
	DamageProvenance     Provenance
	BehaviorProvenance   Provenance
}

// DeathBehavior owns one cancellable Behavior_Death lifetime. It starts no
// goroutine and advances only with its parent deterministic Simulator.
type DeathBehavior struct {
	simulator      *Simulator
	scope          CancelScope
	input          DeathBehaviorInput
	pendingTask    TaskID
	isActive       bool
	isEffectActive bool
	isCorpseFading bool
	isMarkedDelete bool
}

// StartDeathBehavior emits the killing combat boundary and schedules the
// descriptor-selected Behavior_Death branch.
func StartDeathBehavior(
	simulator *Simulator, scope CancelScope, input DeathBehaviorInput,
) (*DeathBehavior, error) {
	if simulator == nil {
		return nil, errors.New("nil simulator")
	}
	if input.ActorRole == "" || input.TargetRole == "" || input.Damage <= 0 || input.AnimationName == "" {
		return nil, fmt.Errorf("death input: %#v", input)
	}
	isOrdinaryNPC := !input.IsPlayerControlled &&
		(!input.IsFastCriticalDeath || input.IsBoss)
	if isOrdinaryNPC && input.FadeEffectName == "" {
		return nil, errors.New("fade effect missing")
	}
	if input.IsRemnantRetained && input.DeleteDelay <= 0 {
		return nil, errors.New("retained remnant delay missing")
	}
	if !simulator.isScopeActive(scope) {
		return nil, errors.New("inactive scope")
	}
	behavior := &DeathBehavior{simulator: simulator, scope: scope, input: input, isActive: true}
	err := behavior.emitEntry()
	if err != nil {
		return nil, fmt.Errorf("entryEmit: %w", err)
	}
	err = behavior.scheduleBranch()
	if err != nil {
		return nil, fmt.Errorf("branchSchedule: %w", err)
	}
	return behavior, nil
}

func (b *DeathBehavior) emitEntry() error {
	intents := []Intent{
		DamageIntent{
			ActorRole: b.input.ActorRole, TargetRole: b.input.TargetRole,
			DeltaHealth: b.input.Damage, IntegerHitPointChange: -int32(b.input.Damage),
			IsKilling: true, IsCritical: b.input.IsCritical,
		},
		HitPointIntent{Role: b.input.TargetRole, HitPoints: 0},
		DeathStateIntent{Role: b.input.TargetRole, Action: DeathTargetCleared},
		AnimationIntent{Role: b.input.TargetRole, AnimationName: b.input.AnimationName},
		AttributeModifierIntent{
			Role: b.input.TargetRole, AttributeKind: AttributeImmobilized, Amount: 1,
		},
		PhysicsStateIntent{
			Role: b.input.TargetRole, CollisionKind: CollisionStatePhysics,
			IsCollisionEnabled: false,
		},
		PhysicsStateIntent{
			Role: b.input.TargetRole, CollisionKind: CollisionStateNavigation,
			IsCollisionEnabled: false,
		},
	}
	if b.input.GraphicsState != 0 {
		intents = append(intents, GraphicsStateIntent{
			Role: b.input.TargetRole, State: b.input.GraphicsState,
		})
	}
	if b.input.PositionedEffectName != "" {
		intents = append(intents, PositionedEffectIntent{
			EffectName: b.input.PositionedEffectName,
			Position:   b.input.Position, IsFacingOmitted: true,
		})
	}
	for index, intent := range intents {
		provenance := b.input.BehaviorProvenance
		if index < 2 {
			provenance = b.input.DamageProvenance
		}
		err := b.simulator.EmitScoped(intent, provenance, b.scope)
		if err != nil {
			return fmt.Errorf("intentEmit[%d]: %w", index, err)
		}
	}
	return nil
}

func (b *DeathBehavior) scheduleBranch() error {
	if b.input.IsFastCriticalDeath && !b.input.IsBoss {
		err := b.setCorpseFading()
		if err != nil {
			return fmt.Errorf("criticalFading: %w", err)
		}
		err = b.emit(WaitIntent{Duration: criticalDeathSettleDelay})
		if err != nil {
			return fmt.Errorf("criticalWaitEmit: %w", err)
		}
		return b.schedule(criticalDeathSettleDelay, b.finishCriticalSettle)
	}
	err := b.emit(LocomotionStopIntent{Role: b.input.TargetRole})
	if err != nil {
		return fmt.Errorf("locomotionStop: %w", err)
	}
	if b.input.IsPlayerControlled {
		return b.emit(DeathStateIntent{Role: b.input.TargetRole, Action: DeathWaitingForRevival})
	}
	if b.input.IsRemnantRetained {
		err = b.emit(WaitIntent{Duration: b.input.DeleteDelay})
		if err != nil {
			return fmt.Errorf("remnantWaitEmit: %w", err)
		}
		return b.schedule(b.input.DeleteDelay, b.finishRetainedRemnant)
	}
	if b.input.DeleteDelay > 0 {
		err = b.emit(WaitIntent{Duration: b.input.DeleteDelay})
		if err != nil {
			return fmt.Errorf("deleteWaitEmit: %w", err)
		}
		return b.schedule(b.input.DeleteDelay, b.markForDeletion)
	}
	corpseFadeDelay := ordinaryDeathRevivalWindow
	if b.input.CorpseFadeDelay > corpseFadeDelay {
		corpseFadeDelay = b.input.CorpseFadeDelay
	}
	err = b.emit(WaitIntent{Duration: corpseFadeDelay})
	if err != nil {
		return fmt.Errorf("revivalWaitEmit: %w", err)
	}
	return b.schedule(corpseFadeDelay, b.finishRevivalWindow)
}

func (b *DeathBehavior) finishRetainedRemnant() error {
	if !b.isActive {
		return nil
	}
	b.pendingTask = 0
	b.isActive = false
	return nil
}

func (b *DeathBehavior) finishRevivalWindow() error {
	if !b.isActive {
		return nil
	}
	err := b.setCorpseFading()
	if err != nil {
		return fmt.Errorf("corpseFading: %w", err)
	}
	err = b.emit(EffectIntent{
		Role: b.input.TargetRole, EffectName: b.input.FadeEffectName, Slot: 1,
	})
	if err != nil {
		return fmt.Errorf("fadeEffect: %w", err)
	}
	b.isEffectActive = true
	err = b.emit(WaitIntent{Duration: ordinaryDeathFadeDuration})
	if err != nil {
		return fmt.Errorf("fadeWaitEmit: %w", err)
	}
	return b.schedule(ordinaryDeathFadeDuration, b.markForDeletion)
}

func (b *DeathBehavior) finishCriticalSettle() error {
	if !b.isActive {
		return nil
	}
	err := b.emit(LocomotionStopIntent{Role: b.input.TargetRole})
	if err != nil {
		return fmt.Errorf("locomotionStop: %w", err)
	}
	err = b.emit(WaitIntent{Duration: criticalDeathDeleteDelay})
	if err != nil {
		return fmt.Errorf("deleteWaitEmit: %w", err)
	}
	return b.schedule(criticalDeathDeleteDelay, b.markForDeletion)
}

func (b *DeathBehavior) setCorpseFading() error {
	err := b.emit(DeathStateIntent{Role: b.input.TargetRole, Action: DeathCorpseFading})
	if err != nil {
		return fmt.Errorf("stateEmit: %w", err)
	}
	b.isCorpseFading = true
	return nil
}

func (b *DeathBehavior) markForDeletion() error {
	if !b.isActive {
		return nil
	}
	err := b.emit(DeathStateIntent{Role: b.input.TargetRole, Action: DeathMarkedForDeletion})
	if err != nil {
		return fmt.Errorf("stateEmit: %w", err)
	}
	b.pendingTask = 0
	b.isMarkedDelete = true
	b.isActive = false
	return nil
}

// Revive cancels pending deletion and emits Behavior_Death cleanup. It returns
// false after the object has already been marked for deletion.
func (b *DeathBehavior) Revive() (bool, error) {
	if b == nil || b.simulator == nil {
		return false, errors.New("nil death behavior")
	}
	if !b.isActive || b.isMarkedDelete {
		return false, nil
	}
	b.simulator.Cancel(b.pendingTask)
	b.pendingTask = 0
	err := b.cleanup()
	if err != nil {
		return false, fmt.Errorf("cleanup: %w", err)
	}
	b.isActive = false
	return true, nil
}

// Cancel applies the same authored cleanup as an interrupted behavior.
func (b *DeathBehavior) Cancel() error {
	if b == nil || b.simulator == nil || !b.isActive {
		return nil
	}
	_, err := b.Revive()
	if err != nil {
		return fmt.Errorf("revive: %w", err)
	}
	return nil
}

func (b *DeathBehavior) cleanup() error {
	intents := []Intent{
		AttributeModifierIntent{
			Role: b.input.TargetRole, AttributeKind: AttributeImmobilized, Amount: -1,
		},
		AnimationResetIntent{Role: b.input.TargetRole},
		DeathStateIntent{Role: b.input.TargetRole, Action: DeathTargetRestored},
	}
	if b.isEffectActive {
		intents = append(intents, EffectIntent{
			Role: b.input.TargetRole, EffectName: b.input.FadeEffectName, Slot: 1, IsStopped: true,
		})
	}
	if b.isCorpseFading {
		intents = append(intents, DeathStateIntent{Role: b.input.TargetRole, Action: DeathCorpseRestored})
	}
	intents = append(intents,
		PhysicsStateIntent{
			Role: b.input.TargetRole, CollisionKind: CollisionStatePhysics,
			IsCollisionEnabled: true,
		},
		PhysicsStateIntent{
			Role: b.input.TargetRole, CollisionKind: CollisionStateNavigation,
			IsCollisionEnabled: true,
		},
	)
	for index, intent := range intents {
		err := b.emit(intent)
		if err != nil {
			return fmt.Errorf("intentEmit[%d]: %w", index, err)
		}
	}
	b.isEffectActive = false
	b.isCorpseFading = false
	return nil
}

func (b *DeathBehavior) emit(intent Intent) error {
	err := b.simulator.EmitScoped(intent, b.input.BehaviorProvenance, b.scope)
	if err != nil {
		return fmt.Errorf("emitScoped: %w", err)
	}
	return nil
}

func (b *DeathBehavior) schedule(delay time.Duration, callback func() error) error {
	taskID, err := b.simulator.Schedule(delay, b.scope, func(*Simulator) error {
		b.pendingTask = 0
		err := callback()
		if err != nil {
			return fmt.Errorf("deathContinue: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("taskSchedule: %w", err)
	}
	b.pendingTask = taskID
	return nil
}
