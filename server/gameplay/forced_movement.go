package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
)

// Reassert both native resource views after the forced reaction changes the
// locally controlled actor's presentation. This never heals the server hero.
func (e gameplayPeerSession) forcedMovementResources() ([][]byte, error) {
	worldPacket, err := raknet.MarshalApplication(raknet.CombatantDataUpdateMessage{
		ObjectID:  e.deployedObjectID,
		HitPoints: e.deployedHitPoint(), ManaPoints: e.deployedManaPoint(),
	})
	if err != nil {
		return nil, fmt.Errorf("forcedWorldResource: %w", err)
	}
	hudPacket, err := e.marshalCampaignCharacterResource(e.deployedCreatureIndex)
	if err != nil {
		return nil, fmt.Errorf("forcedHUDResource: %w", err)
	}
	return [][]byte{worldPacket, hudPacket}, nil
}
