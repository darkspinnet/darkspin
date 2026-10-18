package raknet103

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/squad"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
)

const EnrageDuration = 30 * time.Second
const EnrageTick = 3 * time.Second
const enrageBodyScale = float32(0.12)

type EnrageTarget struct {
	Creature  game.GameplayCreature
	Character squad.Character
	ObjectID  uint32
}

type EnrageState interface {
	EnrageTarget(uint32, uint32, bool) (EnrageTarget, bool)
	ApplyEnrage(uint32, uint32, bool, float32, float32) error
	RemoveEnrage(uint32, uint32, bool, float32) error
}

type EnrageRun struct {
	creatureIndex      uint32
	targetObjectID     uint32
	isCompanion        bool
	instanceID         uint32
	sourceObjectID     uint32
	replacedInstanceID uint32
	replacedRun        *EnrageRun
	effectPool         AttachmentPool
	effectSlot         uint8
	effectObjectID     uint32
	damageBonus        float32
	isApplied          bool
	isEffectBound      bool
	cancel             raknet.CancelSchedule
}

func NewEnrageRun(
	creatureIndex uint32, targetObjectID uint32, isCompanion bool,
	instanceID uint32, sourceObjectID uint32,
	creature game.GameplayCreature, effectPool AttachmentPool,
) (*EnrageRun, error) {
	if creatureIndex >= squad.Size || targetObjectID == 0 ||
		instanceID == 0 || sourceObjectID == 0 ||
		!creature.DamageProfile.IsPrimaryAttributeFound || effectPool == nil {
		return nil, errors.New("invalid campaign Enrage run")
	}
	primaryAttribute := creature.DamageProfile.PrimaryAttribute
	damageBonus := 5 * (1 + (primaryAttribute-8)*0.05)
	if isCompanion {
		damageBonus = 0.5
	}
	if damageBonus <= 0 || zoneability.IsInvalidNumber(damageBonus) {
		return nil, errors.New("invalid campaign Enrage damage bonus")
	}
	return &EnrageRun{
		creatureIndex: creatureIndex, targetObjectID: targetObjectID,
		isCompanion: isCompanion, instanceID: instanceID,
		sourceObjectID: sourceObjectID, effectPool: effectPool,
		damageBonus: damageBonus,
	}, nil
}

func (r *EnrageRun) Replace(previous *EnrageRun) bool {
	if r == nil || previous == nil || r.creatureIndex != previous.creatureIndex ||
		!previous.isApplied {
		return false
	}
	r.isApplied = true
	r.replacedInstanceID = previous.instanceID
	r.replacedRun = previous
	return true
}

func (r *EnrageRun) Apply(
	state EnrageState, definition sim.AbilityDefinition, timestamp uint64,
) ([][]byte, float32, error) {
	if r == nil || state == nil || r.creatureIndex >= squad.Size {
		return nil, 0, errors.New("campaign Enrage unavailable")
	}
	target, isFound := state.EnrageTarget(
		r.creatureIndex, r.targetObjectID, r.isCompanion,
	)
	if !isFound || !target.Character.IsAvailable || target.Character.HitPoints <= 0 ||
		target.ObjectID == 0 {
		return nil, 0, nil
	}
	messages := make([]raknet.ApplicationMessage, 0, 5)
	damageBonus := float32(0)
	if !r.isApplied {
		err := r.bindEffect(target.ObjectID)
		if err != nil {
			return nil, 0, fmt.Errorf("enrageEffectBind: %w", err)
		}
		damageBonus = r.damageBonus
		messages = append(messages,
			raknet.ServerEventMessage{
				Asset: util.HashID(definition.MuzzleEffectName), ObjectID: r.sourceObjectID,
			},
			raknet.ServerEventMessage{
				Asset: util.HashID(definition.HitEffectName), ObjectID: target.ObjectID,
			},
			raknet.ModifierCreatedMessage{
				TargetID: target.ObjectID, ModifierGUID: definition.RootModifierID,
				InstanceID:           r.instanceID,
				DurationMilliseconds: uint32(EnrageDuration.Milliseconds()),
				StackCount:           1, StartMilliseconds: timestamp, SourceID: target.ObjectID,
			},
			raknet.AttachedEffectMessage{
				Slot: r.effectSlot + 1, IsForceAttached: true,
				Asset: util.HashID("status_enraged.ServerEventDef"), ObjectID: target.ObjectID,
			},
			raknet.AttributeDataUpdateMessage{
				ObjectID: target.ObjectID,
				Value:    map[uint8]float32{113: enrageBodyScale},
			},
		)
	} else if r.replacedInstanceID != 0 {
		err := r.bindEffect(target.ObjectID)
		if err != nil {
			return nil, 0, fmt.Errorf("enrageEffectBind: %w", err)
		}
		if r.replacedRun != nil && r.replacedRun.ReleaseEffect() {
			messages = append(messages, raknet.AttachedEffectMessage{
				Slot: r.replacedRun.effectSlot + 1, IsRemovalRequested: true,
				IsHardStop: true, ObjectID: target.ObjectID,
			})
		}
		messages = append(messages,
			raknet.ModifierDeletedMessage{
				TargetID: target.ObjectID, InstanceID: r.replacedInstanceID,
			},
			raknet.ServerEventMessage{
				Asset: util.HashID(definition.MuzzleEffectName), ObjectID: r.sourceObjectID,
			},
			raknet.ServerEventMessage{
				Asset: util.HashID(definition.HitEffectName), ObjectID: target.ObjectID,
			},
			raknet.ModifierCreatedMessage{
				TargetID: target.ObjectID, ModifierGUID: definition.RootModifierID,
				InstanceID:           r.instanceID,
				DurationMilliseconds: uint32(EnrageDuration.Milliseconds()),
				StackCount:           1, StartMilliseconds: timestamp, SourceID: target.ObjectID,
			},
			raknet.AttachedEffectMessage{
				Slot: r.effectSlot + 1, IsForceAttached: true,
				Asset: util.HashID("status_enraged.ServerEventDef"), ObjectID: target.ObjectID,
			},
		)
	}
	healing, err := game.ResolveAbilityHealing(game.AbilityHealing{
		Amount: 2, Coefficient: 0.05, Descriptor: 4112, IsDescriptorFound: true,
	}, target.Creature.HealingProfile)
	if err != nil {
		return nil, 0, fmt.Errorf("enrageHealing: %w", err)
	}
	healing, err = game.ApplyTargetHealingReduction(
		healing, target.Creature.HealingTargetProfile,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("enrageReduction: %w", err)
	}
	maximumHitPoint := target.Creature.MaximumHitPoint
	if maximumHitPoint <= 0 {
		maximumHitPoint = target.Creature.HitPoint
	}
	if maximumHitPoint <= 0 {
		maximumHitPoint = 200
	}
	hitPoint := min(maximumHitPoint, target.Character.HitPoints+healing)
	healedAmount := hitPoint - target.Character.HitPoints
	if healedAmount > 0 {
		integerChange := int32(hitPoint) - int32(target.Character.HitPoints)
		messages = append(
			messages,
			raknet.DamageCombatEventMessage{
				Flags: 0x0002, DeltaHealth: -healedAmount,
				TargetID: target.ObjectID, SourceID: r.sourceObjectID,
				IntegerHPChange: -integerChange,
			},
			raknet.CombatantDataDeltaMessage{
				ObjectID: target.ObjectID, HitPoints: hitPoint,
				IsHitPointChanged: true,
			},
		)
	}
	packets, err := marshalEnrageMessages(messages)
	if err != nil {
		return nil, 0, fmt.Errorf("enrageMarshal: %w", err)
	}
	err = state.ApplyEnrage(
		r.creatureIndex, r.targetObjectID, r.isCompanion,
		damageBonus, hitPoint,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("enrageCommit: %w", err)
	}
	r.isApplied = true
	r.replacedInstanceID = 0
	r.replacedRun = nil
	return packets, healedAmount, nil
}

