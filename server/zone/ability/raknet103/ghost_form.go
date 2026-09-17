package raknet103

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
)

type GhostFormRun struct {
	creatureIndex uint32
	instanceID    uint32
	objectID      uint32
	effectPool    AttachmentPool
	effectSlot    uint8
	isEffectBound bool
	cancel        raknet.CancelSchedule
}

func NewGhostFormRun(
	creatureIndex uint32, instanceID uint32, objectID uint32,
	effectPool AttachmentPool,
) (*GhostFormRun, error) {
	if instanceID == 0 || objectID == 0 || effectPool == nil {
		return nil, errors.New("invalid Ghost Form run")
	}
	effectSlot, isAllocated := effectPool.Allocate(objectID)
	if !isAllocated {
		return nil, errors.New("Ghost Form effect slot unavailable")
	}
	return &GhostFormRun{
		creatureIndex: creatureIndex,
		instanceID:    instanceID,
		objectID:      objectID,
		effectPool:    effectPool,
		effectSlot:    effectSlot,
		isEffectBound: true,
	}, nil
}

func (r *GhostFormRun) InstanceID() uint32 {
	if r == nil {
		return 0
	}
	return r.instanceID
}

func (r *GhostFormRun) ObjectID() uint32 {
	if r == nil {
		return 0
	}
	return r.objectID
}

func (r *GhostFormRun) IsActive(creatureIndex uint32, isAlive bool) bool {
	return r != nil && r.creatureIndex == creatureIndex && isAlive
}

func (r *GhostFormRun) SetCancel(cancel raknet.CancelSchedule) {
	if r != nil {
		r.cancel = cancel
	}
}

func (r *GhostFormRun) Cancel() {
	if r == nil || r.cancel == nil {
		return
	}
	r.cancel()
	r.cancel = nil
}

func (r *GhostFormRun) EffectSlot() uint8 {
	if r == nil || !r.isEffectBound {
		return 0
	}
	return r.effectSlot + 1
}

func (r *GhostFormRun) ReleaseEffect() bool {
	if r == nil || !r.isEffectBound || r.effectPool == nil {
		return false
	}
	isReleased := r.effectPool.Release(r.objectID, r.effectSlot)
	r.isEffectBound = false
	return isReleased
}

func GhostFormStartPackets(
	objectID uint32, instanceID uint32, definition sim.AbilityDefinition,
	remainingManaPoint float32, timestamp uint64, effectSlot uint8,
) ([][]byte, error) {
	if objectID == 0 || instanceID == 0 || definition.Duration <= 0 ||
		effectSlot == 0 {
		return nil, errors.New("ghost form start unavailable")
	}
	messages := []raknet.ApplicationMessage{
		raknet.SetAnimationStateMessage{
			ObjectID: objectID, State: util.HashID(definition.AnimationName),
			Timestamp: timestamp, Scale: 1,
		},
		raknet.CooldownUpdateMessage{
			ObjectID: objectID, AbilityKey: uint64(util.HashID(definition.Name)),
			DurationMilliseconds:    definition.Cooldown.Milliseconds(),
			SourceStartMilliseconds: int64(timestamp),
		},
		raknet.CombatantDataDeltaMessage{
			ObjectID: objectID, ManaPoints: remainingManaPoint,
			IsManaPointChanged: true,
		},
		raknet.ServerEventMessage{
			Asset: util.HashID(definition.MuzzleEffectName), ObjectID: objectID,
		},
		raknet.AttachedEffectMessage{
			Slot: effectSlot, IsForceAttached: true,
			Asset: util.HashID(definition.HitEffectName), ObjectID: objectID,
		},
		raknet.ModifierCreatedMessage{
			TargetID: objectID, ModifierGUID: definition.RootModifierID,
			InstanceID: instanceID, DurationMilliseconds: uint32(definition.Duration.Milliseconds()),
			StackCount: 1, StartMilliseconds: timestamp, SourceID: objectID,
		},
	}
	packets, err := marshalMessages(messages, "ghostFormStart")
	if err != nil {
		return nil, fmt.Errorf("startMarshal: %w", err)
	}
	return packets, nil
}

func (r *GhostFormRun) ExpiryPackets(timestamp uint64) ([][]byte, error) {
	if r == nil || r.objectID == 0 || r.instanceID == 0 || timestamp == 0 {
		return nil, errors.New("ghost form expiry unavailable")
	}
	messages := make([]raknet.ApplicationMessage, 0, 3)
	if r.ReleaseEffect() {
		messages = append(messages, raknet.AttachedEffectMessage{
			Slot: r.effectSlot + 1, IsRemovalRequested: true,
			IsHardStop: true, ObjectID: r.objectID,
		})
	}
	messages = append(messages, raknet.ModifierDeletedMessage{
		TargetID: r.objectID, InstanceID: r.instanceID,
	})
	messages = append(messages, raknet.SetAnimationStateMessage{
		ObjectID: r.objectID, Timestamp: timestamp, Scale: 1,
	})
	packets, err := marshalMessages(messages, "ghostFormExpiry")
	if err != nil {
		return nil, fmt.Errorf("expiryMarshal: %w", err)
	}
	return packets, nil
}

func GhostFormDodgePackets(
	targetObjectID uint32, sourceObjectID uint32, effectName string,
) ([][]byte, error) {
	if targetObjectID == 0 || sourceObjectID == 0 || effectName == "" {
		return nil, errors.New("ghost form dodge presentation missing")
	}
	feedbackPacket, err := raknet.MarshalApplication(
		raknet.DamageCombatEventMessage{
			Flags: 0x0020, TargetID: targetObjectID, SourceID: sourceObjectID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("dodgeFeedbackMarshal: %w", err)
	}
	effectPacket, err := raknet.MarshalApplication(raknet.ServerEventMessage{
		Asset: util.HashID(effectName), ObjectID: targetObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("dodgeEffectMarshal: %w", err)
	}
	return [][]byte{feedbackPacket, effectPacket}, nil
}
