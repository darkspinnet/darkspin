package raknet

import (
	"encoding/binary"
	"math"
)

// ObjectReflection is build 103's complete 23-field cObject reflection.
// Pointer presence selects the corresponding sparse field, including false and
// zero values that must still be transmitted.
type ObjectReflection struct {
	Team                           *uint8
	IsPlayerControlled             *bool
	InputSyncStamp                 *uint32
	PlayerIndex                    *uint8
	LinearVelocity                 *Vector3
	AngularVelocity                *Vector3
	Position                       *Vector3
	Orientation                    *Quaternion
	Scale                          *float32
	MarkerScale                    *float32
	LastAnimationState             *uint32
	LastAnimationPlayMilliseconds  *uint64
	OverrideMoveIdleAnimationState *uint32
	GraphicsState                  *uint32
	GraphicsStateStartMilliseconds *uint64
	NewGraphicsStateMilliseconds   *uint64
	IsVisible                      *bool
	IsCollisionEnabled             *bool
	OwnerID                        *uint32
	MovementType                   *uint8
	IsRepulsionDisabled            *bool
	InteractableState              *uint32
	SourceMarkerID                 *uint32
}

// ObjectCreateContractMessage exposes every optional field in the exact
// create prefix and complete object-tail reflection.
type ObjectCreateContractMessage struct {
	ObjectID           uint32
	Noun               *uint32
	Position           *Vector3
	Rotation           *Vector3
	AssetID            *uint64
	Scale              *float32
	Team               *uint8
	IsCollisionEnabled *bool
	IsPlayerControlled *bool
	Object             ObjectReflection
}

func (ObjectCreateContractMessage) PacketID() PacketID { return ObjectCreate }
func (m ObjectCreateContractMessage) EncodePayload() []byte {
	mask := uint16(0)
	if m.Noun != nil {
		mask |= 1 << 0
	}
	if m.Position != nil {
		mask |= 1 << 1
	}
	if m.Rotation != nil {
		mask |= 1 << 2
		mask |= 1 << 3
		mask |= 1 << 4
	}
	if m.AssetID != nil {
		mask |= 1 << 5
	}
	if m.Scale != nil {
		mask |= 1 << 6
	}
	if m.Team != nil {
		mask |= 1 << 7
	}
	if m.IsCollisionEnabled != nil {
		mask |= 1 << 8
	}
	if m.IsPlayerControlled != nil {
		mask |= 1 << 9
	}
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	payload = binary.LittleEndian.AppendUint16(payload, mask)
	if m.Noun != nil {
		payload = binary.LittleEndian.AppendUint32(payload, *m.Noun)
	}
	if m.Position != nil {
		payload = appendVector3(payload, m.Position.X, m.Position.Y, m.Position.Z)
	}
	if m.Rotation != nil {
		for _, angle := range [...]float32{m.Rotation.X, m.Rotation.Y, m.Rotation.Z} {
			payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(angle))
		}
	}
	if m.AssetID != nil {
		payload = binary.LittleEndian.AppendUint64(payload, *m.AssetID)
	}
	if m.Scale != nil {
		payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(*m.Scale))
	}
	if m.Team != nil {
		payload = append(payload, *m.Team)
	}
	if m.IsCollisionEnabled != nil {
		payload = append(payload, boolByte(*m.IsCollisionEnabled))
	}
	if m.IsPlayerControlled != nil {
		payload = append(payload, boolByte(*m.IsPlayerControlled))
	}
	return appendObjectReflection(payload, m.Object)
}

type ObjectUpdateContractMessage struct {
	ObjectID uint32
	Object   ObjectReflection
}

func (ObjectUpdateContractMessage) PacketID() PacketID { return ObjectUpdate }
func (m ObjectUpdateContractMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	return appendObjectReflection(payload, m.Object)
}

