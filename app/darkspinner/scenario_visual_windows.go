//go:build scenario && windows

package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	entryUser32           = windows.NewLazySystemDLL("user32.dll")
	entryGDI32            = windows.NewLazySystemDLL("gdi32.dll")
	entrySHCore           = windows.NewLazySystemDLL("shcore.dll")
	entryEnumWindows      = entryUser32.NewProc("EnumWindows")
	entryWindowPID        = entryUser32.NewProc("GetWindowThreadProcessId")
	entryVisible          = entryUser32.NewProc("IsWindowVisible")
	entryEnabled          = entryUser32.NewProc("IsWindowEnabled")
	entryIconic           = entryUser32.NewProc("IsIconic")
	entryGetWindow        = entryUser32.NewProc("GetWindow")
	entryForeground       = entryUser32.NewProc("GetForegroundWindow")
	entryClientRect       = entryUser32.NewProc("GetClientRect")
	entryWindowRect       = entryUser32.NewProc("GetWindowRect")
	entryClientToScreen   = entryUser32.NewProc("ClientToScreen")
	entryWindowClass      = entryUser32.NewProc("GetClassNameW")
	entryWindowDPI        = entryUser32.NewProc("GetDpiForWindow")
	entryWindowDPIContext = entryUser32.NewProc("GetWindowDpiAwarenessContext")
	entrySystemMetric     = entryUser32.NewProc("GetSystemMetrics")
	entryThreadDPIContext = entryUser32.NewProc("GetThreadDpiAwarenessContext")
	entryDPIAwareness     = entryUser32.NewProc("GetAwarenessFromDpiAwarenessContext")
	entryMonitorFromRect  = entryUser32.NewProc("MonitorFromRect")
	entryMonitorInfo      = entryUser32.NewProc("GetMonitorInfoW")
	entryMonitorScale     = entrySHCore.NewProc("GetScaleFactorForMonitor")
	entryGetDC            = entryUser32.NewProc("GetDC")
	entryReleaseDC        = entryUser32.NewProc("ReleaseDC")
	entryCreateDC         = entryGDI32.NewProc("CreateCompatibleDC")
	entryDeleteDC         = entryGDI32.NewProc("DeleteDC")
	entryCreateDIB        = entryGDI32.NewProc("CreateDIBSection")
	entrySelectObject     = entryGDI32.NewProc("SelectObject")
	entryDeleteObject     = entryGDI32.NewProc("DeleteObject")
	entryBitBlt           = entryGDI32.NewProc("BitBlt")
	entryGDIFlush         = entryGDI32.NewProc("GdiFlush")
	entryWindowCallback   = syscall.NewCallback(visitScenarioEntryWindow)
)

type scenarioEntryWindowScan struct {
	processID uint32
	windows   []uintptr
	count     uint32
	err       error
}

type scenarioEntryWindowState struct {
	window      uintptr
	clientRect  scenarioEntryRect
	dpi         uint32
	calibration scenarioEntryCalibration
	occluder    *scenarioEntryOccluder
}

type scenarioEntryMonitorInfo struct {
	Size    uint32
	Monitor scenarioEntryRect
	Work    scenarioEntryRect
	Flags   uint32
}

type scenarioEntryBitmapInfo struct {
	Size                         uint32
	Width, Height                int32
	Planes, BitCount             uint16
	Compression, SizeImage       uint32
	XPelsPerMeter, YPelsPerMeter int32
	ClrUsed, ClrImportant        uint32
	Colors                       [1]uint32
}

// BOOL/handle queries below document zero as a valid negative/absent result;
// their stale LastError is not an error contract. EDX has no Win32 return slot.
func queryScenarioEntry(proc *windows.LazyProc, arguments ...uintptr) uintptr {
	result, secondary, callErr := proc.Call(arguments...)
	_ = secondary
	if callErr != syscall.Errno(0) {
		// These query APIs do not define LastError. Retain only their result.
	}
	return result
}

