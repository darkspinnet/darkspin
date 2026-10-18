//go:build scenario

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/scenario/desktop"
)

const (
	scenarioEntryPixelLimit     = 16 * 1024 * 1024
	scenarioEntryDimensionLimit = 2048
)

type scenarioEntryFrameRequest struct {
	RunID    string
	Sequence uint32
}

type scenarioEntryRect struct {
	Left   int32 `json:"left"`
	Top    int32 `json:"top"`
	Right  int32 `json:"right"`
	Bottom int32 `json:"bottom"`
}

type scenarioEntryPoint struct {
	X int32 `json:"x"`
	Y int32 `json:"y"`
}

// Availability flags distinguish completed API observations from numeric defaults.
// This owned scalar descriptor is comparable for exact before/after validation.
type scenarioEntryCalibration struct {
	TargetDPI                      uint32             `json:"target_dpi"`
	IsTargetDPIAvailable           bool               `json:"is_target_dpi_available"`
	TargetAwareness                int32              `json:"target_awareness"`
	IsTargetAwarenessAvailable     bool               `json:"is_target_awareness_available"`
	WorkerAwareness                int32              `json:"worker_awareness"`
	IsWorkerAwarenessAvailable     bool               `json:"is_worker_awareness_available"`
	Monitor                        uint64             `json:"monitor"`
	IsMonitorAvailable             bool               `json:"is_monitor_available"`
	MonitorRect                    scenarioEntryRect  `json:"monitor_rect"`
	IsMonitorRectAvailable         bool               `json:"is_monitor_rect_available"`
	MonitorScale                   uint32             `json:"monitor_scale"`
	IsMonitorScaleAvailable        bool               `json:"is_monitor_scale_available"`
	MonitorScaleHRESULT            int32              `json:"monitor_scale_hresult"`
	IsMonitorScaleHRESULTAvailable bool               `json:"is_monitor_scale_hresult_available"`
	ClientTopLeft                  scenarioEntryPoint `json:"client_top_left"`
	IsClientTopLeftAvailable       bool               `json:"is_client_top_left_available"`
	ClientBottomRight              scenarioEntryPoint `json:"client_bottom_right"`
	IsClientBottomRightAvailable   bool               `json:"is_client_bottom_right_available"`
}

// One independently sampled first intersection, never a foreign-window inventory.
// Supplemental query failure cannot make the original occlusion eligible.
type scenarioEntryOccluder struct {
	ObservedAt                      time.Time         `json:"observed_at"`
	ZOrderOrdinal                   uint32            `json:"z_order_ordinal"`
	Window                          uint64            `json:"window"`
	ProcessID                       uint32            `json:"process_id"`
	IsProcessIDAvailable            bool              `json:"is_process_id_available"`
	Class                           string            `json:"class"`
	IsClassAvailable                bool              `json:"is_class_available"`
	Rect                            scenarioEntryRect `json:"rect"`
	IsRectAvailable                 bool              `json:"is_rect_available"`
	IsVisible                       bool              `json:"is_visible"`
	IsIconic                        bool              `json:"is_iconic"`
	IsDWMProcedureAvailabilityKnown bool              `json:"is_dwm_procedure_availability_known"`
	IsDWMProcedureAvailable         bool              `json:"is_dwm_procedure_available"`
	DWMHRESULT                      int32             `json:"dwm_hresult"`
	IsDWMHRESULTAvailable           bool              `json:"is_dwm_hresult_available"`
	CloakFlags                      uint32            `json:"cloak_flags"`
	AreCloakFlagsAvailable          bool              `json:"are_cloak_flags_available"`
	IsCloaked                       *bool             `json:"is_cloaked"`
}

