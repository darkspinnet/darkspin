package raknet103

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
)

func GameOver() ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.GameOverMessage{})
	if err != nil {
		return nil, fmt.Errorf("gameOverMarshal: %w", err)
	}
	return packet, nil
}
