package raknet103

import (
	"errors"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// ForcedJump projects JumpInDirection's retained destination, facing and
// speed/arc fields into the client's 44-byte ObjectJump physics snapshot.
// Unlike ObjectTeleport, it lets the client's mover play the displacement.
func ForcedJump(
	plan zonenpc.AttackPlan, destination game.Vec3, timestamp uint64,
	arcParameters [3]float32,
) ([][]byte, error) {
	profile := plan.Profile
	if plan.SourceObjectID == 0 || plan.TargetObjectID == 0 ||
		profile.ForcedMovementSpeed <= 0 || !isFiniteVec3(destination) {
		return nil, errors.New("invalid forced jump")
	}
	facing := plan.SourcePosition.Sub(plan.TargetPosition)
	distance := facing.Length()
	if distance <= 0 || !isFiniteVec3(facing) {
		return nil, errors.New("forced jump facing unavailable")
	}
	facing = facing.Scale(1 / distance)
	messages := make([]raknet.ApplicationMessage, 0, 3)
	if profile.ForcedMovementReactionName != "" {
		messages = append(messages, raknet.SetAnimationStateMessage{
			ObjectID:  plan.TargetObjectID,
			State:     util.HashID(profile.ForcedMovementReactionName),
			Timestamp: timestamp, Scale: 1,
		})
	}
	if profile.ForcedMovementEffectName != "" {
		messages = append(messages, raknet.ObjectEffectMessage{
			Asset:    util.HashID(profile.ForcedMovementEffectName),
			ObjectID: plan.TargetObjectID, AttackerID: plan.SourceObjectID,
		})
	}
	messages = append(messages, raknet.ObjectJumpMessage{
		ObjectID: plan.TargetObjectID, JumpPosition: vector(destination),
		JumpDirection: vector(facing),
		JumpParameters: [4]float32{
			profile.ForcedMovementSpeed,
			arcParameters[0], arcParameters[1], arcParameters[2],
		},
	})
	return marshalMessages(messages, "forcedJump")
}
