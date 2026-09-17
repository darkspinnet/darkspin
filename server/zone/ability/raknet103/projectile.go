package raknet103

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	simraknet103 "github.com/darkspinnet/darkspin/server/sim/raknet103"
)

const poisonCloudPhase sim.Phase = "poisonCloud"
const poisonCloudActorRole sim.Role = "poisonActor"
const poisonCloudTargetRole sim.Role = "poisonTarget"
const poisonCloudProjectileRole sim.Role = "poisonProjectile"

type ProjectileInput struct {
	StartedAt                   time.Time
	Ability                     sim.AbilityDefinition
	ActorObjectID               uint32
	TargetObjectID              uint32
	ProjectileObjectID          uint32
	ActorPosition               sim.Position
	TargetPosition              sim.Position
	ActorFacing                 sim.Position
	ImpactPosition              sim.Position
	FootprintRadius             float32
	CollisionDelay              time.Duration
	HomingDelay                 time.Duration
	IsHoming                    bool
	IsDirectHit                 bool
	IsPiercing                  bool
	IsCollisionExternallyDriven bool
	IsCollisionSampled          bool
	Damage                      float32
	IsCritical                  bool
	IsControl                   bool
	TargetHitPoint              float32
	TargetManaPoint             float32
	ActorTeam                   uint8
	SourceTime                  uint64
	IsActivationSuppressed      bool
	IsTurnSuppressed            bool
	IsCooldownSuppressed        bool
	IsReleaseSuppressed         bool
	ProjectileEffectName        string
}

type ProjectileRun struct {
	resolver           roleResolver
	simulator          *sim.Simulator
	session            *sim.Session
	behavior           *sim.PoisonCloudBehavior
	outbox             *poisonCloudOutbox
	deadlines          []time.Duration
	cancel             raknet.CancelSchedule
	ability            sim.AbilityDefinition
	objectID           uint32
	sourceObjectID     uint32
	targetObjectID     uint32
	launchAt           time.Time
	launchPosition     sim.Position
	targetPosition     sim.Position
	direction          sim.Position
	speed              float32
	acceleration       float32
	distance           float32
	retargetID         uint32
	motionMutex        sync.Mutex
	motionPosition     sim.Position
	motionUpdated      time.Time
	motionRemaining    float32
	motionSpeedScale   float32
	isCollisionSampled bool
	motionSegments     []sim.ProjectileMotionSegment
	frozenUntil        time.Time
	isGravityDeflected bool
	gravityPosition    sim.Position
	gravityDirection   sim.Position
	gravityDistance    float32
}

type ProjectileSnapshot struct {
	ObjectID                uint32
	SourceObjectID          uint32
	TargetObjectID          uint32
	AbilityName             string
	ProjectileNoun          string
	Position                sim.Position
	TargetPosition          sim.Position
	Direction               sim.Position
	Speed                   float32
	RemainingDistance       float32
	RemainingFlightDuration time.Duration
	IsActive                bool
	IsFrozen                bool
	IsGravityDeflected      bool
}

type ProjectileCollisionTrajectory struct {
	Position  sim.Position
	Direction sim.Position
	Distance  float32
}

type poisonCloudOutbox struct {
	*simraknet103.PacketOutbox
}

type poisonCloudObjectOutbox struct {
	owner *poisonCloudOutbox
}