func callScenarioEntry(proc *windows.LazyProc, arguments ...uintptr) (uintptr, error) {
	result, secondary, callErr := proc.Call(arguments...)
	_ = secondary // Win32 returns a single register; EDX is unspecified.
	if result != 0 {
		return result, nil
	}
	if callErr == syscall.Errno(0) {
		return 0, fmt.Errorf("entry native %s returned zero", proc.Name)
	}
	return 0, fmt.Errorf("entryNative[%s]: %w", proc.Name, callErr)
}

func visitScenarioEntryWindow(window, parameter uintptr) uintptr {
	scan := (*scenarioEntryWindowScan)(unsafe.Pointer(parameter))
	scan.count++
	if scan.count > 512 {
		scan.err = errors.New("entry window enumeration exceeds its bound")
		return 0
	}
	var processID uint32
	threadID, err := callScenarioEntry(entryWindowPID, window, uintptr(unsafe.Pointer(&processID)))
	if err != nil {
		scan.err = fmt.Errorf("entryWindowIdentity: %w", err)
		return 0
	}
	if threadID == 0 || processID != scan.processID || queryScenarioEntry(entryVisible, window) == 0 {
		return 1
	}
	// Any visible owned secondary window makes this first backend unavailable.
	scan.windows = append(scan.windows, window)
	return 1
}

func observeScenarioEntryWindow(ctx context.Context, processID uint32) (scenarioEntryWindowState, error) {
	state, err := observeScenarioEntryOwnedWindow(ctx, processID)
	if err != nil {
		return state, fmt.Errorf("entryOwnedWindow: %w", err)
	}
	state.occluder, err = rejectScenarioEntryOcclusion(ctx, state)
	if err != nil {
		return state, fmt.Errorf("entryOcclusion: %w", err)
	}
	return state, nil
}

