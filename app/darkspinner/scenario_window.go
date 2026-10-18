//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

const scenarioWindowBackend = "windows-graphics-capture-owned-window"

type scenarioWindowSize struct {
	Width  int32 `json:"width"`
	Height int32 `json:"height"`
}

// Enum outputs are meaningful only for the documented successful query. An
// exact CO_E_NOTINITIALIZED records its HRESULT while leaving enums unknown.
type scenarioWindowApartment struct {
	ThreadID                              uint32 `json:"thread_id"`
	IsThreadIDAvailable                   bool   `json:"is_thread_id_available"`
	HRESULT                               int32  `json:"hresult"`
	IsHRESULTAvailable                    bool   `json:"is_hresult_available"`
	ApartmentType                         int32  `json:"apartment_type"`
	IsApartmentTypeAvailable              bool   `json:"is_apartment_type_available"`
	Qualifier                             int32  `json:"qualifier"`
	IsQualifierAvailable                  bool   `json:"is_qualifier_available"`
	IsApartmentProcedureAvailabilityKnown bool   `json:"is_apartment_procedure_availability_known"`
	IsApartmentProcedureAvailable         bool   `json:"is_apartment_procedure_available"`
	IsThreadProcedureAvailabilityKnown    bool   `json:"is_thread_procedure_availability_known"`
	IsThreadProcedureAvailable            bool   `json:"is_thread_procedure_available"`
}

type scenarioWindowObservation struct {
	NativeWindow          uint64                   `json:"native_window"`
	WindowRect            scenarioEntryRect        `json:"window_rect"`
	IsWindowRectAvailable bool                     `json:"is_window_rect_available"`
	Calibration           scenarioEntryCalibration `json:"calibration"`
	ItemSize              scenarioWindowSize       `json:"item_size"`
	IsItemSizeAvailable   bool                     `json:"is_item_size_available"`
}

// The texture descriptor mirrors the documented fixed D3D11_TEXTURE2D_DESC.
type scenarioWindowTexture struct {
	Width          uint32 `json:"width"`
	Height         uint32 `json:"height"`
	MipLevels      uint32 `json:"mip_levels"`
	ArraySize      uint32 `json:"array_size"`
	Format         uint32 `json:"format"`
	SampleCount    uint32 `json:"sample_count"`
	SampleQuality  uint32 `json:"sample_quality"`
	Usage          uint32 `json:"usage"`
	BindFlags      uint32 `json:"bind_flags"`
	CPUAccessFlags uint32 `json:"cpu_access_flags"`
	MiscFlags      uint32 `json:"misc_flags"`
}

type scenarioEntryWindowCapture struct {
	Backend                        string
	RunID                          string
	Sequence                       uint32
	ProcessID                      uint32
	ProcessCreatedAt               time.Time
	StartedAt                      time.Time
	ReceivedAt                     time.Time
	FinishedAt                     time.Time
	CallerApartmentBefore          scenarioWindowApartment
	CallerApartmentAfter           scenarioWindowApartment
	ApartmentBefore                scenarioWindowApartment
	ApartmentInitialized           scenarioWindowApartment
	ApartmentAfter                 scenarioWindowApartment
	RoInitializeHRESULT            int32
	IsRoInitializeHRESULTAvailable bool
	Before                         scenarioWindowObservation
	After                          scenarioWindowObservation
	ContentSize                    scenarioWindowSize
	IsContentSizeAvailable         bool
	Texture                        scenarioWindowTexture
	IsTextureAvailable             bool
	SystemRelativeTime             int64
	IsSystemRelativeTimeAvailable  bool
	OccluderBefore                 *scenarioEntryOccluder
	OccluderAfter                  *scenarioEntryOccluder
	IsAvailable                    bool
	Reason                         string
	Pixels                         []byte
}

