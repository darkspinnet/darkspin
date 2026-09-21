package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
)

// The initiating client owns native pursuit after its acknowledgement. Peers
// never receive that acknowledgement and need an explicit movement goal.
func (e campaignAbilityCommandRuntime) publishPursuitToPeers(
	packet raknet.Packet, objectID uint32, source raknet.Vector3,
	targetObjectID uint32, target game.Vec3, stopDistance float32,
) error {
	packets, err := actionraknet.PursuitRedirect(
		objectID, game.Vec3(source), targetObjectID, target, stopDistance,
	)
	if err != nil {
		return fmt.Errorf("pursuitPeerMove: %w", err)
	}
	err = publishCampaignPeersAfterCommit(e.registry, packet, packets)
	if err != nil {
		return fmt.Errorf("pursuitPeerPublish: %w", err)
	}
	return nil
}
