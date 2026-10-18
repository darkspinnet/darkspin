package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
	zonesecurity "github.com/darkspinnet/darkspin/server/zone/security"
	securityraknet "github.com/darkspinnet/darkspin/server/zone/security/raknet103"
)

func (e *gameplayPeerSession) observeCampaignTunnel(
	previous game.Vec3, current game.Vec3, orientation raknet.Quaternion,
	timestamp uint64, now time.Time,
) ([][]byte, game.CampaignTeleportRoute, bool, error) {
	if e == nil || e.binding.Mode != game.ModeChain || e.zone == nil ||
		e.zone.Security() == nil {
		return nil, game.CampaignTeleportRoute{}, false, nil
	}
	statePackets, err := e.syncCampaignTeleporterStates()
	if err != nil {
		return nil, game.CampaignTeleportRoute{}, false,
			fmt.Errorf("campaignTunnelState: %w", err)
	}
	routes := e.zone.DirectorDefinition().TeleportRoutes()
	transferRoutes := routes
	isTunnelReady := e.campaignTunnelTransfer != nil
	if isTunnelReady {
		if now.Before(e.campaignTunnelTransfer.readyAt) {
			return statePackets, game.CampaignTeleportRoute{}, false, nil
		}
		transferRoutes = []game.CampaignTeleportRoute{e.campaignTunnelTransfer.route}
	}
	if e.isCampaignTunnelExitPending {
		footprintRadius := e.deployedCampaignFootprintRadius()
		if isCampaignTunnelContact(
			previous, current, e.campaignTunnelExitSource,
			e.campaignTunnelExitRadius, footprintRadius,
		) {
			if !isCampaignTunnelPointContact(
				current, e.campaignTunnelExitSource,
				e.campaignTunnelExitRadius, footprintRadius,
			) {
				e.clearCampaignTunnelExit()
			}
			return statePackets, game.CampaignTeleportRoute{}, false, nil
		}
		if isCampaignTunnelExitNear(
			current, e.campaignTunnelExitSource, footprintRadius,
		) {
			return statePackets, game.CampaignTeleportRoute{}, false, nil
		}
		e.clearCampaignTunnelExit()
		return statePackets, game.CampaignTeleportRoute{}, false, nil
	}
	for _, route := range transferRoutes {
		teleport := zonesecurity.Teleport{
			Source: route.Source, Destination: route.Destination,
			IsBoss: route.IsBoss,
		}
		if route.IsSecurity && zonesecurity.HasThreat(
			teleport, e.zone.SecurityThreats(),
		) {
			continue
		}
		if !isTunnelReady && !isCampaignTeleportRouteContact(previous, current, route, e.deployedCampaignFootprintRadius()) {
			continue
		}
		if route.IsTunnel && !isTunnelReady {
			entryPackets, entryErr := e.beginCampaignTunnel(route, current, orientation, now)
			if entryErr != nil {
				return nil, game.CampaignTeleportRoute{}, false, fmt.Errorf("tunnelEntry: %w", entryErr)
			}
			return append(statePackets, entryPackets...), game.CampaignTeleportRoute{}, false, nil
		}
		packets, err := campaignTeleportPackets(
			e.deployedObjectID, route, teleport, orientation, timestamp,
		)
		if err != nil {
			return nil, game.CampaignTeleportRoute{}, false,
				fmt.Errorf("campaignTunnelMarshal: %w", err)
		}
		if isTunnelReady {
			exitPacket, exitErr := e.campaignTunnelEvent("PlayerExitedTunnel")
			if exitErr != nil {
				return nil, game.CampaignTeleportRoute{}, false, fmt.Errorf("tunnelExit: %w", exitErr)
			}
			packets = append(packets, exitPacket)
		}
		destination := raknet.Vector3{
			X: route.Destination.X, Y: route.Destination.Y, Z: route.Destination.Z,
		}
		err = e.teleportPlayer(now, destination)
		if err != nil {
			return nil, game.CampaignTeleportRoute{}, false,
				fmt.Errorf("campaignTunnelMove: %w", err)
		}
		companionPackets, err := e.teleportOwnedCompanions(
			game.Vec3(destination),
			game.Quaternion{
				X: orientation.X, Y: orientation.Y,
				Z: orientation.Z, W: orientation.W,
			},
		)
		if err != nil {
			return nil, game.CampaignTeleportRoute{}, false,
				fmt.Errorf("campaignTunnelCompanion: %w", err)
		}
		destinationStatePackets, err := e.syncCampaignTeleporterStates()
		if err != nil {
			return nil, game.CampaignTeleportRoute{}, false,
				fmt.Errorf("campaignTunnelDestinationState: %w", err)
		}
		exitSource, exitRadius, isExitFound := campaignTunnelExitTrigger(
			routes, route, e.deployedCampaignFootprintRadius(),
		)
		if isExitFound {
			e.campaignTunnelExitSource = exitSource
			e.campaignTunnelExitRadius = exitRadius
			e.isCampaignTunnelExitPending = true
		}
		packets = append(packets, companionPackets...)
		packets = append(packets, destinationStatePackets...)
		e.campaignTunnelTransfer = nil
		return append(statePackets, packets...), route, true, nil
	}
	return statePackets, game.CampaignTeleportRoute{}, false, nil
}

