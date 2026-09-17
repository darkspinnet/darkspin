package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

const missileFlakDuration = 10 * time.Second

type missileFlakRun struct {
	creatureIndex uint32
	objectID      uint32
	instanceID    uint32
	stackCount    uint32
	attackSpeed   float32
	cancel        raknet.CancelSchedule
}

type missileFlakExpiry struct {
	runtime    campaignAbilityCommandRuntime
	sessionKey string
	generation uint64
	run        *missileFlakRun
}

func (e *missileFlakRun) cancelSchedule() {
	if e == nil || e.cancel == nil {
		return
	}
	e.cancel()
	e.cancel = nil
}

func (e *missileFlakRun) remove(peerSession *gameplayPeerSession) {
	if e == nil || peerSession == nil ||
		e.creatureIndex >= uint32(len(peerSession.binding.Creatures)) {
		return
	}
	creature := &peerSession.binding.Creatures[e.creatureIndex]
	creature.TimingProfile.AttackSpeed -= e.attackSpeed
	e.attackSpeed = 0
}

func (e missileFlakExpiry) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.missileFlakRun == e.run
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	e.run.remove(&peerSession)
	peerSession.missileFlakRun = nil
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()

	_ = e.runtime.modifierPool.Release(e.run.instanceID)
	packet, err := effectraknet.ModifierDelete(e.run.objectID, e.run.instanceID)
	if err != nil {
		return nil, fmt.Errorf("missileFlakDelete: %w", err)
	}
	return [][]byte{packet}, nil
}

func (r campaignAbilityCommandRuntime) applyMissileFlakLocked(
	packet raknet.Packet, peerSession *gameplayPeerSession, sessionKey string,
	generation uint64, rank int32, destroyedCount uint32, timestamp uint64,
) ([][]byte, error) {
	if rank != 7 && rank != 8 {
		return nil, nil
	}
	if peerSession == nil || r.modifierPool == nil ||
		(packet.ScheduleGroup == nil && packet.ScheduleGroupResult == nil) {
		return nil, errors.New("missile Flak runtime unavailable")
	}
	packets := make([][]byte, 0, 2)
	if peerSession.missileFlakRun != nil {
		previousRun := peerSession.missileFlakRun
		previousRun.cancelSchedule()
		previousRun.remove(peerSession)
		peerSession.missileFlakRun = nil
		_ = r.modifierPool.Release(previousRun.instanceID)
		deletePacket, err := effectraknet.ModifierDelete(
			previousRun.objectID, previousRun.instanceID,
		)
		if err != nil {
			return nil, fmt.Errorf("missileFlakReplace: %w", err)
		}
		packets = append(packets, deletePacket)
	}
	maximumStack := uint32(10)
	attackSpeedPerStack := float32(0.05)
	if rank == 8 {
		maximumStack = 4
		attackSpeedPerStack = 0.12
	}
	stackCount := min(destroyedCount, maximumStack)
	if stackCount == 0 {
		return packets, nil
	}
	instanceID, err := r.modifierPool.Allocate()
	if err != nil {
		return nil, fmt.Errorf("missileFlakAllocate: %w", err)
	}
	run := &missileFlakRun{
		creatureIndex: peerSession.deployedCreatureIndex,
		objectID:      peerSession.deployedObjectID,
		instanceID:    instanceID,
		stackCount:    stackCount,
		attackSpeed:   float32(stackCount) * attackSpeedPerStack,
	}
	peerSession.binding.Creatures[run.creatureIndex].TimingProfile.AttackSpeed +=
		run.attackSpeed
	createdPacket, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID: run.objectID,
		ModifierGUID: util.HashID(
			"MissileTempest_Support_FlakUpgradeModifier",
		),
		InstanceID: instanceID, DurationMilliseconds: uint32(missileFlakDuration.Milliseconds()),
		StackCount: stackCount, StartMilliseconds: timestamp, SourceID: run.objectID,
	})
	if err != nil {
		run.remove(peerSession)
		_ = r.modifierPool.Release(instanceID)
		return nil, fmt.Errorf("missileFlakCreate: %w", err)
	}
	peerSession.missileFlakRun = run
	expiry := missileFlakExpiry{
		runtime: r, sessionKey: sessionKey, generation: generation, run: run,
	}
	cancel, err := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: missileFlakDuration, Produce: expiry.produce,
	}})
	if err != nil {
		run.remove(peerSession)
		peerSession.missileFlakRun = nil
		_ = r.modifierPool.Release(instanceID)
		return nil, fmt.Errorf("missileFlakSchedule: %w", err)
	}
	run.cancel = cancel
	return append(packets, createdPacket), nil
}

func stopMissileFlak(
	peerSession *gameplayPeerSession, modifierPool *modifierPool,
) ([]byte, error) {
	if peerSession == nil || peerSession.missileFlakRun == nil {
		return nil, nil
	}
	run := peerSession.missileFlakRun
	run.cancelSchedule()
	run.remove(peerSession)
	peerSession.missileFlakRun = nil
	if modifierPool != nil {
		_ = modifierPool.Release(run.instanceID)
	}
	packet, err := effectraknet.ModifierDelete(run.objectID, run.instanceID)
	if err != nil {
		return nil, fmt.Errorf("missileFlakStop: %w", err)
	}
	return packet, nil
}
