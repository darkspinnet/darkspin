package raknet103

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/raknet"
)

type ProgressionRequest struct {
	PlayerIndex uint8
	AvatarLevel uint32
	AvatarXP    float32
}

func Progression(req ProgressionRequest) ([]byte, error) {
	if math.IsNaN(float64(req.AvatarXP)) || math.IsInf(float64(req.AvatarXP), 0) ||
		req.AvatarXP < 0 {
		return nil, errors.New("hero progression invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.LabsPlayerProgressionMessage{
		Slot: req.PlayerIndex, AvatarLevel: req.AvatarLevel, AvatarXP: req.AvatarXP,
	})
	if err != nil {
		return nil, fmt.Errorf("progressionMarshal: %w", err)
	}
	return packet, nil
}
