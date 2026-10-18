package gameplay

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

const repulsionProjectileInnerRadius = float32(2)
const repulsionProjectileMaximumSpeed = float32(20)
const repulsionProjectileMinimumSpeed = float32(7)
const campaignProjectileMotionPollInterval = 250 * time.Millisecond

type hostileProjectileFreezeStep struct {
	runtime            campaignAbilityCommandRuntime
	sessionKey         string
	generation         uint64
	userID             uint64
	projectileObjectID uint32
	instanceID         uint32
	run                *abilityraknet.ProjectileRun
	cancel             raknet.CancelSchedule
	isReleased         bool
	isDeletionQueued   bool
	originalZone       *zone.Zone
	ownerSessionKey    string
	ownerGeneration    uint64
	expiresAt          time.Time
	stopContext        func() bool
}

func (e *hostileProjectileFreezeStep) expire() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	if e.isReleased {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	owner, isFound := e.runtime.registry.sessions[e.ownerSessionKey]
	isCurrent := isFound && owner.generation == e.ownerGeneration && owner.zone == e.originalZone &&
		owner.campaignNPCProjectiles[e.projectileObjectID] == e.run && e.originalZone.IsActive() && e.originalZone.Context().Err() == nil
	if isCurrent {
		e.run.Thaw(e.runtime.now())
	}
	e.cancel = nil
	err := e.releaseLocked(false)
	e.runtime.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("projectileFreezeExpire: %w", err)
	}
	return nil, nil
}

func (e *hostileProjectileFreezeStep) execute() {
	packets, err := e.expire()
	if err != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("RakNet shared projectile freeze expiry failed projectile=%d instance=%d: %v", e.projectileObjectID, e.instanceID, err)
	}
	if len(packets) != 0 && e.runtime.logger != nil {
		e.runtime.logger.Printf("RakNet shared projectile freeze expiry returned unqueued packets projectile=%d", e.projectileObjectID)
	}
}

func (e *hostileProjectileFreezeStep) retireZone() {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	err := e.releaseLocked(true)
	if err != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("RakNet projectile freeze zone retirement failed projectile=%d instance=%d: %v", e.projectileObjectID, e.instanceID, err)
	}
}

