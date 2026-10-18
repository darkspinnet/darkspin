//go:build scenario && windows

package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"
	"unsafe"
)

// The GDI error is retained independently; whole-item pixels cannot establish
// a successful desktop crop, screen identity or control/input eligibility.
func (e *scenarioClient) CaptureEntryFrame(ctx context.Context, req scenarioEntryFrameRequest) (scenarioEntryFrame, error) {
	frame, gdiErr := e.captureEntryGDI(ctx, req)
	contextErr := scenarioEntryContext(ctx)
	if contextErr != nil {
		return frame, gdiErr
	}
	capture := e.captureEntryWindow(ctx, req)
	frame.WindowCapture = &capture
	return frame, gdiErr
}

func (e *scenarioClient) captureEntryWindow(ctx context.Context, req scenarioEntryFrameRequest) (capture scenarioEntryWindowCapture) {
	capture = scenarioEntryWindowCapture{Backend: scenarioWindowBackend, RunID: req.RunID, Sequence: req.Sequence, StartedAt: time.Now().UTC()}
	defer finishScenarioWindowCapture(&capture)
	err := scenarioEntryContext(ctx)
	if err != nil {
		capture.Reason = "window context unavailable: " + err.Error()
		return capture
	}
	if e == nil || req.RunID == "" || req.Sequence == 0 {
		capture.Reason = "window requires owned client/run/sequence"
		return capture
	}
	boundedCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	// Preserve the caller's inherited apartment; these are read-only samples.
	// The nested lock is balanced only after the child completion is joined.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer finalizeScenarioWindowCaller(boundedCtx, &capture)
	capture.CallerApartmentBefore, err = observeScenarioWindowApartment()
	if err != nil {
		capture.Reason = "window caller before unavailable: " + err.Error()
		return capture
	}
	err = validateScenarioWindowCallerApartment(capture.CallerApartmentBefore)
	if err != nil {
		capture.Reason = "window caller before unsupported: " + err.Error()
		return capture
	}
	err = scenarioEntryContext(boundedCtx)
	if err != nil {
		capture.Reason = "window dispatch context unavailable: " + err.Error()
		return capture
	}
	callerBefore, startedAt := capture.CallerApartmentBefore, capture.StartedAt
	completed := make(chan scenarioEntryWindowCapture, 1)
	go runScenarioWindowCapture(boundedCtx, e, req, completed)
	// No context-select return: even canceled calls join owned cleanup. Blocking
	// native calls remain contained by the existing outer worker deadline.
	child, isCompleted := <-completed
	if isCompleted {
		capture = child
	} else {
		capture.Reason = "window child completed without a result"
	}
	capture.StartedAt, capture.CallerApartmentBefore = startedAt, callerBefore
	for range completed {
		invalidateScenarioWindowCapture(&capture, "window child returned multiple results")
	}
	return capture
}

func runScenarioWindowCapture(ctx context.Context, client *scenarioClient, req scenarioEntryFrameRequest, completed chan<- scenarioEntryWindowCapture) {
	// The body returns after all its defers: same-thread COM cleanup, lock
	// release and thread unlock precede this single owned DTO and closure.
	capture := captureScenarioWindowOnThread(ctx, client, req)
	completed <- capture
	close(completed)
}

