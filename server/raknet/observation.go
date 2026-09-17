package raknet

import (
	"context"
	"net"
	"sync"
	"time"
)

// ObservationDirection identifies which side emitted captured transport data.
type ObservationDirection string

const (
	ObservationClientToServer ObservationDirection = "client_to_server"
	ObservationServerToClient ObservationDirection = "server_to_client"
)

// ObservationStage distinguishes exact UDP datagrams from reassembled,
// delivery-ordered application payloads.
type ObservationStage string

const (
	ObservationDatagram    ObservationStage = "datagram"
	ObservationApplication ObservationStage = "application"
	ObservationRetransmit  ObservationStage = "retransmit"
)

// Observation is a lossless, transport-owned view of one RakNet event.
// Observers must return promptly and must not mutate Payload.
type Observation struct {
	CapturedAt       time.Time
	Direction        ObservationDirection
	Stage            ObservationStage
	Remote           string
	TraceID          uint64
	PeerGeneration   uint64
	DatagramSequence uint32
	Reliability      Reliability
	MessageIndex     uint32
	OrderIndex       uint32
	OrderChannel     uint8
	IsSplit          bool
	SplitCount       uint32
	SplitIndex       uint32
	SplitID          uint16
	Payload          []byte
}

// Observer consumes observational transport events without participating in
// packet admission or gameplay decisions.
type Observer interface {
	ObserveRakNet(Observation)
}

type observerSlot struct {
	mu       sync.RWMutex
	observer Observer
}

func (e *observerSlot) set(observer Observer) {
	e.mu.Lock()
	e.observer = observer
	e.mu.Unlock()
}

func (e *observerSlot) get() Observer {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.observer
}

// SetObserver installs an observational sink for this RakNet session space.
func (s *Server) SetObserver(observer Observer) {
	s.observer.set(observer)
}

func (s *Server) observeDatagram(
	ctx context.Context, direction ObservationDirection,
	stage ObservationStage, address *net.UDPAddr, payload []byte,
) {
	observer := s.observer.get()
	if observer == nil || len(payload) == 0 {
		return
	}
	observation := Observation{
		CapturedAt: time.Now().UTC(), Direction: direction, Stage: stage,
		TraceID: TraceID(ctx), Payload: append([]byte(nil), payload...),
	}
	if address != nil {
		observation.Remote = address.String()
		observation.PeerGeneration = s.PeerGeneration(address)
	}
	if payload[0]&0x80 != 0 && payload[0] != 0xa0 && payload[0] != 0xc0 {
		datagram, err := DecodeDatagram(payload)
		if err == nil {
			observation.DatagramSequence = datagram.Sequence
			if len(datagram.Packets) == 1 {
				packet := datagram.Packets[0]
				observation.Reliability = packet.Reliability
				observation.MessageIndex = packet.MessageIndex
				observation.OrderIndex = packet.OrderIndex
				observation.OrderChannel = packet.OrderChannel
				observation.IsSplit = packet.IsSplit
				observation.SplitCount = packet.SplitCount
				observation.SplitIndex = packet.SplitIndex
				observation.SplitID = packet.SplitID
			}
		}
	}
	observer.ObserveRakNet(observation)
}

func (s *Server) observeApplication(
	ctx context.Context, address *net.UDPAddr,
	datagramSequence uint32, packet EncapsulatedPacket,
) {
	observer := s.observer.get()
	if observer == nil || len(packet.Payload) == 0 {
		return
	}
	observation := Observation{
		CapturedAt: time.Now().UTC(), Direction: ObservationClientToServer,
		Stage: ObservationApplication, TraceID: TraceID(ctx),
		DatagramSequence: datagramSequence, Reliability: packet.Reliability,
		MessageIndex: packet.MessageIndex, OrderIndex: packet.OrderIndex,
		OrderChannel: packet.OrderChannel, IsSplit: packet.IsSplit,
		SplitCount: packet.SplitCount, SplitIndex: packet.SplitIndex,
		SplitID: packet.SplitID, Payload: append([]byte(nil), packet.Payload...),
	}
	if address != nil {
		observation.Remote = address.String()
		observation.PeerGeneration = s.PeerGeneration(address)
	}
	observer.ObserveRakNet(observation)
}
