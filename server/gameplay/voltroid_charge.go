package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const voltroidChargeHitDelay = 533333361 * time.Nanosecond
const voltroidChargeStopDelay = 899999976 * time.Nanosecond

// Lua yields while a UniqueIrreplaceable recipient is already charging. The
// server polls that admission; this is not an authored extra animation wait.
const voltroidChargeAdmissionPoll = 50 * time.Millisecond

// Mutable fields are read and written only under the gameplay registry mutex.
type campaignVoltroidPendingCharge struct {
	npc           *zonenpc.Session
	lease         zonenpc.ZapChargeLease
	effects       *channelEffects
	isHitAccepted bool
	isReleased    bool
	cancelExpiry  func()
}

func (e campaignVoltroidSchedule) releaseChargeLocked() {
	if e.charge == nil || e.charge.isReleased {
		return
	}
	e.charge.npc.ReleaseZapCharge(e.charge.lease)
	if e.charge.cancelExpiry != nil {
		e.charge.cancelExpiry()
		e.charge.cancelExpiry = nil
	}
	if e.charge.effects != nil {
		e.charge.effects.Retire()
		e.clearBeamLocked()
		e.runtime.registry.commitChannelRemovalsLocked()
	}
	e.charge.isReleased = true
}

func (e campaignVoltroidSchedule) releaseCharge() {
	e.runtime.registry.mutex.Lock()
	e.releaseChargeLocked()
	e.runtime.registry.mutex.Unlock()
}

func (e campaignVoltroidSchedule) stopBeamLocked() {
	if e.charge != nil && e.charge.effects != nil {
		e.charge.effects.Retire()
		e.clearBeamLocked()
		e.runtime.registry.commitChannelRemovalsLocked()
	}
}

func (e campaignVoltroidSchedule) clearBeamLocked() {
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound {
		return
	}
	beam, isBeamFound := peerSession.campaignNPCVoltroidBeams[e.objectID]
	if isBeamFound && e.charge != nil && beam.effects == e.charge.effects {
		delete(peerSession.campaignNPCVoltroidBeams, e.objectID)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
}

func (e campaignVoltroidSchedule) start(timestamp uint64) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || !peerSession.isCampaignNPCSourceGenerationActive(e.generation, e.objectID, e.plan.ActionGeneration) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	now := e.runtime.now()
	if e.castAt.IsZero() {
		e.castAt = now
		e.castTimestamp = timestamp
	}
	lease, isReserved := peerSession.zone.NPCs().ReserveZapCharge(e.plan, now)
	if !isReserved {
		target, isTargetFound := peerSession.zone.NPCs().NPC(e.targetID)
		source, isSourceFound := peerSession.zone.NPCs().NPC(e.objectID)
		isTargetCurrent := isTargetFound && isSourceFound && !target.IsDefeated && target.HitPoint > 0 && target.Faction == source.Faction
		e.runtime.registry.mutex.Unlock()
		if !isTargetCurrent {
			e.runtime.releaseActionGeneration(e.sessionKey, e.generation, e.objectID, e.plan.ActionGeneration)
			return nil, nil
		}
		err := e.schedule([]raknet.ScheduledPacketProducer{{Delay: voltroidChargeAdmissionPoll, Produce: e.retryStart}})
		if err != nil {
			return e.fail("voltroidChargeRetry", err)
		}
		return nil, nil
	}
	e.charge = &campaignVoltroidPendingCharge{npc: peerSession.zone.NPCs(), lease: lease}
	if e.runtime.timer == nil {
		e.releaseChargeLocked()
		e.runtime.registry.mutex.Unlock()
		return e.fail("voltroidChargeTimer", errors.New("Voltroid world timer unavailable"))
	}
	cancelExpiry, expiryErr := e.runtime.timer.Schedule(6533333361*time.Nanosecond, e.expire)
	if expiryErr != nil {
		if cancelExpiry != nil {
			cancelExpiry()
		}
		e.releaseChargeLocked()
		e.runtime.registry.mutex.Unlock()
		return e.fail("voltroidChargeExpiry", expiryErr)
	}
	if cancelExpiry == nil {
		e.releaseChargeLocked()
		e.runtime.registry.mutex.Unlock()
		return e.fail("voltroidChargeExpiry", errors.New("Voltroid expiry cancellation unavailable"))
	}
	e.charge.cancelExpiry = cancelExpiry
	e.runtime.registry.mutex.Unlock()
	startPackets, err := e.runtime.startNPCAttack(e.sessionKey, e.generation, e.plan, timestamp)
	if err != nil {
		return e.fail("voltroidChargeMarshal", err)
	}
	if len(startPackets) == 0 {
		e.releaseCharge()
		return nil, nil
	}
	e.runtime.registry.mutex.RLock()
	member, isMemberFound := e.runtime.registry.sessions[e.sessionKey]
	isResetCurrent := isMemberFound &&
		member.isCampaignNPCSourceGenerationActive(e.generation, e.objectID, e.plan.ActionGeneration) &&
		e.charge.npc.IsZapChargeCurrent(e.charge.lease, e.runtime.now())
	var resetPacket []byte
	if isResetCurrent {
		resetPacket, err = npcraknet.ResetAnimation(e.targetID, timestamp)
	}
	e.runtime.registry.mutex.RUnlock()
	if err != nil {
		return e.fail("voltroidTargetReset", err)
	}
	if !isResetCurrent {
		e.releaseCharge()
		return nil, nil
	}
	profile := e.plan.Profile
	repeatDelay := max(profile.Cooldown, profile.ReleaseDelay)
	e.timestamp = timestamp + uint64(repeatDelay/time.Millisecond)
	e.chargeReadyTimestamp = timestamp + uint64(campaignVoltroidChargeFriendCooldown/time.Millisecond)
	err = e.schedule([]raknet.ScheduledPacketProducer{
		{Delay: voltroidChargeHitDelay, Produce: e.hit},
		{Delay: voltroidChargeStopDelay, Produce: e.transfer},
		{Delay: repeatDelay, Produce: e.next},
	})
	if err != nil {
		return e.fail("voltroidChargeSchedule", err)
	}
	return append([][]byte{resetPacket}, startPackets...), nil
}

