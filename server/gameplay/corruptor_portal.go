package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
)

const (
	corruptorPortalStartupDelay   = 2100 * time.Millisecond
	corruptorPortalSpawnDelay     = 8 * time.Second
	corruptorPortalMinionAttempts = 2
	// Chunk 347 sets this to zero. Positive boss special caps do not override it.
	corruptorPortalLieutenantAttempts = 0
)

// The modifier's mySpawns and localSpawns belong to one portal, not its boss.
// Both roles would share this count; SetNpcList changes caps, not attempts.
type campaignCorruptorPortalState struct {
	isNPCListPresent bool
	spawnObjectIDs   []uint32
	minionEntries    []game.CampaignDirectorEntry
	specialEntries   []game.CampaignDirectorEntry
	minionCap        int
	specialCap       int
	configName       string
}

func newCampaignCorruptorPortalState() campaignCorruptorPortalState {
	return campaignCorruptorPortalState{isNPCListPresent: true, minionCap: 4, configName: "TNX-173.LevelConfig"}
}

func (e *campaignCorruptorPortalState) setNpcList(
	minionEntries, specialEntries []game.CampaignDirectorEntry, minionCap, specialCap int,
) {
	e.isNPCListPresent = true
	e.minionEntries = append([]game.CampaignDirectorEntry(nil), minionEntries...)
	e.specialEntries = append([]game.CampaignDirectorEntry(nil), specialEntries...)
	e.minionCap = minionCap
	e.specialCap = specialCap
	// Entries are now supplied by the boss's explicit SetNpcList override.
	e.configName = ""
}

func (e campaignCorruptorState) clonePortals() campaignCorruptorState {
	e.portalObjectIDs = append([]uint32(nil), e.portalObjectIDs...)
	e.portalReadyAts = append([]time.Time(nil), e.portalReadyAts...)
	portals := make(map[uint32]campaignCorruptorPortalState, len(e.portals))
	for objectID, portal := range e.portals {
		portal.spawnObjectIDs = append([]uint32(nil), portal.spawnObjectIDs...)
		portals[objectID] = portal
	}
	e.portals = portals
	return e
}

type campaignCorruptorPortalStep struct {
	world           *zone.Zone
	sceneGeneration uint64
	identity        gameplayProducerIdentity
	runtime         campaignNPCActionRuntime
	packet          raknet.Packet
	sessionKey      string
	generation      uint64
	bossObjectID    uint32
	portalObjectID  uint32
	timestamp       uint64
	isStartup       bool
}

func (r campaignNPCActionRuntime) scheduleCorruptorPortals(
	packet raknet.Packet, sessionKey string, generation uint64, bossObjectID uint32,
	plans []campaignCorruptorSpawnPlan, timestamp uint64,
) error {
	r.registry.mutex.RLock()
	member, isMemberFound := r.registry.sessions[sessionKey]
	if !isMemberFound || member.generation != generation || member.zone == nil {
		r.registry.mutex.RUnlock()
		return errors.New("corruptor portal source unavailable")
	}
	boss, isBossFound := member.zone.NPCs().NPC(bossObjectID)
	r.registry.mutex.RUnlock()
	if !isBossFound || boss.IsDefeated {
		return errors.New("corruptor portal boss unavailable")
	}
	sceneGeneration := boss.SecondAggroGeneration()
	for _, plan := range plans {
		if plan.Plan.NounName != campaignCorruptorPortalNounName {
			continue
		}
		cancel, err := r.scheduleCorruptorPortal(packet, sessionKey, generation, bossObjectID, plan, timestamp, sceneGeneration)
		if err != nil {
			return fmt.Errorf("portalStartup: %w", err)
		}
		if cancel == nil {
			return errors.New("corruptor portal cancellation unavailable")
		}
	}
	return nil
}

func (r campaignNPCActionRuntime) scheduleCorruptorPortal(
	packet raknet.Packet, sessionKey string, generation uint64, bossObjectID uint32,
	plan campaignCorruptorSpawnPlan, timestamp uint64, sceneGeneration uint64,
) (raknet.CancelSchedule, error) {
	r.registry.mutex.RLock()
	member, isFound := r.registry.sessions[sessionKey]
	identity := gameplayProducerIdentityFromSession(sessionKey, member, isFound)
	if !identity.isFound || identity.zoneGeneration != generation || identity.transportGeneration != packet.TransportGeneration || member.zone == nil {
		r.registry.mutex.RUnlock()
		return nil, errors.New("corruptor portal carrier unavailable")
	}
	world := member.zone
	boss, isBossFound := world.NPCs().NPC(bossObjectID)
	r.registry.mutex.RUnlock()
	if !isBossFound || boss.IsDefeated || (boss.IsCorruptorStageTwo && !boss.IsSecondAggroCinematic(sceneGeneration)) {
		return nil, errors.New("corruptor portal source replaced")
	}
	step := campaignCorruptorPortalStep{identity: identity, sceneGeneration: sceneGeneration, world: world,
		runtime: r, packet: packet.Autonomous(), sessionKey: sessionKey,
		generation: generation, bossObjectID: bossObjectID,
		portalObjectID: plan.Plan.ObjectID, isStartup: true,
		timestamp: timestamp + uint64(corruptorPortalStartupDelay/time.Millisecond)}
	cancel, err := scheduleNPCProducers(r.registry, step.packet, []raknet.ScheduledPacketProducer{{
		Delay: corruptorPortalStartupDelay, Produce: step.produce,
	}})
	if err != nil {
		return nil, fmt.Errorf("portalAdmission: %w", err)
	}
	return cancel, nil
}

