package raknet103

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/zone/ability"
)

const ProjectileCollisionTick = 16 * time.Millisecond

type TrackedProjectile struct {
	actor                 ability.ProjectileActor
	targetObjectID        uint32
	run                   *ProjectileRun
	collisionTracker      *sim.ProjectileBoxTracker
	isDirectHit           bool
	isImpactResolved      bool
	shotDeadline          time.Duration
	impactDeadline        time.Duration
	lastCollisionDeadline time.Duration
	releaseDeadline       time.Duration
	cooldownDeadline      time.Duration
}

type TrackedProjectileRequest struct {
	Authority          ability.ProjectileAuthority
	Actor              ability.ProjectileActor
	Ability            sim.AbilityDefinition
	FootprintRadius    float32
	Geometry           ability.ProjectileGeometry
	TargetObjectID     uint32
	ProjectileObjectID uint32
	TargetPosition     raknet.Vector3
	TargetHitPoint     float32
	TargetManaPoint    float32
	SourceTime         uint64
}

func NewTrackedProjectile(
	req TrackedProjectileRequest,
) (*TrackedProjectile, [][]byte, error) {
	if req.Authority == nil || req.Actor.ObjectID == 0 || req.Actor.Noun == "" ||
		req.FootprintRadius <= 0 ||
		req.Geometry.ProjectileHalfExtent.X <= 0 ||
		req.Geometry.ProjectileHalfExtent.Y <= 0 ||
		req.Geometry.ProjectileHalfExtent.Z <= 0 {
		return nil, nil, errors.New("invalid tracked projectile request")
	}
	if req.Ability.Kind != sim.AbilityKindProjectile ||
		req.Ability.MaximumDamage <= 0 || req.Ability.Range <= 0 ||
		req.Ability.Distance <= 0 || req.Ability.Speed <= 0 {
		return nil, nil, errors.New("invalid projectile ability")
	}
	actorPosition, isFound := req.Authority.EnemyPosition(req.Actor.ObjectID)
	if !isFound {
		return nil, nil, errors.New("projectile actor missing")
	}
	facing := ability.ProjectileDirection(actorPosition, req.TargetPosition)
	launchPosition := raknet.Vector3{
		X: actorPosition.X + facing.X*req.FootprintRadius,
		Y: actorPosition.Y + facing.Y*req.FootprintRadius,
		Z: actorPosition.Z + facing.Z*req.FootprintRadius,
	}
	direction := ability.ProjectileDirection(launchPosition, req.TargetPosition)
	tracker, err := sim.NewProjectileBoxTracker(sim.ProjectileBoxCollisionInput{
		ProjectilePosition: sim.Position{
			X: launchPosition.X, Y: launchPosition.Y, Z: launchPosition.Z,
		},
		ProjectileDirection: sim.Position{
			X: direction.X, Y: direction.Y, Z: direction.Z,
		},
		ProjectileHalfExtent: req.Geometry.ProjectileHalfExtent,
		TargetPosition: sim.Position{
			X: req.TargetPosition.X, Y: req.TargetPosition.Y, Z: req.TargetPosition.Z,
		},
		TargetMinimum:   req.Geometry.TargetMinimum,
		TargetMaximum:   req.Geometry.TargetMaximum,
		MaximumDistance: req.Ability.Distance,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("collisionCreate: %w", err)
	}
	run, packets, err := NewProjectileRun(ProjectileInput{
		Ability: req.Ability, ActorObjectID: req.Actor.ObjectID,
		TargetObjectID:     req.TargetObjectID,
		ProjectileObjectID: req.ProjectileObjectID,
		ActorPosition: sim.Position{
			X: actorPosition.X, Y: actorPosition.Y, Z: actorPosition.Z,
		},
		TargetPosition: sim.Position{
			X: req.TargetPosition.X, Y: req.TargetPosition.Y, Z: req.TargetPosition.Z,
		},
		ActorFacing: sim.Position{X: facing.X, Y: facing.Y, Z: facing.Z},
		ImpactPosition: sim.Position{
			X: req.TargetPosition.X, Y: req.TargetPosition.Y, Z: req.TargetPosition.Z,
		},
		FootprintRadius: req.FootprintRadius, IsCollisionExternallyDriven: true,
		Damage: req.Ability.MaximumDamage, TargetHitPoint: req.TargetHitPoint,
		TargetManaPoint: req.TargetManaPoint, ActorTeam: 2, SourceTime: req.SourceTime,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("runCreate: %w", err)
	}
	return &TrackedProjectile{
		actor: req.Actor, targetObjectID: req.TargetObjectID,
		run: run, collisionTracker: tracker,
		shotDeadline:          req.Ability.HitDelay,
		impactDeadline:        req.Ability.HitDelay + ProjectileCollisionTick,
		lastCollisionDeadline: req.Ability.HitDelay,
		releaseDeadline:       req.Ability.ReleaseDelay,
		cooldownDeadline:      req.Ability.HitDelay + req.Ability.Cooldown,
	}, packets, nil
}

func (e *TrackedProjectile) ActorObjectID() uint32 {
	if e == nil {
		return 0
	}
	return e.actor.ObjectID
}

func (e *TrackedProjectile) TargetObjectID() uint32 {
	if e == nil {
		return 0
	}
	return e.targetObjectID
}

func (e *TrackedProjectile) ShotDeadline() time.Duration {
	return e.shotDeadline
}

func (e *TrackedProjectile) ImpactDeadline() time.Duration {
	return e.impactDeadline
}

func (e *TrackedProjectile) ReleaseDeadline() time.Duration {
	return e.releaseDeadline
}

func (e *TrackedProjectile) CooldownDeadline() time.Duration {
	return e.cooldownDeadline
}

func (e *TrackedProjectile) AdvanceShot(ctx context.Context) ([][]byte, error) {
	if e == nil || e.run == nil {
		return nil, errors.New("nil tracked projectile")
	}
	packets, err := e.run.Advance(ctx, e.shotDeadline)
	if err != nil {
		return nil, fmt.Errorf("shotAdvance: %w", err)
	}
	return packets, nil
}

func (e *TrackedProjectile) NextCollisionDeadline() time.Duration {
	if e == nil {
		return 0
	}
	return e.lastCollisionDeadline + ProjectileCollisionTick
}

func (e *TrackedProjectile) AdvanceCollision(
	ctx context.Context,
	authority ability.ProjectileAuthority,
	playerPosition raknet.Vector3,
	hitPoint float32,
	deadline time.Duration,
	resolveCritical ability.ProjectileCriticalResolver,
) ([][]byte, float32, bool, error) {
	if e == nil || e.run == nil || e.collisionTracker == nil || authority == nil {
		return nil, hitPoint, false, errors.New("nil tracked projectile collision")
	}
	if e.isImpactResolved || deadline <= e.lastCollisionDeadline {
		return nil, hitPoint, false, errors.New("invalid projectile collision deadline")
	}
	stepDuration := deadline - e.lastCollisionDeadline
	definition := e.run.Definition()
	collision, isResolved, err := e.collisionTracker.Advance(
		definition.Speed*float32(stepDuration.Seconds()),
		sim.Position{X: playerPosition.X, Y: playerPosition.Y, Z: playerPosition.Z},
		hitPoint > 0,
	)
	if err != nil {
		return nil, hitPoint, false, fmt.Errorf("collisionAdvance: %w", err)
	}
	e.lastCollisionDeadline = deadline
	if !isResolved {
		err = e.run.AdvanceClock(max(deadline, e.run.Now()))
		if err != nil {
			return nil, hitPoint, false, fmt.Errorf("clockAdvance: %w", err)
		}
		return nil, hitPoint, false, nil
	}
	e.isImpactResolved = true
	e.isDirectHit = collision.IsDirectHit
	damage := definition.MaximumDamage
	if collision.IsDirectHit {
		damage, err = authority.SelectRankDamage(definition)
		if err != nil {
			return nil, hitPoint, false, fmt.Errorf("damageSelect: %w", err)
		}
	}
	isCritical := false
	if collision.IsDirectHit && hitPoint > 0 && resolveCritical != nil {
		critical, criticalErr := resolveCritical(e.actor.Noun, damage)
		if criticalErr != nil {
			return nil, hitPoint, false, fmt.Errorf("criticalResolve: %w", criticalErr)
		}
		damage = critical.Damage
		isCritical = critical.IsCritical
	}
	appliedDamage := min(damage, hitPoint)
	isTargetValid := collision.IsDirectHit && appliedDamage > 0
	preparedDamage := appliedDamage
	if preparedDamage <= 0 {
		preparedDamage = damage
	}
	actorPosition, isFound := authority.EnemyPosition(e.actor.ObjectID)
	if !isFound {
		actorPosition = e.actor.Position
	}
	facing := ability.ProjectileDirection(actorPosition, playerPosition)
	packets, err := e.run.ResolveCollision(
		ctx, max(deadline, e.run.Now()), collision.IsDirectHit, isTargetValid,
		hitPoint, preparedDamage, isCritical, collision.Position,
		sim.Position{X: facing.X, Y: facing.Y, Z: facing.Z},
	)
	if err != nil {
		return nil, hitPoint, false, fmt.Errorf("impactResolve: %w", err)
	}
	if isTargetValid {
		hitPoint -= appliedDamage
	}
	return packets, hitPoint, true, nil
}

func (e *TrackedProjectile) AdvanceRelease(ctx context.Context) ([][]byte, error) {
	if e == nil || e.run == nil {
		return nil, errors.New("nil tracked projectile")
	}
	packets, err := e.run.Advance(ctx, e.releaseDeadline)
	if err != nil {
		return nil, fmt.Errorf("releaseAdvance: %w", err)
	}
	return packets, nil
}

func (e *TrackedProjectile) Stop() {
	if e != nil && e.run != nil {
		e.run.Stop()
	}
}
