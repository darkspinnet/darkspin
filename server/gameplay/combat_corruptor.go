package gameplay

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
)

const (
	campaignCorruptorActivationDelay = 10 * time.Second
	campaignCorruptorPortalNounName  = "EnemyPortal.Noun"
	campaignCorruptorPortalCount     = 3
	campaignCorruptorPortalRadius    = float32(8)
	corruptorPhaseEffectSlot         = uint8(15)
)

type campaignCorruptorState struct {
	phases           [5]zonenpc.CorruptorPhase
	phaseIndex       int
	portalPositions  []game.Vec3
	portalObjectIDs  []uint32
	portalReadyAts   []time.Time
	portals          map[uint32]campaignCorruptorPortalState
	isStageTwo       bool
	isPhasePending   bool
	isPhaseActivated bool
}

type campaignCorruptorPhaseStep struct {
	runtime         campaignNPCActionRuntime
	packet          raknet.Packet
	sessionKey      string
	generation      uint64
	objectID        uint32
	timestamp       uint64
	isStageTwoStart bool
}

type campaignCorruptorPortalRespawnPlan struct {
	bossObjectID uint32
	portalIndex  int
	delay        time.Duration
}

type campaignCorruptorPortalRespawnStep struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	plan       campaignCorruptorPortalRespawnPlan
	timestamp  uint64
}

func corruptorRank(nounName string) int {
	switch strings.ToLower(nounName) {
	case "scaldronboss.noun", "scaldronboss_stage2.noun":
		return 1
	case "scaldronboss_2.noun":
		return 2
	case "scaldronboss_3.noun":
		return 3
	default:
		return 0
	}
}

func corruptorPhasePeriod(rank int) time.Duration {
	periods := [...]time.Duration{9 * time.Second, 7 * time.Second, 5 * time.Second}
	if rank < 1 || rank > len(periods) {
		return 0
	}
	return periods[rank-1]
}

func corruptorPortalPopulation(rank int, isStageTwo bool) (int, int) {
	if rank < 1 || rank > 3 {
		return 0, 0
	}
	if isStageTwo {
		minions := [...]int{4, 3, 2}
		specials := [...]int{0, 1, 3}
		return minions[rank-1], specials[rank-1]
	}
	minions := [...]int{3, 1, 2}
	specials := [...]int{0, 1, 2}
	return minions[rank-1], specials[rank-1]
}

func corruptorPortalRespawnDuration(rank int) time.Duration {
	durations := [...]time.Duration{60 * time.Second, 45 * time.Second, 30 * time.Second}
	if rank < 1 || rank > len(durations) {
		return 0
	}
	return durations[rank-1]
}

func shuffleCorruptorPhases(
	phases [5]zonenpc.CorruptorPhase, randomFloat func() float64,
) [5]zonenpc.CorruptorPhase {
	if randomFloat == nil {
		return phases
	}
	for index := len(phases) - 1; index > 0; index-- {
		selected := int(randomFloat() * float64(index+1))
		selected = min(max(selected, 0), index)
		phases[index], phases[selected] = phases[selected], phases[index]
	}
	return phases
}

func newCampaignCorruptorState(randomFloat func() float64) campaignCorruptorState {
	phases := [5]zonenpc.CorruptorPhase{
		zonenpc.CorruptorPhaseQuantum,
		zonenpc.CorruptorPhaseNecro,
		zonenpc.CorruptorPhasePlasma,
		zonenpc.CorruptorPhaseLife,
		zonenpc.CorruptorPhaseTech,
	}
	return campaignCorruptorState{
		phases: shuffleCorruptorPhases(phases, randomFloat), phaseIndex: -1,
	}
}

