//go:build scenario && windows

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type controlPipe struct {
	isPauseUnsafe   bool
	pauseID         string
	pauseUntil      time.Time
	mutex           sync.Mutex
	cancel          context.CancelFunc
	r               *os.File
	handle          windows.Handle
	last            controlEnvelope
	lastAt          time.Time
	isConnected     bool
	failure         string
	failureCause    error
	writers         chan struct{}
	pending         *controlPending
	requestSequence uint64
	runID           string
	clientID        string
	generation      string
}

type controlPending struct {
	requestID    string
	operation    string
	sequence     uint64
	isDispatched bool
	deliveries   chan controlDelivery
	command      controlCommand
}

type controlDelivery struct {
	result controlCommandResult
	err    error
}

// controlRequestError retains receipt identity when cancellation/disconnect makes
// an attempted command's native outcome unknown. It never authorizes replay.
type controlRequestError struct {
	RequestID    string
	Operation    string
	IsDispatched bool
	Code         string
	Message      string
	Cause        error
}

func (e *controlRequestError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}
func (e *controlRequestError) Unwrap() error { return e.Cause }

// startControlPipe creates a private single-instance observation endpoint.
// The caller must supply the PID of its suspended owned client before resume.
func startControlPipe(ctx context.Context, pipeName, token string, expectedPID uint32, runID, clientID string) (*controlPipe, error) {
	if expectedPID == 0 || runID == "" || clientID == "" || len(token) < 32 || !strings.HasPrefix(pipeName, `\\.\pipe\darkspin-`) {
		return nil, fmt.Errorf("invalid control pipe identity")
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("pipeUser: %w", err)
	}
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return nil, fmt.Errorf("pipeACL: %w", err)
	}
	name, err := windows.UTF16PtrFromString(pipeName)
	if err != nil {
		return nil, fmt.Errorf("pipeName: %w", err)
	}
	attribute := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: descriptor}
	handle, err := windows.CreateNamedPipe(name, windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_OVERLAPPED|windows.FILE_FLAG_FIRST_PIPE_INSTANCE, windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS, 1, controlLimit+4, controlLimit+4, 0, &attribute)
	if err != nil {
		return nil, fmt.Errorf("pipeCreate: %w", err)
	}
	lifetime, cancel := context.WithCancel(ctx)
	pipe := &controlPipe{handle: handle, cancel: cancel, writers: make(chan struct{}, 1), runID: runID, clientID: clientID}
	go pipe.run(lifetime, token, expectedPID, runID, clientID)
	return pipe, nil
}

func (e *controlPipe) State() (controlEnvelope, time.Time, bool, string) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	// State is immutable after publication; callers receive their own copy.
	envelope := e.last
	if envelope.State != nil {
		state := *envelope.State
		envelope.State = &state
	}
	return envelope, e.lastAt, e.isConnected, e.failure
}

