package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
)

// The caller holds the registry lock and saves the updated peer afterward.
// A control effect must stop the accepted route, not just reject future inputs.
func stopEnemyControlledHeroMovement(peerSession *gameplayPeerSession, at time.Time) ([][]byte, error) {
	// Retained native attacks still own a predicted response token. Answer it
	// before changing the held generation that guards their retry producer.
	err := peerSession.cancelPendingBasic()
	if err != nil {
		return nil, fmt.Errorf("controlPending: %w", err)
	}
	peerSession.campaignPlayerPursuitSession().Cancel()
	peerSession.basicSequenceSession().ReleaseHeld()
	peerSession.isBasicHeldAbilitySet = false
	err = peerSession.stopPlayerMovement(at)
	if err != nil {
		return nil, fmt.Errorf("controlStop: %w", err)
	}
	packets, err := marshalPlayerMovementCorrection(
		peerSession.deployedObjectID, peerSession.playerPosition, raknet.Quaternion{W: 1},
	)
	if err != nil {
		return nil, fmt.Errorf("controlMarshal: %w", err)
	}
	return packets, nil
}
