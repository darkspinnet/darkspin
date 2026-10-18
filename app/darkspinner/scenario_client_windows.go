//go:build windows && scenario

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"unsafe"

	"github.com/darkspinnet/darkspin/server/scenario"
	"golang.org/x/sys/windows"
)

type scenarioClientRequest struct {
	RunID            string
	GamePath         string
	WorkingDirectory string
	FangPath         string
	Arguments        []string
	ServerAddress    string
}

type scenarioClient struct {
	runID        string
	mutex        sync.Mutex
	process      *injectedProcess
	cancellation *injectedCancellation
	capability   scenario.Capability
	processID    uint32
}

func startScenarioClient(lifetimeContext, startupContext context.Context, req scenarioClientRequest) (*scenarioClient, scenario.Capability, error) {
	err := lifetimeContext.Err()
	if err != nil {
		return nil, scenario.Capability{}, fmt.Errorf("clientLifetime: %w", err)
	}
	// Lifetime cancellation also bounds setup, while startup cancellation is
	// detached before ownership is returned to the worker.
	setupContext, cancelSetup := context.WithCancel(startupContext)
	defer cancelSetup()
	stopLifetime := context.AfterFunc(lifetimeContext, cancelSetup)
	defer stopLifetime()
	client := &scenarioClient{runID: req.RunID}
	process, err := startInjectedProcess(setupContext, req.GamePath, req.WorkingDirectory, req.FangPath, req.Arguments, req.ServerAddress, client)
	if err != nil {
		return nil, scenario.Capability{}, fmt.Errorf("clientSuspend: %w", err)
	}
	client.process = process
	client.cancellation = process.watch(lifetimeContext)
	return client, client.capability, nil
}

func (e *scenarioClient) BeforeResume(ctx context.Context, process uintptr, moduleHandle uint32, fangPath string) error {
	capability, err := queryScenarioCapability(ctx, windows.Handle(process), moduleHandle, fangPath)
	if err != nil {
		return fmt.Errorf("loadedCapability: %w", err)
	}
	compiled := scenarioCapability()
	if capability.ProtocolVersion != scenario.ProtocolVersion || capability.Component != "fang" || capability.BuildID == "" || capability.BuildID != compiled.BuildID {
		return errors.New("loaded Fang capability does not match this scenario launcher")
	}
	isSupported := false
	for _, feature := range capability.Features {
		if feature == "scenario-v1" {
			isSupported = true
		}
	}
	if !isSupported {
		return errors.New("loaded Fang does not expose scenario-v1")
	}
	e.capability = capability
	return nil
}

func (e *scenarioClient) AfterResume(ctx context.Context, processID uint32) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("resumedContext: %w", err)
	}
	if processID == 0 {
		return errors.New("resumed client process ID is missing")
	}
	e.processID = processID
	return nil
}

func (e *scenarioClient) Resume(ctx context.Context) (uint32, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.process == nil {
		return 0, errors.New("scenario client is closed")
	}
	err := e.process.resume(ctx, e)
	if err != nil {
		return 0, fmt.Errorf("clientResume: %w", err)
	}
	return e.processID, nil
}

func (e *scenarioClient) Close(ctx context.Context) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.process == nil {
		return nil
	}
	e.cancellation.stop()
	err := e.process.terminate()
	if err != nil {
		return fmt.Errorf("clientTerminate: %w", err)
	}
	err = waitInjectedHandle(ctx, e.process.process, e.process.process)
	if err != nil {
		// Retain owned handles for a bounded cleanup retry.
		return fmt.Errorf("clientCloseWait: %w", err)
	}
	e.process.closeHandles()
	e.process = nil
	return nil
}

type scenarioCapabilityResponse struct {
	ProtocolVersion uint32   `json:"protocol_version"`
	Component       string   `json:"component"`
	BuildID         string   `json:"build_id"`
	Features        []string `json:"features"`
}