// Explicit diagnostic allowlist. Nullable fields intentionally make no geometry,
// isolation, native-clock, semantic or input claim for a whole-item image.
type scenarioWindowMetadata struct {
	SchemaVersion                   uint32                    `json:"schema_version"`
	Backend                         string                    `json:"backend"`
	CoordinateSpace                 string                    `json:"coordinate_space"`
	RunID                           string                    `json:"run_id"`
	Sequence                        uint32                    `json:"sequence"`
	ProcessID                       uint32                    `json:"process_id"`
	ProcessCreatedAt                time.Time                 `json:"process_created_at"`
	StartedAt                       time.Time                 `json:"started_at"`
	ReceivedAt                      time.Time                 `json:"received_at"`
	FinishedAt                      time.Time                 `json:"finished_at"`
	CallerApartmentBefore           scenarioWindowApartment   `json:"caller_apartment_before"`
	CallerApartmentAfter            scenarioWindowApartment   `json:"caller_apartment_after"`
	ApartmentBefore                 scenarioWindowApartment   `json:"apartment_before"`
	ApartmentInitialized            scenarioWindowApartment   `json:"apartment_initialized"`
	ApartmentAfter                  scenarioWindowApartment   `json:"apartment_after"`
	RoInitializeHRESULT             int32                     `json:"ro_initialize_hresult"`
	IsRoInitializeHRESULTAvailable  bool                      `json:"is_ro_initialize_hresult_available"`
	Before                          scenarioWindowObservation `json:"before"`
	After                           scenarioWindowObservation `json:"after"`
	ContentSize                     scenarioWindowSize        `json:"content_size"`
	IsContentSizeAvailable          bool                      `json:"is_content_size_available"`
	Texture                         scenarioWindowTexture     `json:"texture"`
	IsTextureAvailable              bool                      `json:"is_texture_available"`
	SystemRelativeTime              int64                     `json:"system_relative_time"`
	IsSystemRelativeTimeAvailable   bool                      `json:"is_system_relative_time_available"`
	OccluderBefore                  *scenarioEntryOccluder    `json:"occluder_before"`
	OccluderAfter                   *scenarioEntryOccluder    `json:"occluder_after"`
	IsFrameAvailable                bool                      `json:"is_frame_available"`
	Reason                          string                    `json:"reason"`
	EnrollmentSHA256                string                    `json:"enrollment_sha256"`
	AuthenticationSessionID         string                    `json:"authentication_session_id"`
	IsAuthenticationRevalidated     bool                      `json:"is_authentication_revalidated"`
	IsObservationOnly               bool                      `json:"is_observation_only"`
	PNGSHA256                       string                    `json:"png_sha256"`
	PNGSize                         int64                     `json:"png_size"`
	IsClientCropMappingVerified     *bool                     `json:"is_client_crop_mapping_verified"`
	IsPixelGeometryVerified         *bool                     `json:"is_pixel_geometry_verified"`
	IsOverlayIsolationVerified      *bool                     `json:"is_overlay_isolation_verified"`
	IsCaptureIntervalOcclusionClear *bool                     `json:"is_capture_interval_occlusion_clear"`
	IsAlphaInterpretationVerified   *bool                     `json:"is_alpha_interpretation_verified"`
	IsHDRInterpretationVerified     *bool                     `json:"is_hdr_interpretation_verified"`
	IsFrameClockCalibrated          *bool                     `json:"is_frame_clock_calibrated"`
	Screen                          string                    `json:"screen"`
	ModalState                      string                    `json:"modal_state"`
	ControlBounds                   *scenarioEntryRect        `json:"control_bounds"`
	IsControlEnabled                *bool                     `json:"is_control_enabled"`
	HeroIdentity                    *string                   `json:"hero_identity"`
}

