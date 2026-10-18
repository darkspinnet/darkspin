package gameplay

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

type fieldMedicCompanionBuffRun struct {
	mutex          sync.Mutex
	zone           *zone.Zone
	userID         uint64
	generation     uint64
	ownerObjectID  uint32
	expiresAt      time.Time
	isRetired      bool
	isReleased     bool
	instanceID     uint32
	targetObjectID uint32
	buff           zonecompanion.Buff
	cancel         raknet.CancelSchedule
}

func (e *gameplayPeerSession) stopFieldMedicCompanionBuffs(pool *modifierPool) ([][]byte, error) {
	if e == nil {
		return nil, nil
	}
	packets := make([][]byte, 0)
	var retirementErr error
	for _, run := range e.fieldMedicCompanionBuffs {
		retiredPackets, err := run.retire()
		packets = append(packets, retiredPackets...)
		if err != nil {
			retirementErr = errors.Join(retirementErr, err)
		}
	}
	if retirementErr != nil {
		return packets, fmt.Errorf("companionRetire: %w", retirementErr)
	}
	return packets, nil
}

type fieldMedicCompanionBuffExpiry struct {
	runtime campaignAbilityCommandRuntime
	run     *fieldMedicCompanionBuffRun
}

func (e fieldMedicCompanionBuffExpiry) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	packets, err := e.run.retire()
	e.runtime.registry.queueFieldMedicPacketsLocked(e.run.zone, packets)
	releaseErr := e.run.release(e.runtime.modifierPool)
	if releaseErr != nil {
		err = errors.Join(err, releaseErr)
	}
	if e.run.isRetired {
		for sessionKey, candidate := range e.runtime.registry.sessions {
			if candidate.fieldMedicCompanionBuffs[e.run.instanceID] == e.run {
				delete(candidate.fieldMedicCompanionBuffs, e.run.instanceID)
				e.runtime.registry.sessions[sessionKey] = candidate
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("companionExpiry: %w", err)
	}
	return nil, nil
}

func (e fieldMedicCompanionBuffExpiry) execute() {
	packets, err := e.produce()
	if err != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("RakNet Field Medic pet expiry failed instance=%d: %v", e.run.instanceID, err)
	}
	if len(packets) != 0 && e.runtime.logger != nil {
		e.runtime.logger.Printf("RakNet Field Medic pet expiry returned unqueued packets instance=%d", e.run.instanceID)
	}
	e.run.retryRetirement(e.runtime, e.execute)
}

func (e fieldMedicActiveSchedule) transferBuffsToCompanionsLocked(
	peerSession *gameplayPeerSession, center game.Vec3,
	modifiers []zoneeffect.Modifier,
) ([][]byte, []uint32, error) {
	packets := make([][]byte, 0)
	targetObjectIDs := make([]uint32, 0)
	for _, actor := range peerSession.zone.Companion().Snapshots() {
		if !e.runtime.registry.isActivePartyCompanionLocked(actor, *peerSession) ||
			!actor.IsTargetable || actor.HitPoint <= 0 ||
			zonegeometry.Distance(center, actor.Position) > e.definition.Radius {
			continue
		}
		isTransferred := false
		for _, modifier := range modifiers {
			packet, isAdded, err := e.transferBuffToCompanionLocked(
				peerSession, actor.ObjectID, modifier,
			)
			if err != nil {
				return nil, nil, fmt.Errorf("target[%d]: %w", actor.ObjectID, err)
			}
			if !isAdded {
				continue
			}
			packets = append(packets, packet)
			isTransferred = true
		}
		if isTransferred {
			targetObjectIDs = append(targetObjectIDs, actor.ObjectID)
		}
	}
	return packets, targetObjectIDs, nil
}

