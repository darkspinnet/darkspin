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

type scenarioWGCBox struct {
	Left, Top, Front, Right, Bottom, Back uint32
}

type scenarioWGCMapped struct {
	Data       uintptr
	RowPitch   uint32
	DepthPitch uint32
}

func (e *scenarioWGCAttempt) receive(ctx context.Context, capture *scenarioEntryWindowCapture) error {
	for {
		err := scenarioEntryContext(ctx)
		if err != nil {
			return fmt.Errorf("wgcReceiveContext: %w", err)
		}
		var frame uintptr
		status := callScenarioWGC(e.pool, 7, uintptr(unsafe.Pointer(&frame)))
		e.retain(frame)
		// A non-null output on failure is still owned and must be closed.
		e.frame = frame
		err = scenarioWGCHRESULT(status)
		if err != nil {
			return fmt.Errorf("wgcReceive: %w", err)
		}
		if frame != 0 {
			capture.ReceivedAt = time.Now().UTC()
			break
		}
		err = waitScenarioWGC(ctx)
		if err != nil {
			return fmt.Errorf("wgcReceiveWait: %w", err)
		}
	}
	err := scenarioEntryContext(ctx)
	if err != nil {
		return fmt.Errorf("wgcClockContext: %w", err)
	}
	status := callScenarioWGC(e.frame, 7, uintptr(unsafe.Pointer(&capture.SystemRelativeTime)))
	err = scenarioWGCHRESULT(status)
	if err != nil {
		return fmt.Errorf("wgcFrameClock: %w", err)
	}
	capture.IsSystemRelativeTimeAvailable = true // Raw100ns duration, never UTC.
	err = scenarioEntryContext(ctx)
	if err != nil {
		return fmt.Errorf("wgcContentContext: %w", err)
	}
	status = callScenarioWGC(e.frame, 8, uintptr(unsafe.Pointer(&capture.ContentSize)))
	err = scenarioWGCHRESULT(status)
	if err != nil {
		return fmt.Errorf("wgcContentSize: %w", err)
	}
	capture.IsContentSizeAvailable = true
	err = validateScenarioWindowSize(capture.ContentSize)
	if err != nil {
		return fmt.Errorf("wgcContentBounds: %w", err)
	}
	if capture.ContentSize != capture.Before.ItemSize {
		return errors.New("WGC frame content size differs from initial item")
	}
	return nil
}

