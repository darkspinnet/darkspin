package raknet103

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// MaserBeamEffect preserves ScaldronBasicMaser_Shot's nEvent.Notify contract
// (Abilities/0x97F109C9.lua, Tick instructions 106-119). The authored event
// resolves the emitter from objectId and aims at targetPoint; a position-only
// event instead creates a detached effect at the far end of the shot.
func MaserBeamEffect(plan zonenpc.AttackPlan, endpoint game.Vec3) ([]byte, error) {
	if plan.SourceObjectID == 0 || plan.Profile.TrailEffectName == "" ||
		!isFiniteVec3(endpoint) {
		return nil, errors.New("npc maser beam invalid")
	}
	asset := util.HashID(plan.Profile.TrailEffectName)
	targetPoint := vector(endpoint)
	packet, err := raknet.MarshalApplication(raknet.ServerEventContractMessage{
		Asset: &asset, ObjectID: &plan.SourceObjectID, TargetPoint: &targetPoint,
	})
	if err != nil {
		return nil, fmt.Errorf("maserBeamMarshal: %w", err)
	}
	return packet, nil
}
