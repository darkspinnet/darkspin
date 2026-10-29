//go:build scenario

package gameplay

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/scenario"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e *gameplaySessionRegistry) scenarioOwnedParentLocked(ctx context.Context, member gameplayPeerSession, nounName string, owner scenario.ControlledNPCIdentity, requestID string) (zonenpc.SpawnPlan, error) {
	switch strings.ToLower(nounName) {
	case "nomadcyberone_droid_2.noun", "nomadcyberone_droid_3.noun":
	default:
		return zonenpc.SpawnPlan{}, errors.New("owned scenario child noun is unsupported")
	}
	if owner.UserID != int64(member.binding.UserID) || owner.GameID != member.binding.GameID ||
		owner.PeerGeneration != member.generation || owner.TransportGeneration != member.transportGeneration {
		return zonenpc.SpawnPlan{}, errors.New("owned child and parent must share the current client and realm")
	}
	peer, state, err := e.controlledNPCPeerLocked(ctx, owner)
	if err != nil {
		return zonenpc.SpawnPlan{}, fmt.Errorf("ownedParent: %w", err)
	}
	if peer.zone != member.zone || state.IsAIEnabled || state.IsDefeated || state.HitPoint <= 0 || state.IsFixture {
		return zonenpc.SpawnPlan{}, errors.New("owned parent must be a living AI-disabled controlled NPC")
	}
	parent, isFound := member.zone.NPCs().NPC(owner.ObjectID)
	if !isFound || campaignDifficultyNounFamily(parent.Plan.NounName) != "nomadcyberone" {
		return zonenpc.SpawnPlan{}, errors.New("owned droid requires its authored NomadCyberOne parent")
	}
	graph := parent.Plan.NPCProfile.AIGraph
	if !parent.Plan.NPCProfile.IsClassKnown || graph == nil || !graph.IsResolved ||
		graph.PassiveAbility == nil || *graph.PassiveAbility != "NomadCyberOne_Droid" || parent.Plan.OwnerObjectID != 0 {
		return zonenpc.SpawnPlan{}, errors.New("owned parent has no resolved authored droid passive")
	}
	if member.agentSupports[owner.ObjectID] != nil {
		return zonenpc.SpawnPlan{}, errors.New("owned parent support is already started; use a fresh parent")
	}
	for otherID, receipt := range e.spawnReceipts {
		if otherID != requestID && receipt.Owner != nil && *receipt.Owner == owner && receipt.State != "retired" && receipt.Failure == "" {
			return zonenpc.SpawnPlan{}, errors.New("owned parent already has a pending or admitted child")
		}
	}
	childID := e.spawnReceipts[requestID].ObjectID
	for _, child := range member.zone.NPCs().Snapshots() {
		if child.Plan.ObjectID != childID && child.Plan.OwnerObjectID == owner.ObjectID &&
			campaignDifficultyNounFamily(child.Plan.NounName) == "nomadcyberone_droid" && !child.IsDefeated {
			return zonenpc.SpawnPlan{}, errors.New("owned parent already has a living droid")
		}
	}
	return parent.Plan, nil
}

func (e *gameplaySessionRegistry) prepareScenarioOwnedPlanLocked(requestID string, member gameplayPeerSession, plan zonenpc.SpawnPlan) (zonenpc.SpawnPlan, bool, error) {
	receipt, isFound := e.spawnReceipts[requestID]
	if !isFound || receipt.Owner == nil {
		return plan, false, nil
	}
	ctx, cancel := context.WithTimeout(member.zone.Context(), 500*time.Millisecond)
	defer cancel()
	parent, err := e.scenarioOwnedParentLocked(ctx, member, plan.NounName, *receipt.Owner, requestID)
	if err != nil {
		return plan, false, fmt.Errorf("ownedPlanParent: %w", err)
	}
	graph := plan.NPCProfile.AIGraph
	if !plan.NPCProfile.IsKnown || !plan.NPCProfile.IsClassKnown || graph == nil || !graph.IsResolved || graph.DefinitionID != 748733188 {
		return plan, false, errors.New("owned droid requires its exact installed class and resolved AI")
	}
	plan = plan.Clone()
	plan.OwnerObjectID = parent.ObjectID
	// Build-103 chunk690 applies this creator override after owner/team.
	// Preserve the installed class identity while matching the born actor.
	plan.NPCProfile.IsTargetable = false
	plan.ActionProfile = zonenpc.AgentChildProfile(plan.NounName, plan.NPCProfile.NPCRank)
	plan.IsActionKnown = true
	return plan, true, nil
}

