//go:build scenario && windows

package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type debugPauseBootstrap struct {
	PID        uint32
	Creation   uint64
	ParentPID  uint32
	DurationMS uint32
	PauseID    string
}
type debugPauseWorkerReceipt struct {
	Status    string
	ThreadID  uint32
	At        time.Time
	ExpiresAt time.Time
	TickMS    uint64
	Failure   string
}

var pauseKernel = windows.NewLazySystemDLL("kernel32.dll")
var pauseAttach = pauseKernel.NewProc("DebugActiveProcess")
var pauseWait = pauseKernel.NewProc("WaitForDebugEvent")
var pauseContinue = pauseKernel.NewProc("ContinueDebugEvent")
var pauseDetach = pauseKernel.NewProc("DebugActiveProcessStop")

func debugPauseCreation(handle windows.Handle) (uint64, error) {
	var creation, exit, kernel, user windows.Filetime
	err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user)
	if err != nil {
		return 0, fmt.Errorf("pauseCreation: %w", err)
	}
	return uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime), nil
}

func runDebugPauseWorker(arguments []string) (bool, error) {
	if len(arguments) != 1 || arguments[0] != "--scenario-debugger-worker" {
		return false, nil
	}
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 4096))
	var req debugPauseBootstrap
	err := decoder.Decode(&req)
	if err != nil {
		return true, fmt.Errorf("pauseBootstrap: %w", err)
	}
	if req.PID == 0 || req.PID == windows.GetCurrentProcessId() || req.ParentPID == req.PID || req.DurationMS == 0 || req.DurationMS > 5000 || req.Creation == 0 || req.PauseID == "" {
		return true, errors.New("invalid private debugger bootstrap")
	}
	err = debugPauseWorker(req, decoder)
	if err != nil {
		return true, fmt.Errorf("pauseWorker: %w", err)
	}
	return true, nil
}

