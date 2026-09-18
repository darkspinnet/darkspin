package raknet

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net"
	"slices"
	"time"
)

var ErrInvalidPacket = errors.New("raknet: invalid packet")

// ApplicationMessage is a Game gameplay message carried by RakNet.
type ApplicationMessage interface {
	PacketID() PacketID
	EncodePayload() []byte
}

type applicationValidator interface {
	validateApplication() error
}

// MarshalApplication prefixes a gameplay payload with its Game packet ID.
func MarshalApplication(message ApplicationMessage) ([]byte, error) {
	if message == nil {
		return nil, fmt.Errorf("messageNil: %w", ErrInvalidPacket)
	}
	validator, isValidator := message.(applicationValidator)
	if isValidator {
		err := validator.validateApplication()
		if err != nil {
			return nil, fmt.Errorf("messageValidate: %w", err)
		}
	}
	payload := message.EncodePayload()
	packet := make([]byte, 1+len(payload))
	packet[0] = byte(message.PacketID())
	copy(packet[1:], payload)
	return packet, nil
}

type EmptyMessage struct{ ID PacketID }

func (m EmptyMessage) PacketID() PacketID    { return m.ID }
func (m EmptyMessage) EncodePayload() []byte { return nil }

// ConnectedMessage tells build 103 to begin the application handshake. Its
// client handler sends HelloPlayerRequest; it has no application payload.
type ConnectedMessage struct{}

func (ConnectedMessage) PacketID() PacketID    { return Connected }
func (ConnectedMessage) EncodePayload() []byte { return nil }

type HelloPlayerRequestMessage struct {
	UserID             uint64
	PlaygroupID        uint64
	IsPlaygroupPresent bool
}

func DecodeHelloPlayerRequest(payload []byte) (HelloPlayerRequestMessage, error) {
	if len(payload) < 8 {
		return HelloPlayerRequestMessage{}, fmt.Errorf("helloRead: %w", ErrShortBuffer)
	}
	if len(payload) != 8 && len(payload) != 16 {
		return HelloPlayerRequestMessage{}, fmt.Errorf("helloLength[%d]: %w", len(payload), ErrInvalidPacket)
	}
	message := HelloPlayerRequestMessage{UserID: binary.LittleEndian.Uint64(payload[:8])}
	if len(payload) == 16 {
		message.PlaygroupID = binary.LittleEndian.Uint64(payload[8:16])
		message.IsPlaygroupPresent = true
	}
	return message, nil
}

func DecodeTimestamp(payload []byte) (uint64, error) {
	if len(payload) < 8 {
		return 0, fmt.Errorf("timestampRead: %w", ErrShortBuffer)
	}
	if len(payload) != 8 {
		return 0, fmt.Errorf("timestampLength[%d]: %w", len(payload), ErrInvalidPacket)
	}
	return binary.LittleEndian.Uint64(payload), nil
}

// LootDropRequestMessage asks the authoritative inventory owner to drop one
// persistent item onto the ground. Build 103 copies the first uint64 from the selected part
// record, which is the item ID exposed by the account inventory response.
type LootDropRequestMessage struct {
	ItemID uint64
}

func DecodeLootDropRequest(payload []byte) (LootDropRequestMessage, error) {
	if len(payload) != 8 {
		return LootDropRequestMessage{}, fmt.Errorf("lootDropLength[%d]: %w", len(payload), ErrInvalidPacket)
	}
	return LootDropRequestMessage{ItemID: binary.LittleEndian.Uint64(payload)}, nil
}

func (m HelloPlayerRequestMessage) PacketID() PacketID { return HelloPlayerRequest }
func (m HelloPlayerRequestMessage) EncodePayload() []byte {
	length := 8
	if m.IsPlaygroupPresent {
		length = 16
	}
	payload := make([]byte, length)
	binary.LittleEndian.PutUint64(payload[:8], m.UserID)
	if m.IsPlaygroupPresent {
		binary.LittleEndian.PutUint64(payload[8:16], m.PlaygroupID)
	}
	return payload
}

// HelloPlayerMessage identifies the client's slot and gameplay endpoint. The
// IPv4 address is copied as network bytes; the port is a gameplay scalar (LE).
type HelloPlayerMessage struct {
	PlayerType    uint8
	GameplayIndex uint8
	Address       net.IP
	Port          uint16
}

func (m HelloPlayerMessage) PacketID() PacketID { return HelloPlayer }
func (m HelloPlayerMessage) EncodePayload() []byte {
	payload := make([]byte, 8)
	payload[0] = m.PlayerType
	payload[1] = m.GameplayIndex
	ip := m.Address.To4()
	if ip != nil {
		copy(payload[2:6], ip)
	}
	binary.LittleEndian.PutUint16(payload[6:8], m.Port)
	return payload
}

type StateMessage struct{ State GameState }

func (m StateMessage) PacketID() PacketID { return ReconnectPlayer }
func (m StateMessage) EncodePayload() []byte {
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, uint32(m.State))
	return payload
}

type GameStateMessage struct {
	Data GameStateData
}

func (GameStateMessage) PacketID() PacketID { return GameStatePacket }
func (m GameStateMessage) EncodePayload() []byte {
	return m.Data.Encode()
}

type PlayerSlotMessage struct {
	ID   PacketID
	Slot uint8
}

func (m PlayerSlotMessage) PacketID() PacketID    { return m.ID }
func (m PlayerSlotMessage) EncodePayload() []byte { return []byte{m.Slot} }

// VoteKickStartedMessage identifies the player shown in the vote-kick prompt.
// Build 103 reads but does not use the first byte.
type VoteKickStartedMessage struct {
	Unused           uint8
	TargetPlayerSlot uint8
}

func (VoteKickStartedMessage) PacketID() PacketID { return VoteKickStarted }
func (m VoteKickStartedMessage) EncodePayload() []byte {
	return []byte{m.Unused, m.TargetPlayerSlot}
}

type TimestampMessage struct {
	ID        PacketID
	Timestamp uint64
}

func (m TimestampMessage) PacketID() PacketID { return m.ID }
func (m TimestampMessage) EncodePayload() []byte {
	payload := make([]byte, 8)
	binary.LittleEndian.PutUint64(payload, m.Timestamp)
	return payload
}

// DirectorStateMessage encodes the build-103 AI director reflection fields
// needed by the tutorial. Field 3 is mbBossComplete; the client uses it to
// signal nGameDirector.IsBossDead and reveal the normal Beam Out HUD.
type DirectorStateMessage struct {
	IsBossSpawned            bool
	IsBossHorde              bool
	IsCaptainSpawned         bool
	IsBossComplete           bool
	IsHordeSpawned           bool
	IsHordeSpawnedPresent    bool
	BossID                   uint32
	ActiveHordeWaveHandle    uint32
	IsActiveHordeWavePresent bool
}

func (DirectorStateMessage) PacketID() PacketID { return DirectorState }
func (m DirectorStateMessage) EncodePayload() []byte {
	mask := byte(0)
	if m.IsBossSpawned {
		mask |= 1 << 0
	}
	if m.IsBossHorde {
		mask |= 1 << 1
	}
	if m.IsCaptainSpawned {
		mask |= 1 << 2
	}
	if m.IsBossComplete {
		mask |= 1 << 3
	}
	if m.IsHordeSpawned || m.IsHordeSpawnedPresent {
		mask |= 1 << 4
	}
	if m.BossID != 0 {
		mask |= 1 << 5
	}
	if m.IsActiveHordeWavePresent {
		mask |= 1 << 6
	}
	if mask == 0 {
		return []byte{0}
	}
	payload := []byte{mask}
	boolFields := [...]struct {
		bit   byte
		isSet bool
	}{
		{bit: 1 << 0, isSet: m.IsBossSpawned},
		{bit: 1 << 1, isSet: m.IsBossHorde},
		{bit: 1 << 2, isSet: m.IsCaptainSpawned},
		{bit: 1 << 3, isSet: m.IsBossComplete},
		{bit: 1 << 4, isSet: m.IsHordeSpawned},
	}
	for _, field := range boolFields {
		if mask&field.bit != 0 {
			payload = append(payload, boolByte(field.isSet))
		}
	}
	if m.BossID != 0 {
		payload = binary.LittleEndian.AppendUint32(payload, m.BossID)
	}
	if m.IsActiveHordeWavePresent {
		payload = binary.LittleEndian.AppendUint32(payload, m.ActiveHordeWaveHandle)
	}
	return payload
}

// GameOverMessage selects build 103's GameOver client state from active
// gameplay through ChainGameMsgs subtype 1.
type GameOverMessage struct{}

func (GameOverMessage) PacketID() PacketID    { return ChainGameMsgs }
func (GameOverMessage) EncodePayload() []byte { return []byte{1} }

type ChainGameTransitionMessage struct {
	Subtype uint8
}

func (ChainGameTransitionMessage) PacketID() PacketID { return ChainGameMsgs }
func (m ChainGameTransitionMessage) validateApplication() error {
	if m.Subtype != 0 && m.Subtype != 1 && m.Subtype != 3 {
		return fmt.Errorf("chainGameSubtype[%d]: %w", m.Subtype, ErrInvalidPacket)
	}
	return nil
}
func (m ChainGameTransitionMessage) EncodePayload() []byte {
	return []byte{m.Subtype}
}

type ChainGameOverMessage struct {
	Subtype uint8
}

func (ChainGameOverMessage) PacketID() PacketID { return ChainGameOverMsgs }
func (m ChainGameOverMessage) validateApplication() error {
	if m.Subtype > 1 {
		return fmt.Errorf("chainGameOverSubtype[%d]: %w", m.Subtype, ErrInvalidPacket)
	}
	return nil
}
func (m ChainGameOverMessage) EncodePayload() []byte {
	return []byte{m.Subtype}
}

// ReturnToSpaceshipMessage selects client state 2 through QuickGame subtype 0.
// ChainGameOverMsgs is only a no-op consumer in cGameOverState.
type ReturnToSpaceshipMessage struct{}

func (ReturnToSpaceshipMessage) PacketID() PacketID    { return QuickGameMsgs }
func (ReturnToSpaceshipMessage) EncodePayload() []byte { return []byte{0} }

// ReloadLevelMessage requests build 103's in-place level teardown and reload.
// Its application packet has no payload and retains the current RakNet peer.
type ReloadLevelMessage struct{}

func (ReloadLevelMessage) PacketID() PacketID    { return ReloadLevel }
func (ReloadLevelMessage) EncodePayload() []byte { return nil }

// CinematicMessage is the fixed build-103 CinematicMsgs payload. Duration is
// signed on the wire; the retail receiver ignores nonpositive durations.
type CinematicMessage struct {
	DurationMilliseconds int64
	Focus                Vector3
	Radius               float32
}

// NewCinematicMessage converts a Go duration to the wire's integer-millisecond
// clock using nearest-millisecond rounding. time.Duration.Round defines exact
// half milliseconds to round away from zero.
func NewCinematicMessage(duration time.Duration, focus Vector3, radius float32) CinematicMessage {
	return CinematicMessage{
		DurationMilliseconds: duration.Round(time.Millisecond).Milliseconds(),
		Focus:                focus,
		Radius:               radius,
	}
}

func (CinematicMessage) PacketID() PacketID { return CinematicMsgs }
func (m CinematicMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint64(nil, uint64(m.DurationMilliseconds))
	payload = appendVector3(payload, m.Focus.X, m.Focus.Y, m.Focus.Z)
	return binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.Radius))
}

type ObjectCreateMessage struct {
	Rotation           Vector3
	ObjectID           uint32
	Noun               uint32
	AssetID            uint64
	PositionX          float32
	PositionY          float32
	PositionZ          float32
	Scale              float32
	Team               uint8
	PlayerIndex        uint8
	OwnerID            uint32
	IsCollisionEnabled bool
	IsPlayerControlled bool
}