func (r campaignNPCActionRuntime) startCorruptorControllers(
	packet raknet.Packet, sessionKey string, generation uint64,
	plans []zonenpc.SpawnPlan, timestamp uint64,
) error {
	for _, plan := range plans {
		if corruptorRank(plan.NounName) == 0 || plan.OwnerObjectID != 0 {
			continue
		}
		r.registry.mutex.Lock()
		peerSession, isFound := r.registry.sessions[sessionKey]
		isCurrent := isFound && peerSession.generation == generation &&
			peerSession.zone != nil && peerSession.zone.NPCs() != nil
		if !isCurrent {
			r.registry.mutex.Unlock()
			continue
		}
		if peerSession.campaignCorruptorStates == nil {
			peerSession.campaignCorruptorStates = make(map[uint32]campaignCorruptorState)
		}
		_, isStarted := peerSession.campaignCorruptorStates[plan.ObjectID]
		if !isStarted {
			state := newCampaignCorruptorState(peerSession.zone.NPCRandom().Float64)
			peerSession.campaignCorruptorStates[plan.ObjectID] = state
			r.registry.sessions[sessionKey] = peerSession
		}
		r.registry.mutex.Unlock()
		if isStarted {
			continue
		}
		step := campaignCorruptorPhaseStep{
			runtime: r, packet: packet.Autonomous(), sessionKey: sessionKey,
			generation: generation, objectID: plan.ObjectID,
			timestamp: timestamp + uint64(campaignCorruptorActivationDelay/time.Millisecond),
		}
		err := scheduleNPCProducer(r.registry, step.packet, campaignCorruptorActivationDelay, step.produce)
		if err != nil {
			return fmt.Errorf("corruptorActivationSchedule: %w", err)
		}
	}
	return nil
}

func (s *gameplayPeerSession) requestCampaignCorruptorPhase(
	result zonenpc.DamageResult,
) bool {
	if s == nil || result.ObjectID == 0 || result.IsDamageImmune ||
		result.IsCorruptorStageTwoStarted || result.Damage <= 0 ||
		s.campaignCorruptorStates == nil {
		return false
	}
	state, isFound := s.campaignCorruptorStates[result.ObjectID]
	if !isFound || state.isStageTwo || !state.isPhaseActivated || state.isPhasePending {
		return false
	}
	npc, isNPCFound := s.zone.NPCs().NPC(result.ObjectID)
	if !isNPCFound || npc.Plan.NPCProfile.HitPoint <= 0 {
		return false
	}
	previousFraction := result.PreviousHealth / npc.Plan.NPCProfile.HitPoint
	currentFraction := result.HitPoint / npc.Plan.NPCProfile.HitPoint
	thresholds := [...]float32{0.8, 0.6, 0.4, 0.2}
	for _, threshold := range thresholds {
		if previousFraction > threshold && currentFraction <= threshold {
			state.isPhasePending = true
			s.campaignCorruptorStates[result.ObjectID] = state
			return true
		}
	}
	return false
}

func (s *gameplayPeerSession) prepareCampaignCorruptorStageTwo(objectID uint32) {
	if s == nil || objectID == 0 {
		return
	}
	if s.campaignCorruptorStates == nil {
		s.campaignCorruptorStates = make(map[uint32]campaignCorruptorState)
	}
	randomFloat := func() float64 { return 0.5 }
	if s.zone != nil && s.zone.NPCRandom() != nil {
		randomFloat = s.zone.NPCRandom().Float64
	}
	state := newCampaignCorruptorState(randomFloat)
	state.isStageTwo = true
	state.isPhasePending = true
	s.campaignCorruptorStates[objectID] = state
}

