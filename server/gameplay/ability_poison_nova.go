package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

const poisonNovaCooldownModifierName = "LFPoisonRavager_Support_CooldownUpgradeModifier"

type poisonNovaCooldownRun struct {
	objectID   uint32
	instanceID uint32
	expiresAt  time.Time
	cancel     raknet.CancelSchedule
}

type poisonNovaCooldownExpiry struct {
	runtime    campaignAbilityCommandRuntime
	sessionKey string
	generation uint64
	run        *poisonNovaCooldownRun
}

func (e *poisonNovaCooldownRun) cancelSchedule() {
	if e == nil || e.cancel == nil {
		return
	}
	e.cancel()
	e.cancel = nil
}

func (e poisonNovaCooldownExpiry) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.poisonNovaCooldownRun == e.run
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	peerSession.poisonNovaCooldownRun = nil
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()

	_ = e.runtime.modifierPool.Release(e.run.instanceID)
	packet, err := effectraknet.ModifierDelete(e.run.objectID, e.run.instanceID)
	if err != nil {
		return nil, fmt.Errorf("poisonNovaDelete: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e campaignAbilityCommandRuntime) applyPoisonNovaCooldownResetLocked(
	packet raknet.Packet, peerSession *gameplayPeerSession, sessionKey string,
	generation uint64, abilityID uint32, rank int32,
	reservation zoneability.CooldownReservation, duration time.Duration,
	timestamp uint64,
) ([][]byte, error) {
	if rank != 5 && rank != 6 {
		return nil, nil
	}
	if peerSession == nil || abilityID == 0 || duration <= 0 ||
		e.modifierPool == nil ||
		(packet.ScheduleGroup == nil && packet.ScheduleGroupResult == nil) {
		return nil, errors.New("poison Nova cooldown runtime unavailable")
	}
	if peerSession.poisonNovaCooldownRun != nil &&
		e.now().Before(peerSession.poisonNovaCooldownRun.expiresAt) {
		return nil, nil
	}
	instanceID, err := e.modifierPool.Allocate()
	if err != nil {
		return nil, fmt.Errorf("poisonNovaAllocate: %w", err)
	}
	run := &poisonNovaCooldownRun{
		objectID: peerSession.deployedObjectID, instanceID: instanceID,
		expiresAt: e.now().Add(duration),
	}
	resetPacket, err := abilityraknet.CooldownReset(run.objectID, abilityID)
	if err != nil {
		_ = e.modifierPool.Release(instanceID)
		return nil, fmt.Errorf("poisonNovaCooldownReset: %w", err)
	}
	createPacket, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID:     run.objectID,
		ModifierGUID: util.HashID(poisonNovaCooldownModifierName),
		InstanceID:   instanceID, DurationMilliseconds: uint32(duration.Milliseconds()),
		StackCount: 1, StartMilliseconds: timestamp, SourceID: run.objectID,
	})
	if err != nil {
		_ = e.modifierPool.Release(instanceID)
		return nil, fmt.Errorf("poisonNovaCreate: %w", err)
	}
	expiry := poisonNovaCooldownExpiry{
		runtime: e, sessionKey: sessionKey, generation: generation, run: run,
	}
	cancel, err := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: duration, Produce: expiry.produce,
	}})
	if err == nil && cancel == nil {
		err = errors.New("nil poison Nova cancellation")
	}
	if err != nil {
		_ = e.modifierPool.Release(instanceID)
		return nil, fmt.Errorf("poisonNovaSchedule: %w", err)
	}
	if !peerSession.abilityCooldownSession().Rollback(reservation) {
		cancel()
		_ = e.modifierPool.Release(instanceID)
		return nil, errors.New("poison Nova cooldown reset rejected")
	}
	run.cancel = cancel
	peerSession.poisonNovaCooldownRun = run
	return [][]byte{resetPacket, createPacket}, nil
}

func stopPoisonNovaCooldown(
	peerSession *gameplayPeerSession, modifierPool *modifierPool,
) ([]byte, error) {
	if peerSession == nil || peerSession.poisonNovaCooldownRun == nil {
		return nil, nil
	}
	run := peerSession.poisonNovaCooldownRun
	run.cancelSchedule()
	peerSession.poisonNovaCooldownRun = nil
	if modifierPool != nil {
		_ = modifierPool.Release(run.instanceID)
	}
	packet, err := effectraknet.ModifierDelete(run.objectID, run.instanceID)
	if err != nil {
		return nil, fmt.Errorf("poisonNovaStop: %w", err)
	}
	return packet, nil
}