func appendObjectReflection(payload []byte, object ObjectReflection) []byte {
	if object.Team != nil {
		payload = append(payload, 0, *object.Team)
	}
	if object.IsPlayerControlled != nil {
		payload = append(payload, 1, boolByte(*object.IsPlayerControlled))
	}
	if object.InputSyncStamp != nil {
		payload = append(payload, 2)
		payload = binary.LittleEndian.AppendUint32(payload, *object.InputSyncStamp)
	}
	if object.PlayerIndex != nil {
		payload = append(payload, 3, *object.PlayerIndex)
	}
	for _, field := range [...]struct {
		index  uint8
		vector *Vector3
	}{
		{4, object.LinearVelocity},
		{5, object.AngularVelocity},
		{6, object.Position},
	} {
		if field.vector != nil {
			payload = append(payload, field.index)
			payload = appendVector3(payload, field.vector.X, field.vector.Y, field.vector.Z)
		}
	}
	if object.Orientation != nil {
		payload = append(payload, 7)
		payload = appendQuaternion(payload, *object.Orientation)
	}
	for _, field := range [...]struct {
		index  uint8
		amount *float32
	}{
		{8, object.Scale},
		{9, object.MarkerScale},
	} {
		if field.amount != nil {
			payload = append(payload, field.index)
			payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(*field.amount))
		}
	}
	if object.LastAnimationState != nil {
		payload = append(payload, 10)
		payload = binary.LittleEndian.AppendUint32(payload, *object.LastAnimationState)
	}
	if object.LastAnimationPlayMilliseconds != nil {
		payload = append(payload, 11)
		payload = binary.LittleEndian.AppendUint64(payload, *object.LastAnimationPlayMilliseconds)
	}
	if object.OverrideMoveIdleAnimationState != nil {
		payload = append(payload, 12)
		payload = binary.LittleEndian.AppendUint32(payload, *object.OverrideMoveIdleAnimationState)
	}
	if object.GraphicsState != nil {
		payload = append(payload, 13)
		payload = binary.LittleEndian.AppendUint32(payload, *object.GraphicsState)
	}
	if object.GraphicsStateStartMilliseconds != nil {
		payload = append(payload, 14)
		payload = binary.LittleEndian.AppendUint64(payload, *object.GraphicsStateStartMilliseconds)
	}
	if object.NewGraphicsStateMilliseconds != nil {
		payload = append(payload, 15)
		payload = binary.LittleEndian.AppendUint64(payload, *object.NewGraphicsStateMilliseconds)
	}
	for _, field := range [...]struct {
		index uint8
		isSet *bool
	}{
		{16, object.IsVisible},
		{17, object.IsCollisionEnabled},
	} {
		if field.isSet != nil {
			payload = append(payload, field.index, boolByte(*field.isSet))
		}
	}
	if object.OwnerID != nil {
		payload = append(payload, 18)
		payload = binary.LittleEndian.AppendUint32(payload, *object.OwnerID)
	}
	if object.MovementType != nil {
		payload = append(payload, 19, *object.MovementType)
	}
	if object.IsRepulsionDisabled != nil {
		payload = append(payload, 20, boolByte(*object.IsRepulsionDisabled))
	}
	if object.InteractableState != nil {
		payload = append(payload, 21)
		payload = binary.LittleEndian.AppendUint32(payload, *object.InteractableState)
	}
	if object.SourceMarkerID != nil {
		payload = append(payload, 22)
		payload = binary.LittleEndian.AppendUint32(payload, *object.SourceMarkerID)
	}
	return append(payload, 0xff)
}

// LocomotionReflection is the complete 18-field reliable cLocomotionData
// reflection. Presence selects a sparse field.
type LocomotionReflection struct {
	LobStartMilliseconds      *uint64
	PreviousLobSpeedModifier  *float32
	Lob                       *LobParameter
	Projectile                *ProjectileParameter
	GoalFlags                 *uint32
	GoalPosition              *Vector3
	PartialGoalPosition       *Vector3
	Facing                    *Vector3
	ExternalLinearVelocity    *Vector3
	ExternalForce             *Vector3
	AllowedStopDistance       *float32
	DesiredStopDistance       *float32
	TargetObjectID            *uint32
	TargetPosition            *Vector3
	ExpectedGeometryCollision *Vector3
	InitialDirection          *Vector3
	Offset                    *Vector3
	ReflectedLastUpdate       *int32
}

