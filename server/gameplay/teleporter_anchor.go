package gameplay

import (
	"errors"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonesecurity "github.com/darkspinnet/darkspin/server/zone/security"
	securityraknet "github.com/darkspinnet/darkspin/server/zone/security/raknet103"
)

// The initial campaign uses recovered security routes rather than the generic
// route baseline. Retire their authored render objects just as the generic
// baseline does before retaining the server-owned presentation anchors.
func (e *gameplayPeerSession) retireInitialSecurityTeleporters() ([][]byte, error) {
	if e == nil || e.zone == nil {
		return nil, nil
	}
	routes := zonesecurity.Routes(e.binding.Level)
	if len(routes) == 0 {
		return nil, nil
	}
	director := e.zone.DirectorDefinition()
	markerIDs := make([]uint32, 0, len(routes))
	retiredMarkerIDs := make(map[uint32]bool)
	for _, set := range director.MarkerSets {
		for _, definition := range set.Definitions {
			marker, isFound := director.SelectedMarkerDefinition(definition.MarkerID)
			if !isFound || marker.MarkerID == 0 || retiredMarkerIDs[marker.MarkerID] {
				continue
			}
			for _, route := range routes {
				nounName := "SecurityTeleporter.Noun"
				if route.IsBoss {
					nounName = "BossSecurityTeleporter.Noun"
				}
				if !strings.EqualFold(marker.NounName, nounName) ||
					zonegeometry.DistanceSquared(marker.Position, route.Source) > 0.0001 {
					continue
				}
				markerIDs = append(markerIDs, marker.MarkerID)
				retiredMarkerIDs[marker.MarkerID] = true
				break
			}
		}
	}
	if len(markerIDs) == 0 {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: markerIDs})
	if err != nil {
		return nil, fmt.Errorf("securityRetire: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e *gameplayPeerSession) teleporterAnchorObjectID(sourceID uint32) (uint32, error) {
	if e == nil || e.zone == nil || e.zone.Security() == nil {
		return 0, errors.New("teleporter anchor zone unavailable")
	}
	objectID, isFound := e.zone.Security().AnchorObjectID(sourceID)
	if isFound {
		return objectID, nil
	}
	objectID, err := e.zone.ReserveObjectIDs(1)
	if err != nil {
		return 0, fmt.Errorf("anchorReserve: %w", err)
	}
	objectID, err = e.zone.Security().BindAnchor(sourceID, objectID)
	if err != nil {
		return 0, fmt.Errorf("anchorBind: %w", err)
	}
	return objectID, nil
}

// A reconnect baseline can be encoded while threats are moving. Track each
// peer's emitted state independently of the party's logical route progress.
func (e *gameplayPeerSession) syncSecurityTeleporterStates() ([][]byte, error) {
	if e == nil || e.zone == nil || e.zone.Security() == nil {
		return nil, nil
	}
	snapshot := e.zone.Security().Snapshot()
	threats := e.zone.SecurityThreats()
	if e.securityTeleporterStates == nil {
		e.securityTeleporterStates = make(map[uint32]bool)
	}
	packets := make([][]byte, 0)
	for index, objectID := range snapshot.ObjectID {
		if objectID == 0 {
			continue
		}
		teleport, isFound := zonesecurity.Route(index)
		if !isFound {
			return nil, fmt.Errorf("securityStateRoute[%d]: unavailable", index)
		}
		isActive := snapshot.Presented[index] && !zonesecurity.HasThreat(teleport, threats)
		previousState, isPresented := e.securityTeleporterStates[objectID]
		if isPresented && previousState == isActive {
			continue
		}
		statePackets, err := securityraknet.State(objectID, teleport, isActive, false)
		if err != nil {
			return nil, fmt.Errorf("securityStateMarshal[%d]: %w", index, err)
		}
		e.securityTeleporterStates[objectID] = isActive
		packets = append(packets, statePackets...)
	}
	return packets, nil
}
