package udp

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/darkspinnet/darkspin/server/qos"
	"github.com/darkspinnet/darkspin/server/raknet"
)

const (
	udpPacketLimit          = 65535
	udpRouteIdleTimeout     = 5 * time.Minute
	udpRouteCleanupInterval = time.Minute
	rakNetPeerQueueLimit    = 256
	rakNetDispatchWarning   = 250 * time.Millisecond
	rakNetDispatchTimeout   = 5 * time.Second
	partyEnvelopeLength     = 8
)

var partyEnvelope = [partyEnvelopeLength]byte{'D', 'S', 'P', 'G', 1, 1, 0, 0}

type udpProtocol uint8

const (
	udpProtocolUnknown udpProtocol = iota
	udpProtocolQoS
	udpProtocolRakNet
)

type udpEndpointRoute struct {
	protocol udpProtocol
	lastSeen time.Time
}

type rakNetPeerWork struct {
	remote     *net.UDPAddr
	packet     []byte
	channel    rakNetChannel
	receivedAt time.Time
	traceID    uint64
}

type rakNetChannel uint8

const (
	rakNetChannelGameplay rakNetChannel = iota
	rakNetChannelParty
)

type rakNetPeerWorker struct {
	mu                sync.Mutex
	queue             chan rakNetPeerWork
	cancel            context.CancelFunc
	currentTraceID    uint64
	currentRequestID  uint8
	currentReceivedAt time.Time
	currentStartedAt  time.Time
	lastDuration      time.Duration
	lastFailure       string
}

type SnapshotDiagnostics struct {
	Peer                    raknet.PeerDiagnostics
	QueueDepth              int
	CurrentTraceID          uint64
	CurrentRequestID        uint8
	CurrentQueueDelay       time.Duration
	CurrentDispatchDuration time.Duration
	LastDispatchDuration    time.Duration
	LastFailure             string
}

func (s *SharedServer) SnapshotDiagnostics(
	remote string, generation uint64, now time.Time,
) (SnapshotDiagnostics, bool) {
	address, err := net.ResolveUDPAddr("udp", remote)
	if err != nil {
		return SnapshotDiagnostics{}, false
	}
	peerDiagnostics, isFound := s.raknet.PeerDiagnostics(address, generation, now)
	if !isFound {
		return SnapshotDiagnostics{}, false
	}
	key := fmt.Sprintf("%d:%s", rakNetChannelGameplay, remote)
	s.mu.Lock()
	worker := s.workers[key]
	s.mu.Unlock()
	diagnostics := SnapshotDiagnostics{Peer: peerDiagnostics}
	if worker == nil {
		return diagnostics, true
	}
	diagnostics.QueueDepth = len(worker.queue)
	worker.mu.Lock()
	diagnostics.CurrentTraceID = worker.currentTraceID
	diagnostics.CurrentRequestID = worker.currentRequestID
	if !worker.currentReceivedAt.IsZero() {
		diagnostics.CurrentQueueDelay = worker.currentStartedAt.Sub(worker.currentReceivedAt)
	}
	if !worker.currentStartedAt.IsZero() {
		diagnostics.CurrentDispatchDuration = now.Sub(worker.currentStartedAt)
	}
	diagnostics.LastDispatchDuration = worker.lastDuration
	diagnostics.LastFailure = worker.lastFailure
	worker.mu.Unlock()
	return diagnostics, true
}

type rakNetPeerResult struct {
	responses [][]byte
	protocol  string
	err       error
}

type SharedServer struct {
	// Keep the atomically accessed field first for Windows 386 alignment.
	nextTraceID uint64
	address     string
	logger      *log.Logger
	raknet      *raknet.Server
	partyRaknet *raknet.Server

	mu         sync.Mutex
	connection *net.UDPConn
	cancel     context.CancelFunc
	routes     map[string]udpEndpointRoute
	workers    map[string]*rakNetPeerWorker
}