type EnemyObjectCreateMessage struct {
	ObjectID     uint32
	Noun         uint32
	Position     Vector3
	Rotation     Vector3
	Scale        float32
	IsCollidable bool
	MovementType uint8
}

// ProjectileObjectCreateMessage is the generic build-103 projectile creation
// snapshot. MovementType is omitted for ordinary launch and published for
// specialized locomotion such as retained-target homing.
type ProjectileObjectCreateMessage struct {
	ObjectID       uint32
	Noun           uint32
	Position       Vector3
	Rotation       Vector3
	Orientation    Quaternion
	LinearVelocity Vector3
	Scale          float32
	Team           uint8
	MovementType   uint8
}

func (ProjectileObjectCreateMessage) PacketID() PacketID { return ObjectCreate }
func (m ProjectileObjectCreateMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	payload = binary.LittleEndian.AppendUint16(payload, 0x03ff)
	payload = binary.LittleEndian.AppendUint32(payload, m.Noun)
	payload = appendVector3(payload, m.Position.X, m.Position.Y, m.Position.Z)
	payload = appendVector3(payload, m.Rotation.X, m.Rotation.Y, m.Rotation.Z)
	payload = binary.LittleEndian.AppendUint64(payload, 0)
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.Scale))
	payload = append(payload, m.Team, 0, 0)
	payload = append(payload, 0, m.Team)
	payload = append(payload, 4)
	payload = appendVector3(payload, m.LinearVelocity.X, m.LinearVelocity.Y, m.LinearVelocity.Z)
	payload = append(payload, 6)
	payload = appendVector3(payload, m.Position.X, m.Position.Y, m.Position.Z)
	payload = append(payload, 7)
	payload = appendQuaternion(payload, m.Orientation)
	if m.MovementType != 0 {
		payload = append(payload, 19, m.MovementType)
	}
	return append(payload, 0xff)
}

type ProjectileParameter struct {
	Speed                      float32
	Acceleration               float32
	JinkInfo                   uint32
	Range                      float32
	SpinRate                   float32
	Direction                  Vector3
	Flags                      uint8
	HomingDelay                float32
	TurnRate                   float32
	TurnAcceleration           float32
	IsPiercing                 bool
	IsGroundCollisionIgnored   bool
	IsCreatureCollisionIgnored bool
	Eccentricity               float32
	CombatantSweepHeight       float32
}

// LobParameter is the exact 84-byte build-103 cLobParams memory image carried
// by cLocomotionData reflection field two. Unreflected trajectory inputs remain
// part of the nested image because the parent reflection copies the registered
// structure size, as it does for cProjectileParams.
type LobParameter struct {
	StartPosition                Vector3
	Destination                  Vector3
	UpDirection                  Vector3
	PlaneDirectionVelocity       float32
	Height                       float32
	DurationSecond               float32
	BounceNumber                 int32
	BounceRestitution            float32
	IsGroundCollisionOnly        bool
	IsStopBounceOnCreature       bool
	PlaneDirection               Vector3
	ReflectedPlaneDirectionSpeed float32
	UpLinearParameter            float32
	UpQuadraticParameter         float32
}

// LobLocomotionMessage is the receiver-exact sparse 0x94 component update
// dirtied by build-103's generic lob initializer. This type deliberately does
// not choose its ordering relative to object creation or another component;
// the original authoritative sender is absent from the client-shaped executable.
type LobLocomotionMessage struct {
	ObjectID              uint32
	StartTimeMilliseconds uint64
	PreviousSpeedModifier float32
	Parameter             LobParameter
}

func (LobLocomotionMessage) PacketID() PacketID { return LocomotionUpdate }
func (m LobLocomotionMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	payload = append(payload, 0)
	payload = binary.LittleEndian.AppendUint64(payload, m.StartTimeMilliseconds)
	payload = append(payload, 1)
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.PreviousSpeedModifier))
	payload = append(payload, 2)
	payload = appendLobParameter(payload, m.Parameter)
	return append(payload, 0xff)
}

func appendLobParameter(payload []byte, parameter LobParameter) []byte {
	payload = appendVector3(payload, parameter.StartPosition.X, parameter.StartPosition.Y,
		parameter.StartPosition.Z)
	payload = appendVector3(payload, parameter.Destination.X, parameter.Destination.Y,
		parameter.Destination.Z)
	payload = appendVector3(payload, parameter.UpDirection.X, parameter.UpDirection.Y,
		parameter.UpDirection.Z)
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(parameter.PlaneDirectionVelocity))
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(parameter.Height))
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(parameter.DurationSecond))
	payload = binary.LittleEndian.AppendUint32(payload, uint32(parameter.BounceNumber))
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(parameter.BounceRestitution))
	payload = append(payload, boolByte(parameter.IsGroundCollisionOnly),
		boolByte(parameter.IsStopBounceOnCreature), 0, 0)
	payload = appendVector3(payload, parameter.PlaneDirection.X, parameter.PlaneDirection.Y,
		parameter.PlaneDirection.Z)
	payload = binary.LittleEndian.AppendUint32(payload,
		math.Float32bits(parameter.ReflectedPlaneDirectionSpeed))
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(parameter.UpLinearParameter))
	return binary.LittleEndian.AppendUint32(payload, math.Float32bits(parameter.UpQuadraticParameter))
}

// ProjectileLocomotionMessage is the reliable 0x94 projectile component
// snapshot. ExpectedGeometryCollision is optional field 14.
type ProjectileLocomotionMessage struct {
	ObjectID                  uint32
	Parameter                 ProjectileParameter
	ExpectedGeometryCollision *Vector3
	ReflectedLastUpdate       int32
}

func (ProjectileLocomotionMessage) PacketID() PacketID { return LocomotionUpdate }
func (m ProjectileLocomotionMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	payload = append(payload, 3)
	payload = appendProjectileParameter(payload, m.Parameter)
	if m.ExpectedGeometryCollision != nil {
		payload = append(payload, 14)
		payload = appendVector3(payload, m.ExpectedGeometryCollision.X,
			m.ExpectedGeometryCollision.Y, m.ExpectedGeometryCollision.Z)
	}
	payload = append(payload, 17)
	payload = binary.LittleEndian.AppendUint32(payload, uint32(m.ReflectedLastUpdate))
	return append(payload, 0xff)
}

func appendProjectileParameter(payload []byte, parameter ProjectileParameter) []byte {
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(parameter.Speed))
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(parameter.Acceleration))
	payload = binary.LittleEndian.AppendUint32(payload, parameter.JinkInfo)
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(parameter.Range))
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(parameter.SpinRate))
	payload = appendVector3(payload, parameter.Direction.X, parameter.Direction.Y, parameter.Direction.Z)
	payload = append(payload, parameter.Flags, 0, 0, 0)
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(parameter.HomingDelay))
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(parameter.TurnRate))
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(parameter.TurnAcceleration))
	payload = append(payload, boolByte(parameter.IsPiercing), boolByte(parameter.IsGroundCollisionIgnored),
		boolByte(parameter.IsCreatureCollisionIgnored), 0)
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(parameter.Eccentricity))
	return binary.LittleEndian.AppendUint32(payload, math.Float32bits(parameter.CombatantSweepHeight))
}

func (EnemyObjectCreateMessage) PacketID() PacketID { return ObjectCreate }
func (m EnemyObjectCreateMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	payload = binary.LittleEndian.AppendUint16(payload, 0x03ff)
	payload = binary.LittleEndian.AppendUint32(payload, m.Noun)
	payload = appendVector3(payload, m.Position.X, m.Position.Y, m.Position.Z)
	payload = appendVector3(payload, m.Rotation.X, m.Rotation.Y, m.Rotation.Z)
	payload = binary.LittleEndian.AppendUint64(payload, 0)
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.Scale))
	payload = append(payload, 0, boolByte(m.IsCollidable), 0)
	payload = append(payload, 6)
	payload = appendVector3(payload, m.Position.X, m.Position.Y, m.Position.Z)
	// The create prefix owns rotation; an identity reflection would overwrite it.
	if m.MovementType != 0 {
		payload = append(payload, 19, m.MovementType)
	}
	return append(payload, 0xff)
}

func (ObjectCreateMessage) PacketID() PacketID { return ObjectCreate }
func (m ObjectCreateMessage) EncodePayload() []byte {
	payload := make([]byte, 0, 58)
	payload = binary.LittleEndian.AppendUint32(payload, m.ObjectID)
	payload = binary.LittleEndian.AppendUint16(payload, 0x03ff)
	payload = binary.LittleEndian.AppendUint32(payload, m.Noun)
	payload = appendVector3(payload, m.PositionX, m.PositionY, m.PositionZ)
	payload = appendVector3(payload, m.Rotation.X, m.Rotation.Y, m.Rotation.Z)
	payload = binary.LittleEndian.AppendUint64(payload, m.AssetID)
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.Scale))
	payload = append(payload, m.Team, boolByte(m.IsCollisionEnabled), boolByte(m.IsPlayerControlled))
	payload = append(payload, 0, m.Team)
	payload = append(payload, 1, boolByte(m.IsPlayerControlled))
	payload = append(payload, 3, m.PlayerIndex)
	payload = append(payload, 17, boolByte(m.IsCollisionEnabled))
	if m.OwnerID != 0 {
		payload = append(payload, 18)
		payload = binary.LittleEndian.AppendUint32(payload, m.OwnerID)
	}
	payload = append(payload, 0xff)
	return payload
}

type ObjectUpdateMessage struct {
	ObjectID  uint32
	PositionX float32
	PositionY float32
	PositionZ float32
	IsVisible bool
}

// ObjectPositionUpdateMessage publishes only reflected object field 6. It
// synchronizes an authoritative transform without changing visibility.
type ObjectPositionUpdateMessage struct {
	ObjectID  uint32
	PositionX float32
	PositionY float32
	PositionZ float32
}

// ObjectCollisionUpdateMessage publishes only reflected object field 17. It is
// used when authoritative physics/navigation collision changes without a pose
// or visibility mutation.
type ObjectCollisionUpdateMessage struct {
	ObjectID           uint32
	IsCollisionEnabled bool
}

// ServerEventMessage encodes the build-103 reflection fields needed to attach
// a ServerEventDef presentation effect to an object at an authored position.
// The complete registered structure contains 26 fields; unsupported fields
// remain at their client defaults and are deliberately omitted.
type ServerEventMessage struct {
	Asset         uint32
	ObjectID      uint32
	Position      Vector3
	TextValue     uint32
	ClientEventID uint32
	Loot          *ServerEventLoot
}

// ChainedEffectMessage is the fields-6/7/8 ServerEvent shape used by effects
// that connect one replicated object to another, including Arc Weld beams.
type ChainedEffectMessage struct {
	Asset             uint32
	ObjectID          uint32
	SecondaryObjectID uint32
}

func (ChainedEffectMessage) PacketID() PacketID { return ServerEvent }
func (m ChainedEffectMessage) EncodePayload() []byte {
	payload := []byte{6}
	payload = binary.LittleEndian.AppendUint32(payload, m.Asset)
	payload = append(payload, 7)
	payload = binary.LittleEndian.AppendUint32(payload, m.ObjectID)
	payload = append(payload, 8)
	payload = binary.LittleEndian.AppendUint32(payload, m.SecondaryObjectID)
	return append(payload, 0xff)
}

// AttachedEffectMessage is the build-103 reflected ServerEvent shape used by
// AddEffect and RemoveEffectIndex. Slot is one-based on the wire.
type AttachedEffectMessage struct {
	Slot               uint8
	IsRemovalRequested bool
	IsHardStop         bool
	IsForceAttached    bool
	Asset              uint32
	ObjectID           uint32
	SecondaryObjectID  uint32
}

