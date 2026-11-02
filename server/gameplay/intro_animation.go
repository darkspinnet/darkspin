package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/zone"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// SpawnEater waits while visible before starting its entrance animation. This
// stage has its own accepted progress, separate from delayed visibility.
type campaignNPCIntroductionAnimationStep struct {
	runtime    campaignNPCActionRuntime
	zone       *zone.Zone
	sessionKey string
	generation uint64
	plan       zonenpc.FirstActionPlan
	readyAt    time.Time
	timestamp  uint64
}

func (e campaignNPCIntroductionAnimationStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	defer e.runtime.registry.mutex.RUnlock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation || member.zone != e.zone ||
		member.isZoneTerminal() || e.runtime.now().Before(e.readyAt) {
		return nil, nil
	}
	npc, isNPCFound := e.zone.NPCs().NPC(e.plan.ObjectID)
	owner := zonenpc.ActionOwner{UserID: member.binding.UserID, PeerGeneration: member.generation}
	if !isNPCFound || npc.IsDefeated || npc.HitPoint <= 0 || !npc.IsActionStarted ||
		npc.ActionOwner != owner || npc.ActionGeneration != e.plan.ActionGeneration || !npc.IsIntroductionPending() {
		return nil, nil
	}
	packets, err := npcraknet.FirstAggroAnimation(e.plan, e.timestamp)
	if err != nil {
		return nil, fmt.Errorf("introAnimation: %w", err)
	}
	return packets, nil
}
