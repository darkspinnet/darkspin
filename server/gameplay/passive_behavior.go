package gameplay

import (
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// pollPassiveBehaviors admits authored, targetless behavior through the same
// actor-owned lease as combat idle. It never reserves an attack or a target.
func (r gameplayPendingRuntime) pollPassiveBehaviors(packet raknet.Packet) ([][]byte, error) {
	sessionKey := packet.Address.String()
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isEligible := isFound && peerSession.zone != nil && peerSession.zone.NPCs() != nil &&
		peerSession.stage.IsDungeon() && peerSession.dungeonSetup.IsCommitted() &&
		!peerSession.isRejoinPending && !peerSession.isZoneTerminal()
	r.registry.mutex.RUnlock()
	if !isEligible {
		return nil, nil
	}
	npcSession := peerSession.zone.NPCs()
	now := r.now()
	var packets [][]byte
	for _, presence := range npcSession.LiveActorSnapshots() {
		if presence.TargetObjectID != 0 || !presence.IsPublished || presence.IsFixture || presence.HitPoint <= 0 {
			continue
		}
		actor, isActorFound := npcSession.NPC(presence.ObjectID)
		if !isActorFound || !isPassiveBehaviorActorEligible(actor) {
			continue
		}
		// Dark Spin phase policy: pre-aggro until an actor has begun its first
		// action, passive after losing its combat target. The native phase
		// transition order is not established by the authored field names.
		choice, isSelected := npcSession.SelectPassiveIdle(actor.Plan.ObjectID, actor.IsFirstActionStarted, now)
		if !isSelected && !actor.IsFirstActionStarted {
			// An empty pre-aggro field may still have an explicit passive
			// behavior. Empty graphs do not acquire a synthetic wander.
			choice, isSelected = npcSession.SelectPassiveIdle(actor.Plan.ObjectID, true, now)
		}
		if !isSelected {
			continue
		}
		responses, err := r.damage.npc.startPassiveCombatIdle(
			packet, sessionKey, peerSession.generation, actor, choice, packet.SourceTime,
		)
		if err != nil {
			return nil, fmt.Errorf("passiveStart: %w", err)
		}
		packets = append(packets, responses...)
	}
	return packets, nil
}

func isPassiveBehaviorActorEligible(actor zonenpc.Snapshot) bool {
	if actor.IsDefeated || actor.HitPoint <= 0 || !actor.IsPublished || actor.IsBirthPending ||
		actor.TargetObjectID != 0 || actor.IsActionStarted || actor.Plan.IsFixture ||
		actor.Plan.OwnerObjectID != 0 || actor.Plan.NPCProfile.IsPlayerPet ||
		!actor.IsScenarioAIEnabled() || actor.IsScenarioControlled() ||
		actor.Plan.IsIntroductionHidden || actor.IsIntroductionPending() ||
		zonenpc.IsAgentHealingChild(actor.Plan) {
		return false
	}
	graph := actor.Plan.NPCProfile.AIGraph
	if graph == nil || !graph.IsResolved || isMutationBehaviorGraph(graph) {
		return false
	}
	profile, isProfileFound := zonenpc.ActionProfileForPlan(actor.Plan)
	if isProfileFound && profile.AbilityName == "ScaldronBasicCopter_Passive" {
		// The copter's independent passive already owns its pre-aggro wander.
		return false
	}
	return true
}

// Mutation agents already have a dedicated lifecycle and movement controller.
// Identify the authored behavior family rather than enumerating noun aliases.
func isMutationBehaviorGraph(graph *game.CampaignAIGraph) bool {
	if graph.PassiveAbility != nil && strings.HasPrefix(*graph.PassiveAbility, "MutationAgent") {
		return true
	}
	return strings.Contains(graph.PreAggroIdle, "MutationAgent") ||
		strings.Contains(graph.PreAggroIdle2, "MutationAgent") ||
		strings.Contains(graph.PassiveIdle, "MutationAgent")
}