func (s *gameplayPeerSession) requestCampaignCorruptorPortalRespawn(
	result zonenpc.DamageResult,
) *campaignCorruptorPortalRespawnPlan {
	if s == nil || !result.IsDefeated || s.campaignCorruptorStates == nil ||
		s.zone == nil || s.zone.NPCs() == nil {
		return nil
	}
	portal, isPortalFound := s.zone.NPCs().NPC(result.ObjectID)
	if !isPortalFound || !strings.EqualFold(
		portal.Plan.NounName, campaignCorruptorPortalNounName,
	) || portal.Plan.OwnerObjectID == 0 {
		return nil
	}
	state, isStateFound := s.campaignCorruptorStates[portal.Plan.OwnerObjectID]
	if !isStateFound {
		return nil
	}
	portalIndex := -1
	for index, objectID := range state.portalObjectIDs {
		if objectID == result.ObjectID {
			portalIndex = index
			break
		}
	}
	if portalIndex < 0 {
		return nil
	}
	boss, isBossFound := s.zone.NPCs().NPC(portal.Plan.OwnerObjectID)
	delay := corruptorPortalRespawnDuration(corruptorRank(boss.Plan.NounName))
	if !isBossFound || delay <= 0 {
		return nil
	}
	state.portalObjectIDs[portalIndex] = 0
	delete(state.portals, portal.Plan.ObjectID)
	state.portalReadyAts[portalIndex] = time.Now().Add(delay)
	s.campaignCorruptorStates[portal.Plan.OwnerObjectID] = state
	return &campaignCorruptorPortalRespawnPlan{
		bossObjectID: portal.Plan.OwnerObjectID, portalIndex: portalIndex, delay: delay,
	}
}

func (e campaignCorruptorPhaseStep) produce() ([][]byte, error) {
	return e.runtime.advanceCorruptorPhase(e)
}

func (e campaignCorruptorPortalRespawnStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	state, isStateFound := peerSession.campaignCorruptorStates[e.plan.bossObjectID]
	boss, isBossFound := peerSession.zone.NPCs().NPC(e.plan.bossObjectID)
	if !isStateFound || !isBossFound || boss.IsDefeated ||
		e.plan.portalIndex < 0 || e.plan.portalIndex >= len(state.portalObjectIDs) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if state.portalObjectIDs[e.plan.portalIndex] != 0 {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	state = state.clonePortals()
	state.portalReadyAts[e.plan.portalIndex] = time.Time{}
	phase := state.phases[state.phaseIndex]
	targetObjectID := corruptorPopulationTarget(peerSession, boss)
	plans, state, err := e.runtime.planCorruptorPhasePopulation(
		&peerSession, boss, state, phase,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("corruptorPortalPopulation: %w", err)
	}
	packets, actionPlans, err := marshalCorruptorSpawns(plans, targetObjectID)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("corruptorPortalSpawn: %w", err)
	}
	err = admitCorruptorPopulation(&peerSession, plans, targetObjectID)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("corruptorPortalAdmit: %w", err)
	}
	peerSession.campaignCorruptorStates[e.plan.bossObjectID] = state
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	err = e.runtime.scheduleCorruptorPortals(e.packet, e.sessionKey, e.generation,
		e.plan.bossObjectID, plans, e.timestamp)
	if err != nil {
		return nil, fmt.Errorf("portalTimers: %w", err)
	}
	actionPackets, err := e.runtime.scheduleFirstActions(
		e.packet, e.sessionKey, e.generation, actionPlans, e.timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("corruptorPortalAction: %w", err)
	}
	return append(packets, actionPackets...), nil
}