func (AttachedEffectMessage) PacketID() PacketID { return ServerEvent }
func (m AttachedEffectMessage) EncodePayload() []byte {
	payload := []byte{1, m.Slot}
	if m.IsRemovalRequested {
		payload = append(payload, 2, 1)
	}
	if m.IsHardStop {
		payload = append(payload, 3, 1)
	}
	if m.IsForceAttached {
		payload = append(payload, 4, 1)
	}
	if m.Asset != 0 {
		payload = append(payload, 6)
		payload = binary.LittleEndian.AppendUint32(payload, m.Asset)
	}
	payload = append(payload, 7)
	payload = binary.LittleEndian.AppendUint32(payload, m.ObjectID)
	if m.SecondaryObjectID != 0 {
		payload = append(payload, 8)
		payload = binary.LittleEndian.AppendUint32(payload, m.SecondaryObjectID)
	}
	return append(payload, 0xff)
}

// PositionedEffectMessage carries asset/position/facing for projectile effects.
// Vertical directions use field 12 orientation instead of field 11 facing to
// avoid the client's singular facing/world-up cross product.
type PositionedEffectMessage struct {
	Asset    uint32
	Position Vector3
	Facing   Vector3
}

// DebugEffectPreviewMessage is a Fang-only ServerEvent envelope. Retail-normal
// clients treat the reserved client event ID as an unknown local event and do
// not receive a normal effect request. Fang recognizes both magic fields,
// clears them, resolves the controlled object and live position, and invokes
// the generic world-positioned presentation path.
type DebugEffectPreviewMessage struct {
	Asset    uint32
	Position Vector3
}

const (
	debugEffectPreviewSignature uint32 = 0x46414E47
	debugEffectPreviewWorldMode uint32 = 0x574F524C
)

func (DebugEffectPreviewMessage) PacketID() PacketID { return ServerEvent }
func (m DebugEffectPreviewMessage) EncodePayload() []byte {
	payload := []byte{6}
	payload = binary.LittleEndian.AppendUint32(payload, m.Asset)
	payload = append(payload, 10)
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.Position.X))
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.Position.Y))
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.Position.Z))
	// Field 14 is a plain uint32 and survives decoding, so combine the
	// allowlisted asset with WRLD as a reversible Fang-only signature carrier.
	// Fang must never copy this hash into decoded field 6: that field is a
	// resolved resource pointer after reflection decoding.
	payload = append(payload, 14)
	payload = binary.LittleEndian.AppendUint32(payload, m.Asset^debugEffectPreviewWorldMode)
	payload = append(payload, 15)
	payload = binary.LittleEndian.AppendUint32(payload, debugEffectPreviewSignature)
	return append(payload, 0xff)
}

// DropPresentationMessage is the minimal fields-6/10 ServerEvent emitted when
// native loot processing creates an orb before starting its lob.
type DropPresentationMessage struct {
	Asset    uint32
	Position Vector3
}

func (DropPresentationMessage) PacketID() PacketID { return ServerEvent }
func (m DropPresentationMessage) EncodePayload() []byte {
	payload := []byte{6}
	payload = binary.LittleEndian.AppendUint32(payload, m.Asset)
	payload = append(payload, 10)
	payload = appendVector3(payload, m.Position.X, m.Position.Y, m.Position.Z)
	return append(payload, 0xff)
}

// ObjectEffectMessage is the one-shot fields-5/6/7/9/11 ServerEvent used by
// the shared melee template after accepted damage. It does not allocate an
// attached-effect slot and therefore requires no removal packet.
type ObjectEffectMessage struct {
	IsCritical bool
	Asset      uint32
	ObjectID   uint32
	AttackerID uint32
	Facing     Vector3
}

func (ObjectEffectMessage) PacketID() PacketID { return ServerEvent }
func (m ObjectEffectMessage) EncodePayload() []byte {
	payload := make([]byte, 0, 31)
	if m.IsCritical {
		payload = append(payload, 5, 1)
	}
	payload = append(payload, 6)
	payload = binary.LittleEndian.AppendUint32(payload, m.Asset)
	payload = append(payload, 7)
	payload = binary.LittleEndian.AppendUint32(payload, m.ObjectID)
	payload = append(payload, 9)
	payload = binary.LittleEndian.AppendUint32(payload, m.AttackerID)
	payload = append(payload, 11)
	payload = appendVector3(payload, m.Facing.X, m.Facing.Y, m.Facing.Z)
	return append(payload, 0xff)
}

func (PositionedEffectMessage) PacketID() PacketID { return ServerEvent }
func (m PositionedEffectMessage) EncodePayload() []byte {
	if m.Facing.X == 0 && m.Facing.Y == 0 && m.Facing.Z != 0 &&
		!math.IsNaN(float64(m.Facing.Z)) && !math.IsInf(float64(m.Facing.Z), 0) {
		// The client's facing/world-up cross product is singular for vertical
		// effects. An explicit rotation of local +Y preserves the direction
		// without passing that parallel vector through sub_7B1E00.
		halfTurn := float32(math.Sqrt(0.5))
		orientation := Quaternion{X: halfTurn, W: halfTurn}
		if m.Facing.Z < 0 {
			orientation.X = -halfTurn
		}
		return (ServerEventContractMessage{
			Asset: &m.Asset, Position: &m.Position, Orientation: &orientation,
		}).EncodePayload()
	}
	payload := []byte{6}
	payload = binary.LittleEndian.AppendUint32(payload, m.Asset)
	payload = append(payload, 10)
	payload = appendVector3(payload, m.Position.X, m.Position.Y, m.Position.Z)
	payload = append(payload, 11)
	payload = appendVector3(payload, m.Facing.X, m.Facing.Y, m.Facing.Z)
	return append(payload, 0xff)
}

// ClientEventMessage is the minimal reflected ServerEvent shape used when Lua
// supplies only clientEventID. Default asset, object, and position fields are
// omitted rather than serialized as zero-valued reflection members.
type ClientEventMessage struct {
	ClientEventID       uint32
	PlayerExclusionMask uint8
}

// LootRollMessage is the sparse ServerEvent emitted by build 103 for every
// participant in PlayersRollForLoot. Field 7 identifies that participant's
// controlled object, field 14 carries the integer 1..100 roll, and field 15 is
// the native loot-roll client event 0x502A2D7E.
type LootRollMessage struct {
	ObjectID uint32
	Roll     uint32
}

func (LootRollMessage) PacketID() PacketID { return ServerEvent }
func (m LootRollMessage) EncodePayload() []byte {
	payload := []byte{7}
	payload = binary.LittleEndian.AppendUint32(payload, m.ObjectID)
	payload = append(payload, 14)
	payload = binary.LittleEndian.AppendUint32(payload, m.Roll)
	payload = append(payload, 15)
	payload = binary.LittleEndian.AppendUint32(payload, 0x502A2D7E)
	return append(payload, 0xff)
}

func (ClientEventMessage) PacketID() PacketID { return ServerEvent }
func (m ClientEventMessage) EncodePayload() []byte {
	payload := []byte{15}
	payload = binary.LittleEndian.AppendUint32(payload, m.ClientEventID)
	if m.PlayerExclusionMask != 0 {
		payload = append(payload, 16, m.PlayerExclusionMask)
	}
	return append(payload, 0xff)
}

// ServerEventLoot is the build-103 loot tail embedded in a ServerEvent. The
// client uses it for the local LootAwarded event and resulting pickup card.
// RigblockID is the AssetCatalog key that the client resolves to a packaged
// rigblock asset, not the numeric rigblockId stored inside that asset.
type ServerEventLoot struct {
	ReferenceID  uint64
	InstanceID   uint64
	RigblockID   uint32
	SuffixAsset  uint32
	PrefixAsset1 uint32
	PrefixAsset2 uint32
	ItemLevel    int32
	Rarity       int32
	CreationTime uint64
}

func (ServerEventMessage) PacketID() PacketID { return ServerEvent }
func (m ServerEventMessage) EncodePayload() []byte {
	payload := []byte{6}
	payload = binary.LittleEndian.AppendUint32(payload, m.Asset)
	payload = append(payload, 7)
	payload = binary.LittleEndian.AppendUint32(payload, m.ObjectID)
	payload = append(payload, 10)
	payload = appendVector3(payload, m.Position.X, m.Position.Y, m.Position.Z)
	if m.TextValue != 0 {
		payload = append(payload, 14)
		payload = binary.LittleEndian.AppendUint32(payload, m.TextValue)
	}
	if m.ClientEventID != 0 {
		payload = append(payload, 15)
		payload = binary.LittleEndian.AppendUint32(payload, m.ClientEventID)
	}
	if m.Loot != nil {
		payload = append(payload, 17)
		payload = binary.LittleEndian.AppendUint64(payload, m.Loot.ReferenceID)
		payload = append(payload, 18)
		payload = binary.LittleEndian.AppendUint64(payload, m.Loot.InstanceID)
		payload = append(payload, 19)
		payload = binary.LittleEndian.AppendUint32(payload, m.Loot.RigblockID)
		payload = append(payload, 20)
		payload = binary.LittleEndian.AppendUint32(payload, m.Loot.SuffixAsset)
		payload = append(payload, 21)
		payload = binary.LittleEndian.AppendUint32(payload, m.Loot.PrefixAsset1)
		payload = append(payload, 22)
		payload = binary.LittleEndian.AppendUint32(payload, m.Loot.PrefixAsset2)
		payload = append(payload, 23)
		payload = binary.LittleEndian.AppendUint32(payload, uint32(m.Loot.ItemLevel))
		payload = append(payload, 24)
		payload = binary.LittleEndian.AppendUint32(payload, uint32(m.Loot.Rarity))
		payload = append(payload, 25)
		payload = binary.LittleEndian.AppendUint64(payload, m.Loot.CreationTime)
	}
	return append(payload, 0xff)
}

type ObjectTeleportMessage struct {
	ObjectID    uint32
	Position    Vector3
	Orientation Quaternion
}

func (ObjectTeleportMessage) PacketID() PacketID { return ObjectTeleport }
func (m ObjectTeleportMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	payload = appendVector3(payload, m.Position.X, m.Position.Y, m.Position.Z)
	payload = appendVector3(payload, m.Orientation.X, m.Orientation.Y, m.Orientation.Z)
	return binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.Orientation.W))
}

// ObjectJumpMessage copies build 103's complete 44-byte jump snapshot into
// the target object's physics state. The client exposes the two vectors'
// roles, while the four trailing physics parameters remain unnamed.
type ObjectJumpMessage struct {
	ObjectID       uint32
	JumpPosition   Vector3
	JumpDirection  Vector3
	JumpParameters [4]float32
}

func (ObjectJumpMessage) PacketID() PacketID { return ObjectJump }
func (m ObjectJumpMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	payload = appendVector3(payload, m.JumpPosition.X, m.JumpPosition.Y, m.JumpPosition.Z)
	payload = appendVector3(payload, m.JumpDirection.X, m.JumpDirection.Y, m.JumpDirection.Z)
	for _, parameter := range m.JumpParameters {
		payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(parameter))
	}
	return payload
}

// ForcePhysicsUpdateMessage replaces the complete object transform. The
// sender converts its quaternion to three Euler-angle floats on the wire; the
// receiver reconstructs the quaternion before applying position and scale.
type ForcePhysicsUpdateMessage struct {
	ObjectID uint32
	Position Vector3
	Rotation Vector3
	Scale    Vector3
}

func (ForcePhysicsUpdateMessage) PacketID() PacketID { return ForcePhysicsUpdate }
func (m ForcePhysicsUpdateMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	payload = appendVector3(payload, m.Position.X, m.Position.Y, m.Position.Z)
	payload = appendVector3(payload, m.Rotation.X, m.Rotation.Y, m.Rotation.Z)
	return appendVector3(payload, m.Scale.X, m.Scale.Y, m.Scale.Z)
}

