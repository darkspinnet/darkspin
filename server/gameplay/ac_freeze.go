package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
)

// Build 103 Modifiers/0x21F41BB1.lua adds Frozen in Tick, independently of
// the inherited root. Deactivate removes the ice block and emits its break.
type acFreezePresentation struct {
	registry  *gameplaySessionRegistry
	world     *zone.Zone
	pool      *attachedEffectPool
	objectID  uint32
	slot      uint8
	packets   [][]byte
	cancel    raknet.CancelSchedule
	isStopped bool
}

// Called under the registry lock by modifier expiry, cleanse and teardown.
func (e *acFreezePresentation) stop() {
	if e.isStopped {
		return
	}
	e.isStopped = true
	if e.cancel != nil {
		e.cancel()
	}
	isReleased := e.pool.Release(e.objectID, e.slot)
	if !isReleased {
		// Object teardown may already have released every attached effect.
	}
	for key, member := range e.registry.sessions {
		if member.zone != e.world {
			continue
		}
		member.queueCampaignPackets(e.packets)
		e.registry.sessions[key] = member
	}
}

func (e campaignNPCActionRuntime) attachACFreeze(
	sessionKey string, generation uint64, run *campaignNPCModifierRun,
) ([][]byte, error) {
	startPacket, stopPackets, slot, err := preparePhantomChargeEffect(
		e.effectPool, run.record.TargetObjectID, "frostbite_iceblock.ServerEventDef",
	)
	if err != nil {
		return nil, fmt.Errorf("freezeAttach: %w", err)
	}
	asset := util.HashID("cryos_stalagmite_destruction_effect.ServerEventDef")
	objectID := run.record.TargetObjectID
	breakPacket, err := raknet.MarshalApplication(raknet.ServerEventContractMessage{
		Asset: &asset, ObjectID: &objectID,
	})
	if err != nil {
		isReleased := e.effectPool.Release(objectID, slot)
		if !isReleased {
			// No start packet has been published; no client removal is needed.
		}
		return nil, fmt.Errorf("freezeBreak: %w", err)
	}
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.generation != generation || member.campaignNPCModifiers[run.instanceID] != run || !run.isActive() {
		isReleased := e.effectPool.Release(objectID, slot)
		if !isReleased {
			// Concurrent teardown already retired the unpublished attachment.
		}
		return nil, nil
	}
	presentation := &acFreezePresentation{
		registry: e.registry, world: member.zone, pool: e.effectPool,
		objectID: objectID, slot: slot, packets: append(stopPackets, breakPacket), cancel: run.cancel,
	}
	run.cancel = presentation.stop
	return [][]byte{startPacket}, nil
}

// The registry lock protects the tracked modifier set. Recompute rather than
// clearing on one expiry so overlapping freezes cannot thaw each other.
func (e *gameplayPeerSession) refreshACFrozenAttribute() ([][]byte, error) {
	objectID := uint32(0)
	if e.deployedHitPoint() > 0 {
		for _, run := range e.campaignNPCModifiers {
			if run != nil && run.record.TargetObjectID == e.deployedObjectID &&
				run.record.GUID == util.HashID("AcFreeze") && run.isActive() {
				objectID = e.deployedObjectID
				break
			}
		}
	}
	packets := make([][]byte, 0, 2)
	if e.enemyFrozenPresentedObjectID != 0 && e.enemyFrozenPresentedObjectID != objectID {
		packet, err := marshalACFrozenAttribute(e.enemyFrozenPresentedObjectID, 0)
		if err != nil {
			return nil, fmt.Errorf("freezeClear: %w", err)
		}
		packets = append(packets, packet)
	}
	if objectID != 0 {
		packet, err := marshalACFrozenAttribute(objectID, 1)
		if err != nil {
			return nil, fmt.Errorf("freezeHold: %w", err)
		}
		packets = append(packets, packet)
	}
	e.enemyFrozenPresentedObjectID = objectID
	return packets, nil
}

func marshalACFrozenAttribute(objectID uint32, amount float32) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: objectID, Value: map[uint8]float32{chronoFrozenAttribute: amount},
	})
	if err != nil {
		return nil, fmt.Errorf("freezeAttribute: %w", err)
	}
	return packet, nil
}
