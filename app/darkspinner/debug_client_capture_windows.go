//go:build scenario && windows

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario/control"
	"github.com/darkspinnet/darkspin/server/scenario/desktop"
	"golang.org/x/sys/windows"
)

var debugScreenshotLabelPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,47}$`)

// HTTP workers do not inherit the initialized Wails caller apartment expected
// by the existing capture adapter. Own and balance this initialization on the
// same locked thread; never change an already incompatible apartment mode.
func captureDebugClientWindow(ctx context.Context, owner *scenarioClient, req scenarioEntryFrameRequest) (capture scenarioEntryWindowCapture) {
	capture = scenarioEntryWindowCapture{Backend: scenarioWindowBackend, RunID: req.RunID, Sequence: req.Sequence, StartedAt: time.Now().UTC()}
	defer finishScenarioWindowCapture(&capture)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	err := scenarioEntryContext(ctx)
	if err != nil {
		capture.Reason = "debug capture context: " + err.Error()
		return capture
	}
	for _, proc := range []*windows.LazyProc{entryRoInitialize, entryRoUninitialize} {
		err = proc.Find()
		if err != nil {
			capture.Reason = "debug capture apartment support: " + err.Error()
			return capture
		}
	}
	status := uint32(callScenarioWGCProc(entryRoInitialize, 1))
	if status != 0 && status != 1 {
		capture.Reason = fmt.Sprintf("debug capture apartment HRESULT 0x%08x", status)
		return capture
	}
	defer callScenarioWGCProc(entryRoUninitialize)
	req.IsBackgroundWindowAllowed = true
	req.WindowCaptureTimeout = 3 * time.Second
	return owner.captureEntryWindow(ctx, req)
}

func (e *debugClientManager) Screenshot(ctx context.Context, clientID, label string) (debugScreenshot, error) {
	result := debugScreenshot{ClientID: clientID, Label: label, Source: scenarioWindowBackend,
		Pixels: debugScreenshotPixels{Region: "central-half", Reason: "capture pixels unavailable"}}
	err := scenarioEntryContext(ctx)
	if err != nil {
		return result, fmt.Errorf("captureContext: %w", err)
	}
	if !debugScreenshotLabelPattern.MatchString(label) {
		return result, &control.ScenarioError{Code: control.InvalidRequest, Context: "screenshotLabel", Message: "screenshot label must contain 1 to 48 lowercase letters, digits, underscores or hyphens"}
	}
	e.mutex.Lock()
	client := e.clientsByID[clientID]
	e.mutex.Unlock()
	if client == nil {
		return result, &control.ScenarioError{Code: control.NotFound, Context: "screenshotClient", Message: "client is not owned by this session"}
	}
	err = lockScenarioLiveness(ctx, &client.captureMutex)
	if err != nil {
		return result, fmt.Errorf("captureLock: %w", err)
	}
	defer client.captureMutex.Unlock()
	client.mutex.Lock()
	bridge := client.bridge
	client.mutex.Unlock()
	if bridge != nil && bridge.debugPauseActive() {
		return result, errors.New("owned client debugger pause fences screenshots until recovery")
	}
	if client.screenshotSequence >= scenarioEntryFrameLimit || client.screenshotOutputBytes+scenarioEntryMetadataBytes >= scenarioEntryDiskBytes {
		return result, &control.ScenarioError{Code: control.NotAvailable, Context: "screenshotBudget", Message: "client screenshot output budget is exhausted"}
	}
	client.screenshotSequence++
	client.mutex.Lock()
	owner := client.owner
	result.InstanceID = client.state.InstanceID
	result.ProcessID = client.state.ProcessID
	traceDirectory := filepath.Dir(client.state.TracePath)
	client.mutex.Unlock()
	if owner == nil || traceDirectory == "." {
		return result, &control.ScenarioError{Code: control.NotAvailable, Context: "screenshotOwner", Message: "resumed owned client is unavailable"}
	}
	result.Sequence = client.screenshotSequence
	directory := filepath.Join(traceDirectory, "screenshots")
	err = desktop.CheckAncestors(traceDirectory)
	if err != nil {
		return result, fmt.Errorf("captureParent: %w", err)
	}
	err = os.MkdirAll(directory, 0700)
	if err != nil {
		return result, fmt.Errorf("captureDirectory: %w", err)
	}
	err = desktop.CheckAncestors(directory)
	if err != nil {
		return result, fmt.Errorf("capturePath: %w", err)
	}
	// This method owns its native lock/thread lifetime. Holding client.mutex
	// here would deadlock its nested owned-handle checks.
	capture := captureDebugClientWindow(ctx, owner, scenarioEntryFrameRequest{RunID: e.runID, Sequence: result.Sequence})
	result.StartedAt, result.FinishedAt = capture.StartedAt, capture.FinishedAt
	result.Reason = capture.Reason
	result.Width, result.Height = int(capture.ContentSize.Width), int(capture.ContentSize.Height)
	stem := fmt.Sprintf("%04d-%s", result.Sequence, label)
	var imageErr error
	if capture.IsAvailable {
		if capture.ProcessID != result.ProcessID || capture.RunID != e.runID || capture.Sequence != result.Sequence || capture.ReceivedAt.Before(capture.StartedAt) || capture.FinishedAt.Before(capture.ReceivedAt) {
			imageErr = errors.New("capture ownership or fresh-frame interval is inconsistent")
		} else if result.Width <= 0 || result.Height <= 0 || result.Width > scenarioEntryDimensionLimit || result.Height > scenarioEntryDimensionLimit || len(capture.Pixels) != result.Width*result.Height*4 || len(capture.Pixels) > scenarioEntryPixelLimit {
			imageErr = errors.New("capture pixels or dimensions are inconsistent")
		} else {
			limit := min(int64(scenarioEntryPNGBytes), int64(scenarioEntryDiskBytes)-client.screenshotOutputBytes-scenarioEntryMetadataBytes)
			artifact, size, written, pngErr := writeScenarioEntryPNG(ctx, filepath.Join(directory, stem+".png"), limit, scenarioEntryFrame{Width: result.Width, Height: result.Height, Pixels: capture.Pixels})
			client.screenshotOutputBytes += written
			if pngErr != nil {
				imageErr = fmt.Errorf("capturePNG: %w", pngErr)
			} else {
				result.IsAvailable = true
				result.ImagePath, result.ImageSHA256, result.ImageSize = artifact.Path, artifact.SHA256, size
				digest := sha256.Sum256(capture.Pixels)
				result.PixelsSHA256 = hex.EncodeToString(digest[:])
				result.Pixels = sampleDebugScreenshotPixels(capture.Pixels, result.Width, result.Height)
			}
		}
	}
	if imageErr != nil {
		result.Reason = "owned image persistence failed: " + imageErr.Error()
	}
	metadata := debugScreenshotMetadata(result, capture)
	payload, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return result, fmt.Errorf("captureMarshal: %w", err)
	}
	payload = append(payload, '\n')
	if len(payload) > scenarioEntryMetadataBytes {
		return result, errors.New("screenshot metadata exceeds its byte limit")
	}
	// Persist the completed diagnostic even when the capture's request expired.
	// This bounded finalization does not authorize another native capture.
	finalization, cancelFinalization := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancelFinalization()
	metadataArtifact, written, metadataErr := writeScenarioEntryMetadata(finalization, filepath.Join(directory, stem+".json"), payload)
	client.screenshotOutputBytes += written
	if metadataErr != nil {
		return result, fmt.Errorf("captureMetadata: %w", errors.Join(imageErr, metadataErr))
	}
	result.MetadataPath, result.MetadataSHA256 = metadataArtifact.Path, metadataArtifact.SHA256
	result.IsComplete = result.IsAvailable
	if imageErr != nil {
		return result, fmt.Errorf("captureImage: %w", imageErr)
	}
	if !result.IsAvailable {
		return result, &control.ScenarioError{Code: control.NotAvailable, Context: "ownedCapture", Message: capture.Reason}
	}
	return result, nil
}

type debugScreenshotEvidence struct {
	Schema            uint32                 `json:"schema_version"`
	ClientID          string                 `json:"client_id"`
	InstanceID        string                 `json:"instance_id"`
	Label             string                 `json:"label"`
	PixelsSHA256      string                 `json:"pixels_sha256"`
	Pixels            debugScreenshotPixels  `json:"pixels"`
	IsImageComplete   bool                   `json:"is_image_complete"`
	IsCaptureComplete bool                   `json:"is_capture_complete"`
	CompletenessScope string                 `json:"completeness_scope"`
	Capture           scenarioWindowMetadata `json:"capture"`
}

func debugScreenshotMetadata(result debugScreenshot, capture scenarioEntryWindowCapture) debugScreenshotEvidence {
	window := scenarioWindowMetadata{SchemaVersion: 1, Backend: capture.Backend, CoordinateSpace: "wgc-whole-item", RunID: capture.RunID, Sequence: capture.Sequence,
		ProcessID: capture.ProcessID, ProcessCreatedAt: capture.ProcessCreatedAt, StartedAt: capture.StartedAt, ReceivedAt: capture.ReceivedAt, FinishedAt: capture.FinishedAt,
		CallerApartmentBefore: capture.CallerApartmentBefore, CallerApartmentAfter: capture.CallerApartmentAfter, ApartmentBefore: capture.ApartmentBefore, ApartmentInitialized: capture.ApartmentInitialized, ApartmentAfter: capture.ApartmentAfter,
		RoInitializeHRESULT: capture.RoInitializeHRESULT, IsRoInitializeHRESULTAvailable: capture.IsRoInitializeHRESULTAvailable, Before: capture.Before, After: capture.After,
		ContentSize: capture.ContentSize, IsContentSizeAvailable: capture.IsContentSizeAvailable, Texture: capture.Texture, IsTextureAvailable: capture.IsTextureAvailable,
		SystemRelativeTime: capture.SystemRelativeTime, IsSystemRelativeTimeAvailable: capture.IsSystemRelativeTimeAvailable, OccluderBefore: capture.OccluderBefore, OccluderAfter: capture.OccluderAfter,
		IsFrameAvailable: result.IsAvailable, Reason: result.Reason, IsObservationOnly: true, PNGSHA256: result.ImageSHA256, PNGSize: result.ImageSize, Screen: "unknown", ModalState: "unknown"}
	return debugScreenshotEvidence{Schema: 1, ClientID: result.ClientID, InstanceID: result.InstanceID, Label: result.Label, PixelsSHA256: result.PixelsSHA256, Pixels: result.Pixels, IsImageComplete: result.IsAvailable, IsCaptureComplete: capture.IsAvailable,
		CompletenessScope: "fresh whole-owned-window image and metadata only; no client crop, UI state, gameplay readiness, alpha/HDR, or isolation claim", Capture: window}
}
