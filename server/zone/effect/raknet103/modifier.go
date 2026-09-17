package raknet103

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
)

type ModifierCreateRequest struct {
	SourceObjectID uint32
	TargetObjectID uint32
	ModifierID     uint32
	InstanceID     uint32
	StackCount     uint32
	Duration       time.Duration
	Timestamp      uint64
}

func ModifierCreate(req ModifierCreateRequest) ([]byte, error) {
	if req.SourceObjectID == 0 || req.TargetObjectID == 0 ||
		req.ModifierID == 0 || req.InstanceID == 0 || req.Duration <= 0 {
		return nil, errors.New("modifier create invalid")
	}
	stackCount := req.StackCount
	if stackCount == 0 {
		stackCount = 1
	}
	packet, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID: req.TargetObjectID, ModifierGUID: req.ModifierID,
		InstanceID:           req.InstanceID,
		DurationMilliseconds: uint32(req.Duration.Milliseconds()),
		StackCount:           stackCount,
		StartMilliseconds:    req.Timestamp,
		SourceID:             req.SourceObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("modifierCreateMarshal: %w", err)
	}
	return packet, nil
}

func ModifierDelete(targetObjectID uint32, instanceID uint32) ([]byte, error) {
	if targetObjectID == 0 || instanceID == 0 {
		return nil, errors.New("modifier delete invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.ModifierDeletedMessage{
		TargetID: targetObjectID, InstanceID: instanceID,
	})
	if err != nil {
		return nil, fmt.Errorf("modifierDeleteMarshal: %w", err)
	}
	return packet, nil
}