type LocomotionUpdateContractMessage struct {
	ObjectID   uint32
	Locomotion LocomotionReflection
}

func (LocomotionUpdateContractMessage) PacketID() PacketID { return LocomotionUpdate }
func (m LocomotionUpdateContractMessage) EncodePayload() []byte {
	payload := binary.LittleEndian.AppendUint32(nil, m.ObjectID)
	locomotion := m.Locomotion
	if locomotion.LobStartMilliseconds != nil {
		payload = append(payload, 0)
		payload = binary.LittleEndian.AppendUint64(payload, *locomotion.LobStartMilliseconds)
	}
	if locomotion.PreviousLobSpeedModifier != nil {
		payload = append(payload, 1)
		payload = binary.LittleEndian.AppendUint32(payload,
			math.Float32bits(*locomotion.PreviousLobSpeedModifier))
	}
	if locomotion.Lob != nil {
		payload = append(payload, 2)
		payload = appendLobParameter(payload, *locomotion.Lob)
	}
	if locomotion.Projectile != nil {
		payload = append(payload, 3)
		payload = appendProjectileParameter(payload, *locomotion.Projectile)
	}
	if locomotion.GoalFlags != nil {
		payload = append(payload, 4)
		payload = binary.LittleEndian.AppendUint32(payload, *locomotion.GoalFlags)
	}
	for _, field := range [...]struct {
		index  uint8
		vector *Vector3
	}{
		{5, locomotion.GoalPosition},
		{6, locomotion.PartialGoalPosition},
		{7, locomotion.Facing},
		{8, locomotion.ExternalLinearVelocity},
		{9, locomotion.ExternalForce},
	} {
		if field.vector != nil {
			payload = append(payload, field.index)
			payload = appendVector3(payload, field.vector.X, field.vector.Y, field.vector.Z)
		}
	}
	for _, field := range [...]struct {
		index  uint8
		amount *float32
	}{
		{10, locomotion.AllowedStopDistance},
		{11, locomotion.DesiredStopDistance},
	} {
		if field.amount != nil {
			payload = append(payload, field.index)
			payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(*field.amount))
		}
	}
	if locomotion.TargetObjectID != nil {
		payload = append(payload, 12)
		payload = binary.LittleEndian.AppendUint32(payload, *locomotion.TargetObjectID)
	}
	for _, field := range [...]struct {
		index  uint8
		vector *Vector3
	}{
		{13, locomotion.TargetPosition},
		{14, locomotion.ExpectedGeometryCollision},
		{15, locomotion.InitialDirection},
		{16, locomotion.Offset},
	} {
		if field.vector != nil {
			payload = append(payload, field.index)
			payload = appendVector3(payload, field.vector.X, field.vector.Y, field.vector.Z)
		}
	}
	if locomotion.ReflectedLastUpdate != nil {
		payload = append(payload, 17)
		payload = binary.LittleEndian.AppendUint32(payload, uint32(*locomotion.ReflectedLastUpdate))
	}
	return append(payload, 0xff)
}

// ServerEventContractMessage exposes all 26 registered fields. It is intended
// for exact protocol work; higher-level event recipes should continue using
// their narrower allowlisted message types.
type ServerEventContractMessage struct {
	SimpleSwarmEffectID      *uint32
	EffectSlot               *uint8
	IsRemovalRequested       *bool
	IsHardStop               *bool
	IsForceAttached          *bool
	IsCritical               *bool
	Asset                    *uint32
	ObjectID                 *uint32
	SecondaryObjectID        *uint32
	AttackerID               *uint32
	Position                 *Vector3
	Facing                   *Vector3
	Orientation              *Quaternion
	TargetPoint              *Vector3
	Text                     *int32
	ClientEventID            *uint32
	PlayerExclusionMask      *uint8
	LootReferenceID          *uint64
	LootInstanceID           *uint64
	LootRigblockID           *uint32
	LootSuffixAsset          *uint32
	LootPrefixAsset          *uint32
	LootSecondaryPrefixAsset *uint32
	LootItemLevel            *int32
	LootRarity               *int32
	LootCreationMilliseconds *uint64
}