func freezeHostileProjectilesLocked(
	runtime campaignAbilityCommandRuntime, packet raknet.Packet,
	peerSession *gameplayPeerSession, sessionKey string, generation uint64,
	sourceObjectID uint32, center game.Vec3, radius float32,
	modifierID uint32, duration time.Duration, sourceTime uint64, now time.Time,
) ([][]byte, error) {
	if peerSession == nil || radius <= 0 || modifierID == 0 || duration <= 0 ||
		now.IsZero() {
		return nil, nil
	}
	owners := runtime.registry.hostileProjectileOwnersLocked(peerSession, sessionKey)
	for _, owner := range owners {
		projectileObjectID := owner.objectID
		run := owner.run
		snapshot := run.Snapshot(now)
		if !snapshot.IsActive || zonegeometry.Distance(
			center, game.Vec3(snapshot.Position),
		) > radius {
			continue
		}
		reservation, isReserved := run.ReserveFreeze(now, duration)
		if !isReserved {
			continue
		}
		step := &hostileProjectileFreezeStep{
			runtime: runtime, sessionKey: sessionKey, generation: generation,
			projectileObjectID: projectileObjectID, run: run,
			originalZone: peerSession.zone, userID: peerSession.binding.UserID,
			ownerSessionKey: owner.sessionKey, ownerGeneration: owner.generation,
			expiresAt: now.Add(duration),
		}
		instanceID, err := runtime.modifierPool.Allocate()
		if err != nil {
			rollbackErr := step.rollbackAdmissionLocked(reservation, chronoFreezePreparation{})
			return nil, fmt.Errorf("projectileFreezeModifier[%d]: %w", projectileObjectID, errors.Join(err, rollbackErr))
		}
		step.instanceID = instanceID
		created, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
			TargetID: projectileObjectID, ModifierGUID: modifierID,
			InstanceID: instanceID, DurationMilliseconds: uint32(duration / time.Millisecond),
			StackCount: 1, StartMilliseconds: sourceTime, SourceID: sourceObjectID,
		})
		if err != nil {
			rollbackErr := step.rollbackAdmissionLocked(reservation, chronoFreezePreparation{})
			return nil, fmt.Errorf("projectileFreezeCreate[%d]: %w", projectileObjectID, errors.Join(err, rollbackErr))
		}
		cancel, scheduleErr := scheduleSharedFreezeLocked(runtime, step.expiresAt, step.execute)
		step.cancel = cancel
		if scheduleErr == nil && cancel == nil {
			scheduleErr = errors.New("missing freeze cancellation")
		}
		if scheduleErr != nil {
			rollbackErr := step.rollbackAdmissionLocked(reservation, chronoFreezePreparation{})
			return nil, fmt.Errorf("projectileFreezeSchedule[%d]: %w", projectileObjectID, errors.Join(scheduleErr, rollbackErr))
		}
		chronoPreparation, freezeErr := prepareChronoFreezeLocked(
			runtime, packet, sessionKey, generation,
			peerSession.zone, projectileObjectID, duration, run,
		)
		if freezeErr != nil {
			rollbackErr := step.rollbackAdmissionLocked(reservation, chronoPreparation)
			return nil, fmt.Errorf("projectileFreezeState[%d]: %w", projectileObjectID, errors.Join(freezeErr, rollbackErr))
		}
		isCommitted := run.CommitFreeze(reservation)
		if !isCommitted {
			rollbackErr := step.rollbackAdmissionLocked(reservation, chronoPreparation)
			return nil, fmt.Errorf("projectileFreezeCommit[%d]: %w", projectileObjectID, errors.Join(errors.New("freeze reservation no longer current"), rollbackErr))
		}
		chronoPreparation.commitLocked()
		if runtime.registry.projectileFreezes == nil {
			runtime.registry.projectileFreezes = make(map[*abilityraknet.ProjectileRun]map[uint32]*hostileProjectileFreezeStep)
		}
		if runtime.registry.projectileFreezes[run] == nil {
			runtime.registry.projectileFreezes[run] = make(map[uint32]*hostileProjectileFreezeStep)
		}
		runtime.registry.projectileFreezes[run][instanceID] = step
		step.stopContext = context.AfterFunc(peerSession.zone.Context(), step.retireZone)
		packets := [][]byte{created}
		if len(chronoPreparation.packet) > 0 {
			packets = append(packets, chronoPreparation.packet)
		}
		// Preserve the caller's committed mutations before queuing to its
		// stored copy, then reload that copy so its later store keeps delivery.
		runtime.registry.sessions[sessionKey] = *peerSession
		runtime.registry.queueProjectileRetirementLocked(peerSession.zone, packets)
		*peerSession = runtime.registry.sessions[sessionKey]
	}
	return nil, nil
}

func (e *hostileProjectileFreezeStep) rollbackAdmissionLocked(
	reservation abilityraknet.ProjectileFreezeReservation, chronoPreparation chronoFreezePreparation,
) error {
	e.isReleased = true
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	chronoPreparation.cancelLocked()
	cleanupErrs := make([]error, 0, 2)
	isRolledBack := e.run.RollbackFreeze(reservation)
	if !isRolledBack {
		cleanupErrs = append(cleanupErrs, errors.New("freeze reservation no longer current"))
	}
	if e.instanceID != 0 {
		err := e.runtime.modifierPool.Release(e.instanceID)
		if err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("freezeInstanceRelease: %w", err))
		}
	}
	cleanupErr := errors.Join(cleanupErrs...)
	if cleanupErr != nil {
		return fmt.Errorf("freezeRollback: %w", cleanupErr)
	}
	return nil
}

