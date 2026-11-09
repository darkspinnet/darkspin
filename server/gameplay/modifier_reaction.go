package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/sim"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// Prepare the existing controller's deletes before accepting a new control
// modifier. A different status already present is never proof of admission.
func (e *gameplaySessionRegistry) admitLaserControlModifierLocked(member *gameplayPeerSession,
	objectID uint32, kind sim.AbilityStatusKind, at, expiresAt time.Time,
) (laserModifierReaction, bool, error) {
	reaction, err := e.prepareLaserControlReactionLocked(member, objectID)
	if err != nil {
		return laserModifierReaction{}, false, fmt.Errorf("controlPrepare: %w", err)
	}
	if member == nil || member.zone == nil {
		return laserModifierReaction{}, false, errors.New("control world unavailable")
	}
	var isAdmitted bool
	switch kind {
	case sim.AbilityStatusKindStun:
		isAdmitted, err = member.zone.NPCs().AdmitStun(objectID, at, expiresAt)
	case sim.AbilityStatusKindSilence:
		isAdmitted, err = member.zone.NPCs().AdmitSilence(objectID, at, expiresAt)
	case sim.AbilityStatusKindBanish:
		isAdmitted, err = member.zone.NPCs().AdmitBanish(objectID, at, expiresAt)
	default:
		return laserModifierReaction{}, false, errors.New("unsupported laser control modifier")
	}
	if err != nil {
		return laserModifierReaction{}, false, fmt.Errorf("controlAdmission: %w", err)
	}
	return reaction, isAdmitted, nil
}

// Modifier1003 keeps its controller after ModifierInstanceAdded, but destroys
// its zones when the recipient has stun, banish or silence. Callers deliver
// reactions only after successful admission. Prepare and commit share the registry
// lock, so this reaction owns the exact controller and beam instances it encoded.
type laserModifierReaction struct {
	controllers []*campaignLaserController
	packets     [][]byte
}

func (e *gameplaySessionRegistry) prepareLaserSilenceReactionLocked(
	member *gameplayPeerSession, objectID uint32,
) (laserModifierReaction, error) {
	reaction, err := e.prepareLaserControlReactionLocked(member, objectID)
	if err != nil {
		return laserModifierReaction{}, fmt.Errorf("silenceReaction: %w", err)
	}
	return reaction, nil
}

func (e *gameplaySessionRegistry) prepareLaserControlReactionLocked(
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
// invokes this only after control-modifier admission; rejection leaves beams intact.
func (e laserModifierReaction) commit() {
	for _, controller := range e.controllers {
		for _, beam := range controller.zones {
			beam.isActive = false
		}
		controller.zones = nil
	}
}

// Preserve the caller's working session around the shared removal queue. The
// exact controllers are forgotten only after their prepared deletes are queued.
// Caller retains the registry lock from preparation through this commit.
func (e *gameplaySessionRegistry) queueLaserControlReactionLocked(sessionKey string,
	member *gameplayPeerSession, reaction laserModifierReaction,
) {
	e.sessions[sessionKey] = *member
	e.queueChannelRemovalsLocked(member.zone, reaction.packets)
	reaction.commit()
	*member = e.sessions[sessionKey]
}