// Request submits at most once. A timeout after a write attempt reports an
// unknown outcome; the caller must inspect a fresh state rather than replay.
func (e *controlPipe) Request(ctx context.Context, operation string, command controlCommand) (controlCommandResult, error) {
	if (command.HeroNoun != 0 && operation != "editor.open_creature") || (operation == "editor.open_creature" && command.HeroNoun == 0) {
		return controlCommandResult{}, &controlCommandFailure{Code: "invalid_request", Message: "hero_noun is required only for editor.open_creature"}
	}
	if command.ExpectedOwnerGeneration != 0 && operation != "ui.screen_set" {
		return controlCommandResult{}, &controlCommandFailure{Code: "invalid_request", Message: "expected_owner_generation requires ui.screen_set"}
	}
	if command.Screen != "" && operation != "ui.screen_set" {
		return controlCommandResult{}, &controlCommandFailure{Code: "invalid_request", Message: "screen requires ui.screen_set"}
	}
	if command.Text != "" && operation != "window.text" {
		return controlCommandResult{}, &controlCommandFailure{Code: "invalid_request", Message: "text requires window.text"}
	}
	if command.HeroSlot != nil && operation != "gameplay.select_hero" {
		return controlCommandResult{}, &controlCommandFailure{Code: "invalid_request", Message: "hero_slot requires gameplay.select_hero"}
	}
	if command.HoldMS != 0 && operation != "gameplay.hold" {
		return controlCommandResult{}, &controlCommandFailure{Code: "invalid_request", Message: "hold_ms requires gameplay.hold"}
	}
	if operation == "gameplay.hold" && (command.HoldMS == 0 || command.HoldMS > 1200) {
		return controlCommandResult{}, &controlCommandFailure{Code: "invalid_request", Message: "gameplay hold requires 1 through 1200 milliseconds"}
	}
	if command.WorldPoint != nil && operation != "gameplay.project" && operation != "gameplay.click" && operation != "gameplay.hold" && operation != "gameplay.cast_active" {
		return controlCommandResult{}, &controlCommandFailure{Code: "invalid_request", Message: "world point is supported only by gameplay projection"}
	}
	switch operation {
	case "ui.screen_set":
		if command.ExpectedOwnerGeneration == 0 || !isDebugScreen(command.Screen) || command.Text != "" || command.HeroSlot != nil || command.HoldMS != 0 || command.WorldPoint != nil || command.X != 0 || command.Y != 0 || command.Button != "" || command.Key != "" || command.Scene != "" || command.IsIntroSkipped != nil || command.IsCinematicSkipped != nil {
			return controlCommandResult{}, &controlCommandFailure{Code: "invalid_request", Message: "ui.screen_set requires only one exact supported screen"}
		}
	case "ui.screen_state":
		if command != (controlCommand{}) {
			return controlCommandResult{}, &controlCommandFailure{Code: "invalid_request", Message: "ui.screen_state requires an empty command"}
		}
	case "window.text":
		if len(command.Text) == 0 || len(command.Text) > 256 || command.HeroSlot != nil || command.HoldMS != 0 || command.WorldPoint != nil || command.X != 0 || command.Y != 0 || command.Button != "" || command.Key != "" || command.Scene != "" || command.IsIntroSkipped != nil || command.IsCinematicSkipped != nil {
			return controlCommandResult{}, &controlCommandFailure{Code: "invalid_request", Message: "window.text requires only 1 through 256 printable ASCII characters"}
		}
		for _, character := range command.Text {
			if character < 32 || character > 126 {
				return controlCommandResult{}, &controlCommandFailure{Code: "invalid_request", Message: "window.text requires printable ASCII characters"}
			}
		}
	case "gameplay.select_hero":
		if command.HeroSlot == nil || *command.HeroSlot > 2 || command.HoldMS != 0 || command.WorldPoint != nil || command.X != 0 || command.Y != 0 || command.Button != "" || command.Key != "" || command.Scene != "" || command.IsIntroSkipped != nil || command.IsCinematicSkipped != nil {
			return controlCommandResult{}, &controlCommandFailure{Code: "invalid_request", Message: "gameplay.select_hero requires only hero_slot from 0 through 2"}
		}
	case "gameplay.inspect", "gameplay.project", "gameplay.click", "gameplay.hold", "gameplay.cast_active":
		if command.X != 0 || command.Y != 0 || command.Button != "" || command.Key != "" || command.Scene != "" || command.IsIntroSkipped != nil || command.IsCinematicSkipped != nil || operation != "gameplay.inspect" && (command.WorldPoint == nil || !isDebugWorldPointFinite(*command.WorldPoint)) {
			return controlCommandResult{}, &controlCommandFailure{Code: "invalid_request", Message: "gameplay observation requires only an optional finite projection point"}
		}
	case "window.inspect", "window.ready", "mission.map_state", "mission.start", "mission.deployment_state", "mission.continue", "mission.beam_out_state", "mission.beam_out", "mission.campaign", "mission.previous", "mission.next", "mission.arena_lobby", "mission.accept_arena", "editor.cancel", "editor.hierarchy", "editor.open_creature":
		if command.X != 0 || command.Y != 0 || command.Button != "" || command.Key != "" || command.Scene != "" || command.IsIntroSkipped != nil || command.IsCinematicSkipped != nil {
			return controlCommandResult{}, &controlCommandFailure{Code: "invalid_request", Message: "observational window commands require an empty command"}
		}
	case "window.click":
		if command.X < 0 || command.Y < 0 || command.X > 32767 || command.Y > 32767 || command.Button != "" && command.Button != "left" && command.Button != "right" || command.Key != "" || command.Scene != "" || command.IsIntroSkipped != nil || command.IsCinematicSkipped != nil {
			return controlCommandResult{}, &controlCommandFailure{Code: "invalid_request", Message: "window.click requires coordinates from 0 to 32767 and a left or right button"}
		}
	case "cinematic.configure":
		if command.X != 0 || command.Y != 0 || command.Button != "" || command.Key != "" || command.Scene != "" || command.IsIntroSkipped == nil && command.IsCinematicSkipped == nil {
			return controlCommandResult{}, &controlCommandFailure{Code: "invalid_request", Message: "cinematic.configure requires at least one skip flag and no window input arguments"}
		}
	case "scene.set", "scene.ready":
		if command.X != 0 || command.Y != 0 || command.Button != "" || command.Key != "" || command.IsIntroSkipped != nil || command.IsCinematicSkipped != nil || !isDebugShipScene(command.Scene) {
			return controlCommandResult{}, &controlCommandFailure{Code: "invalid_request", Message: "scene.set requires one supported ship scene and no other input"}
		}
	case "window.press":
		if command.X != 0 || command.Y != 0 || command.Button != "" || command.Scene != "" || command.IsIntroSkipped != nil || command.IsCinematicSkipped != nil || command.Key != "Escape" && command.Key != "Enter" {
			return controlCommandResult{}, &controlCommandFailure{Code: "invalid_request", Message: "window.press requires Escape or Enter and no other input"}
		}
	default:
		return controlCommandResult{}, &controlCommandFailure{Code: "unsupported_operation", Message: "operation is not supported by the private control pipe"}
	}
	requestContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err := requestContext.Err()
	if err != nil {
		return controlCommandResult{}, fmt.Errorf("commandContext: %w", err)
	}
	deadline, isDeadline := requestContext.Deadline()
	if !isDeadline {
		return controlCommandResult{}, errors.New("command deadline is required")
	}
	e.mutex.Lock()
	if e.pauseID != "" {
		e.mutex.Unlock()
		return controlCommandResult{}, &controlRequestError{Operation: operation, Code: "client_paused", Message: "owned client debugger pause fences native commands until recovery"}
	}
	if !e.isConnected || e.r == nil || e.generation == "" {
		e.mutex.Unlock()
		return controlCommandResult{}, &controlRequestError{Operation: operation, Code: "disconnected", Message: "owned client bridge is not connected"}
	}
	if e.pending != nil {
		e.mutex.Unlock()
		return controlCommandResult{}, &controlRequestError{Operation: operation, Code: "busy", Message: "the owned client already has an outstanding command"}
	}
	if e.requestSequence == ^uint64(0) {
		e.mutex.Unlock()
		return controlCommandResult{}, errors.New("connection command sequence exhausted")
	}
	e.requestSequence++
	pending := &controlPending{requestID: fmt.Sprintf("command-%d", e.requestSequence),
		operation: operation, sequence: e.requestSequence, deliveries: make(chan controlDelivery, 1), command: command}
	e.pending = pending
	r := e.r
	envelope := controlEnvelope{ProtocolVersion: 1, Kind: "command", RunID: e.runID, ClientID: e.clientID,
		ConnectionGeneration: e.generation, RequestID: pending.requestID, Sequence: pending.sequence,
		Operation: operation, DeadlineAt: deadline.UTC().Format(time.RFC3339Nano)}
	if operation == "window.text" || operation == "window.click" || operation == "window.press" || operation == "cinematic.configure" || operation == "scene.set" || operation == "scene.ready" || operation == "gameplay.project" || operation == "gameplay.click" || operation == "gameplay.hold" || operation == "gameplay.select_hero" || operation == "gameplay.cast_active" {
		envelope.Command = &command
	}
	e.mutex.Unlock()
	if operation == "ui.screen_set" || operation == "editor.open_creature" {
		envelope.Command = &command
	}
	remainingMS := (time.Until(deadline) + time.Millisecond - 1) / time.Millisecond
	if remainingMS <= 0 {
		return controlCommandResult{}, e.abandonPending(pending, context.DeadlineExceeded)
	}
	if remainingMS > 2000 {
		remainingMS = 2000
	}
	envelope.TimeoutMS = uint32(remainingMS)
	err = e.write(requestContext, r, envelope, pending)
	if err != nil {
		receiptErr := e.abandonPending(pending, err)
		if receiptErr.IsDispatched {
			// A partial length-prefixed write makes subsequent framing unsafe.
			closeErr := e.Close()
			return controlCommandResult{}, fmt.Errorf("commandWrite: %w", errors.Join(receiptErr, closeErr))
		}
		return controlCommandResult{}, fmt.Errorf("commandUnsent: %w", receiptErr)
	}
	select {
	case delivery := <-pending.deliveries:
		if delivery.err != nil {
			return delivery.result, fmt.Errorf("commandReceipt: %w", delivery.err)
		}
		return delivery.result, nil
	case <-requestContext.Done():
		// Prefer a receipt already delivered at the cancellation boundary.
		select {
		case delivery := <-pending.deliveries:
			if delivery.err != nil {
				return delivery.result, fmt.Errorf("commandBoundary: %w", delivery.err)
			}
			return delivery.result, nil
		default:
		}
		receiptErr := e.abandonPending(pending, requestContext.Err())
		e.cancelCommand(ctx, envelope)
		return controlCommandResult{}, fmt.Errorf("commandWait: %w", receiptErr)
	}
}