func captureScenarioWindowOnThread(ctx context.Context, client *scenarioClient, req scenarioEntryFrameRequest) (capture scenarioEntryWindowCapture) {
	capture = scenarioEntryWindowCapture{Backend: scenarioWindowBackend, RunID: req.RunID, Sequence: req.Sequence, StartedAt: time.Now().UTC()}
	defer finishScenarioWindowCapture(&capture)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	attempt := scenarioWGCAttempt{}
	isClientLocked := false
	// Cleanup and post-cleanup apartment sampling precede thread unlock.
	defer finalizeScenarioWindowChild(ctx, &attempt, &capture, client, &isClientLocked)
	err := scenarioEntryContext(ctx)
	if err != nil {
		capture.Reason = "window child context unavailable: " + err.Error()
		return capture
	}
	capture.ApartmentBefore, err = observeScenarioWindowApartment()
	if err != nil {
		capture.Reason = "window child apartment unavailable: " + err.Error()
		return capture
	}
	err = validateScenarioWindowCaptureApartment(capture.ApartmentBefore, true)
	if err != nil {
		capture.Reason = "window child apartment unsupported: " + err.Error()
		return capture
	}
	err = lockScenarioLiveness(ctx, &client.mutex)
	if err != nil {
		capture.Reason = "window client lock unavailable: " + err.Error()
		return capture
	}
	isClientLocked = true
	if client.runID != req.RunID || client.process == nil || client.processID == 0 {
		capture.Reason = "window retained resumed client is unavailable"
		return capture
	}
	capture.ProcessID = client.processID
	createdAt, err := observeScenarioEntryProcess(ctx, client.process, client.processID)
	if err != nil {
		capture.Reason = "window process before unavailable: " + err.Error()
		return capture
	}
	capture.ProcessCreatedAt = createdAt
	// Physical worker awareness and current owned geometry are measured here,
	// on the actual capture child, independently of GDI/caller observations.
	before, err := observeScenarioWindowCapture(ctx, client.processID, &capture.Before)
	if err != nil {
		capture.Reason = "window before unavailable: " + err.Error()
		return capture
	}
	capture.OccluderBefore, err = sampleScenarioWindowOcclusion(ctx, before)
	if err != nil {
		capture.Reason = "window before overlap observation unavailable: " + err.Error()
		return capture
	}
	err = attempt.initialize(ctx, &capture)
	if err != nil {
		capture.Reason = "window initialization unavailable: " + err.Error()
		return capture
	}
	itemSize, err := attempt.create(ctx, before.window)
	capture.Before.ItemSize = itemSize
	capture.Before.IsItemSizeAvailable = attempt.isItemSizeAvailable
	if err != nil {
		capture.Reason = "window resource creation unavailable: " + err.Error()
		return capture
	}
	err = attempt.receive(ctx, &capture)
	if err != nil {
		capture.Reason = "window frame unavailable: " + err.Error()
		return capture
	}
	pixels, err := attempt.copySurface(ctx, &capture)
	if err != nil {
		capture.Reason = "window surface unavailable: " + err.Error()
		return capture
	}
	after, err := observeScenarioWindowCapture(ctx, client.processID, &capture.After)
	if err != nil {
		capture.Reason = "window after unavailable: " + err.Error()
		return capture
	}
	capture.OccluderAfter, err = sampleScenarioWindowOcclusion(ctx, after)
	if err != nil {
		capture.Reason = "window after overlap observation unavailable: " + err.Error()
		return capture
	}
	itemSize, err = attempt.itemSize(ctx)
	capture.After.ItemSize = itemSize
	capture.After.IsItemSizeAvailable = attempt.isItemSizeAvailable
	if err != nil {
		capture.Reason = "window final item size unavailable: " + err.Error()
		return capture
	}
	afterCreatedAt, err := observeScenarioEntryProcess(ctx, client.process, client.processID)
	if err != nil {
		capture.Reason = "window process after unavailable: " + err.Error()
		return capture
	}
	if before != after || capture.Before != capture.After || !createdAt.Equal(afterCreatedAt) || capture.ContentSize != itemSize {
		capture.Reason = "window ownership/calibration/item/content size changed"
		return capture
	}
	err = scenarioEntryContext(ctx)
	if err != nil {
		capture.Reason = "window final context unavailable: " + err.Error()
		return capture
	}
	capture.Pixels, capture.IsAvailable = pixels, true
	capture.Reason = "whole-owned-item copied; geometry/isolation/semantics unknown"
	return capture
}

func finishScenarioWindowCapture(capture *scenarioEntryWindowCapture) {
	capture.FinishedAt = time.Now().UTC()
	if !capture.IsAvailable {
		capture.Pixels = nil
	}
}

func finalizeScenarioWindowCaller(ctx context.Context, capture *scenarioEntryWindowCapture) {
	// Fixed read-only diagnostics still finish on the caller thread if the
	// context expired while joining. They neither initialize nor uninitialize.
	observation, err := observeScenarioWindowApartment()
	capture.CallerApartmentAfter = observation
	if err != nil {
		invalidateScenarioWindowCapture(capture, "window caller after unavailable: "+err.Error())
	} else {
		err = validateScenarioWindowCallerApartment(observation)
		if err != nil {
			invalidateScenarioWindowCapture(capture, "window caller after unsupported: "+err.Error())
		} else if capture.CallerApartmentBefore != observation {
			invalidateScenarioWindowCapture(capture, "window caller apartment/thread changed")
		}
	}
	err = scenarioEntryContext(ctx)
	if err != nil {
		invalidateScenarioWindowCapture(capture, "window caller context ended: "+err.Error())
	}
}

