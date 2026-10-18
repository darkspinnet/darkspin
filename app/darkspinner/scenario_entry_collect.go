//go:build scenario

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/scenario/desktop"
)

const (
	scenarioEntryAttemptLimit  = 200
	scenarioEntryFrameLimit    = 128
	scenarioEntryPNGBytes      = 4 * 1024 * 1024
	scenarioEntryMetadataBytes = 16 * 1024
	scenarioEntryDiskBytes     = 96 * 1024 * 1024
	scenarioEntryTraceBytes    = 16 * 1024 * 1024
)

type scenarioEntryFrameReader interface {
	CaptureEntryFrame(context.Context, scenarioEntryFrameRequest) (scenarioEntryFrame, error)
}

// The worker invokes this owner serially. No capture context or goroutine is
// retained. Completed artifact references remain available for terminal replies.
type scenarioEntryCollection struct {
	frameReader            scenarioEntryFrameReader
	attemptCount           uint32
	frameCount             uint32
	imageAttemptCount      uint32
	lastDiagnosticSequence uint32
	nextAttemptAt          time.Time
	outputBytes            int64
	isDisabled             bool
	isDirectoryReady       bool
	reason                 string
	enrollment             scenarioEntryEnrollment
	enrollmentDigest       string
	artifacts              []scenario.Artifact
	traceFI                os.FileInfo
	traceOffset            int64
	traceReadBytes         int64
	tracePending           []byte
	isAccountReady         bool
	isProfileDelivered     bool
	calibrationBefore      scenarioEntryCalibration
	calibrationAfter       scenarioEntryCalibration
	occluderBefore         *scenarioEntryOccluder
	occluderAfter          *scenarioEntryOccluder
}

type scenarioEntryEnrollment struct {
	SchemaVersion     uint32    `json:"schema_version"`
	RunID             string    `json:"run_id"`
	ProcessID         uint32    `json:"process_id"`
	ObservedAt        time.Time `json:"observed_at"`
	Screen            string    `json:"screen"`
	Provider          string    `json:"provider"`
	IsOwnedForeground bool      `json:"is_owned_foreground"`
}

func (e *scenarioHost) BindEntryFrames(reader scenarioEntryFrameReader) error {
	if e == nil || reader == nil {
		return errors.New("entry binding requires host and owned client reader")
	}
	if e.entryCollection.frameReader != nil {
		return errors.New("entry frame reader is already bound")
	}
	e.entryCollection.frameReader = reader
	return nil
}

func (e *scenarioHost) EntryFrameArtifacts() []scenario.Artifact {
	return append([]scenario.Artifact(nil), e.entryCollection.artifacts...)
}

func (e *scenarioHost) collectEntryFrame(ctx context.Context) {
	e.collectEntryAttempt(ctx)
	collection := &e.entryCollection
	if collection.enrollmentDigest == "" || collection.attemptCount == 0 || collection.lastDiagnosticSequence == collection.attemptCount {
		return
	}
	collection.lastDiagnosticSequence = collection.attemptCount
	if collection.reason == "entry frame finalized; screen/control/modal interpretation unknown" {
		return
	}
	err := e.persistEntryDiagnostic(ctx)
	if err != nil {
		// Diagnostic persistence never changes the host candidate or propagates
		// an error into readiness. Completed PNG/metadata references stay owned.
		collection.reason = "entry diagnostic could not be finalized"
	}
}

