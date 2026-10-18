package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const bossEngagementInterval = time.Second

type campaignBossCombatStep struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	zone       *zone.Zone
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
}

// A completed or interrupted action may release ownership. Keep the encounter
// running even when no player movement/attack arrives to reacquire the boss.
func (e campaignBossCombatStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && member.generation == e.generation && member.zone == e.zone &&
		!member.isZoneTerminal() && (e.zone.Boss() == nil || !e.zone.Boss().IsBeamOutCommitted())
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	boss, isBossFound := e.zone.NPCs().NPC(e.objectID)
	if !isBossFound || boss.IsDefeated || boss.HitPoint <= 0 {
		return nil, nil
	}
	packets := make([][]byte, 0)
	if !boss.IsActionStarted {
		acquired, err := e.zone.ReacquireBossTarget(e.objectID)
		if err != nil {
			return nil, fmt.Errorf("bossReacquire: %w", err)
		}
		plans := make([]zonenpc.SpawnPlan, 0, len(acquired))
		for _, candidate := range acquired {
			plans = append(plans, candidate.Plan)
		}
		packets, err = e.runtime.scheduleFirstActions(e.packet, e.sessionKey, e.generation, plans, e.timestamp)
		if err != nil {
			return nil, fmt.Errorf("bossResume: %w", err)
		}
	}
	e.timestamp += uint64(bossEngagementInterval / time.Millisecond)
	err := scheduleNPCProducer(e.runtime.registry, e.packet, bossEngagementInterval, e.produce)
	if err != nil {
		return nil, fmt.Errorf("bossContinue: %w", err)
	}
	return packets, nil
}