type scenarioOwnedCommit struct {
	runtime   gameplayPendingRuntime
	packet    raknet.Packet
	requestID string
}

func (e gameplayPendingRuntime) finishScenarioOwnedSpawn(packet raknet.Packet, requestID string) error {
	e.registry.mutex.Lock()
	receipt := e.registry.spawnReceipts[requestID]
	e.registry.mutex.Unlock()
	if receipt.Owner == nil {
		return nil
	}
	commit := scenarioOwnedCommit{runtime: e, packet: packet.Autonomous(), requestID: requestID}
	err := packet.AfterResponseCommit(commit.complete)
	if err != nil {
		return fmt.Errorf("ownedCommitRegister: %w", err)
	}
	return nil
}

func (e scenarioOwnedCommit) complete() {
	err := e.start()
	if err != nil {
		e.runtime.registry.recordScenarioSpawnFailure(e.requestID, err)
		if e.runtime.logger != nil {
			e.runtime.logger.Printf("RakNet owned scenario child startup failed request=%s: %v", e.requestID, err)
		}
	}
}

func (e scenarioOwnedCommit) start() error {
	registry := e.runtime.registry
	registry.mutex.Lock()
	receipt := registry.spawnReceipts[e.requestID]
	member, isFound := registry.sessions[e.packet.Address.String()]
	if receipt.Owner == nil || !receipt.IsAdmitted || !receipt.IsPublished || receipt.Failure != "" || !isFound || member.zone == nil {
		registry.mutex.Unlock()
		return errors.New("owned child publication is no longer current")
	}
	ctx, cancel := context.WithTimeout(member.zone.Context(), 500*time.Millisecond)
	defer cancel()
	parent, err := registry.scenarioOwnedParentLocked(ctx, member, receipt.NounName, *receipt.Owner, e.requestID)
	if err != nil {
		registry.mutex.Unlock()
		return fmt.Errorf("ownedStartParent: %w", err)
	}
	child, isChildFound := member.zone.NPCs().NPC(receipt.ObjectID)
	if !isChildFound || child.IsDefeated || child.Plan.OwnerObjectID != parent.ObjectID || child.Plan.NounName != receipt.NounName {
		registry.mutex.Unlock()
		return errors.New("owned child identity changed before support startup")
	}
	// The caster's response has committed. Transfer every observer's prior
	// semantic birth to its reliable FIFO before any child-sourced output.
	for sessionKey, peer := range registry.sessions {
		if peer.zone != member.zone || peer.isRejoinPending || !peer.isCampaignPresentationAvailable() {
			continue
		}
		err = peer.queueCampaignPresentation(nil)
		registry.sessions[sessionKey] = peer
		if err != nil {
			registry.mutex.Unlock()
			return fmt.Errorf("ownedBirthQueue: %w", err)
		}
	}
	registry.mutex.Unlock()
	packets, err := e.runtime.damage.npc.startAgentSupport(e.packet, e.packet.Address.String(), member.generation, []zonenpc.SpawnPlan{parent}, e.packet.SourceTime)
	registry.mutex.Lock()
	registry.queueChannelPresentationLocked(member.zone, packets)
	registry.mutex.Unlock()
	if err != nil {
		return fmt.Errorf("ownedSupport: %w", err)
	}
	registry.mutex.Lock()
	current, isCurrent := registry.sessions[e.packet.Address.String()]
	parentState, isParentFound := member.zone.NPCs().NPC(parent.ObjectID)
	childState, isChildCurrent := member.zone.NPCs().NPC(receipt.ObjectID)
	support := current.agentSupports[parent.ObjectID]
	if !isCurrent || current.generation != member.generation || current.zone != member.zone ||
		!isParentFound || parentState.IsDefeated || parentState.IsScenarioAIEnabled() ||
		!isChildCurrent || childState.IsDefeated || childState.Plan.OwnerObjectID != parent.ObjectID ||
		support == nil || len(support.childObjectIDs) != 1 || support.childObjectIDs[0] != receipt.ObjectID {
		registry.mutex.Unlock()
		return errors.New("owned support admission changed before completion")
	}
	receipt = registry.spawnReceipts[e.requestID]
	receipt.IsOwnerSupportStarted = true
	registry.spawnReceipts[e.requestID] = receipt
	registry.mutex.Unlock()
	return nil
}
