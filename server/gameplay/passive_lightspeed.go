package gameplay

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

const lightspeedPassiveInterval = 500 * time.Millisecond

var lightspeedPassiveEffect = [...]string{
	"LT_jetpack_effect_lvl1.ServerEventDef",
	"LT_jetpack_effect_lvl2.ServerEventDef",
	"LT_jetpack_effect_lvl3.ServerEventDef",
	"LT_jetpack_effect_lvl4.ServerEventDef",
	"LT_jetpack_effect_lvl5.ServerEventDef",
}

type lightspeedPassiveStep struct {
	runtime    campaignDamageRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	epoch      uint64
}

func startLightspeedPassive(
	runtime campaignDamageRuntime, packet raknet.Packet,
	sessionKey string, generation uint64,
) ([][]byte, error) {
	if runtime.registry == nil || runtime.effectPool == nil ||
		packet.ScheduleFunc == nil || sessionKey == "" || generation == 0 {
		return nil, errors.New("lightspeed passive schedule unavailable")
	}
	runtime.registry.mutex.Lock()
	peerSession, isFound := runtime.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation {
		runtime.registry.mutex.Unlock()
		return nil, nil
	}
	peerSession.lightspeedPassiveEpoch++
	epoch := peerSession.lightspeedPassiveEpoch
	packets, err := peerSession.updateLightspeedPassiveEffect(runtime.effectPool)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("lightspeedPassiveStart: %w", err)
	}
	isActive := peerSession.isLightspeedPassiveActive()
	runtime.registry.sessions[sessionKey] = peerSession
	runtime.registry.mutex.Unlock()
	if !isActive {
		return packets, nil
	}
	step := lightspeedPassiveStep{
		runtime: runtime, packet: packet, sessionKey: sessionKey,
		generation: generation, epoch: epoch,
	}
	err = packet.ScheduleFunc(lightspeedPassiveInterval, step.produce)
	if err != nil {
		return nil, fmt.Errorf("lightspeedPassiveSchedule: %w", err)
	}
	return packets, nil
}

func (e lightspeedPassiveStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.lightspeedPassiveEpoch == e.epoch
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	packets, err := peerSession.updateLightspeedPassiveEffect(e.runtime.effectPool)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("lightspeedPassiveUpdate: %w", err)
	}
	isActive := peerSession.isLightspeedPassiveActive()
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	if !isActive {
		return packets, nil
	}
	err = e.packet.ScheduleFunc(lightspeedPassiveInterval, e.produce)
	if err != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf(
			"RakNet Lightspeed Tempest Passive cycle stopped for %s: %v",
			e.sessionKey, err,
		)
	}
	return packets, nil
}

func (e gameplayPeerSession) isLightspeedPassiveActive() bool {
	return e.deployedObjectID != 0 && e.deployedHitPoint() > 0 &&
		e.deployedCreatureIndex < uint32(len(e.binding.Creatures)) &&
		e.binding.Creatures[e.deployedCreatureIndex].PassiveAbility ==
			util.HashID("LightspeedTempestPassive")
}

func (e *gameplayPeerSession) updateLightspeedPassiveEffect(
	effectPool *attachedEffectPool,
) ([][]byte, error) {
	if e == nil || effectPool == nil {
		return nil, nil
	}
	tier := uint8(0)
	if e.isLightspeedPassiveActive() {
		healthRatio := e.passiveHealthRatio(e.deployedCreatureIndex)
		tier = min(uint8(len(lightspeedPassiveEffect)),
			uint8(math.Floor(float64(healthRatio*float32(len(lightspeedPassiveEffect)))))+1)
	}
	if tier == e.lightspeedEffectTier &&
		e.lightspeedEffectObjectID == e.deployedObjectID {
		return nil, nil
	}
	packets := make([][]byte, 0, 2)
	if e.lightspeedEffectObjectID != 0 {
		objectID := e.lightspeedEffectObjectID
		slot := e.lightspeedEffectSlot
		effectPool.Release(objectID, slot)
		packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
			Slot: slot + 1, IsRemovalRequested: true, ObjectID: objectID,
		})
		if err != nil {
			return nil, fmt.Errorf("lightspeedEffectRemove: %w", err)
		}
		packets = append(packets, packet)
		e.lightspeedEffectObjectID = 0
		e.lightspeedEffectSlot = 0
		e.lightspeedEffectTier = 0
	}
	if tier == 0 {
		return packets, nil
	}
	slot, isAllocated := effectPool.Allocate(e.deployedObjectID)
	if !isAllocated {
		return packets, nil
	}
	packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: slot + 1, IsForceAttached: true,
		Asset:    util.HashID(lightspeedPassiveEffect[tier-1]),
		ObjectID: e.deployedObjectID,
	})
	if err != nil {
		effectPool.Release(e.deployedObjectID, slot)
		return nil, fmt.Errorf("lightspeedEffectAttach: %w", err)
	}
	e.lightspeedEffectObjectID = e.deployedObjectID
	e.lightspeedEffectSlot = slot
	e.lightspeedEffectTier = tier
	return append(packets, packet), nil
}
