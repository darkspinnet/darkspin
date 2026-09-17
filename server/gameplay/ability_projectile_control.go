package gameplay

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
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
	projectileObjectID uint32
	instanceID         uint32
	run                *abilityraknet.ProjectileRun
}

func (e hostileProjectileFreezeStep) expire() ([][]byte, error) {
	e.runtime.modifierPool.Release(e.instanceID)
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.campaignNPCProjectiles[e.projectileObjectID] == e.run
	if isCurrent {
		e.run.Thaw(e.runtime.now())
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.ModifierDeletedMessage{
		TargetID: e.projectileObjectID, InstanceID: e.instanceID,
	})
	if err != nil {
		return nil, fmt.Errorf("projectileFreezeDelete: %w", err)
	}
	return [][]byte{packet}, nil
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
	packets := make([][]byte, 0)
	for projectileObjectID, run := range peerSession.campaignNPCProjectiles {
		snapshot := run.Snapshot(now)
		if !snapshot.IsActive || zonegeometry.Distance(
			center, game.Vec3(snapshot.Position),
		) > radius || !run.Freeze(now, duration) {
			continue
		}
		instanceID, err := runtime.modifierPool.Allocate()
		if err != nil {
			run.Thaw(now.Add(duration))
			return nil, fmt.Errorf("projectileFreezeModifier[%d]: %w", projectileObjectID, err)
		}
		created, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
			TargetID: projectileObjectID, ModifierGUID: modifierID,
			InstanceID: instanceID, DurationMilliseconds: uint32(duration / time.Millisecond),
			StackCount: 1, StartMilliseconds: sourceTime, SourceID: sourceObjectID,
		})
		if err != nil {
			runtime.modifierPool.Release(instanceID)
			run.Thaw(now.Add(duration))
			return nil, fmt.Errorf("projectileFreezeCreate[%d]: %w", projectileObjectID, err)
		}
		step := hostileProjectileFreezeStep{
			runtime: runtime, sessionKey: sessionKey, generation: generation,
			projectileObjectID: projectileObjectID, instanceID: instanceID, run: run,
		}
		_, err = packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
			Delay: duration, Produce: step.expire,
		}})
		if err != nil {
			runtime.modifierPool.Release(instanceID)
			run.Thaw(now.Add(duration))
			return nil, fmt.Errorf("projectileFreezeSchedule[%d]: %w", projectileObjectID, err)
		}
		packets = append(packets, created)
	}
	return packets, nil
}

func destroyHostileProjectilesLocked(
	peerSession *gameplayPeerSession, center game.Vec3, radius float32, now time.Time,
) ([][]byte, uint32, error) {
	if peerSession == nil || radius <= 0 || now.IsZero() {
		return nil, 0, nil
	}
	packets := make([][]byte, 0)
	destroyedCount := uint32(0)
	for objectID, run := range peerSession.campaignNPCProjectiles {
		snapshot := run.Snapshot(now)
		if !snapshot.IsActive || zonegeometry.Distance(
			center, game.Vec3(snapshot.Position),
		) > radius {
			continue
		}
		deletePackets, isDeleted, err := run.DeleteProjectile(context.Background())
		if err != nil {
			return nil, destroyedCount,
				fmt.Errorf("hostileProjectileDelete[%d]: %w", objectID, err)
		}
		if !isDeleted {
			continue
		}
		peerSession.untrackCampaignNPCProjectile(objectID, run)
		packets = append(packets, deletePackets...)
		destroyedCount++
	}
	return packets, destroyedCount, nil
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