func (e *gameplayPeerSession) clearCampaignTunnelExit() {
	e.isCampaignTunnelExitPending = false
	e.campaignTunnelExitSource = game.Vec3{}
	e.campaignTunnelExitRadius = 0
}

func (e *gameplayPeerSession) campaignTeleporterInitialState() ([][]byte, error) {
	if e == nil || e.binding.Mode != game.ModeChain || e.zone == nil ||
		e.zone.Security() == nil {
		return nil, nil
	}
	packets, err := e.retireInitialSecurityTeleporters()
	if err != nil {
		return nil, fmt.Errorf("campaignSecurityRetire: %w", err)
	}
	previousStates := e.campaignTeleporterStates
	e.campaignTeleporterStates = make(map[uint32]bool)
	threats := e.zone.SecurityThreats()
	routes := e.zone.DirectorDefinition().TeleportRoutes()
	for _, route := range routes {
		if !route.IsSecurity || route.MarkerID == 0 {
			continue
		}
		teleport := zonesecurity.Teleport{
			Source: route.Source, Destination: route.Destination,
			IsBoss: route.IsBoss,
		}
		objectID, err := e.teleporterAnchorObjectID(route.MarkerID)
		if err != nil {
			return nil, fmt.Errorf("campaignInitialOwner: %w", err)
		}
		removePacket, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{
			ObjectID: []uint32{route.MarkerID},
		})
		if err != nil {
			return nil, fmt.Errorf("campaignInitialRetire: %w", err)
		}
		anchorPacket, err := securityraknet.Anchor(objectID, teleport)
		if err != nil {
			return nil, fmt.Errorf("campaignInitialAnchor: %w", err)
		}
		isActive := previousStates[route.MarkerID] &&
			isCampaignTeleporterLinkActive(route, routes, threats)
		statePackets, err := securityraknet.State(objectID, teleport, isActive, false)
		if err != nil {
			return nil, fmt.Errorf("campaignTeleporterInitial[%d]: %w", route.MarkerID, err)
		}
		e.campaignTeleporterStates[route.MarkerID] = isActive
		packets = append(packets, removePacket, anchorPacket)
		packets = append(packets, statePackets...)
	}
	return packets, nil
}

func (e *gameplayPeerSession) syncCampaignTeleporterStates() ([][]byte, error) {
	if e == nil || e.binding.Mode != game.ModeChain || e.zone == nil ||
		e.zone.Security() == nil {
		return nil, nil
	}
	if e.campaignTeleporterStates == nil {
		e.campaignTeleporterStates = make(map[uint32]bool)
	}
	threats := e.zone.SecurityThreats()
	routes := e.zone.DirectorDefinition().TeleportRoutes()
	packets := make([][]byte, 0)
	for _, route := range routes {
		if !route.IsSecurity || route.MarkerID == 0 {
			continue
		}
		teleport := zonesecurity.Teleport{
			Source: route.Source, Destination: route.Destination,
			IsBoss: route.IsBoss,
		}
		previousState, isPresented := e.campaignTeleporterStates[route.MarkerID]
		isActive := isCampaignTeleporterLinkActive(route, routes, threats)
		if isPresented && previousState == isActive {
			continue
		}
		objectID, isFound := e.zone.Security().AnchorObjectID(route.MarkerID)
		if !isFound {
			// A peer must receive its owner baseline before slot updates.
			continue
		}
		statePackets, err := securityraknet.State(
			objectID, teleport, isActive, isActive,
		)
		if err != nil {
			return nil, fmt.Errorf("campaignTeleporterState[%d]: %w", route.MarkerID, err)
		}
		e.campaignTeleporterStates[route.MarkerID] = isActive
		packets = append(packets, statePackets...)
	}
	return packets, nil
}

