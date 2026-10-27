package maintenance

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const descriptorFilename = "maintenance.json"

type descriptor struct {
	Address string `json:"address"`
	Token   string `json:"token"`
}

type command struct {
	Minutes int    `json:"minutes"`
	ID      string `json:"id,omitempty"`
}

// Reply is the local control channel's allowlisted response representation.
type Reply struct {
	ID        string    `json:"id"`
	Deadline  time.Time `json:"deadline"`
	Delivered int       `json:"delivered"`
	IsActive  bool      `json:"is_active"`
	Error     string    `json:"error,omitempty"`
}

// Control owns a loopback-only authenticated maintenance listener.
type Control struct {
	announcement   announcement
	server         *http.Server
	descriptor     descriptor
	descriptorPath string
	done           chan error
	closeOnce      sync.Once
	closeErr       error
}

// Start starts a private control listener. The runtime must already be leased
// exclusively by the caller until after Control.Close completes.
func Start(runtimePath string, broadcast Broadcast) (*Control, error) {
	if runtimePath == "" || broadcast == nil {
		return nil, errors.New("maintenance runtime and broadcast are required")
	}
	err := os.MkdirAll(runtimePath, 0o755)
	if err != nil {
		return nil, fmt.Errorf("runtimeCreate: %w", err)
	}
	token, err := randomToken()
	if err != nil {
		return nil, fmt.Errorf("controlToken: %w", err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("controlListen: %w", err)
	}
	e := &Control{
		announcement:   announcement{broadcast: broadcast},
		descriptor:     descriptor{Address: listener.Addr().String(), Token: token},
		descriptorPath: filepath.Join(runtimePath, descriptorFilename),
		done:           make(chan error, 1),
	}
	e.server = &http.Server{
		Handler: e, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 2 * time.Minute, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 4096,
	}
	err = writeDescriptor(e.descriptorPath, e.descriptor)
	if err != nil {
		closeErr := listener.Close()
		return nil, fmt.Errorf("descriptorSave: %w", errors.Join(err, closeErr))
	}
	go e.serve(listener)
	return e, nil
}

func (e *Control) serve(listener net.Listener) {
	err := e.server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	e.done <- err
}

// Close stops the listener and removes its private discovery descriptor.
func (e *Control) Close() error {
	if e == nil {
		return nil
	}
	e.closeOnce.Do(e.close)
	return e.closeErr
}

func (e *Control) close() {
	err := e.server.Close()
	e.announcement.operationMu.Lock()
	e.announcement.operationMu.Unlock()
	serveErr := <-e.done
	removeErr := os.Remove(e.descriptorPath)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	if err != nil || serveErr != nil || removeErr != nil {
		e.closeErr = fmt.Errorf("controlClose: %w", errors.Join(err, serveErr, removeErr))
	}
}

func (e *Control) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "application/json")
	remoteHost, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil || remoteHost != "127.0.0.1" || request.Host != e.descriptor.Address ||
		request.Header.Get("Origin") != "" ||
		subtle.ConstantTimeCompare([]byte(request.Header.Get("Authorization")), []byte("Bearer "+e.descriptor.Token)) != 1 {
		http.Error(writer, "unauthorized local maintenance request", http.StatusUnauthorized)
		return
	}
	if request.Method != http.MethodPost {
		if request.Method == http.MethodGet && request.URL.Path == "/status" {
			state := e.announcement.status()
			encodeErr := json.NewEncoder(writer).Encode(Reply{ID: state.ID, Deadline: state.Deadline, Delivered: state.Delivered, IsActive: state.IsActive})
			if encodeErr != nil {
				// The local status client disconnected.
				return
			}
			return
		}
		http.Error(writer, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var req command
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1024))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&req)
	if err != nil {
		http.Error(writer, "invalid maintenance request", http.StatusBadRequest)
		return
	}
	var trailing any
	err = decoder.Decode(&trailing)
	if !errors.Is(err, io.EOF) {
		http.Error(writer, "unexpected request data", http.StatusBadRequest)
		return
	}
	var state countdown
	switch request.URL.Path {
	case "/announce":
		state, err = e.announcement.announce(request.Context(), req.Minutes)
	case "/cancel":
		state, err = e.announcement.cancel(request.Context(), req.ID)
	default:
		http.Error(writer, "unknown maintenance command", http.StatusNotFound)
		return
	}
	reply := Reply{ID: state.ID, Deadline: state.Deadline, Delivered: state.Delivered, IsActive: state.IsActive}
	if err != nil {
		reply.Error = err.Error()
		writer.WriteHeader(http.StatusServiceUnavailable)
	}
	encodeErr := json.NewEncoder(writer).Encode(reply)
	if encodeErr != nil {
		// The caller disconnected; notification attempts have already completed.
		return
	}
}

func writeDescriptor(path string, req descriptor) error {
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("descriptorEncode: %w", err)
	}
	r, err := os.CreateTemp(filepath.Dir(path), ".maintenance-*.tmp")
	if err != nil {
		return fmt.Errorf("temporaryCreate: %w", err)
	}
	temporaryPath := r.Name()
	defer func() {
		removeErr := os.Remove(temporaryPath)
		if removeErr != nil {
			// A successful rename removes this name; failed cleanup is harmless
			// because CreateTemp restricts this private descriptor to its owner.
			return
		}
	}()
	_, err = r.Write(payload)
	closeErr := r.Close()
	if err != nil || closeErr != nil {
		return fmt.Errorf("descriptorWrite: %w", errors.Join(err, closeErr))
	}
	err = os.Rename(temporaryPath, path)
	if err != nil {
		return fmt.Errorf("descriptorInstall: %w", err)
	}
	return nil
}
