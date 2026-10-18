package gameplay

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

const hardenedMantleDuration = 6 * time.Second

// Modifiers/0x0A09D13C.lua publishes a reducer and force-attached hit effect
// on activation and each successful stack increment. Hits at the cap only
// reset its duration. The run owns the two presentation leases together.
type hardenedMantleRun struct {
	mutex        sync.Mutex
	modifierPool *modifierPool
	effectPool   *attachedEffectPool
	objectID     uint32
	instanceID   uint32
	effectSlot   uint8
	stackCount   uint32
	expiresAt    time.Time
	revision     uint64
	isStopped    bool
	cancel       raknet.CancelSchedule
}

type hardenedMantleExpiry struct {
	run      *hardenedMantleRun
	revision uint64
}

func (e hardenedMantleExpiry) produce() ([][]byte, error) {
	e.run.mutex.Lock()
	defer e.run.mutex.Unlock()
	if e.run.isStopped || e.run.revision != e.revision {
		return nil, nil
	}
	// The producer must publish its removal packets before its schedule ends.
	e.run.cancel = nil
	packets, err := e.run.stopLocked()
	if err != nil {
		return nil, fmt.Errorf("mantleExpiry: %w", err)
	}
	return packets, nil
}

func (e *hardenedMantleRun) stopLocked() ([][]byte, error) {
	if e.isStopped {
		return nil, nil
	}
	e.isStopped = true
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	releaseErr := e.modifierPool.Release(e.instanceID)
	if !e.effectPool.Release(e.objectID, e.effectSlot) {
		// Hero teardown can release every object attachment before this run.
	}
	deletedPacket, deleteErr := raknet.MarshalApplication(raknet.ModifierDeletedMessage{
		TargetID: e.objectID, InstanceID: e.instanceID,
	})
	removedPacket, removeErr := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: e.effectSlot + 1, ObjectID: e.objectID,
		IsRemovalRequested: true, IsHardStop: true,
	})
	err := errors.Join(releaseErr, deleteErr, removeErr)
	if err != nil {
		return nil, fmt.Errorf("mantleRelease: %w", err)
	}
	return [][]byte{deletedPacket, removedPacket}, nil
}

func (e *gameplayPeerSession) stopHardenedMantle() {
	if e == nil || e.hardenedMantleRun == nil {
		return
	}
	run := e.hardenedMantleRun
	e.hardenedMantleRun = nil
	run.mutex.Lock()
	packets, err := run.stopLocked()
	run.mutex.Unlock()
	if err != nil {
		log.Printf("Hardened Mantle cleanup object=%d: %v", run.objectID, err)
		return
	}
	e.queuePackets(packets)
}

func (e *gameplayPeerSession) presentHardenedMantle(
	modifierPool *modifierPool, effectPool *attachedEffectPool,
	timestamp uint64,
) ([][]byte, error) {
	if e == nil || e.deployedCreatureIndex >= uint32(len(e.binding.Creatures)) ||
		e.binding.Creatures[e.deployedCreatureIndex].PassiveAbility != util.HashID("PlasmaSentinelPassive") ||
		e.passiveReductionStack[e.deployedCreatureIndex] == 0 {
		return nil, nil
	}
	if modifierPool == nil || effectPool == nil || e.schedulePacket.ScheduleProducers == nil {
		return nil, errors.New("mantle presentation unavailable")
	}
	packets := make([][]byte, 0, 4)
	expiresAt := e.passiveReductionExpiresAt[e.deployedCreatureIndex]
	hitTime := expiresAt.Add(-hardenedMantleDuration)
	run := e.hardenedMantleRun
	if run != nil {
		run.mutex.Lock()
		if !run.isStopped && !hitTime.Before(run.expiresAt) {
			stopPackets, err := run.stopLocked()
			if err != nil {
				run.mutex.Unlock()
				return nil, fmt.Errorf("mantleReplace: %w", err)
			}
			packets = append(packets, stopPackets...)
		}
		if run.isStopped {
			run.mutex.Unlock()
			run = nil
		}
	}
	isNew := run == nil
	if isNew {
		instanceID, err := modifierPool.Allocate()
		if err != nil {
			return nil, fmt.Errorf("mantleAllocate: %w", err)
		}
		effectSlot, isAllocated := effectPool.Allocate(e.deployedObjectID)
		if !isAllocated {
			releaseErr := modifierPool.Release(instanceID)
			return nil, fmt.Errorf("mantleSlot: %w", errors.Join(errors.New("attachment unavailable"), releaseErr))
		}
		run = &hardenedMantleRun{
			modifierPool: modifierPool, effectPool: effectPool,
			objectID: e.deployedObjectID, instanceID: instanceID, effectSlot: effectSlot,
		}
		e.hardenedMantleRun = run
		run.mutex.Lock()
	}
	defer run.mutex.Unlock()
	stackCount := e.passiveReductionStack[e.deployedCreatureIndex]
	var message raknet.ApplicationMessage = raknet.ModifierUpdatedMessage{
		TargetID: run.objectID, InstanceID: run.instanceID,
		StartMilliseconds: int64(timestamp), StackCount: stackCount,
	}
	if isNew {
		message = raknet.ModifierCreatedMessage{
			TargetID: run.objectID, ModifierGUID: util.HashID("PlasmaSentinelDamageReducer"),
			InstanceID: run.instanceID, DurationMilliseconds: uint32(hardenedMantleDuration.Milliseconds()),
			StartMilliseconds: timestamp, StackCount: stackCount,
		}
	}
	modifierPacket, err := raknet.MarshalApplication(message)
	if err != nil {
		return nil, fmt.Errorf("mantleModifier: %w", err)
	}
	packets = append(packets, modifierPacket)
	if stackCount > run.stackCount {
		effectPacket, effectErr := raknet.MarshalApplication(raknet.AttachedEffectMessage{
			Slot: run.effectSlot + 1, ObjectID: run.objectID, IsForceAttached: true,
			Asset: util.HashID("plasmasentinel_passive_takedamage.ServerEventDef"),
		})
		if effectErr != nil {
			return nil, fmt.Errorf("mantleEffect: %w", effectErr)
		}
		packets = append(packets, effectPacket)
	}
	run.revision++
	expiry := hardenedMantleExpiry{run: run, revision: run.revision}
	cancel, err := e.schedulePacket.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: hardenedMantleDuration, Produce: expiry.produce,
	}})
	if err == nil && cancel == nil {
		err = errors.New("mantle cancellation unavailable")
	}
	if err != nil {
		stopPackets, stopErr := run.stopLocked()
		e.queuePackets(stopPackets)
		return nil, fmt.Errorf("mantleSchedule: %w", errors.Join(err, stopErr))
	}
	if run.cancel != nil {
		run.cancel()
	}
	run.cancel = cancel
	run.stackCount = stackCount
	run.expiresAt = expiresAt
	return packets, nil
}
