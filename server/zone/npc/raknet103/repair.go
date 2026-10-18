package raknet103

import (
	"errors"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// Repair stops and turns toward the corpse before its authored animation.
// Its Lua does not request the generic resurrection trail/impact effects.
func RepairCast(source, target zonenpc.Snapshot, timestamp uint64) ([][]byte, error) {
	if source.Plan.ObjectID == 0 || target.Plan.ObjectID == 0 {
		return nil, errors.New("invalid repair presentation")
	}
	plan := zonenpc.AttackPlan{
		SourceObjectID: source.Plan.ObjectID, TargetObjectID: target.Plan.ObjectID,
		SourcePosition: source.Plan.Position, TargetPosition: target.Plan.Position,
	}
	return marshalMessages([]raknet.ApplicationMessage{
		attackTurn(plan),
		raknet.SetAnimationStateMessage{
			ObjectID: source.Plan.ObjectID, State: util.HashID("zlm_minn_tc_2_rez"),
			Timestamp: timestamp, Scale: 1,
		},
	}, "repairCast")
}
