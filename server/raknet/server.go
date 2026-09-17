package raknet

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"sort"
	"sync"
	"time"
)

var offlineMagic = [16]byte{0x00, 0xff, 0xff, 0x00, 0xfe, 0xfe, 0xfe, 0xfe, 0xfd, 0xfd, 0xfd, 0xfd, 0x12, 0x34, 0x56, 0x78}

var (
	errScheduleCanceled    = errors.New("schedule canceled")
	errScheduleCommitted   = errors.New("schedule committed callback failed")
	errTransportDisconnect = errors.New("transport disconnect")
)

const (
	legacyProtocolVersion byte = 0x0d
	legacyOpenRequest     byte = 0x09
	legacyOpenReply       byte = 0x0a
)

// IsLegacyOpenRequest reports whether packet has the complete build-103
// offline-open identity used to establish a RakNet endpoint route.
func IsLegacyOpenRequest(packet []byte) bool {
	const headerLength = 1 + 1 + 8 + len(offlineMagic)
	return len(packet) >= headerLength &&
		packet[0] == legacyOpenRequest &&
		packet[1] == legacyProtocolVersion &&
		hasOfflineMagic(packet[10:])
}

// Packet is an application packet delivered after transport processing.
type Packet struct {
	ID                            PacketID
	Payload                       []byte
	Address                       *net.UDPAddr
	TraceID                       uint64
	TransportGeneration           uint64
	SourceTime                    uint64
	ClientTime                    uint64
	Schedule                      func(time.Duration, [][]byte) error
	ScheduleFunc                  func(time.Duration, func() ([][]byte, error)) error
	ScheduleGroup                 func([]ScheduledPacketProducer) (CancelSchedule, error)
	ScheduleGroupResult           func([]ScheduledPacketProducer, func(error)) (CancelSchedule, error)
	autonomousSchedule            func(time.Duration, [][]byte) error
	autonomousScheduleFunc        func(time.Duration, func() ([][]byte, error)) error
	autonomousScheduleGroup       func([]ScheduledPacketProducer) (CancelSchedule, error)
	autonomousScheduleGroupResult func([]ScheduledPacketProducer, func(error)) (CancelSchedule, error)
	response                      *packetResponse
}

// Autonomous returns a packet whose scheduling functions remain tied only to
// the transport connection. Gameplay command admission may wrap the public
// scheduling functions so canceling one player command also cancels its
// continuations. Zone-owned actors must not inherit that command lifetime.
func (p Packet) Autonomous() Packet {
	if p.autonomousSchedule != nil {
		p.Schedule = p.autonomousSchedule
	}
	if p.autonomousScheduleFunc != nil {
		p.ScheduleFunc = p.autonomousScheduleFunc
	}
	if p.autonomousScheduleGroup != nil {
		p.ScheduleGroup = p.autonomousScheduleGroup
	}
	if p.autonomousScheduleGroupResult != nil {
		p.ScheduleGroupResult = p.autonomousScheduleGroupResult
	}
	return p
}

type packetResponse struct {
	callbacks []func()
	collector *responseCommitCollector
}

type responseCommitContextKey struct{}

type responseTransportContextKey struct{}

type responseTransport struct {
	peer     *peer
	isLocked bool
	ready    chan struct{}
	once     sync.Once
	payloads [][]byte
}

type peerDeparture struct {
	address    *net.UDPAddr
	generation uint64
}

type peerRetirement struct {
	server  *Server
	address *net.UDPAddr
	peer    *peer
}

func (e peerRetirement) run() {
	isRemoved := e.server.removeExpectedPeer(e.address, e.peer)
	if !isRemoved {
		return
	}
	remote := cloneUDPAddress(e.address)
	go e.server.notifyExpiredPeers([]peerDeparture{{
		address: remote, generation: e.peer.generation,
	}})
}

type PeerDiagnostics struct {
	Generation               uint64
	IsConnected              bool
	PendingDatagramCount     int
	OldestPendingDatagramAge time.Duration
	FutureDatagramCount      int
	MissingDatagramCount     int
	PendingOrderCount        int
	SplitAssemblyCount       int
	SplitByteCount           int
}

func (e *responseTransport) unlock() {
	if e == nil || !e.isLocked || e.peer == nil {
		return
	}
	e.isLocked = false
	e.peer.outboundMu.Unlock()
}

func (e *responseTransport) release() {
	if e == nil || e.ready == nil {
		return
	}
	e.once.Do(func() {
		close(e.ready)
	})
}

func (e *responseTransport) appendPayload(payloads [][]byte) {
	if e == nil {
		return
	}
	for _, payload := range payloads {
		e.payloads = append(e.payloads, append([]byte(nil), payload...))
	}
}

type responseCommitCollector struct {
	callbacks []func()
}

func (e *responseCommitCollector) append(callbacks []func()) {
	if e == nil || len(callbacks) == 0 {
		return
	}
	e.callbacks = append(e.callbacks, callbacks...)
}

func (e *responseCommitCollector) commit() error {
	if e == nil {
		return nil
	}
	callbacks := e.callbacks
	e.callbacks = nil
	var commitErrors []error
	for index, current := range callbacks {
		err := invokeCommit(current)
		if err != nil {
			commitErrors = append(
				commitErrors, fmt.Errorf("responseCommit[%d]: %w", index, err),
			)
		}
	}
	return errors.Join(commitErrors...)
}

// AfterResponseCommit defers a state transition until every application
// response from this handler invocation has been written by managed transport.
func (p Packet) AfterResponseCommit(callback func()) error {
	if p.response == nil || callback == nil {
		return errors.New("response commit unavailable")
	}
	p.response.callbacks = append(p.response.callbacks, callback)
	return nil
}

func (p Packet) commitResponse() error {
	if p.response == nil {
		return nil
	}
	callbacks := p.response.callbacks
	p.response.callbacks = nil
	if p.response.collector != nil {
		p.response.collector.append(callbacks)
		return nil
	}
	var commitErrors []error
	for index, current := range callbacks {
		err := invokeCommit(current)
		if err != nil {
			commitErrors = append(
				commitErrors, fmt.Errorf("responseCommit[%d]: %w", index, err),
			)
		}
	}
	return errors.Join(commitErrors...)
}

// ScheduledPacketProducer creates one same-deadline application packet batch.
type ScheduledPacketProducer struct {
	Delay       time.Duration
	Produce     func() ([][]byte, error)
	AfterCommit func()
}

// CancelSchedule signals cancellation without waiting for a producer to exit.
type CancelSchedule func()

func (p Packet) ScheduleProducers(
	producers []ScheduledPacketProducer,
) (CancelSchedule, error) {
	if p.ScheduleGroupResult != nil {
		cancel, err := p.ScheduleGroupResult(producers, nil)
		if err != nil {
			return nil, fmt.Errorf("scheduleResult: %w", err)
		}
		return cancel, nil
	}
	if p.ScheduleGroup != nil {
		cancel, err := p.ScheduleGroup(producers)
		if err != nil {
			return nil, fmt.Errorf("schedule: %w", err)
		}
		return cancel, nil
	}
	return nil, errors.New("scheduler unavailable")
}

type Handler func(context.Context, Packet) ([][]byte, error)

// ControlHandler handles application-independent payloads below the gameplay
// packet range. The boolean reports whether the payload was consumed.
type ControlHandler func(context.Context, Packet) ([][]byte, bool, error)

// PollHandler produces application packets while connected transport traffic
// is active but no gameplay application message is available. It lets
// server-owned control requests wake an otherwise idle gameplay connection.
type PollHandler func(context.Context, Packet) ([][]byte, error)

// Server serves RakNet offline negotiation and Game packets over UDP.
type Server struct {
	address            string
	guid               uint64
	handler            Handler
	startTime          time.Time
	mu                 sync.Mutex
	outboundMu         sync.RWMutex
	conn               *net.UDPConn
	connGeneration     uint64
	nextPeerGeneration uint64
	connContext        context.Context
	connCancel         context.CancelFunc
	peers              map[string]*peer
	scheduleErrorLog   func(error)
	schedulePublishLog func(*net.UDPAddr, [][]byte)
	peerReplaced       func(*net.UDPAddr, uint64)
	pollHandler        PollHandler
	controlHandler     ControlHandler
	observer           observerSlot
}