func (e *controlPipe) abandonPending(pending *controlPending, cause error) *controlRequestError {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.pending == pending {
		e.pending = nil
	}
	code := "not_dispatched"
	message := "command failed before dispatch"
	if errors.Is(cause, context.Canceled) {
		code = "cancelled"
		message = "command cancelled before dispatch"
	}
	if errors.Is(cause, context.DeadlineExceeded) {
		code = "deadline_exceeded"
		message = "command expired before dispatch"
	}
	if pending.isDispatched {
		e.isPauseUnsafe = true
		code = "outcome_unknown"
		message = "command may have reached the owned client; its result is unknown"
	}
	return &controlRequestError{RequestID: pending.requestID, Operation: pending.operation,
		IsDispatched: pending.isDispatched, Code: code, Message: message, Cause: cause}
}

func (e *controlPipe) failPendingLocked(cause error) {
	if e.pending == nil {
		return
	}
	pending := e.pending
	e.pending = nil
	code := "disconnected"
	if pending.isDispatched {
		code = "outcome_unknown"
	}
	pending.deliveries <- controlDelivery{err: &controlRequestError{RequestID: pending.requestID,
		Operation: pending.operation, IsDispatched: pending.isDispatched, Code: code,
		Message: "owned bridge disconnected before a final command receipt", Cause: cause}}
}