func (ServerEventContractMessage) PacketID() PacketID { return ServerEvent }
func (m ServerEventContractMessage) EncodePayload() []byte {
	payload := make([]byte, 0, 160)
	for _, field := range [...]struct {
		index   uint8
		integer *uint32
	}{
		{0, m.SimpleSwarmEffectID},
	} {
		if field.integer != nil {
			payload = append(payload, field.index)
			payload = binary.LittleEndian.AppendUint32(payload, *field.integer)
		}
	}
	if m.EffectSlot != nil {
		payload = append(payload, 1, *m.EffectSlot)
	}
	for _, field := range [...]struct {
		index uint8
		isSet *bool
	}{
		{2, m.IsRemovalRequested},
		{3, m.IsHardStop},
		{4, m.IsForceAttached},
		{5, m.IsCritical},
	} {
		if field.isSet != nil {
			payload = append(payload, field.index, boolByte(*field.isSet))
		}
	}
	for _, field := range [...]struct {
		index   uint8
		integer *uint32
	}{
		{6, m.Asset},
		{7, m.ObjectID},
		{8, m.SecondaryObjectID},
		{9, m.AttackerID},
	} {
		if field.integer != nil {
			payload = append(payload, field.index)
			payload = binary.LittleEndian.AppendUint32(payload, *field.integer)
		}
	}
	for _, field := range [...]struct {
		index  uint8
		vector *Vector3
	}{
		{10, m.Position},
		{11, m.Facing},
	} {
		if field.vector != nil {
			payload = append(payload, field.index)
			payload = appendVector3(payload, field.vector.X, field.vector.Y, field.vector.Z)
		}
	}
	if m.Orientation != nil {
		payload = append(payload, 12)
		payload = appendQuaternion(payload, *m.Orientation)
	}
	if m.TargetPoint != nil {
		payload = append(payload, 13)
		payload = appendVector3(payload, m.TargetPoint.X, m.TargetPoint.Y, m.TargetPoint.Z)
	}
	if m.Text != nil {
		payload = append(payload, 14)
		payload = binary.LittleEndian.AppendUint32(payload, uint32(*m.Text))
	}
	if m.ClientEventID != nil {
		payload = append(payload, 15)
		payload = binary.LittleEndian.AppendUint32(payload, *m.ClientEventID)
	}
	if m.PlayerExclusionMask != nil {
		payload = append(payload, 16, *m.PlayerExclusionMask)
	}
	for _, field := range [...]struct {
		index   uint8
		integer *uint64
	}{
		{17, m.LootReferenceID},
		{18, m.LootInstanceID},
	} {
		if field.integer != nil {
			payload = append(payload, field.index)
			payload = binary.LittleEndian.AppendUint64(payload, *field.integer)
		}
	}
	for _, field := range [...]struct {
		index   uint8
		integer *uint32
	}{
		{19, m.LootRigblockID},
		{20, m.LootSuffixAsset},
		{21, m.LootPrefixAsset},
		{22, m.LootSecondaryPrefixAsset},
	} {
		if field.integer != nil {
			payload = append(payload, field.index)
			payload = binary.LittleEndian.AppendUint32(payload, *field.integer)
		}
	}
	for _, field := range [...]struct {
		index   uint8
		integer *int32
	}{
		{23, m.LootItemLevel},
		{24, m.LootRarity},
	} {
		if field.integer != nil {
			payload = append(payload, field.index)
			payload = binary.LittleEndian.AppendUint32(payload, uint32(*field.integer))
		}
	}
	if m.LootCreationMilliseconds != nil {
		payload = append(payload, 25)
		payload = binary.LittleEndian.AppendUint64(payload, *m.LootCreationMilliseconds)
	}
	return append(payload, 0xff)
}