type ObjectPlayerMoveMessage struct {
	ObjectID            uint32
	GoalFlags           uint32
	GoalPosition        Vector3
	Facing              Vector3
	ExternalVelocity    Vector3
	ExternalForce       Vector3
	AllowedStopDistance float32
	DesiredStopDistance float32
	TargetPosition      Vector3
	TargetObjectID      uint32
}

type LocomotionUnreliableMessage struct {
	ObjectID     uint32
	GoalPosition Vector3
}

func (LocomotionUnreliableMessage) PacketID() PacketID { return LocomotionUnreliable }
func (m LocomotionUnreliableMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	return appendVector3(payload, m.GoalPosition.X, m.GoalPosition.Y, m.GoalPosition.Z)
}

// PhysicsChangedMessage is the fixed build-103 physics representation toggle.
type PhysicsChangedMessage struct {
	ObjectID  uint32
	IsEnabled bool
}

func (PhysicsChangedMessage) PacketID() PacketID { return PhysicsChanged }
func (m PhysicsChangedMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	return append(payload, boolByte(m.IsEnabled))
}

// GravityForceUpdateMessage carries the two reflected vectors in build 103's
// cGravityForce component. Pointers distinguish an omitted sparse field from
// an explicitly published zero vector.
type GravityForceUpdateMessage struct {
	ObjectID      uint32
	Force         *Vector3
	ForceForMover *Vector3
}

func (GravityForceUpdateMessage) PacketID() PacketID { return GravityForceUpdate }
func (m GravityForceUpdateMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	if m.Force != nil {
		payload = append(payload, 0)
		payload = appendVector3(payload, m.Force.X, m.Force.Y, m.Force.Z)
	}
	if m.ForceForMover != nil {
		payload = append(payload, 1)
		payload = appendVector3(payload, m.ForceForMover.X, m.ForceForMover.Y, m.ForceForMover.Z)
	}
	return append(payload, 0xff)
}

type CombatantDataUpdateMessage struct {
	ObjectID   uint32
	HitPoints  float32
	ManaPoints float32
}

// CombatantDataDeltaMessage carries only changed build-103 combatant fields.
// Bit 0 is hit points and bit 1 is mana; full object creation continues to use
// CombatantDataUpdateMessage.
type CombatantDataDeltaMessage struct {
	ObjectID           uint32
	HitPoints          float32
	ManaPoints         float32
	IsHitPointChanged  bool
	IsManaPointChanged bool
}

type AgentBlackboardUpdateMessage struct {
	ObjectID      uint32
	TargetID      uint32
	IsInCombat    bool
	Stealth       uint8
	IsTargetable  bool
	AttackerCount uint32
}

// InteractableDataUpdateMessage updates build-103 cInteractableData. The
// registered fields are mNumTimesUsed, mNumUsesAllowed, and
// mInteractableAbility, in that order.
type InteractableDataUpdateMessage struct {
	ObjectID    uint32
	TimesUsed   int32
	UsesAllowed int32
	Ability     uint32
}

// LootDataUpdateMessage updates build-103 cLootData. Its ten reflected fields
// are crystalLevel, the embedded loot item fields, mLootInstanceId, and
// mDNAAmount. Later catalyst fields are not part of the build-103 type.
type LootDataUpdateMessage struct {
	ObjectID             uint32
	CrystalLevel         int32
	ItemID               uint64
	RigblockAsset        uint32
	SuffixAsset          uint32
	PrefixAsset          uint32
	SecondaryPrefixAsset uint32
	ItemLevel            int32
	Rarity               int32
	InstanceID           uint64
	DNAAmount            float32
}

// CrystalLootDataUpdateMessage is the minimal build-103 cLootData reflection
// produced after a crystal pickup is created. Field zero is crystalLevel; the
// remaining loot fields retain their object-construction baselines.
type CrystalLootDataUpdateMessage struct {
	ObjectID     uint32
	CrystalLevel int32
}

// CrystalAcquiredMessage is subtype zero of build-103's fixed 29-byte
// kGmsCrystalMessage body. CrystalColor is the noun's five-way catalyst color,
// not its stat identity. The client ignores CrystalLevel, but the native sender
// still populates it; every other ignored byte remains zero.
type CrystalAcquiredMessage struct {
	Slot         int32
	NounAsset    uint32
	CrystalColor int32
	CrystalLevel int32
}

func (CrystalAcquiredMessage) PacketID() PacketID { return CrystalMessage }
func (m CrystalAcquiredMessage) EncodePayload() []byte {
	payload := make([]byte, 29)
	binary.LittleEndian.PutUint32(payload[1:5], uint32(m.Slot))
	binary.LittleEndian.PutUint32(payload[5:9], m.NounAsset)
	binary.LittleEndian.PutUint32(payload[13:17], uint32(m.CrystalColor))
	binary.LittleEndian.PutUint32(payload[17:21], uint32(m.CrystalLevel))
	return payload
}

// CrystalMoveResultMessage is subtype 2 (accepted) or 3 (rejected) of the
// fixed build-103 crystal HUD queue record.
type CrystalMoveResultMessage struct {
	IsAccepted      bool
	SourceSlot      int32
	DestinationSlot int32
}

func (CrystalMoveResultMessage) PacketID() PacketID { return CrystalMessage }
func (m CrystalMoveResultMessage) EncodePayload() []byte {
	payload := make([]byte, 29)
	payload[0] = 3
	if m.IsAccepted {
		payload[0] = 2
		binary.LittleEndian.PutUint32(payload[21:25], uint32(m.SourceSlot))
		binary.LittleEndian.PutUint32(payload[25:29], uint32(m.DestinationSlot))
	}
	return payload
}

func (CrystalLootDataUpdateMessage) PacketID() PacketID { return LootDataUpdate }
func (m CrystalLootDataUpdateMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	payload = append(payload, 0)
	payload = binary.LittleEndian.AppendUint32(payload, uint32(m.CrystalLevel))
	return append(payload, 0xff)
}

func (LootDataUpdateMessage) PacketID() PacketID { return LootDataUpdate }
func (m LootDataUpdateMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	payload = append(payload, 0xff, 0x03)
	payload = binary.LittleEndian.AppendUint32(payload, uint32(m.CrystalLevel))
	payload = binary.LittleEndian.AppendUint64(payload, m.ItemID)
	payload = binary.LittleEndian.AppendUint32(payload, m.RigblockAsset)
	payload = binary.LittleEndian.AppendUint32(payload, m.SuffixAsset)
	payload = binary.LittleEndian.AppendUint32(payload, m.PrefixAsset)
	payload = binary.LittleEndian.AppendUint32(payload, m.SecondaryPrefixAsset)
	payload = binary.LittleEndian.AppendUint32(payload, uint32(m.ItemLevel))
	payload = binary.LittleEndian.AppendUint32(payload, uint32(m.Rarity))
	payload = binary.LittleEndian.AppendUint64(payload, m.InstanceID)
	return binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.DNAAmount))
}

func (InteractableDataUpdateMessage) PacketID() PacketID { return InteractableUpdate }
func (m InteractableDataUpdateMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	payload = append(payload, 0x07)
	payload = binary.LittleEndian.AppendUint32(payload, uint32(m.TimesUsed))
	payload = binary.LittleEndian.AppendUint32(payload, uint32(m.UsesAllowed))
	return binary.LittleEndian.AppendUint32(payload, m.Ability)
}

// ObjectInteractableStateMessage enables an authored interactable and retains
// the source marker used by tutorial/objective logic. These are sporelabsObject
// reflection fields 21 and 22 in build 103.
type ObjectInteractableStateMessage struct {
	ObjectID uint32
	State    uint32
	MarkerID uint32
}

func (ObjectInteractableStateMessage) PacketID() PacketID { return ObjectUpdate }
func (m ObjectInteractableStateMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	payload = append(payload, 21)
	payload = binary.LittleEndian.AppendUint32(payload, m.State)
	payload = append(payload, 22)
	payload = binary.LittleEndian.AppendUint32(payload, m.MarkerID)
	return append(payload, 0xff)
}

func (AgentBlackboardUpdateMessage) PacketID() PacketID { return AgentBlackboardUpdate }
func (m AgentBlackboardUpdateMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	payload = append(payload, 0x1f)
	payload = binary.LittleEndian.AppendUint32(payload, m.TargetID)
	payload = append(payload, boolByte(m.IsInCombat), m.Stealth, boolByte(m.IsTargetable))
	return binary.LittleEndian.AppendUint32(payload, m.AttackerCount)
}

func (CombatantDataUpdateMessage) PacketID() PacketID { return CombatantDataUpdate }
func (m CombatantDataUpdateMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	payload = append(payload, 0x03)
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.HitPoints))
	return binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.ManaPoints))
}

func (CombatantDataDeltaMessage) PacketID() PacketID { return CombatantDataUpdate }
func (m CombatantDataDeltaMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	mask := byte(0)
	if m.IsHitPointChanged {
		mask |= 0x01
	}
	if m.IsManaPointChanged {
		mask |= 0x02
	}
	payload = append(payload, mask)
	if m.IsHitPointChanged {
		payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.HitPoints))
	}
	if m.IsManaPointChanged {
		payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.ManaPoints))
	}
	return payload
}

type AttributeDataUpdateMessage struct {
	ObjectID uint32
	Value    map[uint8]float32
}

func (AttributeDataUpdateMessage) PacketID() PacketID { return AttributeDataUpdate }
func (m AttributeDataUpdateMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	fieldID := make([]uint8, 0, len(m.Value))
	for id := range m.Value {
		fieldID = append(fieldID, id)
	}
	slices.Sort(fieldID)
	for _, id := range fieldID {
		payload = append(payload, id)
		payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.Value[id]))
	}
	return append(payload, 0xff)
}

func TutorialHeroStateMessages(objectID uint32) []ApplicationMessage {
	return HeroStateMessages(objectID, 200, 200)
}

func HeroStateMessages(objectID uint32, hitPoint, powerPoint float32) []ApplicationMessage {
	if hitPoint <= 0 {
		hitPoint = 200
	}
	if powerPoint <= 0 {
		powerPoint = 200
	}
	return []ApplicationMessage{
		CombatantDataUpdateMessage{ObjectID: objectID, HitPoints: hitPoint, ManaPoints: powerPoint},
		AttributeDataUpdateMessage{ObjectID: objectID, Value: map[uint8]float32{
			0: 23, 1: 12, 2: 15, 4: hitPoint, 5: powerPoint, 7: 50, 9: 50, 10: 50,
			11: 8.25, 12: 7.5, 109: 1, 111: 1, 112: 5,
		}},
	}
}

func TutorialEnemyStateMessages(objectID uint32, hitPoint float32) []ApplicationMessage {
	return []ApplicationMessage{
		CombatantDataUpdateMessage{ObjectID: objectID, HitPoints: hitPoint, ManaPoints: 75},
		AttributeDataUpdateMessage{ObjectID: objectID, Value: map[uint8]float32{
			0: 10, 1: 10, 2: 10, 4: hitPoint, 5: 75, 10: 5,
			11: 3, 12: 4.5,
		}},
	}
}

func (ObjectPlayerMoveMessage) PacketID() PacketID { return ObjectPlayerMove }
func (m ObjectPlayerMoveMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	payload = binary.LittleEndian.AppendUint32(payload, m.GoalFlags)
	payload = appendVector3(payload, m.GoalPosition.X, m.GoalPosition.Y, m.GoalPosition.Z)
	payload = appendVector3(payload, m.Facing.X, m.Facing.Y, m.Facing.Z)
	payload = appendVector3(payload, m.ExternalVelocity.X, m.ExternalVelocity.Y, m.ExternalVelocity.Z)
	payload = appendVector3(payload, m.ExternalForce.X, m.ExternalForce.Y, m.ExternalForce.Z)
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.AllowedStopDistance))
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.DesiredStopDistance))
	payload = appendVector3(payload, m.TargetPosition.X, m.TargetPosition.Y, m.TargetPosition.Z)
	return binary.LittleEndian.AppendUint32(payload, m.TargetObjectID)
}