func NewSharedServer(address string, logger *log.Logger, handler raknet.Handler) *SharedServer {
	rakNetServer := raknet.NewServer("", handler)
	rakNetServer.SetScheduleErrorHandler(func(err error) {
		logger.Printf("RakNet scheduled publication failed: %v", err)
	})
	rakNetServer.SetSchedulePublishHandler(func(address *net.UDPAddr, payloads [][]byte) {
		for _, payload := range payloads {
			if len(payload) < 5 ||
				(payload[0] != byte(raknet.ObjectPlayerMove) &&
					payload[0] != byte(raknet.LocomotionUnreliable)) {
				continue
			}
			objectID := binary.LittleEndian.Uint32(payload[1:5])
			if objectID <= 3 {
				continue
			}
			logger.Printf(
				"RakNet scheduled NPC locomotion written remote=%s opcode=%#02x object=%d length=%d payload=%x",
				address, payload[0], objectID, len(payload), payload,
			)
		}
	})
	return &SharedServer{
		address: address,
		logger:  logger,
		raknet:  rakNetServer,
		routes:  make(map[string]udpEndpointRoute),
		workers: make(map[string]*rakNetPeerWorker),
	}
}

func (s *SharedServer) SetPollHandler(handler raknet.PollHandler) {
	s.raknet.SetPollHandler(handler)
}

func (s *SharedServer) SetControlHandler(handler raknet.ControlHandler) {
	s.raknet.SetControlHandler(handler)
}

// SetRakNetObserver installs the gameplay transport's observational sink.
func (s *SharedServer) SetRakNetObserver(observer raknet.Observer) {
	s.raknet.SetObserver(observer)
}

// SetPartyHandlers installs an independent RakNet session space for Fang-
// enveloped party relay datagrams received on this server's UDP socket.
func (s *SharedServer) SetPartyHandlers(
	handler raknet.Handler, controlHandler raknet.ControlHandler,
	peerReplacedHandler func(*net.UDPAddr, uint64),
) {
	partyRaknet := raknet.NewServer("", handler)
	partyRaknet.SetControlHandler(controlHandler)
	partyRaknet.SetPeerReplacedHandler(peerReplacedHandler)
	partyRaknet.SetScheduleErrorHandler(func(err error) {
		s.logger.Printf("Party RakNet scheduled publication failed: %v", err)
	})
	s.partyRaknet = partyRaknet
}

func (s *SharedServer) SetPeerReplacedHandler(
	handler func(*net.UDPAddr, uint64),
) {
	s.raknet.SetPeerReplacedHandler(handler)
}

