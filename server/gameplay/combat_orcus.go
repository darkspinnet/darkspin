package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zone "github.com/darkspinnet/darkspin/server/zone"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
)

type campaignOrcusServantStep struct {
	runtime          campaignNPCActionRuntime
	packet           raknet.Packet
	sessionKey       string
	generation       uint64
	actionGeneration uint64
	orcusObjectID    uint32
	objectID         uint32
	timestamp        uint64
	definition       zonenpc.OrcusSpawnDefinition
}

type campaignOrcusState struct {
	nextSpawnTimestamp      uint64
	nextGroundSlamTimestamp uint64
	nextDiseaseTimestamp    uint64
}

type campaignOrcusRequest struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
}

func (e campaignOrcusRequest) resume(timestamp uint64) ([][]byte, error) {
	return e.runtime.produceDronePunch(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
}

type campaignOrcusSpawnEnd struct {
	request          campaignOrcusRequest
	actionGeneration uint64
}

func (e campaignOrcusSpawnEnd) produce() ([][]byte, error) {
	e.request.runtime.registry.mutex.RLock()
	peerSession, isFound := e.request.runtime.registry.sessions[e.request.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceGenerationActive(
		e.request.generation, e.request.objectID, e.actionGeneration,
	)
	orcus, isOrcusFound := zonenpc.Snapshot{}, false
	if isCurrent {
		orcus, isOrcusFound = peerSession.zone.NPCs().NPC(e.request.objectID)
	}
	e.request.runtime.registry.mutex.RUnlock()
	if !isCurrent || !isOrcusFound || orcus.IsDefeated {
		return nil, nil
	}
	presentationPackets, err := npcraknet.CancelAction(
		e.request.objectID, orcus.Plan.Position, e.request.timestamp,
	)
	if err != nil {
		e.request.runtime.releaseAction(
			e.request.sessionKey, e.request.generation, e.request.objectID,
		)
		return nil, fmt.Errorf("orcusSpawnEndPresentation: %w", err)
	}
	followupPackets, err := e.request.resume(e.request.timestamp)
	if err != nil {
		e.request.runtime.releaseAction(
			e.request.sessionKey, e.request.generation, e.request.objectID,
		)
		return nil, fmt.Errorf("orcusSpawnEndFollowup: %w", err)
	}
	return append(presentationPackets, followupPackets...), nil
}

type campaignOrcusGroundSlamEnd struct {
	request   campaignOrcusRequest
	expiresAt time.Time
}

func (e campaignOrcusGroundSlamEnd) produce() ([][]byte, error) {
	e.request.runtime.registry.mutex.Lock()
	peerSession, isFound := e.request.runtime.registry.sessions[e.request.sessionKey]
	isCurrent := isFound && peerSession.generation == e.request.generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if isCurrent {
		peerSession.zone.NPCs().ClearDamageReduction(e.request.objectID, e.expiresAt)
		e.request.runtime.registry.sessions[e.request.sessionKey] = peerSession
	}
	e.request.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	packet, err := npcraknet.ShieldEffectAsset(
		e.request.objectID, 0, "verdanth_boss_shield.ServerEventDef", true,
	)
	if err != nil {
		return nil, fmt.Errorf("orcusGroundSlamShieldRemove: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e campaignOrcusServantStep) spawn() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceGenerationActive(
		e.generation, e.orcusObjectID, e.actionGeneration,
	)
	if !isCurrent || peerSession.zone.NPCs().OwnedActiveCount(e.orcusObjectID) >=
		e.definition.MaximumServant {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	orcus, isOrcusFound := peerSession.zone.NPCs().NPC(e.orcusObjectID)
	if !isOrcusFound || orcus.IsDefeated {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	plan := zonenpc.SpawnPlan{
		ObjectID: e.objectID, OwnerObjectID: e.orcusObjectID,
		NounName: e.definition.ServantNoun, Position: orcus.Plan.Position,
		IsRewardSuppressed: true, NPCProfile: e.definition.ServantProfile,
		ActionProfile: e.definition.ServantAction, IsActionKnown: true,
	}
	err := peerSession.zone.NPCs().Add(
		[]zonenpc.SpawnPlan{plan}, orcus.TargetObjectID,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("orcusServantAdd: %w", err)
	}
	err = peerSession.zone.PublishNPCSpawn(zoneprojection.NPCSpawn{
		Plans: []zonenpc.SpawnPlan{plan}, TargetObjectID: orcus.TargetObjectID,
	}, peerSession.binding.UserID, e.generation)
	if err != nil {
		rollbackErr := peerSession.zone.NPCs().RollbackAdd([]zonenpc.SpawnPlan{plan})
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("orcusServantPublish: %w", errors.Join(err, rollbackErr))
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	packets, err := npcraknet.TargetedSpawn(plan, orcus.TargetObjectID)
	if err != nil {
		return nil, fmt.Errorf("orcusServantMarshal: %w", err)
	}
	actionPackets, err := e.runtime.scheduleFirstActions(
		e.packet, e.sessionKey, e.generation,
		[]zonenpc.SpawnPlan{plan}, e.timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("orcusServantAction: %w", err)
	}
	return append(packets, actionPackets...), nil
}

func (r campaignNPCActionRuntime) produceOrcusSpawn(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, bool, error) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	orcus, isOrcusFound := peerSession.zone.NPCs().NPC(objectID)
	if !isOrcusFound || orcus.IsDefeated {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	definition, isDefinitionFound := zonenpc.OrcusSpawnProfile(orcus.Plan.NounName)
	if !isDefinitionFound {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	isAbilityAvailable := peerSession.isCampaignNPCActionActiveAt(
		generation, objectID, r.now(),
	) && peerSession.zone.NPCs().SilenceRemaining(objectID, r.now()) == 0
	if !isAbilityAvailable {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	if peerSession.campaignNPCOrcusStates == nil {
		peerSession.campaignNPCOrcusStates = make(map[uint32]campaignOrcusState)
	}
	previousState := peerSession.campaignNPCOrcusStates[objectID]
	if previousState.nextSpawnTimestamp > timestamp {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	footprintRadius := definition.ServantProfile.FootprintRadius
	importedFootprintRadius, err := r.program.FootprintRadius(definition.ServantNoun)
	if err != nil && footprintRadius <= 0 {
		r.registry.mutex.Unlock()
		return nil, true, fmt.Errorf("orcusServantFootprint: %w", err)
	}
	if err == nil {
		footprintRadius = importedFootprintRadius
	}
	definition.ServantProfile.FootprintRadius = footprintRadius
	ownedCount := peerSession.zone.NPCs().OwnedActiveCount(objectID)
	spawnCount := min(definition.ServantCountPerCast, definition.MaximumServant-ownedCount)
	if spawnCount <= 0 {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	firstObjectID, err := peerSession.zone.ReserveObjectIDs(uint32(spawnCount))
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, true, fmt.Errorf("orcusServantReserve: %w", err)
	}
	castPlan := zonenpc.AttackPlan{
		SourceObjectID: objectID, SourcePosition: orcus.Plan.Position,
		Profile: zonenpc.ActionProfile{AnimationName: definition.AnimationName},
	}
	startPackets, err := npcraknet.StationaryCastStart(castPlan, timestamp)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, true, fmt.Errorf("orcusSpawnStart: %w", err)
	}
	steps := make([]raknet.ScheduledPacketProducer, 0, spawnCount+1)
	for index := 0; index < spawnCount; index++ {
		delay := definition.FirstSpawnDelay + time.Duration(index)*definition.SpawnInterval
		step := campaignOrcusServantStep{
			runtime: r, packet: packet, sessionKey: sessionKey, generation: generation,
			actionGeneration: orcus.ActionGeneration,
			orcusObjectID:    objectID, objectID: firstObjectID + uint32(index),
			timestamp: timestamp + uint64(delay/time.Millisecond), definition: definition,
		}
		steps = append(steps, raknet.ScheduledPacketProducer{Delay: delay, Produce: step.spawn})
	}
	castDuration := definition.FirstSpawnDelay +
		time.Duration(spawnCount)*definition.SpawnInterval
	end := campaignOrcusSpawnEnd{
		request: campaignOrcusRequest{
			runtime: r, packet: packet, sessionKey: sessionKey, generation: generation,
			objectID: objectID, timestamp: timestamp + uint64(castDuration/time.Millisecond),
		},
		actionGeneration: orcus.ActionGeneration,
	}
	steps = append(steps, raknet.ScheduledPacketProducer{
		Delay: castDuration, Produce: end.produce,
	})
	state := previousState
	state.nextSpawnTimestamp = timestamp + uint64(definition.Cooldown/time.Millisecond)
	peerSession.campaignNPCOrcusStates[objectID] = state
	r.registry.sessions[sessionKey] = peerSession
	cancel, err := scheduleNPCProducers(r.registry, packet, steps)
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	r.registry.mutex.Unlock()
	if err != nil {
		r.restoreOrcusState(sessionKey, generation, objectID, state, previousState)
		r.releaseAction(sessionKey, generation, objectID)
		return nil, true, fmt.Errorf("orcusServantSchedule: %w", err)
	}
	return startPackets, true, nil
}

func (r campaignNPCActionRuntime) produceOrcusAbility(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, bool, error) {
	request := campaignOrcusRequest{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil, false, nil
	}
	orcus, isOrcusFound := peerSession.zone.NPCs().NPC(objectID)
	_, isOrcus := zonenpc.OrcusGroundSlamProfile(orcus.Plan.NounName)
	isSilenced := isOrcusFound &&
		peerSession.zone.NPCs().SilenceRemaining(objectID, r.now()) > 0
	r.registry.mutex.RUnlock()
	if !isOrcusFound || !isOrcus || isSilenced {
		return nil, false, nil
	}
	isDeferred, err := r.pursuit.deferAction(
		packet, sessionKey, generation, objectID, timestamp, request.resume,
	)
	if err != nil {
		return nil, true, fmt.Errorf("orcusAbilityStun: %w", err)
	}
	if isDeferred {
		return nil, true, nil
	}

	r.registry.mutex.Lock()
	peerSession, isFound = r.registry.sessions[sessionKey]
	isCurrent = isFound && peerSession.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, true, nil
	}
	orcus, isOrcusFound = peerSession.zone.NPCs().NPC(objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, orcus.TargetObjectID,
	)
	if !isOrcusFound || orcus.IsDefeated || !isTargetFound {
		r.registry.mutex.Unlock()
		r.releaseAction(sessionKey, generation, objectID)
		return nil, true, nil
	}
	if peerSession.campaignNPCOrcusStates == nil {
		peerSession.campaignNPCOrcusStates = make(map[uint32]campaignOrcusState)
	}
	state := peerSession.campaignNPCOrcusStates[objectID]
	profile := zonenpc.ActionProfile{}
	isGroundSlam := false
	if timestamp >= state.nextGroundSlamTimestamp {
		profile, _ = zonenpc.OrcusGroundSlamProfile(orcus.Plan.NounName)
		isGroundSlam = true
	} else if timestamp >= state.nextDiseaseTimestamp {
		profile, _ = zonenpc.OrcusDiseaseConeProfile(orcus.Plan.NounName)
	} else {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	plan, planErr := r.planOrcusAbility(orcus, target, profile, isGroundSlam)
	if planErr != nil {
		r.registry.mutex.Unlock()
		return r.pursueOrcusAbility(request, orcus, target, profile)
	}
	previousState := state
	if isGroundSlam {
		state.nextGroundSlamTimestamp = timestamp +
			uint64(profile.Cooldown/time.Millisecond)
	} else {
		state.nextDiseaseTimestamp = timestamp +
			uint64(profile.Cooldown/time.Millisecond)
	}
	peerSession.campaignNPCOrcusStates[objectID] = state
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	packets, err := r.startOrcusAbility(request, plan, isGroundSlam)
	if err != nil {
		r.restoreOrcusState(sessionKey, generation, objectID, state, previousState)
		r.releaseAction(sessionKey, generation, objectID)
		return nil, true, fmt.Errorf("orcusAbilityStart: %w", err)
	}
	return packets, true, nil
}

func (r campaignNPCActionRuntime) planOrcusAbility(
	orcus zonenpc.Snapshot, target zone.NPCTarget,
	profile zonenpc.ActionProfile, isGroundSlam bool,
) (zonenpc.AttackPlan, error) {
	if isGroundSlam {
		plan, err := zonenpc.PlanAttackWithProfile(
			orcus, target.ObjectID, target.Position, profile, target.FootprintRadius,
		)
		if err != nil {
			return zonenpc.AttackPlan{}, fmt.Errorf("orcusGroundSlamPlan: %w", err)
		}
		return plan, nil
	}
	plan, err := zonenpc.PlanControlWithProfile(
		orcus, target.ObjectID, target.Position, profile, target.FootprintRadius,
	)
	if err != nil {
		return zonenpc.AttackPlan{}, fmt.Errorf("orcusDiseasePlan: %w", err)
	}
	return plan, nil
}

func (r campaignNPCActionRuntime) pursueOrcusAbility(
	request campaignOrcusRequest, orcus zonenpc.Snapshot,
	target zone.NPCTarget, profile zonenpc.ActionProfile,
) ([][]byte, bool, error) {
	action, err := campaignNPCActionWithProfile(
		orcus.Plan, target.ObjectID, target.Position, profile,
		target.FootprintRadius,
	)
	if err != nil || !action.IsPursuitNeeded {
		return nil, true, nil
	}
	packets, err := npcraknet.Pursuit(action)
	if err != nil {
		return nil, true, fmt.Errorf("orcusAbilityPursuitMarshal: %w", err)
	}
	err = r.pursuit.schedule(
		request.packet, request.sessionKey, request.generation, request.objectID,
		request.timestamp, action.TargetPosition, profile, request.resume,
	)
	if err != nil {
		r.releaseAction(request.sessionKey, request.generation, request.objectID)
		return nil, true, fmt.Errorf("orcusAbilityPursuitSchedule: %w", err)
	}
	return packets, true, nil
}

func (r campaignNPCActionRuntime) startOrcusAbility(
	request campaignOrcusRequest, plan zonenpc.AttackPlan, isGroundSlam bool,
) ([][]byte, error) {
	packets, err := r.startNPCAttack(request.sessionKey, request.generation, plan, request.timestamp)
	if err != nil {
		return nil, fmt.Errorf("orcusAttackStart: %w", err)
	}
	schedule := campaignConeSchedule{
		runtime: r, packet: request.packet, sessionKey: request.sessionKey,
		generation: request.generation, objectID: request.objectID,
		timestamp: request.timestamp, nextDelay: plan.Profile.ReleaseDelay,
		plan: plan,
	}
	producers := make([]raknet.ScheduledPacketProducer, 0, 8)
	reductionExpiresAt := time.Time{}
	if isGroundSlam {
		reductionExpiresAt = r.now().Add(plan.Profile.ReleaseDelay)
		r.registry.mutex.Lock()
		peerSession, isFound := r.registry.sessions[request.sessionKey]
		isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
			request.generation, request.objectID,
		)
		if !isCurrent {
			r.registry.mutex.Unlock()
			return nil, errors.New("Orcus ground slam source unavailable")
		}
		err = peerSession.zone.NPCs().ApplyDamageReduction(
			request.objectID, 0.75, reductionExpiresAt,
		)
		if err == nil {
			r.registry.sessions[request.sessionKey] = peerSession
		}
		r.registry.mutex.Unlock()
		if err != nil {
			return nil, fmt.Errorf("orcusGroundSlamReduction: %w", err)
		}
		shieldPacket, shieldErr := npcraknet.ShieldEffectAsset(
			request.objectID, 0, "verdanth_boss_shield.ServerEventDef", false,
		)
		if shieldErr != nil {
			r.clearOrcusGroundSlamReduction(request, reductionExpiresAt)
			return nil, fmt.Errorf("orcusGroundSlamShield: %w", shieldErr)
		}
		castPacket, castErr := npcraknet.PositionedEffect(
			"verdanth_bosspit_effect.ServerEventDef", plan.SourcePosition,
		)
		if castErr != nil {
			r.clearOrcusGroundSlamReduction(request, reductionExpiresAt)
			return nil, fmt.Errorf("orcusGroundSlamCast: %w", castErr)
		}
		packets = append(packets, shieldPacket, castPacket)
		end := campaignOrcusGroundSlamEnd{
			request: request, expiresAt: reductionExpiresAt,
		}
		producers = append(producers,
			raknet.ScheduledPacketProducer{Delay: plan.Profile.HitDelay, Produce: schedule.hit},
			raknet.ScheduledPacketProducer{Delay: plan.Profile.ReleaseDelay, Produce: end.produce},
		)
	} else {
		for delay := plan.Profile.HitDelay; delay < plan.Profile.ReleaseDelay; delay += 500 * time.Millisecond {
			pulse := schedule
			pulse.hitDelay = delay
			producers = append(producers, raknet.ScheduledPacketProducer{
				Delay: delay, Produce: pulse.hit,
			})
		}
	}
	producers = append(producers, raknet.ScheduledPacketProducer{
		Delay: plan.Profile.ReleaseDelay, Produce: schedule.next,
	})
	cancel, err := scheduleNPCProducers(r.registry, request.packet, producers)
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		if isGroundSlam {
			r.clearOrcusGroundSlamReduction(request, reductionExpiresAt)
		}
		return nil, fmt.Errorf("orcusAbilitySchedule: %w", err)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) clearOrcusGroundSlamReduction(
	request campaignOrcusRequest, expiresAt time.Time,
) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[request.sessionKey]
	if isFound && peerSession.generation == request.generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil {
		peerSession.zone.NPCs().ClearDamageReduction(request.objectID, expiresAt)
		r.registry.sessions[request.sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
}

func (r campaignNPCActionRuntime) restoreOrcusState(
	sessionKey string, generation uint64, objectID uint32,
	expected campaignOrcusState, previous campaignOrcusState,
) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if isFound && peerSession.generation == generation &&
		peerSession.campaignNPCOrcusStates[objectID] == expected {
		peerSession.campaignNPCOrcusStates[objectID] = previous
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
}
