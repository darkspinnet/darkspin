package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

func (e campaignNPCActionRuntime) restartCorruptorAction(
	packet raknet.Packet, sessionKey string, generation uint64,
	boss zonenpc.Snapshot, timestamp uint64,
) ([][]byte, error) {
	// The old cast has been retired. Stop at the authoritative pose before
	// starting a new action generation on the same actor.
	posePacket, err := npcraknet.RestorePose(boss.Plan.ObjectID, boss.Plan.Position, boss.Facing)
	if err != nil {
		return nil, fmt.Errorf("phasePose: %w", err)
	}
	packets, err := npcraknet.MovementStop(boss.Plan.ObjectID, boss.Plan.Position)
	if err != nil {
		return nil, fmt.Errorf("phaseStop: %w", err)
	}
	packets = append([][]byte{posePacket}, packets...)
	e.registry.mutex.RLock()
	session, isFound := e.registry.sessions[sessionKey]
	if !isFound || session.generation != generation || session.zone == nil || session.isZoneTerminal() {
		e.registry.mutex.RUnlock()
		return nil, nil
	}
	current, isNPCFound := session.zone.NPCs().NPC(boss.Plan.ObjectID)
	if !isNPCFound || current.IsDefeated || current.ActionGeneration != boss.ActionGeneration {
		e.registry.mutex.RUnlock()
		return nil, nil
	}
	target, isTargetFound := session.campaignNPCTarget(generation, boss.TargetObjectID)
	if !isTargetFound {
		e.registry.mutex.RUnlock()
		return packets, nil
	}
	owner := zonenpc.ActionOwner{UserID: session.binding.UserID, PeerGeneration: generation}
	npc, isStarted, isFirstAction, err := session.zone.NPCs().StartAction(boss.Plan.ObjectID, owner, target.ObjectID)
	e.registry.mutex.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("phaseStart: %w", err)
	}
	if !isStarted {
		return packets, nil
	}
	if isFirstAction && e.logger != nil {
		e.logger.Printf("RakNet Corruptor activated with phase object=%d", boss.Plan.ObjectID)
	}
	next := campaignNPCFirstActionStep{
		runtime: e, packet: packet.Autonomous(), sessionKey: sessionKey,
		generation: generation, objectID: boss.Plan.ObjectID,
		actionGeneration: npc.ActionGeneration, timestamp: timestamp,
	}
	nextPackets, err := next.produce()
	if err != nil {
		return nil, fmt.Errorf("phaseAction: %w", err)
	}
	return append(packets, nextPackets...), nil
}

func (e campaignNPCActionRuntime) produceCorruptorPose(
	packet raknet.Packet, sessionKey string, generation uint64, objectID uint32, timestamp uint64,
) ([][]byte, bool, error) {
	e.registry.mutex.RLock()
	session, isFound := e.registry.sessions[sessionKey]
	if !isFound || !session.isCampaignNPCSourceActive(generation, objectID) {
		e.registry.mutex.RUnlock()
		return nil, false, nil
	}
	npc, isNPCFound := session.zone.NPCs().NPC(objectID)
	target, isTargetFound := session.campaignNPCTarget(generation, npc.TargetObjectID)
	e.registry.mutex.RUnlock()
	if !isNPCFound || !isTargetFound || npc.Plan.ActionProfile.AbilityName != "ScaldronBoss_Pose" {
		return nil, false, nil
	}
	profile := npc.Plan.ActionProfile
	plan := zonenpc.AttackPlan{
		SourceObjectID: objectID, TargetObjectID: target.ObjectID,
		ActionGeneration: npc.ActionGeneration, SourcePosition: npc.Plan.Position,
		TargetPosition: target.Position, Profile: profile,
	}
	packets, err := e.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		return nil, true, fmt.Errorf("poseStart: %w", err)
	}
	next := campaignNPCFirstActionStep{
		runtime: e, packet: packet, sessionKey: sessionKey, generation: generation,
		objectID: objectID, actionGeneration: npc.ActionGeneration,
		timestamp: timestamp + uint64(profile.ReleaseDelay/time.Millisecond),
	}
	cancel, err := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: profile.ReleaseDelay, Produce: next.produce,
	}})
	if err == nil && cancel == nil {
		err = errors.New("pose cancellation unavailable")
	}
	if err != nil {
		e.releaseActionGeneration(sessionKey, generation, objectID, npc.ActionGeneration)
		return nil, true, fmt.Errorf("poseSchedule: %w", err)
	}
	return packets, true, nil
}