func (e campaignCorruptorPortalStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || peerSession.generation != e.generation ||
		gameplayProducerIdentityFromSession(e.sessionKey, peerSession, isFound) != e.identity ||
		peerSession.zone == nil || peerSession.zone.NPCs() == nil ||
		peerSession.zone != e.world ||
		peerSession.isZoneTerminal() || !peerSession.zone.IsActive() {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	state, isStateFound := peerSession.campaignCorruptorStates[e.bossObjectID]
	portalState, isPortalStateFound := state.portals[e.portalObjectID]
	boss, isBossFound := peerSession.zone.NPCs().NPC(e.bossObjectID)
	portal, isPortalFound := peerSession.zone.NPCs().NPC(e.portalObjectID)
	if !isStateFound || !isPortalStateFound || !isBossFound || boss.IsDefeated ||
		(boss.IsCorruptorStageTwo && !boss.IsSecondAggroCinematic(e.sceneGeneration)) ||
		!isPortalFound || portal.IsDefeated || portal.Plan.OwnerObjectID != e.bossObjectID {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if e.isStartup {
		stablePortal, profileErr := peerSession.zone.NPCs().ActivateEnemyPortal(e.portalObjectID)
		if profileErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("portalStableProfile: %w", profileErr)
		}
		e.runtime.registry.mutex.Unlock()
		packets, err := corruptorPortalStablePackets(stablePortal.Plan.ObjectID)
		if err != nil {
			return nil, fmt.Errorf("portalStable: %w", err)
		}
		err = e.scheduleNext()
		if err != nil {
			return nil, fmt.Errorf("portalFirstPass: %w", err)
		}
		return packets, nil
	}
	// Remove every invalid/dead member before checking the shared local count.
	spawnObjectIDs := make([]uint32, 0, len(portalState.spawnObjectIDs))
	for _, objectID := range portalState.spawnObjectIDs {
		spawn, isSpawnFound := peerSession.zone.NPCs().NPC(objectID)
		if isSpawnFound && !spawn.IsDefeated {
			spawnObjectIDs = append(spawnObjectIDs, objectID)
		}
	}
	portalState.spawnObjectIDs = spawnObjectIDs
	if portalState.configName != "" {
		entries, err := corruptorRosterEntries(peerSession.zone.DirectorDefinition(),
			portalState.configName, "minion", peerSession.binding.ChainLevelIndex)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("portalDefaultRoster: %w", err)
		}
		portalState.minionEntries = entries
	}
	// Native math.random(1,2) consumes Index(1) and always returns one.
	// A nil authored npcList skips the draw; an empty or capped live list does not.
	attemptCount := float64(0)
	if portalState.isNPCListPresent {
		var attemptErr error
		attemptCount, attemptErr = sim.ScriptRandom(peerSession.zone.NPCRandom(), 1, corruptorPortalMinionAttempts)
		if attemptErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("portalAttempts: %w", attemptErr)
		}
	}
	plans := make([]campaignCorruptorSpawnPlan, 0, corruptorPortalMinionAttempts)
	for attempt := 0; attempt < int(attemptCount); attempt++ {
		if len(portalState.spawnObjectIDs) >= portalState.minionCap ||
			len(portalState.minionEntries) == 0 {
			continue
		}
		entryIndex, drawErr := peerSession.zone.NPCRandom().Index(
			uint32(len(portalState.minionEntries)),
		)
		if drawErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("portalMinionDraw: %w", drawErr)
		}
		plan, planErr := e.runtime.corruptorMinionPlan(&peerSession, boss,
			portal.Plan.Position, portalState.minionEntries[entryIndex], plans)
		if planErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("portalMinionPlan: %w", planErr)
		}
		plans = append(plans, campaignCorruptorSpawnPlan{Plan: plan})
		portalState.spawnObjectIDs = append(portalState.spawnObjectIDs, plan.ObjectID)
	}
	// numLieutenantSpawns is zero, so its Lua loop makes no attempts and no draw.
	// Do not substitute specialCap for this unresolved authored attempt setting.
	targetObjectID := corruptorPopulationTarget(peerSession, boss)
	packets, actionPlans, err := marshalCorruptorSpawns(plans, targetObjectID)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("portalSpawn: %w", err)
	}
	err = admitCorruptorPopulation(&peerSession, plans, targetObjectID)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("portalAdmit: %w", err)
	}
	state.portals[e.portalObjectID] = portalState
	peerSession.campaignCorruptorStates[e.bossObjectID] = state
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	err = e.scheduleNext()
	if err != nil {
		return nil, fmt.Errorf("portalRepeat: %w", err)
	}
	actionPackets, err := e.runtime.scheduleFirstActions(e.packet, e.sessionKey,
		e.generation, actionPlans, e.timestamp)
	if err != nil {
		return nil, fmt.Errorf("portalActions: %w", err)
	}
	return append(packets, actionPackets...), nil
}

func (e campaignCorruptorPortalStep) scheduleNext() error {
	e.isStartup = false
	e.timestamp += uint64(corruptorPortalSpawnDelay / time.Millisecond)
	err := scheduleNPCProducer(e.runtime.registry, e.packet, corruptorPortalSpawnDelay, e.produce)
	if err != nil {
		return fmt.Errorf("portalSchedule: %w", err)
	}
	return nil
}

func corruptorPortalStablePackets(objectID uint32) ([][]byte, error) {
	messages := []raknet.AttachedEffectMessage{
		{Slot: 16, ObjectID: objectID, IsRemovalRequested: true, IsHardStop: true},
		{Slot: 16, ObjectID: objectID, IsForceAttached: true,
			Asset: util.HashID("scaldron_boss_portal_effect.ServerEventDef")},
	}
	packets := make([][]byte, 0, len(messages))
	for _, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("stableMarshal: %w", err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}
