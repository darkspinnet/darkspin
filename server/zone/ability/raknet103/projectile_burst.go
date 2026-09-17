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

type ProjectileBurst struct {
	actor                  ability.BurstActor
	targetObjectID         uint32
	run                    *BurstRun
	trackers               []*sim.ProjectileBoxTracker
	lastCollisionDeadlines []time.Duration
	areResolved            []bool
	isReleased             bool
	footprintRadius        float32
	geometry               ability.BurstGeometry
}

type ProjectileBurstRequest struct {
	Authority          ability.BurstAuthority
	Actor              ability.BurstActor
	Ability            sim.AbilityDefinition
	FootprintRadius    float32
	Geometry           ability.BurstGeometry
	TargetObjectID     uint32
	ProjectileObjectID []uint32
	TargetPosition     raknet.Vector3
	TargetHitPoint     float32
	TargetManaPoint    float32
	SourceTime         uint64
}

func NewProjectileBurst(
	req ProjectileBurstRequest,
) (*ProjectileBurst, [][]byte, error) {
	if req.Authority == nil || req.Actor.ObjectID == 0 || req.Actor.Noun == "" ||
		req.FootprintRadius <= 0 ||
		req.Geometry.ProjectileHalfExtent.X <= 0 ||
		req.Geometry.ProjectileHalfExtent.Y <= 0 ||
		req.Geometry.ProjectileHalfExtent.Z <= 0 ||
		len(req.ProjectileObjectID) != len(req.Ability.HitDelays) {
		return nil, nil, errors.New("invalid projectile burst request")
	}
	actorPosition, isFound := req.Authority.EnemyPosition(req.Actor.ObjectID)
	if !isFound {
		return nil, nil, errors.New("projectile burst actor missing")
	}
	facing := ability.BurstDirection(actorPosition, req.TargetPosition)
	run, packets, err := NewBurstRun(BurstInput{
		Ability: req.Ability, ActorObjectID: req.Actor.ObjectID,
		TargetObjectID:      req.TargetObjectID,
		ProjectileObjectIDs: req.ProjectileObjectID,
		ActorPosition: sim.Position{
			X: actorPosition.X, Y: actorPosition.Y, Z: actorPosition.Z,
		},
		TargetPosition: sim.Position{
			X: req.TargetPosition.X, Y: req.TargetPosition.Y, Z: req.TargetPosition.Z,
		},
		ActorFacing:     sim.Position{X: facing.X, Y: facing.Y, Z: facing.Z},
		FootprintRadius: req.FootprintRadius,
		Damage:          req.Ability.MaximumDamage, TargetHitPoint: req.TargetHitPoint,
		TargetManaPoint: req.TargetManaPoint, ActorTeam: 2, SourceTime: req.SourceTime,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("runCreate: %w", err)
	}
	return &ProjectileBurst{
		actor: req.Actor, targetObjectID: req.TargetObjectID, run: run,
		trackers:               make([]*sim.ProjectileBoxTracker, len(req.Ability.HitDelays)),
		lastCollisionDeadlines: append([]time.Duration(nil), req.Ability.HitDelays...),
		areResolved:            make([]bool, len(req.Ability.HitDelays)),
		footprintRadius:        req.FootprintRadius, geometry: req.Geometry,
	}, packets, nil
}

func (e *ProjectileBurst) ActorObjectID() uint32 {
	if e == nil {
		return 0
	}
	return e.actor.ObjectID
}

func (e *ProjectileBurst) TargetObjectID() uint32 {
	if e == nil {
		return 0
	}
	return e.targetObjectID
}

func (e *ProjectileBurst) LastCollisionDeadline(index int) (time.Duration, bool) {
	if e == nil || index < 0 || index >= len(e.lastCollisionDeadlines) {
		return 0, false
	}
	return e.lastCollisionDeadlines[index], true
}

func (e *ProjectileBurst) AdvanceLaunch(
	ctx context.Context,
	index int,
	authority ability.BurstAuthority,
	playerPosition raknet.Vector3,
) ([][]byte, error) {
	if e == nil || e.run == nil || authority == nil ||
		index < 0 || index >= len(e.trackers) {
		return nil, errors.New("invalid projectile burst launch")
	}
	actorPosition, isFound := authority.EnemyPosition(e.actor.ObjectID)
	if !isFound {
		return nil, errors.New("projectile burst actor missing")
	}
	facing := ability.BurstDirection(actorPosition, playerPosition)
	launchPosition := raknet.Vector3{
		X: actorPosition.X + facing.X*e.footprintRadius,
		Y: actorPosition.Y + facing.Y*e.footprintRadius,
		Z: actorPosition.Z + facing.Z*e.footprintRadius,
	}
	direction := ability.BurstDirection(launchPosition, playerPosition)
	tracker, err := sim.NewProjectileBoxTracker(sim.ProjectileBoxCollisionInput{
		ProjectilePosition: sim.Position{
			X: launchPosition.X, Y: launchPosition.Y, Z: launchPosition.Z,
		},
		ProjectileDirection: sim.Position{
			X: direction.X, Y: direction.Y, Z: direction.Z,
		},
		ProjectileHalfExtent: e.geometry.ProjectileHalfExtent,
		TargetPosition: sim.Position{
			X: playerPosition.X, Y: playerPosition.Y, Z: playerPosition.Z,
		},
		TargetMinimum: e.geometry.TargetMinimum, TargetMaximum: e.geometry.TargetMaximum,
		MaximumDistance: e.run.Definition().Distance,
	})
	if err != nil {
		return nil, fmt.Errorf("trackerCreate[%d]: %w", index, err)
	}
	err = e.run.PrepareLaunch(
		index,
		sim.Position{X: actorPosition.X, Y: actorPosition.Y, Z: actorPosition.Z},
		sim.Position{X: playerPosition.X, Y: playerPosition.Y, Z: playerPosition.Z},
		sim.Position{X: facing.X, Y: facing.Y, Z: facing.Z},
		e.targetObjectID, true,
	)
	if err != nil {
		return nil, fmt.Errorf("launchPrepare[%d]: %w", index, err)
	}
	packets, err := e.run.Advance(ctx, e.run.Definition().HitDelays[index])
	if err != nil {
		return nil, fmt.Errorf("launchAdvance[%d]: %w", index, err)
	}
	e.trackers[index] = tracker
	return packets, nil
}

func (e *ProjectileBurst) AdvanceCollision(
	ctx context.Context,
	index int,
	authority ability.BurstAuthority,
	playerPosition raknet.Vector3,
	hitPoint float32,
	deadline time.Duration,
	resolveCritical ability.BurstCriticalResolver,
) ([][]byte, float32, bool, error) {
	if e == nil || e.run == nil || authority == nil ||
		index < 0 || index >= len(e.trackers) || e.trackers[index] == nil ||
		e.areResolved[index] || deadline <= e.lastCollisionDeadlines[index] {
		return nil, hitPoint, false, errors.New("invalid projectile burst collision")
	}
	stepDuration := deadline - e.lastCollisionDeadlines[index]
	definition := e.run.Definition()
	collision, isResolved, err := e.trackers[index].Advance(
		definition.Speed*float32(stepDuration.Seconds()),
		sim.Position{X: playerPosition.X, Y: playerPosition.Y, Z: playerPosition.Z},
		hitPoint > 0,
	)
	if err != nil {
		return nil, hitPoint, false, fmt.Errorf("trackerAdvance[%d]: %w", index, err)
	}
	e.lastCollisionDeadlines[index] = deadline
	if !isResolved {
		err = e.run.AdvanceClock(max(deadline, e.run.Now()))
		if err != nil {
			return nil, hitPoint, false, fmt.Errorf("clockAdvance[%d]: %w", index, err)
		}
		return nil, hitPoint, false, nil
	}
	damage := definition.MaximumDamage
	if collision.IsDirectHit {
		damage, err = authority.SelectRankDamage(definition)
		if err != nil {
			return nil, hitPoint, false, fmt.Errorf("damageSelect[%d]: %w", index, err)
		}
	}
	isCritical := false
	if collision.IsDirectHit && hitPoint > 0 && resolveCritical != nil {
		critical, criticalErr := resolveCritical(e.actor.Noun, damage)
		if criticalErr != nil {
			return nil, hitPoint, false, fmt.Errorf("criticalResolve[%d]: %w", index, criticalErr)
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
	facing := ability.BurstDirection(actorPosition, playerPosition)
	packets, err := e.run.ResolveCollision(
		ctx, index, max(deadline, e.run.Now()), collision.IsDirectHit,
		isTargetValid, hitPoint, preparedDamage, isCritical, collision.Position,
		sim.Position{X: facing.X, Y: facing.Y, Z: facing.Z},
	)
	if err != nil {
		return nil, hitPoint, false, fmt.Errorf("collisionResolve[%d]: %w", index, err)
	}
	if isTargetValid {
		hitPoint -= appliedDamage
	}
	e.areResolved[index] = true
	return packets, hitPoint, true, nil
}

func (e *ProjectileBurst) AdvanceRelease(ctx context.Context) ([][]byte, error) {
	if e == nil || e.run == nil {
		return nil, errors.New("nil projectile burst")
	}
	packets, err := e.run.Advance(ctx, e.run.Definition().ReleaseDelay)
	if err != nil {
		return nil, fmt.Errorf("releaseAdvance: %w", err)
	}
	e.isReleased = true
	return packets, nil
}

func (e *ProjectileBurst) IsComplete() bool {
	if e == nil || !e.isReleased {
		return false
	}
	for _, isResolved := range e.areResolved {
		if !isResolved {
			return false
		}
	}
	return true
}

func (e *ProjectileBurst) Stop() {
	if e != nil && e.run != nil {
		e.run.Stop()
	}
}