func (r campaignNPCActionRuntime) advanceCorruptorPhase(
	step campaignCorruptorPhaseStep,
) ([][]byte, error) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[step.sessionKey]
	isCurrent := isFound && peerSession.generation == step.generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	state, isStateFound := peerSession.campaignCorruptorStates[step.objectID]
	boss, isBossFound := peerSession.zone.NPCs().NPC(step.objectID)
	if !isStateFound || !isBossFound || boss.IsDefeated {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	if step.isStageTwoStart {
		state.isStageTwo = true
	}
	state.phaseIndex++
	if state.phaseIndex >= len(state.phases) {
		state.phases = shuffleCorruptorPhases(
			state.phases, peerSession.zone.NPCRandom().Float64,
		)
		state.phaseIndex = 0
	}
	phase := state.phases[state.phaseIndex]
	profile, isProfileFound := zonenpc.ScaldronBossPhaseProfile(
		boss.Plan.NounName, phase, state.isStageTwo,
	)
	if !isProfileFound {
		r.registry.mutex.Unlock()
		return nil, errors.New("corruptor phase profile unavailable")
	}
	boss, err := peerSession.zone.NPCs().SetActionProfile(step.objectID, profile)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("corruptorPhaseProfile: %w", err)
	}
	state.isPhaseActivated = true
	state.isPhasePending = false
	if len(state.portalPositions) == 0 {
		state.portalPositions = campaignCorruptorPortalPositions(
			peerSession.zone.DirectorDefinition(), boss.Plan.Position,
		)
	}
	targetObjectID := corruptorPopulationTarget(peerSession, boss)
	plans, state, err := r.planCorruptorPhasePopulation(
		&peerSession, boss, state, phase,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, err
	}
	spawnPackets, spawnedActionPlans, err := marshalCorruptorSpawns(
		plans, targetObjectID,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("corruptorPhaseSpawn: %w", err)
	}
	err = admitCorruptorPopulation(&peerSession, plans, targetObjectID)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("corruptorPhaseAdmit: %w", err)
	}
	peerSession.campaignCorruptorStates[step.objectID] = state
	r.registry.sessions[step.sessionKey] = peerSession
	r.registry.mutex.Unlock()
	err = r.scheduleCorruptorPortals(step.packet, step.sessionKey, step.generation,
		step.objectID, plans, step.timestamp)
	if err != nil {
		return nil, fmt.Errorf("phasePortalTimers: %w", err)
	}

	packets, err := corruptorPhasePackets(boss.Plan.ObjectID, phase)
	if err != nil {
		return nil, err
	}
	packets = append(packets, spawnPackets...)
	resumePackets, err := r.restartCorruptorAction(
		step.packet, step.sessionKey, step.generation, boss, step.timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("phaseResume: %w", err)
	}
	packets = append(packets, resumePackets...)
	actionPackets, err := r.scheduleFirstActions(
		step.packet, step.sessionKey, step.generation, spawnedActionPlans, step.timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("corruptorPhaseAction: %w", err)
	}
	packets = append(packets, actionPackets...)
	if state.isStageTwo {
		period := corruptorPhasePeriod(corruptorRank(boss.Plan.NounName))
		next := step
		next.timestamp += uint64(period / time.Millisecond)
		next.isStageTwoStart = false
		err = scheduleNPCProducer(r.registry, step.packet, period, next.produce)
		if err != nil {
			return nil, fmt.Errorf("corruptorPhaseSchedule: %w", err)
		}
	}
	return packets, nil
}

type campaignCorruptorSpawnPlan struct {
	Plan zonenpc.SpawnPlan
}

func corruptorPopulationTarget(
	peerSession gameplayPeerSession, boss zonenpc.Snapshot,
) uint32 {
	if boss.TargetObjectID != 0 {
		return boss.TargetObjectID
	}
	return peerSession.deployedObjectID
}