func (e *controlPipe) receiveResult(envelope controlEnvelope) error {
	if envelope.RequestID == "" || envelope.Operation == "" || envelope.Sequence == 0 || envelope.Result == nil && envelope.Failure == nil {
		return errors.New("command result requires a receipt identity and result or failure")
	}
	if envelope.Result != nil && envelope.Result.IsGameplayComplete {
		return errors.New("window command receipt cannot claim gameplay completion")
	}
	if envelope.Failure != nil && envelope.Failure.Code == "" {
		return errors.New("command failure requires a code")
	}
	if envelope.Failure == nil && (envelope.Result == nil || envelope.Result.Status != "processed" || !envelope.Result.IsProcessed || envelope.Result.ThreadID == 0 || envelope.Result.CompletedTickMS == 0) {
		return errors.New("command success requires an explicit processed receipt with owner thread and completion tick")
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if envelope.Sequence > e.requestSequence {
		return errors.New("command result references an unissued sequence")
	}
	pending := e.pending
	if pending == nil || envelope.Sequence < pending.sequence {
		// A cancelled/expired request may finish later; never reattach or replay it.
		return nil
	}
	if envelope.Sequence != pending.sequence || envelope.RequestID != pending.requestID || envelope.Operation != pending.operation {
		return errors.New("command result does not match the outstanding receipt")
	}
	if envelope.Failure == nil && pending.operation == "window.text" && (envelope.Result.ProcessedCharacterCount == nil || *envelope.Result.ProcessedCharacterCount != uint32(len(pending.command.Text))) {
		return errors.New("text processing receipt omits the complete character count")
	}
	if envelope.Failure == nil && (pending.operation == "window.inspect" || pending.operation == "window.ready" || pending.operation == "window.text" || pending.operation == "window.click" || pending.operation == "window.press" || pending.operation == "scene.set" || pending.operation == "scene.ready" || pending.operation == "mission.arena_lobby" || pending.operation == "mission.accept_arena" || pending.operation == "mission.map_state" || pending.operation == "mission.start" || pending.operation == "mission.deployment_state" || pending.operation == "mission.beam_out_state" || pending.operation == "mission.continue" || pending.operation == "mission.beam_out" || pending.operation == "mission.campaign" || pending.operation == "mission.previous" || pending.operation == "mission.next" || pending.operation == "editor.cancel" || pending.operation == "editor.hierarchy" || pending.operation == "editor.open_creature") {
		window := envelope.Result.Window
		if window == nil || !window.IsAvailable || e.last.State == nil || window.ProcessID != e.last.State.ProcessID || window.ThreadID == 0 || window.Width <= 0 || window.Height <= 0 {
			return errors.New("window inspection result does not identify a valid owned client window")
		}
		if pending.operation != "window.inspect" && envelope.Result.ThreadID != window.ThreadID {
			return errors.New("window readiness was not processed by the reported window owner")
		}
	}
	if envelope.Failure == nil && (pending.operation == "mission.accept_arena" || pending.operation == "mission.start" || pending.operation == "mission.continue" || pending.operation == "mission.beam_out" || pending.operation == "mission.campaign" || pending.operation == "mission.previous" || pending.operation == "mission.next") {
		result := envelope.Result
		if result.IsInvocationAttempted == nil || !*result.IsInvocationAttempted || result.IsInvocationAccepted == nil || !*result.IsInvocationAccepted {
			return errors.New("mission action receipt does not confirm the authored invocation")
		}
	}
	if envelope.Failure == nil && (pending.operation == "mission.arena_lobby" || pending.operation == "mission.accept_arena") && envelope.Result.ArenaLobby == nil {
		return errors.New("arena receipt is missing native observations")
	}
	if envelope.Failure == nil && pending.operation == "mission.beam_out_state" && envelope.Result.BeamOutState == nil {
		return errors.New("beam out state response omits native eligibility observations")
	}
	if envelope.Failure == nil && (pending.operation == "ui.screen_state" || pending.operation == "ui.screen_set") {
		window := envelope.Result.Window
		if window == nil || !window.IsAvailable || e.last.State == nil || window.ProcessID != e.last.State.ProcessID || window.ThreadID == 0 || window.Width <= 0 || window.Height <= 0 || envelope.Result.ThreadID != window.ThreadID {
			return errors.New("screen command was not processed by the owned window thread")
		}
		if envelope.Result.ScreenState == nil {
			return errors.New("screen command omitted native screen observations")
		}
		if pending.operation == "ui.screen_set" && (envelope.Result.IsInvocationAttempted == nil || !*envelope.Result.IsInvocationAttempted || !envelope.Result.ScreenState.IsInvocationAttempted || !envelope.Result.ScreenState.IsInvocationReturned || envelope.Result.IsInvocationAccepted != nil) {
			return errors.New("screen action receipt must confirm only the void invocation returned, without claiming acceptance")
		}
		if pending.operation == "ui.screen_set" && envelope.Result.ScreenState.OwnerGeneration != pending.command.ExpectedOwnerGeneration {
			return errors.New("screen action receipt belongs to a different native UI lifecycle")
		}
	}
	if envelope.Failure == nil && pending.operation == "cinematic.configure" {
		result := envelope.Result
		if result.IsIntroSkipped == nil || result.IsCinematicSkipped == nil {
			return errors.New("cinematic configuration receipt omits actual skip settings")
		}
		if pending.command.IsIntroSkipped != nil && *pending.command.IsIntroSkipped != *result.IsIntroSkipped || pending.command.IsCinematicSkipped != nil && *pending.command.IsCinematicSkipped != *result.IsCinematicSkipped {
			return errors.New("cinematic configuration receipt does not match the requested settings")
		}
	}
	delivery := controlDelivery{}
	if envelope.Result != nil {
		delivery.result = *envelope.Result
	}
	if envelope.Failure != nil {
		if envelope.Failure.Code == "outcome_unknown" {
			e.isPauseUnsafe = true
		}
		delivery.err = &controlRequestError{RequestID: pending.requestID, Operation: pending.operation,
			IsDispatched: true, Code: envelope.Failure.Code, Message: "owned client command failed", Cause: envelope.Failure}
	}
	e.pending = nil
	pending.deliveries <- delivery
	return nil
}

func (e *controlPipe) cancelCommand(parent context.Context, envelope controlEnvelope) {
	e.mutex.Lock()
	r := e.r
	isUsable := e.isConnected && r != nil && e.generation == envelope.ConnectionGeneration
	e.mutex.Unlock()
	if !isUsable {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 100*time.Millisecond)
	defer cancel()
	envelope.Kind = "cancel"
	envelope.Command = nil
	err := e.write(ctx, r, envelope, nil)
	if err != nil {
		// Cancellation is best effort: the original receipt remains unknown.
		return
	}
}

type controlWriteCancellation struct {
	r    *os.File
	done chan struct{}
}

func (e *controlWriteCancellation) interrupt() {
	err := e.r.SetWriteDeadline(time.Now())
	if err != nil {
		// Closing the connection already interrupts its writer.
	}
	close(e.done)
}

func finishControlWrite(cancellation *controlWriteCancellation, stop func() bool) {
	if !stop() {
		<-cancellation.done
	}
}

func (e *controlPipe) write(ctx context.Context, r *os.File, envelope controlEnvelope, pending *controlPending) error {
	writeContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	select {
	case e.writers <- struct{}{}:
	case <-writeContext.Done():
		return fmt.Errorf("pipeWriter: %w", writeContext.Err())
	}
	defer e.releaseWriter()
	err := writeContext.Err()
	if err != nil {
		return fmt.Errorf("pipeWriteContext: %w", err)
	}
	deadline, isDeadline := writeContext.Deadline()
	if !isDeadline {
		return errors.New("pipe write requires a deadline")
	}
	err = r.SetWriteDeadline(deadline)
	if err != nil {
		return fmt.Errorf("pipeWriteDeadline: %w", err)
	}
	cancellation := &controlWriteCancellation{r: r, done: make(chan struct{})}
	stop := context.AfterFunc(writeContext, cancellation.interrupt)
	defer finishControlWrite(cancellation, stop)
	if pending != nil {
		e.mutex.Lock()
		if e.pending != pending {
			e.mutex.Unlock()
			return errors.New("command receipt is no longer outstanding")
		}
		pending.isDispatched = true // A write attempt can have an unknown outcome.
		e.mutex.Unlock()
	}
	err = writeControlEnvelope(r, envelope)
	if err != nil {
		return fmt.Errorf("pipeWrite: %w", err)
	}
	return nil
}

func (e *controlPipe) releaseWriter() { <-e.writers }

func (e *controlPipe) Close() error {
	e.cancel()
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.isConnected = false
	e.failPendingLocked(errors.New("owned bridge closed"))
	if e.r != nil {
		err := e.r.Close()
		e.r = nil
		e.handle = windows.InvalidHandle
		if err != nil && !errors.Is(err, os.ErrClosed) {
			return fmt.Errorf("pipeClose: %w", err)
		}
		return nil
	}
	if e.handle != windows.InvalidHandle {
		err := windows.CancelIoEx(e.handle, nil)
		if err != nil && err != windows.ERROR_NOT_FOUND && err != windows.ERROR_INVALID_HANDLE {
			return fmt.Errorf("pipeCancel: %w", err)
		}
	}
	return nil
}

func (e *controlPipe) run(ctx context.Context, token string, expectedPID uint32, runID, clientID string) {
	e.mutex.Lock()
	handle := e.handle
	e.mutex.Unlock()
	err := e.serve(ctx, handle, token, expectedPID, runID, clientID)
	e.mutex.Lock()
	e.isConnected = false
	e.failPendingLocked(err)
	if err != nil && ctx.Err() == nil {
		// Errors contain operation tags only, never incoming payloads/tokens.
		e.failure = "bridge disconnected or rejected"
		e.failureCause = err
	}
	if e.r != nil {
		closeErr := e.r.Close()
		if closeErr != nil && !errors.Is(closeErr, os.ErrClosed) && e.failure == "" {
			e.failure = "pipe close failed"
		}
		e.r = nil
	} else if e.handle != windows.InvalidHandle {
		closeErr := windows.CloseHandle(e.handle)
		if closeErr != nil && e.failure == "" {
			e.failure = "pipe handle close failed"
		}
	}
	e.handle = windows.InvalidHandle
	e.mutex.Unlock()
}

func (e *controlPipe) serve(ctx context.Context, handle windows.Handle, token string, expectedPID uint32, runID, clientID string) error {
	err := connectControlPipe(ctx, handle)
	if err != nil {
		return fmt.Errorf("pipeConnect: %w", err)
	}
	var actualPID uint32
	err = windows.GetNamedPipeClientProcessId(handle, &actualPID)
	if err != nil {
		return fmt.Errorf("pipePeer: %w", err)
	}
	if actualPID != expectedPID {
		return fmt.Errorf("unexpected pipe peer")
	}
	e.mutex.Lock()
	if ctx.Err() != nil {
		e.mutex.Unlock()
		return fmt.Errorf("pipeContext: %w", ctx.Err())
	}
	r := os.NewFile(uintptr(handle), "scenario-control")
	e.r = r
	e.mutex.Unlock()
	stopClose := context.AfterFunc(ctx, e.closeOnCancel)
	defer stopClose()
	err = r.SetDeadline(time.Now().Add(5 * time.Second))
	if err != nil {
		return fmt.Errorf("helloDeadline: %w", err)
	}
	hello, err := readControlEnvelope(r)
	if err != nil {
		return fmt.Errorf("helloRead: %w", err)
	}
	if hello.ProtocolVersion != 1 || hello.Kind != "hello" || hello.ProcessID != expectedPID || hello.BuildID == "" || subtle.ConstantTimeCompare([]byte(hello.Token), []byte(token)) != 1 {
		return fmt.Errorf("invalid bridge handshake")
	}
	token = ""
	hello.Token = ""
	generationBytes := make([]byte, 16)
	readCount, err := rand.Read(generationBytes)
	if err != nil {
		return fmt.Errorf("generationRead: %w", err)
	}
	if readCount != len(generationBytes) {
		return fmt.Errorf("generationShort: %w", io.ErrUnexpectedEOF)
	}
	generation := hex.EncodeToString(generationBytes)
	err = e.write(ctx, r, controlEnvelope{ProtocolVersion: 1, Kind: "accepted", RunID: runID, ClientID: clientID, ConnectionGeneration: generation}, nil)
	if err != nil {
		return fmt.Errorf("helloWrite: %w", err)
	}
	e.mutex.Lock()
	e.generation = generation
	e.mutex.Unlock()
	var sequence uint64
	for {
		e.mutex.Lock()
		readDeadline := time.Now().Add(3 * time.Second)
		if e.pauseID != "" && e.pauseUntil.After(readDeadline) {
			readDeadline = e.pauseUntil
		}
		err = r.SetReadDeadline(readDeadline)
		e.mutex.Unlock()
		if err != nil {
			return fmt.Errorf("stateDeadline: %w", err)
		}
		envelope, readErr := readControlEnvelope(r)
		if readErr != nil {
			return fmt.Errorf("stateRead: %w", readErr)
		}
		if envelope.ProtocolVersion != 1 || envelope.Token != "" || envelope.RunID != runID || envelope.ClientID != clientID || envelope.ConnectionGeneration != generation {
			return errors.New("invalid bridge envelope identity")
		}
		if envelope.Kind == "result" {
			err = e.receiveResult(envelope)
			if err != nil {
				return fmt.Errorf("commandResult: %w", err)
			}
			continue
		}
		if envelope.Kind != "state" || envelope.Sequence <= sequence || envelope.RequestID == "" || envelope.TimeoutMS == 0 || envelope.TimeoutMS > 2000 || envelope.State == nil || envelope.State.ProcessID != expectedPID || envelope.State.BuildID != hello.BuildID {
			return fmt.Errorf("invalid bridge state")
		}
		sequence = envelope.Sequence
		e.mutex.Lock()
		e.last = envelope
		e.lastAt = time.Now().UTC()
		e.isConnected = true
		e.mutex.Unlock()
	}
}

func (e *controlPipe) closeOnCancel() {
	err := e.Close()
	if err != nil {
		e.mutex.Lock()
		e.failure = "pipe cancellation failed"
		e.mutex.Unlock()
	}
}

func connectControlPipe(ctx context.Context, handle windows.Handle) error {
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return fmt.Errorf("eventCreate: %w", err)
	}
	defer closeControlEvent(event)
	overlap := windows.Overlapped{HEvent: event}
	err = windows.ConnectNamedPipe(handle, &overlap)
	if err == nil || err == windows.ERROR_PIPE_CONNECTED {
		return nil
	}
	if err != windows.ERROR_IO_PENDING {
		return fmt.Errorf("connectStart: %w", err)
	}
	for {
		status, waitErr := windows.WaitForSingleObject(event, 100)
		if waitErr != nil {
			return fmt.Errorf("connectWait: %w", waitErr)
		}
		if ctx.Err() != nil {
			cancelErr := windows.CancelIoEx(handle, &overlap)
			if cancelErr != nil && cancelErr != windows.ERROR_NOT_FOUND {
				return fmt.Errorf("connectCancel: %w", cancelErr)
			}
			var transferred uint32
			finishErr := windows.GetOverlappedResult(handle, &overlap, &transferred, true)
			if finishErr != nil && finishErr != windows.ERROR_OPERATION_ABORTED {
				return fmt.Errorf("connectFinish: %w", finishErr)
			}
			return fmt.Errorf("connectContext: %w", ctx.Err())
		}
		if status == windows.WAIT_OBJECT_0 {
			var transferred uint32
			finishErr := windows.GetOverlappedResult(handle, &overlap, &transferred, false)
			if finishErr != nil {
				return fmt.Errorf("connectResult: %w", finishErr)
			}
			return nil
		}
	}
}

