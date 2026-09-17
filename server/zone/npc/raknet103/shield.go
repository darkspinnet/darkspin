package raknet103

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

func ShieldAnimation(
	objectID uint32, animationName string, timestamp uint64,
) ([]byte, error) {
	if objectID == 0 || animationName == "" {
		return nil, errors.New("invalid Invincitron animation")
	}
	packet, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
		ObjectID: objectID, State: util.HashID(animationName),
		Timestamp: timestamp, Scale: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("invincitronAnimationMarshal: %w", err)
	}
	return packet, nil
}

func ShieldEffect(
	objectID uint32, slot uint8, isRemoval bool,
) ([]byte, error) {
	packet, err := ShieldEffectAsset(
		objectID, slot, "cyber_omnishield.ServerEventDef", isRemoval,
	)
	if err != nil {
		return nil, fmt.Errorf("invincitronEffect: %w", err)
	}
	return packet, nil
}

func ShieldEffectAsset(
	objectID uint32, slot uint8, effectName string, isRemoval bool,
) ([]byte, error) {
	if objectID == 0 || effectName == "" {
		return nil, errors.New("invalid shield effect")
	}
	message := raknet.AttachedEffectMessage{
		Slot: slot + 1, IsRemovalRequested: isRemoval, ObjectID: objectID,
	}
	if !isRemoval {
		message.IsForceAttached = true
		message.Asset = util.HashID(effectName)
	}
	packet, err := raknet.MarshalApplication(message)
	if err != nil {
		return nil, fmt.Errorf("shieldEffectMarshal: %w", err)
	}
	return packet, nil
}
