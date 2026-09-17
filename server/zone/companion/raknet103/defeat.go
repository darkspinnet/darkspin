package raknet103

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
)

func Defeat(objectID uint32) ([]byte, error) {
	if objectID == 0 {
		return nil, errors.New("companion defeat invalid")
	}
	packet, err := raknet.MarshalApplication(
		raknet.ObjectDeleteMessage{ObjectID: []uint32{objectID}},
	)
	if err != nil {
		return nil, fmt.Errorf("defeatMarshal: %w", err)
	}
	return packet, nil
}