func (r campaignNPCActionRuntime) planCorruptorPhasePopulation(
	peerSession *gameplayPeerSession, boss zonenpc.Snapshot,
	state campaignCorruptorState, phase zonenpc.CorruptorPhase,
) ([]campaignCorruptorSpawnPlan, campaignCorruptorState, error) {
	if peerSession == nil || peerSession.zone == nil {
		return nil, state, errors.New("corruptor population unavailable")
	}
	targetObjectID := corruptorPopulationTarget(*peerSession, boss)
	if targetObjectID == 0 {
		return nil, state, errors.New("corruptor population target unavailable")
	}
	minionEntries, specialEntries, err := corruptorPopulationEntries(
		peerSession.zone.DirectorDefinition(), phase, peerSession.binding.ChainLevelIndex,
	)
	if err != nil {
		return nil, state, fmt.Errorf("corruptorRoster: %w", err)
	}
	state = state.clonePortals()
	plans := make([]campaignCorruptorSpawnPlan, 0)
	if len(state.portalObjectIDs) != len(state.portalPositions) {
		portalObjectIDs := make([]uint32, len(state.portalPositions))
		copy(portalObjectIDs, state.portalObjectIDs)
		state.portalObjectIDs = portalObjectIDs
	}
	if len(state.portalReadyAts) != len(state.portalPositions) {
		portalReadyAts := make([]time.Time, len(state.portalPositions))
		copy(portalReadyAts, state.portalReadyAts)
		state.portalReadyAts = portalReadyAts
	}
	for index, objectID := range state.portalObjectIDs {
		portal, isPortalFound := peerSession.zone.NPCs().NPC(objectID)
		if isPortalFound && !portal.IsDefeated {
			continue
		}
		delete(state.portals, objectID)
		state.portalObjectIDs[index] = 0
	}
	portalProfile := boss.Plan.NPCProfile
	portalNounKey := strings.ToLower(campaignCorruptorPortalNounName)
	profile, isFound := peerSession.zone.DirectorDefinition().NPCProfilesByNoun[portalNounKey]
	if isFound && profile.IsKnown && profile.HitPoint > 0 {
		portalProfile = profile
	} else {
		portalProfile.HitPoint = 50
		portalProfile.PowerPoint = 0
		portalProfile.FootprintRadius = 1.5
		portalProfile.IsTargetable = true
		portalProfile.IsKnown = true
	}
	for index, position := range state.portalPositions {
		if state.portalObjectIDs[index] != 0 ||
			(!state.portalReadyAts[index].IsZero() &&
				r.now().Before(state.portalReadyAts[index])) {
			continue
		}
		objectID, err := peerSession.reserveCampaignObjectID()
		if err != nil {
			return nil, state, fmt.Errorf("corruptorPortalReserve[%d]: %w", index, err)
		}
		plan := zonenpc.SpawnPlan{
			ObjectID: objectID, OwnerObjectID: boss.Plan.ObjectID,
			NounName:  campaignCorruptorPortalNounName,
			Position:  position,
			LocusID:   boss.Plan.LocusID,
			IsFixture: true, IsLootSuppressed: true, IsExperienceSuppressed: true, IsEncounterAuxiliary: true, NPCProfile: portalProfile,
			IsActionKnown: true,
			ActionProfile: zonenpc.ActionProfile{
				PassiveEffectName: "scaldron_portal_rampUp_effect.ServerEventDef",
			},
		}
		plans = append(plans, campaignCorruptorSpawnPlan{Plan: plan})
		state.portalObjectIDs[index] = objectID
		state.portalReadyAts[index] = time.Time{}
	}
	minionCount, specialCount := corruptorPortalPopulation(
		corruptorRank(boss.Plan.NounName), state.isStageTwo,
	)
	for _, portalObjectID := range state.portalObjectIDs {
		if portalObjectID == 0 {
			continue
		}
		portalState, isFound := state.portals[portalObjectID]
		if !isFound {
			portalState = newCampaignCorruptorPortalState()
		}
		portalState.setNpcList(minionEntries, specialEntries, minionCount, specialCount)
		state.portals[portalObjectID] = portalState
	}
	return plans, state, nil
}

func marshalCorruptorSpawns(
	plans []campaignCorruptorSpawnPlan, targetObjectID uint32,
) ([][]byte, []zonenpc.SpawnPlan, error) {
	packets := make([][]byte, 0, len(plans)*5)
	actionPlans := make([]zonenpc.SpawnPlan, 0, len(plans))
	for index, plan := range plans {
		var spawnPackets [][]byte
		var err error
		if plan.Plan.IsFixture {
			spawnPackets, err = npcraknet.Spawn(plan.Plan)
		} else {
			spawnPackets, err = npcraknet.TargetedSpawn(plan.Plan, targetObjectID)
			actionPlans = append(actionPlans, plan.Plan)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("corruptorSpawnMarshal[%d]: %w", index, err)
		}
		packets = append(packets, spawnPackets...)
	}
	return packets, actionPlans, nil
}