type ActionResponseType uint8

const (
	ActionResponseAccepted      ActionResponseType = 1
	ActionResponseRejected      ActionResponseType = 2
	ActionResponseReleased      ActionResponseType = 4
	ActionResponsePursuit       ActionResponseType = 8
	ActionResponseClearUserData ActionResponseType = 16
)

type ActionCommandResponseMessage struct {
	SyncStamp                  uint8
	ResponseType               ActionResponseType
	ObjectID                   uint32
	AbilityIndex               uint32
	SourceStartMilliseconds    uint64
	SourceCommitMilliseconds   uint64
	SourceEndMilliseconds      uint64
	GlobalCooldownMilliseconds uint64
	UserData                   uint32
}

func (m ActionCommandResponseMessage) validateApplication() error {
	switch m.ResponseType {
	case ActionResponseAccepted, ActionResponseRejected, ActionResponseReleased,
		ActionResponsePursuit, ActionResponseClearUserData:
		return nil
	default:
		return fmt.Errorf("actionResponseType[%d]: %w", m.ResponseType, ErrInvalidPacket)
	}
}

type CooldownUpdateMessage struct {
	ObjectID                   uint32
	AbilityKey                 uint64
	DurationMilliseconds       int64
	SourceStartMilliseconds    int64
	GlobalCooldownMilliseconds int64
}

// ModifierCreatedMessage is the fixed build-103 modifier lifecycle payload.
// It is a packed 37-byte structure, not a reflection bitmap.
type ModifierCreatedMessage struct {
	TargetID             uint32
	ModifierGUID         uint32
	InstanceID           uint32
	DurationMilliseconds uint32
	Overdrive            uint32
	StackCount           uint32
	StartMilliseconds    uint64
	SourceID             uint32
	IsBound              bool
}

func (ModifierCreatedMessage) PacketID() PacketID { return ModifierCreated }
func (m ModifierCreatedMessage) EncodePayload() []byte {
	payload := make([]byte, 0, 37)
	payload = binary.LittleEndian.AppendUint32(payload, m.TargetID)
	payload = binary.LittleEndian.AppendUint32(payload, m.ModifierGUID)
	payload = binary.LittleEndian.AppendUint32(payload, m.InstanceID)
	payload = binary.LittleEndian.AppendUint32(payload, m.DurationMilliseconds)
	payload = binary.LittleEndian.AppendUint32(payload, m.Overdrive)
	payload = binary.LittleEndian.AppendUint32(payload, m.StackCount)
	payload = binary.LittleEndian.AppendUint64(payload, m.StartMilliseconds)
	payload = binary.LittleEndian.AppendUint32(payload, m.SourceID)
	return append(payload, boolByte(m.IsBound))
}

// ModifierUpdatedMessage is the fixed build-103 modifier update payload.
// StartMilliseconds may be -1 to preserve the modifier's existing start time.
type ModifierUpdatedMessage struct {
	TargetID          uint32
	InstanceID        uint32
	StartMilliseconds int64
	StackCount        uint32
	IsBound           bool
}

func (ModifierUpdatedMessage) PacketID() PacketID { return ModifierUpdated }
func (m ModifierUpdatedMessage) EncodePayload() []byte {
	payload := make([]byte, 0, 21)
	payload = binary.LittleEndian.AppendUint32(payload, m.TargetID)
	payload = binary.LittleEndian.AppendUint32(payload, m.InstanceID)
	payload = binary.LittleEndian.AppendUint64(payload, uint64(m.StartMilliseconds))
	payload = binary.LittleEndian.AppendUint32(payload, m.StackCount)
	return append(payload, boolByte(m.IsBound))
}

type ModifierDeletedMessage struct {
	TargetID   uint32
	InstanceID uint32
}

func (ModifierDeletedMessage) PacketID() PacketID { return ModifierDeleted }
func (m ModifierDeletedMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.TargetID)
	return binary.LittleEndian.AppendUint32(payload, m.InstanceID)
}

func (CooldownUpdateMessage) PacketID() PacketID { return CooldownUpdate }
func (m CooldownUpdateMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	payload = binary.LittleEndian.AppendUint64(payload, m.AbilityKey)
	payload = binary.LittleEndian.AppendUint64(payload, uint64(m.DurationMilliseconds))
	payload = binary.LittleEndian.AppendUint64(payload, uint64(m.SourceStartMilliseconds))
	return binary.LittleEndian.AppendUint64(payload, uint64(m.GlobalCooldownMilliseconds))
}

type SetAnimationStateMessage struct {
	ObjectID  uint32
	State     uint32
	Timestamp uint64
	IsOverlay bool
	Scale     float32
}

type SetObjectGFXStateMessage struct {
	ObjectID  uint32
	State     uint32
	Timestamp uint64
}

func (SetObjectGFXStateMessage) PacketID() PacketID { return SetObjectGFXState }
func (m SetObjectGFXStateMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	payload = binary.LittleEndian.AppendUint32(payload, m.State)
	return binary.LittleEndian.AppendUint64(payload, m.Timestamp)
}

func (SetAnimationStateMessage) PacketID() PacketID { return SetAnimationState }
func (m SetAnimationStateMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	payload = binary.LittleEndian.AppendUint32(payload, m.State)
	payload = binary.LittleEndian.AppendUint64(payload, m.Timestamp)
	payload = append(payload, boolByte(m.IsOverlay))
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.Scale))
	// Build 103's native Lua sender passes zero for the final source/echo-gate
	// field. It is not a repeated copy of the animation state.
	return binary.LittleEndian.AppendUint32(payload, 0)
}

type CombatEventMessage struct {
	Flags           uint16
	DeltaHealth     float32
	AbsorbedAmount  float32
	TargetID        uint32
	SourceID        uint32
	AbilityID       uint32
	DamageDirection Vector3
	IntegerHPChange int32
}

func (CombatEventMessage) PacketID() PacketID { return CombatEvent }
func (m CombatEventMessage) EncodePayload() []byte {
	payload := []byte{0xff}
	payload = binary.LittleEndian.AppendUint16(payload, m.Flags)
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.DeltaHealth))
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.AbsorbedAmount))
	payload = binary.LittleEndian.AppendUint32(payload, m.TargetID)
	payload = binary.LittleEndian.AppendUint32(payload, m.SourceID)
	payload = binary.LittleEndian.AppendUint32(payload, m.AbilityID)
	payload = appendVector3(payload, m.DamageDirection.X, m.DamageDirection.Y, m.DamageDirection.Z)
	return binary.LittleEndian.AppendUint32(payload, uint32(m.IntegerHPChange))
}

// DamageCombatEventMessage is the sparse build-103 native damage shape. Its
// reflection mask 0x9b includes flags, positive health delta, target, source,
// and signed HP change while omitting absorbed amount, ability, and direction.
type DamageCombatEventMessage struct {
	Flags           uint16
	DeltaHealth     float32
	TargetID        uint32
	SourceID        uint32
	IntegerHPChange int32
}

func (DamageCombatEventMessage) PacketID() PacketID { return CombatEvent }
func (m DamageCombatEventMessage) EncodePayload() []byte {
	payload := []byte{0x9b}
	payload = binary.LittleEndian.AppendUint16(payload, m.Flags)
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.DeltaHealth))
	payload = binary.LittleEndian.AppendUint32(payload, m.TargetID)
	payload = binary.LittleEndian.AppendUint32(payload, m.SourceID)
	return binary.LittleEndian.AppendUint32(payload, uint32(m.IntegerHPChange))
}

type ObjectDeleteMessage struct {
	ObjectID []uint32
}

func (ObjectDeleteMessage) PacketID() PacketID { return ObjectDelete }
func (m ObjectDeleteMessage) EncodePayload() []byte {
	payload := make([]byte, 0, 4*len(m.ObjectID))
	for _, objectID := range m.ObjectID {
		payload = binary.LittleEndian.AppendUint32(payload, objectID)
	}
	return payload
}

func (ActionCommandResponseMessage) PacketID() PacketID { return ActionCommandResponse }
func (m ActionCommandResponseMessage) EncodePayload() []byte {
	payload := make([]byte, 56)
	payload[0] = m.SyncStamp
	payload[1] = byte(m.ResponseType)
	binary.LittleEndian.PutUint32(payload[4:8], m.ObjectID)
	binary.LittleEndian.PutUint32(payload[8:12], m.AbilityIndex)
	binary.LittleEndian.PutUint64(payload[16:24], m.SourceStartMilliseconds)
	binary.LittleEndian.PutUint64(payload[24:32], m.SourceCommitMilliseconds)
	binary.LittleEndian.PutUint64(payload[32:40], m.SourceEndMilliseconds)
	binary.LittleEndian.PutUint64(payload[40:48], m.GlobalCooldownMilliseconds)
	binary.LittleEndian.PutUint32(payload[52:56], m.UserData)
	return payload
}

func (ObjectUpdateMessage) PacketID() PacketID { return ObjectUpdate }
func (m ObjectUpdateMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	payload = append(payload, 6)
	payload = appendVector3(payload, m.PositionX, m.PositionY, m.PositionZ)
	return append(payload, 16, boolByte(m.IsVisible), 0xff)
}

func (ObjectPositionUpdateMessage) PacketID() PacketID { return ObjectUpdate }
func (m ObjectPositionUpdateMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	payload = append(payload, 6)
	payload = appendVector3(payload, m.PositionX, m.PositionY, m.PositionZ)
	return append(payload, 0xff)
}

func (ObjectCollisionUpdateMessage) PacketID() PacketID { return ObjectUpdate }
func (m ObjectCollisionUpdateMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	return append(payload, 17, boolByte(m.IsCollisionEnabled), 0xff)
}

type PlayerCharacterDeployMessage struct {
	PlayerIndex   uint8
	CreatureIndex uint32
	ObjectID      uint32
}

// ObjectiveRecord is the exact 56-byte build-103 wire record. Each of four
// players owns one medal/state byte and three independent detokenization words.
type ObjectiveRecord struct {
	ObjectiveID uint32
	State       [4]uint8
	Token       [4][3]uint32
}

type ObjectivesInitMessage struct {
	Record []ObjectiveRecord
}

func (ObjectivesInitMessage) PacketID() PacketID { return ObjectivesInit }
func (m ObjectivesInitMessage) EncodePayload() []byte {
	payload := make([]byte, 1, 1+len(m.Record)*56)
	payload[0] = byte(len(m.Record))
	for _, record := range m.Record {
		payload = appendObjectiveRecord(payload, record)
	}
	return payload
}

// ObjectivesCompleteMessage is build 103's completion snapshot. The four
// trailing bytes per player are structurally exact, but their gameplay
// meanings remain intentionally unnamed until stronger evidence is available.
type ObjectivesCompleteMessage struct {
	Record      []ObjectiveRecord
	ResultField [4][4]uint8
}

type ObjectiveAddMessage struct {
	Record ObjectiveRecord
}

func (ObjectiveAddMessage) PacketID() PacketID { return ObjectiveAdd }
func (m ObjectiveAddMessage) EncodePayload() []byte {
	return appendObjectiveRecord(nil, m.Record)
}

func (ObjectivesCompleteMessage) PacketID() PacketID { return ObjectivesComplete }
func (m ObjectivesCompleteMessage) EncodePayload() []byte {
	payload := make([]byte, 1, 17+len(m.Record)*56)
	payload[0] = byte(len(m.Record))
	for _, record := range m.Record {
		payload = appendObjectiveRecord(payload, record)
	}
	for _, playerResult := range m.ResultField {
		payload = append(payload, playerResult[:]...)
	}
	return payload
}

