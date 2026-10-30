//go:build windows && cgo && scenario && fangdebug

package scenario

/*
#include "scenario/control.h"
*/
import "C"

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/buildinfo"
	"golang.org/x/sys/windows"
)

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
	AbilityID               uint32             `json:"ability_id,omitempty"`
	HeroNoun                uint32             `json:"hero_noun,omitempty"`
	IsImportEditor          bool               `json:"is_import_editor,omitempty"`
	Screen                  string             `json:"screen,omitempty"`
	ExpectedOwnerGeneration uint64             `json:"expected_owner_generation,omitempty"`
	WorldPoint              *controlWorldPoint `json:"world_point,omitempty"`
	HoldMS                  uint32             `json:"hold_ms,omitempty"`
	HeroSlot                *uint32            `json:"hero_slot,omitempty"`
	Text                    string             `json:"text,omitempty"`
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
	CinematicSnapshot       *controlCinematicSnapshot `json:"cinematic_snapshot,omitempty"`
	EditorHierarchy         *controlEditorArtifact    `json:"editor_hierarchy,omitempty"`
	ScreenState             *controlScreenState       `json:"screen_state,omitempty"`
	GameplayState           *controlGameplayState     `json:"gameplay_state,omitempty"`
	Projection              *controlProjection        `json:"projection,omitempty"`
	GameplayClick           *controlGameplayClick     `json:"gameplay_click,omitempty"`
	Status                  string                    `json:"status"`
	IsProcessed             bool                      `json:"is_processed"`
	IsGameplayComplete      bool                      `json:"is_gameplay_complete"`
	ThreadID                uint32                    `json:"thread_id"`
	CompletedTickMS         uint64                    `json:"completed_tick_ms"`
	Window                  *controlWindow            `json:"window,omitempty"`
	MapState                *controlMapState          `json:"map_state,omitempty"`
	ArenaLobby              *controlArenaLobbyState   `json:"arena_lobby,omitempty"`
	DeploymentState         *controlDeploymentState   `json:"deployment_state,omitempty"`
	BeamOutState            *controlBeamOutState      `json:"beam_out_state,omitempty"`
	HeroSelection           *controlHeroSelection     `json:"hero_selection,omitempty"`
	ActiveCast              *controlActiveCast        `json:"active_cast,omitempty"`
	ProcessedCharacterCount *uint32                   `json:"processed_character_count,omitempty"`
	IsInvocationAttempted   *bool                     `json:"is_invocation_attempted,omitempty"`
	IsInvocationAccepted    *bool                     `json:"is_invocation_accepted,omitempty"`
	IsIntroSkipped          *bool                     `json:"is_intro_skipped,omitempty"`
	IsCinematicSkipped      *bool                     `json:"is_cinematic_skipped,omitempty"`
}

type controlCommandFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

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

var controlStart sync.Once

// Start runs after installation, outside every native callback.
func Start() {
	controlStart.Do(startControlBootstrap)
}

func startControlBootstrap() {
	pipeName := os.Getenv("DARKSPIN_DEBUG_PIPE")
	token := os.Getenv("DARKSPIN_DEBUG_TOKEN")
	pipeErr := os.Unsetenv("DARKSPIN_DEBUG_PIPE")
	tokenErr := os.Unsetenv("DARKSPIN_DEBUG_TOKEN")
	if pipeErr != nil || tokenErr != nil || token == "" || !strings.HasPrefix(pipeName, `\\.\pipe\darkspin-`) {
		return
	}
	go runControlClient(pipeName, token)
}

func runControlClient(pipeName, token string) {
	r, err := openControlPipe(pipeName)
	if err != nil {
		return // Development bridge failure never changes native gameplay.
	}
	defer closeControlPipe(r)
	err = r.SetDeadline(time.Now().Add(5 * time.Second))
	if err != nil {
		return
	}
	err = writeControlEnvelope(r, controlEnvelope{ProtocolVersion: 1, Kind: "hello", Token: token, ProcessID: uint32(os.Getpid()), BuildID: buildinfo.ID})
	token = ""
	if err != nil {
		return
	}
	accepted, err := readControlEnvelope(r)
	if err != nil || accepted.ProtocolVersion != 1 || accepted.Kind != "accepted" || accepted.RunID == "" || accepted.ClientID == "" || accepted.ConnectionGeneration == "" {
		return
	}
	err = r.SetReadDeadline(time.Time{})
	if err != nil {
		return
	}
	connection := controlConnection{r: r, identity: accepted, commands: make(chan controlReceivedCommand, 1), done: make(chan struct{})}
	go connection.readCommands()
	err = connection.run()
	if err != nil {
		// The private transport closes without changing native game lifetime.
		return
	}
}

func closeControlPipe(r *os.File) {
	err := r.Close()
	if err != nil {
		// No native operation or retry depends on a failed debug-only close.
		return
	}
}

func openControlPipe(pipeName string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(pipeName)
	if err != nil {
		return nil, fmt.Errorf("pipeName: %w", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		handle, openErr := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED|windows.SECURITY_SQOS_PRESENT|windows.SECURITY_IDENTIFICATION, 0)
		if openErr == nil {
			return os.NewFile(uintptr(handle), "scenario-control"), nil
		}
		if time.Now().After(deadline) || (openErr != windows.ERROR_PIPE_BUSY && openErr != windows.ERROR_FILE_NOT_FOUND) {
			return nil, fmt.Errorf("pipeOpen: %w", openErr)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func observeControlState() controlState {
	var snapshot C.fang_control_snapshot
	C.fang_control_read(&snapshot)
	frame := controlFrame{controlField: controlField{IsAvailable: snapshot.frame_tick_ms != 0}, Sequence: uint32(snapshot.frame_sequence), ThreadID: uint32(snapshot.frame_thread_id), ObservedTickMS: uint64(snapshot.frame_tick_ms)}
	if !frame.IsAvailable {
		frame.Reason = "not_observed"
	}
	scene := controlScene{controlField: controlField{IsAvailable: snapshot.scene_load_return_count != 0 && snapshot.scene_asset_id != 0}, AssetID: uint32(snapshot.scene_asset_id), LoadReturnCount: uint32(snapshot.scene_load_return_count), FrameSequence: uint32(snapshot.scene_frame_sequence), ObservedTickMS: uint64(snapshot.scene_tick_ms)}
	if !scene.IsAvailable {
		scene.Reason = "not_observed"
	}
	return controlState{ProcessID: uint32(os.Getpid()), BuildID: buildinfo.ID, Frame: frame, Scene: scene, World: controlField{Reason: "not_observed"}, Hero: controlField{Reason: "not_observed"}, UI: controlField{Reason: "not_observed"}}
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
