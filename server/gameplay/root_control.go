package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
)

// GlobalDefinitions assigns Immobilized to attribute 15. The root template's
// Activate adds one; ModifierCreated only presents its icon and effects.
const enemyRootImmobilizedAttribute = uint8(15)

// Refresh from the authoritative deadline rather than modifier removal alone:
// roots can overlap, be cleansed, or end when the deployed hero changes. Keep
// publishing the active attribute so a rejoining client receives it as well.
// The caller holds the registry lock and saves the updated session.
func (e *gameplayPeerSession) refreshEnemyRootAttributes(at time.Time) ([][]byte, error) {
	objectID := uint32(0)
	if e.isEnemyRootActive(at) {
		objectID = e.deployedObjectID
	}
	packets := make([][]byte, 0, 2)
	if e.enemyRootPresentedObjectID != 0 && e.enemyRootPresentedObjectID != objectID {
		packet, err := marshalEnemyRootAttribute(e.enemyRootPresentedObjectID, 0)
		if err != nil {
			return nil, fmt.Errorf("rootClear: %w", err)
		}
		packets = append(packets, packet)
	}
	if objectID != 0 {
		packet, err := marshalEnemyRootAttribute(objectID, 1)
		if err != nil {
			return nil, fmt.Errorf("rootHold: %w", err)
		}
		packets = append(packets, packet)
	}
	e.enemyRootPresentedObjectID = objectID
	return packets, nil
}

func marshalEnemyRootAttribute(objectID uint32, amount float32) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: objectID, Value: map[uint8]float32{enemyRootImmobilizedAttribute: amount},
	})
	if err != nil {
		return nil, fmt.Errorf("rootAttribute: %w", err)
	}
	return packet, nil
}