// Pixels are owned RGBA bytes copied only from the private DIB. This type is
// never serialized; collection persists an explicit metadata allowlist.
type scenarioEntryFrame struct {
	RunID             string
	Sequence          uint32
	ProcessID         uint32
	ProcessCreatedAt  time.Time
	Window            uint64
	ClientRect        scenarioEntryRect
	DPI               uint32
	Width             int
	Height            int
	StartedAt         time.Time
	FinishedAt        time.Time
	IsForeground      bool
	IsVisible         bool
	IsEnabled         bool
	Pixels            []byte
	CalibrationBefore scenarioEntryCalibration
	CalibrationAfter  scenarioEntryCalibration
	OccluderBefore    *scenarioEntryOccluder
	OccluderAfter     *scenarioEntryOccluder
	WindowCapture     *scenarioEntryWindowCapture
}

func scenarioEntryContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("entry observation requires context")
	}
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("entryContext: %w", err)
	}
	deadline, isDeadlinePresent := ctx.Deadline()
	if isDeadlinePresent && !time.Now().Before(deadline) {
		return fmt.Errorf("entryWall: %w", context.DeadlineExceeded)
	}
	return nil
}

type scenarioEntryMetadata struct {
	SchemaVersion                      uint32                   `json:"schema_version"`
	RunID                              string                   `json:"run_id"`
	Sequence                           uint32                   `json:"sequence"`
	ProcessID                          uint32                   `json:"process_id"`
	ProcessCreatedAt                   time.Time                `json:"process_created_at"`
	NativeWindow                       uint64                   `json:"native_window"`
	ClientRect                         scenarioEntryRect        `json:"client_rect"`
	DPI                                uint32                   `json:"dpi"`
	Width                              int                      `json:"width"`
	Height                             int                      `json:"height"`
	StartedAt                          time.Time                `json:"started_at"`
	FinishedAt                         time.Time                `json:"finished_at"`
	IsForeground                       bool                     `json:"is_foreground"`
	IsVisible                          bool                     `json:"is_visible"`
	IsWindowEnabled                    bool                     `json:"is_window_enabled"`
	IsNativePopupClear                 bool                     `json:"is_native_popup_clear"`
	AreBeforeAfterOcclusionChecksClear bool                     `json:"are_before_after_occlusion_checks_clear"`
	IsCaptureIntervalOcclusionClear    *bool                    `json:"is_capture_interval_occlusion_clear"`
	IsOwnershipRevalidated             bool                     `json:"is_ownership_revalidated"`
	IsAuthenticationRevalidated        bool                     `json:"is_authentication_revalidated"`
	IsObservationOnly                  bool                     `json:"is_observation_only"`
	PixelFormat                        string                   `json:"pixel_format"`
	EligibilityScope                   string                   `json:"eligibility_scope"`
	EnrollmentSHA256                   string                   `json:"enrollment_sha256"`
	PNGSHA256                          string                   `json:"png_sha256"`
	PNGSize                            int64                    `json:"png_size"`
	Screen                             string                   `json:"screen"`
	ModalState                         string                   `json:"modal_state"`
	ControlBounds                      *scenarioEntryRect       `json:"control_bounds"`
	IsControlEnabled                   *bool                    `json:"is_control_enabled"`
	CalibrationBefore                  scenarioEntryCalibration `json:"calibration_before"`
	CalibrationAfter                   scenarioEntryCalibration `json:"calibration_after"`
	IsPixelGeometryVerified            *bool                    `json:"is_pixel_geometry_verified"`
	OccluderBefore                     *scenarioEntryOccluder   `json:"occluder_before"`
	OccluderAfter                      *scenarioEntryOccluder   `json:"occluder_after"`
}

