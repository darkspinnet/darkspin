package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
)

// GlobalDefinitions identifies Frozen as attribute 25. ModifierCreated only
// presents the modifier; it does not run its server-side AddAttributeModifier.
const chronoFrozenAttribute = uint8(25)

type chronoFreezeKey struct {
	zone     *zone.Zone
	objectID uint32
}

type chronoFreezeStep struct {
	runtime     campaignAbilityCommandRuntime
	key         chronoFreezeKey
	expiresAt   time.Time
	isCanceled  bool
	run         *abilityraknet.ProjectileRun
	cancel      raknet.CancelSchedule
	stopContext func() bool
}

type chronoFreezePreparation struct {
	step   *chronoFreezeStep
	cancel raknet.CancelSchedule
	packet []byte
}

func (e chronoFreezePreparation) cancelLocked() {
	if e.step != nil {
		e.step.isCanceled = true
	}
	if e.cancel != nil {
		e.cancel()
	}
}

func (e chronoFreezePreparation) commitLocked() {
	if e.step == nil {
		return
	}
	registry := e.step.runtime.registry
	if registry.chronoFreezes == nil {
		registry.chronoFreezes = make(map[chronoFreezeKey]*chronoFreezeStep)
	}
	previous := registry.chronoFreezes[e.step.key]
	if previous != nil {
		previous.cancelLocked()
	}
	e.step.cancel = e.cancel
	registry.chronoFreezes[e.step.key] = e.step
	e.step.stopContext = context.AfterFunc(e.step.key.zone.Context(), e.step.retireZone)
}

// The registry lock protects the shared zone/object deadline, including freezes
// from different party members. An older expiry must not thaw a newer freeze.
func freezeChronoObjectLocked(
	runtime campaignAbilityCommandRuntime, packet raknet.Packet,
	sessionKey string, generation uint64,
	currentZone *zone.Zone, objectID uint32, duration time.Duration,
) ([]byte, error) {
	preparation, err := prepareChronoFreezeLocked(runtime, packet, sessionKey, generation, currentZone, objectID, duration)
	if err != nil {
		return nil, fmt.Errorf("chronoFreezePrepare: %w", err)
	}
	preparation.commitLocked()
	if len(preparation.packet) != 0 {
		runtime.registry.queueProjectileRetirementLocked(currentZone, [][]byte{preparation.packet})
	}
	return nil, nil
}

func prepareChronoFreezeLocked(
	runtime campaignAbilityCommandRuntime, packet raknet.Packet,
	sessionKey string, generation uint64,
	currentZone *zone.Zone, objectID uint32, duration time.Duration, runs ...*abilityraknet.ProjectileRun,
) (chronoFreezePreparation, error) {
	if currentZone == nil || !currentZone.IsActive() || currentZone.Context() == nil || currentZone.Context().Err() != nil {
		return chronoFreezePreparation{}, errors.New("freeze zone unavailable")
	}
	key := chronoFreezeKey{zone: currentZone, objectID: objectID}
	expiresAt := runtime.now().Add(duration)
	previous := runtime.registry.chronoFreezes[key]
	if previous != nil && !expiresAt.After(previous.expiresAt) {
		return chronoFreezePreparation{}, nil
	}
	encoded, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: objectID, Value: map[uint8]float32{chronoFrozenAttribute: 1},
	})
	if err != nil {
		return chronoFreezePreparation{}, fmt.Errorf("chronoFreezeEncode: %w", err)
	}
	step := &chronoFreezeStep{
		runtime: runtime, key: key, expiresAt: expiresAt,
	}
	if len(runs) == 1 {
		step.run = runs[0]
	}
	cancel, err := scheduleSharedFreezeLocked(runtime, expiresAt, step.execute)
	if err != nil {
		step.isCanceled = true
		return chronoFreezePreparation{}, fmt.Errorf("chronoFreezeSchedule: %w", err)
	}
	if cancel == nil {
		step.isCanceled = true
		return chronoFreezePreparation{}, fmt.Errorf("chronoFreezeSchedule: missing cancellation")
	}
	return chronoFreezePreparation{step: step, cancel: cancel, packet: encoded}, nil
}

func (e *chronoFreezeStep) cancelLocked() {
	e.isCanceled = true
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	if e.stopContext != nil {
		isStopped := e.stopContext()
		if !isStopped {
			// A dispatched context callback will observe the canceled identity.
		}
		e.stopContext = nil
	}
}

func (e *chronoFreezeStep) isTargetCurrentLocked() bool {
	if !e.key.zone.IsActive() || e.key.zone.Context().Err() != nil {
		return false
	}
	if e.run != nil {
		return e.runtime.registry.isHostileProjectileCurrentLocked(e.key.zone, e.key.objectID, e.run) && e.run.Snapshot(e.runtime.now()).IsActive
	}
	if e.key.zone.NPCs() == nil {
		return false
	}
	npc, isFound := e.key.zone.NPCs().NPC(e.key.objectID)
	return isFound && !npc.IsDefeated && npc.HitPoint > 0
}

func (e *chronoFreezeStep) execute() {
	packets, err := e.expire()
	if err != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("RakNet shared freeze expiry failed object=%d: %v", e.key.objectID, err)
	}
	if len(packets) != 0 && e.runtime.logger != nil {
		e.runtime.logger.Printf("RakNet shared freeze expiry returned unqueued packets object=%d", e.key.objectID)
	}
}

func (e *chronoFreezeStep) retireZone() {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	if e.runtime.registry.chronoFreezes[e.key] != e {
		return
	}
	e.cancelLocked()
	delete(e.runtime.registry.chronoFreezes, e.key)
}

func (e *gameplaySessionRegistry) retireProjectileChronoLocked(originZone *zone.Zone, objectID uint32, run *abilityraknet.ProjectileRun) {
	key := chronoFreezeKey{zone: originZone, objectID: objectID}
	step := e.chronoFreezes[key]
	if step == nil || step.run != run {
		return
	}
	step.cancelLocked()
	delete(e.chronoFreezes, key)
}

func (e *chronoFreezeStep) expire() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	if e.isCanceled || e.runtime.registry.chronoFreezes[e.key] != e {
		return nil, nil
	}
	if !e.isTargetCurrentLocked() {
		e.cancelLocked()
		delete(e.runtime.registry.chronoFreezes, e.key)
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: e.key.objectID, Value: map[uint8]float32{chronoFrozenAttribute: 0},
	})
	if err != nil {
		return nil, fmt.Errorf("chronoThawEncode: %w", err)
	}
	e.runtime.registry.queueProjectileRetirementLocked(e.key.zone, [][]byte{packet})
	e.cancelLocked()
	delete(e.runtime.registry.chronoFreezes, e.key)
	return nil, nil
}
