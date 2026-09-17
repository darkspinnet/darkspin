package sim

import (
	"errors"
	"fmt"
	"time"
)

// ProjectileBurstInput is one authored activation with independently owned
// projectile launches. ShotDelays are cumulative activation-time deadlines.
type ProjectileBurstInput struct {
	ActorRole             Role
	TargetRole            Role
	ProjectileRoles       []Role
	AbilityName           string
	AnimationName         string
	ProjectileNoun        string
	MuzzleEffectName      string
	TrailEffectName       string
	ImpactEffectName      string
	MissEffectName        string
	ActorPosition         Position
	ActorFacing           Position
	TargetPosition        Position
	FootprintRadius       float32
	ShotDelays            []time.Duration
	ReleaseDelay          time.Duration
	Cooldown              time.Duration
	ProjectileSpeed       float32
	ProjectileDistance    float32
	Damage                float32
	TargetHitPoint        float32
	IsTargetValid         bool
	IsImpactFacingOmitted bool
	Provenance            Provenance
}

type projectileBurstShot struct {
	role           Role
	actorPosition  Position
	targetPosition Position
	actorFacing    Position
	isTargetValid  bool
	isLaunched     bool
	isResolved     bool
}

type ProjectileBurstBehavior struct {
	simulator  *Simulator
	scope      CancelScope
	input      ProjectileBurstInput
	shots      []projectileBurstShot
	tasks      []TaskID
	isReleased bool
	isCanceled bool
}

// ProjectileBurstShotSnapshot is one active burst projectile projected at a
// wall-clock offset from the activation boundary.
type ProjectileBurstShotSnapshot struct {
	Index             int
	Position          Position
	Direction         Position
	TargetPosition    Position
	RemainingDistance float32
	IsTargetValid     bool
	IsActive          bool
}

// Snapshots projects every launched, unresolved shot without advancing or
// mutating the retained simulator.
func (b *ProjectileBurstBehavior) Snapshots(
	elapsed time.Duration,
) []ProjectileBurstShotSnapshot {
	if b == nil || elapsed < 0 || b.isCanceled {
		return nil
	}
	snapshots := make([]ProjectileBurstShotSnapshot, 0, len(b.shots))
	for index, shot := range b.shots {
		if !shot.isLaunched || shot.isResolved {
			continue
		}
		launchPosition := addPosition(
			shot.actorPosition,
			scalePosition(shot.actorFacing, b.input.FootprintRadius),
		)
		direction, err := normalizePosition(
			subtractPosition(shot.targetPosition, launchPosition),
		)
		if err != nil {
			continue
		}
		flightDuration := elapsed - b.input.ShotDelays[index]
		if flightDuration < 0 {
			flightDuration = 0
		}
		travelDistance := b.input.ProjectileSpeed * float32(flightDuration.Seconds())
		travelDistance = min(b.input.ProjectileDistance, max(float32(0), travelDistance))
		position := addPosition(
			launchPosition, scalePosition(direction, travelDistance),
		)
		snapshots = append(snapshots, ProjectileBurstShotSnapshot{
			Index: index, Position: position, Direction: direction,
			TargetPosition:    shot.targetPosition,
			RemainingDistance: b.input.ProjectileDistance - travelDistance,
			IsTargetValid:     shot.isTargetValid, IsActive: true,
		})
	}
	return snapshots
}

func StartProjectileBurst(
	simulator *Simulator, scope CancelScope, input ProjectileBurstInput,
) (*ProjectileBurstBehavior, error) {
	if simulator == nil {
		return nil, errors.New("nil simulator")
	}
	if input.ActorRole == "" || input.TargetRole == "" || input.AbilityName == "" ||
		input.AnimationName == "" || input.ProjectileNoun == "" ||
		input.ImpactEffectName == "" || len(input.ShotDelays) == 0 ||
		len(input.ProjectileRoles) != len(input.ShotDelays) || input.FootprintRadius < 0 ||
		input.ReleaseDelay < 0 || input.Cooldown < 0 || input.ProjectileSpeed <= 0 ||
		input.ProjectileDistance <= 0 || input.Damage <= 0 || input.TargetHitPoint < 0 {
		return nil, fmt.Errorf("burst input: %#v", input)
	}
	if !isFinitePosition(input.ActorPosition) || !isFinitePosition(input.ActorFacing) ||
		!isFinitePosition(input.TargetPosition) {
		return nil, errors.New("non-finite burst input")
	}
	if !simulator.isScopeActive(scope) {
		return nil, errors.New("inactive scope")
	}
	previousDelay := time.Duration(-1)
	for index, delay := range input.ShotDelays {
		if input.ProjectileRoles[index] == "" || delay < previousDelay || delay > input.ReleaseDelay {
			return nil, fmt.Errorf("shot[%d]: role=%q delay=%s", index, input.ProjectileRoles[index], delay)
		}
		previousDelay = delay
	}
	facing, err := normalizePosition(input.ActorFacing)
	if err != nil {
		return nil, fmt.Errorf("facingNormalize: %w", err)
	}
	input.ActorFacing = facing
	behavior := &ProjectileBurstBehavior{
		simulator: simulator, scope: scope, input: input,
		shots: make([]projectileBurstShot, len(input.ShotDelays)),
	}
	for index, role := range input.ProjectileRoles {
		behavior.shots[index] = projectileBurstShot{
			role: role, actorPosition: input.ActorPosition,
			targetPosition: input.TargetPosition, actorFacing: facing,
			isTargetValid: input.IsTargetValid,
		}
	}
	err = behavior.emit(AnimationIntent{Role: input.ActorRole, AnimationName: input.AnimationName})
	if err != nil {
		return nil, fmt.Errorf("animationEmit: %w", err)
	}
	err = behavior.emit(TargetValidationIntent{
		ActorRole: input.ActorRole, TargetRole: input.TargetRole,
		Stage: "activation", IsValid: input.IsTargetValid,
	})
	if err != nil {
		return nil, fmt.Errorf("activationValidate: %w", err)
	}
	for index, delay := range input.ShotDelays {
		shotIndex := index
		err = behavior.schedule(delay, func() error { return behavior.launch(shotIndex) })
		if err != nil {
			return nil, fmt.Errorf("launchSchedule[%d]: %w", index, err)
		}
	}
	err = behavior.schedule(input.ReleaseDelay, behavior.release)
	if err != nil {
		return nil, fmt.Errorf("releaseSchedule: %w", err)
	}
	return behavior, nil
}

