//go:build windows && !bindings

package window

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	windowOwnerCommand  = 4
	extendedWindowStyle = ^uintptr(19)
	toolWindowStyle     = 0x00000080
	showWindowRestore   = 9
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindow                = user32.NewProc("GetWindow")
	procGetWindowLong            = user32.NewProc("GetWindowLongW")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procShowWindow               = user32.NewProc("ShowWindow")
)

// InstanceOptions controls single-instance discovery and replacement.
type InstanceOptions struct {
	MutexName string
}

// Acquire enforces a named single instance for the current executable.
func Acquire(options InstanceOptions) (func(), bool, error) {
	if strings.TrimSpace(options.MutexName) == "" {
		return nil, false, errors.New("empty mutex name")
	}
	mutexName, err := windows.UTF16PtrFromString(options.MutexName)
	if err != nil {
		return nil, false, fmt.Errorf("mutexName: %w", err)
	}
	mutex, mutexErr := windows.CreateMutex(nil, false, mutexName)
	if mutexErr != nil && !errors.Is(mutexErr, windows.ERROR_ALREADY_EXISTS) {
		return nil, false, fmt.Errorf("mutexCreate: %w", mutexErr)
	}
	release := func() {
		_ = windows.CloseHandle(mutex)
	}

	processIDs, err := peerProcessIDs()
	if err != nil {
		release()
		return nil, false, fmt.Errorf("peerList: %w", err)
	}
	if len(processIDs) == 0 && errors.Is(mutexErr, windows.ERROR_ALREADY_EXISTS) {
		for range 5 {
			time.Sleep(50 * time.Millisecond)
			processIDs, err = peerProcessIDs()
			if err != nil {
				release()
				return nil, false, fmt.Errorf("peerRetry: %w", err)
			}
			if len(processIDs) != 0 {
				break
			}
		}
	}
	if len(processIDs) == 0 {
		return release, false, nil
	}

	for _, processID := range processIDs {
		window := processWindow(processID, true)
		if window == 0 {
			continue
		}
		_, _, _ = procShowWindow.Call(window, showWindowRestore)
		_, _, _ = procSetForegroundWindow.Call(window)
		release()
		return func() {}, true, nil
	}

	release()
	return func() {}, true, nil
}

func peerProcessIDs() ([]uint32, error) {
	executablePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("executablePath: %w", err)
	}
	executableName := filepath.Base(executablePath)
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("snapshotCreate: %w", err)
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	err = windows.Process32First(snapshot, &entry)
	if err != nil {
		return nil, fmt.Errorf("processFirst: %w", err)
	}
	currentProcessID := uint32(os.Getpid())
	processIDs := make([]uint32, 0, 1)
	for {
		name := windows.UTF16ToString(entry.ExeFile[:])
		if entry.ProcessID != currentProcessID && strings.EqualFold(name, executableName) {
			processIDs = append(processIDs, entry.ProcessID)
		}
		err = windows.Process32Next(snapshot, &entry)
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("processNext: %w", err)
		}
	}
	return processIDs, nil
}

func processWindow(processID uint32, isTaskbarOnly bool) uintptr {
	var window uintptr
	callback := syscall.NewCallback(func(currentWindow uintptr, _ uintptr) uintptr {
		var windowProcessID uint32
		_, _, _ = procGetWindowThreadProcessID.Call(currentWindow, uintptr(unsafe.Pointer(&windowProcessID)))
		if windowProcessID != processID {
			return 1
		}
		if isTaskbarOnly && !isTaskbarWindow(currentWindow) {
			return 1
		}
		window = currentWindow
		return 0
	})
	_, _, _ = procEnumWindows.Call(callback, 0)
	return window
}

func isTaskbarWindow(window uintptr) bool {
	isVisible, reserved, visibilityErr := procIsWindowVisible.Call(window)
	_ = reserved
	if visibilityErr != syscall.Errno(0) {
		// IsWindowVisible does not expose an extended-error contract, so the
		// return flag remains authoritative even when the syscall slot is stale.
	}
	if isVisible == 0 {
		return false
	}
	owner, reserved, ownerErr := procGetWindow.Call(window, windowOwnerCommand)
	_ = reserved
	if ownerErr != syscall.Errno(0) {
		// GetWindow does not expose an extended-error contract; a zero owner is
		// the documented representation for an unowned top-level window.
	}
	if owner != 0 {
		return false
	}
	extendedStyle, reserved, styleErr := procGetWindowLong.Call(window, extendedWindowStyle)
	_ = reserved
	if styleErr != syscall.Errno(0) && extendedStyle == 0 {
		// GetWindowLong can legitimately return zero, and LazyProc.Call cannot
		// clear the thread's prior last-error state before the call.
	}
	return extendedStyle&toolWindowStyle == 0
}
