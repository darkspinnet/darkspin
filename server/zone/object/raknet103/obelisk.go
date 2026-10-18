package raknet103

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

// The shipped ObeliskPassiveModifier and HealthObeliskPassiveModifier attach
// their hover effect until interaction marks the modifier for deletion. Their
// Deactivate functions remove it with hardStop=false, then notify ramp-down.
// Script objects own slot 16; they do not also receive NPC passive effects.
func obeliskEffects(objectID uint32, abilityName string, isActive, isRampDown bool) ([][]byte, error) {
	var hoverName, rampDownName string
	switch abilityName {
	case "InteractWithObelisk":
		hoverName = "obelisk_hover_effect.ServerEventDef"
		rampDownName = "obelisk_hover_effect_ramp_down.ServerEventDef"
	case "InteractHealthObelisk":
		hoverName = "obelisk_hover_effect_green.ServerEventDef"
		rampDownName = "obelisk_hover_effect_ramp_down_green.ServerEventDef"
	default:
		return nil, nil
	}
	hoverPacket, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: 16, ObjectID: objectID,
		Asset: util.HashID(hoverName), IsRemovalRequested: !isActive,
		// Native 506D90 only stores effect instances on the object when field 4
		// is set (or the asset itself requests attachment). World-spawned hover
		// instances otherwise survive the later slot-removal request.
		IsForceAttached: isActive,
	})
	if err != nil {
		return nil, fmt.Errorf("hoverMarshal: %w", err)
	}
	packets := [][]byte{hoverPacket}
	if !isRampDown {
		return packets, nil
	}
	rampDownPacket, err := raknet.MarshalApplication(raknet.ObjectEffectMessage{
		ObjectID: objectID, Asset: util.HashID(rampDownName),
	})
	if err != nil {
		return nil, fmt.Errorf("rampDownMarshal: %w", err)
	}
	return append(packets, rampDownPacket), nil
}