func (e *scenarioHost) collectEntryAttempt(ctx context.Context) {
	collection := &e.entryCollection
	err := scenarioEntryContext(ctx)
	if err != nil {
		collection.reason = "entry observation context ended"
		return
	}
	deadline, isDeadlinePresent := ctx.Deadline()
	if !isDeadlinePresent || time.Until(deadline) < 750*time.Millisecond || collection.isDisabled ||
		collection.frameReader == nil || time.Now().Before(collection.nextAttemptAt) {
		return
	}
	if collection.attemptCount >= scenarioEntryAttemptLimit || collection.imageAttemptCount >= scenarioEntryFrameLimit ||
		collection.outputBytes+scenarioEntryMetadataBytes >= scenarioEntryDiskBytes {
		collection.isDisabled, collection.reason = true, "entry collection limit reached"
		return
	}
	collection.attemptCount++
	collection.calibrationBefore = scenarioEntryCalibration{}
	collection.calibrationAfter = scenarioEntryCalibration{}
	collection.occluderBefore, collection.occluderAfter = nil, nil
	collection.nextAttemptAt = time.Now().Add(3 * time.Second)
	if e.authenticatedSessionID == "" {
		collection.reason = "entry requires pinned authentication"
		return
	}
	err = e.checkEntryAuthentication(ctx)
	if err != nil {
		collection.reason = "entry live authentication unavailable"
		if collection.enrollmentDigest != "" {
			collection.isDisabled = true
		}
		return
	}
	err = e.readEntryEnrollment(ctx)
	if err != nil {
		collection.reason = "entry enrollment unavailable: " + err.Error()
		return
	}
	err = e.readEntryAccountTrace(ctx)
	if err != nil {
		collection.reason = "entry account trace unavailable: " + err.Error()
		return
	}
	if !collection.isAccountReady || !collection.isProfileDelivered {
		collection.reason = "entry account-ready/profile dispatch facts not yet observed"
		return
	}
	frame, err := collection.frameReader.CaptureEntryFrame(ctx, scenarioEntryFrameRequest{
		RunID: e.runID, Sequence: collection.attemptCount,
	})
	collection.calibrationBefore, collection.calibrationAfter = frame.CalibrationBefore, frame.CalibrationAfter
	collection.occluderBefore, collection.occluderAfter = frame.OccluderBefore, frame.OccluderAfter
	// Whole-item evidence is independent of the GDI refusal. Persist it before
	// that early return, with current admission checks and shared image limits.
	if frame.WindowCapture != nil {
		windowErr := e.collectEntryWindow(ctx, *frame.WindowCapture)
		if windowErr != nil {
			// A failed additive diagnostic cannot advance or alter readiness.
			// Its already completed artifacts/output bytes remain registered.
		}
	}
	if err != nil {
		collection.reason = "entry frame unavailable: " + err.Error()
		return
	}
	if collection.imageAttemptCount >= scenarioEntryFrameLimit || collection.isDisabled {
		collection.reason = "entry image/admission limit reached after window capture"
		return
	}
	if frame.RunID != e.runID || frame.Sequence != collection.attemptCount ||
		frame.ProcessID != collection.enrollment.ProcessID || frame.ProcessCreatedAt.IsZero() ||
		frame.StartedAt.Before(collection.enrollment.ObservedAt) || frame.FinishedAt.Before(frame.StartedAt) ||
		!frame.IsForeground || !frame.IsVisible || !frame.IsEnabled {
		collection.reason = "entry frame identity/eligibility mismatch"
		return
	}
	err = e.checkEntryAuthentication(ctx)
	if err != nil {
		collection.isDisabled, collection.reason = true, "entry authentication changed during capture"
		return
	}
	// Re-read enrollment after capture as well: a changed admission never gains
	// a finalized pixel reference through an earlier passing digest.
	err = e.readEntryEnrollment(ctx)
	if err != nil {
		collection.reason = "entry enrollment changed during capture: " + err.Error()
		return
	}
	err = e.persistEntryFrame(ctx, frame)
	if err != nil {
		collection.reason = "entry persistence unavailable: " + err.Error()
		return
	}
	collection.reason = "entry frame finalized; screen/control/modal interpretation unknown"
}