// Shared ownership/calibration observation; GDI still rejects every overlap.
func observeScenarioEntryOwnedWindow(ctx context.Context, processID uint32) (scenarioEntryWindowState, error) {
	state := scenarioEntryWindowState{}
	scan := scenarioEntryWindowScan{processID: processID}
	result, err := callScenarioEntry(entryEnumWindows, entryWindowCallback, uintptr(unsafe.Pointer(&scan)))
	runtime.KeepAlive(&scan)
	if scan.err != nil {
		return state, fmt.Errorf("entryWindowScan: %w", scan.err)
	}
	if err != nil || result == 0 {
		return state, fmt.Errorf("entryWindowEnumerate: %w", err)
	}
	if len(scan.windows) != 1 {
		return state, errors.New("entry capture requires one visible owned top-level window")
	}
	state.window = scan.windows[0]
	if queryScenarioEntry(entryGetWindow, state.window, 4) != 0 ||
		queryScenarioEntry(entryForeground) != state.window ||
		queryScenarioEntry(entryEnabled, state.window) == 0 ||
		queryScenarioEntry(entryIconic, state.window) != 0 {
		return state, errors.New("entry window is owned-popup, unfocused, disabled or minimized")
	}
	className := make([]uint16, 256)
	classLength, err := callScenarioEntry(entryWindowClass, state.window, uintptr(unsafe.Pointer(&className[0])), uintptr(len(className)))
	if err != nil {
		return state, fmt.Errorf("entryWindowClass: %w", err)
	}
	if classLength >= uintptr(len(className)-1) || windows.UTF16ToString(className) == "#32770" {
		return state, errors.New("entry window is a native dialog or has unsupported class metadata")
	}
	err = observeScenarioEntryAwareness(&state)
	if err != nil {
		return state, fmt.Errorf("entryAwareness: %w", err)
	}
	rect := scenarioEntryRect{}
	result, err = callScenarioEntry(entryClientRect, state.window, uintptr(unsafe.Pointer(&rect)))
	if err != nil || result == 0 {
		return state, fmt.Errorf("entryClientBounds: %w", err)
	}
	topLeft := scenarioEntryPoint{X: rect.Left, Y: rect.Top}
	result, err = callScenarioEntry(entryClientToScreen, state.window, uintptr(unsafe.Pointer(&topLeft)))
	if err != nil || result == 0 {
		return state, fmt.Errorf("entryClientOrigin: %w", err)
	}
	state.calibration.ClientTopLeft, state.calibration.IsClientTopLeftAvailable = topLeft, true
	bottomRight := scenarioEntryPoint{X: rect.Right, Y: rect.Bottom}
	result, err = callScenarioEntry(entryClientToScreen, state.window, uintptr(unsafe.Pointer(&bottomRight)))
	if err != nil || result == 0 {
		return state, fmt.Errorf("entryClientCorner: %w", err)
	}
	state.calibration.ClientBottomRight, state.calibration.IsClientBottomRightAvailable = bottomRight, true
	// Both API results are device coordinates on the already-aware locked thread.
	// Target DPI and monitor scale are observations, never conversion multipliers.
	width, height := int64(bottomRight.X)-int64(topLeft.X), int64(bottomRight.Y)-int64(topLeft.Y)
	if width <= 0 || height <= 0 || width > scenarioEntryDimensionLimit || height > scenarioEntryDimensionLimit || width*height*4 > scenarioEntryPixelLimit {
		return state, errors.New("entry client dimensions exceed the supported frame bound")
	}
	state.clientRect = scenarioEntryRect{Left: topLeft.X, Top: topLeft.Y, Right: bottomRight.X, Bottom: bottomRight.Y}
	err = validateScenarioEntryMonitor(&state)
	if err != nil {
		return state, fmt.Errorf("entryMonitor: %w", err)
	}
	screenLeft := int32(queryScenarioEntry(entrySystemMetric, 76))
	screenTop := int32(queryScenarioEntry(entrySystemMetric, 77))
	screenWidth := int32(queryScenarioEntry(entrySystemMetric, 78))
	screenHeight := int32(queryScenarioEntry(entrySystemMetric, 79))
	if screenWidth <= 0 || screenHeight <= 0 || state.clientRect.Left < screenLeft || state.clientRect.Top < screenTop ||
		int64(state.clientRect.Right) > int64(screenLeft)+int64(screenWidth) || int64(state.clientRect.Bottom) > int64(screenTop)+int64(screenHeight) {
		return state, errors.New("entry client rectangle is off the virtual desktop")
	}
	return state, nil
}

func observeScenarioEntryAwareness(state *scenarioEntryWindowState) error {
	for _, proc := range []*windows.LazyProc{entryWindowDPI, entryWindowDPIContext, entryThreadDPIContext, entryDPIAwareness} {
		err := proc.Find()
		if err != nil {
			return fmt.Errorf("entryDPISupport[%s]: %w", proc.Name, err)
		}
	}
	dpi := queryScenarioEntry(entryWindowDPI, state.window)
	state.dpi = uint32(dpi)
	state.calibration.TargetDPI, state.calibration.IsTargetDPIAvailable = uint32(dpi), dpi != 0
	targetDPI := queryScenarioEntry(entryWindowDPIContext, state.window)
	if targetDPI != 0 {
		awareness := int32(queryScenarioEntry(entryDPIAwareness, targetDPI))
		state.calibration.TargetAwareness, state.calibration.IsTargetAwarenessAvailable = awareness, awareness >= 0 && awareness <= 2
	}
	threadDPI := queryScenarioEntry(entryThreadDPIContext)
	if threadDPI != 0 {
		awareness := int32(queryScenarioEntry(entryDPIAwareness, threadDPI))
		state.calibration.WorkerAwareness, state.calibration.IsWorkerAwarenessAvailable = awareness, awareness >= 0 && awareness <= 2
	}
	if !state.calibration.IsWorkerAwarenessAvailable || state.calibration.WorkerAwareness != 2 {
		return errors.New("entry capture requires a per-monitor-aware worker thread")
	}
	if !state.calibration.IsTargetDPIAvailable || !state.calibration.IsTargetAwarenessAvailable {
		return errors.New("entry target DPI/awareness is unavailable")
	}
	return nil
}