func admitCorruptorPopulation(
	peerSession *gameplayPeerSession, plans []campaignCorruptorSpawnPlan,
	targetObjectID uint32,
) error {
	if len(plans) == 0 {
		return nil
	}
	plainPlans := make([]zonenpc.SpawnPlan, 0, len(plans))
	for _, plan := range plans {
		plainPlans = append(plainPlans, plan.Plan)
	}
	err := peerSession.zone.NPCs().Add(plainPlans, targetObjectID)
	if err != nil {
		return fmt.Errorf("corruptorPopulationAdd: %w", err)
	}
	err = peerSession.zone.PublishNPCSpawn(zoneprojection.NPCSpawn{
		Plans: plainPlans, TargetObjectID: targetObjectID,
	}, peerSession.binding.UserID, peerSession.generation)
	if err != nil {
		rollbackErr := peerSession.zone.NPCs().RollbackAdd(plainPlans)
		return fmt.Errorf(
			"corruptorPopulationPublish: %w", errors.Join(err, rollbackErr),
		)
	}
	return nil
}

func (r campaignNPCActionRuntime) corruptorMinionPlan(
	s *gameplayPeerSession,
	boss zonenpc.Snapshot, position game.Vec3,
	entry game.CampaignDirectorEntry, pendingPlans []campaignCorruptorSpawnPlan,
) (zonenpc.SpawnPlan, error) {
	objectID, err := s.reserveCampaignObjectID()
	if err != nil {
		return zonenpc.SpawnPlan{}, fmt.Errorf("corruptorMinionReserve: %w", err)
	}
	nounName := entry.NounName
	director := s.zone.DirectorDefinition()
	npcProfile, isProfileFound := director.NPCProfilesByNoun[strings.ToLower(nounName)]
	if !isProfileFound {
		npcProfile = entry.NPCProfile
	}
	actionProfile, isActionFound := zonenpc.ActionProfileForNoun(nounName)
	if !isActionFound {
		return zonenpc.SpawnPlan{}, fmt.Errorf("corruptorMinionProfile: %s", nounName)
	}
	if !npcProfile.IsKnown {
		npcProfile = boss.Plan.NPCProfile
		hitPoint := r.program.NonPlayerHitPoint[util.HashID(
			strings.TrimSuffix(nounName, ".Noun"),
		)]
		if hitPoint <= 0 {
			hitPoint = 20
		}
		footprintRadius, footprintErr := r.program.FootprintRadius(nounName)
		if footprintErr != nil || footprintRadius <= 0 {
			footprintRadius = 0.5
		}
		npcProfile.HitPoint = hitPoint
		npcProfile.FootprintRadius = footprintRadius
		npcProfile.GraphicsScale = 1
		npcProfile.IsTargetable = true
		npcProfile.IsKnown = true
	}
	position = r.corruptorScatterPosition(s, position, nounName, pendingPlans)
	return zonenpc.SpawnPlan{
		ObjectID: objectID, OwnerObjectID: boss.Plan.ObjectID,
		NounName: nounName, Position: position,
		LocusID:              boss.Plan.LocusID,
		IsEncounterAuxiliary: true, NPCProfile: npcProfile,
		ActionProfile: actionProfile, IsActionKnown: true,
	}, nil
}

func campaignCorruptorPortalPositions(
	director game.CampaignDirector, bossPosition game.Vec3,
) []game.Vec3 {
	positions := make([]game.Vec3, 0, campaignCorruptorPortalCount)
	for _, markerSet := range director.MarkerSets {
		for _, marker := range markerSet.Markers {
			nounName := strings.ToLower(marker.NounName)
			if !strings.Contains(nounName, "bossportal") &&
				!strings.Contains(nounName, "enemyportal") {
				continue
			}
			if marker.Position.Sub(bossPosition).Length() > 50 {
				continue
			}
			positions = append(positions, marker.Position)
		}
	}
	if len(positions) != 0 {
		return positions
	}
	for index := 0; index < campaignCorruptorPortalCount; index++ {
		angle := float64(index) * 2 * math.Pi / campaignCorruptorPortalCount
		position := bossPosition
		position.X += float32(math.Cos(angle)) * campaignCorruptorPortalRadius
		position.Y += float32(math.Sin(angle)) * campaignCorruptorPortalRadius
		positions = append(positions, position)
	}
	return positions
}