func (e *scenarioHost) collectEntryWindow(ctx context.Context, capture scenarioEntryWindowCapture) error {
	collection := &e.entryCollection
	if capture.Backend != scenarioWindowBackend || capture.RunID != e.runID || capture.Sequence != collection.attemptCount {
		return errors.New("window result request identity mismatch")
	}
	isCorrelated := capture.IsAvailable && capture.ProcessID == collection.enrollment.ProcessID &&
		!capture.ProcessCreatedAt.IsZero() && !capture.ProcessCreatedAt.After(capture.StartedAt) && !capture.StartedAt.Before(collection.enrollment.ObservedAt) &&
		!capture.ReceivedAt.Before(capture.StartedAt) && !capture.FinishedAt.Before(capture.ReceivedAt) &&
		!capture.FinishedAt.After(time.Now().UTC()) && capture.Before.NativeWindow != 0 && capture.Before == capture.After
	if capture.IsAvailable && !isCorrelated {
		capture.IsAvailable, capture.Pixels = false, nil
		capture.Reason = "window identity/eligibility mismatch"
	}
	err := e.checkEntryAuthentication(ctx)
	if err != nil {
		collection.isDisabled = true
		capture.IsAvailable, capture.Pixels, isCorrelated = false, nil, false
		capture.Reason = "window authentication changed during capture"
	}
	enrollmentErr := e.readEntryEnrollment(ctx)
	if enrollmentErr != nil {
		capture.IsAvailable, capture.Pixels, isCorrelated = false, nil, false
		capture.Reason = "window enrollment unavailable after capture"
	}
	persistErr := e.persistEntryWindow(ctx, capture, isCorrelated)
	if persistErr != nil {
		return fmt.Errorf("windowCollectPersist: %w", persistErr)
	}
	if err != nil || enrollmentErr != nil {
		return fmt.Errorf("windowCollectAdmission: %w", errors.Join(err, enrollmentErr))
	}
	return nil
}

func (e *scenarioHost) checkEntryAuthentication(ctx context.Context) error {
	observation, err := e.server.ScenarioBoundObservation(ctx, e.userID, e.authenticatedSessionID, scenario.Authenticated)
	if err != nil {
		return fmt.Errorf("entryLogin: %w", err)
	}
	if observation.Outcome != scenario.Passed || observation.SessionID == "" || observation.SessionID != e.authenticatedSessionID {
		return errors.New("entry pinned login is no longer live")
	}
	err = scenarioEntryContext(ctx)
	if err != nil {
		return fmt.Errorf("entryLoginContext: %w", err)
	}
	return nil
}

func (e *scenarioHost) readEntryEnrollment(ctx context.Context) error {
	path := filepath.Join(e.reportDirectory, "entry-enrollment.json")
	payload, err := readScenarioEntryJSON(ctx, path, 4096)
	if err != nil {
		return fmt.Errorf("entryEnrollmentRead: %w", err)
	}
	digest := sha256.Sum256(payload)
	digestText := hex.EncodeToString(digest[:])
	collection := &e.entryCollection
	if collection.enrollmentDigest != "" && digestText != collection.enrollmentDigest {
		collection.isDisabled = true
		return errors.New("entry enrollment digest changed")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var enrollment scenarioEntryEnrollment
	err = decoder.Decode(&enrollment)
	if err != nil {
		return fmt.Errorf("entryEnrollmentDecode: %w", err)
	}
	var trailing any
	err = decoder.Decode(&trailing)
	if !errors.Is(err, io.EOF) {
		return errors.New("entry enrollment contains trailing JSON")
	}
	if enrollment.SchemaVersion != 1 || enrollment.RunID != e.runID || enrollment.ProcessID == 0 ||
		enrollment.Screen != "arsenal" || enrollment.Provider != "computer-use-sky" || !enrollment.IsOwnedForeground ||
		enrollment.ObservedAt.IsZero() || enrollment.ObservedAt.Location() != time.UTC || enrollment.ObservedAt.After(time.Now().UTC()) {
		return errors.New("entry enrollment lacks the fixed supervised ordinary-screen fact")
	}
	launchPayload, err := readScenarioEntryJSON(ctx, filepath.Join(e.reportDirectory, "launch.json"), 16*1024)
	if err != nil {
		return fmt.Errorf("entryLaunchRead: %w", err)
	}
	var launch struct {
		RunID      string    `json:"run_id"`
		ProcessID  uint32    `json:"process_id"`
		ObservedAt time.Time `json:"observed_at"`
	}
	err = json.Unmarshal(launchPayload, &launch)
	if err != nil {
		return fmt.Errorf("entryLaunchDecode: %w", err)
	}
	if launch.RunID != e.runID || launch.ProcessID != enrollment.ProcessID || launch.ObservedAt.IsZero() || !enrollment.ObservedAt.After(launch.ObservedAt) {
		return errors.New("entry enrollment predates or differs from the owned resumed process")
	}
	liveness, err := e.clientLivenessReader.Liveness(ctx)
	if err != nil {
		return fmt.Errorf("entryEnrollmentAlive: %w", err)
	}
	if liveness.ProcessID != enrollment.ProcessID || !liveness.IsAlive || liveness.IsExited || liveness.ExitCode != nil {
		return errors.New("entry enrollment process is not positively alive")
	}
	if collection.enrollmentDigest == "" {
		collection.enrollment, collection.enrollmentDigest = enrollment, digestText
		collection.artifacts = append(collection.artifacts, scenario.Artifact{Role: "entry_enrollment", Path: path, SHA256: digestText})
	}
	return nil
}

func readScenarioEntryJSON(ctx context.Context, path string, limit int64) ([]byte, error) {
	err := scenarioEntryContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("entryJSONContext: %w", err)
	}
	err = desktop.CheckRegular(path)
	if err != nil {
		return nil, fmt.Errorf("entryJSONPath: %w", err)
	}
	r, err := openScenarioContent(path)
	if err != nil {
		return nil, fmt.Errorf("entryJSONOpen: %w", err)
	}
	payload, readErr := io.ReadAll(io.LimitReader(r, limit+1))
	closeErr := r.Close()
	if readErr != nil || closeErr != nil {
		return nil, fmt.Errorf("entryJSONRead: %w", errors.Join(readErr, closeErr))
	}
	if int64(len(payload)) > limit {
		return nil, errors.New("entry JSON exceeds its byte limit")
	}
	err = scenarioEntryContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("entryJSONFinal: %w", err)
	}
	return payload, nil
}