func validateScenarioEntryMonitor(state *scenarioEntryWindowState) error {
	rect := state.clientRect
	monitor, err := callScenarioEntry(entryMonitorFromRect, uintptr(unsafe.Pointer(&rect)), 0)
	if err != nil {
		return fmt.Errorf("entryMonitorHandle: %w", err)
	}
	state.calibration.Monitor, state.calibration.IsMonitorAvailable = uint64(monitor), true
	monitorInfo := scenarioEntryMonitorInfo{Size: uint32(unsafe.Sizeof(scenarioEntryMonitorInfo{}))}
	result, err := callScenarioEntry(entryMonitorInfo, monitor, uintptr(unsafe.Pointer(&monitorInfo)))
	if err != nil || result == 0 {
		return fmt.Errorf("entryMonitorBounds: %w", err)
	}
	state.calibration.MonitorRect, state.calibration.IsMonitorRectAvailable = monitorInfo.Monitor, true
	if rect.Left < monitorInfo.Monitor.Left || rect.Top < monitorInfo.Monitor.Top || rect.Right > monitorInfo.Monitor.Right || rect.Bottom > monitorInfo.Monitor.Bottom {
		return errors.New("entry crop must fit wholly on one monitor")
	}
	err = entryMonitorScale.Find()
	if err != nil {
		return fmt.Errorf("entryScaleSupport: %w", err)
	}
	var scale uint32
	status, secondary, callErr := entryMonitorScale.Call(monitor, uintptr(unsafe.Pointer(&scale)))
	_ = secondary // HRESULT is the only result; EDX has no API contract.
	if callErr != syscall.Errno(0) {
		// This API returns HRESULT; LastError is not meaningful.
	}
	state.calibration.MonitorScaleHRESULT, state.calibration.IsMonitorScaleHRESULTAvailable = int32(status), true
	state.calibration.MonitorScale = scale
	if status != 0 {
		// The documented API supplies a fallback scale even on failure. It is
		// retained numerically but never accepted as a successful observation.
		return fmt.Errorf("entry monitor scale HRESULT 0x%08x", uint32(status))
	}
	switch scale {
	case 100, 120, 125, 140, 150, 160, 175, 180, 200, 225, 250, 300, 350, 400, 450, 500:
		state.calibration.IsMonitorScaleAvailable = true
	default:
		return errors.New("entry monitor scale is outside its documented supported enum")
	}
	return nil
}

