// Package admission serializes gameplay entry with maintenance state changes.
package admission

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

var ErrClosed = errors.New("gameplay admission is closed")

type State struct {
	IsBlocked bool
	Reason    string
	Deadline  time.Time
}

type StateReader interface {
	CurrentAdmission() State
}

type ClosedError struct {
	State State
}

func (e *ClosedError) Error() string { return ErrClosed.Error() }
func (e *ClosedError) Unwrap() error { return ErrClosed }

type Gate struct {
	mutex              sync.RWMutex
	source             StateReader
	waitingWriterCount atomic.Int64
}

func New(source StateReader) *Gate { return &Gate{source: source} }

type Lease struct {
	gate     *Gate
	parent   *Lease
	isWriter bool
	isClosed atomic.Bool
}

type contextKey struct{ gate *Gate }

// Context carries an owned admission lease through nested feature calls, so a
// nested read cannot wait behind a state writer while its parent owns a read.
func (e *Lease) Context(ctx context.Context) context.Context {
	if e == nil || e.gate == nil {
		return ctx
	}
	lease := e
	if e.parent != nil {
		lease = e.parent
	}
	return context.WithValue(ctx, contextKey{gate: e.gate}, lease)
}

func (e *Lease) Close() error {
	if e == nil || e.gate == nil || e.parent != nil || e.isClosed.Swap(true) {
		return nil
	}
	if e.isWriter {
		e.gate.mutex.Unlock()
	} else {
		e.gate.mutex.RUnlock()
	}
	return nil
}

func (e *Gate) Status() State {
	if e == nil || e.source == nil {
		return State{}
	}
	return e.source.CurrentAdmission()
}

func (e *Gate) Enter(ctx context.Context) (*Lease, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if e == nil {
		return &Lease{}, nil
	}
	parent, isLease := ctx.Value(contextKey{gate: e}).(*Lease)
	if isLease && !parent.isClosed.Load() && parent.isWriter {
		return nil, errors.New("cannot enter admission while owning a change lease")
	}
	if isLease && !parent.isClosed.Load() && !parent.isWriter {
		return &Lease{gate: e, parent: parent}, nil
	}
	err := e.lock(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("entryLock: %w", err)
	}
	state := e.Status()
	if state.IsBlocked {
		e.mutex.RUnlock()
		return nil, &ClosedError{State: state}
	}
	return &Lease{gate: e}, nil
}

// Change exclusively owns the gate while the caller mutates maintenance state.
// It deliberately does not mutate or persist that state itself.
func (e *Gate) Change(ctx context.Context) (*Lease, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if e == nil {
		return &Lease{}, nil
	}
	parent, isLease := ctx.Value(contextKey{gate: e}).(*Lease)
	if isLease && !parent.isClosed.Load() {
		return nil, errors.New("cannot change admission while owning an entry lease")
	}
	e.waitingWriterCount.Add(1)
	defer e.waitingWriterCount.Add(-1)
	err := e.lock(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("changeLock: %w", err)
	}
	return &Lease{gate: e, isWriter: true}, nil
}

func (e *Gate) lock(ctx context.Context, isWriter bool) error {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := ctx.Err()
		if err != nil {
			return fmt.Errorf("gateContext: %w", err)
		}
		isLocked := false
		if isWriter {
			isLocked = e.mutex.TryLock()
		} else if e.waitingWriterCount.Load() == 0 {
			isLocked = e.mutex.TryRLock()
		}
		if isLocked {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("gateWait: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
