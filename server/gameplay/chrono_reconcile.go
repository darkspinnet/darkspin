package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
)

// Modifier release can already hold the registry lock. Dispatching this named
// operation lets it project the latest surviving contributors after release.
type npcChronoReconciliation struct {
	runtime campaignAbilityCommandRuntime
	key     chronoFreezeKey
}

func (e *gameplaySessionRegistry) reconcileNPCChronoLocked(originZone *zone.Zone, objectID uint32) {
	key := chronoFreezeKey{zone: originZone, objectID: objectID}
	step := e.chronoFreezes[key]
	if step == nil || step.run != nil {
		return
	}
	reconcile := npcChronoReconciliation{runtime: step.runtime, key: key}
	err := reconcile.reconcileLocked()
	if err != nil {
		reconcile.retry(err)
	}
}

func (e npcChronoReconciliation) schedule(delay time.Duration) error {
	if e.runtime.registry.timer == nil {
		return errors.New("chrono reconciliation timer unavailable")
	}
	cancel, err := e.runtime.registry.timer.Schedule(delay, e.execute)
	if err != nil {
		return fmt.Errorf("chronoReconcileSchedule: %w", err)
	}
	if cancel == nil {
		return errors.New("chrono reconciliation cancellation unavailable")
	}
	return nil
}

func (e npcChronoReconciliation) retry(err error) {
	if e.runtime.logger != nil {
		e.runtime.logger.Printf("NPC time stop reconciliation deferred object=%d: %v", e.key.objectID, err)
	}
	// Keep the pending projection when the registry timer rejects scheduling.
	// The named operation drops itself when its zone is no longer active.
	retryTimer := time.AfterFunc(50*time.Millisecond, e.execute)
	if retryTimer == nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("NPC time stop reconciliation retry unavailable object=%d", e.key.objectID)
	}
}

func (e npcChronoReconciliation) execute() {
	e.runtime.registry.mutex.Lock()
	err := e.reconcileLocked()
	e.runtime.registry.mutex.Unlock()
	if err != nil {
		e.retry(err)
	}
}

func (e npcChronoReconciliation) reconcileLocked() error {
	step := e.runtime.registry.chronoFreezes[e.key]
	if step == nil || step.run != nil {
		return nil
	}
	if !e.key.zone.IsActive() || e.key.zone.Context().Err() != nil {
		step.cancelLocked()
		delete(e.runtime.registry.chronoFreezes, e.key)
		return nil
	}
	expiresAt := e.key.zone.NPCs().TimeStopEnd(e.key.objectID)
	if expiresAt.After(e.runtime.now()) {
		if expiresAt.Equal(step.expiresAt) {
			return nil
		}
		cancel, err := scheduleSharedFreezeLocked(e.runtime, expiresAt, step.execute)
		if err != nil {
			return fmt.Errorf("chronoReproject: %w", err)
		}
		if cancel == nil {
			return errors.New("chrono reprojection cancellation unavailable")
		}
		if step.cancel != nil {
			step.cancel()
		}
		step.cancel = cancel
		step.expiresAt = expiresAt
		return nil
	}
	packet, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: e.key.objectID, Value: map[uint8]float32{chronoFrozenAttribute: 0},
	})
	if err != nil {
		return fmt.Errorf("chronoReconcileEncode: %w", err)
	}
	e.runtime.registry.queueProjectileRetirementLocked(e.key.zone, [][]byte{packet})
	step.cancelLocked()
	delete(e.runtime.registry.chronoFreezes, e.key)
	return nil
}
