package raknet

import (
	"encoding/binary"
	"fmt"
	"math"
)

type ActionCommand uint8

const (
	ActionReserved0           ActionCommand = 0
	ActionReserved1           ActionCommand = 1
	ActionReserved2           ActionCommand = 2
	ActionMovement            ActionCommand = 3
	ActionStopMovement        ActionCommand = 4
	ActionSwitchCharacter     ActionCommand = 5
	ActionOverdrive           ActionCommand = 6
	ActionUseCharacterAbility ActionCommand = 7
	ActionUseSquadAbility     ActionCommand = 8
	ActionCatalystPickup      ActionCommand = 9
	ActionCancel              ActionCommand = 10
	ActionUseInteractable     ActionCommand = 11
	ActionDance               ActionCommand = 12
)

type ActionCommon struct {
	Type                     ActionCommand
	Unknown                  [3]byte
	InputSyncStamp, ObjectID uint32
	Position                 Vector3
	Orientation              Quaternion
}
type ActionMovementData struct {
	Unknown             uint32
	GoalPosition        Vector3
	GoalFlags, Unknown2 uint32
}
type ActionCatalystData struct {
	TargetObjectID uint32
	Position       Vector3
	Selector       int8
}
type ActionAbilityData struct {
	TargetID                       uint32
	CursorPosition, TargetPosition Vector3
	Index                          uint32
	Rank                           int32
	Unknown, UserData              uint32
}
type ActionCommandData struct {
	Common   ActionCommon
	Movement *ActionMovementData
	Catalyst *ActionCatalystData
	Ability  *ActionAbilityData
	Value    uint32
	RawTail  []byte
}

func actionCommandTailLength(action ActionCommand) (int, bool) {
	switch action {
	case ActionReserved0, ActionDance:
		return 0, true
	case ActionReserved1:
		return 12, true
	case ActionReserved2:
		return 16, true
	case ActionMovement, ActionStopMovement, ActionCancel:
		return 24, true
	case ActionSwitchCharacter, ActionUseInteractable:
		return 4, true
	case ActionOverdrive:
		return 1, true
	case ActionUseCharacterAbility, ActionUseSquadAbility:
		return 44, true
	case ActionCatalystPickup:
		return 20, true
	default:
		return 0, false
	}
}

// DecodeActionCommand reads the exact packed little-endian client structure
// selected by build 103's native action-tail size switch.
func DecodeActionCommand(data []byte) (ActionCommandData, error) {
	if len(data) < 40 {
		return ActionCommandData{}, ErrShortBuffer
	}
	command := ActionCommandData{}
	command.Common.Type = ActionCommand(data[0])
	tailLength, isKnown := actionCommandTailLength(command.Common.Type)
	if !isKnown {
		return ActionCommandData{}, fmt.Errorf("actionType[%d]: %w", command.Common.Type, ErrInvalidPacket)
	}
	expectedLength := 40 + tailLength
	if len(data) < expectedLength {
		return ActionCommandData{}, fmt.Errorf(
			"actionLength[%d:%d]: %w", command.Common.Type, len(data), ErrShortBuffer,
		)
	}
	if len(data) > expectedLength {
		return ActionCommandData{}, fmt.Errorf(
			"actionLength[%d:%d]: %w", command.Common.Type, len(data), ErrInvalidPacket,
		)
	}
	if command.Common.Type <= ActionReserved2 {
		return ActionCommandData{}, fmt.Errorf(
			"actionReserved[%d]: %w", command.Common.Type, ErrInvalidPacket,
		)
	}
	copy(command.Common.Unknown[:], data[1:4])
	command.Common.InputSyncStamp = binary.LittleEndian.Uint32(data[4:8])
	command.Common.ObjectID = binary.LittleEndian.Uint32(data[8:12])
	command.Common.Position = littleVector3(data[12:24])
	command.Common.Orientation = littleQuaternion(data[24:40])
	payload := data[40:]
	command.RawTail = append([]byte(nil), payload...)
	switch command.Common.Type {
	case ActionMovement:
		command.Movement = &ActionMovementData{Unknown: binary.LittleEndian.Uint32(payload[0:4]), GoalPosition: littleVector3(payload[4:16]), GoalFlags: binary.LittleEndian.Uint32(payload[16:20]), Unknown2: binary.LittleEndian.Uint32(payload[20:24])}
	case ActionUseCharacterAbility, ActionUseSquadAbility:
		command.Ability = &ActionAbilityData{TargetID: binary.LittleEndian.Uint32(payload[0:4]), CursorPosition: littleVector3(payload[4:16]), TargetPosition: littleVector3(payload[16:28]), Index: binary.LittleEndian.Uint32(payload[28:32]), Rank: int32(binary.LittleEndian.Uint32(payload[32:36])), Unknown: binary.LittleEndian.Uint32(payload[36:40]), UserData: binary.LittleEndian.Uint32(payload[40:44])}
	case ActionCatalystPickup:
		command.Catalyst = &ActionCatalystData{
			TargetObjectID: binary.LittleEndian.Uint32(payload[0:4]),
			Position:       littleVector3(payload[4:16]), Selector: int8(payload[16]),
		}
	case ActionSwitchCharacter, ActionUseInteractable:
		command.Value = binary.LittleEndian.Uint32(payload)
	}
	return command, nil
}

