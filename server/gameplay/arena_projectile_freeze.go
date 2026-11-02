package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

// Chrono Shift freezes hostile Arena flights owned by their actual player.
// The existing projectile retirement path releases these modifier leases if
// a flight ends before the authored three-second thaw.
func freezeArenaHostileProjectilesLocked(
	runtime campaignAbilityCommandRuntime, source *gameplayPeerSession,
	sessionKey string, sourceObjectID uint32, center game.Vec3,
	radius float32, modifierID uint32, duration time.Duration,
	sourceTime uint64, now time.Time,
) ([][]byte, error) {
	match := runtime.registry.arenaMatches[source.binding.GameID]
	if match == nil || !match.isCombatActive || runtime.registry.timer == nil {
		return nil, nil
	}
	owners := runtime.registry.hostileProjectileOwnersLocked(source, sessionKey)
	for _, owner := range owners {
		snapshot := owner.run.Snapshot(now)
		if !snapshot.IsActive || zonegeometry.Distance(center, game.Vec3(snapshot.Position)) > radius {
			continue
		}
		reservation, isReserved := owner.run.ReserveFreeze(now, duration)
		if !isReserved {
			continue
		}
		instanceID, err := runtime.modifierPool.Allocate()
		if err != nil {
			owner.run.RollbackFreeze(reservation)
			return nil, fmt.Errorf("arenaFreezeAllocate: %w", err)
		}
		created, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
			TargetID: owner.objectID, ModifierGUID: modifierID,
			InstanceID: instanceID, DurationMilliseconds: uint32(duration / time.Millisecond),
			StackCount: 1, StartMilliseconds: sourceTime, NPCAffixAssetID: 0,
		})
		if err != nil {
			owner.run.RollbackFreeze(reservation)
			releaseErr := runtime.modifierPool.Release(instanceID)
			return nil, fmt.Errorf("arenaFreezeCreate: %w", errors.Join(err, releaseErr))
		}
		attribute, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
			ObjectID: owner.objectID, Value: map[uint8]float32{chronoFrozenAttribute: 1},
		})
		if err != nil {
			owner.run.RollbackFreeze(reservation)
			releaseErr := runtime.modifierPool.Release(instanceID)
			return nil, fmt.Errorf("arenaFreezeAttribute: %w", errors.Join(err, releaseErr))
		}
		step := &hostileProjectileFreezeStep{
			runtime: runtime, sessionKey: sessionKey, generation: source.generation,
			userID: source.binding.UserID, projectileObjectID: owner.objectID,
			instanceID: instanceID, run: owner.run, arenaGameID: source.binding.GameID,
			ownerSessionKey: owner.sessionKey, ownerGeneration: owner.generation,
			expiresAt: now.Add(duration),
		}
		cancel, scheduleErr := scheduleSharedFreezeLocked(runtime, step.expiresAt, step.execute)
		if scheduleErr != nil || cancel == nil {
			owner.run.RollbackFreeze(reservation)
			releaseErr := runtime.modifierPool.Release(instanceID)
			return nil, fmt.Errorf("arenaFreezeSchedule: %w", errors.Join(scheduleErr, releaseErr))
		}
		step.cancel = cancel
		if !owner.run.CommitFreeze(reservation) {
			cancel()
			releaseErr := runtime.modifierPool.Release(instanceID)
			return nil, fmt.Errorf("arenaFreezeCommit: %w", errors.Join(errors.New("reservation changed"), releaseErr))
		}
		if runtime.registry.projectileFreezes == nil {
			runtime.registry.projectileFreezes = make(map[*abilityraknet.ProjectileRun]map[uint32]*hostileProjectileFreezeStep)
		}
		if runtime.registry.projectileFreezes[owner.run] == nil {
			runtime.registry.projectileFreezes[owner.run] = make(map[uint32]*hostileProjectileFreezeStep)
		}
		runtime.registry.projectileFreezes[owner.run][instanceID] = step
		runtime.registry.queueArenaForcePulsePacketsLocked(source.binding.GameID, [][]byte{created, attribute})
	}
	return nil, nil
}
