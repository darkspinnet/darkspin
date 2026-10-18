//go:build scenario && windows

package main

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	entryDWM32        = windows.NewLazySystemDLL("dwmapi.dll")
	entryDWMAttribute = entryDWM32.NewProc("DwmGetWindowAttribute")
)

func rejectScenarioEntryOcclusion(ctx context.Context, state scenarioEntryWindowState) (*scenarioEntryOccluder, error) {
	seenWindows := make(map[uintptr]struct{})
	window := queryScenarioEntry(entryGetWindow, state.window, 3)
	for window != 0 {
		err := scenarioEntryContext(ctx)
		if err != nil {
			return nil, fmt.Errorf("entryOcclusionContext: %w", err)
		}
		if len(seenWindows) >= 256 {
			return nil, errors.New("entry window z-order exceeds its bound")
		}
		seenRecord, isSeen := seenWindows[window]
		_ = seenRecord // The set stores no record payload or foreign inventory.
		if isSeen {
			return nil, errors.New("entry window z-order changed during observation")
		}
		seenWindows[window] = struct{}{}
		isVisible := queryScenarioEntry(entryVisible, window) != 0
		isIconic := queryScenarioEntry(entryIconic, window) != 0
		if isVisible && !isIconic {
			rect := scenarioEntryRect{}
			result, err := callScenarioEntry(entryWindowRect, window, uintptr(unsafe.Pointer(&rect)))
			if err != nil || result == 0 {
				return nil, fmt.Errorf("entryOverlayBounds: %w", err)
			}
			if rect.Left < state.clientRect.Right && rect.Right > state.clientRect.Left && rect.Top < state.clientRect.Bottom && rect.Bottom > state.clientRect.Top {
				occluder := describeScenarioEntryOccluder(ctx, window, uint32(len(seenWindows)), rect, isVisible, isIconic)
				// No class, PID, cloak flags or query failure filters this window.
				return occluder, errors.New("another visible window overlaps the owned client crop")
			}
		}
		window = queryScenarioEntry(entryGetWindow, window, 3)
	}
	return nil, nil
}

func describeScenarioEntryOccluder(ctx context.Context, window uintptr, ordinal uint32, rect scenarioEntryRect, isVisible, isIconic bool) *scenarioEntryOccluder {
	occluder := &scenarioEntryOccluder{ObservedAt: time.Now().UTC(), ZOrderOrdinal: ordinal,
		Window: uint64(window), Rect: rect, IsRectAvailable: true, IsVisible: isVisible, IsIconic: isIconic}
	err := scenarioEntryContext(ctx)
	if err != nil {
		// Keep the completed first-intersection fields; supplemental fields stay unknown.
		return occluder
	}
	var processID uint32
	threadID, err := callScenarioEntry(entryWindowPID, window, uintptr(unsafe.Pointer(&processID)))
	if err != nil {
		// A raced or failed supplemental identity query never changes the rejection.
	} else if threadID != 0 && processID != 0 {
		occluder.ProcessID, occluder.IsProcessIDAvailable = processID, true
	}
	classNames := make([]uint16, 256)
	classLength, err := callScenarioEntry(entryWindowClass, window, uintptr(unsafe.Pointer(&classNames[0])), uintptr(len(classNames)))
	if err != nil {
		// A missing class is unknown; no caption or foreign content is queried.
	} else if classLength < uintptr(len(classNames)-1) {
		occluder.Class, occluder.IsClassAvailable = windows.UTF16ToString(classNames), true
	}
	err = scenarioEntryContext(ctx)
	if err != nil {
		return occluder
	}
	err = entryDWMAttribute.Find()
	occluder.IsDWMProcedureAvailabilityKnown = true
	if err != nil {
		// Installed procedure availability is independent of numeric DWM observations.
		return occluder
	}
	occluder.IsDWMProcedureAvailable = true
	var cloakFlags uint32
	status, secondary, callErr := entryDWMAttribute.Call(window, 14, uintptr(unsafe.Pointer(&cloakFlags)), 4)
	_ = secondary // The HRESULT occupies EAX; EDX has no API return contract.
	if callErr != syscall.Errno(0) {
		// DwmGetWindowAttribute returns HRESULT; stale LastError is not meaningful.
	}
	hresult := uint32(status)
	occluder.DWMHRESULT, occluder.IsDWMHRESULTAvailable = int32(hresult), true
	if hresult != 0 {
		return occluder
	}
	occluder.CloakFlags, occluder.AreCloakFlagsAvailable = cloakFlags, true
	if cloakFlags & ^uint32(7) == 0 {
		isCloaked := cloakFlags != 0
		occluder.IsCloaked = &isCloaked
	}
	// Unknown bits retain raw successful flags but leave their interpretation unknown.
	return occluder
}