func (e *scenarioClient) captureEntryGDI(ctx context.Context, req scenarioEntryFrameRequest) (frame scenarioEntryFrame, err error) {
	err = scenarioEntryContext(ctx)
	if err != nil {
		return frame, fmt.Errorf("frameContext: %w", err)
	}
	if e == nil || req.RunID == "" || req.Sequence == 0 {
		return frame, errors.New("entry frame requires owned client/run/sequence")
	}
	err = lockScenarioLiveness(ctx, &e.mutex)
	if err != nil {
		return frame, fmt.Errorf("frameClientLock: %w", err)
	}
	defer e.mutex.Unlock()
	if e.runID != req.RunID || e.process == nil || e.processID == 0 {
		return frame, errors.New("entry frame run or resumed client is unavailable")
	}
	process := e.process
	createdAt, err := observeScenarioEntryProcess(ctx, process, e.processID)
	if err != nil {
		return frame, fmt.Errorf("frameProcessBefore: %w", err)
	}
	frame.RunID, frame.Sequence, frame.ProcessID = req.RunID, req.Sequence, e.processID
	frame.ProcessCreatedAt, frame.StartedAt = createdAt, time.Now().UTC()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	before, err := observeScenarioEntryWindow(ctx, e.processID)
	frame.CalibrationBefore = before.calibration
	frame.OccluderBefore = before.occluder
	if err != nil {
		return frame, fmt.Errorf("frameWindowBefore: %w", err)
	}
	pixels, err := copyScenarioEntryPixels(ctx, before)
	if err != nil {
		return frame, fmt.Errorf("framePixels: %w", err)
	}
	after, err := observeScenarioEntryWindow(ctx, e.processID)
	frame.CalibrationAfter = after.calibration
	frame.OccluderAfter = after.occluder
	if err != nil {
		return frame, fmt.Errorf("frameWindowAfter: %w", err)
	}
	afterCreatedAt, err := observeScenarioEntryProcess(ctx, process, e.processID)
	if err != nil {
		return frame, fmt.Errorf("frameProcessAfter: %w", err)
	}
	if before != after || !createdAt.Equal(afterCreatedAt) {
		return frame, errors.New("entry ownership/geometry changed during capture")
	}
	err = scenarioEntryContext(ctx)
	if err != nil {
		return frame, fmt.Errorf("frameFinalContext: %w", err)
	}
	frame.Window = uint64(before.window)
	frame.ClientRect, frame.DPI = before.clientRect, before.dpi
	frame.Width, frame.Height = int(before.clientRect.Right-before.clientRect.Left), int(before.clientRect.Bottom-before.clientRect.Top)
	frame.FinishedAt, frame.Pixels = time.Now().UTC(), pixels
	frame.IsForeground, frame.IsVisible, frame.IsEnabled = true, true, true
	return frame, nil
}

func observeScenarioEntryProcess(ctx context.Context, process *injectedProcess, processID uint32) (time.Time, error) {
	err := lockScenarioLiveness(ctx, &process.mutex)
	if err != nil {
		return time.Time{}, fmt.Errorf("entryProcessLock: %w", err)
	}
	defer process.mutex.Unlock()
	if process.process == 0 || process.processID != processID || !process.isResumed {
		return time.Time{}, errors.New("entry retained process identity is unavailable")
	}
	status, err := windows.WaitForSingleObject(process.process, 0)
	if err != nil {
		return time.Time{}, fmt.Errorf("entryProcessWait: %w", err)
	}
	if status != uint32(windows.WAIT_TIMEOUT) {
		return time.Time{}, errors.New("entry retained process is not positively alive")
	}
	var creation, exit, kernel, user windows.Filetime
	err = windows.GetProcessTimes(process.process, &creation, &exit, &kernel, &user)
	if err != nil {
		return time.Time{}, fmt.Errorf("entryProcessTimes: %w", err)
	}
	err = scenarioEntryContext(ctx)
	if err != nil {
		return time.Time{}, fmt.Errorf("entryProcessContext: %w", err)
	}
	return time.Unix(0, creation.Nanoseconds()).UTC(), nil
}

