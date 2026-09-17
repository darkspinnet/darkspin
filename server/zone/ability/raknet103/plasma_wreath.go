package raknet103

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
)

const (
	plasmaWreathBubbleEffect = "lightning_shield_bubble.ServerEventDef"
	plasmaWreathOrbEffect    = "lightning_shield.ServerEventDef"
	plasmaWreathImpactEffect = "lightningbolt_impact.ServerEventDef"
	plasmaWreathBeamEffect   = "lightning_shield_electric_hitting_beam_effect.ServerEventDef"
)

type AttachmentPool interface {
	Allocate(objectID uint32) (uint8, bool)
	Release(objectID uint32, slot uint8) bool
}

type ModifierPool interface {
	Release(instanceID uint32) error
}

type PlasmaWreathRun struct {
	instanceID          uint32
	objectID            uint32
	remainingOrb        uint32
	bubbleSlot          uint8
	orbSlots            []uint8
	isBubbleBound       bool
	effectPool          AttachmentPool
	modifierPool        ModifierPool
	isModifierAllocated bool
	tickCount           uint64
}

func NewPlasmaWreathRun(
	instanceID uint32, objectID uint32, effectPool AttachmentPool,
	modifierPool ModifierPool,
) (*PlasmaWreathRun, [][]byte, error) {
	if instanceID == 0 || objectID == 0 || effectPool == nil || modifierPool == nil {
		return nil, nil, errors.New("invalid Plasma Wreath run")
	}
	run := &PlasmaWreathRun{
		instanceID: instanceID, objectID: objectID,
		remainingOrb: zoneability.PlasmaWreathOrbCount,
		orbSlots:     make([]uint8, 0, zoneability.PlasmaWreathOrbCount),
		effectPool:   effectPool, modifierPool: modifierPool,
		isModifierAllocated: true,
	}
	bubbleSlot, isAllocated := effectPool.Allocate(objectID)
	if !isAllocated {
		return nil, nil, errors.New("Plasma Wreath bubble slot unavailable")
	}
	run.bubbleSlot = bubbleSlot
	run.isBubbleBound = true
	for index := uint32(0); index < zoneability.PlasmaWreathOrbCount; index++ {
		slot, isOrbAllocated := effectPool.Allocate(objectID)
		if !isOrbAllocated {
			run.releaseSlots()
			return nil, nil, fmt.Errorf("Plasma Wreath orb slot[%d] unavailable", index)
		}
		run.orbSlots = append(run.orbSlots, slot)
	}
	messages := make([]raknet.ApplicationMessage, 0, 2+zoneability.PlasmaWreathOrbCount)
	messages = append(messages,
		raknet.ModifierCreatedMessage{
			TargetID: objectID, ModifierGUID: util.HashID("LightningRogueSupportModifier"),
			InstanceID: instanceID, StackCount: 1, SourceID: objectID,
		},
		raknet.AttachedEffectMessage{
			Slot: bubbleSlot + 1, IsForceAttached: true,
			Asset: util.HashID(plasmaWreathBubbleEffect), ObjectID: objectID,
		},
	)
	for _, slot := range run.orbSlots {
		messages = append(messages, raknet.AttachedEffectMessage{
			Slot: slot + 1, IsForceAttached: true,
			Asset: util.HashID(plasmaWreathOrbEffect), ObjectID: objectID,
		})
	}
	packets, err := marshalMessages(messages, "plasmaWreathStart")
	if err != nil {
		run.releaseSlots()
		return nil, nil, fmt.Errorf("startMarshal: %w", err)
	}
	return run, packets, nil
}

func (r *PlasmaWreathRun) RemainingOrb() uint32 {
	if r == nil {
		return 0
	}
	return r.remainingOrb
}

func (r *PlasmaWreathRun) AdvanceTick() uint64 {
	if r == nil {
		return 0
	}
	r.tickCount++
	return r.tickCount
}

func (r *PlasmaWreathRun) ConsumeOrb() ([]byte, bool, error) {
	if r == nil || r.objectID == 0 || r.effectPool == nil ||
		r.remainingOrb == 0 || len(r.orbSlots) == 0 {
		return nil, false, errors.New("Plasma Wreath orb unavailable")
	}
	last := len(r.orbSlots) - 1
	slot := r.orbSlots[last]
	r.orbSlots = r.orbSlots[:last]
	if !r.effectPool.Release(r.objectID, slot) {
		return nil, false, errors.New("Plasma Wreath orb slot stale")
	}
	r.remainingOrb--
	packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: slot + 1, IsRemovalRequested: true, ObjectID: r.objectID,
	})
	if err != nil {
		return nil, false, fmt.Errorf("orbMarshal: %w", err)
	}
	return packet, r.remainingOrb == 0, nil
}

func (r *PlasmaWreathRun) Stop() ([][]byte, error) {
	if r == nil || r.objectID == 0 || r.effectPool == nil {
		return nil, errors.New("Plasma Wreath stop unavailable")
	}
	messages := make([]raknet.ApplicationMessage, 0, len(r.orbSlots)+2)
	for _, slot := range r.orbSlots {
		if r.effectPool.Release(r.objectID, slot) {
			messages = append(messages, raknet.AttachedEffectMessage{
				Slot: slot + 1, IsRemovalRequested: true, ObjectID: r.objectID,
			})
		}
	}
	r.orbSlots = nil
	r.remainingOrb = 0
	if r.isBubbleBound && r.effectPool.Release(r.objectID, r.bubbleSlot) {
		messages = append(messages, raknet.AttachedEffectMessage{
			Slot: r.bubbleSlot + 1, IsRemovalRequested: true, ObjectID: r.objectID,
		})
	}
	r.isBubbleBound = false
	r.releaseModifier()
	messages = append(messages, raknet.ModifierDeletedMessage{
		TargetID: r.objectID, InstanceID: r.instanceID,
	})
	packets, err := marshalMessages(messages, "plasmaWreathStop")
	if err != nil {
		return nil, fmt.Errorf("stopMarshal: %w", err)
	}
	return packets, nil
}

func (r *PlasmaWreathRun) Abort() {
	if r == nil {
		return
	}
	r.releaseSlots()
	r.releaseModifier()
}

func PlasmaWreathHitPackets(sourceObjectID uint32, targetObjectID uint32) ([][]byte, error) {
	return marshalMessages([]raknet.ApplicationMessage{
		raknet.ChainedEffectMessage{
			Asset:    util.HashID(plasmaWreathBeamEffect),
			ObjectID: sourceObjectID, SecondaryObjectID: targetObjectID,
		},
		raknet.ServerEventMessage{
			Asset: util.HashID(plasmaWreathImpactEffect), ObjectID: targetObjectID,
		},
	}, "plasmaWreathHit")
}

func (r *PlasmaWreathRun) releaseModifier() {
	if r == nil || !r.isModifierAllocated || r.modifierPool == nil {
		return
	}
	_ = r.modifierPool.Release(r.instanceID)
	r.isModifierAllocated = false
}

func (r *PlasmaWreathRun) releaseSlots() {
	if r == nil || r.effectPool == nil {
		return
	}
	for _, slot := range r.orbSlots {
		r.effectPool.Release(r.objectID, slot)
	}
	r.orbSlots = nil
	if r.isBubbleBound {
		r.effectPool.Release(r.objectID, r.bubbleSlot)
		r.isBubbleBound = false
	}
}

func marshalMessages(messages []raknet.ApplicationMessage, operation string) ([][]byte, error) {
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: %w", operation, index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}
