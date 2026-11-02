package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// RandomTeleport modifier 0xB6F1EB76 resets its reaction after its 200ms lifetime.
type randomTeleport struct {
	objectID    uint32
	destination game.Vec3
	moveAt      time.Time
	resetAt     time.Time
	reservation zoneaction.ReleaseReservation
	isMoved     bool
}

func (e *gameplayPeerSession) advanceRandomTeleport(now time.Time, timestamp uint64) ([][]byte, error) {
	state := e.randomTeleport
	if state.objectID == 0 {
		return nil, nil
	}
	if state.objectID != e.deployedObjectID || e.deployedHitPoint() <= 0 || !e.abilityReleaseSession().IsCurrent(state.reservation) {
		e.randomTeleport = randomTeleport{}
		return nil, nil
	}
	packets := make([][]byte, 0)
	if !state.isMoved && !now.Before(state.moveAt) {
		movementPackets, err := marshalPlayerMovementCorrection(state.objectID, raknet.Vector3(state.destination), raknet.Quaternion{W: 1})
		if err != nil {
			return nil, fmt.Errorf("teleportPlacement: %w", err)
		}
		err = e.teleportPlayer(now, raknet.Vector3(state.destination))
		if err != nil {
			return nil, fmt.Errorf("teleportMove: %w", err)
		}
		err = e.syncZoneHeroPose()
		if err != nil {
			return nil, fmt.Errorf("teleportPose: %w", err)
		}
		e.playerMovementGoal = raknet.Vector3(state.destination)
		packets = append(packets, movementPackets...)
		state.isMoved = true
		e.randomTeleport = state
	}
	if !now.Before(state.resetAt) {
		resetPacket, err := npcraknet.ResetAnimation(state.objectID, timestamp)
		if err != nil {
			return nil, fmt.Errorf("teleportReset: %w", err)
		}
		packets = append(packets, resetPacket)
		e.randomTeleport = randomTeleport{}
	}
	return packets, nil
}
