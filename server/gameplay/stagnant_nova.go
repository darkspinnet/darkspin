package gameplay

import (
	"fmt"
	"time"

	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// Above-ground Stagnant Nova returns to idle without entering its separate
// underground movement ability (Abilities/0xBC5FD585.lua, chunk 624).
func (e campaignConeSchedule) endStagnantNovaAbove() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	defer e.runtime.registry.mutex.RUnlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || !peerSession.isCampaignNPCSourceGenerationActive(
		e.generation, e.objectID, e.actionGeneration,
	) {
		return nil, nil
	}
	packet, err := npcraknet.AnimationState(
		e.objectID, e.plan.Profile.EndAnimationName,
		e.timestamp+uint64(e.plan.Profile.ReleaseDelay/time.Millisecond),
	)
	if err != nil {
		return nil, fmt.Errorf("novaIdle: %w", err)
	}
	return [][]byte{packet}, nil
}
