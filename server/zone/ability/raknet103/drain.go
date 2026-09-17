package raknet103

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

func ChannelDrainEffects(
	beamAsset string, impactAsset string, sourceObjectID uint32,
	targetObjectID uint32,
) ([][]byte, error) {
	if beamAsset == "" || impactAsset == "" || sourceObjectID == 0 ||
		targetObjectID == 0 {
		return nil, errors.New("channel drain effect invalid")
	}
	messages := []raknet.ApplicationMessage{
		raknet.ChainedEffectMessage{
			Asset: util.HashID(beamAsset), ObjectID: sourceObjectID,
			SecondaryObjectID: targetObjectID,
		},
		raknet.ChainedEffectMessage{
			Asset: util.HashID(impactAsset), ObjectID: targetObjectID,
			SecondaryObjectID: sourceObjectID,
		},
	}
	packets, err := marshalApplicationMessages(messages, "channelDrainEffect")
	if err != nil {
		return nil, fmt.Errorf("channelDrainEffectMarshal: %w", err)
	}
	return packets, nil
}

func ChannelDrainAttachedEffects(
	beamAsset string, impactAsset string, sourceObjectID uint32,
	sourceSlot uint8, targetObjectID uint32, targetSlot uint8,
) ([][]byte, error) {
	if beamAsset == "" || impactAsset == "" || sourceObjectID == 0 ||
		targetObjectID == 0 {
		return nil, errors.New("channel drain effect invalid")
	}
	messages := []raknet.ApplicationMessage{
		raknet.AttachedEffectMessage{
			Slot: sourceSlot + 1, IsForceAttached: true,
			Asset: util.HashID(beamAsset), ObjectID: sourceObjectID,
			SecondaryObjectID: targetObjectID,
		},
		raknet.AttachedEffectMessage{
			Slot: targetSlot + 1, IsForceAttached: true,
			Asset: util.HashID(impactAsset), ObjectID: targetObjectID,
			SecondaryObjectID: sourceObjectID,
		},
	}
	packets, err := marshalApplicationMessages(messages, "channelDrainAttachedEffect")
	if err != nil {
		return nil, fmt.Errorf("channelDrainAttachedEffectMarshal: %w", err)
	}
	return packets, nil
}

func ChannelDrainTargetEffect(
	impactAsset string, sourceObjectID uint32, targetObjectID uint32,
	targetSlot uint8,
) ([][]byte, error) {
	if impactAsset == "" || sourceObjectID == 0 || targetObjectID == 0 {
		return nil, errors.New("channel drain target effect invalid")
	}
	packets, err := marshalApplicationMessages([]raknet.ApplicationMessage{
		raknet.AttachedEffectMessage{
			Slot: targetSlot + 1, IsForceAttached: true,
			Asset: util.HashID(impactAsset), ObjectID: targetObjectID,
			SecondaryObjectID: sourceObjectID,
		},
	}, "channelDrainTargetEffect")
	if err != nil {
		return nil, fmt.Errorf("channelDrainTargetEffectMarshal: %w", err)
	}
	return packets, nil
}

func ChannelDrainEffectRemovals(
	sourceObjectID uint32, sourceSlot uint8,
	targetObjectID uint32, targetSlot uint8,
) ([][]byte, error) {
	if sourceObjectID == 0 || targetObjectID == 0 {
		return nil, errors.New("channel drain effect removal invalid")
	}
	messages := []raknet.ApplicationMessage{
		raknet.AttachedEffectMessage{
			Slot: sourceSlot + 1, IsRemovalRequested: true,
			IsHardStop: true, ObjectID: sourceObjectID,
		},
		raknet.AttachedEffectMessage{
			Slot: targetSlot + 1, IsRemovalRequested: true,
			IsHardStop: true, ObjectID: targetObjectID,
		},
	}
	packets, err := marshalApplicationMessages(messages, "channelDrainEffectRemoval")
	if err != nil {
		return nil, fmt.Errorf("channelDrainEffectRemovalMarshal: %w", err)
	}
	return packets, nil
}

func ChannelDrainTargetEffectRemoval(
	targetObjectID uint32, targetSlot uint8,
) ([][]byte, error) {
	if targetObjectID == 0 {
		return nil, errors.New("channel drain target effect removal invalid")
	}
	packets, err := marshalApplicationMessages([]raknet.ApplicationMessage{
		raknet.AttachedEffectMessage{
			Slot: targetSlot + 1, IsRemovalRequested: true,
			IsHardStop: true, ObjectID: targetObjectID,
		},
	}, "channelDrainTargetEffectRemoval")
	if err != nil {
		return nil, fmt.Errorf("channelDrainTargetEffectRemovalMarshal: %w", err)
	}
	return packets, nil
}

func ChannelDrainHealing(
	sourceObjectID uint32, hitPoint float32, amount float32,
) ([][]byte, error) {
	packets, err := DamageHealing(
		sourceObjectID, sourceObjectID, hitPoint, amount,
	)
	if err != nil {
		return nil, fmt.Errorf("channelDrainHealing: %w", err)
	}
	return packets, nil
}

func DamageHealing(
	sourceObjectID uint32, targetObjectID uint32,
	hitPoint float32, amount float32,
) ([][]byte, error) {
	if sourceObjectID == 0 || targetObjectID == 0 || hitPoint < 0 || amount <= 0 {
		return nil, errors.New("damage healing invalid")
	}
	previousHitPoint := hitPoint - amount
	integerChange := int32(hitPoint) - int32(previousHitPoint)
	packets, err := marshalApplicationMessages([]raknet.ApplicationMessage{
		raknet.DamageCombatEventMessage{
			Flags: 0x0002, DeltaHealth: -amount,
			TargetID: targetObjectID, SourceID: sourceObjectID,
			IntegerHPChange: -integerChange,
		},
		raknet.CombatantDataDeltaMessage{
			ObjectID: targetObjectID, HitPoints: hitPoint,
			IsHitPointChanged: true,
		},
	}, "damageHealing")
	if err != nil {
		return nil, fmt.Errorf("damageHealingMarshal: %w", err)
	}
	return packets, nil
}