func appendObjectiveRecord(payload []byte, record ObjectiveRecord) []byte {
	payload = binary.LittleEndian.AppendUint32(payload, record.ObjectiveID)
	payload = append(payload, record.State[:]...)
	for _, token := range record.Token {
		for _, integer := range token {
			payload = binary.LittleEndian.AppendUint32(payload, integer)
		}
	}
	return payload
}

type ObjectiveUpdatedMessage struct {
	ObjectiveID uint32
	PlayerIndex uint8
	Medal       uint8
	Voiceover   uint32
	IsShown     bool
	Token       [3]uint32
}

func (ObjectiveUpdatedMessage) PacketID() PacketID { return ObjectiveUpdated }
func (m ObjectiveUpdatedMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectiveID)
	payload = append(payload, m.PlayerIndex, m.Medal)
	payload = binary.LittleEndian.AppendUint32(payload, m.Voiceover)
	payload = append(payload, boolByte(m.IsShown))
	for _, integer := range m.Token {
		payload = binary.LittleEndian.AppendUint32(payload, integer)
	}
	return payload
}

func ObjectiveInitializationMessages(
	records []ObjectiveRecord, initialUpdate ObjectiveUpdatedMessage,
) []ApplicationMessage {
	return []ApplicationMessage{
		ObjectivesInitMessage{Record: records},
		initialUpdate,
	}
}

func TutorialAbilityLessonMessage(objectiveID uint32, playerIndex uint8) ApplicationMessage {
	return ObjectiveUpdatedMessage{
		ObjectiveID: objectiveID,
		PlayerIndex: playerIndex,
		Medal:       4,
		IsShown:     true,
		Token:       [3]uint32{1, 2, 3},
	}
}

func TutorialAbilityLessonCompleteMessage(objectiveID uint32, playerIndex uint8) ApplicationMessage {
	return ObjectiveUpdatedMessage{
		ObjectiveID: objectiveID,
		PlayerIndex: playerIndex,
		Medal:       4,
		Token:       [3]uint32{1, 2, 3},
	}
}

func (PlayerCharacterDeployMessage) PacketID() PacketID { return PlayerCharacterDeploy }
func (m PlayerCharacterDeployMessage) EncodePayload() []byte {
	payload := []byte{m.PlayerIndex}
	payload = binary.LittleEndian.AppendUint32(payload, m.CreatureIndex)
	return binary.LittleEndian.AppendUint32(payload, m.ObjectID)
}

func appendVector3(payload []byte, x, y, z float32) []byte {
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(x))
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(y))
	return binary.LittleEndian.AppendUint32(payload, math.Float32bits(z))
}

func appendQuaternion(payload []byte, quaternion Quaternion) []byte {
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(quaternion.X))
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(quaternion.Y))
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(quaternion.Z))
	return binary.LittleEndian.AppendUint32(payload, math.Float32bits(quaternion.W))
}

func boolByte(value bool) byte {
	if value {
		return 1
	}
	return 0
}

type GameStartMessage struct{ LevelIndex uint32 }

func (m GameStartMessage) PacketID() PacketID { return GameStart }
func (m GameStartMessage) EncodePayload() []byte {
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, m.LevelIndex)
	return payload
}

type ChainVoteMessage struct {
	CurrentLevel       uint32
	NextDifficulty     uint32
	StarLevel          uint32
	TimeRemaining      float32
	PlanetsRepresented uint8
	EnemyNouns         [6]uint32
	FirstPresentation  [3]uint32
	FirstVoice         uint32
	UnlockOrdinal      uint32
	NextPresentation   [3]uint32
	NextLevel          uint32
}

func (m ChainVoteMessage) PacketID() PacketID { return ChainVoteMsgs }
func (m ChainVoteMessage) EncodePayload() []byte {
	data := make([]byte, 0x151)
	binary.LittleEndian.PutUint32(data[0x000:0x004], m.CurrentLevel)
	binary.LittleEndian.PutUint32(data[0x004:0x008], m.NextDifficulty)
	binary.LittleEndian.PutUint32(data[0x008:0x00c], m.StarLevel)
	binary.LittleEndian.PutUint32(data[0x00c:0x010], math.Float32bits(m.TimeRemaining))
	data[0x010] = m.PlanetsRepresented
	offset := 17
	for _, noun := range m.EnemyNouns {
		binary.LittleEndian.PutUint32(data[offset:offset+4], noun)
		offset += 4
	}
	for index, presentation := range m.FirstPresentation {
		offset := 0x039 + index*4
		binary.LittleEndian.PutUint32(data[offset:offset+4], presentation)
	}
	binary.LittleEndian.PutUint32(data[0x045:0x049], m.FirstVoice)
	binary.LittleEndian.PutUint32(data[0x049:0x04d], m.UnlockOrdinal)
	for index, presentation := range m.NextPresentation {
		offset := 0x0d9 + index*4
		binary.LittleEndian.PutUint32(data[offset:offset+4], presentation)
	}
	binary.LittleEndian.PutUint32(data[0x0e9:0x0ed], m.NextLevel)
	return append([]byte{0}, data...)
}

// ChainSelectionMessage is the initial campaign planet-selection envelope.
// Its build-103 field placement differs from the post-level result record even
// though both use application packet 0xA9 subtype zero.
type ChainSelectionMessage struct {
	Level         uint32
	Difficulty    uint32
	TimeRemaining float32
	EnemyNoun     [6]uint32
	LevelNoun     [2]uint32
	IntroMovie    uint32
	IntroVoice    uint32
	NextMovie     uint32
}

func (m ChainSelectionMessage) PacketID() PacketID { return ChainVoteMsgs }
func (m ChainSelectionMessage) EncodePayload() []byte {
	data := make([]byte, 0x151)
	binary.LittleEndian.PutUint32(data[0x000:0x004], m.Level)
	binary.LittleEndian.PutUint32(data[0x004:0x008], m.Difficulty)
	binary.LittleEndian.PutUint32(
		data[0x00c:0x010], math.Float32bits(m.TimeRemaining),
	)
	offset := 0x011
	for _, noun := range m.EnemyNoun {
		binary.LittleEndian.PutUint32(data[offset:offset+4], noun)
		offset += 4
	}
	for _, noun := range m.LevelNoun {
		binary.LittleEndian.PutUint32(data[offset:offset+4], noun)
		offset += 4
	}
	introVoice := m.IntroVoice
	if introVoice == 0 {
		introVoice = m.IntroMovie
	}
	binary.LittleEndian.PutUint32(data[0x039:0x03d], m.IntroMovie)
	binary.LittleEndian.PutUint32(data[0x03d:0x041], m.IntroMovie)
	binary.LittleEndian.PutUint32(data[0x041:0x045], introVoice)
	binary.LittleEndian.PutUint32(data[0x045:0x049], m.IntroVoice)
	binary.LittleEndian.PutUint32(data[0x0d9:0x0dd], m.NextMovie)
	binary.LittleEndian.PutUint32(data[0x0dd:0x0e1], m.NextMovie)
	binary.LittleEndian.PutUint32(data[0x0e1:0x0e5], m.NextMovie)
	binary.LittleEndian.PutUint32(data[0x0e9:0x0ed], m.Level)
	return append([]byte{0}, data...)
}

type ChainVoteCountdownMessage struct{ Seconds float32 }

func (m ChainVoteCountdownMessage) PacketID() PacketID { return ChainVoteMsgs }
func (m ChainVoteCountdownMessage) EncodePayload() []byte {
	payload := []byte{1}
	return binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.Seconds))
}

type ChainVoteRouteMessage struct {
	IsCashOut bool
}

func (ChainVoteRouteMessage) PacketID() PacketID { return ChainVoteMsgs }
func (m ChainVoteRouteMessage) EncodePayload() []byte {
	return []byte{2, boolByte(m.IsCashOut)}
}

// ChainCashOutReward is one build-103 item-card record inside AB subtype zero.
// The leading eight opaque bytes remain zero because the client does not read
// them and no server identity semantics have been recovered for that range.
type ChainCashOutReward struct {
	Rigblock uint32
	Suffix   uint32
	Prefix   uint32
	Prefix2  uint32
	Level    int32
	Rarity   int32
	Roll     int32
}

// ChainCashOutMessage is the exact 712-byte AB subtype-zero presentation body.
type ChainCashOutMessage struct {
	PlanetsCompleted       int32
	DNA                    int32
	StartingExperiences    [4]int32
	FinalExperiences       [4]int32
	GoldMedalCounts        [4]int32
	SilverMedalCounts      [4]int32
	BronzeMedalCounts      [4]int32
	UpperRarityBoundaries  [4]int32
	LowerRarityBoundaries  [4]int32
	AreCashoutBonusesGiven [4]bool
	Rewards                [4][4]ChainCashOutReward
}

func (m ChainCashOutMessage) PacketID() PacketID { return ChainCashOutMsgs }
func (m ChainCashOutMessage) EncodePayload() []byte {
	data := make([]byte, 0x2c8)
	putInt32 := func(offset int, integer int32) {
		binary.LittleEndian.PutUint32(data[offset:offset+4], uint32(integer))
	}
	putInt32(0x000, m.PlanetsCompleted)
	putInt32(0x004, m.DNA)
	for playerIndex := range 4 {
		putInt32(0x014+playerIndex*4, m.StartingExperiences[playerIndex])
		putInt32(0x024+playerIndex*4, m.FinalExperiences[playerIndex])
		putInt32(0x034+playerIndex*4, m.GoldMedalCounts[playerIndex])
		putInt32(0x044+playerIndex*4, m.SilverMedalCounts[playerIndex])
		putInt32(0x054+playerIndex*4, m.BronzeMedalCounts[playerIndex])
		putInt32(0x064+playerIndex*4, m.UpperRarityBoundaries[playerIndex])
		putInt32(0x074+playerIndex*4, m.LowerRarityBoundaries[playerIndex])
		data[0x084+playerIndex] = boolByte(m.AreCashoutBonusesGiven[playerIndex])
		for rewardIndex, reward := range m.Rewards[playerIndex] {
			offset := 0x088 + 0x090*playerIndex + 0x024*rewardIndex
			binary.LittleEndian.PutUint32(data[offset+0x08:offset+0x0c], reward.Rigblock)
			binary.LittleEndian.PutUint32(data[offset+0x0c:offset+0x10], reward.Suffix)
			binary.LittleEndian.PutUint32(data[offset+0x10:offset+0x14], reward.Prefix)
			binary.LittleEndian.PutUint32(data[offset+0x14:offset+0x18], reward.Prefix2)
			putInt32(offset+0x18, reward.Level)
			putInt32(offset+0x1c, reward.Rarity)
			putInt32(offset+0x20, reward.Roll)
		}
	}
	return append([]byte{0}, data...)
}

type PrepareForStartMessage struct {
	Level      uint32
	Markerset  uint32
	PlayerMask uint32
	LevelIndex uint32
}

type LabsPlayerStatusMessage struct {
	OnlineID            uint64
	AvatarLevel         uint32
	AvatarXP            float32
	ChainProgression    uint32
	DNA                 uint32
	Slot                uint8
	Team                uint8
	Status              uint32
	Progress            float32
	IsInitial           bool
	IsStatusPreserved   bool
	ControlledObjectID  uint32
	HeroNoun            uint32
	HeroAsset           uint64
	HeroVersion         int32
	HeroType            uint32
	SecondHeroNoun      uint32
	SecondHeroAsset     uint64
	SecondHeroVersion   int32
	SecondHeroType      uint32
	ThirdHeroNoun       uint32
	ThirdHeroAsset      uint64
	ThirdHeroVersion    int32
	ThirdHeroType       uint32
	AbilityCount        uint32
	LockedDeckMinimum   uint32
	DeckScore           uint32
	EnergyPoint         float32
	IsOverdriveUnlocked bool
	CharacterResources  [3]LabsCharacterResource
	CrystalResources    [9]LabsCrystalResource
	CrystalBonuses      [8]bool
}