func NewProjectileRun(input ProjectileInput) (*ProjectileRun, [][]byte, error) {
	if input.IsCollisionSampled && !input.IsCollisionExternallyDriven {
		return nil, nil, errors.New("sampled collision requires external resolution")
	}
	if input.ActorObjectID == 0 || input.TargetObjectID == 0 || input.ProjectileObjectID == 0 ||
		(input.Damage <= 0 && !input.IsControl) || input.TargetHitPoint < 0 {
		return nil, nil, errors.New("invalid Poison Cloud input")
	}
	if input.IsHoming && input.HomingDelay <= 0 {
		return nil, nil, errors.New("invalid homing projectile input")
	}
	resolver := roleResolver{
		poisonCloudActorRole: {
			ObjectID: input.ActorObjectID, Position: input.ActorPosition, Team: input.ActorTeam,
		},
		poisonCloudTargetRole: {
			ObjectID: input.TargetObjectID, Position: input.TargetPosition, ManaPoints: input.TargetManaPoint,
		},
		poisonCloudProjectileRole: {
			ObjectID: input.ProjectileObjectID, Orientation: raknet.Quaternion{W: 1},
		},
	}
	encoder, err := simraknet103.NewEncoder(resolver, input.SourceTime)
	if err != nil {
		return nil, nil, fmt.Errorf("encoderCreate: %w", err)
	}
	packetOutbox, err := simraknet103.NewPacketOutbox(encoder)
	if err != nil {
		return nil, nil, fmt.Errorf("outboxCreate: %w", err)
	}
	outbox := &poisonCloudOutbox{PacketOutbox: packetOutbox}
	director, err := sim.NewDirector([]sim.PhaseDefinition{{
		Name: poisonCloudPhase,
		ActiveRoles: []sim.Role{
			poisonCloudActorRole,
			poisonCloudTargetRole,
			poisonCloudProjectileRole,
		},
	}}, poisonCloudPhase)
	if err != nil {
		return nil, nil, fmt.Errorf("directorCreate: %w", err)
	}
	dispatcher := sim.NewDispatcher(sim.Ports{
		Object: poisonCloudObjectOutbox{owner: outbox}, Locomotion: outbox,
		Projectile: outbox, Cooldown: outbox, Damage: outbox,
		Presentation: outbox, PositionedPresentation: outbox,
	})
	session, err := sim.NewSession(director, dispatcher)
	if err != nil {
		return nil, nil, fmt.Errorf("sessionCreate: %w", err)
	}
	simulator := director.Simulator()
	scope := simulator.Scope(poisonCloudProjectileRole)
	behavior, err := sim.StartPoisonCloud(simulator, scope, sim.PoisonCloudInput{
		ActorRole: poisonCloudActorRole, TargetRole: poisonCloudTargetRole,
		ProjectileRole: poisonCloudProjectileRole,
		AbilityName:    input.Ability.Name, AnimationName: input.Ability.AnimationName,
		ProjectileNoun:       input.Ability.ProjectileNoun,
		TrailEffectName:      input.Ability.TrailEffectName,
		ProjectileEffectName: input.ProjectileEffectName,
		MuzzleEffectName:     input.Ability.MuzzleEffectName,
		ImpactEffectName:     input.Ability.ImpactEffectName,
		ActorPosition:        input.ActorPosition, TargetPosition: input.TargetPosition,
		ActorFacing: input.ActorFacing, ImpactPosition: input.ImpactPosition,
		FootprintRadius: input.FootprintRadius, CollisionDelay: input.CollisionDelay,
		HomingDelay: input.HomingDelay, IsHoming: input.IsHoming,
		IsCollisionExternallyDriven: input.IsCollisionExternallyDriven,
		ShotDelay:                   input.Ability.HitDelay, ReleaseDelay: input.Ability.ReleaseDelay,
		Cooldown: input.Ability.Cooldown, ProjectileSpeed: input.Ability.Speed,
		ProjectileAcceleration: input.Ability.Acceleration,
		ProjectileDistance:     input.Ability.Distance,
		Damage:                 input.Damage, TargetHitPoint: input.TargetHitPoint,
		IsControl:   input.IsControl,
		IsCritical:  input.IsCritical,
		IsDirectHit: input.IsDirectHit, IsTargetValidAtShot: true,
		IsPiercing:             input.IsPiercing,
		IsTargetValidAtImpact:  input.IsDirectHit,
		IsActivationSuppressed: input.IsActivationSuppressed,
		IsTurnSuppressed:       input.IsTurnSuppressed,
		IsCooldownSuppressed:   input.IsCooldownSuppressed,
		IsReleaseSuppressed:    input.IsReleaseSuppressed,
		Provenance:             input.Ability.Provenance,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("behaviorStart: %w", err)
	}
	deadlines := []time.Duration{input.Ability.HitDelay, input.Ability.ReleaseDelay}
	if !input.IsCollisionExternallyDriven {
		deadlines = append(deadlines, input.Ability.HitDelay+input.CollisionDelay)
	}
	slices.Sort(deadlines)
	deadlines = slices.Compact(deadlines)
	run := &ProjectileRun{
		resolver:  resolver,
		simulator: simulator, session: session, behavior: behavior, outbox: outbox, deadlines: deadlines,
		ability: input.Ability, objectID: input.ProjectileObjectID,
		sourceObjectID: input.ActorObjectID, targetObjectID: input.TargetObjectID,
	}
	launchPosition := sim.Position{
		X: input.ActorPosition.X + input.ActorFacing.X*input.FootprintRadius,
		Y: input.ActorPosition.Y + input.ActorFacing.Y*input.FootprintRadius,
		Z: input.ActorPosition.Z + input.ActorFacing.Z*input.FootprintRadius,
	}
	delta := sim.Position{
		X: input.TargetPosition.X - launchPosition.X,
		Y: input.TargetPosition.Y - launchPosition.Y,
		Z: input.TargetPosition.Z - launchPosition.Z,
	}
	length := float32(math.Sqrt(float64(
		delta.X*delta.X + delta.Y*delta.Y + delta.Z*delta.Z,
	)))
	if length > 0 {
		run.direction = sim.Position{
			X: delta.X / length, Y: delta.Y / length, Z: delta.Z / length,
		}
	}
	startedAt := input.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	run.launchAt = startedAt.Add(input.Ability.HitDelay)
	run.launchPosition = launchPosition
	run.targetPosition = input.TargetPosition
	run.speed = input.Ability.Speed
	run.acceleration = input.Ability.Acceleration
	run.distance = input.Ability.Distance
	run.motionPosition = launchPosition
	run.motionUpdated = run.launchAt
	run.motionRemaining = input.Ability.Distance
	if !input.IsCollisionSampled && input.CollisionDelay > 0 &&
		(input.Ability.Speed > 0 || input.Ability.Acceleration > 0) {
		duration := float32(input.CollisionDelay.Seconds())
		collisionDistance := input.Ability.Speed*duration +
			0.5*input.Ability.Acceleration*duration*duration
		run.motionRemaining = min(run.motionRemaining, collisionDistance)
	}
	run.motionSpeedScale = 1
	run.isCollisionSampled = input.IsCollisionSampled
	err = session.DispatchPending(context.Background())
	if err != nil {
		return nil, nil, fmt.Errorf("immediateDispatch: %w", err)
	}
	return run, outbox.Drain(), nil
}

// SetImpactTarget binds presentation to the object actually struck, which can
// differ from the selected target when another object intercepts the shot.
func (e *ProjectileRun) SetImpactTarget(objectID uint32, position sim.Position) error {
	if e == nil || e.resolver == nil || objectID == 0 {
		return errors.New("invalid projectile impact target")
	}
	target := e.resolver[poisonCloudTargetRole]
	target.ObjectID = objectID
	target.Position = position
	e.resolver[poisonCloudTargetRole] = target
	return nil
}

func (r *ProjectileRun) Snapshot(now time.Time) ProjectileSnapshot {
	if r == nil || now.IsZero() || r.objectID == 0 || r.behavior == nil ||
		!r.behavior.IsProjectileActive() {
		return ProjectileSnapshot{}
	}
	r.motionMutex.Lock()
	defer r.motionMutex.Unlock()
	return r.snapshotLocked(now)
}

// SampleCollisionMotion drains travel accumulated by all motion operations,
// including intervening gravity, freeze, and speed changes.
func (e *ProjectileRun) SampleCollisionMotion(now time.Time) (ProjectileSnapshot, []sim.ProjectileMotionSegment) {
	if e == nil || now.IsZero() || !e.isCollisionSampled || e.behavior == nil || !e.behavior.IsProjectileActive() {
		return ProjectileSnapshot{}, nil
	}
	e.motionMutex.Lock()
	defer e.motionMutex.Unlock()
	snapshot := e.snapshotLocked(now)
	segments := e.motionSegments
	e.motionSegments = nil
	return snapshot, segments
}

func (r *ProjectileRun) snapshotLocked(now time.Time) ProjectileSnapshot {
	if now.Before(r.launchAt) {
		return ProjectileSnapshot{}
	}
	r.advanceMotionLocked(now)
	position := r.motionPosition
	motionSpeed := r.speed * r.motionSpeedScale
	speed := motionSpeed
	isFrozen := now.Before(r.frozenUntil)
	remainingDuration := time.Duration(0)
	if isFrozen {
		remainingDuration = r.frozenUntil.Sub(now)
		speed = 0
	}
	remainingDuration += r.remainingMotionDurationLocked()
	snapshot := ProjectileSnapshot{
		ObjectID: r.objectID, SourceObjectID: r.sourceObjectID,
		TargetObjectID: r.targetObjectID, AbilityName: r.ability.Name,
		ProjectileNoun: r.ability.ProjectileNoun,
		Position:       position, TargetPosition: r.targetPosition,
		Direction: r.direction, Speed: speed,
		RemainingDistance:       r.motionRemaining,
		RemainingFlightDuration: remainingDuration,
		IsActive:                true, IsFrozen: isFrozen,
		IsGravityDeflected: r.isGravityDeflected,
	}
	return snapshot
}

func (r *ProjectileRun) advanceMotionLocked(now time.Time) {
	if r == nil || now.IsZero() || !now.After(r.motionUpdated) ||
		r.motionRemaining <= 0 {
		return
	}
	startedAt := r.motionUpdated
	if startedAt.Before(r.launchAt) {
		startedAt = r.launchAt
	}
	if startedAt.Before(r.frozenUntil) {
		startedAt = r.frozenUntil
		if now.Before(startedAt) {
			startedAt = now
		}
	}
	if !now.After(startedAt) {
		r.motionUpdated = now
		return
	}
	duration := float32(now.Sub(startedAt).Seconds())
	distance := (r.speed*duration + 0.5*r.acceleration*duration*duration) *
		r.motionSpeedScale
	distance = min(r.motionRemaining, max(float32(0), distance))
	if r.isCollisionSampled && distance > 0 {
		r.motionSegments = append(r.motionSegments, sim.ProjectileMotionSegment{
			Position: r.motionPosition, Direction: r.direction, Distance: distance,
			Speed:        r.speed * r.motionSpeedScale,
			Acceleration: r.acceleration * r.motionSpeedScale,
		})
	}
	r.motionPosition.X += r.direction.X * distance
	r.motionPosition.Y += r.direction.Y * distance
	r.motionPosition.Z += r.direction.Z * distance
	r.motionRemaining -= distance
	r.speed += r.acceleration * duration
	r.motionUpdated = now
}

// Freeze preserves the projectile's live position and remaining flight while
// native Frozen presentation owns the client-side pause.
func (r *ProjectileRun) Freeze(now time.Time, duration time.Duration) bool {
	if r == nil || now.IsZero() || duration <= 0 {
		return false
	}
	r.motionMutex.Lock()
	defer r.motionMutex.Unlock()
	r.advanceMotionLocked(now)
	if r.motionRemaining <= 0 {
		return false
	}
	expiresAt := now.Add(duration)
	if expiresAt.After(r.frozenUntil) {
		r.frozenUntil = expiresAt
	}
	return true
}

func (r *ProjectileRun) SetSpeedScale(now time.Time, scale float32) bool {
	if r == nil || now.IsZero() || scale <= 0 ||
		math.IsNaN(float64(scale)) || math.IsInf(float64(scale), 0) {
		return false
	}
	r.motionMutex.Lock()
	defer r.motionMutex.Unlock()
	r.advanceMotionLocked(now)
	if r.motionRemaining <= 0 {
		return false
	}
	r.motionSpeedScale = scale
	return true
}

func (r *ProjectileRun) Thaw(now time.Time) {
	if r == nil || now.IsZero() {
		return
	}
	r.motionMutex.Lock()
	defer r.motionMutex.Unlock()
	if now.Before(r.frozenUntil) {
		return
	}
	if r.motionUpdated.Before(r.frozenUntil) {
		r.motionUpdated = r.frozenUntil
	}
	r.frozenUntil = time.Time{}
}

// RemainingFlightDelay returns the wall-clock delay before authoritative
// impact after live speed and Frozen state are applied.
func (r *ProjectileRun) RemainingFlightDelay(now time.Time) time.Duration {
	if r == nil || now.IsZero() {
		return 0
	}
	r.motionMutex.Lock()
	defer r.motionMutex.Unlock()
	r.advanceMotionLocked(now)
	if r.motionRemaining <= 0 {
		return 0
	}
	delay := time.Duration(0)
	if now.Before(r.frozenUntil) {
		delay = r.frozenUntil.Sub(now)
	}
	return delay + r.remainingMotionDurationLocked()
}

func (e *ProjectileRun) remainingMotionDurationLocked() time.Duration {
	speed := float64(e.speed * e.motionSpeedScale)
	acceleration := float64(e.acceleration * e.motionSpeedScale)
	if e.motionRemaining <= 0 || speed <= 0 || acceleration < 0 {
		return 0
	}
	finalSpeed := math.Sqrt(speed*speed + 2*acceleration*float64(e.motionRemaining))
	return time.Duration(math.Ceil(2 * float64(e.motionRemaining) / (speed + finalSpeed) * float64(time.Second)))
}

func (r *ProjectileRun) Retarget(
	now time.Time, targetObjectID uint32, targetPosition sim.Position,
	turnRate float32,
) ([][]byte, error) {
	if r == nil || now.IsZero() || turnRate <= 0 {
		return nil, errors.New("projectile retarget invalid")
	}
	r.motionMutex.Lock()
	isAlreadyTargeted := r.retargetID == targetObjectID
	r.motionMutex.Unlock()
	if isAlreadyTargeted {
		return nil, nil
	}
	snapshot := r.Snapshot(now)
	if !snapshot.IsActive {
		return nil, nil
	}
	r.motionMutex.Lock()
	r.advanceMotionLocked(now)
	remainingDistance := r.motionRemaining
	r.motionMutex.Unlock()
	if remainingDistance <= 0 {
		return nil, nil
	}
	direction := snapshot.Direction
	if targetObjectID != 0 {
		delta := sim.Position{
			X: targetPosition.X - snapshot.Position.X,
			Y: targetPosition.Y - snapshot.Position.Y,
			Z: targetPosition.Z - snapshot.Position.Z,
		}
		length := float32(math.Sqrt(float64(
			delta.X*delta.X + delta.Y*delta.Y + delta.Z*delta.Z,
		)))
		if length <= 0 {
			return nil, nil
		}
		direction = sim.Position{
			X: delta.X / length, Y: delta.Y / length, Z: delta.Z / length,
		}
	} else {
		targetPosition = sim.Position{
			X: snapshot.Position.X + direction.X*remainingDistance,
			Y: snapshot.Position.Y + direction.Y*remainingDistance,
			Z: snapshot.Position.Z + direction.Z*remainingDistance,
		}
	}
	r.motionMutex.Lock()
	r.launchAt = now
	r.launchPosition = snapshot.Position
	r.direction = direction
	r.targetPosition = targetPosition
	r.distance = remainingDistance
	r.retargetID = targetObjectID
	r.targetObjectID = targetObjectID
	r.motionPosition = snapshot.Position
	r.motionUpdated = now
	r.motionRemaining = remainingDistance
	projectileSpeed := r.speed
	r.motionMutex.Unlock()
	target := raknet.Vector3{
		X: targetPosition.X, Y: targetPosition.Y, Z: targetPosition.Z,
	}
	initialDirection := raknet.Vector3{
		X: direction.X, Y: direction.Y, Z: direction.Z,
	}
	lastUpdate := int32(1000)
	packet, err := raknet.MarshalApplication(raknet.LocomotionUpdateContractMessage{
		ObjectID: r.objectID,
		Locomotion: raknet.LocomotionReflection{
			Projectile: &raknet.ProjectileParameter{
				Speed: projectileSpeed, Range: remainingDistance,
				Direction: initialDirection, Flags: 1, TurnRate: turnRate,
				IsGroundCollisionIgnored:   true,
				IsCreatureCollisionIgnored: true,
			},
			TargetObjectID: &targetObjectID, TargetPosition: &target,
			InitialDirection: &initialDirection, ReflectedLastUpdate: &lastUpdate,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("projectileRetargetMarshal: %w", err)
	}
	return [][]byte{packet}, nil
}

func (r *ProjectileRun) ApplyGravity(
	now time.Time, center sim.Position, acceleration float32, duration time.Duration,
) ([][]byte, error) {
	if r == nil || now.IsZero() || acceleration <= 0 || duration <= 0 ||
		math.IsNaN(float64(acceleration)) || math.IsInf(float64(acceleration), 0) {
		return nil, errors.New("projectile gravity invalid")
	}
	snapshot := r.Snapshot(now)
	if !snapshot.IsActive || snapshot.Speed <= 0 {
		return nil, nil
	}
	delta := sim.Position{
		X: center.X - snapshot.Position.X,
		Y: center.Y - snapshot.Position.Y,
		Z: center.Z - snapshot.Position.Z,
	}
	distance := float32(math.Sqrt(float64(
		delta.X*delta.X + delta.Y*delta.Y + delta.Z*delta.Z,
	)))
	if distance <= 0 {
		return nil, nil
	}
	attractionScale := acceleration * float32(duration.Seconds()) / distance
	velocity := sim.Position{
		X: snapshot.Direction.X*snapshot.Speed + delta.X*attractionScale,
		Y: snapshot.Direction.Y*snapshot.Speed + delta.Y*attractionScale,
		Z: snapshot.Direction.Z*snapshot.Speed + delta.Z*attractionScale,
	}
	velocityLength := float32(math.Sqrt(float64(
		velocity.X*velocity.X + velocity.Y*velocity.Y + velocity.Z*velocity.Z,
	)))
	if velocityLength <= 0 {
		return nil, nil
	}
	direction := sim.Position{
		X: velocity.X / velocityLength,
		Y: velocity.Y / velocityLength,
		Z: velocity.Z / velocityLength,
	}
	r.motionMutex.Lock()
	r.advanceMotionLocked(now)
	remainingDistance := r.motionRemaining
	if remainingDistance <= 0 {
		r.motionMutex.Unlock()
		return nil, nil
	}
	r.launchAt = now
	r.launchPosition = snapshot.Position
	r.direction = direction
	gravityTargetPosition := sim.Position{
		X: snapshot.Position.X + direction.X*remainingDistance,
		Y: snapshot.Position.Y + direction.Y*remainingDistance,
		Z: snapshot.Position.Z + direction.Z*remainingDistance,
	}
	r.targetPosition = gravityTargetPosition
	r.distance = remainingDistance
	r.retargetID = 0
	r.targetObjectID = 0
	r.motionPosition = snapshot.Position
	r.motionUpdated = now
	r.motionRemaining = remainingDistance
	r.isGravityDeflected = true
	// The replacement native locomotion has a constant speed. Preserve that
	// same speed after changing direction instead of continuing acceleration
	// which is absent from the packet below.
	r.acceleration = 0
	r.gravityPosition = snapshot.Position
	r.gravityDirection = direction
	r.gravityDistance = remainingDistance
	r.motionMutex.Unlock()
	targetPosition := raknet.Vector3{
		X: gravityTargetPosition.X,
		Y: gravityTargetPosition.Y,
		Z: gravityTargetPosition.Z,
	}
	initialDirection := raknet.Vector3{X: direction.X, Y: direction.Y, Z: direction.Z}
	targetObjectID := uint32(0)
	lastUpdate := int32(1000)
	packet, err := raknet.MarshalApplication(raknet.LocomotionUpdateContractMessage{
		ObjectID: r.objectID,
		Locomotion: raknet.LocomotionReflection{
			Projectile: &raknet.ProjectileParameter{
				Speed: snapshot.Speed, Range: remainingDistance,
				Direction:                initialDirection,
				IsGroundCollisionIgnored: true, IsCreatureCollisionIgnored: true,
			},
			TargetObjectID: &targetObjectID, TargetPosition: &targetPosition,
			InitialDirection: &initialDirection, ReflectedLastUpdate: &lastUpdate,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("projectileGravityMarshal: %w", err)
	}
	return [][]byte{packet}, nil
}

func (r *ProjectileRun) GravityCollisionTrajectory() (ProjectileCollisionTrajectory, bool) {
	if r == nil {
		return ProjectileCollisionTrajectory{}, false
	}
	r.motionMutex.Lock()
	defer r.motionMutex.Unlock()
	if !r.isGravityDeflected || r.gravityDistance <= 0 {
		return ProjectileCollisionTrajectory{}, false
	}
	return ProjectileCollisionTrajectory{
		Position: r.gravityPosition, Direction: r.gravityDirection,
		Distance: r.gravityDistance,
	}, true
}

func (r *ProjectileRun) DeleteProjectile(
	ctx context.Context,
) ([][]byte, bool, error) {
	if r == nil || r.behavior == nil || r.session == nil || r.outbox == nil {
		return nil, false, errors.New("nil projectile run")
	}
	if ctx == nil {
		return nil, false, errors.New("nil projectile context")
	}
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	isDeleted, err := r.behavior.DeleteProjectile()
	if err != nil {
		return nil, false, fmt.Errorf("projectileDelete: %w", err)
	}
	if !isDeleted {
		return nil, false, nil
	}
	err = r.session.DispatchPending(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("projectileDeleteDispatch: %w", err)
	}
	packets := r.outbox.Drain()
	r.Finish()
	return packets, true, nil
}

func (r *ProjectileRun) ResolveCollision(
	ctx context.Context, deadline time.Duration, isDirectHit bool, isTargetValid bool,
	targetHitPoint float32, damage float32, isCritical bool,
	impactPosition sim.Position, facing sim.Position,
) ([][]byte, error) {
	if r == nil || r.simulator == nil || r.session == nil || r.behavior == nil || r.outbox == nil {
		return nil, errors.New("nil Poison Cloud run")
	}
	err := r.simulator.AdvanceTo(deadline)
	if err != nil {
		return nil, fmt.Errorf("clockAdvance: %w", err)
	}
	err = r.behavior.PrepareCollision(
		isDirectHit, isTargetValid, targetHitPoint, damage, isCritical, impactPosition, facing,
	)
	if err != nil {
		return nil, fmt.Errorf("collisionPrepare: %w", err)
	}
	err = r.behavior.ResolveCollision()
	if err != nil {
		return nil, fmt.Errorf("collisionResolve: %w", err)
	}
	err = r.session.DispatchPending(ctx)
	if err != nil {
		return nil, fmt.Errorf("collisionDispatch: %w", err)
	}
	return r.outbox.Drain(), nil
}

func (r *ProjectileRun) AdvanceClock(deadline time.Duration) error {
	if r == nil || r.simulator == nil {
		return errors.New("nil Poison Cloud run")
	}
	err := r.simulator.AdvanceTo(deadline)
	if err != nil {
		return fmt.Errorf("clockAdvance: %w", err)
	}
	return nil
}

func (r *ProjectileRun) Advance(ctx context.Context, deadline time.Duration) ([][]byte, error) {
	if r == nil || r.simulator == nil || r.session == nil || r.outbox == nil {
		return nil, errors.New("nil Poison Cloud run")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	now := r.simulator.Now()
	if deadline < now {
		return nil, fmt.Errorf("deadlineBeforeNow: %s < %s", deadline, now)
	}
	err := r.session.AdvanceBy(ctx, deadline-now)
	if err != nil {
		return nil, fmt.Errorf("sessionAdvance: %w", err)
	}
	return r.outbox.Drain(), nil
}

func (r *ProjectileRun) PrepareImpact(
	isTargetValid bool, targetHitPoint float32, damage float32,
	impactPosition sim.Position, facing sim.Position,
) error {
	if r == nil || r.behavior == nil {
		return errors.New("nil Poison Cloud run")
	}
	err := r.behavior.PrepareImpact(isTargetValid, targetHitPoint, damage, impactPosition, facing)
	if err != nil {
		return fmt.Errorf("behaviorPrepare: %w", err)
	}
	return nil
}

func (o poisonCloudObjectOutbox) Spawn(
	_ context.Context, _ sim.EventMeta, intent sim.SpawnIntent,
) error {
	return fmt.Errorf("spawnUnexpected: %#v", intent)
}

func (o poisonCloudObjectOutbox) Despawn(
	ctx context.Context, meta sim.EventMeta, intent sim.DespawnIntent,
) error {
	if o.owner == nil {
		return errors.New("Poison Cloud outbox missing")
	}
	return o.owner.Encode(ctx, meta, intent)
}

func (o poisonCloudObjectOutbox) Move(
	_ context.Context, _ sim.EventMeta, intent sim.MovementIntent,
) error {
	return fmt.Errorf("movementUnexpected: %#v", intent)
}

func (o poisonCloudObjectOutbox) Teleport(
	_ context.Context, _ sim.EventMeta, intent sim.TeleportIntent,
) error {
	return fmt.Errorf("teleportUnexpected: %#v", intent)
}

func (o poisonCloudObjectOutbox) CreateTriggerVolume(
	_ context.Context, _ sim.EventMeta, intent sim.TriggerVolumeIntent,
) error {
	return fmt.Errorf("triggerUnexpected: %#v", intent)
}

func (o poisonCloudObjectOutbox) DestroyTriggerVolume(
	_ context.Context, _ sim.EventMeta, intent sim.DestroyTriggerVolumeIntent,
) error {
	return fmt.Errorf("triggerDestroyUnexpected: %#v", intent)
}

func (o *poisonCloudOutbox) Start(
	ctx context.Context, meta sim.EventMeta, intent sim.CooldownIntent,
) error {
	if intent.Role != poisonCloudActorRole {
		return fmt.Errorf("cooldownIntent: %#v", intent)
	}
	return o.Encode(ctx, meta, intent)
}

func (o *poisonCloudOutbox) Stop(
	ctx context.Context, meta sim.EventMeta, intent sim.LocomotionStopIntent,
) error {
	if intent.Role != poisonCloudActorRole {
		return fmt.Errorf("locomotionIntent: %#v", intent)
	}
	return o.Encode(ctx, meta, intent)
}

func (o *poisonCloudOutbox) Launch(
	ctx context.Context, meta sim.EventMeta, intent sim.ProjectileLaunchIntent,
) error {
	return o.Encode(ctx, meta, intent)
}

func (o *poisonCloudOutbox) Move(
	ctx context.Context, meta sim.EventMeta, intent sim.ProjectileMotionIntent,
) error {
	return o.Encode(ctx, meta, intent)
}

func (o *poisonCloudOutbox) Impact(
	ctx context.Context, meta sim.EventMeta, intent sim.ProjectileImpactIntent,
) error {
	return o.Encode(ctx, meta, intent)
}

func (o *poisonCloudOutbox) Damage(
	ctx context.Context, meta sim.EventMeta, intent sim.DamageIntent,
) error {
	return o.Encode(ctx, meta, intent)
}

func (o *poisonCloudOutbox) SetHitPoints(
	ctx context.Context, meta sim.EventMeta, intent sim.HitPointIntent,
) error {
	return o.Encode(ctx, meta, intent)
}

func (r *ProjectileRun) Stop() {
	if r == nil {
		return
	}
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	r.Finish()
}

func (r *ProjectileRun) Finish() {
	if r == nil {
		return
	}
	r.cancel = nil
	if r.behavior != nil {
		_ = r.behavior.Cancel()
	}
	if r.simulator != nil {
		r.simulator.InvalidateRole(poisonCloudProjectileRole)
		r.simulator.Stop()
	}
}

func (r *ProjectileRun) AddDeadline(deadline time.Duration) {
	if r == nil {
		return
	}
	r.deadlines = append(r.deadlines, deadline)
	slices.Sort(r.deadlines)
	r.deadlines = slices.Compact(r.deadlines)
}

func (r *ProjectileRun) Deadlines() []time.Duration {
	if r == nil {
		return nil
	}
	return append([]time.Duration(nil), r.deadlines...)
}

func (r *ProjectileRun) LastDeadline() time.Duration {
	if r == nil || len(r.deadlines) == 0 {
		return 0
	}
	return r.deadlines[len(r.deadlines)-1]
}

func (r *ProjectileRun) Definition() sim.AbilityDefinition {
	if r == nil {
		return sim.AbilityDefinition{}
	}
	return r.ability
}

func (r *ProjectileRun) Now() time.Duration {
	if r == nil || r.simulator == nil {
		return 0
	}
	return r.simulator.Now()
}

func (r *ProjectileRun) SetCancel(cancel raknet.CancelSchedule) {
	if r != nil {
		r.cancel = cancel
	}
}

func (r *ProjectileRun) ClearCancel() {
	if r != nil {
		r.cancel = nil
	}
}

func (r *ProjectileRun) IsCancelSet() bool {
	return r != nil && r.cancel != nil
}