func (r *EnrageRun) Expire(state EnrageState) ([][]byte, error) {
	if r == nil || state == nil {
		return nil, errors.New("campaign Enrage expiry unavailable")
	}
	target, isFound := state.EnrageTarget(
		r.creatureIndex, r.targetObjectID, r.isCompanion,
	)
	if !isFound || target.ObjectID == 0 {
		return nil, errors.New("campaign Enrage expiry target missing")
	}
	packets := make([][]byte, 0, 3)
	if r.ReleaseEffect() {
		effectPacket, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
			Slot: r.effectSlot + 1, IsRemovalRequested: true,
			IsHardStop: true, ObjectID: target.ObjectID,
		})
		if err != nil {
			return nil, fmt.Errorf("enrageEffectDelete: %w", err)
		}
		packets = append(packets, effectPacket)
	}
	modifierPacket, err := raknet.MarshalApplication(raknet.ModifierDeletedMessage{
		TargetID: target.ObjectID, InstanceID: r.instanceID,
	})
	if err != nil {
		return nil, fmt.Errorf("enrageDelete: %w", err)
	}
	packets = append(packets, modifierPacket)
	attributePacket, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: target.ObjectID,
		Value:    map[uint8]float32{113: 0},
	})
	if err != nil {
		return nil, fmt.Errorf("enrageScaleReset: %w", err)
	}
	if r.isApplied {
		err = state.RemoveEnrage(
			r.creatureIndex, r.targetObjectID, r.isCompanion, r.damageBonus,
		)
		if err != nil {
			return nil, fmt.Errorf("enrageRemove: %w", err)
		}
		r.isApplied = false
	}
	packets = append(packets, attributePacket)
	return packets, nil
}

func (r *EnrageRun) bindEffect(objectID uint32) error {
	if r == nil || r.effectPool == nil || objectID == 0 {
		return errors.New("campaign Enrage effect unavailable")
	}
	if r.isEffectBound {
		return nil
	}
	effectSlot, isAllocated := r.effectPool.Allocate(objectID)
	if !isAllocated {
		return errors.New("campaign Enrage effect slot unavailable")
	}
	r.effectSlot = effectSlot
	r.effectObjectID = objectID
	r.isEffectBound = true
	return nil
}

func (r *EnrageRun) ReleaseEffect() bool {
	if r == nil || !r.isEffectBound || r.effectPool == nil {
		return false
	}
	isReleased := r.effectPool.Release(r.effectObjectID, r.effectSlot)
	r.isEffectBound = false
	return isReleased
}

func (r *EnrageRun) CreatureIndex() uint32 {
	if r == nil {
		return squad.Size
	}
	return r.creatureIndex
}

func (r *EnrageRun) TargetObjectID() uint32 {
	if r == nil {
		return 0
	}
	return r.targetObjectID
}

func (r *EnrageRun) InstanceID() uint32 {
	if r == nil {
		return 0
	}
	return r.instanceID
}

func (r *EnrageRun) SetCancel(cancel raknet.CancelSchedule) {
	if r != nil {
		r.cancel = cancel
	}
}

func (r *EnrageRun) ClearCancel() {
	if r != nil {
		r.cancel = nil
	}
}

func (r *EnrageRun) Cancel() {
	if r != nil && r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
}

func marshalEnrageMessages(messages []raknet.ApplicationMessage) ([][]byte, error) {
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("message[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}
