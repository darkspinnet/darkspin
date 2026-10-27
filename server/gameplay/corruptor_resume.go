package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// Semantic phase/population preparation happens once. A rejected transport
// continuation retries only its unfinished scheduling stages.
type corruptorPhaseContinuation struct {
	state              campaignCorruptorState
	boss               zonenpc.Snapshot
	plans              []campaignCorruptorSpawnPlan
	actionPlans        []zonenpc.SpawnPlan
	packets            [][]byte
	isPrepared         bool
	portalCancels      map[uint32]raknet.CancelSchedule
	isActionRestarted  bool
	areActorsScheduled bool
	isPeriodScheduled  bool
	isComplete         bool
	isStageTwo         bool
}

func (e *corruptorPhaseContinuation) cancelPortals() {
	for _, cancel := range e.portalCancels {
		cancel()
	}
	e.portalCancels = nil
}

func (e *corruptorPhaseContinuation) drainPackets() [][]byte {
	packets := e.packets
	e.packets = nil
	return packets
}

func (e *corruptorPhaseContinuation) admit(step campaignCorruptorPhaseStep) ([][]byte, error) {
	runtime := step.runtime
	err := e.validate(step)
	if err != nil {
		return e.drainPackets(), fmt.Errorf("phaseCarrier: %w", err)
	}
	if e.portalCancels == nil {
		e.portalCancels = make(map[uint32]raknet.CancelSchedule)
	}
	var portalPlans []campaignCorruptorSpawnPlan
	for _, plan := range e.plans {
		if plan.Plan.NounName != campaignCorruptorPortalNounName || e.portalCancels[plan.Plan.ObjectID] != nil {
			continue
		}
		portalPlans = append(portalPlans, plan)
	}
	if len(portalPlans) > 0 {
		err = e.validate(step)
		if err != nil {
			return e.drainPackets(), fmt.Errorf("portalCarrier: %w", err)
		}
		cancel, err := runtime.scheduleCorruptorPortalGroup(step.packet, step.sessionKey, step.generation,
			step.objectID, portalPlans, step.timestamp)
		if err != nil {
			return e.drainPackets(), fmt.Errorf("phasePortalTimer: %w", err)
		}
		for _, plan := range portalPlans {
			e.portalCancels[plan.Plan.ObjectID] = cancel
		}
	}
	if !e.isActionRestarted {
		err = e.validate(step)
		if err != nil {
			return e.drainPackets(), fmt.Errorf("actionCarrier: %w", err)
		}
		runtime.registry.mutex.RLock()
		member, isMemberFound := runtime.registry.sessions[step.sessionKey]
		if !isMemberFound || member.generation != step.generation || member.zone == nil {
			runtime.registry.mutex.RUnlock()
			return e.drainPackets(), errors.New("corruptor resume carrier unavailable")
		}
		boss, isBossFound := member.zone.NPCs().NPC(step.objectID)
		runtime.registry.mutex.RUnlock()
		if !isBossFound || boss.IsDefeated {
			return e.drainPackets(), errors.New("corruptor resume actor unavailable")
		}
		e.boss = boss
		packets, err := runtime.restartCorruptorAction(step.packet, step.sessionKey, step.generation,
			e.boss, step.timestamp, step.sceneGeneration, step.world)
		e.packets = append(e.packets, packets...)
		if err != nil {
			return e.drainPackets(), fmt.Errorf("phaseResume: %w", err)
		}
		e.isActionRestarted = true
	}
	if !e.areActorsScheduled {
		err = e.validate(step)
		if err != nil {
			return e.drainPackets(), fmt.Errorf("actorCarrier: %w", err)
		}
		packets, err := runtime.scheduleFirstActions(step.packet, step.sessionKey, step.generation,
			e.actionPlans, step.timestamp)
		e.packets = append(e.packets, packets...)
		if err != nil {
			return e.drainPackets(), fmt.Errorf("phaseActors: %w", err)
		}
		e.areActorsScheduled = true
	}
	if e.isStageTwo && !e.isPeriodScheduled {
		err = e.validate(step)
		if err != nil {
			return e.drainPackets(), fmt.Errorf("periodCarrier: %w", err)
		}
		period := corruptorPhasePeriod(corruptorRank(e.boss.Plan.NounName))
		next := step
		next.timestamp += uint64(period / time.Millisecond)
		next.isStageTwoStart = false
		next.continuation = nil
		err := scheduleNPCProducer(runtime.registry, step.packet, period, next.produce)
		if err != nil {
			return e.drainPackets(), fmt.Errorf("phaseSchedule: %w", err)
		}
		e.isPeriodScheduled = true
	}
	e.isComplete = true
	return e.drainPackets(), nil
}

func (e *corruptorPhaseContinuation) validate(step campaignCorruptorPhaseStep) error {
	step.runtime.registry.mutex.RLock()
	defer step.runtime.registry.mutex.RUnlock()
	member, isFound := step.runtime.registry.sessions[step.sessionKey]
	if !isFound || member.generation != step.generation || member.zone == nil || member.isZoneTerminal() || !member.zone.IsActive() ||
		(step.world != nil && member.zone != step.world) ||
		(step.identity.isFound && gameplayProducerIdentityFromSession(step.sessionKey, member, isFound) != step.identity) {
		return errors.New("corruptor phase carrier replaced")
	}
	boss, isBossFound := member.zone.NPCs().NPC(step.objectID)
	if !isBossFound || boss.IsDefeated ||
		(boss.IsCorruptorStageTwo && !boss.IsSecondAggroCinematic(step.sceneGeneration)) {
		return errors.New("corruptor phase source replaced")
	}
	return nil
}
