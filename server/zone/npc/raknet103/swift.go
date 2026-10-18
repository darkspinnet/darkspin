package raknet103

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func swiftAuraMessage(plan zonenpc.SpawnPlan) raknet.ModifierCreatedMessage {
	return raknet.ModifierCreatedMessage{
		TargetID: plan.ObjectID, SourceID: plan.SwiftAuraSourceObjectID,
		ModifierGUID:      util.HashID(zonenpc.SwiftModifierName),
		InstanceID:        zoneeffect.NounSwiftModifierInstanceID(plan.ObjectID),
		StartMilliseconds: permanentModifierStartMilliseconds, StackCount: 1,
		DurationMilliseconds: uint32(zonenpc.EliteModifierDuration.Milliseconds()),
	}
}

// SwiftChanges encodes the semantic zone changes for both the current peer
// and its allies. A supplying-source switch retires the old unique instance.
func SwiftChanges(changes []zonenpc.SwiftChange) ([][]byte, error) {
	packets := make([][]byte, 0, len(changes)*3)
	for _, change := range changes {
		plan := change.Target.Plan
		messages := make([]raknet.ApplicationMessage, 0, 3)
		if change.PreviousSourceObjectID != plan.SwiftAuraSourceObjectID {
			if change.PreviousSourceObjectID != 0 {
				messages = append(messages, raknet.ModifierDeletedMessage{
					TargetID: plan.ObjectID, InstanceID: zoneeffect.NounSwiftModifierInstanceID(plan.ObjectID),
				})
			}
			if plan.SwiftAuraSourceObjectID != 0 {
				messages = append(messages, swiftAuraMessage(plan))
			}
		}
		messages = append(messages, raknet.AttributeDataUpdateMessage{
			ObjectID: plan.ObjectID, Value: map[uint8]float32{48: change.MovementSpeedBuff},
		})
		for _, message := range messages {
			packet, err := raknet.MarshalApplication(message)
			if err != nil {
				return nil, fmt.Errorf("swiftMarshal: %w", err)
			}
			packets = append(packets, packet)
		}
	}
	return packets, nil
}