type LabsCharacterResource struct {
	Health             float32
	MaxHealth          float32
	Mana               float32
	MaxMana            float32
	GearScore          float32
	FlattenedGearScore float32
	PartAttribute      [111]float32
}

type LabsCrystalResource struct {
	Noun  uint32
	Level uint16
}

// LabsPlayerCrystalInventoryMessage refreshes the nine fixed mCrystals
// records consumed by the build-103 inventory tooltip. CrystalMessage updates
// the HUD presentation only and does not populate these player-owned records.
type LabsPlayerCrystalInventoryMessage struct {
	PlayerSlot uint8
	Crystals   [9]LabsCrystalResource
}

// LabsPlayerCrystalBonusesMessage refreshes the eight line flags consumed by
// MaxisHUDCatalysts to reveal and animate horizontal, vertical, and diagonal
// link clips.
type LabsPlayerCrystalBonusesMessage struct {
	PlayerSlot       uint8
	AreBonusesActive [8]bool
}

func (LabsPlayerCrystalInventoryMessage) PacketID() PacketID { return LabsPlayerUpdate }
func (m LabsPlayerCrystalInventoryMessage) EncodePayload() []byte {
	// Player field 13 copies the nine fixed crystal records, then outer fields
	// 3..11 resolve each wire noun ID into the client-owned resource handle
	// consumed by the HUD tooltip. Omitting those outer bits leaves the noun ID
	// in the handle slot and makes the tooltip dereference the hash as a pointer.
	payload := []byte{m.PlayerSlot, 0xf8, 0x1f, 13}
	for _, crystal := range m.Crystals {
		payload = appendLabsFixedCrystal(payload, crystal)
	}
	payload = append(payload, 0xff)
	for _, crystal := range m.Crystals {
		payload = appendLabsCrystalReflection(payload, crystal)
	}
	return payload
}

func (LabsPlayerCrystalBonusesMessage) PacketID() PacketID { return LabsPlayerUpdate }
func (m LabsPlayerCrystalBonusesMessage) EncodePayload() []byte {
	payload := []byte{m.PlayerSlot, 0, 0x10, 14}
	for _, isBonusActive := range m.AreBonusesActive {
		payload = append(payload, boolByte(isBonusActive))
	}
	return append(payload, 0xff)
}

func (m LabsPlayerStatusMessage) PacketID() PacketID { return LabsPlayerUpdate }
func (m LabsPlayerStatusMessage) EncodePayload() []byte {
	updateBits := uint16(0x1000)
	capacity := 18
	if m.IsInitial {
		// Build 103 requires valid reflections for all three fixed character
		// records even while tutorial squad positions remain locked. Field 22
		// owns that visible-position boundary.
		updateBits = 0x1fff
		capacity = 5000
	}
	payload := make([]byte, 0, capacity)
	payload = append(payload, m.Slot)
	payload = binary.LittleEndian.AppendUint16(payload, updateBits)
	if m.IsInitial {
		payload = append(payload, 0, 1)
		payload = append(payload, 1)
		payload = binary.LittleEndian.AppendUint32(payload, 0)
		payload = append(payload, 2)
		payload = binary.LittleEndian.AppendUint32(payload, 0)
		// Field 3 is the native character array and is marked never-reflect.
		// Its build-103 stride is 0x5e8, not the old 0x620 server layout;
		// emitting it shifts later player fields and blocks Arena readiness.
		// The three character reflections below carry their wire state.
		payload = append(payload, 4, m.Slot)
		team := m.Team
		if team == 0 {
			team = 1
		}
		payload = append(payload, 5, team)
		payload = append(payload, 6)
		payload = binary.LittleEndian.AppendUint64(payload, m.OnlineID)
	}
	// Roster refreshes must not rewind the client's loading/cinematic state.
	// These tagged fields are independently optional in the player reflection.
	if !m.IsStatusPreserved {
		payload = append(payload, 7)
		payload = binary.LittleEndian.AppendUint32(payload, m.Status)
		payload = append(payload, 8)
		payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.Progress))
	}
	if m.IsInitial {
		payload = append(payload, 9)
		payload = binary.LittleEndian.AppendUint32(payload, m.ControlledObjectID)
		payload = append(payload, 10)
		payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.EnergyPoint))
		payload = append(payload, 12)
		payload = binary.LittleEndian.AppendUint32(payload, m.DNA)
		payload = append(payload, 13)
		for _, crystal := range m.CrystalResources {
			payload = appendLabsFixedCrystal(payload, crystal)
		}
		payload = append(payload, 14)
		for _, isBonusActive := range m.CrystalBonuses {
			payload = append(payload, boolByte(isBonusActive))
		}
		payload = append(payload, 15)
		payload = binary.LittleEndian.AppendUint32(payload, m.AvatarLevel)
		payload = append(payload, 16)
		payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.AvatarXP))
		payload = append(payload, 17)
		payload = binary.LittleEndian.AppendUint32(payload, m.ChainProgression)
		payload = append(payload, 18, 0)
		payload = append(payload, 19, boolByte(!m.IsOverdriveUnlocked))
		payload = append(payload, 21)
		payload = binary.LittleEndian.AppendUint32(payload, m.AbilityCount)
		payload = append(payload, 22)
		payload = binary.LittleEndian.AppendUint32(payload, m.LockedDeckMinimum)
		payload = append(payload, 23)
		payload = binary.LittleEndian.AppendUint32(payload, m.DeckScore)
	}
	payload = append(payload, 0xff)
	if m.IsInitial {
		payload = appendLabsCharacterReflection(
			payload, m, m.HeroType, initialLabsCharacterResource(m.CharacterResources[0]),
		)
		if m.SecondHeroNoun != 0 && m.SecondHeroAsset != 0 {
			payload = appendLabsCharacterReflectionValues(
				payload, m.SecondHeroNoun, m.SecondHeroAsset, m.SecondHeroVersion, m.SecondHeroType,
				initialLabsCharacterResource(m.CharacterResources[1]),
			)
		} else {
			payload = appendLabsCharacterReflection(
				payload, m, 6, initialLabsCharacterResource(m.CharacterResources[0]),
			)
		}
		if m.ThirdHeroNoun != 0 && m.ThirdHeroAsset != 0 {
			payload = appendLabsCharacterReflectionValues(
				payload, m.ThirdHeroNoun, m.ThirdHeroAsset, m.ThirdHeroVersion, m.ThirdHeroType,
				initialLabsCharacterResource(m.CharacterResources[2]),
			)
		} else {
			payload = appendLabsCharacterReflection(
				payload, m, 6, initialLabsCharacterResource(m.CharacterResources[0]),
			)
		}
		for _, crystal := range m.CrystalResources {
			payload = appendLabsCrystalReflection(payload, crystal)
		}
	}
	return payload
}

func appendLabsFixedCrystal(payload []byte, crystal LabsCrystalResource) []byte {
	record := make([]byte, 16)
	binary.LittleEndian.PutUint32(record[0:4], crystal.Noun)
	binary.LittleEndian.PutUint16(record[4:6], crystal.Level)
	return append(payload, record...)
}

func appendLabsCrystalReflection(payload []byte, crystal LabsCrystalResource) []byte {
	payload = append(payload, 3)
	payload = binary.LittleEndian.AppendUint32(payload, crystal.Noun)
	return binary.LittleEndian.AppendUint16(payload, crystal.Level)
}

func initialLabsCharacterResource(resource LabsCharacterResource) LabsCharacterResource {
	if resource.MaxHealth <= 0 {
		resource.MaxHealth = 200
	}
	if resource.Health <= 0 {
		resource.Health = resource.MaxHealth
	}
	if resource.MaxMana <= 0 {
		resource.MaxMana = 200
	}
	if resource.Mana <= 0 {
		resource.Mana = resource.MaxMana
	}
	return resource
}

// LabsPlayerControlledObjectMessage updates the build-103 LabsPlayer field at
// offset +0x1238. Combat presentation resolves this field to classify local
// damage and healing.
type LabsPlayerControlledObjectMessage struct {
	Slot     uint8
	ObjectID uint32
}

func (LabsPlayerControlledObjectMessage) PacketID() PacketID { return LabsPlayerUpdate }
func (m LabsPlayerControlledObjectMessage) EncodePayload() []byte {
	payload := []byte{m.Slot, 0, 0x10, 9}
	payload = binary.LittleEndian.AppendUint32(payload, m.ObjectID)
	return append(payload, 0xff)
}

type LabsPlayerAbilityCountMessage struct {
	Slot         uint8
	AbilityCount uint32
}

// LabsPlayerDeployCooldownMessage updates character reflection field 4 for
// both reserve slots after a successful deployment. Build 103 reads these as
// absolute gameplay-clock deadlines for the reserve portrait wipes.
type LabsPlayerDeployCooldownMessage struct {
	PlayerSlot            uint8
	DeployedCreatureIndex uint32
	DeadlineMilliseconds  uint64
}

// LabsPlayerCharacterResourceMessage refreshes the nested character record
// consumed by reserve portraits. Combatant updates alone refresh the world
// object, but not the deck's cached health and mana bars.
type LabsPlayerCharacterResourceMessage struct {
	PlayerSlot    uint8
	CreatureIndex uint32
	Resource      LabsCharacterResource
}

func (LabsPlayerCharacterResourceMessage) PacketID() PacketID { return LabsPlayerUpdate }
func (m LabsPlayerCharacterResourceMessage) EncodePayload() []byte {
	if m.CreatureIndex >= 3 {
		return nil
	}
	payload := []byte{m.PlayerSlot}
	payload = binary.LittleEndian.AppendUint16(payload, uint16(1)<<m.CreatureIndex)
	for index, amount := range [...]float32{
		m.Resource.Health, m.Resource.MaxHealth, m.Resource.Mana, m.Resource.MaxMana,
	} {
		payload = append(payload, byte(7+index))
		payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(amount))
	}
	return append(payload, 0xff)
}

func (LabsPlayerDeployCooldownMessage) PacketID() PacketID { return LabsPlayerUpdate }
func (m LabsPlayerDeployCooldownMessage) EncodePayload() []byte {
	updateBits := uint16(0)
	for creatureIndex := uint32(0); creatureIndex < 3; creatureIndex++ {
		if creatureIndex != m.DeployedCreatureIndex {
			updateBits |= uint16(1) << creatureIndex
		}
	}
	payload := []byte{m.PlayerSlot}
	payload = binary.LittleEndian.AppendUint16(payload, updateBits)
	for creatureIndex := uint32(0); creatureIndex < 3; creatureIndex++ {
		if creatureIndex == m.DeployedCreatureIndex {
			continue
		}
		payload = append(payload, 4)
		payload = binary.LittleEndian.AppendUint64(payload, m.DeadlineMilliseconds)
		payload = append(payload, 0xff)
	}
	return payload
}

func (m LabsPlayerAbilityCountMessage) PacketID() PacketID { return LabsPlayerUpdate }
func (m LabsPlayerAbilityCountMessage) EncodePayload() []byte {
	payload := []byte{m.Slot, 0, 0x10, 21}
	payload = binary.LittleEndian.AppendUint32(payload, m.AbilityCount)
	return append(payload, 0xff)
}

type LabsPlayerProgressionMessage struct {
	Slot        uint8
	AvatarLevel uint32
	AvatarXP    float32
}

type LabsPlayerDNAUpdateMessage struct {
	Slot uint8
	DNA  uint32
}

func (LabsPlayerDNAUpdateMessage) PacketID() PacketID { return LabsPlayerUpdate }
func (m LabsPlayerDNAUpdateMessage) EncodePayload() []byte {
	payload := []byte{m.Slot, 0, 0x10, 12}
	payload = binary.LittleEndian.AppendUint32(payload, m.DNA)
	return append(payload, 0xff)
}

