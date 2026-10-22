package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/zone"
	zonecheckpoint "github.com/darkspinnet/darkspin/server/zone/checkpoint"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// Initial entrances belong to the zone's recipient projection queues, rather
// than to the response which happened to start the actor's timeline. The scheduler
// admission barrier must remain closed until publish returns.
type campaignNPCInitialIntroduction struct {
	runtime     campaignNPCActionRuntime
	zone        *zone.Zone
	identity    gameplayProducerIdentity
	plan        zonenpc.FirstActionPlan
	timestamp   uint64
	isPublished bool
}

func (e campaignNPCActionRuntime) publishInitialIntroductions(publications []*campaignNPCInitialIntroduction) error {
	if len(publications) == 0 {
		return nil
	}
	registry := e.registry
	registry.mutex.Lock()
	for index, publication := range publications {
		err := publication.validateLocked()
		if err != nil {
			registry.mutex.Unlock()
			return fmt.Errorf("introInitialValidate[%d]: %w", index, err)
		}
	}
	for _, publication := range publications {
		publication.publishLocked()
	}
	registry.mutex.Unlock()
	publications[0].zone.SaveCheckpoint(zonecheckpoint.ReasonIntroductionProgress)
	return nil
}

func (e *campaignNPCInitialIntroduction) validateLocked() error {
	registry := e.runtime.registry
	if e.isPublished {
		return nil
	}
	member, isFound := registry.sessions[e.identity.sessionKey]
	if !isFound || !e.identity.isFound || registry.isClosed || member.zone != e.zone ||
		gameplaySessionMemberKey(member) != e.identity.memberKey ||
		member.generation != e.identity.zoneGeneration ||
		member.transportGeneration != e.identity.transportGeneration || member.isZoneTerminal() {
		return errors.New("initial introduction driver unavailable")
	}
	npc, isNPCFound := e.zone.NPCs().NPC(e.plan.ObjectID)
	owner := zonenpc.ActionOwner{UserID: member.binding.UserID, PeerGeneration: member.generation}
	if !isNPCFound || npc.IsDefeated || npc.HitPoint <= 0 || !npc.IsActionStarted ||
		npc.ActionOwner != owner || npc.ActionGeneration != e.plan.ActionGeneration || !npc.IsIntroductionPending() {
		return errors.New("initial introduction actor unavailable")
	}
	if member.isRejoinPending || !e.plan.Profile.IsFirstAggroDurationKnown {
		return errors.New("initial introduction recipient unavailable")
	}
	return nil
}

func (e *campaignNPCInitialIntroduction) publishLocked() {
	if e.isPublished {
		return
	}
	// Include the initiating subscriber. Semantic projection retains spawn
	// ordering and lets the adapter project the current remaining hold when
	// each recipient drains, instead of retaining an encoded full duration.
	e.zone.PublishNPCActionAll(zonenpc.ActionEvent{
		Kind: zonenpc.ActionEventAggro, Plan: e.plan, Timestamp: e.timestamp,
	})
	e.zone.NPCs().CommitIntroduction(e.plan.ObjectID, e.plan.ActionGeneration)
	e.isPublished = true
}
