package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// The forced strike/scream state must relinquish animation ownership before
// locomotion resumes. Stealth modifier expiry is separate from cast recovery.
func (e campaignStealtherSchedule) resetAnimation() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	defer e.runtime.registry.mutex.RUnlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || !peerSession.isCampaignNPCSourceGenerationActive(
		e.generation, e.objectID, e.plan.ActionGeneration,
	) {
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(e.objectID)
	if !isEnemyFound {
		return nil, nil
	}
	// The scream's turn goal outlives its forced animation. Clear locomotion
	// before handing animation control back to idle during the cooldown.
	stopPacket, err := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{
		ObjectID: e.objectID, GoalFlags: 0x20,
		GoalPosition: raknet.Vector3(enemy.Plan.Position),
	})
	if err != nil {
		return nil, fmt.Errorf("stealthRecoveryStop: %w", err)
	}
	timestamp := e.timestamp + uint64(e.plan.Profile.ReleaseDelay/time.Millisecond)
	packet, err := npcraknet.ResetAnimation(e.objectID, timestamp)
	if err != nil {
		return nil, fmt.Errorf("stealthRecoveryReset: %w", err)
	}
	return [][]byte{stopPacket, packet}, nil
}
