//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procVirtualAllocEx     = kernel32.NewProc("VirtualAllocEx")
	procVirtualFreeEx      = kernel32.NewProc("VirtualFreeEx")
	procCreateRemoteThread = kernel32.NewProc("CreateRemoteThread")
	procGetExitCodeThread  = kernel32.NewProc("GetExitCodeThread")
	procLoadLibraryW       = kernel32.NewProc("LoadLibraryW")
	procWaitForDebugEvent  = kernel32.NewProc("WaitForDebugEvent")
	procContinueDebugEvent = kernel32.NewProc("ContinueDebugEvent")
	procDebugProcessStop   = kernel32.NewProc("DebugActiveProcessStop")
)

const debugContinue = 0x00010002

type debugEventLoop struct {
	processID uint32
	ready     chan error
	done      chan struct{}
	stop      chan struct{}
}

func newDebugEventLoop(processID uint32) *debugEventLoop {
	return &debugEventLoop{
		processID: processID, ready: make(chan error, 1),
		done: make(chan struct{}), stop: make(chan struct{}),
	}
}

func (e *debugEventLoop) run() {
	defer close(e.done)
	isReady := false
	for {
		select {
		case <-e.stop:
			if !isReady {
				e.ready <- errors.New("debug event loop stopped before the first event")
			}
			return
		default:
		}
		var event [256]byte
		result, _, callErr := procWaitForDebugEvent.Call(
			uintptr(unsafe.Pointer(&event[0])), 100,
		)
		if result == 0 {
			if errors.Is(callErr, windows.ERROR_SEM_TIMEOUT) {
				continue
			}
			if !isReady {
				e.ready <- fmt.Errorf("eventWait: %w", callErr)
			}
			return
		}
		eventProcessID := *(*uint32)(unsafe.Pointer(&event[4]))
		eventThreadID := *(*uint32)(unsafe.Pointer(&event[8]))
		result, _, callErr = procContinueDebugEvent.Call(
			uintptr(eventProcessID), uintptr(eventThreadID), debugContinue,
		)
		if result == 0 {
			if !isReady {
				e.ready <- fmt.Errorf("eventContinue: %w", callErr)
			}
			return
		}
		if !isReady {
			isReady = true
			e.ready <- nil
		}
	}
}

func (e *debugEventLoop) detach() error {
	result, _, callErr := procDebugProcessStop.Call(uintptr(e.processID))
	close(e.stop)
	<-e.done
	if result == 0 {
		return fmt.Errorf("debugDetach: %w", callErr)
	}
	return nil
}

func launchInjected(
	ctx context.Context, gamePath, gameWorkingDirectory, fangPath string,
	gameArguments []string, serverAddress string,
) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("launchContext: %w", err)
	}
	commandLine := windows.ComposeCommandLine(append([]string{gamePath}, gameArguments...))
	commandLinePointer, err := windows.UTF16PtrFromString(commandLine)
	if err != nil {
		return fmt.Errorf("commandLine: %w", err)
	}
	workingDirectory, err := windows.UTF16PtrFromString(gameWorkingDirectory)
	if err != nil {
		return fmt.Errorf("workingDir: %w", err)
	}
	startup := windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfo{}))}
	process := windows.ProcessInformation{}
	environmentBlock := managedGameEnvironment(serverAddress)
	creationFlags := uint32(windows.CREATE_DEFAULT_ERROR_MODE | windows.CREATE_SUSPENDED | windows.CREATE_UNICODE_ENVIRONMENT)
	isWineDebugLaunch := os.Getenv("DARKSPIN_WINE_DEBUG_LAUNCH") == "1"
	if isWineDebugLaunch {
		creationFlags |= windows.DEBUG_ONLY_THIS_PROCESS
	}
	err = windows.CreateProcess(nil, commandLinePointer, nil, nil, false, creationFlags, &environmentBlock[0], workingDirectory, &startup, &process)
	if err != nil {
		return fmt.Errorf("processCreate: %w", err)
	}
	defer windows.CloseHandle(process.Process)
	defer windows.CloseHandle(process.Thread)
	err = writeClientProcessRegistration(process.ProcessId)
	if err != nil {
		_ = windows.TerminateProcess(process.Process, 1)
		return fmt.Errorf("processRegister: %w", err)
	}
	stopDebug := func() error { return nil }
	isDebugActive := false
	if isWineDebugLaunch {
		stopDebug, err = continueDebugEvents(process.ProcessId)
		if err != nil {
			_ = windows.TerminateProcess(process.Process, 1)
			return fmt.Errorf("debugStart: %w", err)
		}
		isDebugActive = true
	}
	defer func() {
		if isDebugActive {
			_ = stopDebug()
		}
	}()
	cancellationDone := make(chan struct{})
	stopCancellation := context.AfterFunc(ctx, func() {
		defer close(cancellationDone)
		_ = windows.TerminateProcess(process.Process, 1)
	})
	defer func() {
		if !stopCancellation() {
			<-cancellationDone
		}
	}()
	err = os.Unsetenv(launchJWTEnvironment)
	if err != nil {
		_ = windows.TerminateProcess(process.Process, 1)
		return fmt.Errorf("jwtParentClear: %w", err)
	}
	moduleHandle, err := injectDLL(process.Process, fangPath)
	if err != nil {
		_ = windows.TerminateProcess(process.Process, 1)
		return fmt.Errorf("fangInject: %w", err)
	}
	err = callRemoteInitializer(process.Process, moduleHandle, fangPath)
	if err != nil {
		_ = windows.TerminateProcess(process.Process, 1)
		return fmt.Errorf("fangInit: %w", err)
	}
	if isDebugActive {
		err = stopDebug()
		if err != nil {
			_ = windows.TerminateProcess(process.Process, 1)
			return fmt.Errorf("debugStop: %w", err)
		}
		isDebugActive = false
	}
	_, err = windows.ResumeThread(process.Thread)
	if err != nil {
		_ = windows.TerminateProcess(process.Process, 1)
		return fmt.Errorf("gameResume: %w", err)
	}
	_, err = windows.WaitForSingleObject(process.Process, windows.INFINITE)
	if err != nil {
		return fmt.Errorf("gameWait: %w", err)
	}
	err = ctx.Err()
	if err != nil {
		return fmt.Errorf("gameCancel: %w", err)
	}
	var exitCode uint32
	err = windows.GetExitCodeProcess(process.Process, &exitCode)
	if err != nil {
		return fmt.Errorf("exitCode: %w", err)
	}
	if exitCode != 0 {
		return gameExitError(exitCode)
	}
	return nil
}

