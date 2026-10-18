package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
)

// Chunk 534 emits PlayerEnteredTunnel, waits walkTime (2 seconds), relocates,
// then emits PlayerExitedTunnel. Native 103 handles these events as one-second
// fades (sub_4E17E0's event dispatch), independently of security-pad beam states.
type campaignTunnelTransfer struct {
	route   game.CampaignTeleportRoute
	readyAt time.Time
}

func (e *gameplayPeerSession) campaignTunnelEvent(name string) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.ClientEventMessage{
		ClientEventID: util.HashID(name),
		// Lua NotifyPlayer targets only the entrant, even when the enclosing
		// movement response is replicated to every participant.
		PlayerExclusionMask: ^(uint8(1) << e.binding.Slot),
	})
	if err != nil {
		return nil, fmt.Errorf("tunnelEvent: %w", err)
	}
	return packet, nil
}

func (e *gameplayPeerSession) beginCampaignTunnel(
	route game.CampaignTeleportRoute, position game.Vec3,
	orientation raknet.Quaternion, now time.Time,
) ([][]byte, error) {
	err := zonenavigation.ValidateTeleport(e.zone.Navigation(), route.Destination, e.deployedCampaignNavigationRadius())
	if err != nil {
		return nil, fmt.Errorf("entryDestination: %w", err)
	}
	entryPacket, err := e.campaignTunnelEvent("PlayerEnteredTunnel")
	if err != nil {
		return nil, fmt.Errorf("entryEvent: %w", err)
	}
	packets, err := actionraknet.Teleport(actionraknet.TeleportRequest{
		ObjectID: e.deployedObjectID, Position: position,
		Orientation: game.Quaternion{
			X: orientation.X, Y: orientation.Y, Z: orientation.Z, W: orientation.W,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("entryStop: %w", err)
	}
	err = e.stopPlayerMovement(now)
	if err != nil {
		return nil, fmt.Errorf("entryMotion: %w", err)
	}
	e.campaignTunnelTransfer = &campaignTunnelTransfer{
		route: route, readyAt: now.Add(2 * time.Second),
	}
	return append(packets, entryPacket), nil
}
