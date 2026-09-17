package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/zone"
	zoneboss "github.com/darkspinnet/darkspin/server/zone/boss"
	bossraknet "github.com/darkspinnet/darkspin/server/zone/boss/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func isCampaignBossIntroDelayed(plan zonenpc.SpawnPlan) bool {
	if plan.OwnerObjectID != 0 || !zoneboss.IsFinalBossNoun(plan.NounName) || plan.Introduction == zonenpc.SpawnIntroductionFloorWarp {
		return false
	}
	profile, isFound := zonenpc.ActionProfileForPlan(plan)
	return isFound && profile.IsFirstAggroDurationKnown && profile.FirstAggroDelay > 0
}

type campaignBossIntroStep struct {
	zone       *zone.Zone
	runtime    campaignNPCActionRuntime
	sessionKey string
	generation uint64
	plan       zonenpc.FirstActionPlan
	readyAt    time.Time
}

func (e campaignBossIntroStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	current, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || current.generation != e.generation || current.zone != e.zone || current.isZoneTerminal() || e.runtime.now().Before(e.readyAt) {
		return nil, nil
	}
	boss, isBossFound := current.zone.NPCs().NPC(e.plan.ObjectID)
	if !isBossFound || boss.IsDefeated || boss.HitPoint <= 0 {
		return nil, nil
	}
	packet, err := bossraknet.Active(e.plan.ObjectID, true)
	if err != nil {
		return nil, fmt.Errorf("bossIntroActive: %w", err)
	}
	current.zone.PublishNPCAction(zonenpc.ActionEvent{Kind: zonenpc.ActionEventBossActive, Plan: e.plan}, current.binding.UserID, current.generation)
	return [][]byte{packet}, nil
}
