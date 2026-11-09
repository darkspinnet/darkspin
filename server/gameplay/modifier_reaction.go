package gameplay

import (
	"fmt"

	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// Modifier1003 keeps its controller after ModifierInstanceAdded, but destroys
// its zones when the recipient has stun, banish or silence. Psistorm supplies
// silence only after successful admission. Prepare and commit share the registry
// lock, so this reaction owns the exact controller and beam instances it encoded.
type laserModifierReaction struct {
	controllers []*campaignLaserController
	packets     [][]byte
}

func (e *gameplaySessionRegistry) prepareLaserSilenceReactionLocked(
	member *gameplayPeerSession, objectID uint32,
) (laserModifierReaction, error) {
	reaction := laserModifierReaction{}
	if member == nil || member.zone == nil {
		return reaction, nil
	}
	controllers := make(map[*campaignLaserController]struct{})
	controller := member.campaignNPCLaserZones[objectID]
	if controller != nil {
		controllers[controller] = struct{}{}
	}
	for _, observer := range e.sessions {
		if observer.zone != member.zone {
			continue
		}
		controller = observer.campaignNPCLaserZones[objectID]
		if controller != nil {
			controllers[controller] = struct{}{}
		}
	}
	for controller := range controllers {
		for _, beam := range controller.zones {
			if !beam.isActive {
				continue
			}
			packets, err := npcraknet.LaserZoneEnd(beam.objectIDs)
			if err != nil {
				return laserModifierReaction{}, fmt.Errorf("modifierLaserEnd: %w", err)
			}
			reaction.packets = append(reaction.packets, packets...)
		}
		reaction.controllers = append(reaction.controllers, controller)
	}
	return reaction, nil
}

// Queue the prepared packets before forgetting the accepted zones. The caller
// invokes this only after silence admission; rejection leaves every beam intact.
func (e laserModifierReaction) commit() {
	for _, controller := range e.controllers {
		for _, beam := range controller.zones {
			beam.isActive = false
		}
		controller.zones = nil
	}
}
