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
	if e == nil || e.binding.Mode != game.ModeChain || e.zone == nil {
		return nil, game.CampaignTeleportRoute{}, false, nil
	}
	statePackets, err := e.syncCampaignTeleporterStates()
	if err != nil {
		return nil, game.CampaignTeleportRoute{}, false,
			fmt.Errorf("campaignTunnelState: %w", err)
	}
	routes := e.zone.DirectorDefinition().TeleportRoutes()
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
	for _, route := range routes {
		teleport := zonesecurity.Teleport{
			Source: route.Source, Destination: route.Destination,
			IsBoss: route.IsBoss,
		}
		if route.IsSecurity && zonesecurity.HasThreat(
			teleport, e.zone.SecurityThreats(),
		) {
			continue
		}
		if !isCampaignTunnelContact(
			previous, current, route.Source, route.TriggerRadius,
			e.deployedCampaignFootprintRadius(),
		) {
			continue
		}
		packets, err := campaignTeleportPackets(
			e.deployedObjectID, route, teleport, orientation, timestamp,
		)
		if err != nil {
			return nil, game.CampaignTeleportRoute{}, false,
				fmt.Errorf("campaignTunnelMarshal: %w", err)
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
	if e == nil || e.binding.Mode != game.ModeChain || e.zone == nil {
		return nil, nil
	}
	e.campaignTeleporterStates = make(map[uint32]bool)
	packets := make([][]byte, 0)
	for _, route := range e.zone.DirectorDefinition().TeleportRoutes() {
		if !route.IsSecurity || route.MarkerID == 0 {
			continue
		}
		teleport := zonesecurity.Teleport{
			Source: route.Source, Destination: route.Destination,
			IsBoss: route.IsBoss,
		}
		statePackets, err := securityraknet.State(route.MarkerID, teleport, false, false)
		if err != nil {
			return nil, fmt.Errorf("campaignTeleporterInitial[%d]: %w", route.MarkerID, err)
		}
		e.campaignTeleporterStates[route.MarkerID] = false
		packets = append(packets, statePackets...)
	}
	return packets, nil
}

func (e *gameplayPeerSession) syncCampaignTeleporterStates() ([][]byte, error) {
	if e == nil || e.binding.Mode != game.ModeChain || e.zone == nil {
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
		previousState := e.campaignTeleporterStates[route.MarkerID]
		isActive := isCampaignTeleporterLinkActive(route, routes, threats)
		if previousState == isActive {
			continue
		}
		statePackets, err := securityraknet.State(
			route.MarkerID, teleport, isActive, isActive,
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
	if !zonesecurity.HasThreat(teleport, threats) {
		return true
	}
	for _, linkedRoute := range routes {
		if linkedRoute.MarkerID != route.DestinationMarkerID ||
			linkedRoute.DestinationMarkerID != route.MarkerID ||
			!linkedRoute.IsSecurity {
			continue
		}
		linkedTeleport := zonesecurity.Teleport{
			Source: linkedRoute.Source, Destination: linkedRoute.Destination,
			IsBoss: linkedRoute.IsBoss,
		}
		return !zonesecurity.HasThreat(linkedTeleport, threats)
	}
	return false
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
	delta := game.Vec3{
		X: current.X - previous.X,
		Y: current.Y - previous.Y,
		Z: current.Z - previous.Z,
	}
	lengthSquared := delta.X*delta.X + delta.Y*delta.Y + delta.Z*delta.Z
	projection := float32(0)
	if lengthSquared > 0 {
		projection = ((source.X-previous.X)*delta.X +
			(source.Y-previous.Y)*delta.Y +
			(source.Z-previous.Z)*delta.Z) / lengthSquared
		projection = min(float32(1), max(float32(0), projection))
	}
	x := previous.X + projection*delta.X - source.X
	y := previous.Y + projection*delta.Y - source.Y
	z := previous.Z + projection*delta.Z - source.Z
	if triggerRadius <= 0 {
		triggerRadius = zonesecurity.TriggerRadius
	}
	contactRadius := triggerRadius + max(float32(0), footprintRadius)
	return x*x+y*y+z*z <= contactRadius*contactRadius
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