// Boss Lua chunks 305/81 supply these explicit LevelConfig references. The
// EnemyPortal passive's unconfigured default is TNX-173.LevelConfig.
func corruptorPhaseConfigName(phase zonenpc.CorruptorPhase) string {
	switch phase {
	case zonenpc.CorruptorPhaseQuantum:
		return "Zelem.LevelConfig"
	case zonenpc.CorruptorPhaseNecro:
		return "Nocturna.LevelConfig"
	case zonenpc.CorruptorPhasePlasma:
		return "Cryos.LevelConfig"
	case zonenpc.CorruptorPhaseLife:
		return "Verdanth.LevelConfig"
	case zonenpc.CorruptorPhaseTech:
		return "Sentios.LevelConfig"
	default:
		return "TNX-173.LevelConfig"
	}
}

func corruptorPopulationEntries(
	director game.CampaignDirector, phase zonenpc.CorruptorPhase, stage uint32,
) ([]game.CampaignDirectorEntry, []game.CampaignDirectorEntry, error) {
	if stage == 0 {
		return nil, nil, errors.New("corruptor roster stage unavailable")
	}
	configName := corruptorPhaseConfigName(phase)
	minionEntries, err := corruptorRosterEntries(director, configName, "minion", stage)
	if err != nil {
		return nil, nil, fmt.Errorf("minionRoster: %w", err)
	}
	specialEntries, err := corruptorRosterEntries(director, configName, "special", stage)
	if err != nil {
		return nil, nil, fmt.Errorf("specialRoster: %w", err)
	}
	return minionEntries, specialEntries, nil
}

func corruptorRosterEntries(
	director game.CampaignDirector, configName, configKind string, stage uint32,
) ([]game.CampaignDirectorEntry, error) {
	for _, pool := range director.ExternalPools {
		if !strings.EqualFold(pool.ConfigurationName, configName) ||
			!strings.EqualFold(pool.ConfigKind, configKind) {
			continue
		}
		entries := make([]game.CampaignDirectorEntry, 0, len(pool.Entries))
		// sub_9F9550 -> sub_9F7A50 retains authored order and duplicates,
		// gates by the inclusive stage range, and never checks horde legality.
		// Each spawn draws from this unchanged list with replacement.
		for _, entry := range pool.Entries {
			if stage < entry.MinimumDifficulty || stage > entry.MaximumDifficulty {
				continue
			}
			entries = append(entries, entry)
		}
		// An authored empty role (or no eligible entries) safely skips spawning.
		return entries, nil
	}
	return nil, fmt.Errorf("corruptor roster missing: %s/%s", configName, configKind)
}

func corruptorPhasePackets(
	objectID uint32, phase zonenpc.CorruptorPhase,
) ([][]byte, error) {
	effectName := zonenpc.CorruptorPhaseEffectName(phase)
	if objectID == 0 || effectName == "" {
		return nil, errors.New("corruptor phase presentation invalid")
	}
	messages := []raknet.ApplicationMessage{
		raknet.AttachedEffectMessage{
			Slot: corruptorPhaseEffectSlot, ObjectID: objectID,
			IsRemovalRequested: true, IsHardStop: true,
		},
		raknet.ObjectEffectMessage{
			Asset:    util.HashID("scaldronboss_transition_shader_effect.ServerEventDef"),
			ObjectID: objectID,
		},
		raknet.AttachedEffectMessage{
			Slot: corruptorPhaseEffectSlot, IsForceAttached: true,
			Asset: util.HashID(effectName), ObjectID: objectID,
		},
	}
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("corruptorPhaseMarshal[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) produceCorruptorGravityOrb(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, bool, error) {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	npc, isNPCFound := zonenpc.Snapshot{}, false
	if isCurrent {
		npc, isNPCFound = peerSession.zone.NPCs().NPC(objectID)
	}
	r.registry.mutex.RUnlock()
	if !isNPCFound || npc.Plan.ActionProfile.AbilityName != "SummonGravityOrb" ||
		corruptorRank(npc.Plan.NounName) == 0 {
		return nil, false, nil
	}
	packets, err := r.producePolarisGravityOrb(
		packet, sessionKey, generation, objectID, timestamp,
	)
	if err != nil {
		return nil, true, err
	}
	return packets, true, nil
}