func closeControlEvent(event windows.Handle) {
	err := windows.CloseHandle(event)
	if err != nil {
		// This event owns no gameplay state; closing failure cannot be retried.
		return
	}
}

const controlLimit = 64 * 1024

type controlEnvelope struct {
	ProtocolVersion      uint32                 `json:"protocol_version"`
	Kind                 string                 `json:"kind"`
	Token                string                 `json:"token,omitempty"`
	ProcessID            uint32                 `json:"process_id,omitempty"`
	BuildID              string                 `json:"build_id,omitempty"`
	RunID                string                 `json:"run_id,omitempty"`
	ClientID             string                 `json:"client_id,omitempty"`
	ConnectionGeneration string                 `json:"connection_generation,omitempty"`
	RequestID            string                 `json:"request_id,omitempty"`
	TimeoutMS            uint32                 `json:"timeout_ms,omitempty"`
	Sequence             uint64                 `json:"sequence,omitempty"`
	ObservedAt           string                 `json:"observed_at,omitempty"`
	State                *controlState          `json:"state,omitempty"`
	Operation            string                 `json:"operation,omitempty"`
	DeadlineAt           string                 `json:"deadline_at,omitempty"`
	Command              *controlCommand        `json:"command,omitempty"`
	Result               *controlCommandResult  `json:"result,omitempty"`
	Failure              *controlCommandFailure `json:"failure,omitempty"`
}