func (e *scenarioHost) persistEntryFrame(ctx context.Context, frame scenarioEntryFrame) error {
	collection := &e.entryCollection
	if frame.Width <= 0 || frame.Height <= 0 || frame.Width > scenarioEntryDimensionLimit || frame.Height > scenarioEntryDimensionLimit ||
		len(frame.Pixels) != frame.Width*frame.Height*4 || len(frame.Pixels) > scenarioEntryPixelLimit {
		return errors.New("entry frame storage/dimensions are inconsistent")
	}
	err := scenarioEntryContext(ctx)
	if err != nil {
		return fmt.Errorf("entryPersistContext: %w", err)
	}
	directory, err := e.entryFrameDirectory()
	if err != nil {
		return fmt.Errorf("entryFramePath: %w", err)
	}
	path := filepath.Join(directory, fmt.Sprintf("frame-%04d.png", frame.Sequence))
	limit := min(int64(scenarioEntryPNGBytes), int64(scenarioEntryDiskBytes)-collection.outputBytes-scenarioEntryMetadataBytes)
	if limit <= 0 {
		return errors.New("entry output budget is exhausted")
	}
	if collection.imageAttemptCount >= scenarioEntryFrameLimit {
		return errors.New("entry image attempt limit reached")
	}
	// Conservatively count even a failed create/partial encode, so incomplete
	// image files cannot exceed the shared total-image cap.
	collection.imageAttemptCount++
	artifact, size, written, err := writeScenarioEntryPNG(ctx, path, limit, frame)
	collection.outputBytes += written
	if err != nil {
		return fmt.Errorf("entryPNG: %w", err)
	}
	// Register the completed PNG before metadata/context errors can occur.
	collection.artifacts = append(collection.artifacts, artifact)
	collection.frameCount++
	metadata := scenarioEntryMetadata{SchemaVersion: 1, RunID: frame.RunID, Sequence: frame.Sequence,
		ProcessID: frame.ProcessID, ProcessCreatedAt: frame.ProcessCreatedAt, NativeWindow: frame.Window,
		ClientRect: frame.ClientRect, DPI: frame.DPI, Width: frame.Width, Height: frame.Height,
		StartedAt: frame.StartedAt, FinishedAt: frame.FinishedAt, IsForeground: frame.IsForeground,
		IsVisible: frame.IsVisible, IsWindowEnabled: frame.IsEnabled, IsNativePopupClear: true,
		AreBeforeAfterOcclusionChecksClear: true, IsOwnershipRevalidated: true, IsAuthenticationRevalidated: true,
		IsObservationOnly: true, PixelFormat: "RGBA8 private DIB copy", EligibilityScope: "before/after metadata; asynchronous desktop changes during copy remain unknown",
		EnrollmentSHA256: collection.enrollmentDigest,
		PNGSHA256:        artifact.SHA256, PNGSize: size, Screen: "unknown", ModalState: "unknown",
		CalibrationBefore: frame.CalibrationBefore, CalibrationAfter: frame.CalibrationAfter,
		OccluderBefore: frame.OccluderBefore, OccluderAfter: frame.OccluderAfter}
	payload, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("entryMetadataEncode: %w", err)
	}
	payload = append(payload, '\n')
	if len(payload) > scenarioEntryMetadataBytes {
		return errors.New("entry metadata exceeds its byte limit")
	}
	metadataPath := filepath.Join(directory, fmt.Sprintf("frame-%04d.json", frame.Sequence))
	metadataArtifact, written, err := writeScenarioEntryMetadata(ctx, metadataPath, payload)
	collection.outputBytes += written
	if err != nil {
		return fmt.Errorf("entryMetadata: %w", err)
	}
	collection.artifacts = append(collection.artifacts, metadataArtifact)
	return nil
}

func (e *scenarioHost) entryFrameDirectory() (string, error) {
	directory := filepath.Join(e.reportDirectory, "entry-frames")
	err := desktop.CheckAncestors(e.reportDirectory)
	if err != nil {
		return "", fmt.Errorf("entryReportPath: %w", err)
	}
	if !e.entryCollection.isDirectoryReady {
		err = os.Mkdir(directory, 0700)
		if err != nil {
			return "", fmt.Errorf("entryDirectory: %w", err)
		}
		e.entryCollection.isDirectoryReady = true
	}
	err = desktop.CheckAncestors(directory)
	if err != nil {
		return "", fmt.Errorf("entryDirectoryPath: %w", err)
	}
	return directory, nil
}

