package raknet103

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

// Keep the split shader separate from spawn (16) and ordinary attack effects.
const dopplerShaderSlot uint8 = 15

func DopplerShaders(sourceID, fakeID uint32, isRemovalRequested bool) ([][]byte, error) {
	messages := make([]raknet.ApplicationMessage, 0, 2)
	for _, objectID := range []uint32{sourceID, fakeID} {
		messages = append(messages, raknet.AttachedEffectMessage{
			Slot: dopplerShaderSlot, ObjectID: objectID,
			Asset:              util.HashID("shadow_doppler_split_shader_effect.ServerEventDef"),
			IsForceAttached:    !isRemovalRequested,
			IsRemovalRequested: isRemovalRequested, IsHardStop: isRemovalRequested,
		})
	}
	packets, err := marshalMessages(messages, "dopplerShader")
	if err != nil {
		return nil, fmt.Errorf("shaderPackets: %w", err)
	}
	return packets, nil
}