func (e fieldMedicActiveSchedule) transferBuffToCompanionLocked(
	peerSession *gameplayPeerSession, targetObjectID uint32,
	modifier zoneeffect.Modifier,
) ([]byte, bool, error) {
	if peerSession == nil || peerSession.zone == nil {
		return nil, false, nil
	}
	actor, isActorFound := peerSession.zone.Companion().Snapshot(targetObjectID)
	if !isActorFound || !actor.IsTargetable || actor.HitPoint <= 0 ||
		!e.runtime.registry.isActivePartyCompanionLocked(actor, *peerSession) {
		return nil, false, nil
	}
	buff := zonecompanion.Buff{
		DamageBuff:        modifier.DamageBuff,
		EnergyDamageBuff:  modifier.EnergyDamageBuff,
		AttackSpeed:       modifier.AttackSpeed,
		CooldownReduction: modifier.CooldownReduction,
		MovementSpeedBuff: modifier.MovementSpeedBuff,
	}
	if buff == (zonecompanion.Buff{}) {
		return nil, false, nil
	}
	instanceID, err := e.runtime.modifierPool.Allocate()
	if err != nil {
		return nil, false, fmt.Errorf("allocate: %w", err)
	}
	run := &fieldMedicCompanionBuffRun{
		zone: peerSession.zone, userID: actor.UserID, generation: actor.PeerGeneration,
		ownerObjectID: actor.OwnerObjectID, expiresAt: e.runtime.now().Add(modifier.Duration),
		instanceID: instanceID, targetObjectID: targetObjectID, buff: buff,
	}
	packet, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: e.sourceObjectID, TargetObjectID: targetObjectID,
		ModifierID: modifier.GUID, InstanceID: instanceID,
		Duration: modifier.Duration, StackCount: modifier.StackCount,
		Timestamp: e.packet.SourceTime +
			uint64(e.definition.HitDelay/time.Millisecond),
	})
	if err != nil {
		releaseErr := e.runtime.modifierPool.Release(instanceID)
		err = errors.Join(err, releaseErr)
		return nil, false, fmt.Errorf("create: %w", err)
	}
	err = peerSession.zone.Companion().AddBuff(targetObjectID, buff)
	if err != nil {
		releaseErr := e.runtime.modifierPool.Release(instanceID)
		err = errors.Join(err, releaseErr)
		return nil, false, fmt.Errorf("apply: %w", err)
	}
	if peerSession.fieldMedicCompanionBuffs == nil {
		peerSession.fieldMedicCompanionBuffs =
			make(map[uint32]*fieldMedicCompanionBuffRun)
	}
	peerSession.fieldMedicCompanionBuffs[instanceID] = run
	record := modifier
	record.InstanceID = instanceID
	record.SourceObjectID = e.sourceObjectID
	record.TargetObjectID = targetObjectID
	record.Kind = zoneeffect.ModifierKindBuff
	err = peerSession.zone.Effect().Put(record)
	if err != nil {
		peerSession.zone.Companion().RemoveBuff(targetObjectID, buff)
		delete(peerSession.fieldMedicCompanionBuffs, instanceID)
		releaseErr := e.runtime.modifierPool.Release(instanceID)
		err = errors.Join(err, releaseErr)
		return nil, false, fmt.Errorf("inventory: %w", err)
	}
	expiry := fieldMedicCompanionBuffExpiry{
		runtime: e.runtime, run: run,
	}
	cancel, scheduleErr := scheduleFieldMedicExpiry(e.runtime, run.expiresAt, expiry.execute)
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		peerSession.zone.Effect().Remove(instanceID)
		peerSession.zone.Companion().RemoveBuff(targetObjectID, buff)
		delete(peerSession.fieldMedicCompanionBuffs, instanceID)
		releaseErr := e.runtime.modifierPool.Release(instanceID)
		err = errors.Join(err, releaseErr)
		return nil, false, fmt.Errorf("schedule: %w", errors.Join(scheduleErr, err))
	}
	run.cancel = cancel
	return packet, true, nil
}
