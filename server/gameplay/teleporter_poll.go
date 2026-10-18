package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zoneboss "github.com/darkspinnet/darkspin/server/zone/boss"
	zonesecurity "github.com/darkspinnet/darkspin/server/zone/security"
)

// Check continuous movement and newly cleared gates between player commands.
func (e gameplayPendingRuntime) pollTeleporters(packet raknet.Packet) ([][]byte, error) {
	sessionKey := packet.Address.String()
	e.registry.mutex.Lock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	if !isFound || peerSession.transportGeneration != packet.TransportGeneration ||
		peerSession.zone == nil || !peerSession.stage.IsDungeon() ||
		!peerSession.dungeonSetup.IsCommitted() || peerSession.isRejoinPending ||
		peerSession.isZoneTerminal() || peerSession.isHeroSelectionPending ||
		peerSession.deployedObjectID == 0 || peerSession.deployedHitPoint() <= 0 ||
		(peerSession.binding.Mode != game.ModeChain && peerSession.binding.Mode != game.ModeTutorial) {
		if isFound && peerSession.transportGeneration == packet.TransportGeneration {
			peerSession.teleporterSample = teleporterContactSample{}
			e.registry.sessions[sessionKey] = peerSession
		}
		e.registry.mutex.Unlock()
		return nil, nil
	}
	now := e.now()
	orientation := raknet.Quaternion{W: 1}
	packets, route, isTeleported, err := peerSession.pollTeleporterContacts(now, packet.SourceTime, orientation)
	publications := make([]game.CampaignDirectorPublication, 0)
	if err == nil && isTeleported && peerSession.binding.Mode == game.ModeChain {
		arrival := game.Vec3(peerSession.playerPosition)
		arrivals, advanceErr := e.registry.advanceCampaignTriggersLocked(peerSession, arrival, arrival, now)
		if advanceErr != nil {
			if e.logger != nil {
				e.logger.Printf("RakNet teleporter arrival trigger deferred for %s: %v", packet.Address, advanceErr)
			}
		} else {
			for _, publication := range arrivals {
				if publication.CallbackName == zoneboss.GenericCallback {
					publications = append(publications, publication)
				}
			}
		}
	}
	e.registry.sessions[sessionKey] = peerSession
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("teleporterContact: %w", err)
	}
	if isTeleported {
		followPackets, followCount, followErr := e.registry.followPlayerAITeleporterLocked(
			sessionKey, peerSession, route, orientation, packet.SourceTime, now,
		)
		packets = append(packets, followPackets...)
		if followErr != nil && e.logger != nil {
			e.logger.Printf("RakNet teleporter ally follow completed=%d: %v", followCount, followErr)
		}
	}
	e.registry.mutex.Unlock()
	if len(publications) != 0 {
		err = e.action.movement.campaign.encounter.scheduleNamedBossTriggers(
			sessionKey, peerSession, publications,
		)
		if err != nil {
			if e.logger != nil {
				e.logger.Printf("RakNet teleporter boss scheduling deferred for %s: %v", packet.Address, err)
			}
		}
	}
	err = publishCampaignPeersAfterCommit(e.registry, packet, packets)
	if err != nil {
		return nil, fmt.Errorf("teleporterPublish: %w", err)
	}
	return packets, nil
}

func (e *gameplayPeerSession) pollTeleporterContacts(
	now time.Time, timestamp uint64, orientation raknet.Quaternion,
) ([][]byte, game.CampaignTeleportRoute, bool, error) {
	positionBeforeContact := e.playerPosition
	current := game.Vec3(e.playerPosition)
	if e.playerMotion != nil {
		position, err := e.playerMotion.SamplePosition(now)
		if err != nil {
			return nil, game.CampaignTeleportRoute{}, false, fmt.Errorf("teleporterSample: %w", err)
		}
		current = game.Vec3(position)
	}
	previous := e.previousTeleporterSample(current)
	// Consume each real segment once, including polls with no accepted route.
	// Successful relocation reseeds the sample at its destination instead.
	e.retainTeleporterSample(current)
	packets, route, isTeleported, err := e.observeCampaignTunnel(previous, current, orientation, timestamp, now)
	if err != nil {
		return nil, route, false, fmt.Errorf("teleporterTunnel: %w", err)
	}
	if !isTeleported {
		securityPackets, securityErr := e.observeSecurityTeleporter(previous, raknet.Vector3(current), now, timestamp, orientation)
		if securityErr != nil {
			return nil, route, false, fmt.Errorf("teleporterSecurity: %w", securityErr)
		}
		packets = append(packets, securityPackets...)
		isTeleported = positionBeforeContact != e.playerPosition
		if isTeleported {
			for _, teleport := range zonesecurity.Routes(e.binding.Level) {
				if teleport.Destination == game.Vec3(e.playerPosition) {
					route = game.CampaignTeleportRoute{
						Source: teleport.Source, Destination: teleport.Destination,
						IsBoss: teleport.IsBoss, IsSecurity: true,
					}
					break
				}
			}
		}
	}
	if !isTeleported {
		tutorialPackets, isTutorialTeleport, tutorialErr := e.observeTutorialTeleporter(previous, current, orientation, timestamp, now)
		if tutorialErr != nil {
			return nil, route, false, fmt.Errorf("teleporterTutorial: %w", tutorialErr)
		}
		packets = append(packets, tutorialPackets...)
		isTeleported = isTutorialTeleport
	}
	if isTeleported {
		e.playerMovementGoal = e.playerPosition
		err = e.syncZoneHeroPose()
		if err != nil {
			return nil, route, false, fmt.Errorf("teleporterPose: %w", err)
		}
	}
	securityStatePackets, err := e.syncSecurityTeleporterStates()
	if err != nil {
		return nil, route, false, fmt.Errorf("teleporterPresentation: %w", err)
	}
	packets = append(packets, securityStatePackets...)
	return packets, route, isTeleported, nil
}