func finalizeScenarioWindowChild(ctx context.Context, attempt *scenarioWGCAttempt, capture *scenarioEntryWindowCapture, client *scenarioClient, isClientLocked *bool) {
	finalizeScenarioWGCAttempt(ctx, attempt, capture)
	if *isClientLocked {
		client.mutex.Unlock()
	}
}

func finalizeScenarioWGCAttempt(ctx context.Context, attempt *scenarioWGCAttempt, capture *scenarioEntryWindowCapture) {
	err := attempt.close()
	if err != nil {
		invalidateScenarioWindowCapture(capture, "window cleanup unavailable: "+err.Error())
	}
	observation, err := observeScenarioWindowApartment()
	capture.ApartmentAfter = observation
	if err != nil {
		invalidateScenarioWindowCapture(capture, "window child after unavailable: "+err.Error())
	} else {
		err = validateScenarioWindowCaptureApartment(observation, true)
		if err != nil {
			invalidateScenarioWindowCapture(capture, "window child after unsupported: "+err.Error())
		} else if !capture.ApartmentBefore.IsThreadIDAvailable || capture.ApartmentBefore.ThreadID != observation.ThreadID {
			invalidateScenarioWindowCapture(capture, "window child thread changed")
		}
	}
	err = scenarioEntryContext(ctx)
	if err != nil {
		invalidateScenarioWindowCapture(capture, "window context ended during finalization: "+err.Error())
	}
}

func invalidateScenarioWindowCapture(capture *scenarioEntryWindowCapture, reason string) {
	if capture.IsAvailable {
		// A generic success description is replaced; failed-operation reasons
		// (including late HRESULTs) stay first when finalization adds failures.
		capture.Reason = ""
	}
	capture.IsAvailable, capture.Pixels = false, nil
	appendScenarioWindowFailure(capture, reason)
}

func appendScenarioWindowFailure(capture *scenarioEntryWindowCapture, reason string) {
	if capture.Reason == "" {
		capture.Reason = reason
		return
	}
	capture.Reason += "; " + reason
}

func observeScenarioWindowCapture(ctx context.Context, processID uint32, observation *scenarioWindowObservation) (scenarioEntryWindowState, error) {
	state, err := observeScenarioEntryOwnedWindow(ctx, processID)
	observation.NativeWindow, observation.Calibration = uint64(state.window), state.calibration
	if err != nil {
		return state, fmt.Errorf("windowOwned: %w", err)
	}
	var rect scenarioEntryRect
	result, err := callScenarioEntry(entryWindowRect, state.window, uintptr(unsafe.Pointer(&rect)))
	if err != nil || result == 0 {
		return state, fmt.Errorf("windowBounds: %w", errors.Join(err, errors.New("window rectangle unavailable")))
	}
	observation.WindowRect, observation.IsWindowRectAvailable = rect, true
	width, height := int64(rect.Right)-int64(rect.Left), int64(rect.Bottom)-int64(rect.Top)
	monitor := state.calibration.MonitorRect
	if width <= 0 || height <= 0 || width > scenarioEntryDimensionLimit || height > scenarioEntryDimensionLimit || width*height*4 > scenarioEntryPixelLimit ||
		rect.Left < monitor.Left || rect.Top < monitor.Top || rect.Right > monitor.Right || rect.Bottom > monitor.Bottom {
		return state, errors.New("whole-owned-window rectangle exceeds supported single-monitor bounds")
	}
	return state, nil
}

func sampleScenarioWindowOcclusion(ctx context.Context, state scenarioEntryWindowState) (*scenarioEntryOccluder, error) {
	occluder, err := rejectScenarioEntryOcclusion(ctx, state)
	if err != nil && occluder == nil {
		return nil, fmt.Errorf("windowOverlapSample: %w", err)
	}
	if err != nil {
		// The unchanged desktop guard found a real first intersection. Item
		// capture retains it as diagnostic, never as a desktop eligibility pass.
	}
	return occluder, nil
}

func validateScenarioWindowSize(size scenarioWindowSize) error {
	if size.Width <= 0 || size.Height <= 0 || size.Width > scenarioEntryDimensionLimit || size.Height > scenarioEntryDimensionLimit ||
		int64(size.Width)*int64(size.Height)*4 > scenarioEntryPixelLimit {
		return errors.New("WGC item/content dimensions exceed supported bounds")
	}
	return nil
}

func waitScenarioWGC(ctx context.Context) error {
	err := scenarioEntryContext(ctx)
	if err != nil {
		return fmt.Errorf("wgcPollContext: %w", err)
	}
	timer := time.NewTimer(10 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("wgcPollWait: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}