func (e *scenarioHost) persistEntryWindow(ctx context.Context, capture scenarioEntryWindowCapture, isAuthenticated bool) error {
	collection := &e.entryCollection
	err := scenarioEntryContext(ctx)
	if err != nil {
		return fmt.Errorf("windowPersistContext: %w", err)
	}
	if collection.outputBytes+scenarioEntryMetadataBytes > scenarioEntryDiskBytes {
		return errors.New("window diagnostic output limit reached")
	}
	directory, err := e.entryFrameDirectory()
	if err != nil {
		return fmt.Errorf("windowDirectory: %w", err)
	}
	metadata := scenarioWindowMetadata{SchemaVersion: 1, Backend: scenarioWindowBackend, CoordinateSpace: "wgc-whole-item",
		RunID: capture.RunID, Sequence: capture.Sequence, ProcessID: capture.ProcessID, ProcessCreatedAt: capture.ProcessCreatedAt,
		StartedAt: capture.StartedAt, ReceivedAt: capture.ReceivedAt, FinishedAt: capture.FinishedAt,
		CallerApartmentBefore: capture.CallerApartmentBefore, CallerApartmentAfter: capture.CallerApartmentAfter,
		ApartmentBefore: capture.ApartmentBefore, ApartmentInitialized: capture.ApartmentInitialized, ApartmentAfter: capture.ApartmentAfter,
		RoInitializeHRESULT: capture.RoInitializeHRESULT, IsRoInitializeHRESULTAvailable: capture.IsRoInitializeHRESULTAvailable,
		Before: capture.Before, After: capture.After, ContentSize: capture.ContentSize, IsContentSizeAvailable: capture.IsContentSizeAvailable,
		Texture: capture.Texture, IsTextureAvailable: capture.IsTextureAvailable, SystemRelativeTime: capture.SystemRelativeTime,
		IsSystemRelativeTimeAvailable: capture.IsSystemRelativeTimeAvailable, OccluderBefore: capture.OccluderBefore, OccluderAfter: capture.OccluderAfter,
		IsFrameAvailable: capture.IsAvailable && isAuthenticated, Reason: capture.Reason, EnrollmentSHA256: collection.enrollmentDigest,
		IsAuthenticationRevalidated: isAuthenticated, IsObservationOnly: true, Screen: "unknown", ModalState: "unknown"}
	if isAuthenticated {
		metadata.AuthenticationSessionID = e.authenticatedSessionID
	}
	var imageErr error
	if metadata.IsFrameAvailable {
		if collection.imageAttemptCount >= scenarioEntryFrameLimit {
			return errors.New("window image count limit reached")
		}
		width, height := int(capture.ContentSize.Width), int(capture.ContentSize.Height)
		if width <= 0 || height <= 0 || width > scenarioEntryDimensionLimit || height > scenarioEntryDimensionLimit ||
			len(capture.Pixels) != width*height*4 || len(capture.Pixels) > scenarioEntryPixelLimit {
			return errors.New("window copied pixels are inconsistent")
		}
		limit := min(int64(scenarioEntryPNGBytes), int64(scenarioEntryDiskBytes)-collection.outputBytes-scenarioEntryMetadataBytes)
		collection.imageAttemptCount++
		artifact, size, written, pngErr := writeScenarioEntryPNG(ctx, filepath.Join(directory, fmt.Sprintf("window-%04d.png", capture.Sequence)), limit,
			scenarioEntryFrame{Width: width, Height: height, Pixels: capture.Pixels})
		collection.outputBytes += written
		if pngErr != nil {
			metadata.IsFrameAvailable, metadata.Reason = false, "window PNG unavailable: "+pngErr.Error()
			imageErr = fmt.Errorf("windowPNG: %w", pngErr)
		} else {
			artifact.Role = "entry_window_wgc"
			collection.artifacts = append(collection.artifacts, artifact)
			collection.frameCount++
			metadata.PNGSHA256, metadata.PNGSize = artifact.SHA256, size
		}
	}
	payload, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("windowMetadataEncode: %w", err)
	}
	payload = append(payload, '\n')
	if len(payload) > scenarioEntryMetadataBytes {
		return errors.New("window metadata exceeds byte limit")
	}
	artifact, written, err := writeScenarioEntryMetadata(ctx, filepath.Join(directory, fmt.Sprintf("window-%04d.json", capture.Sequence)), payload)
	collection.outputBytes += written
	if err != nil {
		return fmt.Errorf("windowMetadataWrite: %w", err)
	}
	artifact.Role = "entry_window_wgc_metadata"
	collection.artifacts = append(collection.artifacts, artifact)
	if imageErr != nil {
		return fmt.Errorf("windowImageFinalize: %w", imageErr)
	}
	return nil
}