// SetPeerReplacedHandler observes replacement of an existing transport peer
// after the RakNet lock has been released. It is intended for address-scoped
// gameplay lifecycle cleanup.
func (s *Server) SetPeerReplacedHandler(handler func(*net.UDPAddr, uint64)) {
	s.mu.Lock()
	s.peerReplaced = handler
	s.mu.Unlock()
}

// SetPollHandler installs the application-control poller invoked by connected
// transport traffic that does not contain a gameplay application packet.
func (s *Server) SetPollHandler(handler PollHandler) {
	s.mu.Lock()
	s.pollHandler = handler
	s.mu.Unlock()
}

// SetControlHandler installs a handler for native low-numbered session
// messages, such as the playgroup PvP capability exchange.
func (s *Server) SetControlHandler(handler ControlHandler) {
	s.mu.Lock()
	s.controlHandler = handler
	s.mu.Unlock()
}

type peer struct {
	outboundMu              sync.Mutex
	generation              uint64
	clientGUID              uint64
	mtu                     int
	address                 *net.UDPAddr
	nextDatagram            uint32
	nextMessageIndex        uint32
	nextOrderIndex          [32]uint32
	nextSplitID             uint16
	isConnected             bool
	clientTimeOrigin        uint64
	serverTimeOrigin        uint64
	isClockSynced           bool
	lastClockDriftLogSource uint64
	lastReceive             time.Time
	pendingDatagrams        map[uint32]outboundDatagram
	receive                 receiveState
}

func NewServer(address string, handler Handler) *Server {
	guidBytes := make([]byte, 8)
	_, err := rand.Read(guidBytes)
	if err != nil {
		binary.BigEndian.PutUint64(guidBytes, 1)
	}
	return &Server{
		address:   address,
		guid:      binary.BigEndian.Uint64(guidBytes),
		handler:   handler,
		startTime: time.Now(),
		peers:     make(map[string]*peer),
		scheduleErrorLog: func(err error) {
			log.Printf("RakNet scheduled publication failed: %v", err)
		},
	}
}

func (s *Server) sourceTime() uint64 {
	return uint64(time.Since(s.startTime) / time.Millisecond)
}

func (s *Server) clientTime(address *net.UDPAddr, sourceTime uint64) uint64 {
	if address == nil {
		return sourceTime
	}
	s.mu.Lock()
	currentPeer := s.peers[address.String()]
	if currentPeer == nil || !currentPeer.isClockSynced {
		s.mu.Unlock()
		return sourceTime
	}
	clientTimeOrigin := currentPeer.clientTimeOrigin
	serverTimeOrigin := currentPeer.serverTimeOrigin
	s.mu.Unlock()
	if sourceTime < serverTimeOrigin {
		return clientTimeOrigin
	}
	return clientTimeOrigin + sourceTime - serverTimeOrigin
}

// AttachConnection supplies the UDP socket used for delayed application
// messages when RakNet is hosted by the shared QoS/gameplay listener.
func (s *Server) AttachConnection(connection *net.UDPConn) {
	s.mu.Lock()
	previousCancel := s.connCancel
	s.mu.Unlock()
	if previousCancel != nil {
		previousCancel()
	}
	s.outboundMu.Lock()
	s.mu.Lock()
	if s.connCancel != nil {
		s.connCancel()
	}
	departures := s.detachPeersLocked()
	s.conn = connection
	s.connGeneration++
	s.connContext = nil
	s.connCancel = nil
	if connection != nil {
		s.connContext, s.connCancel = context.WithCancel(context.Background())
		go s.runRetransmission(s.connContext, connection, s.connGeneration)
	}
	s.mu.Unlock()
	s.outboundMu.Unlock()
	if len(departures) != 0 {
		go s.notifyExpiredPeers(departures)
	}
}

// SetScheduleErrorHandler observes asynchronous producer and publication
// failures. Passing nil restores the no-op handler.
func (s *Server) SetScheduleErrorHandler(handler func(error)) {
	s.mu.Lock()
	if handler == nil {
		handler = func(error) {}
	}
	s.scheduleErrorLog = handler
	s.mu.Unlock()
}

