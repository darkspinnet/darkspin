package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
)

// GlobalDefinitions assigns Immobilized to attribute 15. The root template and
// CryosBossShock's Activate add one; ModifierCreated only presents their effects.
const enemyImmobilizedAttribute = uint8(15)

// Refresh from the authoritative deadline rather than modifier removal alone:
// deployment, roots and stuns can overlap, be cleansed, or end when the deployed
// hero changes. The existing gameplay poll releases expired holds and restores
// the current attribute for rejoining clients.
// The caller holds the registry lock and saves the updated session.
func (e *gameplayPeerSession) refreshEnemyImmobilizedAttributes(at time.Time) ([][]byte, error) {
	objectID := uint32(0)
	if e.heroInputLockedObjectID == e.deployedObjectID && e.isHeroArrivalProtected(at) ||
		e.isSleepingCloudActive(at) || e.isEnemySleepActive(at) || e.isEnemyRootActive(at) || e.isArenaBanished(at) || e.deployedObjectID == e.enemyStunTargetObjectID &&
		at.Before(e.enemyStunExpiresAt) || e.isArenaHitStunActive(at) {
		// Do not use isEnemyStunActive here: its knockback/cage guards also
		// protect input while a forced native mover owns the hero.
		objectID = e.deployedObjectID
	}
	packets := make([][]byte, 0, 2)
	if e.enemyControlPresentedObjectID != 0 && e.enemyControlPresentedObjectID != objectID {
		packet, err := marshalEnemyImmobilizedAttribute(e.enemyControlPresentedObjectID, 0)
		if err != nil {
			return nil, fmt.Errorf("immobilizedClear: %w", err)
		}
		packets = append(packets, packet)
	}
	if objectID != 0 {
		packet, err := marshalEnemyImmobilizedAttribute(objectID, 1)
		if err != nil {
			return nil, fmt.Errorf("immobilizedHold: %w", err)
		}
		packets = append(packets, packet)
	}
	e.enemyControlPresentedObjectID = objectID
	frozenPackets, err := e.refreshACFrozenAttribute()
	if err != nil {
		return nil, fmt.Errorf("frozenRefresh: %w", err)
	}
	return append(packets, frozenPackets...), nil
}

func marshalEnemyImmobilizedAttribute(objectID uint32, amount float32) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: objectID, Value: map[uint8]float32{enemyImmobilizedAttribute: amount},
	})
	if err != nil {
		return nil, fmt.Errorf("immobilizedAttribute: %w", err)
	}
	return packet, nil
}