func copyScenarioEntryPixels(ctx context.Context, state scenarioEntryWindowState) (pixels []byte, err error) {
	err = scenarioEntryContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("pixelContext: %w", err)
	}
	desktopDC, err := callScenarioEntry(entryGetDC, 0)
	if err != nil {
		return nil, fmt.Errorf("pixelDesktopDC: %w", err)
	}
	var memoryDC, bitmap, previousObject uintptr
	defer func() {
		cleanupErr := releaseScenarioEntryGDI(desktopDC, memoryDC, bitmap, previousObject)
		if cleanupErr != nil {
			pixels = nil
			err = fmt.Errorf("pixelRelease: %w", errors.Join(err, cleanupErr))
		}
	}()
	memoryDC, err = callScenarioEntry(entryCreateDC, desktopDC)
	if err != nil {
		return nil, fmt.Errorf("pixelMemoryDC: %w", err)
	}
	width, height := state.clientRect.Right-state.clientRect.Left, state.clientRect.Bottom-state.clientRect.Top
	bitmapInfo := scenarioEntryBitmapInfo{Size: 40, Width: width, Height: -height, Planes: 1, BitCount: 32}
	var bits uintptr
	bitmap, err = callScenarioEntry(entryCreateDIB, desktopDC, uintptr(unsafe.Pointer(&bitmapInfo)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if err != nil {
		return nil, fmt.Errorf("pixelDIB: %w", err)
	}
	if bits == 0 {
		return nil, errors.New("entry private DIB has no pixel storage")
	}
	previousObject, err = callScenarioEntry(entrySelectObject, memoryDC, bitmap)
	if err != nil {
		return nil, fmt.Errorf("pixelSelect: %w", err)
	}
	if previousObject == ^uintptr(0) {
		previousObject = 0
		return nil, errors.New("entry DIB selection returned HGDI_ERROR")
	}
	err = scenarioEntryContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("pixelCopyContext: %w", err)
	}
	result, err := callScenarioEntry(entryBitBlt, memoryDC, 0, 0, uintptr(width), uintptr(height), desktopDC, uintptr(state.clientRect.Left), uintptr(state.clientRect.Top), 0x00CC0020)
	if err != nil || result == 0 {
		return nil, fmt.Errorf("pixelBitBlt: %w", err)
	}
	result, err = callScenarioEntry(entryGDIFlush)
	if err != nil || result == 0 {
		return nil, fmt.Errorf("pixelFlush: %w", err)
	}
	err = scenarioEntryContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("pixelReadContext: %w", err)
	}
	size := int(width) * int(height) * 4
	privatePixels := unsafe.Slice((*byte)(unsafe.Pointer(bits)), size)
	pixels = make([]byte, size)
	isVaried := false
	for index := 0; index < size; index += 4 {
		if index%65536 == 0 {
			err = scenarioEntryContext(ctx)
			if err != nil {
				return nil, fmt.Errorf("pixelConvertContext: %w", err)
			}
		}
		pixels[index], pixels[index+1], pixels[index+2], pixels[index+3] = privatePixels[index+2], privatePixels[index+1], privatePixels[index], 255
		if index > 0 && (pixels[index] != pixels[0] || pixels[index+1] != pixels[1] || pixels[index+2] != pixels[2]) {
			isVaried = true
		}
	}
	if !isVaried {
		return nil, errors.New("entry frame is blank or uniformly colored")
	}
	return pixels, nil
}

func releaseScenarioEntryGDI(desktopDC, memoryDC, bitmap, previousObject uintptr) error {
	errs := []error{}
	if previousObject != 0 {
		restored, err := callScenarioEntry(entrySelectObject, memoryDC, previousObject)
		if err != nil || restored == ^uintptr(0) {
			errs = append(errs, errors.Join(err, errors.New("entry GDI object restoration failed")))
		}
	}
	// Delete the private DC first if restoration failed, detaching its bitmap.
	if memoryDC != 0 {
		deleted, err := callScenarioEntry(entryDeleteDC, memoryDC)
		if err != nil || deleted == 0 {
			errs = append(errs, fmt.Errorf("entryDeleteDC: %w", err))
		}
	}
	if bitmap != 0 {
		deleted, err := callScenarioEntry(entryDeleteObject, bitmap)
		if err != nil || deleted == 0 {
			errs = append(errs, fmt.Errorf("entryDeleteBitmap: %w", err))
		}
	}
	released, err := callScenarioEntry(entryReleaseDC, 0, desktopDC)
	if err != nil || released == 0 {
		errs = append(errs, fmt.Errorf("entryReleaseDC: %w", err))
	}
	err = errors.Join(errs...)
	if err != nil {
		return fmt.Errorf("entryGDIRelease: %w", err)
	}
	return nil
}
