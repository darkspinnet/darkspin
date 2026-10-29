package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type corruptorDirectSetup struct {
	plans              []campaignCorruptorSpawnPlan
	actionPlans        []zonenpc.SpawnPlan
	packets            [][]byte
	isPrepared         bool
	isBirthScheduled   bool
	areTimersScheduled bool
}

// Chunk728 starts the second entrance on an existing actor. Chunk991's corpse,
// replacement birth and blink animation belong only to the stage-one creator.
func (e campaignNPCActionRuntime) startDirectCorruptor(packet raknet.Packet, sessionKey string, generation uint64, plan zonenpc.SpawnPlan, timestamp uint64) error {
	if e.now == nil || e.timer == nil || e.modifierPool == nil {
		return errors.New("direct corruptor authority unavailable")
	}
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.generation != generation || member.zone == nil || member.isZoneTerminal() {
		e.registry.mutex.Unlock()
		return errors.New("direct corruptor carrier unavailable")
	}
	for _, candidate := range e.registry.sessions {
		if candidate.zone == member.zone {
			existingState := candidate.campaignCorruptorStates[plan.ObjectID]
			if existingState.isStageTwo {
				e.registry.mutex.Unlock()
				return nil
			}
		}
	}
	startedAt := e.now()
	boss, err := member.zone.NPCs().BeginDirectCorruptor(plan.ObjectID, startedAt)
	if err != nil {
		e.registry.mutex.Unlock()
		return fmt.Errorf("directReserve: %w", err)
	}
	state := newCampaignCorruptorState(member.zone.NPCRandom().Float64)
	state.phaseGeneration = 1
	state.isStageTwo = true
	state.isDirectEntry = true
	state.isPhasePending = true
	run := &corruptorCinematicRun{zone: member.zone, objectID: plan.ObjectID,
		sourceGeneration: boss.SecondAggroGeneration(), entryAt: startedAt, deathEndsAt: startedAt,
		until: startedAt.Add(zonenpc.SecondAggroCinematicDuration), entryBoss: boss,
		focus: raknet.Vector3(zonenpc.SecondAggroCinematicFocus(boss.Plan.Position)), isDirectEntry: true}
	state.secondAggro = run
	run.state = state
	if member.campaignCorruptorStates == nil {
		member.campaignCorruptorStates = make(map[uint32]campaignCorruptorState)
	}
	member.campaignCorruptorStates[plan.ObjectID] = state
	e.registry.sessions[sessionKey] = member
	e.registry.mutex.Unlock()
	err = e.armCorruptorSecondAggro(run, timestamp, startedAt)
	if err != nil {
		return fmt.Errorf("directArm: %w", err)
	}
	// This queues the entire scene through each member's reliable FIFO. Its
	// source creation response and observers' semantic births precede the scene.
	e.publishCorruptorSecondAggro(run)
	return nil
}

