package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

func (e *spoffitRun) releaseCreatorLocked() ([][]byte, error) {
	if e.instanceID == 0 {
		return nil, nil
	}
	packet, err := effectraknet.ModifierDelete(e.ownerObjectID, e.instanceID)
	if err != nil {
		return nil, fmt.Errorf("spoffitModifierDelete: %w", err)
	}
	err = e.runtime.modifierPool.Release(e.instanceID)
	if err != nil {
		return nil, fmt.Errorf("spoffitModifierRelease: %w", err)
	}
	removed, isRemoved := e.world.Effect().Remove(e.instanceID)
	if isRemoved && removed.TargetObjectID != e.ownerObjectID {
		e.runtime.logger.Printf("Spoffit modifier target mismatch instance=%d target=%d", e.instanceID, removed.TargetObjectID)
	}
	e.instanceID = 0
	if !e.isCreatorPublished {
		return nil, nil
	}
	return [][]byte{packet}, nil
}

// Explicit build103 content repair:960.Deactivate reads burrowModifer, while
// its table defines burrowModifier. Translate that exact misspelling to its
// authored LifeTempestPetBurrow130 instead of changing installed Lua assets.
func (e *spoffitRun) beginBurrowLocked(now time.Time) ([][]byte, error) {
	if e.isStopped || e.isBurrowing {
		return nil, nil
	}
	packets, err := e.releaseCreatorLocked()
	if err != nil {
		return nil, fmt.Errorf("spoffitCreatorRetire: %w", err)
	}
	actor, isFound := e.world.Companion().Snapshot(e.objectID)
	if !isFound || actor.HitPoint <= 0 {
		deleted, deleteErr := e.deleteLocked()
		if deleteErr != nil {
			return packets, fmt.Errorf("spoffitMissingDelete: %w", deleteErr)
		}
		return append(packets, deleted...), nil
	}
	stop, err := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{ObjectID: e.objectID, GoalFlags: 0x20})
	if err != nil {
		return packets, fmt.Errorf("spoffitBurrowStop: %w", err)
	}
	animation, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
		ObjectID: e.objectID, State: util.HashID(sageBurrowAnimation),
		Timestamp: advanceHeroArrivalTimestamp(e.sourceTime, e.startedAt, now), Scale: 1,
	})
	if err != nil {
		return packets, fmt.Errorf("spoffitBurrowAnimation: %w", err)
	}
	err = e.world.Companion().Recall(e.objectID, now, now.Add(sageBurrowDuration), sageBurrowAnimation)
	if err != nil {
		return packets, fmt.Errorf("spoffitBurrowControl: %w", err)
	}
	e.isBurrowing = true
	e.burrowEndsAt = now.Add(sageBurrowDuration)
	e.follow = nil
	e.runtime.registry.recordSpoffitRemovalLocked(e.removalRequestID, e.objectID, e.ownerObjectID, false)
	return append(packets, stop, animation), nil
}

func (e *spoffitRun) produceRemoval() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	if e.isStopped || e.isRemovalPublished {
		return nil, nil
	}
	if e.removalPackets != nil {
		return clonePendingPackets(e.removalPackets), nil
	}
	e.isRemovalProducing = true
	packets, err := e.beginBurrowLocked(e.runtime.now())
	e.isRemovalProducing = false
	if err != nil {
		return nil, fmt.Errorf("spoffitRemovalPrepare: %w", err)
	}
	e.removalPackets = packets
	return clonePendingPackets(packets), nil
}

func (e *spoffitRun) commitRemoval() {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	if e.isRemovalPublished || (e.isStopped && len(e.removalPackets) == 0) {
		return
	}
	// The scheduled source write contains the real A4/stop/burrow batch.
	// An empty gameplay response cannot strand this transition, and observers
	// receive it before the independent world timer may publish child deletion.
	e.queueLocked(e.removalPackets, false)
	e.isRemovalPublished = true
	e.birthCancel = nil
	if e.isStopped {
		e.runtime.registry.recordSpoffitRemovalLocked(e.removalRequestID, e.objectID, e.ownerObjectID, true)
	}
}

func (e *spoffitRun) deleteLocked() ([][]byte, error) {
	if e.isStopped {
		return nil, nil
	}
	packets, err := e.releaseCreatorLocked()
	if err != nil {
		return nil, fmt.Errorf("spoffitFinalModifier: %w", err)
	}
	if e.objectID != 0 {
		deleted, marshalErr := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{e.objectID}})
		if marshalErr != nil {
			return packets, fmt.Errorf("spoffitObjectDelete: %w", marshalErr)
		}
		e.world.Companion().Remove(e.objectID)
		packets = append(packets, deleted)
	}
	e.isStopped = true
	delete(e.runtime.registry.spoffits, e)
	if e.pollCancel != nil {
		e.pollCancel()
		e.pollCancel = nil
	}
	// Packet schedule cancellation is a nonblocking context cancellation;
	// its failure callback takes the registry lock after this transaction.
	if e.birthCancel != nil && !e.isRemovalProducing {
		e.birthCancel()
		e.birthCancel = nil
	}
	e.runtime.registry.recordSpoffitRemovalLocked(e.requestID, e.objectID, e.ownerObjectID, true)
	if e.isRemovalPublished {
		e.runtime.registry.recordSpoffitRemovalLocked(e.removalRequestID, e.objectID, e.ownerObjectID, true)
	}
	return packets, nil
}