func (m LabsPlayerProgressionMessage) PacketID() PacketID { return LabsPlayerUpdate }
func (m LabsPlayerProgressionMessage) EncodePayload() []byte {
	payload := []byte{m.Slot, 0, 0x10, 15}
	payload = binary.LittleEndian.AppendUint32(payload, m.AvatarLevel)
	payload = append(payload, 16)
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.AvatarXP))
	return append(payload, 0xff)
}

// LabsPlayerOverdriveUnlockMessage applies the exact sparse reflection written
// by build 103's nPlayer.UnlockOverdrive native: fill energy field 10 and
// clear locked-overdrive field 19.
type LabsPlayerOverdriveUnlockMessage struct {
	Slot          uint8
	MaximumEnergy float32
}

func (LabsPlayerOverdriveUnlockMessage) PacketID() PacketID { return LabsPlayerUpdate }
func (m LabsPlayerOverdriveUnlockMessage) EncodePayload() []byte {
	payload := []byte{m.Slot, 0, 0x10, 10}
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(m.MaximumEnergy))
	payload = append(payload, 19, 0)
	return append(payload, 0xff)
}

// LabsPlayerCrystalUnlockMessage applies the exact sparse reflection written
// by build 103's nPlayer.UnlockCrystals native: clear locked-crystals field 20.
type LabsPlayerCrystalUnlockMessage struct {
	Slot uint8
}

func (LabsPlayerCrystalUnlockMessage) PacketID() PacketID { return LabsPlayerUpdate }
func (m LabsPlayerCrystalUnlockMessage) EncodePayload() []byte {
	return []byte{m.Slot, 0, 0x10, 20, 0, 0xff}
}

func appendLabsCharacterReflection(
	payload []byte, message LabsPlayerStatusMessage, creatureType uint32, resource LabsCharacterResource,
) []byte {
	return appendLabsCharacterReflectionValues(
		payload, message.HeroNoun, message.HeroAsset, message.HeroVersion, creatureType, resource,
	)
}

func appendLabsCharacterReflectionValues(
	payload []byte, noun uint32, asset uint64, version int32, creatureType uint32,
	resource LabsCharacterResource,
) []byte {
	payload = append(payload, 0)
	payload = binary.LittleEndian.AppendUint32(payload, uint32(version))
	payload = append(payload, 1)
	payload = binary.LittleEndian.AppendUint32(payload, noun)
	payload = append(payload, 2)
	payload = binary.LittleEndian.AppendUint64(payload, asset)
	payload = append(payload, 3)
	payload = binary.LittleEndian.AppendUint32(payload, creatureType)
	payload = append(payload, 5)
	payload = binary.LittleEndian.AppendUint32(payload, 10)
	payload = append(payload, 6)
	for range 9 {
		payload = binary.LittleEndian.AppendUint32(payload, 1)
	}
	for index, amount := range [...]float32{resource.Health, resource.MaxHealth, resource.Mana, resource.MaxMana} {
		payload = append(payload, byte(7+index))
		payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(amount))
	}
	payload = append(payload, 11)
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(resource.GearScore))
	payload = append(payload, 12)
	payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(resource.FlattenedGearScore))
	for attributeIndex, amount := range resource.PartAttribute {
		if amount == 0 {
			continue
		}
		payload = append(payload, byte(13+attributeIndex))
		payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(amount))
	}
	return append(payload, 0xff)
}

func (m PrepareForStartMessage) PacketID() PacketID { return GamePrepareForStart }
func (m PrepareForStartMessage) EncodePayload() []byte {
	payload := make([]byte, 16)
	binary.LittleEndian.PutUint32(payload[0:4], m.Level)
	binary.LittleEndian.PutUint32(payload[4:8], m.Markerset)
	binary.LittleEndian.PutUint32(payload[8:12], m.PlayerMask)
	binary.LittleEndian.PutUint32(payload[12:16], m.LevelIndex)
	return payload
}

// ArenaGameBranchMessage selects build 103's Arena state 15 or 16. Subtype 5
// consumes the branch flag.
type ArenaGameBranchMessage struct {
	IsAlternate bool
}

func (ArenaGameBranchMessage) PacketID() PacketID { return ArenaGameMsgs }
func (m ArenaGameBranchMessage) EncodePayload() []byte {
	return []byte{5, boolByte(m.IsAlternate)}
}

// ArenaGameExitMessage selects build 103's Arena state 6. Subtype 8 has no
// trailing body.
type ArenaGameExitMessage struct{}

func (ArenaGameExitMessage) PacketID() PacketID    { return ArenaGameMsgs }
func (ArenaGameExitMessage) EncodePayload() []byte { return []byte{8} }

type ArenaGameStateSevenMessage struct{}

func (ArenaGameStateSevenMessage) PacketID() PacketID    { return ArenaGameMsgs }
func (ArenaGameStateSevenMessage) EncodePayload() []byte { return []byte{7} }

type ArenaGamePlayerWordMessage struct {
	Subtype uint8
	Word    uint32
}

func (ArenaGamePlayerWordMessage) PacketID() PacketID { return ArenaGameMsgs }
func (m ArenaGamePlayerWordMessage) EncodePayload() []byte {
	if m.Subtype != 9 && m.Subtype != 10 {
		return nil
	}
	return binary.LittleEndian.AppendUint32([]byte{m.Subtype}, m.Word)
}

type ArenaRoundResult struct {
	Outcome          uint32
	Field04          uint32
	CreatureResource [3]uint64
	HealthPercent    [3]float32
	ManaPercent      [3]float32
}

type ArenaPlayerResult struct {
	PlayerID        uint64
	AvatarID        uint32
	Team            uint8
	Kills           uint32
	Deaths          uint32
	DamageDealt     float32
	DamageTaken     float32
	HealingDealt    float32
	HealingReceived float32
	Round           [3]ArenaRoundResult
}

type ArenaResultsBody struct {
	SelectorA        uint32
	SelectorB        uint32
	CountA           uint32
	CountB           uint32
	RoundTimeSeconds int32
	DNAAmount        [6]uint32
	Player           [6]ArenaPlayerResult
}

type ArenaLobbySnapshotMessage struct {
	LobbyFlag uint8
	LobbyWord uint32
	Results   ArenaResultsBody
}

func (ArenaLobbySnapshotMessage) PacketID() PacketID { return ArenaLobbyMsgs }
func (m ArenaLobbySnapshotMessage) EncodePayload() []byte {
	payload := []byte{3, m.LobbyFlag}
	payload = binary.LittleEndian.AppendUint32(payload, m.LobbyWord)
	return appendArenaResultsBody(payload, m.Results)
}

type ArenaResultsSnapshotMessage struct {
	Results ArenaResultsBody
}

func (ArenaResultsSnapshotMessage) PacketID() PacketID { return ArenaResultsMsgs }
func (m ArenaResultsSnapshotMessage) EncodePayload() []byte {
	return appendArenaResultsBody([]byte{4}, m.Results)
}

func appendArenaResultsBody(payload []byte, result ArenaResultsBody) []byte {
	payload = binary.LittleEndian.AppendUint32(payload, result.SelectorA)
	payload = binary.LittleEndian.AppendUint32(payload, result.SelectorB)
	payload = binary.LittleEndian.AppendUint32(payload, result.CountA)
	payload = binary.LittleEndian.AppendUint32(payload, result.CountB)
	payload = binary.LittleEndian.AppendUint32(payload, uint32(result.RoundTimeSeconds))
	for _, amount := range result.DNAAmount {
		payload = binary.LittleEndian.AppendUint32(payload, amount)
	}
	for _, player := range result.Player {
		payload = binary.LittleEndian.AppendUint64(payload, player.PlayerID)
		payload = binary.LittleEndian.AppendUint32(payload, player.AvatarID)
		payload = append(payload, player.Team)
		payload = binary.LittleEndian.AppendUint32(payload, player.Kills)
		payload = binary.LittleEndian.AppendUint32(payload, player.Deaths)
		for _, amount := range [...]float32{
			player.DamageDealt, player.DamageTaken, player.HealingDealt, player.HealingReceived,
		} {
			payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(amount))
		}
		for _, round := range player.Round {
			payload = binary.LittleEndian.AppendUint32(payload, round.Outcome)
			payload = binary.LittleEndian.AppendUint32(payload, round.Field04)
			for _, resource := range round.CreatureResource {
				payload = binary.LittleEndian.AppendUint64(payload, resource)
			}
			for _, amount := range round.HealthPercent {
				payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(amount))
			}
			for _, amount := range round.ManaPercent {
				payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(amount))
			}
		}
	}
	return payload
}

type KillRaceGameExitMessage struct {
	Opaque [7]byte
}

func (KillRaceGameExitMessage) PacketID() PacketID { return KillRaceGameMsgs }
func (m KillRaceGameExitMessage) EncodePayload() []byte {
	return append([]byte{7}, m.Opaque[:]...)
}

type KillRaceGameSelectResultsMessage struct{}

func (KillRaceGameSelectResultsMessage) PacketID() PacketID { return KillRaceGameMsgs }
func (KillRaceGameSelectResultsMessage) EncodePayload() []byte {
	return []byte{8}
}

type KillRaceResultsMessage struct {
	Subtype uint8
}

func (KillRaceResultsMessage) PacketID() PacketID { return KillRaceResultsMsgs }
func (m KillRaceResultsMessage) EncodePayload() []byte {
	return []byte{m.Subtype}
}

// JuggernautGameExitMessage is subtype 3's packed eight-byte body. Build 103
// copies the four retained fields into shared mode state before exiting.
type JuggernautGameExitMessage struct {
	Field01 uint8
	Field02 uint16
	Field04 uint16
	Field06 uint16
}

func (JuggernautGameExitMessage) PacketID() PacketID { return JuggernautGameMsgs }
func (m JuggernautGameExitMessage) EncodePayload() []byte {
	payload := []byte{3, m.Field01}
	payload = binary.LittleEndian.AppendUint16(payload, m.Field02)
	payload = binary.LittleEndian.AppendUint16(payload, m.Field04)
	return binary.LittleEndian.AppendUint16(payload, m.Field06)
}

// JuggernautGameSelectResultsMessage selects state 18. It has no trailing
// body after subtype 4.
type JuggernautGameSelectResultsMessage struct{}

func (JuggernautGameSelectResultsMessage) PacketID() PacketID { return JuggernautGameMsgs }
func (JuggernautGameSelectResultsMessage) EncodePayload() []byte {
	return []byte{4}
}

// JuggernautResultsMessage is the one-byte no-op discriminator consumed by
// the results state. Subtype 2 is the only explicitly handled value.
type JuggernautResultsMessage struct {
	Subtype uint8
}

func (JuggernautResultsMessage) PacketID() PacketID { return JuggernautResultsMsgs }
func (m JuggernautResultsMessage) EncodePayload() []byte {
	return []byte{m.Subtype}
}

// TutorialCompleteMessage is build 103's TutorialGameMsgs subtype 0. The
// value is cumulative account XP, not an XP delta; the client treats a
// positive value as successful tutorial completion and zero/negative as an
// incomplete reset.
type TutorialCompleteMessage struct {
	CumulativeXP int32
}

func (TutorialCompleteMessage) PacketID() PacketID { return TutorialGameMsgs }
func (m TutorialCompleteMessage) EncodePayload() []byte {
	payload := []byte{0}
	return binary.LittleEndian.AppendUint32(payload, uint32(m.CumulativeXP))
}

// TutorialGameplayTransitionMessage is the bodyless subtype-zero form
// consumed by active generic gameplay rather than cTutorialGameState.
type TutorialGameplayTransitionMessage struct{}

func (TutorialGameplayTransitionMessage) PacketID() PacketID { return TutorialGameMsgs }
func (TutorialGameplayTransitionMessage) EncodePayload() []byte {
	return []byte{0}
}
