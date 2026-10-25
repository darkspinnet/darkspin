//go:build windows && cgo && scenario && fangdebug

package main

/*
#include "control.h"
#include "cinematic.h"
#include <stdlib.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// One owner writes both heartbeat and command receipts. The reader has a
// bounded mailbox and performs no gameplay/native input work.
type controlConnection struct {
	r                     *os.File
	identity              controlEnvelope
	commands              chan controlReceivedCommand
	done                  chan struct{}
	lastCommandSequence   uint64
	nativeMutex           sync.Mutex
	activeNativeID        uint32
	activeRequestID       string
	lastCanceledSequence  uint64
	lastCanceledRequestID string
	isDisconnected        bool
}

type controlReceivedCommand struct {
	envelope   controlEnvelope
	receivedAt time.Time
}

func (e *controlConnection) readCommands() {
	defer e.disconnect()
	for {
		message, err := readControlEnvelope(e.r)
		if err != nil {
			return
		}
		if message.ProtocolVersion != 1 || message.RunID != e.identity.RunID || message.ClientID != e.identity.ClientID || message.ConnectionGeneration != e.identity.ConnectionGeneration || message.Token != "" || message.State != nil || message.Result != nil || message.Failure != nil {
			return
		}
		if message.Kind == "cancel" {
			e.cancelCommand(message)
			continue
		}
		if message.Kind != "command" {
			return
		}
		select {
		case e.commands <- controlReceivedCommand{envelope: message, receivedAt: time.Now()}:
		default:
			return // A second queued request violates the one-command contract.
		}
	}
}

func (e *controlConnection) disconnect() {
	e.nativeMutex.Lock()
	e.isDisconnected = true
	if e.activeNativeID != 0 {
		C.fang_control_window_cancel(C.uint32_t(e.activeNativeID))
		C.fang_control_gameplay_cancel(C.uint32_t(e.activeNativeID))
	}
	e.nativeMutex.Unlock()
	close(e.done)
}

func (e *controlConnection) cancelCommand(req controlEnvelope) {
	e.nativeMutex.Lock()
	defer e.nativeMutex.Unlock()
	if req.Sequence == 0 || req.RequestID == "" {
		return
	}
	if req.Sequence > e.lastCanceledSequence {
		e.lastCanceledSequence = req.Sequence
		e.lastCanceledRequestID = req.RequestID
	}
	if e.activeNativeID != 0 && uint64(e.activeNativeID) == req.Sequence && e.activeRequestID == req.RequestID {
		C.fang_control_window_cancel(C.uint32_t(e.activeNativeID))
		C.fang_control_gameplay_cancel(C.uint32_t(e.activeNativeID))
	}
}

func (e *controlConnection) run() error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	defer e.cancelActiveCommand()
	replies := make(chan controlEnvelope, 1)
	isPending := false
	sequence := uint64(1)
	err := e.heartbeat(sequence)
	if err != nil {
		return fmt.Errorf("initialHeartbeat: %w", err)
	}
	for {
		select {
		case <-e.done:
			return errors.New("control peer disconnected")
		case request := <-e.commands:
			if isPending {
				return errors.New("control peer sent a concurrent command")
			}
			select {
			case <-e.done:
				return errors.New("control peer disconnected before command")
			default:
			}
			isPending = true
			go e.processCommand(request, replies)
		case response := <-replies:
			isPending = false
			err = e.send(response)
			if err != nil {
				return fmt.Errorf("commandReply: %w", err)
			}
		case <-ticker.C:
			sequence++
			err = e.heartbeat(sequence)
			if err != nil {
				return fmt.Errorf("heartbeatWrite: %w", err)
			}
		}
	}
}

func (e *controlConnection) processCommand(req controlReceivedCommand, replies chan<- controlEnvelope) {
	response := e.command(req.envelope, req.receivedAt)
	select {
	case replies <- response:
	case <-e.done:
	}
}

func (e *controlConnection) cancelActiveCommand() {
	e.nativeMutex.Lock()
	defer e.nativeMutex.Unlock()
	e.isDisconnected = true
	if e.activeNativeID != 0 {
		C.fang_control_window_cancel(C.uint32_t(e.activeNativeID))
		C.fang_control_gameplay_cancel(C.uint32_t(e.activeNativeID))
	}
}

func (e *controlConnection) heartbeat(sequence uint64) error {
	state := observeControlState()
	err := e.send(controlEnvelope{ProtocolVersion: 1, Kind: "state", RunID: e.identity.RunID, ClientID: e.identity.ClientID, ConnectionGeneration: e.identity.ConnectionGeneration,
		RequestID: fmt.Sprintf("state-%d", sequence), TimeoutMS: 2000, Sequence: sequence, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), State: &state})
	if err != nil {
		return fmt.Errorf("stateSend: %w", err)
	}
	return nil
}

func (e *controlConnection) send(message controlEnvelope) error {
	err := e.r.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err != nil {
		return fmt.Errorf("replyDeadline: %w", err)
	}
	err = writeControlEnvelope(e.r, message)
	if err != nil {
		return fmt.Errorf("replyWrite: %w", err)
	}
	return nil
}

func (e *controlConnection) command(req controlEnvelope, receivedAt time.Time) controlEnvelope {
	response := controlEnvelope{ProtocolVersion: 1, Kind: "result", RunID: e.identity.RunID, ClientID: e.identity.ClientID, ConnectionGeneration: e.identity.ConnectionGeneration,
		RequestID: req.RequestID, Sequence: req.Sequence, Operation: req.Operation, Result: &controlCommandResult{Status: "rejected"}}
	if isControlMissionAction(req.Operation) || req.Operation == "gameplay.select_hero" || req.Operation == "gameplay.cast_active" || req.Operation == "ui.screen_set" {
		isAttempted, isAccepted := false, false
		response.Result.IsInvocationAttempted = &isAttempted
		response.Result.IsInvocationAccepted = &isAccepted
	}
	if req.Sequence == 0 || req.Sequence <= e.lastCommandSequence || req.RequestID == "" || len(req.RequestID) > 128 {
		response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "command identity or sequence is invalid"}
		return response
	}
	e.lastCommandSequence = req.Sequence
	if req.Operation != "ui.screen_set" && req.Command != nil && (req.Command.Screen != "" || req.Command.ExpectedOwnerGeneration != 0) {
		response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "screen selection and owner generation are accepted only by ui.screen_set"}
		return response
	}
	if req.Operation != "window.text" && req.Command != nil && req.Command.Text != "" {
		response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "text is accepted only by window.text"}
		return response
	}
	if req.Operation != "gameplay.select_hero" && req.Command != nil && req.Command.HeroSlot != nil {
		response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "hero slot is accepted only by gameplay.select_hero"}
		return response
	}
	if req.Operation != "gameplay.hold" && req.Command != nil && req.Command.HoldMS != 0 {
		response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "hold duration is accepted only by gameplay.hold"}
		return response
	}
	if req.Operation != "gameplay.project" && req.Operation != "gameplay.click" && req.Operation != "gameplay.hold" && req.Operation != "gameplay.cast_active" && req.Command != nil && req.Command.WorldPoint != nil {
		response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "operation does not accept a world point"}
		return response
	}
	if req.TimeoutMS == 0 || req.TimeoutMS > 2000 {
		response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "command timeout must be between 1 and 2000 milliseconds"}
		return response
	}
	deadline, err := time.Parse(time.RFC3339Nano, req.DeadlineAt)
	if err != nil {
		response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "command deadline must be RFC3339Nano"}
		return response
	}
	localDeadline := receivedAt.Add(time.Duration(req.TimeoutMS) * time.Millisecond)
	if deadline.Before(localDeadline) {
		localDeadline = deadline
	}
	if !time.Now().Before(localDeadline) {
		response.Result.Status = "expired"
		response.Failure = &controlCommandFailure{Code: "deadline_exceeded", Message: "command expired before processing"}
		return response
	}
	switch req.Operation {
	case "ui.screen_set", "ui.screen_state":
		return e.windowCommand(req, response, localDeadline)
	case "gameplay.inspect", "gameplay.project", "gameplay.click", "gameplay.hold", "gameplay.select_hero", "gameplay.cast_active":
		return e.gameplayCommand(req, response, localDeadline)
	case "cinematic.configure":
		return e.configureCinematic(req, response, localDeadline)
	case "window.inspect":
		if req.Command != nil && (req.Command.Key != "" || req.Command.Scene != "" || req.Command.X != 0 || req.Command.Y != 0 || req.Command.Button != "" || req.Command.IsIntroSkipped != nil || req.Command.IsCinematicSkipped != nil) {
			response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "window inspection takes no input arguments"}
			return response
		}
		window := inspectControlWindow()
		response.Result.Window = &window
		response.Result.ThreadID = window.ThreadID
		response.Result.CompletedTickMS = uint64(windows.DurationSinceBoot() / time.Millisecond)
		if !window.IsAvailable {
			response.Result.Status = "unavailable"
			response.Failure = &controlCommandFailure{Code: "unavailable", Message: "owned game window is unavailable or ambiguous"}
			return response
		}
		response.Result.Status = "processed"
		response.Result.IsProcessed = true
	case "window.click", "window.ready", "window.press", "window.text", "scene.set", "scene.ready", "editor.cancel", "editor.hierarchy", "editor.open_creature", "mission.map_state", "mission.start", "mission.deployment_state", "mission.continue", "mission.campaign", "mission.previous", "mission.next", "mission.beam_out", "mission.beam_out_state", "mission.arena_lobby", "mission.accept_arena":
		return e.windowCommand(req, response, localDeadline)
	default:
		response.Result.Status = "unavailable"
		response.Failure = &controlCommandFailure{Code: "unsupported_operation", Message: "operation is not supported by this Fang bridge"}
	}
	if time.Now().After(localDeadline) && response.Result.IsProcessed {
		response.Result.Status = "expired"
		response.Failure = &controlCommandFailure{Code: "deadline_exceeded", Message: "window inspection completed after its deadline"}
	}
	return response
}

func (e *controlConnection) configureCinematic(req controlEnvelope, response controlEnvelope, deadline time.Time) controlEnvelope {
	if req.Command == nil || req.Command.Key != "" || req.Command.Scene != "" || req.Command.X != 0 || req.Command.Y != 0 || req.Command.Button != "" || (req.Command.IsIntroSkipped == nil && req.Command.IsCinematicSkipped == nil) {
		response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "cinematic configuration requires at least one skip flag and no window input"}
		return response
	}
	var mask, flags uint32
	if req.Command.IsIntroSkipped != nil {
		mask |= 1
		if *req.Command.IsIntroSkipped {
			flags |= 1
		}
	}
	if req.Command.IsCinematicSkipped != nil {
		mask |= 2
		if *req.Command.IsCinematicSkipped {
			flags |= 2
		}
	}
	e.nativeMutex.Lock()
	defer e.nativeMutex.Unlock()
	if e.isDisconnected || (req.Sequence == e.lastCanceledSequence && req.RequestID == e.lastCanceledRequestID) {
		response.Result.Status = "canceled"
		response.Failure = &controlCommandFailure{Code: "canceled", Message: "cinematic configuration canceled before processing"}
		return response
	}
	if !time.Now().Before(deadline) {
		response.Result.Status = "expired"
		response.Failure = &controlCommandFailure{Code: "deadline_exceeded", Message: "cinematic configuration expired before processing"}
		return response
	}
	var setting C.fang_cinematic_setting
	isConfigured := C.fang_cinematic_configure(C.uint32_t(mask), C.uint32_t(flags), &setting)
	if isConfigured == 0 {
		response.Result.Status = "unavailable"
		response.Failure = &controlCommandFailure{Code: "unavailable", Message: "validated native cinematic Escape hook is unavailable"}
		return response
	}
	isIntroSkipped := setting.flags&1 != 0
	isCinematicSkipped := setting.flags&2 != 0
	response.Result.Status = "processed"
	response.Result.IsProcessed = true
	response.Result.ThreadID = uint32(setting.thread_id)
	response.Result.CompletedTickMS = uint64(setting.completed_tick_ms)
	response.Result.IsIntroSkipped = &isIntroSkipped
	response.Result.IsCinematicSkipped = &isCinematicSkipped
	if time.Now().After(deadline) {
		response.Result.Status = "expired"
		response.Failure = &controlCommandFailure{Code: "deadline_exceeded", Message: "cinematic configuration processed after its deadline"}
	}
	return response
}

func (e *controlConnection) windowCommand(req controlEnvelope, response controlEnvelope, deadline time.Time) controlEnvelope {
	if req.Command != nil && req.Command.HeroNoun != 0 && req.Operation != "editor.open_creature" {
		response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "hero_noun requires editor.open_creature"}
		return response
	}
	if req.Operation == "editor.open_creature" && (req.Command == nil || req.Command.HeroNoun == 0) {
		response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "Creature Editor requires an owned hero noun"}
		return response
	}
	isScreen := req.Operation == "ui.screen_set" || req.Operation == "ui.screen_state"
	isScreenAction := req.Operation == "ui.screen_set"
	var screen uint32
	if isScreen {
		if req.Command != nil && (req.Command.Key != "" || req.Command.Scene != "" || req.Command.X != 0 || req.Command.Y != 0 || req.Command.Button != "" || req.Command.IsIntroSkipped != nil || req.Command.IsCinematicSkipped != nil) {
			response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "screen operation takes only a screen name and expected owner generation"}
			return response
		}
		if isScreenAction {
			if req.Command == nil || req.Command.ExpectedOwnerGeneration == 0 {
				response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "screen setter requires a name and observed owner generation"}
				return response
			}
			var isKnown bool
			screen, isKnown = controlScreenNumber(req.Command.Screen)
			if !isKnown {
				response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "screen name is unsupported"}
				return response
			}
		}
	}
	isReady := req.Operation == "window.ready"
	isMissionAction := isControlMissionAction(req.Operation)
	isMission := req.Operation == "mission.map_state" || req.Operation == "mission.deployment_state" || req.Operation == "mission.beam_out_state" || req.Operation == "mission.arena_lobby" || isMissionAction
	isEditorHierarchy := req.Operation == "editor.hierarchy"
	isEditorOpen := req.Operation == "editor.open_creature"
	isEditor := req.Operation == "editor.cancel" || isEditorHierarchy || isEditorOpen
	isMissionObservation := isMission && !isMissionAction
	if (isMission || isEditor) && req.Command != nil && (req.Command.Key != "" || req.Command.Scene != "" || req.Command.X != 0 || req.Command.Y != 0 || req.Command.Button != "" || req.Command.IsIntroSkipped != nil || req.Command.IsCinematicSkipped != nil) {
		response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "fixed UI operation takes no input arguments"}
		return response
	}
	isScene := req.Operation == "scene.set" || req.Operation == "scene.ready"
	isPress := req.Operation == "window.press"
	isText := req.Operation == "window.text"
	if isText {
		if req.Command == nil || req.Command.Key != "" || req.Command.Scene != "" || req.Command.X != 0 || req.Command.Y != 0 || req.Command.Button != "" || len(req.Command.Text) < 1 || len(req.Command.Text) > 256 {
			response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "text requires 1 through 256 printable ASCII characters and no other input"}
			return response
		}
		for index := 0; index < len(req.Command.Text); index++ {
			if req.Command.Text[index] < 32 || req.Command.Text[index] > 126 {
				response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "text contains a non-printable or non-ASCII character"}
				return response
			}
		}
	}
	var key uint32
	if isPress {
		if req.Command == nil || req.Command.Scene != "" || req.Command.X != 0 || req.Command.Y != 0 || req.Command.Button != "" {
			response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "key press requires an allowlisted key and no other window input"}
			return response
		}
		switch req.Command.Key {
		case "Escape":
			key = 27
		case "Enter":
			key = 13
		default:
			response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "key must be Escape or Enter"}
			return response
		}
	} else if req.Command != nil && req.Command.Key != "" {
		response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "operation does not accept a key"}
		return response
	}
	var room uint32
	if isScene {
		if req.Command == nil || req.Command.X != 0 || req.Command.Y != 0 || req.Command.Button != "" {
			response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "scene operation requires a scene name and no window input"}
			return response
		}
		switch req.Command.Scene {
		case "arsenal":
			room = 1
		case "navigation":
			room = 2
		case "editor":
			room = 3
		default:
			response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "scene must be arsenal, navigation, or editor"}
			return response
		}
	} else if req.Command != nil && req.Command.Scene != "" {
		response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "window commands do not accept a scene name"}
		return response
	}
	if req.Command != nil && (req.Command.IsIntroSkipped != nil || req.Command.IsCinematicSkipped != nil) {
		response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "window commands do not accept cinematic configuration"}
		return response
	}
	if req.Sequence > uint64(^uint32(0)) {
		response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "native window command requires a representable request sequence"}
		return response
	}
	if isReady && req.Command != nil {
		response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "window readiness takes no input arguments"}
		return response
	}
	if !isScreen && !isReady && !isMission && !isEditor && !isScene && !isPress && !isText && (req.Command == nil || req.Command.X < 0 || req.Command.Y < 0 || req.Command.X > 32767 || req.Command.Y > 32767 || (req.Command.Button != "" && req.Command.Button != "left" && req.Command.Button != "right")) {
		response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "click requires a representable request sequence and bounded left- or right-button coordinates"}
		return response
	}
	window := inspectControlWindow()
	response.Result.Window = &window
	if !window.IsAvailable || (!isReady && !isMissionObservation && (!window.IsEnabled || window.IsMinimized)) {
		response.Result.Status = "unavailable"
		response.Failure = &controlCommandFailure{Code: "unavailable", Message: "owned game window is unavailable, disabled, or minimized"}
		return response
	}
	if !isScreen && !isReady && !isMission && !isEditor && !isScene && !isPress && !isText && (req.Command.X >= window.Width || req.Command.Y >= window.Height) {
		response.Failure = &controlCommandFailure{Code: "invalid_request", Message: "click coordinates are outside the owned game window"}
		return response
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		response.Result.Status = "expired"
		response.Failure = &controlCommandFailure{Code: "deadline_exceeded", Message: "window command expired before native submission"}
		return response
	}
	nativeID := uint32(req.Sequence)
	// Submission only queues a finite message for the original HWND owner.
	// The native callback owns input processing; this worker never invokes it.
	e.nativeMutex.Lock()
	if e.isDisconnected || (req.Sequence == e.lastCanceledSequence && req.RequestID == e.lastCanceledRequestID) {
		e.nativeMutex.Unlock()
		response.Result.Status = "canceled"
		response.Failure = &controlCommandFailure{Code: "canceled", Message: "window command canceled before native submission"}
		return response
	}
	timeoutMS := uint32((remaining + time.Millisecond - 1) / time.Millisecond)
	var isAccepted C.int
	if isScreen {
		var expectedGeneration uint64
		var isAction C.int
		if isScreenAction {
			expectedGeneration = req.Command.ExpectedOwnerGeneration
			isAction = 1
		}
		isAccepted = C.fang_control_window_screen(C.uint32_t(nativeID), C.uint32_t(screen), isAction, C.uint64_t(expectedGeneration), C.uint32_t(timeoutMS))
	} else if isReady {
		isAccepted = C.fang_control_window_ready(C.uint32_t(nativeID), C.uint32_t(timeoutMS))
	} else if isEditorOpen {
		isAccepted = C.fang_control_window_editor_open(C.uint32_t(nativeID), C.uint32_t(req.Command.HeroNoun), C.uint32_t(timeoutMS))
	} else if isEditorHierarchy {
		isAccepted = C.fang_control_window_editor_hierarchy(C.uint32_t(nativeID), C.uint32_t(timeoutMS))
	} else if isEditor {
		isAccepted = C.fang_control_window_editor_cancel(C.uint32_t(nativeID), C.uint32_t(timeoutMS))
	} else if isMission {
		switch req.Operation {
		case "mission.arena_lobby":
			isAccepted = C.fang_control_window_mission(C.uint32_t(nativeID), 19, C.uint32_t(timeoutMS))
		case "mission.accept_arena":
			isAccepted = C.fang_control_window_mission(C.uint32_t(nativeID), 20, C.uint32_t(timeoutMS))
		case "mission.map_state":
			isAccepted = C.fang_control_window_map(C.uint32_t(nativeID), C.uint32_t(timeoutMS))
		case "mission.start":
			isAccepted = C.fang_control_window_mission(C.uint32_t(nativeID), 6, C.uint32_t(timeoutMS))
		case "mission.deployment_state":
			isAccepted = C.fang_control_window_mission(C.uint32_t(nativeID), 7, C.uint32_t(timeoutMS))
		case "mission.continue":
			isAccepted = C.fang_control_window_mission(C.uint32_t(nativeID), 8, C.uint32_t(timeoutMS))
		case "mission.campaign":
			isAccepted = C.fang_control_window_mission(C.uint32_t(nativeID), 9, C.uint32_t(timeoutMS))
		case "mission.previous":
			isAccepted = C.fang_control_window_mission(C.uint32_t(nativeID), 10, C.uint32_t(timeoutMS))
		case "mission.next":
			isAccepted = C.fang_control_window_mission(C.uint32_t(nativeID), 11, C.uint32_t(timeoutMS))
		case "mission.beam_out":
			isAccepted = C.fang_control_window_mission(C.uint32_t(nativeID), 13, C.uint32_t(timeoutMS))
		case "mission.beam_out_state":
			isAccepted = C.fang_control_window_mission(C.uint32_t(nativeID), 14, C.uint32_t(timeoutMS))
		}
	} else if isPress {
		isAccepted = C.fang_control_window_press(C.uint32_t(nativeID), C.uint32_t(key), C.uint32_t(timeoutMS))
	} else if isText {
		text := C.CString(req.Command.Text)
		isAccepted = C.fang_control_window_text(C.uint32_t(nativeID), text, C.uint32_t(len(req.Command.Text)), C.uint32_t(timeoutMS))
		C.free(unsafe.Pointer(text))
	} else if isScene {
		if req.Operation == "scene.ready" {
			isAccepted = C.fang_control_window_scene_ready(C.uint32_t(nativeID), C.uint32_t(room), C.uint32_t(timeoutMS))
		} else {
			isAccepted = C.fang_control_window_scene(C.uint32_t(nativeID), C.uint32_t(room), C.uint32_t(timeoutMS))
		}
	} else if req.Command.Button == "right" {
		isAccepted = C.fang_control_window_right_click(C.uint32_t(nativeID), C.int32_t(req.Command.X), C.int32_t(req.Command.Y), C.uint32_t(timeoutMS))
	} else {
		isAccepted = C.fang_control_window_submit(C.uint32_t(nativeID), C.int32_t(req.Command.X), C.int32_t(req.Command.Y), C.uint32_t(timeoutMS))
	}
	if isAccepted != 0 {
		e.activeNativeID = nativeID
		e.activeRequestID = req.RequestID
		if isMissionAction || isScreenAction {
			// Until the owner returns a terminal receipt, dispatch outcome is
			// unknown. Disconnect/deadline never implies it is safe to replay.
			response.Result.IsInvocationAttempted = nil
			response.Result.IsInvocationAccepted = nil
		}
	}
	e.nativeMutex.Unlock()
	if isAccepted == 0 {
		response.Result.Status = "unavailable"
		response.Failure = &controlCommandFailure{Code: "unavailable", Message: "native window queue rejected the command"}
		return response
	}
	defer e.clearNativeCommand(nativeID)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	for {
		var receipt C.fang_control_window_receipt
		isFound := C.fang_control_window_result(C.uint32_t(nativeID), &receipt)
		if isFound != 0 && receipt.status != 0 {
			return controlWindowReceipt(response, receipt, deadline)
		}
		select {
		case <-e.done:
			C.fang_control_window_cancel(C.uint32_t(nativeID))
			response.Result.Status = "canceled"
			response.Failure = &controlCommandFailure{Code: "canceled", Message: "control peer disconnected while window command was pending"}
		case <-timer.C:
			C.fang_control_window_cancel(C.uint32_t(nativeID))
			response.Result.Status = "expired"
			response.Failure = &controlCommandFailure{Code: "deadline_exceeded", Message: "native window receipt deadline exceeded"}
		case <-ticker.C:
			continue
		}
		// Cancellation may race a completed owner-thread callback. Preserve its
		// actual receipt instead of claiming the input could not have happened.
		isFound = C.fang_control_window_result(C.uint32_t(nativeID), &receipt)
		if isFound != 0 && receipt.status != 0 {
			terminalStatus := response.Result.Status
			terminalFailure := response.Failure
			finalResponse := controlWindowReceipt(response, receipt, deadline)
			if receipt.status == 1 {
				finalResponse.Result.Status = terminalStatus
				finalResponse.Failure = terminalFailure
			}
			return finalResponse
		}
		return response
	}
}

func (e *controlConnection) clearNativeCommand(nativeID uint32) {
	e.nativeMutex.Lock()
	defer e.nativeMutex.Unlock()
	if e.activeNativeID == nativeID {
		e.activeNativeID = 0
		e.activeRequestID = ""
	}
}

func controlWindowReceipt(response controlEnvelope, receipt C.fang_control_window_receipt, deadline time.Time) controlEnvelope {
	response.Result.ThreadID = uint32(receipt.thread_id)
	response.Result.CompletedTickMS = uint64(receipt.completed_tick_ms)
	window := inspectControlWindow()
	response.Result.Window = &window
	if response.Operation == "ui.screen_state" || response.Operation == "ui.screen_set" {
		response.Result.ScreenState = readControlScreenReceipt(uint32(receipt.request_id))
	}
	if response.Operation == "window.text" {
		count := uint32(receipt.processed_character_count)
		response.Result.ProcessedCharacterCount = &count
	}
	if response.Operation == "mission.map_state" || (isControlMissionAction(response.Operation) && response.Operation != "mission.continue" && response.Operation != "mission.beam_out" && response.Operation != "mission.accept_arena") {
		response.Result.MapState = readControlMapReceipt(uint32(receipt.request_id))
	}
	if response.Operation == "mission.arena_lobby" || response.Operation == "mission.accept_arena" {
		response.Result.ArenaLobby = readControlArenaReceipt(uint32(receipt.request_id))
	}
	if response.Operation == "mission.deployment_state" || response.Operation == "mission.continue" {
		response.Result.DeploymentState = readControlDeploymentReceipt(uint32(receipt.request_id))
	}
	if response.Operation == "mission.beam_out" || response.Operation == "mission.beam_out_state" {
		response.Result.BeamOutState = readControlBeamReceipt(uint32(receipt.request_id))
	}
	if response.Operation == "ui.screen_set" && response.Result.ScreenState != nil {
		isAttempted := response.Result.ScreenState.IsInvocationAttempted
		response.Result.IsInvocationAttempted = &isAttempted
		response.Result.IsInvocationAccepted = nil
	}
	if isControlMissionAction(response.Operation) {
		isAttempted := receipt.status == 1 || receipt.status == 6
		isAccepted := receipt.status == 1
		response.Result.IsInvocationAttempted = &isAttempted
		response.Result.IsInvocationAccepted = &isAccepted
	}
	switch uint32(receipt.status) {
	case 1:
		response.Result.Status = "processed"
		response.Result.IsProcessed = true
		response.Failure = nil
		if response.Operation == "editor.hierarchy" {
			artifact, err := saveControlEditorHierarchy(uint32(receipt.request_id))
			if err != nil {
				response.Result.Status = "unavailable"
				response.Result.IsProcessed = false
				response.Failure = &controlCommandFailure{Code: "unavailable", Message: err.Error()}
				return response
			}
			response.Result.EditorHierarchy = artifact
		}
		if response.Operation == "mission.map_state" || response.Operation == "mission.deployment_state" {
			if response.Result.MapState == nil && response.Result.DeploymentState == nil {
				response.Result.Status = "unavailable"
				response.Failure = &controlCommandFailure{Code: "unavailable", Message: "processed map state snapshot is unavailable"}
			}
		}
		if response.Operation == "mission.arena_lobby" && response.Result.ArenaLobby == nil {
			response.Result.Status = "unavailable"
			response.Failure = &controlCommandFailure{Code: "unavailable", Message: "processed arena snapshot is unavailable"}
		}
		if response.Operation == "mission.beam_out_state" && response.Result.BeamOutState == nil {
			response.Result.Status = "unavailable"
			response.Failure = &controlCommandFailure{Code: "unavailable", Message: "processed Beam Out snapshot is unavailable"}
		}
		if time.Now().After(deadline) {
			response.Result.Status = "expired"
			response.Failure = &controlCommandFailure{Code: "deadline_exceeded", Message: "native window receipt arrived after its deadline"}
		}
	case 2:
		response.Result.Status = "rejected"
		response.Failure = &controlCommandFailure{Code: "native_rejected", Message: "owned window rejected the queued command"}
	case 3:
		response.Result.Status = "canceled"
		response.Failure = &controlCommandFailure{Code: "canceled", Message: "native window command was canceled before processing"}
	case 4:
		response.Result.Status = "expired"
		response.Failure = &controlCommandFailure{Code: "deadline_exceeded", Message: "native window command expired before processing"}
	case 5:
		response.Result.Status = "unavailable"
		response.Failure = &controlCommandFailure{Code: "unavailable", Message: "native UI operation is not currently eligible or available"}
	case 6:
		response.Result.Status = "outcome_unknown"
		response.Failure = &controlCommandFailure{Code: "outcome_unknown", Message: "authored UI invocation was attempted without an accepted result; do not replay"}
		if response.Operation == "window.text" {
			response.Failure = &controlCommandFailure{Code: "outcome_unknown", Message: "native character processing was interrupted or remains in flight; do not replay"}
		}
	case 7:
		response.Result.Status = "native_refused"
		response.Failure = &controlCommandFailure{Code: "native_refused", Message: "ordinary native ability targeting or caster refused the request; do not replay automatically"}
	default:
		response.Result.Status = "unavailable"
		response.Failure = &controlCommandFailure{Code: "invalid_receipt", Message: "native window command returned an unknown receipt status"}
	}
	return response
}

func isControlMissionAction(operation string) bool {
	switch operation {
	case "mission.start", "mission.continue", "mission.campaign", "mission.previous", "mission.next", "mission.beam_out", "mission.accept_arena":
		return true
	default:
		return false
	}
}

func inspectControlWindow() controlWindow {
	var snapshot C.fang_control_window_snapshot
	isFound := C.fang_control_window_inspect(&snapshot)
	return controlWindow{IsAvailable: isFound != 0 && snapshot.is_available != 0,
		WindowID: fmt.Sprintf("%08x", uint32(snapshot.window_id)), ProcessID: uint32(snapshot.process_id), ThreadID: uint32(snapshot.thread_id),
		Width: int32(snapshot.width), Height: int32(snapshot.height), IsEnabled: snapshot.is_enabled != 0, IsMinimized: snapshot.is_minimized != 0}
}
