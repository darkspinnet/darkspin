package sim

import (
	"errors"
	"fmt"
	"math"
	"time"
)

type PoisonCloudInput struct {
	ActorRole                   Role
	TargetRole                  Role
	ProjectileRole              Role
	AbilityName                 string
	AnimationName               string
	ProjectileNoun              string
	TrailEffectName             string
	ProjectileEffectName        string
	MuzzleEffectName            string
	ImpactEffectName            string
	ActorPosition               Position
	TargetPosition              Position
	ActorFacing                 Position
	FootprintRadius             float32
	ShotDelay                   time.Duration
	ReleaseDelay                time.Duration
	Cooldown                    time.Duration
	ProjectileSpeed             float32
	ProjectileAcceleration      float32
	ProjectileDistance          float32
	CollisionDelay              time.Duration
	HomingDelay                 time.Duration
	IsHoming                    bool
	IsCollisionExternallyDriven bool
	ImpactPosition              Position
	Damage                      float32
	IsCritical                  bool
	IsControl                   bool
	TargetHitPoint              float32
	RangeIncrease               float32
	IsDirectHit                 bool
	IsTargetValidAtShot         bool
	IsTargetValidAtImpact       bool
	SplashTargets               []PoisonCloudTarget
	PreviouslyHitRoles          []Role
	IsPiercing                  bool
	IsActivationSuppressed      bool
	IsTurnSuppressed            bool
	IsCooldownSuppressed        bool
	IsReleaseSuppressed         bool
	Provenance                  Provenance
}

type PoisonCloudTarget struct {
	Role       Role
	Damage     float32
	HitPoint   float32
	IsValid    bool
	IsCritical bool
}

type ProjectileHitTracker struct {
	hitRoles map[Role]struct{}
}

func (t *ProjectileHitTracker) Accept(role Role) bool {
	if role == "" {
		return false
	}
	if t.hitRoles == nil {
		t.hitRoles = make(map[Role]struct{})
	}
	if _, isFound := t.hitRoles[role]; isFound {
		return false
	}
	t.hitRoles[role] = struct{}{}
	return true
}

type PoisonCloudBehavior struct {
	simulator       *Simulator
	scope           CancelScope
	input           PoisonCloudInput
	tasks           []TaskID
	isProjectile    bool
	isImpactStarted bool
	isComplete      bool
	isCanceled      bool
	flightTask      TaskID
	hitTracker      ProjectileHitTracker
}

func (b *PoisonCloudBehavior) IsProjectileActive() bool {
	return b != nil && b.isProjectile && !b.isComplete && !b.isCanceled
}

func StartPoisonCloud(simulator *Simulator, scope CancelScope, input PoisonCloudInput) (*PoisonCloudBehavior, error) {
	if simulator == nil {
		return nil, errors.New("nil simulator")
	}
	if input.ActorRole == "" || input.TargetRole == "" || input.ProjectileRole == "" ||
		input.AbilityName == "" ||
		(!input.IsActivationSuppressed && input.AnimationName == "") ||
		input.ProjectileNoun == "" ||
		input.TrailEffectName == "" || input.ImpactEffectName == "" ||
		(input.Damage <= 0 && !input.IsControl) ||
		input.FootprintRadius < 0 || input.ShotDelay < 0 ||
		(!input.IsReleaseSuppressed && input.ReleaseDelay < input.ShotDelay) ||
		input.Cooldown < 0 || input.ProjectileSpeed <= 0 ||
		input.ProjectileAcceleration < 0 || input.ProjectileDistance <= 0 ||
		input.CollisionDelay < 0 || input.RangeIncrease < 0 {
		return nil, fmt.Errorf("poison input: %#v", input)
	}
	if input.IsHoming && input.HomingDelay <= 0 {
		return nil, errors.New("homing delay invalid")
	}
	for index, target := range input.SplashTargets {
		if target.Role == "" || target.Damage < 0 || target.HitPoint < 0 {
			return nil, fmt.Errorf("splashTarget[%d]: %#v", index, target)
		}
	}
	if !isFinitePosition(input.ActorPosition) || !isFinitePosition(input.TargetPosition) ||
		!isFinitePosition(input.ActorFacing) || !isFinitePosition(input.ImpactPosition) {
		return nil, errors.New("non-finite poison input")
	}
	if !simulator.isScopeActive(scope) {
		return nil, errors.New("inactive scope")
	}
	facing, err := normalizePosition(input.ActorFacing)
	if err != nil {
		return nil, fmt.Errorf("facingNormalize: %w", err)
	}
	input.ActorFacing = facing
	behavior := &PoisonCloudBehavior{simulator: simulator, scope: scope, input: input}
	for _, role := range input.PreviouslyHitRoles {
		behavior.hitTracker.Accept(role)
	}
	if !input.IsActivationSuppressed && !input.IsTurnSuppressed {
		err = behavior.emit(LocomotionStopIntent{
			Role: input.ActorRole, Facing: input.ActorFacing,
			TargetPosition: input.TargetPosition, IsTurn: true,
		})
		if err != nil {
			return nil, fmt.Errorf("stopEmit: %w", err)
		}
	}
	if !input.IsActivationSuppressed {
		err = behavior.emit(AnimationIntent{
			Role: input.ActorRole, AnimationName: input.AnimationName,
		})
		if err != nil {
			return nil, fmt.Errorf("animationEmit: %w", err)
		}
	}
	err = behavior.emit(TargetValidationIntent{
		ActorRole: input.ActorRole, TargetRole: input.TargetRole, Stage: "activation", IsValid: true,
	})
	if err != nil {
		return nil, fmt.Errorf("activationValidate: %w", err)
	}
	err = behavior.schedule(input.ShotDelay, behavior.shot)
	if err != nil {
		return nil, fmt.Errorf("shotSchedule: %w", err)
	}
	err = behavior.schedule(input.ReleaseDelay, behavior.release)
	if err != nil {
		return nil, fmt.Errorf("releaseSchedule: %w", err)
	}
	return behavior, nil
}

