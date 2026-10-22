package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

// Native modifier replacement prepares a new passive whose clock is armed
// only after the scheduled owner and co-op presentation has committed.
func (e gameplaySwitchRuntime) restartSagePassiveScheduled(
	packet raknet.Packet, sessionKey string, generation uint64, rank uint32,
) ([][]byte, *sagePassivePoll, error) {
	packets, poll, err := e.replaceSagePassive(packet, sessionKey, generation, rank, true, time.Time{})
	if err != nil {
		return packets, poll, fmt.Errorf("sageScheduled: %w", err)
	}
	return packets, poll, nil
}

func (e gameplaySwitchRuntime) replaceSagePassive(
	packet raknet.Packet, sessionKey string, generation uint64, rank uint32,
	isScheduledCommit bool, startedAt time.Time,
) ([][]byte, *sagePassivePoll, error) {
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.generation != generation || member.deployedHitPoint() <= 0 || member.zone == nil ||
		member.deployedCreatureIndex >= uint32(len(member.binding.Creatures)) ||
		member.binding.Creatures[member.deployedCreatureIndex].PassiveAbility != util.HashID("SupportHealerPassiveModifier") {
		e.registry.mutex.Unlock()
		return nil, nil, nil
	}
	definition := e.program.SupportHealerPassive.AtRank(rank)
	firstObjectID, err := member.reserveCampaignProjectileIDs(definition.MaximumCompanion, 2000)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, nil, fmt.Errorf("sageRankIDs: %w", err)
	}
	objectIDs := make([]uint32, definition.MaximumCompanion)
	for index := range objectIDs {
		objectIDs[index] = firstObjectID + uint32(index)
	}
	run, err := newSummonPassiveRun(summonPassiveRunInput{
		Definition: definition, OwnerObjectID: member.deployedObjectID,
		CompanionObjectIDs: objectIDs,
	})
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, nil, fmt.Errorf("sageRankStart: %w", err)
	}
	packets, err := member.stopCampaignSagePassive()
	if err != nil {
		stopRequests, stopErr := run.Stop()
		if stopErr != nil {
			e.logger.Printf("RakNet Sage replacement rollback failed remote=%s requests=%d: %v", sessionKey, len(stopRequests), stopErr)
		}
		e.registry.sessions[sessionKey] = member
		e.registry.mutex.Unlock()
		return nil, nil, fmt.Errorf("sageRankStop: %w", err)
	}
	member.sagePassive = run
	e.registry.sessions[sessionKey] = member
	e.registry.mutex.Unlock()
	schedule := gameplaySwitchPassiveSchedule{
		runtime: e, sessionKey: sessionKey, sessionGeneration: generation,
		targetObjectID: member.deployedObjectID, run: run, isCampaign: true,
		isScheduledCommit: isScheduledCommit, startedAt: startedAt,
	}
	poll, err := schedule.prepareWorldPoll(packet)
	if err != nil {
		e.recoverPassiveSchedule(sessionKey, generation, member.deployedObjectID, run, err)
		return packets, poll, fmt.Errorf("sageRankSchedule: %w", err)
	}
	return packets, poll, nil
}

func (e gameplaySetupRuntime) startSagePassive(packet raknet.Packet, sessionKey string, generation uint64, startedAt time.Time) ([][]byte, error) {
	e.registry.mutex.RLock()
	member, isFound := e.registry.sessions[sessionKey]
	isNeeded := isFound && member.generation == generation && member.sagePassive == nil
	e.registry.mutex.RUnlock()
	if !isNeeded {
		return nil, nil
	}
	rank := uint32(1)
	if member.isOverdriveActiveAt(e.now()) {
		rank = 3
	}
	runtime := gameplaySwitchRuntime{registry: e.registry, program: e.program,
		npc: e.damage.npc, damage: e.damage, now: e.now, logger: e.logger}
	packets, poll, err := runtime.replaceSagePassive(packet, sessionKey, generation, rank, false, startedAt)
	if err != nil {
		return packets, fmt.Errorf("sageInitial: %w", err)
	}
	if poll == nil {
		return packets, nil
	}
	return packets, nil
}
