package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

type fieldMedicCompanionBuffRun struct {
	instanceID     uint32
	targetObjectID uint32
	buff           zonecompanion.Buff
	cancel         raknet.CancelSchedule
}

func (e *gameplayPeerSession) stopFieldMedicCompanionBuffs(pool *modifierPool) {
	if e == nil {
		return
	}
	for instanceID, run := range e.fieldMedicCompanionBuffs {
		if run.cancel != nil {
			run.cancel()
			run.cancel = nil
		}
		if e.zone != nil {
			e.zone.Companion().RemoveBuff(run.targetObjectID, run.buff)
			e.zone.Effect().Remove(instanceID)
		}
		if pool != nil {
			_ = pool.Release(instanceID)
		}
		delete(e.fieldMedicCompanionBuffs, instanceID)
	}
}

type fieldMedicCompanionBuffExpiry struct {
	runtime    campaignAbilityCommandRuntime
	sessionKey string
	generation uint64
	run        *fieldMedicCompanionBuffRun
}

func (e fieldMedicCompanionBuffExpiry) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.fieldMedicCompanionBuffs[e.run.instanceID] == e.run &&
		peerSession.zone != nil
	if isCurrent {
		peerSession.zone.Companion().RemoveBuff(e.run.targetObjectID, e.run.buff)
		peerSession.zone.Effect().Remove(e.run.instanceID)
		delete(peerSession.fieldMedicCompanionBuffs, e.run.instanceID)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	err := e.runtime.modifierPool.Release(e.run.instanceID)
	if err != nil {
		return nil, fmt.Errorf("fieldMedicCompanionRelease: %w", err)
	}
	packet, err := effectraknet.ModifierDelete(
		e.run.targetObjectID, e.run.instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("fieldMedicCompanionDelete: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e fieldMedicActiveSchedule) transferBuffsToCompanionsLocked(
	peerSession *gameplayPeerSession, center game.Vec3,
	modifiers []zoneeffect.Modifier,
) ([][]byte, []uint32, error) {
	packets := make([][]byte, 0)
	targetObjectIDs := make([]uint32, 0)
	for _, actor := range peerSession.zone.Companion().Snapshots() {
		if !actor.IsTargetable || actor.HitPoint <= 0 ||
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
		_ = e.runtime.modifierPool.Release(instanceID)
		return nil, false, fmt.Errorf("create: %w", err)
	}
	err = peerSession.zone.Companion().AddBuff(targetObjectID, buff)
	if err != nil {
		_ = e.runtime.modifierPool.Release(instanceID)
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
		_ = e.runtime.modifierPool.Release(instanceID)
		return nil, false, fmt.Errorf("inventory: %w", err)
	}
	expiry := fieldMedicCompanionBuffExpiry{
		runtime: e.runtime, sessionKey: e.sessionKey,
		generation: e.generation, run: run,
	}
	cancel, scheduleErr := e.packet.ScheduleProducers(
		[]raknet.ScheduledPacketProducer{{
			Delay: modifier.Duration, Produce: expiry.produce,
		}},
	)
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		peerSession.zone.Effect().Remove(instanceID)
		peerSession.zone.Companion().RemoveBuff(targetObjectID, buff)
		delete(peerSession.fieldMedicCompanionBuffs, instanceID)
		_ = e.runtime.modifierPool.Release(instanceID)
		return nil, false, fmt.Errorf("schedule: %w", scheduleErr)
	}
	run.cancel = cancel
	return packet, true, nil
}
