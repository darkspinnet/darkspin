package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	geometryraknet "github.com/darkspinnet/darkspin/server/zone/geometry/raknet103"
)

// A teleport can legitimately place the actor at the selected enemy's center.
// Target-directed facing then has no planar direction. Preserve an actual
// command aim or authored forward for the ordinary melee sector instead of
// letting a zero retained pose fail later in the scheduled hit producer.
func campaignMeleeFacing(peerSession gameplayPeerSession, command raknet.ActionCommandData, targetPosition raknet.Vector3) (raknet.Vector3, error) {
	if !isFiniteZonePosition(peerSession.playerPosition) || !isFiniteZonePosition(targetPosition) || command.Ability == nil {
		return raknet.Vector3{}, errors.New("melee position or command is unavailable")
	}
	var facing raknet.Vector3
	if targetPosition.X != peerSession.playerPosition.X || targetPosition.Y != peerSession.playerPosition.Y {
		facing = geometryraknet.Direction(peerSession.playerPosition, targetPosition)
		if isMeleeFacingAvailable(facing) {
			return facing, nil
		}
	}
	for _, aim := range []raknet.Vector3{command.Ability.CursorPosition, command.Ability.TargetPosition} {
		if !isReportedZonePosition(aim) || !isFiniteZonePosition(aim) {
			continue
		}
		if aim.X == peerSession.playerPosition.X && aim.Y == peerSession.playerPosition.Y {
			continue
		}
		facing = geometryraknet.Direction(peerSession.playerPosition, aim)
		if isMeleeFacingAvailable(facing) {
			return facing, nil
		}
	}
	if isMeleeFacingAvailable(peerSession.attackPose.facing) {
		return peerSession.attackPose.facing, nil
	}
	forward, err := geometryraknet.Forward(command.Common.Orientation)
	if err != nil {
		return raknet.Vector3{}, fmt.Errorf("meleeForward: %w", err)
	}
	if !isMeleeFacingAvailable(forward) {
		return raknet.Vector3{}, errors.New("melee command has no planar aim or authored forward")
	}
	return forward, nil
}

func isMeleeFacingAvailable(facing raknet.Vector3) bool {
	return isFiniteZonePosition(facing) && (facing.X != 0 || facing.Y != 0)
}