func (s *SharedServer) ListenAndServe(ctx context.Context) error {
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	address, err := net.ResolveUDPAddr("udp", s.address)
	if err != nil {
		return fmt.Errorf("udpResolve: %w", err)
	}
	connection, err := net.ListenUDP("udp", address)
	if err != nil {
		return fmt.Errorf("udpListen: %w", err)
	}
	s.mu.Lock()
	s.connection = connection
	s.cancel = cancel
	s.mu.Unlock()
	s.raknet.AttachConnection(connection)
	if s.partyRaknet != nil {
		s.partyRaknet.AttachConnection(connection)
	}
	defer s.Close()
	go s.runRouteCleanup(serveCtx)
	go func() {
		<-serveCtx.Done()
		_ = s.Close()
	}()
	packet := make([]byte, udpPacketLimit)
	for {
		count, remote, readErr := connection.ReadFromUDP(packet)
		if readErr != nil {
			if serveCtx.Err() != nil || errors.Is(readErr, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("udpRead: %w", readErr)
		}
		partyPacket, isParty := unwrapPartyEnvelope(packet[:count])
		if isParty {
			if s.partyRaknet == nil {
				s.logger.Printf("Party RakNet packet from %s rejected: handler unavailable", remote)
				continue
			}
			s.enqueueRakNet(serveCtx, remote, partyPacket, rakNetChannelParty)
			continue
		}
		routeProtocol := s.endpointProtocol(remote)
		if routeProtocol == udpProtocolRakNet ||
			isRakNetRouteIdentifier(packet[:count]) ||
			(routeProtocol == udpProtocolUnknown && isRakNetPacket(packet[:count])) {
			s.enqueueRakNet(serveCtx, remote, packet[:count], rakNetChannelGameplay)
			continue
		}
		response, protocol, processErr := s.processPacketSafe(
			serveCtx, remote, packet[:count],
		)
		if processErr != nil {
			s.logger.Printf("%s packet from %s rejected: %v", protocol, remote, processErr)
			continue
		}
		if len(response) == 0 {
			continue
		}
		for _, datagram := range response {
			_, writeErr := connection.WriteToUDP(datagram, remote)
			if writeErr != nil && serveCtx.Err() == nil {
				return fmt.Errorf("udpWrite: %w", writeErr)
			}
		}
	}
}

func (s *SharedServer) enqueueRakNet(
	ctx context.Context, remote *net.UDPAddr, packet []byte, channel rakNetChannel,
) {
	remoteCopy := *remote
	remoteCopy.IP = append(net.IP(nil), remote.IP...)
	work := rakNetPeerWork{
		remote: &remoteCopy, packet: append([]byte(nil), packet...),
		channel: channel, receivedAt: time.Now(),
		traceID: atomic.AddUint64(&s.nextTraceID, 1),
	}
	key := fmt.Sprintf("%d:%s", channel, remote)
	rakNetServer := s.rakNetServer(channel)
	peerGeneration := rakNetServer.PeerGeneration(remote)
	s.mu.Lock()
	worker := s.workers[key]
	if worker == nil {
		workerCtx, cancel := context.WithCancel(ctx)
		worker = &rakNetPeerWorker{
			queue:  make(chan rakNetPeerWork, rakNetPeerQueueLimit),
			cancel: cancel,
		}
		s.workers[key] = worker
		go s.runRakNetPeer(workerCtx, key, worker)
	}
	isQueued := false
	select {
	case worker.queue <- work:
		isQueued = true
	default:
	}
	if !isQueued && s.workers[key] == worker {
		delete(s.workers, key)
		if channel == rakNetChannelGameplay {
			delete(s.routes, remote.String())
		}
	}
	s.mu.Unlock()
	if !isQueued {
		if worker.cancel != nil {
			worker.cancel()
		}
		isRetired := rakNetServer.RetirePeerGeneration(remote, peerGeneration)
		s.logger.Printf(
			"RakNet peer queue full trace=%d remote=%s generation=%d capacity=%d retired=%t; worker discarded for reconnect",
			work.traceID, remote, peerGeneration, rakNetPeerQueueLimit, isRetired,
		)
	}
}

func (s *SharedServer) runRakNetPeer(
	ctx context.Context, key string, worker *rakNetPeerWorker,
) {
	if worker.cancel != nil {
		defer worker.cancel()
	}
	idleTimer := time.NewTimer(udpRouteIdleTimeout)
	defer idleTimer.Stop()
	defer func() {
		s.mu.Lock()
		if s.workers[key] == worker {
			delete(s.workers, key)
		}
		s.mu.Unlock()
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-idleTimer.C:
			s.mu.Lock()
			isIdle := s.workers[key] == worker && len(worker.queue) == 0
			if isIdle {
				delete(s.workers, key)
			}
			s.mu.Unlock()
			if isIdle {
				return
			}
			idleTimer.Reset(udpRouteIdleTimeout)
		case work := <-worker.queue:
			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}
			idleTimer.Reset(udpRouteIdleTimeout)
			isHealthy := s.processRakNetWork(ctx, key, worker, work)
			if !isHealthy {
				return
			}
		}
	}
}

