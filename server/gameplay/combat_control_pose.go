package gameplay

import (
	"fmt"
	"time"
)

// The caller holds the registry lock and saves the updated peer afterward.
// A control effect must stop the accepted route, not just reject future inputs.
func stopEnemyControlledHeroMovement(peerSession *gameplayPeerSession, at time.Time) ([][]byte, error) {
	err := peerSession.stopPlayerMovement(at)
	if err != nil {
		return nil, fmt.Errorf("controlStop: %w", err)
	}
	packets, err := marshalZonePlayerStop(peerSession.deployedObjectID, peerSession.playerPosition)
	if err != nil {
		return nil, fmt.Errorf("controlMarshal: %w", err)
	}
	return packets, nil
}
