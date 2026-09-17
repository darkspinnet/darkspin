package raknet103

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

type CooldownRequest struct {
	ObjectID  uint32
	AbilityID uint32
	Duration  time.Duration
	StartTime uint64
}

type AcknowledgeRequest struct {
	SyncStamp                uint8
	ResponseType             raknet.ActionResponseType
	ObjectID                 uint32
	AbilityIndex             uint32
	SourceStartMilliseconds  uint64
	SourceCommitMilliseconds uint64
	SourceEndMilliseconds    uint64
}

func Acknowledge(req AcknowledgeRequest) ([]byte, error) {
	if req.ObjectID == 0 {
		return nil, errors.New("ability acknowledge invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.ActionCommandResponseMessage{
		SyncStamp: req.SyncStamp, ResponseType: req.ResponseType,
		ObjectID: req.ObjectID, AbilityIndex: req.AbilityIndex,
		SourceStartMilliseconds:  req.SourceStartMilliseconds,
		SourceCommitMilliseconds: req.SourceCommitMilliseconds,
		SourceEndMilliseconds:    req.SourceEndMilliseconds,
		UserData:                 0xffffffff,
	})
	if err != nil {
		return nil, fmt.Errorf("acknowledgeMarshal: %w", err)
	}
	return packet, nil
}

func Cooldown(req CooldownRequest) ([]byte, error) {
	if req.ObjectID == 0 || req.AbilityID == 0 || req.Duration < 0 {
		return nil, errors.New("ability cooldown invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.CooldownUpdateMessage{
		ObjectID: req.ObjectID, AbilityKey: uint64(req.AbilityID),
		DurationMilliseconds:    req.Duration.Milliseconds(),
		SourceStartMilliseconds: int64(req.StartTime),
	})
	if err != nil {
		return nil, fmt.Errorf("cooldownMarshal: %w", err)
	}
	return packet, nil
}

func CooldownReset(objectID uint32, abilityID uint32) ([]byte, error) {
	if objectID == 0 || abilityID == 0 {
		return nil, errors.New("ability cooldown reset invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.CooldownUpdateMessage{
		ObjectID: objectID, AbilityKey: uint64(abilityID),
	})
	if err != nil {
		return nil, fmt.Errorf("cooldownResetMarshal: %w", err)
	}
	return packet, nil
}

func Mana(objectID uint32, manaPoint float32) ([]byte, error) {
	if objectID == 0 || math.IsNaN(float64(manaPoint)) ||
		math.IsInf(float64(manaPoint), 0) || manaPoint < 0 {
		return nil, errors.New("ability mana invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.CombatantDataDeltaMessage{
		ObjectID: objectID, ManaPoints: manaPoint, IsManaPointChanged: true,
	})
	if err != nil {
		return nil, fmt.Errorf("manaMarshal: %w", err)
	}
	return packet, nil
}

func Animation(
	objectID uint32, animationName string, timestamp uint64,
) ([]byte, error) {
	if objectID == 0 || animationName == "" {
		return nil, errors.New("ability animation invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
		ObjectID: objectID, State: util.HashID(animationName),
		Timestamp: timestamp, Scale: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("animationMarshal: %w", err)
	}
	return packet, nil
}

func AnimationReset(objectID uint32, timestamp uint64) ([]byte, error) {
	if objectID == 0 {
		return nil, errors.New("ability animation reset invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.SetAnimationStateMessage{
		ObjectID: objectID, Timestamp: timestamp, Scale: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("animationResetMarshal: %w", err)
	}
	return packet, nil
}

func StartPresentation(
	objectID uint32, abilityID uint32, animationName string,
	cooldown time.Duration, sourceTime uint64, cooldownDelay time.Duration,
) ([][]byte, error) {
	animationPacket, err := Animation(objectID, animationName, sourceTime)
	if err != nil {
		return nil, fmt.Errorf("startAnimation: %w", err)
	}
	cooldownPacket, err := Cooldown(CooldownRequest{
		ObjectID: objectID, AbilityID: abilityID, Duration: cooldown,
		StartTime: sourceTime + uint64(cooldownDelay/time.Millisecond),
	})
	if err != nil {
		return nil, fmt.Errorf("startCooldown: %w", err)
	}
	return [][]byte{animationPacket, cooldownPacket}, nil
}

func SpendPresentation(
	objectID uint32, abilityID uint32, cooldown time.Duration,
	sourceTime uint64, manaPoint float32,
) ([][]byte, error) {
	cooldownPacket, err := Cooldown(CooldownRequest{
		ObjectID: objectID, AbilityID: abilityID,
		Duration: cooldown, StartTime: sourceTime,
	})
	if err != nil {
		return nil, fmt.Errorf("spendCooldown: %w", err)
	}
	manaPacket, err := Mana(objectID, manaPoint)
	if err != nil {
		return nil, fmt.Errorf("spendMana: %w", err)
	}
	return [][]byte{cooldownPacket, manaPacket}, nil
}

func StartSpendPresentation(
	objectID uint32, abilityID uint32, animationName string,
	cooldown time.Duration, sourceTime uint64, manaPoint float32,
) ([][]byte, error) {
	startPacket, err := StartPresentation(
		objectID, abilityID, animationName, cooldown, sourceTime, 0,
	)
	if err != nil {
		return nil, fmt.Errorf("startSpendPresentation: %w", err)
	}
	manaPacket, err := Mana(objectID, manaPoint)
	if err != nil {
		return nil, fmt.Errorf("startSpendMana: %w", err)
	}
	return append(startPacket, manaPacket), nil
}
