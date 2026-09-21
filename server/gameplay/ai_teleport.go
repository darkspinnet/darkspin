package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonesecurity "github.com/darkspinnet/darkspin/server/zone/security"
)

// followPlayerAITeleporterLocked moves every AI-controlled co-op ally through
// the route that a human party member just used. The registry lock must be held.
func (e *gameplaySessionRegistry) followPlayerAITeleporterLocked(
	sourceKey string, source gameplayPeerSession, route game.CampaignTeleportRoute,
	orientation raknet.Quaternion, timestamp uint64, now time.Time,
) ([][]byte, int, error) {
	if e == nil || source.playerAI.isEnabled || source.binding.Mode != game.ModeChain ||
		source.zone == nil {
		return nil, 0, nil
	}
	teleport := zonesecurity.Teleport{
		Source: route.Source, Destination: route.Destination, IsBoss: route.IsBoss,
	}
	destination := raknet.Vector3{
		X: route.Destination.X, Y: route.Destination.Y, Z: route.Destination.Z,
	}
	packets := make([][]byte, 0)
	var followErrors []error
	followCount := 0
	for sessionKey, member := range e.sessions {
		if sessionKey == sourceKey || !member.playerAI.isEnabled ||
			member.binding.GameID != source.binding.GameID || member.zone != source.zone ||
			!isPlayerAISession(member) || member.isZoneTerminal() {
			continue
		}
		memberPackets, err := campaignTeleportPackets(
			member.deployedObjectID, route, teleport, orientation, timestamp,
		)
		if err != nil {
			followErrors = append(followErrors, fmt.Errorf("aiTeleportMarshal[%d]: %w", member.binding.UserID, err))
			continue
		}
		err = member.stopPlayerMovement(now)
		if err != nil {
			followErrors = append(followErrors, fmt.Errorf("aiTeleportStop[%d]: %w", member.binding.UserID, err))
			continue
		}
		member.basicSequenceSession().ReleaseHeld()
		member.campaignPlayerPursuitSession().Cancel()
		e.clearActionLeasesLocked(sessionKey, member.transportGeneration)
		err = member.teleportPlayer(now, destination)
		if err != nil {
			followErrors = append(followErrors, fmt.Errorf("aiTeleportMove[%d]: %w", member.binding.UserID, err))
			continue
		}
		member.playerAI.isMoving = false
		member.playerAI.combatGoal = destination
		member.playerAI.moveUntil = time.Time{}
		member.playerAI.nextDecisionAt = now.Add(playerAIInterval)
		member.followTargetUserID = 0
		exitSource, exitRadius, isExitFound := campaignTunnelExitTrigger(
			member.zone.DirectorDefinition().TeleportRoutes(), route,
			member.deployedCampaignFootprintRadius(),
		)
		if isExitFound {
			member.campaignTunnelExitSource = exitSource
			member.campaignTunnelExitRadius = exitRadius
			member.isCampaignTunnelExitPending = true
		}
		companionPackets, companionErr := member.teleportOwnedCompanions(
			game.Vec3(destination),
			game.Quaternion{
				X: orientation.X, Y: orientation.Y, Z: orientation.Z, W: orientation.W,
			},
		)
		if companionErr != nil {
			followErrors = append(followErrors, fmt.Errorf(
				"aiTeleportCompanion[%d]: %w", member.binding.UserID, companionErr,
			))
		}
		err = member.syncZoneHeroPose()
		if err != nil {
			followErrors = append(followErrors, fmt.Errorf("aiTeleportPose[%d]: %w", member.binding.UserID, err))
		}
		e.sessions[sessionKey] = member
		packets = append(packets, memberPackets...)
		packets = append(packets, companionPackets...)
		followCount++
	}
	return packets, followCount, errors.Join(followErrors...)
}