func (s *SharedServer) processRakNetWork(
	ctx context.Context, key string, worker *rakNetPeerWorker,
	work rakNetPeerWork,
) bool {
	queueDelay := time.Since(work.receivedAt)
	startedAt := time.Now()
	requestID := byte(0)
	if len(work.packet) != 0 {
		requestID = work.packet[0]
	}
	rakNetServer := s.rakNetServer(work.channel)
	peerGeneration := rakNetServer.PeerGeneration(work.remote)
	worker.mu.Lock()
	worker.currentTraceID = work.traceID
	worker.currentRequestID = requestID
	worker.currentReceivedAt = work.receivedAt
	worker.currentStartedAt = startedAt
	worker.mu.Unlock()
	traceCtx := raknet.WithTraceID(ctx, work.traceID)
	workCtx, cancel := context.WithCancel(traceCtx)
	defer cancel()
	result := make(chan rakNetPeerResult, 1)
	go func() {
		response, protocol, err := s.processRakNetPacketSafe(
			workCtx, work.remote, work.packet, rakNetServer, work.channel,
		)
		result <- rakNetPeerResult{
			responses: response, protocol: protocol, err: err,
		}
	}()
	warningTimer := time.NewTimer(rakNetDispatchWarning)
	defer warningTimer.Stop()
	select {
	case current := <-result:
		s.completeRakNetWork(ctx, worker, work, current, queueDelay, startedAt)
		return true
	case <-ctx.Done():
		return false
	case <-warningTimer.C:
		diagnostics, isDiagnosticsFound := rakNetServer.PeerDiagnostics(
			work.remote, peerGeneration, time.Now(),
		)
		if !isDiagnosticsFound {
			diagnostics = raknet.PeerDiagnostics{Generation: peerGeneration}
		}
		s.logger.Printf(
			"RakNet peer dispatch remains active trace=%d remote=%s queue_delay=%s duration=%s queue=%d request_id=%#x request_length=%d generation=%d connected=%t reliable_pending=%d reliable_oldest=%s receive_future=%d receive_missing=%d ordered_pending=%d split_pending=%d split_bytes=%d",
			work.traceID, work.remote, queueDelay, time.Since(startedAt), len(worker.queue),
			requestID, len(work.packet), peerGeneration,
			diagnostics.IsConnected, diagnostics.PendingDatagramCount,
			diagnostics.OldestPendingDatagramAge,
			diagnostics.FutureDatagramCount, diagnostics.MissingDatagramCount,
			diagnostics.PendingOrderCount, diagnostics.SplitAssemblyCount,
			diagnostics.SplitByteCount,
		)
	}
	timeoutTimer := time.NewTimer(rakNetDispatchTimeout - rakNetDispatchWarning)
	defer timeoutTimer.Stop()
	select {
	case current := <-result:
		s.completeRakNetWork(ctx, worker, work, current, queueDelay, startedAt)
		return true
	case <-ctx.Done():
		return false
	case <-timeoutTimer.C:
	}
	cancel()
	diagnostics, isDiagnosticsFound := rakNetServer.PeerDiagnostics(
		work.remote, peerGeneration, time.Now(),
	)
	if !isDiagnosticsFound {
		diagnostics = raknet.PeerDiagnostics{Generation: peerGeneration}
	}
	isRetired := rakNetServer.RetirePeerGeneration(
		work.remote, peerGeneration,
	)
	s.mu.Lock()
	if s.workers[key] == worker {
		delete(s.workers, key)
	}
	if work.channel == rakNetChannelGameplay {
		delete(s.routes, work.remote.String())
	}
	s.mu.Unlock()
	stack := make([]byte, 256*1024)
	stackLength := runtime.Stack(stack, true)
	s.logger.Printf(
		"RakNet peer dispatch timed out trace=%d remote=%s duration=%s queued=%d request_id=%#x request_length=%d generation=%d retired=%t connected=%t reliable_pending=%d reliable_oldest=%s receive_future=%d receive_missing=%d ordered_pending=%d split_pending=%d split_bytes=%d; worker detached for reconnect\n%s",
		work.traceID, work.remote, time.Since(startedAt), len(worker.queue), requestID,
		len(work.packet), peerGeneration, isRetired,
		diagnostics.IsConnected, diagnostics.PendingDatagramCount,
		diagnostics.OldestPendingDatagramAge,
		diagnostics.FutureDatagramCount, diagnostics.MissingDatagramCount,
		diagnostics.PendingOrderCount, diagnostics.SplitAssemblyCount,
		diagnostics.SplitByteCount, stack[:stackLength],
	)
	return false
}