// SetSchedulePublishHandler observes application payloads after their encoded
// RakNet datagrams have been written successfully.
func (s *Server) SetSchedulePublishHandler(
	handler func(*net.UDPAddr, [][]byte),
) {
	s.mu.Lock()
	s.schedulePublishLog = handler
	s.mu.Unlock()
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	address, err := net.ResolveUDPAddr("udp", s.address)
	if err != nil {
		return fmt.Errorf("addressResolve: %w", err)
	}
	conn, err := net.ListenUDP("udp", address)
	if err != nil {
		return fmt.Errorf("raknetListen: %w", err)
	}
	s.AttachConnection(conn)
	defer func() {
		s.AttachConnection(nil)
		_ = conn.Close()
	}()

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()
	buffer := make([]byte, 65535)
	for {
		length, remote, readErr := conn.ReadFromUDP(buffer)
		if readErr != nil {
			if ctx.Err() != nil || errors.Is(readErr, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("packetRead: %w", readErr)
		}
		packet := append([]byte(nil), buffer[:length]...)
		responses, handleErr := s.HandleAndWriteDatagrams(ctx, remote, packet)
		if handleErr != nil || len(responses) == 0 {
			continue
		}
	}
}

func (s *Server) Close() error {
	s.mu.Lock()
	previousCancel := s.connCancel
	s.mu.Unlock()
	if previousCancel != nil {
		previousCancel()
	}
	s.outboundMu.Lock()
	s.mu.Lock()
	conn := s.detachConnectionLocked()
	departures := s.detachPeersLocked()
	s.mu.Unlock()
	s.outboundMu.Unlock()
	if len(departures) != 0 {
		go s.notifyExpiredPeers(departures)
	}
	if conn == nil {
		return nil
	}
	err := conn.Close()
	if err != nil {
		return fmt.Errorf("raknetClose: %w", err)
	}
	return nil
}

func (s *Server) detachConnectionLocked() *net.UDPConn {
	conn := s.conn
	if s.connCancel != nil {
		s.connCancel()
	}
	s.conn = nil
	s.connGeneration++
	s.connContext = nil
	s.connCancel = nil
	return conn
}

func (s *Server) detachPeersLocked() []peerDeparture {
	departures := make([]peerDeparture, 0, len(s.peers))
	for endpoint, currentPeer := range s.peers {
		if currentPeer == nil {
			delete(s.peers, endpoint)
			continue
		}
		departures = append(departures, peerDeparture{
			address:    cloneUDPAddress(currentPeer.address),
			generation: currentPeer.generation,
		})
		delete(s.peers, endpoint)
	}
	return departures
}

func (s *Server) handleDatagram(ctx context.Context, address *net.UDPAddr, packet []byte) ([]byte, error) {
	if len(packet) == 0 {
		return nil, ErrShortBuffer
	}
	switch packet[0] {
	case legacyOpenRequest:
		response, err := s.legacyOpenReply(ctx, address, packet)
		if err != nil {
			return nil, fmt.Errorf("legacyOpen: %w", err)
		}
		return response, nil
	default:
		return nil, nil
	}
}

// legacyOpenReply implements the single-datagram RakNet 3.92 negotiation used
// by Game 5.3.0.103. The request is ID, protocol, client GUID, magic, then
// zero padding to the probed MTU. The reply is ID, magic, server GUID, the
// observed client address, then zero padding to the same datagram length.
func (s *Server) legacyOpenReply(
	ctx context.Context, address *net.UDPAddr, packet []byte,
) ([]byte, error) {
	const headerLength = 1 + 1 + 8 + len(offlineMagic)
	const replyLength = 1 + len(offlineMagic) + 8 + 4 + 2
	if address == nil {
		return nil, errors.New("raknet: legacy open address missing")
	}
	if len(packet) < headerLength {
		return nil, errors.New("raknet: short legacy open request")
	}
	if len(packet) < replyLength {
		return nil, errors.New("raknet: legacy MTU is smaller than reply")
	}
	if packet[1] != legacyProtocolVersion {
		return nil, fmt.Errorf("raknet: unsupported legacy protocol %d", packet[1])
	}
	if !hasOfflineMagic(packet[10:]) {
		return nil, errors.New("raknet: invalid legacy offline magic")
	}
	clientGUID := binary.BigEndian.Uint64(packet[2:10])
	previousPeer, err := s.lockPeerForReplacement(ctx, address)
	if err != nil {
		return nil, fmt.Errorf("raknet: peer replacement: %w", err)
	}
	if previousPeer != nil {
		defer previousPeer.outboundMu.Unlock()
	}
	s.mu.Lock()
	if s.peers == nil {
		s.peers = make(map[string]*peer)
	}
	s.nextPeerGeneration++
	if s.nextPeerGeneration == 0 {
		s.nextPeerGeneration++
	}
	generation := s.nextPeerGeneration
	remote := *address
	remote.IP = append(net.IP(nil), address.IP...)
	s.peers[address.String()] = &peer{
		generation: generation, clientGUID: clientGUID, mtu: len(packet), address: &remote,
		lastReceive: time.Now(), receive: receiveState{isDatagramInitialized: true},
	}
	s.mu.Unlock()
	if previousPeer != nil {
		departure := peerDeparture{
			address:    cloneUDPAddress(address),
			generation: previousPeer.generation,
		}
		go s.notifyExpiredPeers([]peerDeparture{departure})
	}

	writer := NewWriter(len(packet))
	writer.Uint8(legacyOpenReply)
	writer.WriteBytes(offlineMagic[:])
	writer.Uint64(s.guid)
	writeLegacySystemAddress(writer, address)
	response := writer.Bytes()
	response = append(response, make([]byte, len(packet)-len(response))...)
	return response, nil
}

func (s *Server) lockPeerForReplacement(
	ctx context.Context, address *net.UDPAddr,
) (*peer, error) {
	if address == nil {
		return nil, errors.New("address missing")
	}
	for {
		s.mu.Lock()
		currentPeer := s.peers[address.String()]
		s.mu.Unlock()
		if currentPeer == nil {
			return nil, nil
		}
		isLocked := lockPeerOutbound(ctx, context.Background(), currentPeer)
		if !isLocked {
			return nil, fmt.Errorf("outboundLock: %w", ctx.Err())
		}
		s.mu.Lock()
		isCurrent := s.peers[address.String()] == currentPeer
		s.mu.Unlock()
		if isCurrent {
			return currentPeer, nil
		}
		currentPeer.outboundMu.Unlock()
		contextErr := ctx.Err()
		if contextErr != nil {
			return nil, fmt.Errorf("replacementContext: %w", contextErr)
		}
	}
}

func writeLegacySystemAddress(writer *Writer, address *net.UDPAddr) {
	ip := address.IP.To4()
	if ip == nil {
		ip = net.IPv4zero
	}
	for _, octet := range ip {
		writer.Uint8(^octet)
	}
	writer.Uint16(uint16(address.Port))
}

// HandleDatagram processes one packet for a shared UDP listener.
func (s *Server) HandleDatagram(ctx context.Context, address *net.UDPAddr, packet []byte) ([]byte, error) {
	return s.handleDatagram(ctx, address, packet)
}

// HandleDatagrams processes one incoming UDP packet and returns every UDP
// response. Connected traffic needs a standalone ACK and can also need a data
// datagram, so one input may produce more than one output. A valid connected
// datagram can return transport responses together with an application error;
// callers must publish those responses before handling the error.
func (s *Server) HandleDatagrams(ctx context.Context, address *net.UDPAddr, packet []byte) ([][]byte, error) {
	if len(packet) == 0 {
		return nil, ErrShortBuffer
	}
	s.observeDatagram(ctx, ObservationClientToServer, ObservationDatagram, address, packet)
	if packet[0] == 0xa0 || packet[0] == 0xc0 {
		err := s.lockResponseTransport(ctx, address)
		if err != nil {
			return nil, fmt.Errorf("ackOutbound: %w", err)
		}
		sequences, err := DecodeACK(packet)
		if err != nil {
			return nil, fmt.Errorf("ackDecode: %w", err)
		}
		responses, err := s.handleAcknowledgement(address, packet[0] == 0xa0, sequences)
		if err != nil {
			return nil, fmt.Errorf("ackHandle: %w", err)
		}
		return responses, nil
	}
	if packet[0]&0x80 != 0 {
		return s.handleConnectedDatagram(ctx, address, packet)
	}
	response, err := s.handleDatagram(ctx, address, packet)
	if err != nil {
		return nil, fmt.Errorf("offlineHandle: %w", err)
	}
	if len(response) == 0 {
		return nil, nil
	}
	return [][]byte{response}, nil
}

// HandleAndWriteDatagrams processes and publishes one inbound datagram.
// Application handling intentionally runs outside outboundMu so gameplay
// cancellation cannot deadlock a scheduled publication. Returned bytes are
// provided for diagnostics and have already been written, including transport
// ACK/NACK responses returned with an application error.
func (s *Server) HandleAndWriteDatagrams(ctx context.Context, address *net.UDPAddr, packet []byte) ([][]byte, error) {
	shouldNotifyRemoval := false
	currentGeneration := uint64(0)
	defer func() {
		if shouldNotifyRemoval {
			s.notifyExpiredPeers([]peerDeparture{{
				address: cloneUDPAddress(address), generation: currentGeneration,
			}})
		}
	}()
	s.mu.Lock()
	conn := s.conn
	connGeneration := s.connGeneration
	currentPeer := s.peers[address.String()]
	if currentPeer != nil {
		currentGeneration = currentPeer.generation
	}
	s.mu.Unlock()
	if conn == nil {
		return nil, errors.New("datagramConn: not listening")
	}
	commitCollector := &responseCommitCollector{}
	transport := &responseTransport{
		peer: currentPeer, ready: make(chan struct{}),
	}
	defer transport.unlock()
	defer transport.release()
	managedContext := context.WithValue(
		ctx, responseCommitContextKey{}, commitCollector,
	)
	managedContext = context.WithValue(
		managedContext, responseTransportContextKey{}, transport,
	)
	responses, handleErr := s.HandleDatagrams(managedContext, address, packet)
	if handleErr != nil && len(responses) == 0 {
		isRemoved := s.removeExpectedPeer(address, currentPeer)
		shouldNotifyRemoval = shouldNotifyRemoval || isRemoved
		return nil, fmt.Errorf("datagramHandle: %w", handleErr)
	}
	if len(packet) != 0 && packet[0] == legacyOpenRequest {
		s.mu.Lock()
		currentPeer = s.peers[address.String()]
		s.mu.Unlock()
		transport.peer = currentPeer
		if currentPeer != nil {
			currentGeneration = currentPeer.generation
		}
	}
	err := s.lockResponseTransport(managedContext, address)
	if err != nil {
		return nil, fmt.Errorf("datagramOutbound: %w", err)
	}
	encoded, err := s.encodeResponsePayloads(address, transport.payloads)
	if err != nil {
		isRemoved := s.removeExpectedPeer(address, currentPeer)
		shouldNotifyRemoval = shouldNotifyRemoval || isRemoved
		return nil, fmt.Errorf("datagramEncode: %w", err)
	}
	responses = append(responses, encoded...)
	s.outboundMu.RLock()
	s.mu.Lock()
	isCurrentConnection := s.conn == conn && s.connGeneration == connGeneration
	s.mu.Unlock()
	if !isCurrentConnection {
		s.outboundMu.RUnlock()
		isRemoved := s.removeExpectedPeer(address, currentPeer)
		shouldNotifyRemoval = shouldNotifyRemoval || isRemoved
		return nil, errors.New("datagramConn: changed")
	}
	s.mu.Lock()
	isPeerRemoved := currentPeer != nil && s.peers[address.String()] != currentPeer
	s.mu.Unlock()
	if isPeerRemoved {
		s.outboundMu.RUnlock()
		return nil, errors.New("datagramPeer: changed")
	}
	for index, response := range responses {
		_, writeErr := conn.WriteToUDP(response, address)
		if writeErr != nil {
			s.outboundMu.RUnlock()
			isRemoved := s.removeExpectedPeer(address, currentPeer)
			shouldNotifyRemoval = shouldNotifyRemoval || isRemoved
			return nil, fmt.Errorf("datagramWrite[%d]: %w", index, writeErr)
		}
		s.observeDatagram(
			managedContext, ObservationServerToClient,
			ObservationDatagram, address, response,
		)
	}
	s.outboundMu.RUnlock()
	commitErr := commitCollector.commit()
	if commitErr != nil {
		isRemoved := s.removeExpectedPeer(address, currentPeer)
		shouldNotifyRemoval = shouldNotifyRemoval || isRemoved
		return responses, fmt.Errorf("datagramCommit: %w", commitErr)
	}
	if handleErr != nil {
		isRemoved := s.removeExpectedPeer(address, currentPeer)
		shouldNotifyRemoval = shouldNotifyRemoval || isRemoved
		return responses, fmt.Errorf("datagramHandle: %w", handleErr)
	}
	return responses, nil
}

func (s *Server) handleConnectedDatagram(ctx context.Context, address *net.UDPAddr, packet []byte) ([][]byte, error) {
	datagram, err := DecodeDatagram(packet)
	if err != nil {
		return nil, fmt.Errorf("datagramDecode: %w", err)
	}
	responses := [][]byte{EncodeACK(datagram.Sequence)}
	isAccepted, missingSequences, err := s.acceptIncomingDatagram(
		address, datagram.Sequence,
	)
	if err != nil {
		return nil, fmt.Errorf("datagramReceive: %w", err)
	}
	if len(missingSequences) != 0 {
		responses = append(responses, EncodeNACK(missingSequences...))
	}
	if !isAccepted {
		return responses, nil
	}
	var handleErrors []error
	incomingPackets, receiveErr := s.prepareIncomingPackets(address, datagram.Packets)
	if receiveErr != nil {
		handleErrors = append(handleErrors, fmt.Errorf("packetReceive: %w", receiveErr))
	}
	for index, encapsulated := range incomingPackets {
		if len(encapsulated.Payload) == 0 {
			continue
		}
		s.observeApplication(ctx, address, datagram.Sequence, encapsulated)
		payloadResponses, handleErr := s.handleConnectedPayload(ctx, address, encapsulated.Payload)
		responses = append(responses, payloadResponses...)
		if errors.Is(handleErr, errTransportDisconnect) {
			break
		}
		if handleErr != nil {
			handleErrors = append(handleErrors, fmt.Errorf(
				"payloadHandle[%d]: %w", index, handleErr,
			))
			continue
		}
	}
	return responses, errors.Join(handleErrors...)
}

func (s *Server) handleConnectedPayload(ctx context.Context, address *net.UDPAddr, payload []byte) ([][]byte, error) {
	switch payload[0] {
	case 0x00:
		pong, err := s.connectedPongPayload(address, payload)
		if err != nil {
			return nil, fmt.Errorf("connectedPing: %w", err)
		}
		responses, err := s.connectedPayloadResponse(ctx, address, pong)
		if err != nil {
			return nil, fmt.Errorf("connectedPong: %w", err)
		}
		polled, err := s.pollConnected(ctx, address)
		if err != nil {
			return responses, fmt.Errorf("connectedPoll: %w", err)
		}
		return append(responses, polled...), nil
	case 0x04:
		response, err := s.connectionAcceptedPayload(address, payload)
		if err != nil {
			return nil, fmt.Errorf("connectionAccept: %w", err)
		}
		return s.connectedPayloadResponse(ctx, address, response)
	case 0x11:
		s.mu.Lock()
		currentPeer := s.peers[address.String()]
		if currentPeer != nil {
			currentPeer.isConnected = true
		}
		s.mu.Unlock()
		if currentPeer == nil {
			return nil, errors.New("newIncoming: unknown peer")
		}
		connected, err := MarshalApplication(ConnectedMessage{})
		if err != nil {
			return nil, fmt.Errorf("connectedMarshal: %w", err)
		}
		return s.connectedPayloadResponse(ctx, address, connected)
	case 0x13, 0x14:
		s.retireConnectedPeer(ctx, address)
		return nil, errTransportDisconnect
	default:
		if !s.isPeerConnected(address) {
			return nil, errors.New("connected payload from inactive peer")
		}
		if payload[0] < byte(HelloPlayerRequest) {
			s.mu.Lock()
			controlHandler := s.controlHandler
			s.mu.Unlock()
			if controlHandler != nil {
				packet := s.applicationPacket(
					ctx, address, PacketID(payload[0]), payload[1:],
				)
				packets, isHandled, err := controlHandler(ctx, packet)
				if err != nil {
					return nil, fmt.Errorf("controlHandle[%02x]: %w", payload[0], err)
				}
				if isHandled {
					if len(packets) == 0 {
						return nil, nil
					}
					if queueResponsePayload(ctx, packets) {
						return nil, nil
					}
					return s.connectedPayloadResponses(ctx, address, packets)
				}
			}
			responses, err := s.pollConnected(ctx, address)
			if err != nil {
				return nil, fmt.Errorf("controlPoll: %w", err)
			}
			return responses, nil
		}
		if s.handler == nil {
			return nil, nil
		}
		packet := s.applicationPacket(ctx, address, PacketID(payload[0]), payload[1:])
		packets, err := invokeHandler(ctx, s.handler, packet)
		if err != nil {
			return nil, fmt.Errorf("gamePacket[%02x]: %w", payload[0], err)
		}
		if len(packets) == 0 {
			return nil, nil
		}
		if queueResponsePayload(ctx, packets) {
			err = packet.commitResponse()
			if err != nil {
				return nil, fmt.Errorf("gameCommit: %w", err)
			}
			return nil, nil
		}
		err = s.lockResponseTransport(ctx, address)
		if err != nil {
			return nil, fmt.Errorf("gameOutbound: %w", err)
		}
		err = s.preflightConnectedPackets(address, ReliableOrdered, packets)
		if err != nil {
			return nil, fmt.Errorf("gamePreflight: %w", err)
		}
		responses := make([][]byte, 0, len(packets))
		for index, packet := range packets {
			encoded, encodeErr := s.encodeConnectedPackets(address, ReliableOrdered, packet)
			if encodeErr != nil {
				return nil, fmt.Errorf("gameEncode[%d]: %w", index, encodeErr)
			}
			responses = append(responses, encoded...)
		}
		err = packet.commitResponse()
		if err != nil {
			return nil, fmt.Errorf("gameCommit: %w", err)
		}
		return responses, nil
	}
}

func (s *Server) connectedPayloadResponses(
	ctx context.Context, address *net.UDPAddr, packets [][]byte,
) ([][]byte, error) {
	err := s.lockResponseTransport(ctx, address)
	if err != nil {
		return nil, fmt.Errorf("controlOutbound: %w", err)
	}
	err = s.preflightConnectedPackets(address, ReliableOrdered, packets)
	if err != nil {
		return nil, fmt.Errorf("controlPreflight: %w", err)
	}
	responses := make([][]byte, 0, len(packets))
	for index, packet := range packets {
		encoded, encodeErr := s.encodeConnectedPackets(
			address, ReliableOrdered, packet,
		)
		if encodeErr != nil {
			return nil, fmt.Errorf("controlEncode[%d]: %w", index, encodeErr)
		}
		responses = append(responses, encoded...)
	}
	return responses, nil
}

func (s *Server) connectedPongPayload(
	address *net.UDPAddr, payload []byte,
) ([]byte, error) {
	const connectedPingLength = 9
	if address == nil || len(payload) != connectedPingLength {
		return nil, errors.New("invalid connected ping")
	}
	clientTime := binary.BigEndian.Uint64(payload[1:])
	serverTime := s.sourceTime()
	s.mu.Lock()
	currentPeer := s.peers[address.String()]
	if currentPeer == nil || !currentPeer.isConnected {
		s.mu.Unlock()
		return nil, errors.New("connected ping from inactive peer")
	}
	expectedClientTime := currentPeer.clientTimeOrigin
	if serverTime >= currentPeer.serverTimeOrigin {
		expectedClientTime += serverTime - currentPeer.serverTimeOrigin
	}
	clockDrift := int64(clientTime) - int64(expectedClientTime)
	isDriftLogDue := (clockDrift < -1000 || clockDrift > 1000) &&
		serverTime-currentPeer.lastClockDriftLogSource >= 5000
	if isDriftLogDue {
		currentPeer.lastClockDriftLogSource = serverTime
	}
	s.mu.Unlock()
	if isDriftLogDue {
		log.Printf(
			"RakNet connected clock drift peer=%s client=%d expected=%d drift_ms=%d",
			address, clientTime, expectedClientTime, clockDrift,
		)
	}
	writer := NewWriter(17)
	writer.Uint8(0x03)
	writer.Uint64(clientTime)
	writer.Uint64(serverTime)
	return writer.Bytes(), nil
}

func (s *Server) retireConnectedPeer(
	ctx context.Context, address *net.UDPAddr,
) {
	if address == nil {
		return
	}
	s.mu.Lock()
	currentPeer := s.peers[address.String()]
	s.mu.Unlock()
	if currentPeer == nil {
		return
	}
	retirement := peerRetirement{
		server: s, address: cloneUDPAddress(address), peer: currentPeer,
	}
	collector, isCollector := ctx.Value(responseCommitContextKey{}).(*responseCommitCollector)
	if !isCollector {
		collector = nil
	}
	if collector == nil {
		retirement.run()
		return
	}
	collector.append([]func(){retirement.run})
}

// connectedPayloadResponse defers sequence allocation and encoding when the
// caller owns a response transport. This keeps connected control handling out
// of the per-peer outbound critical section, including when a client batches a
// connection-control payload with later application traffic.
func (s *Server) connectedPayloadResponse(
	ctx context.Context, address *net.UDPAddr, payload []byte,
) ([][]byte, error) {
	if queueResponsePayload(ctx, [][]byte{payload}) {
		return nil, nil
	}
	err := s.lockResponseTransport(ctx, address)
	if err != nil {
		return nil, fmt.Errorf("connectedOutbound: %w", err)
	}
	response, err := s.encodeConnected(address, ReliableOrdered, payload)
	if err != nil {
		return nil, fmt.Errorf("connectedEncode: %w", err)
	}
	return [][]byte{response}, nil
}

func (s *Server) isPeerConnected(address *net.UDPAddr) bool {
	if address == nil {
		return false
	}
	s.mu.Lock()
	currentPeer := s.peers[address.String()]
	isConnected := currentPeer != nil && currentPeer.isConnected
	s.mu.Unlock()
	return isConnected
}

func (s *Server) removePeer(address *net.UDPAddr) (uint64, bool) {
	if address == nil {
		return 0, false
	}
	s.mu.Lock()
	currentPeer := s.peers[address.String()]
	if currentPeer == nil {
		s.mu.Unlock()
		return 0, false
	}
	delete(s.peers, address.String())
	s.mu.Unlock()
	return currentPeer.generation, true
}

func cloneUDPAddress(address *net.UDPAddr) *net.UDPAddr {
	if address == nil {
		return nil
	}
	cloned := *address
	cloned.IP = append(net.IP(nil), address.IP...)
	return &cloned
}

func (s *Server) removeExpectedPeer(address *net.UDPAddr, expected *peer) bool {
	if address == nil || expected == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.peers[address.String()] != expected {
		return false
	}
	delete(s.peers, address.String())
	return true
}

func (s *Server) lockResponseTransport(
	ctx context.Context, address *net.UDPAddr,
) error {
	transport, isTransport := ctx.Value(responseTransportContextKey{}).(*responseTransport)
	if !isTransport {
		return nil
	}
	if transport == nil || transport.peer == nil || transport.isLocked {
		return nil
	}
	isLocked := lockPeerOutbound(ctx, context.Background(), transport.peer)
	if !isLocked {
		return fmt.Errorf("outboundLock: %w", ctx.Err())
	}
	s.mu.Lock()
	isCurrent := address != nil && s.peers[address.String()] == transport.peer
	s.mu.Unlock()
	if !isCurrent {
		transport.peer.outboundMu.Unlock()
		return errors.New("outboundPeer: changed")
	}
	transport.isLocked = true
	return nil
}

func queueResponsePayload(ctx context.Context, payloads [][]byte) bool {
	transport, isTransport := ctx.Value(responseTransportContextKey{}).(*responseTransport)
	if !isTransport {
		return false
	}
	if transport == nil {
		return false
	}
	transport.appendPayload(payloads)
	return true
}

func (s *Server) encodeResponsePayloads(
	address *net.UDPAddr, payloads [][]byte,
) ([][]byte, error) {
	if len(payloads) == 0 {
		return nil, nil
	}
	err := s.preflightConnectedPackets(address, ReliableOrdered, payloads)
	if err != nil {
		return nil, fmt.Errorf("responsePreflight: %w", err)
	}
	responses := make([][]byte, 0, len(payloads))
	for index, payload := range payloads {
		encoded, encodeErr := s.encodeConnectedPackets(
			address, ReliableOrdered, payload,
		)
		if encodeErr != nil {
			return nil, fmt.Errorf("responseEncode[%d]: %w", index, encodeErr)
		}
		responses = append(responses, encoded...)
	}
	return responses, nil
}

// PeerGeneration identifies the current address binding without exposing its
// mutable transport state.
func (s *Server) PeerGeneration(address *net.UDPAddr) uint64 {
	if address == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	currentPeer := s.peers[address.String()]
	if currentPeer == nil {
		return 0
	}
	return currentPeer.generation
}

func (s *Server) PeerDiagnostics(
	address *net.UDPAddr, generation uint64, now time.Time,
) (PeerDiagnostics, bool) {
	if address == nil || generation == 0 {
		return PeerDiagnostics{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	currentPeer := s.peers[address.String()]
	if currentPeer == nil || currentPeer.generation != generation {
		return PeerDiagnostics{}, false
	}
	diagnostics := PeerDiagnostics{
		Generation: generation, IsConnected: currentPeer.isConnected,
		PendingDatagramCount: len(currentPeer.pendingDatagrams),
		FutureDatagramCount:  len(currentPeer.receive.futureDatagrams),
		MissingDatagramCount: len(currentPeer.receive.missingDatagrams),
		PendingOrderCount:    currentPeer.receive.pendingOrderCount,
		SplitAssemblyCount:   len(currentPeer.receive.splitAssemblies),
		SplitByteCount:       currentPeer.receive.splitByteCount,
	}
	for _, datagram := range currentPeer.pendingDatagrams {
		age := now.Sub(datagram.lastSent)
		if age > diagnostics.OldestPendingDatagramAge {
			diagnostics.OldestPendingDatagramAge = age
		}
	}
	return diagnostics, true
}

// RetirePeerGeneration removes one exact stalled address binding and notifies
// gameplay asynchronously after the transport lock is released. A replacement
// generation at the same endpoint is preserved, and a blocked gameplay
// lifecycle callback cannot prevent the transport worker from detaching.
func (s *Server) RetirePeerGeneration(
	address *net.UDPAddr, generation uint64,
) bool {
	if address == nil || generation == 0 {
		return false
	}
	s.mu.Lock()
	currentPeer := s.peers[address.String()]
	if currentPeer == nil || currentPeer.generation != generation {
		s.mu.Unlock()
		return false
	}
	delete(s.peers, address.String())
	s.mu.Unlock()
	remote := cloneUDPAddress(address)
	go s.notifyExpiredPeers([]peerDeparture{{
		address: remote, generation: generation,
	}})
	return true
}

func (s *Server) pollConnected(ctx context.Context, address *net.UDPAddr) ([][]byte, error) {
	s.mu.Lock()
	pollHandler := s.pollHandler
	s.mu.Unlock()
	if pollHandler == nil {
		return nil, nil
	}
	packet := s.applicationPacket(ctx, address, 0, nil)
	packets, err := pollHandler(ctx, packet)
	if err != nil {
		return nil, fmt.Errorf("pollHandle: %w", err)
	}
	if queueResponsePayload(ctx, packets) {
		err = packet.commitResponse()
		if err != nil {
			return nil, fmt.Errorf("pollCommit: %w", err)
		}
		return nil, nil
	}
	err = s.lockResponseTransport(ctx, address)
	if err != nil {
		return nil, fmt.Errorf("pollOutbound: %w", err)
	}
	responses := make([][]byte, 0, len(packets))
	err = s.preflightConnectedPackets(address, ReliableOrdered, packets)
	if err != nil {
		return nil, fmt.Errorf("pollPreflight: %w", err)
	}
	for index, packet := range packets {
		encoded, encodeErr := s.encodeConnectedPackets(address, ReliableOrdered, packet)
		if encodeErr != nil {
			return nil, fmt.Errorf("pollEncode[%d]: %w", index, encodeErr)
		}
		responses = append(responses, encoded...)
	}
	err = packet.commitResponse()
	if err != nil {
		return nil, fmt.Errorf("pollCommit: %w", err)
	}
	return responses, nil
}

func (s *Server) preflightConnectedPackets(
	address *net.UDPAddr, reliability Reliability, payloads [][]byte,
) error {
	if address == nil {
		return errors.New("connected preflight address missing")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	currentPeer := s.peers[address.String()]
	if currentPeer == nil {
		return errors.New("connected preflight unknown peer")
	}
	mtu := currentPeer.mtu
	if mtu <= 0 {
		mtu = 1464
	}
	datagramCount := 0
	for _, payload := range payloads {
		count, err := connectedPayloadDatagramCount(mtu, reliability, len(payload))
		if err != nil {
			return fmt.Errorf("connectedPreflightSize: %w", err)
		}
		datagramCount += count
		if datagramCount > maxPendingDatagram {
			return errors.New("connected preflight batch too large")
		}
	}
	if !reliabilityHasMessageIndex(reliability) {
		return nil
	}
	if len(currentPeer.pendingDatagrams)+datagramCount > maxPendingDatagram {
		return errors.New("connected preflight retransmission window full")
	}
	for offset := 0; offset < datagramCount; offset++ {
		sequence := advanceTriad(currentPeer.nextDatagram, uint32(offset))
		if _, isFound := currentPeer.pendingDatagrams[sequence]; isFound {
			return fmt.Errorf(
				"connected preflight sequence %d remains unacknowledged", sequence,
			)
		}
	}
	return nil
}

func connectedPayloadDatagramCount(
	mtu int, reliability Reliability, payloadLength int,
) (int, error) {
	if mtu <= 0 || payloadLength < 0 {
		return 0, errors.New("invalid connected payload size")
	}
	const datagramHeaderLength = 4
	encapsulatedHeaderLength := 3
	if reliabilityHasMessageIndex(reliability) {
		encapsulatedHeaderLength += 3
	}
	if reliabilitySequenced(reliability) || reliabilityOrdered(reliability) {
		encapsulatedHeaderLength += 4
	}
	if payloadLength+datagramHeaderLength+encapsulatedHeaderLength <= mtu {
		return 1, nil
	}
	const splitHeaderLength = 10
	chunkLength := mtu - datagramHeaderLength - encapsulatedHeaderLength -
		splitHeaderLength
	if chunkLength <= 0 {
		return 0, fmt.Errorf("connected MTU %d", mtu)
	}
	return (payloadLength + chunkLength - 1) / chunkLength, nil
}

func (s *Server) applicationPacket(
	ctx context.Context, address *net.UDPAddr, id PacketID, payload []byte,
) Packet {
	sourceTime := s.sourceTime()
	commitCollector, isCommitCollector := ctx.Value(responseCommitContextKey{}).(*responseCommitCollector)
	if !isCommitCollector {
		commitCollector = nil
	}
	scheduleContext := context.WithoutCancel(ctx)
	transportGeneration := s.PeerGeneration(address)
	packet := Packet{
		ID: id, Payload: payload, Address: address,
		TraceID:             TraceID(ctx),
		TransportGeneration: transportGeneration,
		SourceTime:          sourceTime, ClientTime: s.clientTime(address, sourceTime),
		Schedule: func(delay time.Duration, packets [][]byte) error {
			return s.scheduleConnectedPackets(
				scheduleContext, address, delay, packets,
			)
		},
		ScheduleFunc: func(delay time.Duration, producer func() ([][]byte, error)) error {
			return s.scheduleConnectedPacketFunc(
				scheduleContext, address, delay, producer,
			)
		},
		ScheduleGroup: func(producers []ScheduledPacketProducer) (CancelSchedule, error) {
			return s.scheduleConnectedPacketGroup(
				scheduleContext, address, producers,
			)
		},
		ScheduleGroupResult: func(
			producers []ScheduledPacketProducer, onFailure func(error),
		) (CancelSchedule, error) {
			return s.scheduleConnectedPacketGroupResult(
				scheduleContext, address, producers, onFailure,
			)
		},
		response: &packetResponse{collector: commitCollector},
	}
	packet.autonomousSchedule = packet.Schedule
	packet.autonomousScheduleFunc = packet.ScheduleFunc
	packet.autonomousScheduleGroup = packet.ScheduleGroup
	packet.autonomousScheduleGroupResult = packet.ScheduleGroupResult
	return packet
}

func (s *Server) scheduleConnectedPackets(ctx context.Context, address *net.UDPAddr, delay time.Duration, packets [][]byte) error {
	payloads := make([][]byte, len(packets))
	for index, packet := range packets {
		payloads[index] = append([]byte(nil), packet...)
	}
	return s.scheduleConnectedPacketFunc(ctx, address, delay, func() ([][]byte, error) {
		return payloads, nil
	})
}

func (s *Server) scheduleConnectedPacketFunc(ctx context.Context, address *net.UDPAddr, delay time.Duration, producer func() ([][]byte, error)) error {
	_, err := s.scheduleConnectedPacketGroup(ctx, address, []ScheduledPacketProducer{{
		Delay: delay, Produce: producer,
	}})
	if err != nil {
		return fmt.Errorf("scheduleGroup: %w", err)
	}
	return nil
}

func (s *Server) scheduleConnectedPacketGroup(ctx context.Context, address *net.UDPAddr, producers []ScheduledPacketProducer) (CancelSchedule, error) {
	return s.scheduleConnectedPacketGroupResult(ctx, address, producers, nil)
}

func (s *Server) scheduleConnectedPacketGroupResult(
	ctx context.Context, address *net.UDPAddr, producers []ScheduledPacketProducer, onFailure func(error),
) (CancelSchedule, error) {
	if ctx == nil {
		return nil, errors.New("scheduleContext: nil")
	}
	if address == nil {
		return nil, errors.New("scheduleAddress: nil")
	}
	if len(producers) == 0 {
		return nil, errors.New("scheduleProducers: empty")
	}
	group := append([]ScheduledPacketProducer(nil), producers...)
	for index, producer := range group {
		if producer.Delay < 0 {
			return nil, fmt.Errorf("scheduleDelay[%d]: negative", index)
		}
		if producer.Produce == nil {
			return nil, fmt.Errorf("scheduleProducer[%d]: unavailable", index)
		}
	}
	sort.SliceStable(group, func(left int, right int) bool {
		return group[left].Delay < group[right].Delay
	})
	s.mu.Lock()
	currentPeer := s.peers[address.String()]
	conn := s.conn
	connGeneration := s.connGeneration
	connContext := s.connContext
	s.mu.Unlock()
	if currentPeer == nil || !currentPeer.isConnected {
		return nil, errors.New("schedulePeer: not connected")
	}
	if conn == nil || connContext == nil {
		return nil, errors.New("scheduleConn: not listening")
	}
	remote := *address
	groupContext, cancel := context.WithCancel(connContext)
	run := connectedPacketSchedule{
		server: s, conn: conn, connGeneration: connGeneration,
		peer: currentPeer, address: remote, producers: group,
		onFailure: onFailure,
	}
	go run.start(ctx, groupContext, cancel)
	return CancelSchedule(cancel), nil
}

type connectedPacketSchedule struct {
	server         *Server
	conn           *net.UDPConn
	connGeneration uint64
	peer           *peer
	address        net.UDPAddr
	producers      []ScheduledPacketProducer
	onFailure      func(error)
}

func (e connectedPacketSchedule) start(
	ctx context.Context, groupContext context.Context, cancel context.CancelFunc,
) {
	defer cancel()
	startedAt := time.Now()
	for first := 0; first < len(e.producers); {
		last, isReady := e.waitDeadline(ctx, groupContext, startedAt, first)
		if !isReady {
			return
		}
		err := e.server.produceAndSendScheduledDeadline(
			ctx, groupContext, e.conn, e.connGeneration, e.peer,
			&e.address, e.producers[first:last], first,
		)
		if err != nil {
			e.handleFailure(err)
			return
		}
		first = last
	}
}

func (e connectedPacketSchedule) waitDeadline(
	ctx context.Context, groupContext context.Context,
	startedAt time.Time, first int,
) (int, bool) {
	deadline := e.producers[first].Delay
	delay := time.Until(startedAt.Add(deadline))
	if delay < 0 {
		delay = 0
	}
	timer := time.NewTimer(delay)
	select {
	case <-ctx.Done():
		timer.Stop()
		return 0, false
	case <-groupContext.Done():
		timer.Stop()
		return 0, false
	case <-timer.C:
	}
	last := first
	for last < len(e.producers) && e.producers[last].Delay == deadline {
		last++
	}
	return last, waitResponsePublication(ctx, groupContext)
}

func (e connectedPacketSchedule) handleFailure(err error) {
	if errors.Is(err, errScheduleCanceled) {
		return
	}
	publicationErr := fmt.Errorf("schedulePublish: %w", err)
	e.server.reportScheduleError(publicationErr)
	if e.onFailure == nil || errors.Is(err, errScheduleCommitted) {
		return
	}
	callbackErr := invokeCommit(func() {
		e.onFailure(publicationErr)
	})
	if callbackErr != nil {
		e.server.reportScheduleError(fmt.Errorf(
			"scheduleFailureCallback: %w", callbackErr,
		))
	}
}

func waitResponsePublication(
	ctx context.Context, groupContext context.Context,
) bool {
	transport, isTransport := ctx.Value(responseTransportContextKey{}).(*responseTransport)
	if !isTransport {
		return true
	}
	if transport == nil || transport.ready == nil {
		return true
	}
	select {
	case <-transport.ready:
		return true
	case <-ctx.Done():
		return false
	case <-groupContext.Done():
		return false
	}
}

func isScheduleCanceled(ctx context.Context, groupContext context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	case <-groupContext.Done():
		return true
	default:
		return false
	}
}

func (s *Server) reportScheduleError(err error) {
	s.mu.Lock()
	handler := s.scheduleErrorLog
	s.mu.Unlock()
	if handler == nil {
		return
	}
	callbackErr := invokeCommit(func() {
		handler(err)
	})
	if callbackErr != nil {
		log.Printf("RakNet schedule error callback failed: %v", callbackErr)
	}
}

func (s *Server) produceAndSendScheduledDeadline(
	ctx context.Context, groupContext context.Context,
	conn *net.UDPConn, connGeneration uint64, scheduledPeer *peer,
	address *net.UDPAddr, producers []ScheduledPacketProducer, producerOffset int,
) error {
	// A deadline is an atomic production and publication boundary. Cancellation
	// prevents a later deadline from starting, but must not discard packets from
	// a producer that is already running. Gameplay cleanup can legitimately
	// cancel the schedule that delivered its terminal transition.
	if isScheduleCanceled(ctx, groupContext) {
		return errScheduleCanceled
	}
	payloads := make([][]byte, 0)
	for index, producer := range producers {
		produced, err := invokeProducer(producer.Produce)
		if err != nil {
			return fmt.Errorf("scheduleProduce[%d]: %w", producerOffset+index, err)
		}
		for _, payload := range produced {
			payloads = append(payloads, append([]byte(nil), payload...))
		}
	}
	isLocked := lockPeerOutbound(ctx, ctx, scheduledPeer)
	if !isLocked {
		return errScheduleCanceled
	}
	defer scheduledPeer.outboundMu.Unlock()
	s.outboundMu.RLock()
	s.mu.Lock()
	currentPeer := s.peers[address.String()]
	currentConn := s.conn
	isCurrentPeer := currentPeer == scheduledPeer && currentPeer != nil && currentPeer.isConnected
	isCurrentConnection := currentConn == conn && s.connGeneration == connGeneration
	s.mu.Unlock()
	if !isCurrentPeer {
		s.outboundMu.RUnlock()
		return errors.New("scheduledPeer: stale")
	}
	if !isCurrentConnection {
		s.outboundMu.RUnlock()
		return errors.New("scheduledConn: changed")
	}
	err := s.preflightConnectedPackets(address, ReliableOrdered, payloads)
	if err != nil {
		s.outboundMu.RUnlock()
		return fmt.Errorf("scheduledPreflight: %w", err)
	}
	responses := make([][]byte, 0, len(payloads))
	for index, payload := range payloads {
		encoded, encodeErr := s.encodeConnectedPackets(address, ReliableOrdered, payload)
		if encodeErr != nil {
			s.outboundMu.RUnlock()
			return fmt.Errorf("scheduledEncode[%d]: %w", index, encodeErr)
		}
		responses = append(responses, encoded...)
	}
	for index, response := range responses {
		_, err := conn.WriteToUDP(response, address)
		if err != nil {
			s.outboundMu.RUnlock()
			return fmt.Errorf("scheduledWrite[%d]: %w", index, err)
		}
		s.observeDatagram(
			ctx, ObservationServerToClient,
			ObservationDatagram, address, response,
		)
	}
	s.outboundMu.RUnlock()
	var commitErrors []error
	for index, producer := range producers {
		if producer.AfterCommit != nil {
			err := invokeCommit(producer.AfterCommit)
			if err != nil {
				commitErrors = append(
					commitErrors, fmt.Errorf(
						"scheduledCommit[%d]: %w", producerOffset+index, err,
					),
				)
			}
		}
	}
	commitErr := errors.Join(commitErrors...)
	if commitErr != nil {
		return fmt.Errorf("%w: %v", errScheduleCommitted, commitErr)
	}
	s.mu.Lock()
	publishHandler := s.schedulePublishLog
	s.mu.Unlock()
	if publishHandler != nil {
		callbackErr := invokeCommit(func() {
			publishHandler(address, payloads)
		})
		if callbackErr != nil {
			s.reportScheduleError(fmt.Errorf(
				"scheduleObserver: %w", callbackErr,
			))
		}
	}
	return nil
}

func lockPeerOutbound(
	ctx context.Context, groupContext context.Context, currentPeer *peer,
) bool {
	if currentPeer == nil {
		return false
	}
	if currentPeer.outboundMu.TryLock() {
		return true
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-groupContext.Done():
			return false
		case <-ticker.C:
			if currentPeer.outboundMu.TryLock() {
				return true
			}
		}
	}
}

func (s *Server) encodeConnectedPackets(address *net.UDPAddr, reliability Reliability, payload []byte) ([][]byte, error) {
	s.mu.Lock()
	currentPeer := s.peers[address.String()]
	if currentPeer == nil {
		s.mu.Unlock()
		return nil, errors.New("connectedEncode: unknown peer")
	}
	mtu := currentPeer.mtu
	s.mu.Unlock()
	if mtu <= 0 {
		mtu = 1464
	}
	const datagramHeaderLength = 4
	encapsulatedHeaderLength := 3
	if reliabilityHasMessageIndex(reliability) {
		encapsulatedHeaderLength += 3
	}
	if reliabilitySequenced(reliability) || reliabilityOrdered(reliability) {
		encapsulatedHeaderLength += 4
	}
	if len(payload)+datagramHeaderLength+encapsulatedHeaderLength <= mtu {
		packet, err := s.encodeConnectedOnChannel(address, reliability, 1, payload)
		if err != nil {
			return nil, fmt.Errorf("connectedSingle: %w", err)
		}
		return [][]byte{packet}, nil
	}
	const splitHeaderLength = 10
	chunkLength := mtu - datagramHeaderLength - encapsulatedHeaderLength - splitHeaderLength
	if chunkLength <= 0 {
		return nil, fmt.Errorf("connectedMTU: %d", mtu)
	}
	splitCount := (len(payload) + chunkLength - 1) / chunkLength
	s.mu.Lock()
	currentPeer = s.peers[address.String()]
	if currentPeer == nil {
		s.mu.Unlock()
		return nil, errors.New("connectedSplit: unknown peer")
	}
	if reliabilityHasMessageIndex(reliability) &&
		len(currentPeer.pendingDatagrams)+splitCount > maxPendingDatagram {
		s.mu.Unlock()
		return nil, errors.New("connectedSplit: retransmission window full")
	}
	datagramSequence := currentPeer.nextDatagram
	if reliabilityHasMessageIndex(reliability) {
		for splitIndex := 0; splitIndex < splitCount; splitIndex++ {
			sequence := advanceTriad(datagramSequence, uint32(splitIndex))
			if _, isFound := currentPeer.pendingDatagrams[sequence]; isFound {
				s.mu.Unlock()
				return nil, fmt.Errorf(
					"connectedSplit: sequence %d remains unacknowledged", sequence,
				)
			}
		}
	}
	messageIndex := currentPeer.nextMessageIndex
	const orderChannel uint8 = 1
	orderIndex := currentPeer.nextOrderIndex[orderChannel]
	splitID := currentPeer.nextSplitID
	currentPeer.nextDatagram = advanceTriad(currentPeer.nextDatagram, uint32(splitCount))
	if reliabilityHasMessageIndex(reliability) {
		currentPeer.nextMessageIndex = advanceTriad(
			currentPeer.nextMessageIndex, uint32(splitCount),
		)
	}
	if reliabilitySequenced(reliability) || reliabilityOrdered(reliability) {
		currentPeer.nextOrderIndex[orderChannel] = advanceTriad(
			currentPeer.nextOrderIndex[orderChannel], 1,
		)
	}
	currentPeer.nextSplitID++
	s.mu.Unlock()

	responses := make([][]byte, 0, splitCount)
	retained := make([]outboundDatagram, 0, splitCount)
	for splitIndex := 0; splitIndex < splitCount; splitIndex++ {
		start := splitIndex * chunkLength
		end := min(start+chunkLength, len(payload))
		sequence := advanceTriad(datagramSequence, uint32(splitIndex))
		datagram := Datagram{
			Flags: 0x84, Sequence: sequence,
			Packets: []EncapsulatedPacket{{
				Reliability:  reliability,
				MessageIndex: advanceTriad(messageIndex, uint32(splitIndex)),
				OrderIndex:   orderIndex, OrderChannel: orderChannel, IsSplit: true,
				SplitCount: uint32(splitCount), SplitID: splitID, SplitIndex: uint32(splitIndex),
				Payload: payload[start:end],
			}},
		}
		response, err := EncodeDatagram(datagram)
		if err != nil {
			return nil, fmt.Errorf("splitEncode[%d]: %w", splitIndex, err)
		}
		responses = append(responses, response)
		if reliabilityHasMessageIndex(reliability) {
			retained = append(retained, newOutboundDatagram(sequence, response))
		}
	}
	if len(retained) != 0 {
		err := s.retainDatagrams(address, currentPeer, retained)
		if err != nil {
			return nil, fmt.Errorf("splitRetain: %w", err)
		}
	}
	return responses, nil
}

func (s *Server) connectionAcceptedPayload(address *net.UDPAddr, payload []byte) ([]byte, error) {
	const requestLength = 1 + len(offlineMagic) + 8 + 8
	if len(payload) != requestLength {
		return nil, fmt.Errorf("connectionRequest: length %d, want %d", len(payload), requestLength)
	}
	if !hasOfflineMagic(payload[1:]) {
		return nil, errors.New("connectionRequest: invalid offline magic")
	}
	clientGUID := binary.BigEndian.Uint64(payload[17:25])
	clientTimeOrigin := binary.BigEndian.Uint64(payload[25:33])
	serverTimeOrigin := s.sourceTime()
	s.mu.Lock()
	currentPeer := s.peers[address.String()]
	if currentPeer == nil {
		s.mu.Unlock()
		return nil, errors.New("connectionRequest: peer did not complete offline opening")
	}
	if currentPeer.clientGUID != clientGUID {
		s.mu.Unlock()
		return nil, errors.New("connectionRequest: client GUID changed")
	}
	currentPeer.clientTimeOrigin = clientTimeOrigin
	currentPeer.serverTimeOrigin = serverTimeOrigin
	currentPeer.isClockSynced = true
	s.mu.Unlock()

	writer := NewWriter(85)
	writer.Uint8(0x0e)
	writeLegacySystemAddress(writer, address)
	writer.Uint16(0)
	writeLegacySystemAddress(writer, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 42127})
	for index := 1; index < 10; index++ {
		writeLegacySystemAddress(writer, &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	}
	writer.WriteBytes(payload[25:33])
	writer.Uint64(serverTimeOrigin)
	return writer.Bytes(), nil
}

func (s *Server) encodeConnected(address *net.UDPAddr, reliability Reliability, payload []byte) ([]byte, error) {
	return s.encodeConnectedOnChannel(address, reliability, 0, payload)
}

func (s *Server) encodeConnectedOnChannel(address *net.UDPAddr, reliability Reliability, orderChannel uint8, payload []byte) ([]byte, error) {
	s.mu.Lock()
	currentPeer := s.peers[address.String()]
	if currentPeer == nil {
		s.mu.Unlock()
		return nil, errors.New("connectedEncode: unknown peer")
	}
	if reliabilityHasMessageIndex(reliability) &&
		len(currentPeer.pendingDatagrams) >= maxPendingDatagram {
		s.mu.Unlock()
		return nil, errors.New("connectedEncode: retransmission window full")
	}
	datagramSequence := currentPeer.nextDatagram
	if reliabilityHasMessageIndex(reliability) {
		if _, isFound := currentPeer.pendingDatagrams[datagramSequence]; isFound {
			s.mu.Unlock()
			return nil, fmt.Errorf(
				"connectedEncode: sequence %d remains unacknowledged", datagramSequence,
			)
		}
	}
	messageIndex := currentPeer.nextMessageIndex
	orderIndex := currentPeer.nextOrderIndex[orderChannel]
	currentPeer.nextDatagram = advanceTriad(currentPeer.nextDatagram, 1)
	if reliabilityHasMessageIndex(reliability) {
		currentPeer.nextMessageIndex = advanceTriad(currentPeer.nextMessageIndex, 1)
	}
	if reliabilityOrdered(reliability) {
		currentPeer.nextOrderIndex[orderChannel] = advanceTriad(
			currentPeer.nextOrderIndex[orderChannel], 1,
		)
	}
	s.mu.Unlock()

	datagram := Datagram{
		Flags:    0x84,
		Sequence: datagramSequence,
		Packets: []EncapsulatedPacket{{
			Reliability:  reliability,
			MessageIndex: messageIndex,
			OrderIndex:   orderIndex,
			OrderChannel: orderChannel,
			Payload:      payload,
		}},
	}
	response, err := EncodeDatagram(datagram)
	if err != nil {
		return nil, fmt.Errorf("connectedEncode: %w", err)
	}
	if reliabilityHasMessageIndex(reliability) {
		err = s.retainDatagrams(
			address, currentPeer, []outboundDatagram{
				newOutboundDatagram(datagramSequence, response),
			},
		)
		if err != nil {
			return nil, fmt.Errorf("connectedRetain: %w", err)
		}
	}
	return response, nil
}

func hasOfflineMagic(value []byte) bool {
	if len(value) < len(offlineMagic) {
		return false
	}
	for index, expected := range offlineMagic {
		if value[index] != expected {
			return false
		}
	}
	return true
}
