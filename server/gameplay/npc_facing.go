package gameplay

import (
	"fmt"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

func (e campaignNPCActionRuntime) startNPCAttack(
	sessionKey string, generation uint64, plan zonenpc.AttackPlan, timestamp uint64,
) ([][]byte, error) {
	e.registry.mutex.RLock()
	defer e.registry.mutex.RUnlock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation || peerSession.zone == nil ||
		peerSession.isZoneTerminal() {
		return nil, nil
	}
	if !peerSession.isCampaignNPCSourceGenerationActive(
		generation, plan.SourceObjectID, plan.ActionGeneration,
	) {
		return nil, nil
	}
	packets, err := marshalNPCAttack(peerSession.zone.NPCs(), plan, timestamp)
	if err != nil {
		return nil, fmt.Errorf("npcAttackFacing: %w", err)
	}
	return packets, nil
}

// Callers already holding the peer lock use this same operation directly.
func marshalNPCAttack(
	npcSession *zonenpc.Session, plan zonenpc.AttackPlan, timestamp uint64,
) ([][]byte, error) {
	if npcSession == nil {
		return nil, nil
	}
	snapshot, isFound := npcSession.NPC(plan.SourceObjectID)
	if !isFound || snapshot.IsDefeated || !snapshot.IsPublished ||
		!snapshot.IsActionStarted || snapshot.ActionGeneration != plan.ActionGeneration {
		return nil, nil
	}
	var packets [][]byte
	var err error
	if plan.Profile.AbilityName == "StealthAttack" {
		packets, err = npcraknet.StealthArrival(plan, timestamp)
	} else {
		packets, err = npcraknet.AttackStart(plan, timestamp)
	}
	if err != nil {
		return nil, fmt.Errorf("npcAttackMarshal: %w", err)
	}
	if plan.Profile.AbilityName != "StealthAttack" {
		// A stop/turn changes the client goal, not necessarily its physics root.
		// Repair arrival drift before the cast without overriding facing policy.
		posePacket, poseErr := npcraknet.RestorePose(
			plan.SourceObjectID, plan.SourcePosition, snapshot.Facing,
		)
		if poseErr != nil {
			return nil, fmt.Errorf("npcAttackPose: %w", poseErr)
		}
		packets = append([][]byte{posePacket}, packets...)
	}
	if !npcSession.CommitFacing(plan) {
		return nil, nil
	}
	if !npcSession.CommitCorruptorAction(plan, timestamp) {
		return nil, nil
	}
	return packets, nil
}