func (e campaignVoltroidSchedule) retryStart() ([][]byte, error) {
	elapsed := max(time.Duration(0), e.runtime.now().Sub(e.castAt))
	packets, err := e.start(e.castTimestamp + uint64(elapsed/time.Millisecond))
	if err != nil {
		return packets, fmt.Errorf("voltroidChargeAdmission: %w", err)
	}
	return packets, nil
}

func (e campaignVoltroidSchedule) hit() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if e.charge == nil || e.charge.isReleased || !isFound ||
		!peerSession.isCampaignNPCSourceGenerationActive(e.generation, e.objectID, e.plan.ActionGeneration) ||
		!e.charge.npc.IsZapChargeCurrent(e.charge.lease, e.runtime.now()) {
		e.releaseChargeLocked()
		return nil, nil
	}
	e.charge.isHitAccepted = true
	effects, err := e.runtime.registry.allocateChannelEffectsLocked(peerSession.zone, e.runtime.effectPool, []uint32{e.objectID})
	if err != nil {
		e.releaseChargeLocked()
		return nil, fmt.Errorf("voltroidBeamSlot: %w", err)
	}
	e.charge.effects = effects
	slots := effects.Slots()
	beamPacket, err := npcraknet.VoltroidEffect(e.objectID, e.targetID, slots[0].slot, e.plan.Profile.TrailEffectName, false)
	if err != nil {
		e.releaseChargeLocked()
		return nil, fmt.Errorf("voltroidBeamMarshal: %w", err)
	}
	err = e.runtime.registry.publishChannelEffectsLocked(effects, [][]byte{beamPacket})
	if err != nil {
		e.releaseChargeLocked()
		return nil, fmt.Errorf("voltroidBeamPublish: %w", err)
	}
	// Publication queues recipient batches in the session aggregate.
	peerSession = e.runtime.registry.sessions[e.sessionKey]
	if peerSession.campaignNPCVoltroidBeams == nil {
		peerSession.campaignNPCVoltroidBeams = make(map[uint32]campaignVoltroidActiveBeam)
	}
	peerSession.campaignNPCVoltroidBeams[e.objectID] = campaignVoltroidActiveBeam{
		targetID: e.targetID, effects: effects,
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	return nil, nil
}

// Retain the feature failure callback as well as the normal NPC observer. A
// later publication failure must retire only this exact pending child/beam.
func (e campaignVoltroidSchedule) schedule(producers []raknet.ScheduledPacketProducer) error {
	if e.packet.ScheduleGroupResult == nil || e.packet.Address == nil {
		return errors.New("Voltroid result scheduler unavailable")
	}
	guardedProducers := make([]raknet.ScheduledPacketProducer, len(producers))
	for index, producer := range producers {
		observer := &campaignNPCProducerObserver{
			registry: e.runtime.registry, sessionKey: e.packet.Address.String(),
			transportGeneration: e.packet.TransportGeneration,
			observer: gameplayProducerObserver{guard: e.runtime.registry.producerGuard,
				produceFunc: producer.Produce, commitFunc: producer.AfterCommit},
		}
		guardedProducers[index] = producer
		guardedProducers[index].Produce = observer.produce
		guardedProducers[index].AfterCommit = observer.observer.commit
	}
	cancel, err := e.packet.ScheduleGroupResult(guardedProducers, e.failed)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return fmt.Errorf("voltroidSchedule: %w", err)
	}
	if cancel == nil {
		return errors.New("Voltroid scheduler cancellation unavailable")
	}
	return nil
}

func (e campaignVoltroidSchedule) failed(err error) {
	e.releaseCharge()
	e.runtime.releaseActionGeneration(e.sessionKey, e.generation, e.objectID, e.plan.ActionGeneration)
	e.runtime.logger.Printf("RakNet Voltroid charging schedule failed object=%d target=%d: %v", e.objectID, e.targetID, err)
}

// Unlike transport-bound producers, this authored child timeout still retires
// the exact lease and held beam if the initiating peer leaves or is replaced.
func (e campaignVoltroidSchedule) expire() {
	e.releaseCharge()
}
