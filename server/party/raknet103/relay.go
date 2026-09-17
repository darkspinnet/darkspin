// Package raknet103 adapts build-103 party control traffic to server relay.
package raknet103

import (
	"context"
	"log"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
)

const (
	partyCapabilityMessage raknet.PacketID = 7
	partyReadyMessage      raknet.PacketID = 8
)

type memberRoute struct {
	partyID uint32
	userID  int64
}

type memberTransport struct {
	address  *net.UDPAddr
	schedule func(time.Duration, [][]byte) error
}

// Relay owns the server-side routing state for native party control packets.
// Client-published endpoints are retained only as authentication hints and are
// never projected to another client.
type Relay struct {
	endpoint netip.AddrPort
	logger   *log.Logger

	mu                        sync.Mutex
	routesByEndpoint          map[netip.AddrPort]memberRoute
	routesByUserID            map[int64]memberRoute
	registeredEndpointsByUser map[int64][]netip.AddrPort
	transportsByUserID        map[int64]memberTransport
	statesByUserID            map[int64]map[raknet.PacketID]byte
}

// NewRelay creates a relay advertised through every playgroup network field.
func NewRelay(endpoint netip.AddrPort, logger *log.Logger) *Relay {
	return &Relay{
		endpoint:                  endpoint,
		logger:                    logger,
		routesByEndpoint:          make(map[netip.AddrPort]memberRoute),
		routesByUserID:            make(map[int64]memberRoute),
		registeredEndpointsByUser: make(map[int64][]netip.AddrPort),
		transportsByUserID:        make(map[int64]memberTransport),
		statesByUserID:            make(map[int64]map[raknet.PacketID]byte),
	}
}

// Endpoint returns the only client-visible party endpoint.
func (e *Relay) Endpoint() (netip.AddrPort, bool) {
	if e == nil || !e.endpoint.IsValid() || e.endpoint.Port() == 0 {
		return netip.AddrPort{}, false
	}
	return e.endpoint, true
}

// RegisterMember records the endpoints submitted by one authenticated Blaze
// member. They are used only to bind the later RakNet transport to that member.
func (e *Relay) RegisterMember(
	partyID uint32, userID int64, endpoints []netip.AddrPort,
) {
	if e == nil || partyID == 0 || userID <= 0 {
		return
	}
	e.mu.Lock()
	e.removeMemberLocked(userID)
	route := memberRoute{partyID: partyID, userID: userID}
	e.routesByUserID[userID] = route
	registeredEndpoints := make([]netip.AddrPort, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if !endpoint.IsValid() || endpoint.Port() == 0 {
			continue
		}
		endpoint = netip.AddrPortFrom(endpoint.Addr().Unmap(), endpoint.Port())
		e.routesByEndpoint[endpoint] = route
		registeredEndpoints = append(registeredEndpoints, endpoint)
	}
	e.registeredEndpointsByUser[userID] = registeredEndpoints
	e.mu.Unlock()
}

// RemoveMember removes every routing and retained-state reference for a user.
func (e *Relay) RemoveMember(partyID uint32, userID int64) {
	if e == nil {
		return
	}
	e.mu.Lock()
	route, isFound := e.routesByUserID[userID]
	if isFound && route.partyID == partyID {
		e.removeMemberLocked(userID)
	}
	e.mu.Unlock()
}

// RemoveParty removes every routing and retained-state reference for a party.
func (e *Relay) RemoveParty(partyID uint32) {
	if e == nil {
		return
	}
	e.mu.Lock()
	userIDs := make([]int64, 0)
	for userID, route := range e.routesByUserID {
		if route.partyID == partyID {
			userIDs = append(userIDs, userID)
		}
	}
	for _, userID := range userIDs {
		e.removeMemberLocked(userID)
	}
	e.mu.Unlock()
}

func (e *Relay) removeMemberLocked(userID int64) {
	for _, endpoint := range e.registeredEndpointsByUser[userID] {
		route, isFound := e.routesByEndpoint[endpoint]
		if isFound && route.userID == userID {
			delete(e.routesByEndpoint, endpoint)
		}
	}
	delete(e.registeredEndpointsByUser, userID)
	delete(e.routesByUserID, userID)
	delete(e.transportsByUserID, userID)
	delete(e.statesByUserID, userID)
}