func destroyHostileProjectilesLocked(
	runtime campaignAbilityCommandRuntime, peerSession *gameplayPeerSession, sessionKey string,
	center game.Vec3, radius float32, now time.Time,
) ([][]byte, uint32, error) {
	if peerSession == nil || radius <= 0 || now.IsZero() {
		return nil, 0, nil
	}
	destroyedCount := uint32(0)
	owners := runtime.registry.hostileProjectileOwnersLocked(peerSession, sessionKey)
	for _, owner := range owners {
		objectID := owner.objectID
		run := owner.run
		snapshot := run.Snapshot(now)
		if !snapshot.IsActive || zonegeometry.Distance(
			center, game.Vec3(snapshot.Position),
		) > radius {
			continue
		}
		retirement := runtime.registry.projectileRetirements[run]
		if retirement == nil || !retirement.isAdmitted || !retirement.isPublished || retirement.isRetired || retirement.isRetirementPending {
			continue
		}
		err := runtime.registry.retireCampaignProjectileLocked(retirement)
		*peerSession = runtime.registry.sessions[sessionKey]
		if err != nil {
			return nil, destroyedCount,
				fmt.Errorf("hostileProjectileDelete[%d]: %w", objectID, err)
		}
		if !retirement.isRetired && !retirement.isRetirementPending {
			continue
		}
		// A successful unique retirement includes an in-flight producer's
		// deferred cleanup; that producer queues committed consequences first.
		destroyedCount++
	}
	return nil, destroyedCount, nil
}

func reflectHostileProjectilesLocked(
	peerSession *gameplayPeerSession, center game.Vec3, radius float32, now time.Time,
) ([][]byte, uint32, error) {
	if peerSession == nil || radius <= repulsionProjectileInnerRadius || now.IsZero() {
		return nil, 0, nil
	}
	packets := make([][]byte, 0)
	reflectedCount := uint32(0)
	for objectID, run := range peerSession.campaignNPCProjectiles {
		snapshot := run.Snapshot(now)
		if !snapshot.IsActive {
			continue
		}
		position := game.Vec3(snapshot.Position)
		delta := position.Sub(center)
		delta.Z = 0
		distance := delta.Length()
		if distance > radius {
			continue
		}
		if distance <= 0 {
			delta = game.Vec3{X: 1}
			distance = 1
		}
		direction := delta.Scale(1 / distance)
		falloff := min(
			float32(1), max(float32(0),
				(distance-repulsionProjectileInnerRadius)/
					(radius-repulsionProjectileInnerRadius),
			),
		)
		speed := repulsionProjectileMaximumSpeed -
			(repulsionProjectileMaximumSpeed-repulsionProjectileMinimumSpeed)*falloff
		if math.IsNaN(float64(speed)) || math.IsInf(float64(speed), 0) {
			return nil, reflectedCount, fmt.Errorf(
				"hostileProjectileReflectionSpeed[%d]: invalid", objectID,
			)
		}
		team := uint8(1)
		linearVelocity := raknet.Vector3{
			X: direction.X * speed, Y: direction.Y * speed,
		}
		projectilePosition := raknet.Vector3{
			X: position.X, Y: position.Y, Z: position.Z,
		}
		objectPacket, err := raknet.MarshalApplication(
			raknet.ObjectUpdateContractMessage{
				ObjectID: objectID,
				Object: raknet.ObjectReflection{
					Team: &team, Position: &projectilePosition,
					LinearVelocity: &linearVelocity,
				},
			},
		)
		if err != nil {
			return nil, reflectedCount, fmt.Errorf(
				"hostileProjectileReflectionObject[%d]: %w", objectID, err,
			)
		}
		targetObjectID := uint32(0)
		initialDirection := raknet.Vector3{
			X: direction.X, Y: direction.Y,
		}
		reflectedLastUpdate := int32(1000)
		locomotionPacket, err := raknet.MarshalApplication(
			raknet.LocomotionUpdateContractMessage{
				ObjectID: objectID,
				Locomotion: raknet.LocomotionReflection{
					Projectile: &raknet.ProjectileParameter{
						Speed: speed, Range: run.Definition().Distance,
						Direction: initialDirection,
					},
					TargetObjectID:      &targetObjectID,
					TargetPosition:      &projectilePosition,
					InitialDirection:    &initialDirection,
					ReflectedLastUpdate: &reflectedLastUpdate,
				},
			},
		)
		if err != nil {
			return nil, reflectedCount, fmt.Errorf(
				"hostileProjectileReflectionLocomotion[%d]: %w", objectID, err,
			)
		}
		run.Stop()
		peerSession.untrackCampaignNPCProjectile(objectID, run)
		packets = append(packets, objectPacket, locomotionPacket)
		reflectedCount++
	}
	return packets, reflectedCount, nil
}