// The writer owns its request context only for this synchronous encode. It
// retains the original wall deadline and counts even incomplete disk output.
type scenarioEntryArtifactWriter struct {
	done              <-chan struct{}
	deadline          time.Time
	isDeadlinePresent bool
	w                 io.Writer
	hash              hash.Hash
	limit             int64
	written           int64
}

func (e *scenarioEntryArtifactWriter) Write(payload []byte) (int, error) {
	select {
	case <-e.done:
		return 0, fmt.Errorf("entryWriteCanceled: %w", context.Canceled)
	default:
	}
	if e.isDeadlinePresent && !time.Now().Before(e.deadline) {
		return 0, fmt.Errorf("entryWriteDeadline: %w", context.DeadlineExceeded)
	}
	if int64(len(payload)) > e.limit-e.written {
		return 0, errors.New("entry artifact byte limit reached")
	}
	count, err := e.w.Write(payload)
	e.written += int64(count)
	hashed, hashErr := e.hash.Write(payload[:count])
	if hashErr != nil || hashed != count {
		return count, fmt.Errorf("entryWriteHash: %w", errors.Join(hashErr, errors.New("entry artifact hash incomplete")))
	}
	if err != nil {
		return count, fmt.Errorf("entryWriteDisk: %w", err)
	}
	if count != len(payload) {
		return count, errors.New("short entry artifact write")
	}
	return count, nil
}

func writeScenarioEntryPNG(ctx context.Context, path string, limit int64, frame scenarioEntryFrame) (scenario.Artifact, int64, int64, error) {
	err := scenarioEntryContext(ctx)
	if err != nil {
		return scenario.Artifact{}, 0, 0, fmt.Errorf("entryPNGContext: %w", err)
	}
	w, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return scenario.Artifact{}, 0, 0, fmt.Errorf("entryPNGCreate: %w", err)
	}
	deadline, isDeadlinePresent := ctx.Deadline()
	writer := scenarioEntryArtifactWriter{done: ctx.Done(), deadline: deadline, isDeadlinePresent: isDeadlinePresent,
		w: w, hash: sha256.New(), limit: limit}
	img := &image.NRGBA{Pix: frame.Pixels, Stride: frame.Width * 4, Rect: image.Rect(0, 0, frame.Width, frame.Height)}
	encodeErr := png.Encode(&writer, img)
	syncErr := w.Sync()
	closeErr := w.Close()
	if encodeErr != nil || syncErr != nil || closeErr != nil {
		return scenario.Artifact{}, 0, writer.written, fmt.Errorf("entryPNGFinish: %w", errors.Join(encodeErr, syncErr, closeErr))
	}
	return scenario.Artifact{Role: "entry_frame", Path: path, SHA256: hex.EncodeToString(writer.hash.Sum(nil))}, writer.written, writer.written, nil
}

func writeScenarioEntryMetadata(ctx context.Context, path string, payload []byte) (scenario.Artifact, int64, error) {
	err := scenarioEntryContext(ctx)
	if err != nil {
		return scenario.Artifact{}, 0, fmt.Errorf("entryMetadataContext: %w", err)
	}
	w, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return scenario.Artifact{}, 0, fmt.Errorf("entryMetadataCreate: %w", err)
	}
	deadline, isDeadlinePresent := ctx.Deadline()
	writer := scenarioEntryArtifactWriter{done: ctx.Done(), deadline: deadline, isDeadlinePresent: isDeadlinePresent,
		w: w, hash: sha256.New(), limit: scenarioEntryMetadataBytes}
	count, writeErr := writer.Write(payload)
	syncErr := w.Sync()
	closeErr := w.Close()
	if writeErr != nil || count != len(payload) || syncErr != nil || closeErr != nil {
		return scenario.Artifact{}, writer.written, fmt.Errorf("entryMetadataFinish: %w", errors.Join(writeErr, syncErr, closeErr, errors.New("entry metadata not finalized")))
	}
	return scenario.Artifact{Role: "entry_frame_metadata", Path: path, SHA256: hex.EncodeToString(writer.hash.Sum(nil))}, writer.written, nil
}