func (e *corruptorCinematicRun) prepareDirectEntryPackets() ([][]byte, error) {
	position := e.entryBoss.Plan.Position
	packet, err := raknet.MarshalApplication(raknet.ObjectUpdateMessage{
		ObjectID: e.objectID, PositionX: position.X, PositionY: position.Y,
		PositionZ: position.Z, IsVisible: true,
	})
	if err != nil {
		return nil, fmt.Errorf("directReveal: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e *corruptorCinematicRun) directStateLocked() campaignCorruptorState {
	for _, member := range e.runtime.registry.sessions {
		if member.zone == e.zone {
			state, isFound := member.campaignCorruptorStates[e.objectID]
			if isFound && state.phaseGeneration == e.state.phaseGeneration && state.secondAggro == e {
				return state
			}
		}
	}
	for _, retained := range e.runtime.registry.retainedSessions {
		if retained.peerSession.zone == e.zone {
			state, isFound := retained.peerSession.campaignCorruptorStates[e.objectID]
			if isFound && state.phaseGeneration == e.state.phaseGeneration && state.secondAggro == e {
				return state
			}
		}
	}
	return e.state
}

// The camera ends at6.7s; the passive independently sets up portals at10s and
// switches phase after another ranked9/7/5s, using its authored0.5s poll.
// Called with registry.mutex held. A deferred return releases that lock.
func (e *corruptorCinematicRun) deferDirectPassiveLocked(sessionKey string, member gameplayPeerSession, boss zonenpc.Snapshot) bool {
	now := e.runtime.now()
	setupAt := e.entryAt.Add(campaignCorruptorActivationDelay)
	phaseAt := setupAt.Add(corruptorPhasePeriod(corruptorRank(boss.Plan.NounName)) + 500*time.Millisecond)
	if e.isDirectSetupComplete && !now.Before(phaseAt) {
		return false
	}
	e.state = member.campaignCorruptorStates[e.objectID]
	shouldStartIdle := !e.isDirectIdleStarted
	shouldPrepare := !now.Before(setupAt) && !e.directSetup.isPrepared
	var prepareErr error
	if shouldPrepare {
		prepareErr = e.prepareDirectPortalsLocked(sessionKey, &member, boss)
	}
	e.isResuming = true
	e.runtime.registry.mutex.Unlock()
	if shouldStartIdle {
		packets, err := e.runtime.restartCorruptorAction(member.schedulePacket.Autonomous(), sessionKey,
			member.generation, boss, e.sourceTimestamp(now), e.sourceGeneration, e.zone)
		e.runtime.registry.mutex.Lock()
		e.queueLocked(packets)
		actor, isActorFound := e.zone.NPCs().NPC(e.objectID)
		e.isDirectIdleStarted = err == nil && isActorFound && actor.IsActionStarted &&
			actor.ActionOwner == (zonenpc.ActionOwner{UserID: member.binding.UserID, PeerGeneration: member.generation})
		e.runtime.registry.mutex.Unlock()
		if err != nil && e.runtime.logger != nil {
			e.runtime.logger.Printf("Direct Corruptor initial pose failed: %v", err)
		}
	}
	setupErr := prepareErr
	if setupErr == nil && e.directSetup.isPrepared && !e.isDirectSetupComplete {
		setupErr = e.admitDirectPortals(sessionKey, member)
	}
	e.runtime.registry.mutex.Lock()
	e.isResuming = false
	if e.isComplete || !e.zone.IsActive() {
		e.runtime.registry.mutex.Unlock()
		return true
	}
	nextAt := setupAt
	if e.isDirectSetupComplete {
		nextAt = phaseAt
	}
	if !e.isDirectIdleStarted {
		retryAt := e.runtime.now().Add(time.Second)
		if retryAt.Before(nextAt) {
			nextAt = retryAt
		}
	}
	if setupErr != nil {
		nextAt = e.runtime.now().Add(time.Second)
		if e.runtime.logger != nil {
			e.runtime.logger.Printf("Direct Corruptor passive setup pending: %v", setupErr)
		}
	}
	e.runtime.registry.mutex.Unlock()
	err := e.zone.Timeline().Schedule(e.key(), max(time.Duration(0), nextAt.Sub(e.runtime.now())), e.finish, e.runtime.timer.Schedule)
	if err != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("Direct Corruptor passive deadline failed: %v", err)
	}
	return true
}

func (e *corruptorCinematicRun) prepareDirectPortalsLocked(sessionKey string, member *gameplayPeerSession, boss zonenpc.Snapshot) error {
	state := e.state
	state.portalPositions = e.zone.DirectorDefinition().SelectedBossPortalPositions()
	state.arePortalPositionsKnown = true
	plans := make([]campaignCorruptorSpawnPlan, 0)
	if len(state.portalPositions) != 0 {
		var err error
		plans, state, err = e.runtime.planCorruptorPhasePopulation(member, boss, state, zonenpc.CorruptorPhaseUnknown)
		if err != nil {
			return fmt.Errorf("directPortalPlan: %w", err)
		}
	}
	packets, actions, err := marshalCorruptorSpawns(plans, corruptorPopulationTarget(*member, boss))
	if err != nil {
		return fmt.Errorf("directPortalMarshal: %w", err)
	}
	taunt, err := raknet.MarshalApplication(raknet.ObjectEffectMessage{
		Asset: util.HashID("scaldronTaunt4.ServerEventDef"), ObjectID: e.objectID,
	})
	if err != nil {
		return fmt.Errorf("directTaunt: %w", err)
	}
	err = admitCorruptorPopulation(member, plans, corruptorPopulationTarget(*member, boss))
	if err != nil {
		return fmt.Errorf("directPortalAdd: %w", err)
	}
	e.directSetup = corruptorDirectSetup{plans: plans, actionPlans: actions, packets: append(packets, taunt), isPrepared: true}
	e.state = state
	member.campaignCorruptorStates[e.objectID] = state
	e.runtime.registry.sessions[sessionKey] = *member
	return nil
}

func (e *corruptorCinematicRun) admitDirectPortals(sessionKey string, member gameplayPeerSession) error {
	if !e.directSetup.isBirthScheduled {
		if len(e.directSetup.plans) != 0 {
			err := e.runtime.publishCorruptorBirth(member.schedulePacket.Autonomous(), sessionKey, member.generation,
				e.directSetup.plans, e.directSetup.actionPlans, e.directSetup.packets, e.sourceTimestamp(e.entryAt.Add(campaignCorruptorActivationDelay)))
			if err != nil {
				return fmt.Errorf("directPortalBirth: %w", err)
			}
		} else {
			e.runtime.registry.mutex.Lock()
			e.queueLocked(e.directSetup.packets)
			e.runtime.registry.mutex.Unlock()
		}
		e.runtime.registry.mutex.Lock()
		e.directSetup.isBirthScheduled = true
		e.runtime.registry.mutex.Unlock()
	}
	if !e.directSetup.areTimersScheduled && len(e.directSetup.plans) != 0 {
		err := e.runtime.scheduleCorruptorPortals(member.schedulePacket.Autonomous(), sessionKey,
			member.generation, e.objectID, e.directSetup.plans, e.sourceTimestamp(e.runtime.now()))
		if err != nil {
			return fmt.Errorf("directPortalTimers: %w", err)
		}
		e.runtime.registry.mutex.Lock()
		e.directSetup.areTimersScheduled = true
		e.runtime.registry.mutex.Unlock()
	}
	e.runtime.registry.mutex.Lock()
	e.isDirectSetupComplete = true
	e.runtime.registry.mutex.Unlock()
	return nil
}