// PrepareLaunch refreshes target position and caster facing before a launch
// continuation. Already launched shots retain their original direction.
func (b *ProjectileBurstBehavior) PrepareLaunch(
	index int, actorPosition Position, targetPosition Position, actorFacing Position, isTargetValid bool,
) error {
	if b == nil || b.simulator == nil || index < 0 || index >= len(b.shots) {
		return errors.New("invalid burst launch")
	}
	if b.shots[index].isLaunched || b.isCanceled || !isFinitePosition(actorPosition) ||
		!isFinitePosition(targetPosition) ||
		!isFinitePosition(actorFacing) {
		return errors.New("burst launch unavailable")
	}
	facing, err := normalizePosition(actorFacing)
	if err != nil {
		return fmt.Errorf("facingNormalize: %w", err)
	}
	b.shots[index].actorPosition = actorPosition
	b.shots[index].targetPosition = targetPosition
	b.shots[index].actorFacing = facing
	b.shots[index].isTargetValid = isTargetValid
	return nil
}

func (b *ProjectileBurstBehavior) launch(index int) error {
	shot := &b.shots[index]
	launchPosition := addPosition(shot.actorPosition,
		scalePosition(shot.actorFacing, b.input.FootprintRadius))
	direction, err := normalizePosition(subtractPosition(shot.targetPosition, launchPosition))
	if err != nil {
		return fmt.Errorf("directionNormalize[%d]: %w", index, err)
	}
	err = b.emit(TargetValidationIntent{
		ActorRole: b.input.ActorRole, TargetRole: b.input.TargetRole,
		Stage: fmt.Sprintf("launch[%d]", index), IsValid: shot.isTargetValid,
	})
	if err != nil {
		return fmt.Errorf("launchValidate[%d]: %w", index, err)
	}
	if index == 0 {
		err = b.emit(CooldownIntent{
			Role: b.input.ActorRole, AbilityName: b.input.AbilityName, Duration: b.input.Cooldown,
		})
		if err != nil {
			return fmt.Errorf("cooldownEmit: %w", err)
		}
	}
	targetRole := Role("")
	if shot.isTargetValid {
		targetRole = b.input.TargetRole
	}
	intent := make([]Intent, 0, 3)
	if b.input.MuzzleEffectName != "" {
		intent = append(intent, EffectIntent{
			Role: b.input.ActorRole, ActorRole: b.input.ActorRole,
			EffectName: b.input.MuzzleEffectName,
		})
	}
	intent = append(intent, ProjectileLaunchIntent{
		Role: shot.role, ActorRole: b.input.ActorRole, TargetRole: targetRole,
		NounName: b.input.ProjectileNoun, Position: launchPosition, Direction: direction,
		Speed: b.input.ProjectileSpeed, Distance: b.input.ProjectileDistance,
		IsOrientationRecomputed: true,
	})
	if b.input.TrailEffectName != "" {
		intent = append(intent, EffectIntent{
			Role: shot.role, EffectName: b.input.TrailEffectName,
		})
	}
	intent = append(intent, ProjectileMotionIntent{
		Role: shot.role, Direction: direction, Speed: b.input.ProjectileSpeed,
		Distance: b.input.ProjectileDistance,
	})
	for intentIndex, emitted := range intent {
		err = b.emit(emitted)
		if err != nil {
			return fmt.Errorf("launchEmit[%d/%d]: %w", index, intentIndex, err)
		}
	}
	shot.isLaunched = true
	return nil
}

