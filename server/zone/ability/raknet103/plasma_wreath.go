package raknet103

import (
	"errors"
	"fmt"
	"log"

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
	removedSlots        []uint8
	removalPackets      [][]byte
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
	run, packets, err := NewPlasmaWreathRunWithOrbCount(instanceID, objectID, effectPool, modifierPool, zoneability.PlasmaWreathOrbCount)
	if err != nil {
		return nil, nil, fmt.Errorf("wreathRun: %w", err)
	}
	return run, packets, nil
}

func NewPlasmaWreathRunWithOrbCount(
	instanceID uint32, objectID uint32, effectPool AttachmentPool,
	modifierPool ModifierPool, orbCount uint32,
) (*PlasmaWreathRun, [][]byte, error) {
	run, packets, err := newPlasmaWreathRun(instanceID, objectID, effectPool, modifierPool, orbCount, 1)
	if err != nil {
		return nil, nil, fmt.Errorf("wreathCount: %w", err)
	}
	return run, packets, nil
}

func NewPlasmaWreathRunForRank(
	instanceID uint32, objectID uint32, effectPool AttachmentPool,
	modifierPool ModifierPool, rank int32,
) (*PlasmaWreathRun, [][]byte, error) {
	profile := zoneability.PlasmaWreathProfileForRank(rank)
	run, packets, err := newPlasmaWreathRun(instanceID, objectID, effectPool, modifierPool, profile.Definition.ShotCount, rank)
	if err != nil {
		return nil, nil, fmt.Errorf("wreathRank: %w", err)
	}
	return run, packets, nil
}