func (s *SharedServer) completeRakNetWork(
	ctx context.Context, worker *rakNetPeerWorker, work rakNetPeerWork,
	result rakNetPeerResult, queueDelay time.Duration, startedAt time.Time,
) {
	duration := time.Since(startedAt)
	worker.mu.Lock()
	worker.lastDuration = duration
	worker.lastFailure = ""
	if result.err != nil {
		worker.lastFailure = result.err.Error()
	}
	worker.currentTraceID = 0
	worker.currentRequestID = 0
	worker.currentReceivedAt = time.Time{}
	worker.currentStartedAt = time.Time{}
	worker.mu.Unlock()
	requestID := byte(0)
	if len(work.packet) != 0 {
		requestID = work.packet[0]
	}
	if queueDelay >= 100*time.Millisecond || duration >= rakNetDispatchWarning {
		s.logger.Printf(
			"RakNet slow peer dispatch trace=%d remote=%s queue_delay=%s duration=%s queue=%d request_id=%#x request_length=%d",
			work.traceID, work.remote, queueDelay, duration, len(worker.queue), requestID,
			len(work.packet),
		)
	}
	if result.err != nil {
		s.logger.Printf(
			"%s packet trace=%d from %s rejected: %v",
			result.protocol, work.traceID, work.remote, result.err,
		)
		return
	}
	if len(result.responses) != 0 {
		s.writeResponses(ctx, work.remote, result.responses)
	}
}

func (s *SharedServer) writeResponses(
	ctx context.Context, remote *net.UDPAddr, response [][]byte,
) {
	s.mu.Lock()
	connection := s.connection
	s.mu.Unlock()
	if connection == nil {
		return
	}
	for _, datagram := range response {
		_, err := connection.WriteToUDP(datagram, remote)
		if err != nil && ctx.Err() == nil {
			s.logger.Printf("UDP response to %s failed: %v", remote, err)
			return
		}
	}
}

func (s *SharedServer) runRouteCleanup(ctx context.Context) {
	ticker := time.NewTicker(udpRouteCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.expireRoutes(now)
		}
	}
}

func (s *SharedServer) expireRoutes(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	expiredCount := 0
	for endpoint, route := range s.routes {
		if now.Sub(route.lastSeen) <= udpRouteIdleTimeout {
			continue
		}
		delete(s.routes, endpoint)
		expiredCount++
	}
	return expiredCount
}

func (s *SharedServer) processPacket(ctx context.Context, remote *net.UDPAddr, packet []byte) ([][]byte, string, error) {
	protocol := s.endpointProtocol(remote)
	if isRakNetRouteIdentifier(packet) {
		protocol = udpProtocolRakNet
	}
	if protocol == udpProtocolUnknown {
		protocol = udpProtocolQoS
		if isRakNetPacket(packet) {
			protocol = udpProtocolRakNet
		}
	}
	if protocol == udpProtocolRakNet {
		return s.processRakNetPacket(
			ctx, remote, packet, s.raknet, rakNetChannelGameplay,
		)
	}
	response, err := qos.ProcessPacket(packet, remote)
	if err == nil && len(response) != 0 {
		s.bindEndpoint(remote, udpProtocolQoS)
	}
	if len(response) == 0 {
		return nil, "QoS", err
	}
	return [][]byte{response}, "QoS", err
}