func managedGameEnvironment(serverAddress string) []uint16 {
	managedPrefix := managedLaunchEnvironment + "="
	serverPrefix := serverAddressEnvironment + "="
	environmentEntries := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		upperEntry := strings.ToUpper(entry)
		if strings.HasPrefix(upperEntry, strings.ToUpper(managedPrefix)) ||
			strings.HasPrefix(upperEntry, strings.ToUpper(serverPrefix)) {
			continue
		}
		environmentEntries = append(environmentEntries, entry)
	}
	environmentEntries = append(environmentEntries, managedPrefix+"1")
	if serverAddress != "" {
		environmentEntries = append(environmentEntries, serverPrefix+serverAddress)
	}
	sort.Slice(environmentEntries, func(left, right int) bool {
		return strings.ToUpper(environmentEntries[left]) < strings.ToUpper(environmentEntries[right])
	})
	contents := strings.Join(environmentEntries, "\x00") + "\x00\x00"
	return utf16.Encode([]rune(contents))
}

func continueDebugEvents(processID uint32) (func() error, error) {
	eventLoop := newDebugEventLoop(processID)
	go eventLoop.run()
	err := <-eventLoop.ready
	if err != nil {
		close(eventLoop.stop)
		<-eventLoop.done
		return nil, fmt.Errorf("eventLoopReady: %w", err)
	}
	return eventLoop.detach, nil
}

func injectDLL(process windows.Handle, dllPath string) (uint32, error) {
	loaderAddress, err := gameLoaderAddress()
	if err != nil {
		return 0, fmt.Errorf("gameLoader: %w", err)
	}
	encodedPath, err := windows.UTF16FromString(dllPath)
	if err != nil {
		return 0, fmt.Errorf("dllPath: %w", err)
	}
	size := uintptr(len(encodedPath) * 2)
	remoteAddress, _, callErr := procVirtualAllocEx.Call(uintptr(process), 0, size, windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	if remoteAddress == 0 {
		return 0, fmt.Errorf("remoteAlloc: %w", callErr)
	}
	defer procVirtualFreeEx.Call(uintptr(process), remoteAddress, 0, windows.MEM_RELEASE)
	var written uintptr
	err = windows.WriteProcessMemory(process, remoteAddress, (*byte)(unsafe.Pointer(&encodedPath[0])), size, &written)
	if err != nil {
		return 0, fmt.Errorf("remoteWrite: %w", err)
	}
	if written != size {
		return 0, errors.New("hook path was only partially written into game")
	}
	var threadID uint32
	thread, _, callErr := procCreateRemoteThread.Call(uintptr(process), 0, 0, loaderAddress, remoteAddress, 0, uintptr(unsafe.Pointer(&threadID)))
	if thread == 0 {
		return 0, fmt.Errorf("loaderStart: %w", callErr)
	}
	threadHandle := windows.Handle(thread)
	defer windows.CloseHandle(threadHandle)
	_, err = windows.WaitForSingleObject(threadHandle, windows.INFINITE)
	if err != nil {
		return 0, fmt.Errorf("loaderWait: %w", err)
	}
	var moduleHandle uint32
	result, _, callErr := procGetExitCodeThread.Call(thread, uintptr(unsafe.Pointer(&moduleHandle)))
	if result == 0 {
		return 0, fmt.Errorf("loaderResult: %w", callErr)
	}
	if moduleHandle == 0 {
		return 0, errors.New("fang.dll failed to load in the game process")
	}
	return moduleHandle, nil
}

func callRemoteInitializer(process windows.Handle, moduleHandle uint32, dllPath string) error {
	functionRVA, err := exportedFunctionRVA(dllPath, "RecapInitializeThread")
	if err != nil {
		functionRVA, err = exportedFunctionRVA(dllPath, "RecapInitializeThread@4")
	}
	if err != nil {
		return fmt.Errorf("initializerRVA: %w", err)
	}
	initializerAddress := uintptr(moduleHandle) + uintptr(functionRVA)
	var threadID uint32
	thread, _, callErr := procCreateRemoteThread.Call(uintptr(process), 0, 0, initializerAddress, 0, 0, uintptr(unsafe.Pointer(&threadID)))
	if thread == 0 {
		return fmt.Errorf("initializerStart: %w", callErr)
	}
	threadHandle := windows.Handle(thread)
	defer windows.CloseHandle(threadHandle)
	_, err = windows.WaitForSingleObject(threadHandle, windows.INFINITE)
	if err != nil {
		return fmt.Errorf("initializerWait: %w", err)
	}
	var exitCode uint32
	result, _, callErr := procGetExitCodeThread.Call(thread, uintptr(unsafe.Pointer(&exitCode)))
	if result == 0 {
		return fmt.Errorf("initializerResult: %w", callErr)
	}
	if exitCode != 0 {
		return fmt.Errorf("fang.dll hook initialization failed with code 0x%x", exitCode)
	}
	return nil
}