type PlayerStatus struct {
	Status   uint32
	Progress float32
}

func DecodePlayerStatus(data []byte) (PlayerStatus, error) {
	if len(data) < 8 {
		return PlayerStatus{}, ErrShortBuffer
	}
	return PlayerStatus{Status: binary.LittleEndian.Uint32(data[:4]), Progress: math.Float32frombits(binary.LittleEndian.Uint32(data[4:8]))}, nil
}

type CrystalDragOperation int32

const (
	CrystalDragIntoWorld        CrystalDragOperation = 0
	CrystalDragIntoWorldAtPoint CrystalDragOperation = 1
	CrystalDragIntoSlot         CrystalDragOperation = 2
)

type CrystalDragCommand struct {
	SourceSlot      int32
	Operation       CrystalDragOperation
	WorldPosition   Vector3
	DestinationSlot int32
}

// DecodeCrystalDrag reads the fixed 24-byte MaxisHUDCatalysts request. The
// position is consumed only by operation 1 and the destination only by
// operation 2, but both fields remain present in every sender body.
func DecodeCrystalDrag(data []byte) (CrystalDragCommand, error) {
	if len(data) < 24 {
		return CrystalDragCommand{}, ErrShortBuffer
	}
	if len(data) > 24 {
		return CrystalDragCommand{}, ErrInvalidPacket
	}
	command := CrystalDragCommand{
		SourceSlot:      int32(binary.LittleEndian.Uint32(data[0:4])),
		Operation:       CrystalDragOperation(int32(binary.LittleEndian.Uint32(data[4:8]))),
		WorldPosition:   littleVector3(data[8:20]),
		DestinationSlot: int32(binary.LittleEndian.Uint32(data[20:24])),
	}
	switch command.Operation {
	case CrystalDragIntoWorld, CrystalDragIntoWorldAtPoint, CrystalDragIntoSlot:
		return command, nil
	default:
		return CrystalDragCommand{}, fmt.Errorf(
			"crystalDragOperation[%d]: %w", command.Operation, ErrInvalidPacket,
		)
	}
}

type ChainPlayerCommandType uint8

const (
	ChainPlayerRequestVoteData    ChainPlayerCommandType = 0
	ChainPlayerSelectContinue     ChainPlayerCommandType = 1
	ChainPlayerSelectCashOut      ChainPlayerCommandType = 2
	ChainPlayerRequestCashOutData ChainPlayerCommandType = 4
	ChainPlayerSlotUpdate         ChainPlayerCommandType = 7
	ChainPlayerFlagUpdate         ChainPlayerCommandType = 8
)

type ChainPlayerCommand struct {
	Type             ChainPlayerCommandType
	Choice           uint8
	SelectedRecordID uint32
	PlayerSlot       uint8
	PlayerFlag       bool
}

