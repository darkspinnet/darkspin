package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	deathraknet "github.com/darkspinnet/darkspin/server/zone/death/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type corruptorDeathKill struct {
	owner      *corruptorDeathRun
	objectID   uint32
	death      *deathraknet.Run
	transition campaignDamageTransition
	sessionKey string
	generation uint64
	packet     raknet.Packet
	timestamp  uint64
}

func (e *corruptorDeathRun) killLocked(target zonenpc.Snapshot, now time.Time) (*corruptorDeathKill, error) {
	timestamp := e.timestamp + uint64(now.Sub(e.startedAt)/time.Millisecond)
	if target.Plan.ObjectID == e.job.ObjectID {
		packet, deleteErr := e.deleteJobLocked()
		if len(packet) > 0 {
			e.queueLocked([][]byte{packet})
		}
		if deleteErr != nil {
			return nil, fmt.Errorf("deathJobFinish: %w", deleteErr)
		}
		return nil, nil
	}
	definition, err := campaignNPCDeathDefinition(target, e.runtime.program.NPCDeathPhysics(target.Plan.NounName))
	if err != nil {
		return nil, fmt.Errorf("deathTargetDefinition: %w", err)
	}
	// KillObject is a lifecycle operation, not an authored attack roll. Reuse
	// the canonical death behavior with the actual HP removed, without player
	// kill credit, loot creation or an invented damage profile. The optional
	// native Creature=true presentation flag remains independently unresolved.
	run, packets, err := deathraknet.NewRun(campaignDeathTarget(definition), e.job.ObjectID,
		target.HitPoint, false, timestamp, e.runtime.effectPool)
	if err != nil {
		return nil, fmt.Errorf("deathTargetPresentation: %w", err)
	}
	result, err := e.world.NPCs().Defeat(e.job.ObjectID, target.Plan.ObjectID)
	if err != nil {
		run.Stop()
		return nil, fmt.Errorf("deathTargetDefeat: %w", err)
	}
	if !result.IsDefeated {
		run.Stop()
		return nil, nil
	}
	kill := &corruptorDeathKill{owner: e, objectID: target.Plan.ObjectID,
		death: run, timestamp: timestamp}
	var transitionErr error
	for sessionKey, member := range e.runtime.registry.sessions {
		if member.zone != e.world || member.isZoneTerminal() || member.schedulePacket.ScheduleGroupResult == nil {
			continue
		}
		kill.transition, transitionErr = member.applyCampaignNPCSelfDamageTransition(result)
		kill.sessionKey, kill.generation, kill.packet = sessionKey, member.generation, member.schedulePacket.Autonomous()
		e.runtime.registry.sessions[sessionKey] = member
		break
	}
	if kill.sessionKey == "" {
		// Shared lifecycle and corpse deadlines still advance with no live
		// carrier; ordinary quest bookkeeping does not require a transport.
		encounter, observeErr := e.world.ObserveNPCDamage(result)
		if observeErr != nil {
			transitionErr = fmt.Errorf("deathEncounter: %w", observeErr)
		}
		if encounter.Boss.IsComplete || encounter.Boss.NextWaveActorCount != 0 ||
			encounter.Horde.IsComplete || encounter.Horde.NextWaveActorCount != 0 {
			e.runtime.logger.Printf("Corruptor death encounter callback has no live carrier object=%d bossComplete=%t bossNext=%d hordeComplete=%t hordeNext=%d",
				target.Plan.ObjectID, encounter.Boss.IsComplete, encounter.Boss.NextWaveActorCount,
				encounter.Horde.IsComplete, encounter.Horde.NextWaveActorCount)
		}
	}
	recordErr := e.world.RecordNPCDeathObjectives(e.world.Context(), run.DrainProjection())
	e.queueLocked(packets)
	if transitionErr != nil {
		e.runtime.logger.Printf("Corruptor death optional callback preparation object=%d: %v", target.Plan.ObjectID, transitionErr)
		kill.sessionKey = ""
	}
	if recordErr != nil {
		return kill, fmt.Errorf("deathTargetRecord: %w", recordErr)
	}
	return kill, nil
}

func (e *corruptorDeathKill) publish() {
	runtime := e.owner.runtime
	err := runtime.death.schedule(e.owner.world, e.objectID, e.death)
	if err != nil {
		runtime.registry.mutex.Lock()
		deletePacket, marshalErr := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{e.objectID}})
		recordErr := e.owner.world.RecordNPCDeathObjectives(e.owner.world.Context(), []zonenpc.DeathEvent{{
			Kind: zonenpc.DeathDelete, TargetObjectID: e.objectID}})
		if marshalErr == nil {
			e.owner.queueLocked([][]byte{deletePacket})
		}
		retireErr := e.owner.retireLocked()
		runtime.registry.mutex.Unlock()
		runtime.logger.Printf("Corruptor death corpse object=%d: %v; cleanup=%v", e.objectID, err,
			errors.Join(marshalErr, recordErr, retireErr))
	}
	if e.sessionKey == "" {
		return
	}
	damage := campaignDamageRuntime{registry: runtime.registry, npc: runtime,
		projection: runtime.projection, effectPool: runtime.effectPool,
		gameplayJoin: runtime.gameplayJoin, logger: runtime.logger}
	packets, err := damage.publishTransition(e.packet, e.sessionKey, e.generation, e.transition, e.timestamp)
	if err != nil {
		runtime.logger.Printf("Corruptor scripted death callback object=%d: %v", e.objectID, err)
	}
	runtime.registry.mutex.Lock()
	e.owner.queueLocked(packets)
	runtime.registry.mutex.Unlock()
}