func newPlasmaWreathRun(
	instanceID uint32, objectID uint32, effectPool AttachmentPool,
	modifierPool ModifierPool, orbCount uint32, rank int32,
) (*PlasmaWreathRun, [][]byte, error) {
	if instanceID == 0 || objectID == 0 || effectPool == nil || modifierPool == nil {
		return nil, nil, errors.New("invalid Plasma Wreath run")
	}
	if orbCount == 0 || orbCount > zoneability.PlasmaWreathOrbCount {
		return nil, nil, errors.New("invalid Plasma Wreath orb count")
	}
	run := &PlasmaWreathRun{
		instanceID: instanceID, objectID: objectID,
		remainingOrb: orbCount,
		orbSlots:     make([]uint8, 0, orbCount),
		effectPool:   effectPool, modifierPool: modifierPool,
		isModifierAllocated: true,
	}
	bubbleSlot, isAllocated := effectPool.Allocate(objectID)
	if !isAllocated {
		return nil, nil, errors.New("Plasma Wreath bubble slot unavailable")
	}
	run.bubbleSlot = bubbleSlot
	run.isBubbleBound = true
	for index := uint32(0); index < orbCount; index++ {
		slot, isOrbAllocated := effectPool.Allocate(objectID)
		if !isOrbAllocated {
			run.releaseSlots()
			return nil, nil, fmt.Errorf("Plasma Wreath orb slot[%d] unavailable", index)
		}
		run.orbSlots = append(run.orbSlots, slot)
	}
	messages := make([]raknet.ApplicationMessage, 0, 2+orbCount)
	bubbleEffect := plasmaWreathBubbleEffect
	orbEffect := plasmaWreathOrbEffect
	if rank == 5 || rank == 6 {
		bubbleEffect = "lightning_shield_bubble_upon_hit.ServerEventDef"
		orbEffect = "lightning_shield_upon_hit.ServerEventDef"
	}
	if rank == 7 || rank == 8 {
		orbEffect = "lightning_shield_haste.ServerEventDef"
	}
	messages = append(messages,
		raknet.ModifierCreatedMessage{
			TargetID: objectID, ModifierGUID: util.HashID("LightningRogueSupportModifier"),
			InstanceID: instanceID, StackCount: 1, SourceID: objectID,
		},
		raknet.AttachedEffectMessage{
			Slot: bubbleSlot + 1, IsForceAttached: true,
			Asset: util.HashID(bubbleEffect), ObjectID: objectID,
		},
	)
	for _, slot := range run.orbSlots {
		messages = append(messages, raknet.AttachedEffectMessage{
			Slot: slot + 1, IsForceAttached: true,
			Asset: util.HashID(orbEffect), ObjectID: objectID,
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

func (e *PlasmaWreathRun) AttachmentSlots() []uint8 {
	if e == nil {
		return nil
	}
	slots := append([]uint8(nil), e.orbSlots...)
	slots = append(slots, e.removedSlots...)
	if e.isBubbleBound {
		slots = append(slots, e.bubbleSlot)
	}
	return slots
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
	packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: slot + 1, IsRemovalRequested: true, ObjectID: r.objectID,
	})
	if err != nil {
		return nil, false, fmt.Errorf("orbMarshal: %w", err)
	}
	if !r.holdRemoval(slot) {
		return nil, false, errors.New("Plasma Wreath orb slot stale")
	}
	r.removalPackets = append(r.removalPackets, packet)
	r.orbSlots = r.orbSlots[:last]
	r.remainingOrb--
	return packet, r.remainingOrb == 0, nil
}

func (r *PlasmaWreathRun) Stop() ([][]byte, error) {
	if r == nil || r.objectID == 0 || r.effectPool == nil {
		return nil, errors.New("Plasma Wreath stop unavailable")
	}
	slots := append([]uint8(nil), r.orbSlots...)
	if r.isBubbleBound {
		slots = append(slots, r.bubbleSlot)
	}
	messages := make([]raknet.ApplicationMessage, 0, len(slots)+1)
	for _, slot := range slots {
		messages = append(messages, raknet.AttachedEffectMessage{Slot: slot + 1, IsRemovalRequested: true, ObjectID: r.objectID})
	}
	messages = append(messages, raknet.ModifierDeletedMessage{TargetID: r.objectID, InstanceID: r.instanceID})
	packets, err := marshalMessages(messages, "plasmaWreathStop")
	if err != nil {
		return r.PendingRemovals(), fmt.Errorf("stopMarshal: %w", err)
	}
	// Encoding succeeds before any ownership mutation. A stale reservation must
	// never remove a different effect now occupying that native slot.
	for index, slot := range slots {
		if r.holdRemoval(slot) {
			r.removalPackets = append(r.removalPackets, packets[index])
		}
	}
	r.removalPackets = append(r.removalPackets, packets[len(packets)-1])
	r.orbSlots = nil
	r.remainingOrb = 0
	r.isBubbleBound = false
	r.releaseModifier()
	return r.PendingRemovals(), nil
}

func (r *PlasmaWreathRun) Abort() {
	if r == nil {
		return
	}
	r.releaseSlots()
	r.CommitRemovals()
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
	err := r.modifierPool.Release(r.instanceID)
	if err != nil {
		log.Printf("Plasma Wreath modifier release instance=%d: %v", r.instanceID, err)
	}
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

type attachmentRemovalPool interface {
	HoldRemoval(objectID uint32, slot uint8) bool
	CommitRemoval(objectID uint32, slot uint8) bool
}

func (e *PlasmaWreathRun) holdRemoval(slot uint8) bool {
	pool, isSupported := e.effectPool.(attachmentRemovalPool)
	if isSupported && !pool.HoldRemoval(e.objectID, slot) {
		return false
	}
	e.removedSlots = append(e.removedSlots, slot)
	return true
}

// PendingRemovals retains deletion packets across a failed publication.
func (e *PlasmaWreathRun) PendingRemovals() [][]byte {
	return append([][]byte(nil), e.removalPackets...)
}

// PlasmaWreathRemoval owns slots until every recipient commits their deletion.
type PlasmaWreathRemoval struct {
	run   *PlasmaWreathRun
	slots []uint8
}

func (e *PlasmaWreathRun) TakeRemovals() *PlasmaWreathRemoval {
	if len(e.removedSlots) == 0 {
		return nil
	}
	removal := &PlasmaWreathRemoval{run: e, slots: e.removedSlots}
	e.removedSlots = nil
	e.removalPackets = nil
	return removal
}
func (e *PlasmaWreathRemoval) Commit() {
	if e == nil {
		return
	}
	for _, slot := range e.slots {
		pool, isSupported := e.run.effectPool.(attachmentRemovalPool)
		var isReleased bool
		if isSupported {
			isReleased = pool.CommitRemoval(e.run.objectID, slot)
		} else {
			isReleased = e.run.effectPool.Release(e.run.objectID, slot)
		}
		if !isReleased {
			log.Printf("Plasma Wreath held slot source=%d slot=%d stale", e.run.objectID, slot)
		}
	}
	e.slots = nil
}

// CommitRemovals is only for candidates whose creates were never published.
func (e *PlasmaWreathRun) CommitRemovals() { e.TakeRemovals().Commit() }
