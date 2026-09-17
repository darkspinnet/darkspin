package raknet103

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
)

func Complete(cumulativeXP int32) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.TutorialCompleteMessage{
		CumulativeXP: cumulativeXP,
	})
	if err != nil {
		return nil, fmt.Errorf("completeMarshal: %w", err)
	}
	return packet, nil
}