// HandleControl relays the recovered two-byte PvP capability and ready
// messages. Unknown control packets retain the ordinary gameplay poll path.
func (e *Relay) HandleControl(
	_ context.Context, packet raknet.Packet,
) ([][]byte, bool, error) {
	if e == nil || (packet.ID != partyCapabilityMessage && packet.ID != partyReadyMessage) ||
		len(packet.Payload) != 1 || packet.Address == nil {
		return nil, false, nil
	}
	endpoint, isValid := udpAddrPort(packet.Address)
	if !isValid {
		return nil, false, nil
	}
	e.mu.Lock()
	route, isFound := e.routesByEndpoint[endpoint]
	if !isFound && endpoint.Addr().IsLoopback() && e.endpoint.Addr().IsLoopback() {
		route, isFound = e.uniqueRouteForPortLocked(endpoint.Port())
	}
	if !isFound {
		e.mu.Unlock()
		return nil, false, nil
	}
	e.transportsByUserID[route.userID] = memberTransport{
		address: cloneUDPAddress(packet.Address), schedule: packet.Schedule,
	}
	states := e.statesByUserID[route.userID]
	if states == nil {
		states = make(map[raknet.PacketID]byte)
		e.statesByUserID[route.userID] = states
	}
	states[packet.ID] = packet.Payload[0]
	writtenPacket := []byte{byte(packet.ID), packet.Payload[0]}
	responses := make([][]byte, 0)
	destinations := make([]memberTransport, 0)
	for userID, peerRoute := range e.routesByUserID {
		if userID == route.userID || peerRoute.partyID != route.partyID {
			continue
		}
		for _, messageID := range []raknet.PacketID{
			partyCapabilityMessage, partyReadyMessage,
		} {
			state, isRetained := e.statesByUserID[userID][messageID]
			if isRetained {
				responses = append(responses, []byte{byte(messageID), state})
			}
		}
		transport, isConnected := e.transportsByUserID[userID]
		if isConnected && transport.schedule != nil {
			destinations = append(destinations, transport)
		}
	}
	if e.logger != nil {
		e.logger.Printf(
			"Party relay control party_id=%d user_id=%d remote=%s subtype=%d state=%d peers=%d",
			route.partyID, route.userID, packet.Address, packet.ID,
			packet.Payload[0], len(destinations),
		)
	}
	e.mu.Unlock()
	for _, destination := range destinations {
		err := destination.schedule(0, [][]byte{writtenPacket})
		if err != nil && e.logger != nil {
			e.logger.Printf(
				"Party relay delivery to %s deferred: %v",
				destination.address, err,
			)
		}
	}
	return responses, true, nil
}

// HandleApplication discards non-control payloads sent to the logical party
// transport. Party clients use only the recovered low-numbered state messages.
func (e *Relay) HandleApplication(
	_ context.Context, _ raknet.Packet,
) ([][]byte, error) {
	return nil, nil
}

func (e *Relay) uniqueRouteForPortLocked(port uint16) (memberRoute, bool) {
	matched := memberRoute{}
	isFound := false
	for endpoint, route := range e.routesByEndpoint {
		if endpoint.Port() != port {
			continue
		}
		if isFound && route.userID != matched.userID {
			return memberRoute{}, false
		}
		matched = route
		isFound = true
	}
	return matched, isFound
}

// DisconnectTransport forgets only the ephemeral RakNet binding while
// retaining Blaze-authoritative party membership and published endpoints.
func (e *Relay) DisconnectTransport(address *net.UDPAddr) {
	if e == nil || address == nil {
		return
	}
	e.mu.Lock()
	for userID, transport := range e.transportsByUserID {
		if transport.address != nil && transport.address.String() == address.String() {
			delete(e.transportsByUserID, userID)
			delete(e.statesByUserID, userID)
		}
	}
	e.mu.Unlock()
}

func udpAddrPort(address *net.UDPAddr) (netip.AddrPort, bool) {
	if address == nil || address.Port <= 0 || address.Port > 65535 {
		return netip.AddrPort{}, false
	}
	ip, isFound := netip.AddrFromSlice(address.IP)
	if !isFound {
		return netip.AddrPort{}, false
	}
	return netip.AddrPortFrom(ip.Unmap(), uint16(address.Port)), true
}

func cloneUDPAddress(address *net.UDPAddr) *net.UDPAddr {
	if address == nil {
		return nil
	}
	cloned := *address
	cloned.IP = append(net.IP(nil), address.IP...)
	return &cloned
}
