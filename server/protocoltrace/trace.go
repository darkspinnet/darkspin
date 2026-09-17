// Package protocoltrace records redacted cross-protocol diagnostics.
package protocoltrace

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Event is one protocol observation. Payload contents and credential values are
// intentionally excluded; Length and Digest can correlate client/server events.
type Event struct {
	Time        string   `json:"time"`
	Sequence    uint64   `json:"sequence"`
	Protocol    string   `json:"protocol"`
	Direction   string   `json:"direction,omitempty"`
	Kind        string   `json:"kind"`
	Remote      string   `json:"remote,omitempty"`
	Session     uint32   `json:"session,omitempty"`
	Path        string   `json:"path,omitempty"`
	Method      string   `json:"method,omitempty"`
	Keys        []string `json:"keys,omitempty"`
	Fields      []string `json:"fields,omitempty"`
	Status      int      `json:"status,omitempty"`
	Component   uint16   `json:"component,omitempty"`
	Command     uint16   `json:"command,omitempty"`
	MessageID   uint32   `json:"message_id,omitempty"`
	MessageType uint8    `json:"message_type,omitempty"`
	Version     uint32   `json:"version,omitempty"`
	RequestID   uint32   `json:"request_id,omitempty"`
	ErrorCode   uint16   `json:"error_code,omitempty"`
	Length      int      `json:"length,omitempty"`
	Digest      string   `json:"digest,omitempty"`
	DurationUS  int64    `json:"duration_us,omitempty"`
	Error       string   `json:"error,omitempty"`
}

// Recorder consumes protocol events without affecting protocol behavior.
type Recorder interface {
	Record(Event)
}

// JSONRecorder writes one event per line and serializes concurrent writers.
type JSONRecorder struct {
	mu       sync.Mutex
	encoder  *json.Encoder
	closer   io.Closer
	sequence uint64
	writeErr error
}

// Open creates a new JSONL trace, including its parent directory when needed.
func Open(path string) (*JSONRecorder, error) {
	parent := filepath.Dir(path)
	err := os.MkdirAll(parent, 0o755)
	if err != nil {
		return nil, fmt.Errorf("traceMkdir: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("traceOpen: %w", err)
	}
	return New(file, file), nil
}

// New creates a recorder around a writer and optional closer.
func New(writer io.Writer, closer io.Closer) *JSONRecorder {
	return &JSONRecorder{encoder: json.NewEncoder(writer), closer: closer}
}

// Record appends an event. A trace write failure is retained for Close so it
// cannot interrupt a live client protocol exchange.
func (r *JSONRecorder) Record(event Event) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.writeErr != nil {
		return
	}
	r.sequence++
	event.Sequence = r.sequence
	if event.Time == "" {
		event.Time = time.Now().UTC().Format(time.RFC3339Nano)
	}
	err := r.encoder.Encode(event)
	if err != nil {
		r.writeErr = err
	}
}

// Close flushes ownership to the underlying closer and reports deferred writes.
func (r *JSONRecorder) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	writeErr := r.writeErr
	var closeErr error
	if r.closer != nil {
		closeErr = r.closer.Close()
		r.closer = nil
	}
	if writeErr != nil {
		return fmt.Errorf("traceWrite: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("traceClose: %w", closeErr)
	}
	return nil
}
