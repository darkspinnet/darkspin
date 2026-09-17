package raknet103

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
)

func Countdown(seconds float32) ([]byte, error) {
	if seconds == 0 {
		return nil, errors.New("result countdown invalid")
	}
	packet, err := raknet.MarshalApplication(
		raknet.ChainVoteCountdownMessage{Seconds: seconds},
	)
	if err != nil {
		return nil, fmt.Errorf("countdownMarshal: %w", err)
	}
	return packet, nil
}