func (e *scenarioWGCAttempt) copySurface(ctx context.Context, capture *scenarioEntryWindowCapture) ([]byte, error) {
	err := scenarioEntryContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("wgcSurfaceContext: %w", err)
	}
	var surface uintptr
	status := callScenarioWGC(e.frame, 6, uintptr(unsafe.Pointer(&surface)))
	e.retain(surface)
	err = scenarioWGCHRESULT(status)
	if err != nil {
		return nil, fmt.Errorf("wgcSurface: %w", err)
	}
	if surface == 0 {
		return nil, errors.New("WGC frame surface is missing")
	}
	access, err := e.query(ctx, surface, &entryIIDDXGIAccess)
	if err != nil {
		return nil, fmt.Errorf("wgcSurfaceAccess: %w", err)
	}
	var texture uintptr
	status = callScenarioWGC(access, 3, uintptr(unsafe.Pointer(&entryIIDTexture)), uintptr(unsafe.Pointer(&texture)))
	e.retain(texture)
	err = scenarioWGCHRESULT(status)
	if err != nil {
		return nil, fmt.Errorf("wgcTexture: %w", err)
	}
	if texture == 0 {
		return nil, errors.New("WGC surface texture is missing")
	}
	err = scenarioEntryContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("wgcDescriptorContext: %w", err)
	}
	callScenarioWGC(texture, 10, uintptr(unsafe.Pointer(&capture.Texture))) // GetDesc is void.
	capture.IsTextureAvailable = true
	desc := capture.Texture
	width, height := uint32(capture.ContentSize.Width), uint32(capture.ContentSize.Height)
	if desc.Width == 0 || desc.Height == 0 || desc.Width > scenarioEntryDimensionLimit || desc.Height > scenarioEntryDimensionLimit ||
		uint64(desc.Width)*uint64(desc.Height)*4 > scenarioEntryPixelLimit || desc.Format != 87 || desc.MipLevels != 1 || desc.ArraySize != 1 ||
		desc.SampleCount != 1 || desc.SampleQuality != 0 || width > desc.Width || height > desc.Height {
		return nil, errors.New("WGC texture format/storage/dimensions are unsupported")
	}
	// Allocate only the positive bounded content region. This is a copy of the
	// entire frame ContentSize, not a client crop or scaling operation.
	stagingDesc := scenarioWindowTexture{Width: width, Height: height, MipLevels: 1, ArraySize: 1, Format: 87,
		SampleCount: 1, Usage: 3, CPUAccessFlags: 0x20000}
	err = scenarioEntryContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("wgcStagingContext: %w", err)
	}
	var staging uintptr
	status = callScenarioWGC(e.device, 5, uintptr(unsafe.Pointer(&stagingDesc)), 0, uintptr(unsafe.Pointer(&staging)))
	runtime.KeepAlive(&stagingDesc)
	e.retain(staging)
	err = scenarioWGCHRESULT(status)
	if err != nil {
		return nil, fmt.Errorf("wgcStaging: %w", err)
	}
	if staging == 0 {
		return nil, errors.New("WGC private staging texture is missing")
	}
	err = scenarioEntryContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("wgcCopyContext: %w", err)
	}
	box := scenarioWGCBox{Right: width, Bottom: height, Back: 1}
	callScenarioWGC(e.deviceContext, 46, staging, 0, 0, 0, 0, texture, 0, uintptr(unsafe.Pointer(&box)))
	runtime.KeepAlive(&box)
	err = scenarioEntryContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("wgcDeviceStatusContext: %w", err)
	}
	status = callScenarioWGC(e.device, 39)
	err = scenarioWGCHRESULT(status)
	if err != nil {
		return nil, fmt.Errorf("wgcCopyDevice: %w", err)
	}
	mapped, err := e.mapSurface(ctx, staging)
	if err != nil {
		return nil, fmt.Errorf("wgcMapSurface: %w", err)
	}
	defer unmapScenarioWGCSurface(e.deviceContext, staging)
	rowBytes := uint64(width) * 4
	span := uint64(mapped.RowPitch) * uint64(height)
	if mapped.Data == 0 || uint64(mapped.RowPitch) < rowBytes || span == 0 || span > scenarioEntryPixelLimit ||
		uint64(mapped.Data)+span > uint64(^uintptr(0)) {
		return nil, errors.New("WGC mapped row pitch/span is unsupported")
	}
	pixels := make([]byte, int(rowBytes*uint64(height)))
	for row := uint32(0); row < height; row++ {
		err = scenarioEntryContext(ctx)
		if err != nil {
			return nil, fmt.Errorf("wgcRowContext: %w", err)
		}
		source := unsafe.Slice((*byte)(unsafe.Pointer(mapped.Data+uintptr(row)*uintptr(mapped.RowPitch))), int(rowBytes))
		destination := pixels[int(uint64(row)*rowBytes):int(uint64(row+1)*rowBytes)]
		for offset := 0; offset < len(destination); offset += 4 {
			destination[offset], destination[offset+1], destination[offset+2], destination[offset+3] = source[offset+2], source[offset+1], source[offset], source[offset+3]
		}
	}
	err = scenarioEntryContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("wgcReadStatusContext: %w", err)
	}
	status = callScenarioWGC(e.device, 39)
	err = scenarioWGCHRESULT(status)
	if err != nil {
		return nil, fmt.Errorf("wgcReadDevice: %w", err)
	}
	return pixels, nil
}

func unmapScenarioWGCSurface(deviceContext, staging uintptr) {
	callScenarioWGC(deviceContext, 15, staging, 0) // Unmap is void; no error slot.
}

func (e *scenarioWGCAttempt) mapSurface(ctx context.Context, staging uintptr) (scenarioWGCMapped, error) {
	for {
		err := scenarioEntryContext(ctx)
		if err != nil {
			return scenarioWGCMapped{}, fmt.Errorf("wgcMapContext: %w", err)
		}
		var mapped scenarioWGCMapped
		status := callScenarioWGC(e.deviceContext, 14, staging, 0, 1, 0x100000, uintptr(unsafe.Pointer(&mapped)))
		if uint32(status) == 0x887a000a { // DXGI_ERROR_WAS_STILL_DRAWING.
			err = waitScenarioWGC(ctx)
			if err != nil {
				return scenarioWGCMapped{}, fmt.Errorf("wgcMapWait: %w", err)
			}
			continue
		}
		err = scenarioWGCHRESULT(status)
		if err != nil {
			return scenarioWGCMapped{}, fmt.Errorf("wgcMap: %w", err)
		}
		return mapped, nil
	}
}
