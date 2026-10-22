package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneboss "github.com/darkspinnet/darkspin/server/zone/boss"
	bossraknet "github.com/darkspinnet/darkspin/server/zone/boss/raknet103"
	zonecheckpoint "github.com/darkspinnet/darkspin/server/zone/checkpoint"
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
	zone                  *zone.Zone
	runtime               campaignNPCActionRuntime
	sessionKey            string
	generation            uint64
	plan                  zonenpc.FirstActionPlan
	readyAt               time.Time
	isPublicationDeferred bool
}

// The admission barrier prevents observer fanout and zero-delay continuation
// from escaping a rejected schedule. Closing ready publishes isAdmitted.
type campaignNPCFirstActionAdmission struct {
	ready      chan struct{}
	isAdmitted bool
}

type campaignNPCAdmittedProducer struct {
	admission     *campaignNPCFirstActionAdmission
	producePacket func() ([][]byte, error)
}

func (e campaignNPCAdmittedProducer) produce() ([][]byte, error) {
	<-e.admission.ready
	if !e.admission.isAdmitted {
		return nil, nil
	}
	packets, err := e.producePacket()
	if err != nil {
		return nil, fmt.Errorf("introAdmitted: %w", err)
	}
	return packets, nil
}

type campaignBossIntroPublication struct {
	runtime            campaignNPCActionRuntime
	zone               *zone.Zone
	owner              zonenpc.ActionOwner
	plan               zonenpc.FirstActionPlan
	timelineGeneration uint64
	stage              zonenpc.IntroductionStage
	producePacket      func() ([][]byte, error)
	isProduced         bool
}

func (e *campaignBossIntroPublication) produce() ([][]byte, error) {
	packets, err := e.producePacket()
	if err != nil {
		return nil, fmt.Errorf("introContinue: %w", err)
	}
	e.isProduced = len(packets) > 0
	return packets, nil
}

func (e *campaignBossIntroPublication) commit() {
	if !e.isProduced {
		return
	}
	isCommitted := e.zone.NPCs().CommitPublishedIntroduction(e.plan.ObjectID, e.timelineGeneration, e.runtime.now(), e.stage)
	if !isCommitted {
		return
	}
	if e.stage == zonenpc.IntroductionBossActive {
		e.zone.PublishNPCAction(zonenpc.ActionEvent{Kind: zonenpc.ActionEventBossActive, Plan: e.plan}, e.owner.UserID, e.owner.PeerGeneration)
	}
	e.zone.SaveCheckpoint(zonecheckpoint.ReasonIntroductionProgress)
}

// Entrance completion is independent of whether its first combat action can
// find a target or produce bytes. This explicit native activation is admitted
// at the authored deadline, before the separate first-attack producer.
type campaignNPCIntroductionCompleteStep struct {
	runtime    campaignNPCActionRuntime
	zone       *zone.Zone
	sessionKey string
	generation uint64
	plan       zonenpc.FirstActionPlan
	readyAt    time.Time
}

func (e campaignNPCIntroductionCompleteStep) produce() ([][]byte, error) {
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
	attackerCount := uint32(0)
	if npc.TargetObjectID != 0 {
		attackerCount = 1
	}
	packet, err := raknet.MarshalApplication(raknet.AgentBlackboardUpdateMessage{
		ObjectID: npc.Plan.ObjectID, TargetID: npc.TargetObjectID,
		IsTargetable: true, IsInCombat: npc.TargetObjectID != 0, AttackerCount: attackerCount,
	})
	if err != nil {
		return nil, fmt.Errorf("introCompleteMarshal: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e campaignBossIntroStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	current, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || current.generation != e.generation || current.zone != e.zone || current.isZoneTerminal() || e.runtime.now().Before(e.readyAt) {
		return nil, nil
	}
	boss, isBossFound := current.zone.NPCs().NPC(e.plan.ObjectID)
	owner := zonenpc.ActionOwner{UserID: current.binding.UserID, PeerGeneration: e.generation}
	if !isBossFound || boss.IsDefeated || boss.HitPoint <= 0 ||
		!boss.IsActionStarted || boss.ActionOwner != owner || boss.ActionGeneration != e.plan.ActionGeneration {
		return nil, nil
	}
	packet, err := bossraknet.Active(e.plan.ObjectID, true)
	if err != nil {
		return nil, fmt.Errorf("bossIntroActive: %w", err)
	}
	if e.isPublicationDeferred {
		return [][]byte{packet}, nil
	}
	current.zone.PublishNPCAction(zonenpc.ActionEvent{Kind: zonenpc.ActionEventBossActive, Plan: e.plan}, current.binding.UserID, current.generation)
	if e.runtime.logger != nil {
		e.runtime.logger.Printf(
			"Campaign boss admission stage=active level=%q marker_set=%q leader=%d source=intro",
			current.binding.Level, boss.Plan.MarkerSetName, e.plan.ObjectID,
		)
	}
	return [][]byte{packet}, nil
}
