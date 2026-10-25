//go:build scenario

package main

import (
	"context"
	"sync"
)

// Script cancellation is independent of the owned client/server lifetime.
// Remember a stop received before the queued Lua worker has started.
type debugScriptControl struct {
	mutex           sync.Mutex
	cancel          context.CancelFunc
	isStopRequested bool
}

func (e *debugScriptControl) reset() {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.isStopRequested = false
}

func (e *debugScriptControl) begin(parent context.Context) context.Context {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	ctx, cancel := context.WithCancel(parent)
	e.cancel = cancel
	if e.isStopRequested {
		cancel()
	}
	return ctx
}

func (e *debugScriptControl) finish() {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
}

func (e *debugScriptControl) stop() {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.isStopRequested = true
	if e.cancel != nil {
		e.cancel()
	}
}