// The fixed row allowlist reads no callback body/account fields. Partial final
// lines remain bounded owned bytes until a subsequent append completes them.
func (e *scenarioHost) readEntryAccountTrace(ctx context.Context) error {
	path := filepath.Join(e.reportDirectory, "client.jsonl")
	err := desktop.CheckRegular(path)
	if err != nil {
		return fmt.Errorf("entryTracePath: %w", err)
	}
	r, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("entryTraceOpen: %w", err)
	}
	readErr := e.consumeEntryAccountTrace(ctx, r)
	closeErr := r.Close()
	if readErr != nil || closeErr != nil {
		return fmt.Errorf("entryTraceRead: %w", errors.Join(readErr, closeErr))
	}
	return nil
}

func (e *scenarioHost) persistEntryDiagnostic(ctx context.Context) error {
	collection := &e.entryCollection
	err := scenarioEntryContext(ctx)
	if err != nil {
		return fmt.Errorf("entryDiagnosticContext: %w", err)
	}
	if collection.outputBytes+scenarioEntryMetadataBytes > scenarioEntryDiskBytes {
		return errors.New("entry diagnostic output limit reached")
	}
	directory, err := e.entryFrameDirectory()
	if err != nil {
		return fmt.Errorf("entryDiagnosticDirectory: %w", err)
	}
	metadata := struct {
		SchemaVersion           uint32                   `json:"schema_version"`
		RunID                   string                   `json:"run_id"`
		Sequence                uint32                   `json:"sequence"`
		ObservedAt              time.Time                `json:"observed_at"`
		EnrollmentSHA256        string                   `json:"enrollment_sha256"`
		IsObservationOnly       bool                     `json:"is_observation_only"`
		IsFrameAvailable        bool                     `json:"is_frame_available"`
		Reason                  string                   `json:"reason"`
		CalibrationBefore       scenarioEntryCalibration `json:"calibration_before"`
		CalibrationAfter        scenarioEntryCalibration `json:"calibration_after"`
		IsPixelGeometryVerified *bool                    `json:"is_pixel_geometry_verified"`
		OccluderBefore          *scenarioEntryOccluder   `json:"occluder_before"`
		OccluderAfter           *scenarioEntryOccluder   `json:"occluder_after"`
	}{SchemaVersion: 1, RunID: e.runID, Sequence: collection.attemptCount,
		ObservedAt: time.Now().UTC(), EnrollmentSHA256: collection.enrollmentDigest,
		IsObservationOnly: true, IsFrameAvailable: false, Reason: collection.reason,
		CalibrationBefore: collection.calibrationBefore, CalibrationAfter: collection.calibrationAfter,
		OccluderBefore: collection.occluderBefore, OccluderAfter: collection.occluderAfter}
	payload, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("entryDiagnosticEncode: %w", err)
	}
	payload = append(payload, '\n')
	path := filepath.Join(directory, fmt.Sprintf("frame-%04d.json", collection.attemptCount))
	artifact, written, err := writeScenarioEntryMetadata(ctx, path, payload)
	collection.outputBytes += written
	if err != nil {
		return fmt.Errorf("entryDiagnosticWrite: %w", err)
	}
	collection.artifacts = append(collection.artifacts, artifact)
	return nil
}