// DecodeChainPlayerCommand validates every build-103 client sender shape in
// the ChainPlayer family. Meaning beyond the proven slot/flag wire fields is
// owned by the active chain state.
func DecodeChainPlayerCommand(data []byte) (ChainPlayerCommand, error) {
	if len(data) == 0 {
		return ChainPlayerCommand{}, ErrShortBuffer
	}
	command := ChainPlayerCommand{Type: ChainPlayerCommandType(data[0])}
	switch command.Type {
	case ChainPlayerRequestVoteData, ChainPlayerSelectCashOut, ChainPlayerRequestCashOutData:
		if len(data) != 1 {
			return ChainPlayerCommand{}, fmt.Errorf(
				"chainPlayerLength[%d:%d]: %w", command.Type, len(data), ErrInvalidPacket,
			)
		}
	case ChainPlayerSelectContinue:
		if len(data) < 6 {
			return ChainPlayerCommand{}, ErrShortBuffer
		}
		if len(data) > 6 {
			return ChainPlayerCommand{}, ErrInvalidPacket
		}
		command.Choice = data[1]
		command.SelectedRecordID = binary.LittleEndian.Uint32(data[2:6])
	case ChainPlayerSlotUpdate:
		if len(data) != 2 {
			return ChainPlayerCommand{}, fmt.Errorf(
				"chainPlayerLength[%d:%d]: %w", command.Type, len(data), ErrInvalidPacket,
			)
		}
		command.PlayerSlot = data[1]
	case ChainPlayerFlagUpdate:
		if len(data) != 2 {
			return ChainPlayerCommand{}, fmt.Errorf(
				"chainPlayerLength[%d:%d]: %w", command.Type, len(data), ErrInvalidPacket,
			)
		}
		if data[1] > 1 {
			return ChainPlayerCommand{}, fmt.Errorf(
				"chainPlayerFlag[%d]: %w", data[1], ErrInvalidPacket,
			)
		}
		command.PlayerFlag = data[1] != 0
	default:
		return ChainPlayerCommand{}, fmt.Errorf(
			"chainPlayerType[%d]: %w", command.Type, ErrInvalidPacket,
		)
	}
	return command, nil
}

type ArenaPlayerCommand struct {
	Type       uint8
	IsAccepted bool
	DeckID     int32
}

const (
	ArenaPlayerEnterLobby uint8 = iota
	ArenaPlayerEnterResults
	ArenaPlayerAcceptMission
)

// DecodeArenaPlayerCommand validates all three build-103 ArenaPlayer senders.
// Types zero and one are bodyless result/lobby transitions. Type two is the
// ArenaLobby.AcceptMission request and carries the accepted flag plus the
// selected PVP deck ID.
func DecodeArenaPlayerCommand(data []byte) (ArenaPlayerCommand, error) {
	if len(data) == 0 {
		return ArenaPlayerCommand{}, ErrShortBuffer
	}
	command := ArenaPlayerCommand{Type: data[0]}
	switch command.Type {
	case 0, 1:
		if len(data) != 1 {
			return ArenaPlayerCommand{}, fmt.Errorf(
				"arenaPlayerLength[%d:%d]: %w", command.Type, len(data), ErrInvalidPacket,
			)
		}
	case 2:
		if len(data) < 6 {
			return ArenaPlayerCommand{}, ErrShortBuffer
		}
		if len(data) > 6 || data[1] != 1 {
			return ArenaPlayerCommand{}, ErrInvalidPacket
		}
		command.IsAccepted = true
		command.DeckID = int32(binary.LittleEndian.Uint32(data[2:6]))
	default:
		return ArenaPlayerCommand{}, fmt.Errorf(
			"arenaPlayerType[%d]: %w", command.Type, ErrInvalidPacket,
		)
	}
	return command, nil
}

type JuggernautPlayerCommand struct {
	Type uint8
}

