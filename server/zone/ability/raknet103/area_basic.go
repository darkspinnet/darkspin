package raknet103

import (
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

type AreaBasicStartRequest struct {
	SyncStamp        uint8
	SourceID         uint32
	AbilityID        uint32
	AbilityIndex     uint32
	SourceTime       uint64
	AnimationName    string
	MuzzleEffectName string
	HitDelay         time.Duration
	ReleaseDelay     time.Duration
	Cooldown         time.Duration
}

type AreaBasicStart struct {
	Acknowledge  []byte
	Release      []byte
	Presentation [][]byte
}

type PointBlankImpactRequest struct {
	AssetName  string
	ObjectID   uint32
	AttackerID uint32
	Source     raknet.Vector3
	Target     raknet.Vector3
	IsCritical bool
}

func StartAreaBasic(req AreaBasicStartRequest) (AreaBasicStart, error) {
	acknowledge, err := raknet.MarshalApplication(raknet.ActionCommandResponseMessage{
		SyncStamp: req.SyncStamp, ResponseType: raknet.ActionResponseAccepted,
		ObjectID: req.AbilityID, AbilityIndex: req.AbilityIndex,
		SourceStartMilliseconds:  req.SourceTime,
		SourceCommitMilliseconds: req.SourceTime + uint64(req.HitDelay/time.Millisecond),
		SourceEndMilliseconds:    req.SourceTime + uint64(req.ReleaseDelay/time.Millisecond),
		UserData:                 0xffffffff,
	})
	if err != nil {
		return AreaBasicStart{}, fmt.Errorf("acknowledgeMarshal: %w", err)
	}
	release, err := ReleaseResponse(
		req.SyncStamp, req.AbilityID, req.AbilityIndex,
		req.SourceTime, req.HitDelay, req.ReleaseDelay,
	)
	if err != nil {
		return AreaBasicStart{}, fmt.Errorf("releaseMarshal: %w", err)
	}
	presentationMessage := []raknet.ApplicationMessage{
		raknet.SetAnimationStateMessage{
			ObjectID: req.SourceID, State: util.HashID(req.AnimationName),
			Timestamp: req.SourceTime, Scale: 1,
		},
		raknet.CooldownUpdateMessage{
			ObjectID: req.SourceID, AbilityKey: uint64(req.AbilityID),
			DurationMilliseconds:    req.Cooldown.Milliseconds(),
			SourceStartMilliseconds: int64(req.SourceTime),
		},
	}
	if req.MuzzleEffectName != "" {
		presentationMessage = append(presentationMessage, raknet.ObjectEffectMessage{
			Asset: util.HashID(req.MuzzleEffectName), ObjectID: req.SourceID,
			AttackerID: req.SourceID,
		})
	}
	presentation, err := marshalMessages(presentationMessage, "areaBasicPresentation")
	if err != nil {
		return AreaBasicStart{}, fmt.Errorf("presentationMarshal: %w", err)
	}
	return AreaBasicStart{
		Acknowledge:  acknowledge,
		Release:      release,
		Presentation: presentation,
	}, nil
}

func CursorAreaImpact(assetName string, position raknet.Vector3) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.DropPresentationMessage{
		Asset: util.HashID(assetName), Position: position,
	})
	if err != nil {
		return nil, fmt.Errorf("cursorImpactMarshal: %w", err)
	}
	return packet, nil
}

func PointBlankImpact(req PointBlankImpactRequest) ([]byte, error) {
	facing := direction(req.Target, req.Source)
	packet, err := raknet.MarshalApplication(raknet.ObjectEffectMessage{
		IsCritical: req.IsCritical,
		Asset:      util.HashID(req.AssetName), ObjectID: req.ObjectID,
		AttackerID: req.AttackerID, Facing: facing,
	})
	if err != nil {
		return nil, fmt.Errorf("pointBlankImpactMarshal: %w", err)
	}
	return packet, nil
}

func direction(from raknet.Vector3, to raknet.Vector3) raknet.Vector3 {
	vector := raknet.Vector3{
		X: to.X - from.X,
		Y: to.Y - from.Y,
		Z: to.Z - from.Z,
	}
	lengthSquared := vector.X*vector.X + vector.Y*vector.Y + vector.Z*vector.Z
	if lengthSquared == 0 {
		return raknet.Vector3{X: 1}
	}
	length := float32(math.Sqrt(float64(lengthSquared)))
	return raknet.Vector3{
		X: vector.X / length,
		Y: vector.Y / length,
		Z: vector.Z / length,
	}
}