func (e *scenarioHost) consumeEntryAccountTrace(ctx context.Context, r *os.File) error {
	collection := &e.entryCollection
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("entryTraceStat: %w", err)
	}
	if !fi.Mode().IsRegular() || fi.Size() < collection.traceOffset ||
		collection.traceFI != nil && !os.SameFile(collection.traceFI, fi) {
		collection.isDisabled = true
		return errors.New("entry account trace was replaced or truncated")
	}
	collection.traceFI = fi
	if collection.isAccountReady && collection.isProfileDelivered {
		return nil
	}
	remaining := int64(scenarioEntryTraceBytes) - collection.traceReadBytes
	if remaining <= 0 {
		collection.isDisabled = true
		return errors.New("entry account trace read limit reached")
	}
	err = scenarioEntryContext(ctx)
	if err != nil {
		return fmt.Errorf("entryTraceContext: %w", err)
	}
	offset, err := r.Seek(collection.traceOffset, io.SeekStart)
	if err != nil || offset != collection.traceOffset {
		return fmt.Errorf("entryTraceSeek: %w", errors.Join(err, errors.New("entry trace offset mismatch")))
	}
	readLimit := min(int64(512*1024), remaining)
	payload := make([]byte, int(readLimit))
	count, readErr := r.Read(payload)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return fmt.Errorf("entryTraceChunk: %w", readErr)
	}
	collection.traceOffset += int64(count)
	collection.traceReadBytes += int64(count)
	collection.tracePending = append(collection.tracePending, payload[:count]...)
	for {
		lineEnd := bytes.IndexByte(collection.tracePending, '\n')
		if lineEnd < 0 {
			break
		}
		if lineEnd > 16*1024 {
			collection.isDisabled = true
			return errors.New("entry account trace line limit reached")
		}
		err = scenarioEntryContext(ctx)
		if err != nil {
			return fmt.Errorf("entryTraceLineContext: %w", err)
		}
		var row struct {
			Protocol string          `json:"protocol"`
			Kind     string          `json:"kind"`
			Scalar   json.RawMessage `json:"value"`
			Phase    string          `json:"phase"`
			Callback string          `json:"callback"`
		}
		err = json.Unmarshal(collection.tracePending[:lineEnd], &row)
		if err != nil {
			return fmt.Errorf("entryTraceDecode: %w", err)
		}
		collection.tracePending = collection.tracePending[lineEnd+1:]
		if row.Protocol == "client_state" && row.Kind == "ui_ready" {
			var readiness uint32
			err = json.Unmarshal(row.Scalar, &readiness)
			if err != nil {
				return fmt.Errorf("entryReadyDecode: %w", err)
			}
			if readiness == 1 {
				collection.isAccountReady = true
			}
		}
		if row.Protocol == "client" && row.Kind == "sporenet_callback" && row.Phase == "dispatch" && row.Callback == "spgetplayerprofilecallback" {
			collection.isProfileDelivered = true
		}
	}
	if len(collection.tracePending) > 16*1024 {
		collection.isDisabled = true
		return errors.New("entry account trace partial line limit reached")
	}
	collection.tracePending = append([]byte(nil), collection.tracePending...)
	err = scenarioEntryContext(ctx)
	if err != nil {
		return fmt.Errorf("entryTraceFinal: %w", err)
	}
	return nil
}