func isCampaignTeleporterLinkActive(
	route game.CampaignTeleportRoute, routes []game.CampaignTeleportRoute,
	threats []zonesecurity.Threat,
) bool {
	teleport := zonesecurity.Teleport{
		Source: route.Source, Destination: route.Destination, IsBoss: route.IsBoss,
	}
	return !zonesecurity.HasThreat(teleport, threats)
}

func campaignTeleportPackets(
	objectID uint32, route game.CampaignTeleportRoute,
	teleport zonesecurity.Teleport, orientation raknet.Quaternion,
	timestamp uint64,
) ([][]byte, error) {
	if route.IsSecurity {
		return securityraknet.Teleport(securityraknet.TeleportRequest{
			ObjectID: objectID, Teleport: teleport,
			Orientation: orientation, Timestamp: timestamp,
		})
	}
	return actionraknet.Teleport(actionraknet.TeleportRequest{
		ObjectID: objectID,
		Position: route.Destination,
		Orientation: game.Quaternion{
			X: orientation.X, Y: orientation.Y,
			Z: orientation.Z, W: orientation.W,
		},
	})
}

func isCampaignTunnelContact(
	previous game.Vec3, current game.Vec3, source game.Vec3,
	triggerRadius float32, footprintRadius float32,
) bool {
	return zonesecurity.IsInsideTrigger(previous, current, source, triggerRadius, footprintRadius)
}

func isCampaignTeleportRouteContact(
	previous game.Vec3, current game.Vec3, route game.CampaignTeleportRoute, footprintRadius float32,
) bool {
	if !route.IsSecurity && route.TriggerVolume != nil {
		return route.CrossesTrigger(previous, current)
	}
	return isCampaignTunnelContact(previous, current, route.Source, route.TriggerRadius, footprintRadius)
}

func campaignTunnelExitTrigger(
	routes []game.CampaignTeleportRoute, departure game.CampaignTeleportRoute,
	footprintRadius float32,
) (game.Vec3, float32, bool) {
	nearestDistanceSquared := float32(0)
	nearestRoute := game.CampaignTeleportRoute{}
	isNearestFound := false
	for _, route := range routes {
		if route.MarkerID == departure.MarkerID {
			continue
		}
		deltaX := departure.Destination.X - route.Source.X
		deltaY := departure.Destination.Y - route.Source.Y
		deltaZ := departure.Destination.Z - route.Source.Z
		distanceSquared := deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ
		if isNearestFound && distanceSquared >= nearestDistanceSquared {
			continue
		}
		nearestDistanceSquared = distanceSquared
		nearestRoute = route
		isNearestFound = true
	}
	if !isNearestFound || !isCampaignTunnelExitNear(
		departure.Destination, nearestRoute.Source, footprintRadius,
	) {
		return game.Vec3{}, 0, false
	}
	return nearestRoute.Source, nearestRoute.TriggerRadius, true
}

func isCampaignTunnelExitNear(
	position game.Vec3, source game.Vec3, footprintRadius float32,
) bool {
	activationRadius := zonesecurity.ActivationRadius + max(float32(0), footprintRadius)
	deltaX := position.X - source.X
	deltaY := position.Y - source.Y
	deltaZ := position.Z - source.Z
	return deltaX*deltaX+deltaY*deltaY+deltaZ*deltaZ <=
		activationRadius*activationRadius
}

func isCampaignTunnelPointContact(
	position game.Vec3, source game.Vec3,
	triggerRadius float32, footprintRadius float32,
) bool {
	if triggerRadius <= 0 {
		triggerRadius = zonesecurity.TriggerRadius
	}
	contactRadius := triggerRadius + max(float32(0), footprintRadius)
	deltaX := position.X - source.X
	deltaY := position.Y - source.Y
	deltaZ := position.Z - source.Z
	return deltaX*deltaX+deltaY*deltaY+deltaZ*deltaZ <= contactRadius*contactRadius
}