func (b *PoisonCloudBehavior) shot() error {
	launchPosition := addPosition(b.input.ActorPosition, scalePosition(b.input.ActorFacing, b.input.FootprintRadius))
	direction, err := normalizePosition(subtractPosition(b.input.TargetPosition, launchPosition))
	if err != nil {
		return fmt.Errorf("directionNormalize: %w", err)
	}
	rangeScale := 1 + b.input.RangeIncrease
	distance := b.input.ProjectileDistance * rangeScale * rangeScale
	targetRole := Role("")
	if b.input.IsTargetValidAtShot {
		targetRole = b.input.TargetRole
	}
	intents := make([]Intent, 0, 4)
	if b.input.MuzzleEffectName != "" {
		intents = append(intents, EffectIntent{
			Role: b.input.ActorRole, EffectName: b.input.MuzzleEffectName,
		})
	}
	if !b.input.IsCooldownSuppressed {
		intents = append(intents, CooldownIntent{
			Role: b.input.ActorRole, AbilityName: b.input.AbilityName,
			Duration: b.input.Cooldown,
		})
	}
	intents = append(intents, ProjectileLaunchIntent{
		Role: b.input.ProjectileRole, ActorRole: b.input.ActorRole,
		TargetRole: targetRole, NounName: b.input.ProjectileNoun,
		Position: launchPosition, Direction: direction, Speed: b.input.ProjectileSpeed,
		Acceleration: b.input.ProjectileAcceleration,
		Distance:     distance, RangeIncrease: b.input.RangeIncrease, IsPiercing: b.input.IsPiercing,
		IsHoming:                b.input.IsHoming,
		IsOrientationRecomputed: true,
	}, EffectIntent{
		Role: b.input.ProjectileRole, EffectName: b.input.TrailEffectName,
	})
	if b.input.ProjectileEffectName != "" {
		intents = append(intents, EffectIntent{
			Role: b.input.ProjectileRole, EffectName: b.input.ProjectileEffectName,
		})
	}
	intents = append(intents, ProjectileMotionIntent{
		Role: b.input.ProjectileRole, TargetRole: targetRole,
		Direction: direction, Speed: b.input.ProjectileSpeed,
		Acceleration: b.input.ProjectileAcceleration,
		Distance:     distance, RangeIncrease: b.input.RangeIncrease,
		HomingDelay: b.input.HomingDelay, IsHoming: b.input.IsHoming,
	})
	for index, intent := range intents {
		err = b.emit(intent)
		if err != nil {
			return fmt.Errorf("shotEmit[%d]: %w", index, err)
		}
	}
	b.isProjectile = true
	if b.input.IsCollisionExternallyDriven {
		return nil
	}
	flightDelay := b.input.CollisionDelay
	if !b.input.IsDirectHit {
		flightDelay = time.Duration(float64(time.Second) * float64(distance/b.input.ProjectileSpeed))
	}
	err = b.emit(WaitIntent{Duration: flightDelay})
	if err != nil {
		return fmt.Errorf("flightWait: %w", err)
	}
	err = b.schedule(flightDelay, b.impact)
	if err != nil {
		return fmt.Errorf("impactSchedule: %w", err)
	}
	b.flightTask = b.tasks[len(b.tasks)-1]
	return nil
}