func queryScenarioCapability(ctx context.Context, process windows.Handle, moduleHandle uint32, fangPath string) (scenario.Capability, error) {
	functionRVA, err := exportedFunctionRVA(fangPath, "RecapScenarioCapabilityThread")
	if err != nil {
		functionRVA, err = exportedFunctionRVA(fangPath, "RecapScenarioCapabilityThread@4")
	}
	if err != nil {
		return scenario.Capability{}, fmt.Errorf("capabilityRVA: %w", err)
	}
	var payload [4112]byte
	binary.LittleEndian.PutUint32(payload[0:4], scenario.ProtocolVersion)
	binary.LittleEndian.PutUint32(payload[4:8], 4096)
	address, status, callErr := procVirtualAllocEx.Call(uintptr(process), 0, uintptr(len(payload)), windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	if address == 0 {
		return scenario.Capability{}, fmt.Errorf("capabilityAlloc[%d]: %w", status, callErr)
	}
	allocation := injectedRemoteAllocation{process: process, address: address}
	defer allocation.release()
	var written uintptr
	err = windows.WriteProcessMemory(process, address, &payload[0], uintptr(len(payload)), &written)
	if err != nil {
		return scenario.Capability{}, fmt.Errorf("capabilityWrite: %w", err)
	}
	if written != uintptr(len(payload)) {
		return scenario.Capability{}, errors.New("capability request was only partially written")
	}
	var threadID uint32
	thread, status, callErr := procCreateRemoteThread.Call(uintptr(process), 0, 0, uintptr(moduleHandle)+uintptr(functionRVA), address, 0, uintptr(unsafe.Pointer(&threadID)))
	if thread == 0 {
		return scenario.Capability{}, fmt.Errorf("capabilityThread[%d]: %w", status, callErr)
	}
	threadHandle := windows.Handle(thread)
	defer closeInjectedThread(threadHandle)
	allocation.isThreadRunning = true
	err = waitInjectedHandle(ctx, process, threadHandle)
	if err != nil {
		return scenario.Capability{}, fmt.Errorf("capabilityWait: %w", err)
	}
	allocation.isThreadRunning = false
	var exitCode uint32
	result, status, callErr := procGetExitCodeThread.Call(thread, uintptr(unsafe.Pointer(&exitCode)))
	if result == 0 {
		return scenario.Capability{}, fmt.Errorf("capabilityExit[%d]: %w", status, callErr)
	}
	if exitCode != 0 {
		return scenario.Capability{}, fmt.Errorf("loaded capability query failed with code %d", exitCode)
	}
	var readSize uintptr
	err = windows.ReadProcessMemory(process, address, &payload[0], uintptr(len(payload)), &readSize)
	if err != nil {
		return scenario.Capability{}, fmt.Errorf("capabilityRead: %w", err)
	}
	if readSize != uintptr(len(payload)) || binary.LittleEndian.Uint32(payload[0:4]) != scenario.ProtocolVersion || binary.LittleEndian.Uint32(payload[4:8]) != 4096 || binary.LittleEndian.Uint32(payload[12:16]) != 0 {
		return scenario.Capability{}, errors.New("loaded capability response header is invalid")
	}
	required := int32(binary.LittleEndian.Uint32(payload[8:12]))
	if required <= 1 || required > 4096 || payload[16+required-1] != 0 {
		return scenario.Capability{}, errors.New("loaded capability response length is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload[16 : 16+required-1]))
	decoder.DisallowUnknownFields()
	var response scenarioCapabilityResponse
	err = decoder.Decode(&response)
	if err != nil {
		return scenario.Capability{}, fmt.Errorf("capabilityDecode: %w", err)
	}
	var trailing any
	err = decoder.Decode(&trailing)
	if !errors.Is(err, io.EOF) {
		if err != nil {
			return scenario.Capability{}, fmt.Errorf("capabilityTrailing: %w", err)
		}
		return scenario.Capability{}, errors.New("loaded capability response contains trailing JSON")
	}
	return scenario.Capability{ProtocolVersion: response.ProtocolVersion, Component: response.Component, BuildID: response.BuildID, Features: response.Features}, nil
}