type controlCommand struct {
	HeroNoun                uint32             `json:"hero_noun,omitempty"`
	ExpectedOwnerGeneration uint64             `json:"expected_owner_generation,omitempty"`
	Screen                  string             `json:"screen,omitempty"`
	Text                    string             `json:"text,omitempty"`
	HeroSlot                *uint32            `json:"hero_slot,omitempty"`
	HoldMS                  uint32             `json:"hold_ms,omitempty"`
	WorldPoint              *controlWorldPoint `json:"world_point,omitempty"`
	Key                     string             `json:"key,omitempty"`
	Scene                   string             `json:"scene,omitempty"`
	X                       int32              `json:"x"`
	Y                       int32              `json:"y"`
	Button                  string             `json:"button,omitempty"`
	IsIntroSkipped          *bool              `json:"is_intro_skipped,omitempty"`
	IsCinematicSkipped      *bool              `json:"is_cinematic_skipped,omitempty"`
}

type controlWindow struct {
	IsAvailable bool   `json:"is_available"`
	ProcessID   uint32 `json:"process_id"`
	ThreadID    uint32 `json:"thread_id"`
	Width       int32  `json:"width"`
	Height      int32  `json:"height"`
	IsEnabled   bool   `json:"is_enabled"`
	IsMinimized bool   `json:"is_minimized"`
	WindowID    string `json:"window_id"`
}