func (b *PoisonCloudBehavior) release() error {
	if b.input.IsReleaseSuppressed {
		return nil
	}
	return b.emit(AbilityReleaseIntent{Role: b.input.ActorRole, AbilityName: b.input.AbilityName})
}

func (b *PoisonCloudBehavior) impact() error {
	if !b.isProjectile || b.isComplete {
		return nil
	}
	b.isImpactStarted = true
	position := b.input.ImpactPosition
	if !b.input.IsDirectHit && !b.input.IsCollisionExternallyDriven {
		launchPosition := addPosition(b.input.ActorPosition,
			scalePosition(b.input.ActorFacing, b.input.FootprintRadius))
		direction, err := normalizePosition(subtractPosition(b.input.TargetPosition, launchPosition))
		if err != nil {
			return fmt.Errorf("missDirection: %w", err)
		}
		rangeScale := 1 + b.input.RangeIncrease
		position = addPosition(launchPosition,
			scalePosition(direction, b.input.ProjectileDistance*rangeScale*rangeScale))
	}
	err := b.emit(TargetValidationIntent{
		ActorRole: b.input.ActorRole, TargetRole: b.input.TargetRole,
		Stage: "impact", IsValid: b.input.IsTargetValidAtImpact,
	})
	if err != nil {
		return fmt.Errorf("impactValidate: %w", err)
	}
	targetRole := Role("")
	if b.input.IsDirectHit && b.input.IsTargetValidAtImpact {
		targetRole = b.input.TargetRole
	}
	err = b.emit(ProjectileImpactIntent{
		Role: b.input.ProjectileRole, TargetRole: targetRole, Position: position,
		Facing: b.input.ActorFacing, IsDirect: targetRole != "",
	})
	if err != nil {
		return fmt.Errorf("impactEmit: %w", err)
	}
	err = b.emit(PositionedEffectIntent{
		EffectName: b.input.ImpactEffectName,
		Position:   position, Facing: b.input.ActorFacing,
	})
	if err != nil {
		return fmt.Errorf("impactEffect: %w", err)
	}
	targets := b.input.SplashTargets
	if len(targets) == 0 && targetRole != "" {
		targets = []PoisonCloudTarget{{
			Role: targetRole, Damage: b.input.Damage, HitPoint: b.input.TargetHitPoint, IsValid: true,
		}}
	}
	for index, target := range targets {
		err = b.emit(TargetValidationIntent{
			ActorRole: b.input.ActorRole, TargetRole: target.Role,
			Stage: "splash", IsValid: target.IsValid,
		})
		if err != nil {
			return fmt.Errorf("splashValidate[%d]: %w", index, err)
		}
		if !target.IsValid || !b.hitTracker.Accept(target.Role) {
			continue
		}
		damage := target.Damage
		if damage == 0 {
			damage = b.input.Damage
		}
		isCritical := target.IsCritical
		if len(b.input.SplashTargets) == 0 {
			isCritical = b.input.IsCritical
		}
		err = b.emit(PositionedEffectIntent{
			EffectName: b.input.ImpactEffectName,
			Position:   position, Facing: b.input.ActorFacing,
		})
		if err != nil {
			return fmt.Errorf("hitEffect[%d]: %w", index, err)
		}
		if !b.input.IsControl {
			err = b.emit(DamageIntent{
				ActorRole: b.input.ActorRole, TargetRole: target.Role,
				DeltaHealth: damage, IntegerHitPointChange: -int32(damage),
				IsCritical: isCritical,
			})
			if err != nil {
				return fmt.Errorf("damageEmit[%d]: %w", index, err)
			}
			err = b.emit(HitPointIntent{
				Role: target.Role, HitPoints: max(0, target.HitPoint-damage),
			})
			if err != nil {
				return fmt.Errorf("hitPointEmit[%d]: %w", index, err)
			}
		}
	}
	err = b.emit(DespawnIntent{Role: b.input.ProjectileRole})
	if err != nil {
		return fmt.Errorf("despawnEmit: %w", err)
	}
	b.isProjectile = false
	b.isComplete = true
	return nil
}

// PrepareImpact refreshes collision-time authority before the projectile
// continuation runs. This lets movement, death, and earlier same-deadline
// damage update the target snapshot without changing the authored flight.
func (b *PoisonCloudBehavior) PrepareImpact(
	isTargetValid bool, targetHitPoint float32, damage float32,
	impactPosition Position, facing Position,
) error {
	return b.PrepareCollision(
		b.input.IsDirectHit, isTargetValid, targetHitPoint, damage,
		b.input.IsCritical, impactPosition, facing,
	)
}