func (s *SharedServer) processRakNetPacket(
	ctx context.Context, remote *net.UDPAddr, packet []byte,
	rakNetServer *raknet.Server, channel rakNetChannel,
) ([][]byte, string, error) {
	s.mu.Lock()
	isAttached := s.connection != nil
	s.mu.Unlock()
	var responses [][]byte
	var err error
	if isAttached {
		responses, err = rakNetServer.HandleAndWriteDatagrams(ctx, remote, packet)
	} else {
		responses, err = rakNetServer.HandleDatagrams(ctx, remote, packet)
	}
	if err == nil && channel == rakNetChannelGameplay && isRakNetRouteIdentifier(packet) {
		s.bindEndpoint(remote, udpProtocolRakNet)
	}
	protocol := "RakNet"
	if channel == rakNetChannelParty {
		protocol = "Party RakNet"
	}
	if err == nil && len(responses) != 0 {
		for _, datagram := range responses {
			responsePrefixLength := min(len(datagram), 96)
			s.logger.Printf(
				"%s packet trace=%d from %s answered request_id=%#x request_length=%d response_id=%#x response_length=%d response_prefix=%x",
				protocol, raknet.TraceID(ctx), remote, packet[0], len(packet),
				datagram[0], len(datagram), datagram[:responsePrefixLength],
			)
		}
	}
	if !isAttached && err == nil && len(responses) == 0 && len(packet) != 0 &&
		packet[0] != 0xa0 && packet[0] != 0xc0 {
		prefixLength := min(len(packet), 96)
		s.logger.Printf(
			"%s packet trace=%d from %s produced no response id=%#x length=%d prefix=%x",
			protocol, raknet.TraceID(ctx), remote, packet[0], len(packet), packet[:prefixLength],
		)
	}
	if isAttached {
		return nil, protocol, err
	}
	return responses, protocol, err
}

func (s *SharedServer) processRakNetPacketSafe(
	ctx context.Context, remote *net.UDPAddr, packet []byte,
	rakNetServer *raknet.Server, channel rakNetChannel,
) (responses [][]byte, protocol string, err error) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		responses = nil
		protocol = "UDP"
		err = fmt.Errorf("packetPanic: %v\n%s", recovered, debug.Stack())
	}()
	return s.processRakNetPacket(ctx, remote, packet, rakNetServer, channel)
}

func (s *SharedServer) rakNetServer(channel rakNetChannel) *raknet.Server {
	if channel == rakNetChannelParty && s.partyRaknet != nil {
		return s.partyRaknet
	}
	return s.raknet
}

func unwrapPartyEnvelope(packet []byte) ([]byte, bool) {
	if len(packet) <= partyEnvelopeLength {
		return nil, false
	}
	for index, marker := range partyEnvelope {
		if packet[index] != marker {
			return nil, false
		}
	}
	return packet[partyEnvelopeLength:], true
}

func (s *SharedServer) processPacketSafe(
	ctx context.Context, remote *net.UDPAddr, packet []byte,
) (response [][]byte, protocol string, err error) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		response = nil
		protocol = "UDP"
		err = fmt.Errorf("packetPanic: %v\n%s", recovered, debug.Stack())
	}()
	return s.processPacket(ctx, remote, packet)
}

func (s *SharedServer) endpointProtocol(remote *net.UDPAddr) udpProtocol {
	if remote == nil {
		return udpProtocolUnknown
	}
	key := remote.String()
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	route, isFound := s.routes[key]
	if !isFound {
		return udpProtocolUnknown
	}
	if now.Sub(route.lastSeen) > udpRouteIdleTimeout {
		delete(s.routes, key)
		return udpProtocolUnknown
	}
	route.lastSeen = now
	s.routes[key] = route
	return route.protocol
}

func (s *SharedServer) bindEndpoint(remote *net.UDPAddr, protocol udpProtocol) {
	if remote == nil || protocol == udpProtocolUnknown {
		return
	}
	s.mu.Lock()
	s.routes[remote.String()] = udpEndpointRoute{protocol: protocol, lastSeen: time.Now()}
	s.mu.Unlock()
}

func isRakNetRouteIdentifier(packet []byte) bool {
	return raknet.IsLegacyOpenRequest(packet)
}

func isRakNetPacket(packet []byte) bool {
	if len(packet) == 0 {
		return false
	}
	if len(packet) >= 8 {
		version := binary.BigEndian.Uint32(packet[4:8])
		if version == 1 || version == 2 {
			return false
		}
	}
	return true
}

func (s *SharedServer) Close() error {
	s.mu.Lock()
	connection := s.connection
	cancel := s.cancel
	s.connection = nil
	s.cancel = nil
	s.routes = make(map[string]udpEndpointRoute)
	s.workers = make(map[string]*rakNetPeerWorker)
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.raknet.AttachConnection(nil)
	if s.partyRaknet != nil {
		s.partyRaknet.AttachConnection(nil)
	}
	if connection == nil {
		return nil
	}
	err := connection.Close()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}