type controlCommandResult struct {
	EditorHierarchy         *controlEditorArtifact  `json:"editor_hierarchy,omitempty"`
	ScreenState             *controlScreenState     `json:"screen_state,omitempty"`
	ProcessedCharacterCount *uint32                 `json:"processed_character_count,omitempty"`
	ActiveCast              *controlActiveCast      `json:"active_cast,omitempty"`
	HeroSelection           *controlHeroSelection   `json:"hero_selection,omitempty"`
	BeamOutState            *controlBeamOutState    `json:"beam_out_state,omitempty"`
	GameplayState           *controlGameplayState   `json:"gameplay_state,omitempty"`
	Projection              *controlProjection      `json:"projection,omitempty"`
	GameplayClick           *controlGameplayClick   `json:"gameplay_click,omitempty"`
	ArenaLobby              *controlArenaLobbyState `json:"arena_lobby,omitempty"`
	DeploymentState         *controlDeploymentState `json:"deployment_state,omitempty"`
	IsInvocationAttempted   *bool                   `json:"is_invocation_attempted,omitempty"`
	IsInvocationAccepted    *bool                   `json:"is_invocation_accepted,omitempty"`
	MapState                *controlMapState        `json:"map_state,omitempty"`
	Status                  string                  `json:"status"`
	IsProcessed             bool                    `json:"is_processed"`
	IsGameplayComplete      bool                    `json:"is_gameplay_complete"`
	ThreadID                uint32                  `json:"thread_id"`
	CompletedTickMS         uint64                  `json:"completed_tick_ms"`
	Window                  *controlWindow          `json:"window,omitempty"`
	IsIntroSkipped          *bool                   `json:"is_intro_skipped,omitempty"`
	IsCinematicSkipped      *bool                   `json:"is_cinematic_skipped,omitempty"`
}

type controlCommandFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *controlCommandFailure) Error() string { return e.Code + ": " + e.Message }

type controlField struct {
	IsAvailable bool   `json:"is_available"`
	Reason      string `json:"reason,omitempty"`
}

type controlFrame struct {
	controlField
	Sequence       uint32 `json:"sequence"`
	ThreadID       uint32 `json:"thread_id"`
	ObservedTickMS uint64 `json:"observed_tick_ms"`
}

type controlScene struct {
	controlField
	AssetID                   uint32 `json:"asset_id"`
	LoadReturnCount           uint32 `json:"load_return_count"`
	FrameSequence             uint32 `json:"frame_sequence"`
	ObservedTickMS            uint64 `json:"observed_tick_ms"`
	IsMaterializationComplete *bool  `json:"is_materialization_complete"`
}

type controlState struct {
	ProcessID uint32       `json:"process_id"`
	BuildID   string       `json:"build_id"`
	Frame     controlFrame `json:"frame"`
	Scene     controlScene `json:"scene"`
	World     controlField `json:"world"`
	Hero      controlField `json:"hero"`
	UI        controlField `json:"ui"`
}

func writeControlEnvelope(w io.Writer, envelope controlEnvelope) error {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("messageMarshal: %w", err)
	}
	if len(payload) == 0 || len(payload) > controlLimit {
		return fmt.Errorf("message size exceeds limit")
	}
	packet := make([]byte, 4+len(payload))
	binary.LittleEndian.PutUint32(packet, uint32(len(payload)))
	copy(packet[4:], payload)
	written, err := w.Write(packet)
	if err != nil {
		return fmt.Errorf("messageWrite: %w", err)
	}
	if written != len(packet) {
		return fmt.Errorf("messageShort: %w", io.ErrShortWrite)
	}
	return nil
}

func readControlEnvelope(r io.Reader) (controlEnvelope, error) {
	var envelope controlEnvelope
	var header [4]byte
	headerCount, err := io.ReadFull(r, header[:])
	if err != nil {
		return envelope, fmt.Errorf("headerRead: %w", err)
	}
	if headerCount != len(header) {
		return envelope, fmt.Errorf("incomplete message header")
	}
	length := binary.LittleEndian.Uint32(header[:])
	if length == 0 || length > controlLimit {
		return envelope, fmt.Errorf("message size exceeds limit")
	}
	payload := make([]byte, int(length))
	payloadCount, err := io.ReadFull(r, payload)
	if err != nil {
		return envelope, fmt.Errorf("payloadRead: %w", err)
	}
	if payloadCount != len(payload) {
		return envelope, fmt.Errorf("incomplete message payload")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&envelope)
	if err != nil {
		return envelope, fmt.Errorf("messageDecode: %w", err)
	}
	var extra any
	err = decoder.Decode(&extra)
	if err != io.EOF {
		return envelope, fmt.Errorf("trailing message data")
	}
	return envelope, nil
}