// PrepareCollision injects the latest authoritative physics and target state
// before an externally driven WaitForProjectile continuation is resumed.
func (b *PoisonCloudBehavior) PrepareCollision(
	isDirectHit bool, isTargetValid bool, targetHitPoint float32, damage float32,
	isCritical bool, impactPosition Position, facing Position,
) error {
	if b == nil || b.simulator == nil {
		return errors.New("nil poison behavior")
	}
	impactDeadline := b.input.ShotDelay + b.input.CollisionDelay
	if b.isCanceled || b.isComplete || b.isImpactStarted ||
		(!b.input.IsCollisionExternallyDriven && b.simulator.Now() >= impactDeadline) {
		return errors.New("poison impact already started")
	}
	if targetHitPoint < 0 || damage < 0 ||
		!isFinitePosition(impactPosition) || !isFinitePosition(facing) {
		return errors.New("invalid poison impact state")
	}
	normalizedFacing, err := normalizePosition(facing)
	if err != nil {
		return fmt.Errorf("facingNormalize: %w", err)
	}
	b.input.IsTargetValidAtImpact = isTargetValid
	b.input.IsDirectHit = isDirectHit
	b.input.TargetHitPoint = targetHitPoint
	b.input.Damage = damage
	b.input.IsCritical = isCritical
	b.input.ImpactPosition = impactPosition
	b.input.ActorFacing = normalizedFacing
	return nil
}

// ResolveCollision resumes an externally driven projectile wait at the
// simulator's current clock time.
func (b *PoisonCloudBehavior) ResolveCollision() error {
	if b == nil || b.simulator == nil {
		return errors.New("nil poison behavior")
	}
	if !b.input.IsCollisionExternallyDriven {
		return errors.New("poison collision is internally scheduled")
	}
	err := b.impact()
	if err != nil {
		return fmt.Errorf("impactResolve: %w", err)
	}
	return nil
}

func (b *PoisonCloudBehavior) DeleteProjectile() (bool, error) {
	if b == nil || b.simulator == nil {
		return false, errors.New("nil poison behavior")
	}
	if !b.isProjectile || b.isComplete || b.isCanceled {
		return false, nil
	}
	b.simulator.Cancel(b.flightTask)
	b.flightTask = 0
	err := b.emit(DespawnIntent{Role: b.input.ProjectileRole})
	if err != nil {
		return false, fmt.Errorf("despawnEmit: %w", err)
	}
	b.isProjectile = false
	b.isComplete = true
	return true, nil
}

func (b *PoisonCloudBehavior) Cancel() error {
	if b == nil || b.simulator == nil || b.isCanceled {
		return nil
	}
	for _, taskID := range b.tasks {
		b.simulator.Cancel(taskID)
	}
	b.tasks = nil
	if b.isProjectile {
		err := b.emit(DespawnIntent{Role: b.input.ProjectileRole})
		if err != nil {
			return fmt.Errorf("despawnEmit: %w", err)
		}
		b.isProjectile = false
	}
	b.simulator.InvalidateRole(b.input.ProjectileRole)
	b.isCanceled = true
	return nil
}

func (b *PoisonCloudBehavior) emit(intent Intent) error {
	err := b.simulator.EmitScoped(intent, b.input.Provenance, b.scope)
	if err != nil {
		return fmt.Errorf("emitScoped: %w", err)
	}
	return nil
}

func (b *PoisonCloudBehavior) schedule(delay time.Duration, callback func() error) error {
	taskID, err := b.simulator.Schedule(delay, b.scope, func(*Simulator) error {
		err := callback()
		if err != nil {
			return fmt.Errorf("poisonContinue: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("taskSchedule: %w", err)
	}
	b.tasks = append(b.tasks, taskID)
	return nil
}

func normalizePosition(position Position) (Position, error) {
	length := math.Sqrt(float64(position.X*position.X + position.Y*position.Y + position.Z*position.Z))
	if length == 0 || math.IsNaN(length) || math.IsInf(length, 0) {
		return Position{}, errors.New("zero direction")
	}
	scale := float32(1 / length)
	return scalePosition(position, scale), nil
}

func addPosition(left, right Position) Position {
	return Position{X: left.X + right.X, Y: left.Y + right.Y, Z: left.Z + right.Z}
}

func subtractPosition(left, right Position) Position {
	return Position{X: left.X - right.X, Y: left.Y - right.Y, Z: left.Z - right.Z}
}

func scalePosition(position Position, scale float32) Position {
	return Position{X: position.X * scale, Y: position.Y * scale, Z: position.Z * scale}
}
