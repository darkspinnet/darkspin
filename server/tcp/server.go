package tcp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/blaze"
	recaphttp "github.com/darkspinnet/darkspin/server/http"
)

const protocolDetectTimeout = 10 * time.Second

var httpMethodPrefixes = [][]byte{
	[]byte("CONNECT "),
	[]byte("DELETE "),
	[]byte("GET "),
	[]byte("HEAD "),
	[]byte("OPTIONS "),
	[]byte("PATCH "),
	[]byte("POST "),
	[]byte("PUT "),
	[]byte("TRACE "),
}

type SharedServer struct {
	address string
	blaze   *blaze.Server
	http    *recaphttp.Server

	mu          sync.Mutex
	multiplexer *tcpProtocolMultiplexer
}

func NewSharedServer(address string, blazeServer *blaze.Server, httpServer *recaphttp.Server) *SharedServer {
	return &SharedServer{address: address, blaze: blazeServer, http: httpServer}
}

func (s *SharedServer) ListenAndServe(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.address)
	if err != nil {
		return fmt.Errorf("sharedListen[%s]: %w", s.address, err)
	}
	multiplexer := newTCPProtocolMultiplexer(listener)
	s.mu.Lock()
	s.multiplexer = multiplexer
	s.mu.Unlock()

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	type serviceResult struct {
		name string
		err  error
	}
	results := make(chan serviceResult, 3)
	go func() {
		results <- serviceResult{name: "multiplexer", err: multiplexer.Serve(runCtx)}
	}()
	go func() {
		results <- serviceResult{name: "blaze", err: s.blaze.Serve(runCtx, multiplexer.Blaze())}
	}()
	go func() {
		results <- serviceResult{name: "http", err: s.http.Serve(runCtx, multiplexer.HTTP())}
	}()

	first := <-results
	cancel()
	s.Close()
	allResults := []serviceResult{first, <-results, <-results}
	if ctx.Err() != nil {
		return nil
	}
	for _, result := range allResults {
		if result.err != nil {
			return fmt.Errorf("shared[%s]: %w", result.name, result.err)
		}
	}
	return nil
}

func (s *SharedServer) Close() {
	s.mu.Lock()
	multiplexer := s.multiplexer
	s.multiplexer = nil
	s.mu.Unlock()
	if multiplexer != nil {
		_ = multiplexer.Close()
	}
	_ = s.blaze.Close()
	_ = s.http.Close()
}

type tcpProtocolMultiplexer struct {
	listener net.Listener
	blaze    *routedListener
	http     *routedListener
	done     chan struct{}
	once     sync.Once

	mu                 sync.Mutex
	pendingConnections map[net.Conn]struct{}
}

func newTCPProtocolMultiplexer(listener net.Listener) *tcpProtocolMultiplexer {
	return &tcpProtocolMultiplexer{
		listener:           listener,
		blaze:              newRoutedListener(listener.Addr()),
		http:               newRoutedListener(listener.Addr()),
		done:               make(chan struct{}),
		pendingConnections: make(map[net.Conn]struct{}),
	}
}

func (m *tcpProtocolMultiplexer) Blaze() net.Listener {
	return m.blaze
}

func (m *tcpProtocolMultiplexer) HTTP() net.Listener {
	return m.http
}

func (m *tcpProtocolMultiplexer) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		_ = m.Close()
	}()
	for {
		connection, err := m.listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accept: %w", err)
		}
		if !m.track(connection) {
			_ = connection.Close()
			return nil
		}
		go m.route(connection)
	}
}

func (m *tcpProtocolMultiplexer) Close() error {
	var closeErr error
	m.once.Do(func() {
		close(m.done)
		closeErr = m.listener.Close()
		_ = m.blaze.Close()
		_ = m.http.Close()
		m.mu.Lock()
		for connection := range m.pendingConnections {
			_ = connection.Close()
		}
		m.pendingConnections = make(map[net.Conn]struct{})
		m.mu.Unlock()
	})
	if errors.Is(closeErr, net.ErrClosed) {
		return nil
	}
	if closeErr != nil {
		return fmt.Errorf("listenerClose: %w", closeErr)
	}
	return nil
}

func (m *tcpProtocolMultiplexer) route(connection net.Conn) {
	defer m.untrack(connection)
	err := connection.SetReadDeadline(time.Now().Add(protocolDetectTimeout))
	if err != nil {
		_ = connection.Close()
		return
	}
	reader := bufio.NewReader(connection)
	prefix, err := reader.Peek(8)
	if err != nil {
		_ = connection.Close()
		return
	}
	err = connection.SetReadDeadline(time.Time{})
	if err != nil {
		_ = connection.Close()
		return
	}

	target := m.blaze
	if isHTTPRequestPrefix(prefix) {
		target = m.http
	}
	buffered := &bufferedConnection{Conn: connection, reader: reader}
	select {
	case target.connections <- buffered:
	case <-target.done:
		_ = connection.Close()
	case <-m.done:
		_ = connection.Close()
	}
}

func (m *tcpProtocolMultiplexer) track(connection net.Conn) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	select {
	case <-m.done:
		return false
	default:
	}
	m.pendingConnections[connection] = struct{}{}
	return true
}

func (m *tcpProtocolMultiplexer) untrack(connection net.Conn) {
	m.mu.Lock()
	delete(m.pendingConnections, connection)
	m.mu.Unlock()
}

func isHTTPRequestPrefix(prefix []byte) bool {
	for _, method := range httpMethodPrefixes {
		if bytes.HasPrefix(prefix, method) {
			return true
		}
	}
	return false
}

type bufferedConnection struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConnection) Read(contents []byte) (int, error) {
	return c.reader.Read(contents)
}

type routedListener struct {
	address     net.Addr
	connections chan net.Conn
	done        chan struct{}
	once        sync.Once
}

func newRoutedListener(address net.Addr) *routedListener {
	return &routedListener{
		address:     address,
		connections: make(chan net.Conn),
		done:        make(chan struct{}),
	}
}

func (l *routedListener) Accept() (net.Conn, error) {
	select {
	case <-l.done:
		return nil, net.ErrClosed
	default:
	}
	select {
	case connection := <-l.connections:
		select {
		case <-l.done:
			_ = connection.Close()
			return nil, net.ErrClosed
		default:
		}
		return connection, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *routedListener) Close() error {
	l.once.Do(func() {
		close(l.done)
	})
	return nil
}

func (l *routedListener) Addr() net.Addr {
	return l.address
}