// DecodeJuggernautPlayerCommand preserves the one-byte bodies emitted by both
// build-103 constructors. Their allocation capacities do not add wire bytes.
func DecodeJuggernautPlayerCommand(data []byte) (JuggernautPlayerCommand, error) {
	if len(data) == 0 {
		return JuggernautPlayerCommand{}, ErrShortBuffer
	}
	command := JuggernautPlayerCommand{Type: data[0]}
	switch command.Type {
	case 0, 1:
		if len(data) != 1 {
			return JuggernautPlayerCommand{}, ErrInvalidPacket
		}
	default:
		return JuggernautPlayerCommand{}, fmt.Errorf(
			"juggernautPlayerType[%d]: %w", command.Type, ErrInvalidPacket,
		)
	}
	return command, nil
}

type KillRacePlayerCommand struct {
	Type     uint8
	IsActive bool
	Reserved [4]byte
}

// DecodeKillRacePlayerCommand validates both build-103 constructors. Type one
// is bodyless. Type three carries a true flag followed by a retained dword
// whose meaning is not consumed by the recovered client sender.
func DecodeKillRacePlayerCommand(data []byte) (KillRacePlayerCommand, error) {
	if len(data) == 0 {
		return KillRacePlayerCommand{}, ErrShortBuffer
	}
	command := KillRacePlayerCommand{Type: data[0]}
	switch command.Type {
	case 1:
		if len(data) != 1 {
			return KillRacePlayerCommand{}, ErrInvalidPacket
		}
	case 3:
		if len(data) < 6 {
			return KillRacePlayerCommand{}, ErrShortBuffer
		}
		if len(data) > 6 || data[1] != 1 {
			return KillRacePlayerCommand{}, ErrInvalidPacket
		}
		command.IsActive = true
		copy(command.Reserved[:], data[2:6])
	default:
		return KillRacePlayerCommand{}, fmt.Errorf(
			"killRacePlayerType[%d]: %w", command.Type, ErrInvalidPacket,
		)
	}
	return command, nil
}

// GameType is the gameplay mode stored by the client simulator. Zero is not a
// valid value and sends the retail client down an incompatible UI path.
type GameType uint32

const (
	GameTypeTutorial GameType = iota + 1
	GameTypeChain
	GameTypeArena
	GameTypeKillRace
	GameTypeJuggernaut
	GameTypeQuickplay
	GameTypeDirectEntry
)

type GameStateData struct {
	GameTime    uint64
	TimeElapsed uint64
	State       GameState
	Type        GameType
}

func (g GameStateData) Encode() []byte {
	payload := make([]byte, 25)
	binary.LittleEndian.PutUint64(payload[0:8], g.GameTime)
	binary.LittleEndian.PutUint64(payload[8:16], g.TimeElapsed)
	payload[16] = byte(g.State)
	binary.LittleEndian.PutUint32(payload[17:21], uint32(g.Type))
	binary.LittleEndian.PutUint32(payload[21:25], 1)
	return payload
}

type Client struct {
	ID      uint8
	BlazeID uint64
	State   GameState
	Status  PlayerStatus
}

func NewClient(id uint8) *Client { return &Client{ID: id, State: GameStateInvalid} }
func (c *Client) SetState(state GameState) error {
	if !ValidStateChange(c.State, state) {
		return fmt.Errorf("invalid game state transition %d to %d", c.State, state)
	}
	c.State = state
	return nil
}

func littleVector3(data []byte) Vector3 {
	return Vector3{X: math.Float32frombits(binary.LittleEndian.Uint32(data[0:4])), Y: math.Float32frombits(binary.LittleEndian.Uint32(data[4:8])), Z: math.Float32frombits(binary.LittleEndian.Uint32(data[8:12]))}
}
func littleQuaternion(data []byte) Quaternion {
	return Quaternion{X: math.Float32frombits(binary.LittleEndian.Uint32(data[0:4])), Y: math.Float32frombits(binary.LittleEndian.Uint32(data[4:8])), Z: math.Float32frombits(binary.LittleEndian.Uint32(data[8:12])), W: math.Float32frombits(binary.LittleEndian.Uint32(data[12:16]))}
}
