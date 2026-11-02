// Package instance leases a private runtime to one server process.
package instance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ErrRunning means another server currently owns the runtime lease.
var ErrRunning = errors.New("server is already running for this runtime")

// Lease owns the operating-system lock until Close or process exit.
type Lease struct {
	mu   sync.Mutex
	lock *os.File
}

// Acquire takes a nonblocking exclusive lease for runtimePath. The persistent
// lock file is never removed; its presence alone does not indicate ownership.
func Acquire(runtimePath string) (*Lease, error) {
	if strings.TrimSpace(runtimePath) == "" {
		return nil, errors.New("empty runtime path")
	}
	err := os.MkdirAll(runtimePath, 0o755)
	if err != nil {
		return nil, fmt.Errorf("runtimeCreate: %w", err)
	}
	lock, err := os.OpenFile(filepath.Join(runtimePath, "server.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("lockOpen: %w", err)
	}
	err = acquireLock(lock)
	if err != nil {
		closeErr := lock.Close()
		if closeErr != nil {
			return nil, fmt.Errorf("lockAcquire: %w", errors.Join(err, fmt.Errorf("lockClose: %w", closeErr)))
		}
		return nil, fmt.Errorf("lockAcquire: %w", err)
	}
	return &Lease{lock: lock}, nil
}

// Close releases the lease. It is safe to call concurrently or more than once.
func (e *Lease) Close() error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.lock == nil {
		return nil
	}
	err := e.lock.Close()
	e.lock = nil
	if err != nil {
		return fmt.Errorf("lockClose: %w", err)
	}
	return nil
}
