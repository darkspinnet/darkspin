//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

type injectedProcess struct {
	mutex     sync.Mutex
	process   windows.Handle
	thread    windows.Handle
	processID uint32
	stopDebug func() error
	isResumed bool
}

type injectedCancellation struct {
	process      *injectedProcess
	done         chan struct{}
	stopCallback func() bool
	stopOnce     sync.Once
}

func (e *injectedProcess) watch(ctx context.Context) *injectedCancellation {
	cancellation := &injectedCancellation{process: e, done: make(chan struct{})}
	cancellation.stopCallback = context.AfterFunc(ctx, cancellation.run)
	return cancellation
}

func (e *injectedCancellation) run() {
	defer close(e.done)
	err := e.process.terminate()
	if err != nil {
		writeInjectedCleanup("cancellation", err)
	}
}

func (e *injectedCancellation) stop() {
	e.stopOnce.Do(e.stopAndWait)
}

func (e *injectedCancellation) stopAndWait() {
	if !e.stopCallback() {
		<-e.done
	}
}

func (e *injectedProcess) terminate() error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.process == 0 {
		return nil
	}
	status, err := windows.WaitForSingleObject(e.process, 0)
	if err != nil {
		return fmt.Errorf("processStatus: %w", err)
	}
	if status == windows.WAIT_OBJECT_0 {
		return nil
	}
	err = windows.TerminateProcess(e.process, 1)
	if err != nil {
		return fmt.Errorf("processTerminate: %w", err)
	}
	return nil
}

func (e *injectedProcess) closeHandles() {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.stopDebug != nil {
		err := e.stopDebug()
		if err != nil {
			writeInjectedCleanup("debug", err)
		}
		e.stopDebug = nil
	}
	for _, handle := range []windows.Handle{e.thread, e.process} {
		if handle == 0 {
			continue
		}
		err := windows.CloseHandle(handle)
		if err != nil {
			writeInjectedCleanup("handle", err)
		}
	}
	e.thread = 0
	e.process = 0
}

func (e *injectedProcess) closeOnFailure(err *error) {
	if *err == nil {
		return
	}
	terminateErr := e.terminate()
	if terminateErr != nil {
		*err = errors.Join(*err, fmt.Errorf("failedTerminate: %w", terminateErr))
	}
	e.closeHandles()
}

func (e *injectedProcess) resume(ctx context.Context, observer injectedLaunchObserver) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("resumeContext: %w", err)
	}
	e.mutex.Lock()
	if e.isResumed || e.process == 0 {
		e.mutex.Unlock()
		return errors.New("owned client is closed or already resumed")
	}
	previousCount, err := windows.ResumeThread(e.thread)
	if err != nil {
		e.mutex.Unlock()
		return fmt.Errorf("gameResume: %w", err)
	}
	e.isResumed = true
	e.mutex.Unlock()
	if observer != nil && previousCount != 1 {
		return fmt.Errorf("unexpected primary thread suspend count %d", previousCount)
	}
	if observer != nil {
		err = observer.AfterResume(ctx, e.processID)
		if err != nil {
			return fmt.Errorf("resumeObserve: %w", err)
		}
	}
	return nil
}

type injectedRemoteAllocation struct {
	process         windows.Handle
	address         uintptr
	isThreadRunning bool
}

func (e *injectedRemoteAllocation) release() {
	if e.address == 0 || e.isThreadRunning {
		// A canceled/failed remote thread may still be using its argument. The
		// owned process cleanup, rather than an unsafe free, reclaims it.
		return
	}
	result, status, err := procVirtualFreeEx.Call(uintptr(e.process), e.address, 0, windows.MEM_RELEASE)
	if result == 0 {
		writeInjectedCleanup("remote allocation", fmt.Errorf("remoteFree[%d]: %w", status, err))
	}
}

// Polling leaves every remote initialization wait bounded by its wall context.
func waitInjectedHandle(ctx context.Context, process, handle windows.Handle) error {
	for {
		err := ctx.Err()
		if err != nil {
			terminateErr := windows.TerminateProcess(process, 1)
			if terminateErr != nil {
				return errors.Join(fmt.Errorf("waitContext: %w", err), fmt.Errorf("waitTerminate: %w", terminateErr))
			}
			return fmt.Errorf("waitContext: %w", err)
		}
		waitMS := uint32(50)
		deadline, isDeadlinePresent := ctx.Deadline()
		if isDeadlinePresent {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				continue
			}
			if remaining < 50*time.Millisecond {
				waitMS = uint32(remaining/time.Millisecond) + 1
			}
		}
		status, err := windows.WaitForSingleObject(handle, waitMS)
		if err != nil {
			return fmt.Errorf("handleWait: %w", err)
		}
		if status == windows.WAIT_OBJECT_0 {
			return nil
		}
		if status != uint32(windows.WAIT_TIMEOUT) {
			return fmt.Errorf("unexpected wait status 0x%x", status)
		}
	}
}

func closeInjectedThread(handle windows.Handle) {
	err := windows.CloseHandle(handle)
	if err != nil {
		writeInjectedCleanup("remote thread", err)
	}
}

func writeInjectedCleanup(operation string, err error) {
	written, writeErr := os.Stderr.WriteString(fmt.Sprintf("owned client %s cleanup: %v\n", operation, err))
	if writeErr != nil || written == 0 {
		// Cleanup is already proceeding; stderr failure has no recoverable sink.
		return
	}
}
