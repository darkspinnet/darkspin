// Package qos implements the Game UDP quality-of-service probe server.
package qos

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"

	"github.com/darkspinnet/darkspin/server/protocoltrace"
)

const maxPacketSize = 10240

var errSelfEndpoint = errors.New("QoS remote matches listener")

// Server handles Game QoS probe packets over UDP.
type Server struct {
	address   string
	logger    *log.Logger
	isVerbose bool
	recorder  protocoltrace.Recorder

	mu         sync.Mutex
	connection *net.UDPConn
}

// NewServer creates a QoS server bound by ListenAndServe.
func NewServer(host string, port uint16, logger *log.Logger, isVerbose bool, recorder protocoltrace.Recorder) *Server {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &Server{
		address:   net.JoinHostPort(host, fmt.Sprintf("%d", port)),
		logger:    logger,
		isVerbose: isVerbose,
		recorder:  recorder,
	}
}

// ListenAndServe runs until context cancellation or a socket error.
func (s *Server) ListenAndServe(ctx context.Context) error {
	address, err := net.ResolveUDPAddr("udp", s.address)
	if err != nil {
		return fmt.Errorf("qosResolve: %w", err)
	}
	connection, err := net.ListenUDP("udp", address)
	if err != nil {
		return fmt.Errorf("qosListen: %w", err)
	}

	s.mu.Lock()
	s.connection = connection
	s.mu.Unlock()
	defer s.Close()

	go func() {
		<-ctx.Done()
		s.Close()
	}()

	packet := make([]byte, maxPacketSize)
	for {
		count, remote, readErr := connection.ReadFromUDP(packet)
		if readErr != nil {
			if ctx.Err() != nil || errors.Is(readErr, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("qosRead: %w", readErr)
		}
		if sameEndpoint(connection.LocalAddr(), remote) {
			s.tracePacket("in", remote, packet[:count], errSelfEndpoint)
			s.logger.Printf("QoS packet from %s rejected because it matches the listener", remote)
			continue
		}

		response, processErr := ProcessPacket(packet[:count], remote)
		if processErr != nil {
			s.tracePacket("in", remote, packet[:count], processErr)
			s.logger.Printf("QoS packet from %s rejected: %v", remote, processErr)
			continue
		}
		s.tracePacket("in", remote, packet[:count], nil)
		if s.isVerbose {
			s.logger.Printf("QoS(%d) from %s", count, remote)
		}
		_, writeErr := connection.WriteToUDP(response, remote)
		if writeErr == nil {
			s.tracePacket("out", remote, response, nil)
		}
		if writeErr != nil && ctx.Err() == nil {
			s.logger.Printf("QoS response to %s failed: %v", remote, writeErr)
		}
	}
}

func sameEndpoint(local net.Addr, remote *net.UDPAddr) bool {
	localUDP, isValid := local.(*net.UDPAddr)
	if !isValid || localUDP == nil || remote == nil || localUDP.Port != remote.Port {
		return false
	}
	if localUDP.IP.Equal(remote.IP) {
		return true
	}
	return localUDP.IP.IsUnspecified() && remote.IP.IsLoopback()
}

func (s *Server) tracePacket(direction string, remote *net.UDPAddr, packet []byte, packetErr error) {
	if s.recorder == nil {
		return
	}
	event := protocoltrace.Event{Protocol: "qos", Direction: direction, Kind: "packet", Length: len(packet)}
	if remote != nil {
		event.Remote = remote.String()
	}
	if len(packet) >= 8 {
		event.RequestID = binary.BigEndian.Uint32(packet[0:4])
		event.Version = binary.BigEndian.Uint32(packet[4:8])
	}
	if packetErr != nil {
		event.Error = packetErr.Error()
	}
	s.recorder.Record(event)
}

// Close stops the UDP listener. It is safe before and after ListenAndServe.
func (s *Server) Close() error {
	s.mu.Lock()
	connection := s.connection
	s.connection = nil
	s.mu.Unlock()
	if connection == nil {
		return nil
	}
	err := connection.Close()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return fmt.Errorf("qosClose: %w", err)
}

// ProcessPacket creates the protocol response independently of socket I/O.
func ProcessPacket(packet []byte, remote *net.UDPAddr) ([]byte, error) {
	if len(packet) < 8 {
		return nil, errors.New("QoS packet is shorter than 8 bytes")
	}
	if remote == nil {
		return nil, errors.New("QoS remote address is nil")
	}

	id := binary.BigEndian.Uint32(packet[0:4])
	version := binary.BigEndian.Uint32(packet[4:8])
	switch version {
	case 1:
		response, err := processVersionOne(packet, remote, id)
		if err != nil {
			return nil, fmt.Errorf("v1Process: %w", err)
		}
		return response, nil
	case 2:
		return processVersionTwo(id), nil
	default:
		return nil, fmt.Errorf("unsupported QoS version %d", version)
	}
}

func processVersionOne(packet []byte, remote *net.UDPAddr, id uint32) ([]byte, error) {
	if len(packet) < 20 {
		return nil, errors.New("QoS version 1 packet is shorter than 20 bytes")
	}
	ip := remote.IP.To4()
	if ip == nil {
		return nil, errors.New("QoS version 1 requires an IPv4 remote address")
	}

	response := make([]byte, 30)
	binary.BigEndian.PutUint32(response[0:4], id)
	binary.BigEndian.PutUint32(response[4:8], 1)
	copy(response[8:20], packet[8:20])
	copy(response[20:24], ip)
	binary.BigEndian.PutUint16(response[24:26], uint16(remote.Port))
	binary.BigEndian.PutUint32(response[26:30], 0x12345678)
	return response, nil
}

func processVersionTwo(id uint32) []byte {
	values := []uint32{id, 2, 0x1337, 2, 0, 2, 0xdead, 2, 0xbeef, 2, 0x8925}
	response := make([]byte, len(values)*4)
	for index, value := range values {
		binary.BigEndian.PutUint32(response[index*4:], value)
	}
	return response
}
