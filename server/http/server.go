package recaphttp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	requestReadTimeout    = 30 * time.Second
	responseWriteTimeout  = 30 * time.Second
	connectionIdleTimeout = 60 * time.Second
	maximumHeaderBytes    = 1 << 20
	maximumRequestBytes   = 1 << 20
	requestShutdownWait   = 5 * time.Second
)

// Server owns one HTTP listener and can share a Router with other ports.
type Server struct {
	server        *http.Server
	mu            sync.Mutex
	isClosed      bool
	activeRequest int
	requestIdle   chan struct{}
}

// NewServer creates an HTTP server with bounded request, response, and idle I/O.
func NewServer(host string, port uint16, handler http.Handler) *Server {
	if handler == nil {
		handler = http.DefaultServeMux
	}
	server := &Server{}
	server.server = &http.Server{
		Addr:              net.JoinHostPort(host, fmt.Sprintf("%d", port)),
		Handler:           server.trackRequests(limitRequestBody(handler)),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       requestReadTimeout,
		WriteTimeout:      responseWriteTimeout,
		IdleTimeout:       connectionIdleTimeout,
		MaxHeaderBytes:    maximumHeaderBytes,
	}
	return server
}

func (s *Server) trackRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !s.beginRequest() {
			http.Error(response, "server shutting down", http.StatusServiceUnavailable)
			return
		}
		defer s.endRequest()
		next.ServeHTTP(response, request)
	})
}

func (s *Server) beginRequest() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isClosed {
		return false
	}
	if s.activeRequest == 0 {
		s.requestIdle = make(chan struct{})
	}
	s.activeRequest++
	return true
}

func (s *Server) endRequest() {
	s.mu.Lock()
	if s.activeRequest > 0 {
		s.activeRequest--
	}
	if s.activeRequest == 0 && s.requestIdle != nil {
		close(s.requestIdle)
		s.requestIdle = nil
	}
	s.mu.Unlock()
}

func limitRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Body != nil {
			request.Body = http.MaxBytesReader(
				response, request.Body, maximumRequestBytes,
			)
		}
		next.ServeHTTP(response, request)
	})
}

// ListenAndServe runs until context cancellation or a listener failure.
func (s *Server) ListenAndServe(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.server.Addr)
	if err != nil {
		return fmt.Errorf("httpListen[%s]: %w", s.server.Addr, err)
	}
	err = s.Serve(ctx, listener)
	if err != nil {
		return fmt.Errorf("httpServe: %w", err)
	}
	return nil
}

// Serve handles HTTP requests from an existing listener. The listener is
// closed when the context ends or the server stops.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	if listener == nil {
		return errors.New("nil listener")
	}
	go func() {
		<-ctx.Done()
		_ = s.Close()
	}()

	err := s.server.Serve(listener)
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("serve: %w", err)
}

// Close immediately closes the HTTP listener and active connections.
func (s *Server) Close() error {
	s.mu.Lock()
	s.isClosed = true
	activeRequest := s.activeRequest
	requestIdle := s.requestIdle
	s.mu.Unlock()
	err := s.server.Close()
	if activeRequest != 0 && requestIdle != nil {
		timer := time.NewTimer(requestShutdownWait)
		select {
		case <-requestIdle:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
			log.Printf(
				"HTTP shutdown continuing with %d active requests after %s",
				activeRequest, requestShutdownWait,
			)
		}
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("httpClose: %w", err)
}