func debugPauseWorker(req debugPauseBootstrap, decoder *json.Decoder) error {
	runtime.LockOSThread()
	// Never unlock an attached debugger thread on an error. Exiting this
	// helper keeps Windows' default kill-on-debugger-exit protection enabled.
	target, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, req.PID)
	if err != nil {
		runtime.UnlockOSThread()
		return fmt.Errorf("pauseTarget: %w", err)
	}
	// Retain the checked handle through detach: Windows must not recycle the
	// PID between the identity check and DebugActiveProcess.
	defer debugPauseCloseHandle(target)
	creation, err := debugPauseCreation(target)
	if err != nil || creation != req.Creation {
		runtime.UnlockOSThread()
		return fmt.Errorf("pauseIdentity: %w", errors.Join(err, errors.New("target creation identity must match")))
	}
	parent, err := windows.OpenProcess(windows.SYNCHRONIZE, false, req.ParentPID)
	if err != nil {
		runtime.UnlockOSThread()
		return fmt.Errorf("pauseParent: %w", err)
	}
	defer debugPauseCloseHandle(parent)
	commands := make(chan struct{}, 1)
	go readDebugPauseResume(decoder, commands)
	attached, callErr := pauseBool(pauseAttach, uintptr(req.PID))
	if !attached {
		runtime.UnlockOSThread()
		return fmt.Errorf("pauseAttach: %w", callErr)
	}
	deadline := time.Now().Add(time.Duration(req.DurationMS) * time.Millisecond)
	var heldProcess, heldThread uint32
	isProcessCreated := false
	encoder := json.NewEncoder(os.Stdout)
	for heldThread == 0 {
		if !time.Now().Before(deadline) {
			return failDebugPauseWorker("attach breakpoint did not arrive within lease")
		}
		var event [256]byte
		isEvent, waitErr := pauseBool(pauseWait, uintptr(unsafe.Pointer(&event[0])), 50)
		if !isEvent {
			if waitErr == windows.ERROR_SEM_TIMEOUT {
				continue
			}
			return failDebugPauseWorker("wait attach event: " + waitErr.Error())
		}
		kind := binary.LittleEndian.Uint32(event[0:4])
		pid := binary.LittleEndian.Uint32(event[4:8])
		thread := binary.LittleEndian.Uint32(event[8:12])
		offset := 12
		if unsafe.Sizeof(uintptr(0)) == 8 {
			offset = 16
		}
		if pid != req.PID {
			return failDebugPauseWorker("unexpected debuggee identity")
		}
		status := uintptr(0x00010002) // DBG_CONTINUE for debug notifications.
		if kind == 3 {
			// CREATE_PROCESS_DEBUG_INFO hProcess is retained by Windows;
			// hFile is debugger-owned. Check the actual attached identity.
			handle := *(*windows.Handle)(unsafe.Pointer(&event[offset+int(unsafe.Sizeof(uintptr(0)))]))
			actual, identityErr := debugPauseCreation(handle)
			if identityErr != nil || actual != req.Creation {
				return failDebugPauseWorker("attached process creation identity changed")
			}
			isProcessCreated = true
		}
		if kind == 3 || kind == 6 {
			handle := *(*windows.Handle)(unsafe.Pointer(&event[offset]))
			if handle != 0 {
				debugPauseCloseHandle(handle)
			}
		}
		if kind == 1 {
			exception := binary.LittleEndian.Uint32(event[offset : offset+4])
			exceptionRecordSize := 80
			if unsafe.Sizeof(uintptr(0)) == 8 {
				exceptionRecordSize = 152
			}
			firstChance := binary.LittleEndian.Uint32(event[offset+exceptionRecordSize : offset+exceptionRecordSize+4])
			if exception == 0x80000003 && isProcessCreated && firstChance == 1 {
				heldProcess, heldThread = pid, thread
				break
			}
			status = 0x80010001 // DBG_EXCEPTION_NOT_HANDLED: preserve real faults.
		}
		isContinued, continueErr := pauseBool(pauseContinue, uintptr(pid), uintptr(thread), status)
		if !isContinued {
			return failDebugPauseWorker("continue attach notification: " + continueErr.Error())
		}
		if kind == 5 {
			return failDebugPauseWorker("owned client exited during attach")
		}
	}
	err = encoder.Encode(debugPauseWorkerReceipt{Status: "paused", ThreadID: heldThread, At: time.Now().UTC(), ExpiresAt: deadline.UTC()})
	if err != nil {
		return failDebugPauseWorker("pause acknowledgement: " + err.Error())
	}
	for time.Now().Before(deadline) {
		status, parentErr := windows.WaitForSingleObject(parent, 0)
		if parentErr != nil || status == windows.WAIT_OBJECT_0 {
			break
		}
		select {
		case <-commands:
			deadline = time.Now()
		case <-time.After(10 * time.Millisecond):
		}
	}
	isContinued, continueErr := pauseBool(pauseContinue, uintptr(heldProcess), uintptr(heldThread), 0x00010002)
	if !isContinued {
		return failDebugPauseWorker("resume breakpoint: " + continueErr.Error())
	}
	isDetached, detachErr := pauseBool(pauseDetach, uintptr(req.PID))
	if !isDetached {
		return failDebugPauseWorker("detach debugger: " + detachErr.Error())
	}
	runtime.UnlockOSThread()
	err = encoder.Encode(debugPauseWorkerReceipt{Status: "resumed", At: time.Now().UTC(), TickMS: uint64(windows.DurationSinceBoot() / time.Millisecond)})
	if err != nil {
		return fmt.Errorf("resumeReceipt: %w", err)
	}
	return nil
}

func readDebugPauseResume(decoder *json.Decoder, commands chan<- struct{}) {
	var command string
	err := decoder.Decode(&command)
	if err != nil || command == "resume" {
		commands <- struct{}{}
		return
	}
	// Invalid private commands revoke the lease rather than prolonging it.
	commands <- struct{}{}
}
func pauseBool(proc *windows.LazyProc, args ...uintptr) (bool, error) {
	result, second, err := proc.Call(args...)
	if second != 0 { /* BOOL APIs have no second return contract. */
	}
	if result != 0 {
		return true, nil
	}
	if err == windows.ERROR_SUCCESS {
		err = errors.New("Windows debugger operation returned false")
	}
	return false, err
}
func debugPauseCloseHandle(handle windows.Handle) {
	err := windows.CloseHandle(handle)
	if err != nil {
		fmt.Fprintln(os.Stderr, "debugger handle close:", err)
	}
}
func failDebugPauseWorker(message string) error {
	fmt.Fprintln(os.Stderr, "debugger cleanup failed:", message)
	// Kill-on-debugger-exit is intentionally left at its default TRUE. The
	// supervisor never reports this fallback as a successful resume.
	os.Exit(2)
	return errors.New(message)
}
