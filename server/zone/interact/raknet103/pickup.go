package raknet103

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
)

func DeletePickup(objectID uint32) ([]byte, error) {
	if objectID == 0 {
		return nil, errors.New("invalid pickup object")
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{
		ObjectID: []uint32{objectID},
	})
	if err != nil {
		return nil, fmt.Errorf("pickupDelete: %w", err)
	}
	return packet, nil
}
