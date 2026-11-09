package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
)

// The registry lock is held. Validate before retiring the outgoing hero or
// reserving the swap, so a denied voluntary deployment has no side effects.
func (e gameplayPeerSession) campaignDeploymentPosition(creatureIndex uint32, at time.Time) (raknet.Vector3, error) {
	if e.zone == nil || creatureIndex >= uint32(len(e.binding.Creatures)) {
		return raknet.Vector3{}, errors.New("deployment recipient unavailable")
	}
	radius := e.zone.CreatureActorFootprintRadius(e.binding.UserID, e.generation, creatureIndex)
	if radius <= 0 {
		return raknet.Vector3{}, errors.New("deployment footprint unavailable")
	}
	if e.playerMotion != nil {
		e.playerMotion.SetActorNavigation(e.zone.Navigation(), e.deployedCampaignNavigationRadius(), e.deployedObjectID, uint8(e.binding.Mode))
		position, err := e.playerMotion.DeploymentPosition(at, radius, uint8(e.binding.Mode))
		if err != nil {
			return raknet.Vector3{}, fmt.Errorf("deploymentMotion: %w", err)
		}
		return toRakNetPosition(position), nil
	}
	position, err := zonenavigation.DeploymentPosition(e.zone.Navigation(), zonenavigation.DeploymentRequest{
		Position: game.Vec3(e.playerPosition), FootprintRadius: radius, Mode: uint8(e.binding.Mode),
	})
	if err != nil {
		return raknet.Vector3{}, fmt.Errorf("deploymentPosition: %w", err)
	}
	return raknet.Vector3(position), nil
}
