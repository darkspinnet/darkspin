package raknet103

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
)

func Active(leaderObjectID uint32, isFinalBoss bool) ([]byte, error) {
	if leaderObjectID == 0 {
		return nil, errors.New("boss active: zero leader")
	}
	packet, err := raknet.MarshalApplication(raknet.DirectorStateMessage{
		IsBossSpawned:    isFinalBoss,
		IsBossHorde:      isFinalBoss,
		IsCaptainSpawned: !isFinalBoss,
		IsHordeSpawned:   true,
		BossID:           leaderObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("bossActiveMarshal: %w", err)
	}
	return packet, nil
}

func AddPhase() ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.DirectorStateMessage{
		IsHordeSpawned: true,
	})
	if err != nil {
		return nil, fmt.Errorf("bossAddMarshal: %w", err)
	}
	return packet, nil
}

func Complete(leaderObjectID uint32) ([]byte, error) {
	if leaderObjectID == 0 {
		return nil, errors.New("boss complete: zero leader")
	}
	packet, err := raknet.MarshalApplication(raknet.DirectorStateMessage{
		IsBossComplete:        true,
		IsHordeSpawnedPresent: true,
		BossID:                leaderObjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("bossCompleteMarshal: %w", err)
	}
	return packet, nil
}
