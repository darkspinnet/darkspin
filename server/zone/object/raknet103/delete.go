package raknet103

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
)

func Delete(objectIDs []uint32) ([]byte, error) {
	if len(objectIDs) == 0 {
		return nil, errors.New("object delete empty")
	}
	for index, objectID := range objectIDs {
		if objectID == 0 {
			return nil, fmt.Errorf("objectDelete[%d]: zero", index)
		}
	}
	packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{
		ObjectID: objectIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("objectDeleteMarshal: %w", err)
	}
	return packet, nil
}