// ResolveCollision completes one independently owned projectile at the
// simulator's current deadline.
func (b *ProjectileBurstBehavior) ResolveCollision(
	index int, isDirectHit bool, isTargetValid bool, targetHitPoint float32,
	damage float32, isCritical bool, impactPosition Position, facing Position,
) error {
	if b == nil || b.simulator == nil || index < 0 || index >= len(b.shots) {
		return errors.New("invalid burst collision")
	}
	shot := &b.shots[index]
	if !shot.isLaunched || shot.isResolved || targetHitPoint < 0 || damage <= 0 ||
		!isFinitePosition(impactPosition) || !isFinitePosition(facing) {
		return errors.New("burst collision unavailable")
	}
	normalizedFacing, err := normalizePosition(facing)
	if err != nil {
		return fmt.Errorf("facingNormalize: %w", err)
	}
	err = b.emit(TargetValidationIntent{
		ActorRole: b.input.ActorRole, TargetRole: b.input.TargetRole,
		Stage: fmt.Sprintf("impact[%d]", index), IsValid: isTargetValid,
	})
	if err != nil {
		return fmt.Errorf("impactValidate[%d]: %w", index, err)
	}
	targetRole := Role("")
	if isDirectHit && isTargetValid {
		targetRole = b.input.TargetRole
	}
	err = b.emit(ProjectileImpactIntent{
		Role: shot.role, TargetRole: targetRole, Position: impactPosition,
		Facing: normalizedFacing, IsDirect: targetRole != "",
	})
	if err != nil {
		return fmt.Errorf("impactEmit[%d]: %w", index, err)
	}
	effectName := b.input.MissEffectName
	if isDirectHit {
		effectName = b.input.ImpactEffectName
	}
	if effectName != "" {
		err = b.emit(PositionedEffectIntent{
			EffectName: effectName, Position: impactPosition, Facing: normalizedFacing,
			IsFacingOmitted: b.input.IsImpactFacingOmitted,
		})
		if err != nil {
			return fmt.Errorf("effectEmit[%d]: %w", index, err)
		}
	}
	if targetRole != "" {
		err = b.emit(DamageIntent{
			ActorRole: b.input.ActorRole, TargetRole: targetRole,
			DeltaHealth: damage, IntegerHitPointChange: -int32(damage), IsCritical: isCritical,
		})
		if err != nil {
			return fmt.Errorf("damageEmit[%d]: %w", index, err)
		}
		err = b.emit(HitPointIntent{Role: targetRole, HitPoints: max(0, targetHitPoint-damage)})
		if err != nil {
			return fmt.Errorf("hitPointEmit[%d]: %w", index, err)
		}
	}
	err = b.emit(DespawnIntent{Role: shot.role})
	if err != nil {
		return fmt.Errorf("despawnEmit[%d]: %w", index, err)
	}
	shot.isResolved = true
	return nil
}

func (b *ProjectileBurstBehavior) release() error {
	b.isReleased = true
	return b.emit(AbilityReleaseIntent{Role: b.input.ActorRole, AbilityName: b.input.AbilityName})
}

// ResetActorAnimation releases presentation without canceling projectiles that
// are still traveling or waiting for their authored launch deadline.
func (b *ProjectileBurstBehavior) ResetActorAnimation() error {
	if b == nil || b.simulator == nil || b.isCanceled {
		return errors.New("burst animation unavailable")
	}
	err := b.emit(AnimationResetIntent{Role: b.input.ActorRole})
	if err != nil {
		return fmt.Errorf("animationReset: %w", err)
	}
	return nil
}

func (b *ProjectileBurstBehavior) Cancel() error {
	if b == nil || b.simulator == nil || b.isCanceled {
		return nil
	}
	for _, taskID := range b.tasks {
		b.simulator.Cancel(taskID)
	}
	b.tasks = nil
	for index := range b.shots {
		if !b.shots[index].isLaunched || b.shots[index].isResolved {
			continue
		}
		err := b.emit(DespawnIntent{Role: b.shots[index].role})
		if err != nil {
			return fmt.Errorf("despawnEmit[%d]: %w", index, err)
		}
		b.shots[index].isResolved = true
	}
	b.isCanceled = true
	return nil
}

func (b *ProjectileBurstBehavior) emit(intent Intent) error {
	err := b.simulator.EmitScoped(intent, b.input.Provenance, b.scope)
	if err != nil {
		return fmt.Errorf("emitScoped: %w", err)
	}
	return nil
}

func (b *ProjectileBurstBehavior) schedule(delay time.Duration, callback func() error) error {
	taskID, err := b.simulator.Schedule(delay, b.scope, func(*Simulator) error {
		err := callback()
		if err != nil {
			return fmt.Errorf("burstContinue: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("simSchedule: %w", err)
	}
	b.tasks = append(b.tasks, taskID)
	return nil
}
