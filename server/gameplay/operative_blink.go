package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type spacetimeBlinkStep struct {
	intangibleEnd time.Time
	isArrived     bool
	runtime       campaignNPCActionRuntime
	packet        raknet.Packet
	sessionKey    string
	generation    uint64
	plan          zonenpc.AttackPlan
	timestamp     uint64
}

func (e campaignNPCActionRuntime) produceSpacetimeBlinkIfReady(packet raknet.Packet, sessionKey string, generation uint64, objectID uint32, timestamp uint64) ([][]byte, bool, error) {
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || !member.isCampaignNPCSourceActive(generation, objectID) {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	enemy, isEnemyFound := member.zone.NPCs().NPC(objectID)
	profile, isProfileFound := zonenpc.SpacetimeBlinkProfile(enemy.Plan.NounName)
	if !isEnemyFound || !isProfileFound || enemy.HitPoint*2 > enemy.Plan.NPCProfile.HitPoint || member.agentBlinkReadiness[objectID] > timestamp {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	target, isTargetFound := member.campaignNPCTarget(generation, enemy.TargetObjectID)
	if !isTargetFound {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	if enemy.Plan.Position.Sub(target.Position).Length() > profile.Range+enemy.Plan.ActorFootprintRadius()+target.ActorFootprintRadius {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	profile = zonenpc.ApplyAffixTiming(profile, enemy.Plan)
	if member.agentBlinkReadiness == nil {
		member.agentBlinkReadiness = make(map[uint32]uint64)
	}
	member.agentBlinkReadiness[objectID] = timestamp + uint64(profile.Cooldown/time.Millisecond)
	e.registry.sessions[sessionKey] = member
	e.registry.mutex.Unlock()
	plan := zonenpc.AttackPlan{SourceObjectID: objectID, TargetObjectID: target.ObjectID, ActionGeneration: enemy.ActionGeneration, SourcePosition: enemy.Plan.Position, TargetPosition: target.Position, Profile: profile}
	packets, err := e.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		e.releaseActionGeneration(sessionKey, generation, objectID, plan.ActionGeneration)
		return nil, true, fmt.Errorf("agentBlinkStart: %w", err)
	}
	step := spacetimeBlinkStep{intangibleEnd: e.now().Add(10 * time.Second), runtime: e, packet: packet, sessionKey: sessionKey, generation: generation, plan: plan, timestamp: timestamp}
	e.registry.mutex.Lock()
	member = e.registry.sessions[sessionKey]
	if member.agentBlinkStates == nil {
		member.agentBlinkStates = make(map[uint32]*spacetimeBlinkStep)
	}
	member.agentBlinkStates[objectID] = &step
	e.registry.sessions[sessionKey] = member
	e.registry.mutex.Unlock()
	err = member.zone.NPCs().ApplyIntangible(objectID, step.intangibleEnd)
	if err != nil {
		return step.failAdmission("agentBlinkIntangible", err)
	}
	err = member.zone.NPCs().SetNavigationCollision(objectID, false)
	if err != nil {
		return step.failAdmission("agentBlinkCollisionDisable", err)
	}
	statePackets, stateErr := npcraknet.ChargeMovementState(objectID, zonenpc.EffectiveMovementSpeedBuff(enemy.Plan), 0, true)
	if stateErr != nil {
		return step.failAdmission("agentBlinkCollision", stateErr)
	}
	packets = append(packets, statePackets...)
	cancel, err := scheduleNPCProducers(e.registry, packet, []raknet.ScheduledPacketProducer{{Delay: profile.HitDelay, Produce: step.land}, {Delay: 10 * time.Second, Produce: step.restore}})
	if err == nil && cancel == nil {
		err = errors.New("blink cancellation unavailable")
	}
	if err != nil {
		e.releaseActionGeneration(sessionKey, generation, objectID, plan.ActionGeneration)
		return nil, true, fmt.Errorf("agentBlinkSchedule: %w", err)
	}
	return packets, true, nil
}

func (e spacetimeBlinkStep) land() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || !member.isCampaignNPCSourceGenerationActive(e.generation, e.plan.SourceObjectID, e.plan.ActionGeneration) {
		e.runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	enemy, isEnemyFound := member.zone.NPCs().NPC(e.plan.SourceObjectID)
	target, isTargetFound := member.campaignNPCTarget(e.generation, e.plan.TargetObjectID)
	if !isEnemyFound || !isTargetFound {
		e.runtime.registry.mutex.RUnlock()
		return e.restore()
	}
	destination, isDestinationFound, err := zonenavigation.RandomTargetTeleportDestination(member.zone.Navigation(), member.zone.NPCRandom(), enemy.Plan.Position, zonenavigation.RandomTeleportRequest{SourcePosition: target.Position, FootprintRadius: enemy.Plan.ActorFootprintRadius(), MinimumDistance: e.plan.Profile.TeleportMinimumDistance, NormalDistance: e.plan.Profile.TeleportNormalDistance, MaximumDistance: e.plan.Profile.TeleportMaximumDistance})
	e.runtime.registry.mutex.RUnlock()
	if err != nil {
		return e.fail("agentBlinkLanding", err)
	}
	if !isDestinationFound {
		return e.arrive(e.timestamp + uint64(e.plan.Profile.HitDelay/time.Millisecond))
	}
	profile := e.plan.Profile
	profile.Range = 0
	profile.Family = zonenpc.ActionCharge
	baseSpeed := enemy.Plan.NPCProfile.BaseCombatSpeed
	if baseSpeed <= 0 {
		baseSpeed = 5
	}
	profile.MovementSpeed = baseSpeed * (31 + zonenpc.EffectiveMovementSpeedBuff(enemy.Plan))
	action := zonenpc.FirstActionPlan{ObjectID: e.plan.SourceObjectID, TargetObjectID: e.plan.TargetObjectID, SourcePosition: enemy.Plan.Position, TargetPosition: destination, Profile: profile, IsPursuitNeeded: true}
	packets, err := npcraknet.Pursuit(action)
	if err != nil {
		return e.fail("agentBlinkMoveMarshal", err)
	}
	statePackets, err := npcraknet.ChargeMovementState(e.plan.SourceObjectID, 30+zonenpc.EffectiveMovementSpeedBuff(enemy.Plan), 0, true)
	if err != nil {
		return e.fail("agentBlinkSpeed", err)
	}
	err = e.runtime.pursuit.schedule(e.packet, e.sessionKey, e.generation, e.plan.SourceObjectID, e.timestamp+uint64(profile.HitDelay/time.Millisecond), destination, profile, e.arrive)
	if err != nil {
		return e.fail("agentBlinkMove", err)
	}
	return append(statePackets, packets...), nil
}

func (e spacetimeBlinkStep) arrive(timestamp uint64) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	state := member.agentBlinkStates[e.plan.SourceObjectID]
	if state == nil || state.intangibleEnd != e.intangibleEnd || state.isArrived {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	state.isArrived = true
	e.runtime.registry.mutex.Unlock()
	e.timestamp = timestamp - uint64(e.plan.Profile.HitDelay/time.Millisecond)
	cancel, err := scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{{Delay: 500 * time.Millisecond, Produce: e.turn}})
	if err == nil && cancel == nil {
		err = errors.New("blink turn continuation unavailable")
	}
	if err != nil {
		return e.fail("agentBlinkTurnSchedule", err)
	}
	return nil, nil
}

func (e spacetimeBlinkStep) turn() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || !member.isCampaignNPCSourceGenerationActive(e.generation, e.plan.SourceObjectID, e.plan.ActionGeneration) {
		e.runtime.registry.mutex.RUnlock()
		return e.restore()
	}
	target, isTargetFound := member.campaignNPCTarget(e.generation, e.plan.TargetObjectID)
	enemy, isEnemyFound := member.zone.NPCs().NPC(e.plan.SourceObjectID)
	if !isTargetFound || !isEnemyFound {
		e.runtime.registry.mutex.RUnlock()
		return e.restore()
	}
	err := member.zone.NPCs().FacePosition(e.plan.SourceObjectID, target.Position)
	e.runtime.registry.mutex.RUnlock()
	if err != nil {
		return e.fail("agentBlinkFace", err)
	}
	facing := target.Position.Sub(enemy.Plan.Position)
	if facing.Length() <= 0 {
		facing = enemy.Facing
	}
	posePacket, err := npcraknet.RestorePose(e.plan.SourceObjectID, enemy.Plan.Position, facing)
	if err != nil {
		return e.fail("agentBlinkFaceMarshal", err)
	}
	cancel, err := scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{{Delay: time.Second, Produce: e.emerge}})
	if err == nil && cancel == nil {
		err = errors.New("blink emerge continuation unavailable")
	}
	if err != nil {
		return e.fail("agentBlinkEmergeSchedule", err)
	}
	return [][]byte{posePacket}, nil
}

func (e spacetimeBlinkStep) emerge() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && member.isCampaignNPCSourceGenerationActive(e.generation, e.plan.SourceObjectID, e.plan.ActionGeneration)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return e.restore()
	}
	animation, err := npcraknet.AnimationState(e.plan.SourceObjectID, e.plan.Profile.TeleportAnimationName, e.timestamp+uint64(e.plan.Profile.HitDelay/time.Millisecond)+1500)
	if err != nil {
		return e.fail("agentBlinkArrival", err)
	}
	cancel, err := scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{{Delay: 829999983 * time.Nanosecond, Produce: e.resume}})
	if err == nil && cancel == nil {
		err = errors.New("blink continuation unavailable")
	}
	if err != nil {
		return e.fail("agentBlinkResumeSchedule", err)
	}
	return [][]byte{animation}, nil
}
func (e spacetimeBlinkStep) restore() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation || member.zone == nil {
		return nil, nil
	}
	state := member.agentBlinkStates[e.plan.SourceObjectID]
	if state == nil || state.intangibleEnd != e.intangibleEnd {
		return nil, nil
	}
	packets, err := state.restoreLocked(&member)
	if err != nil {
		return nil, fmt.Errorf("agentBlinkRestore: %w", err)
	}
	delete(member.agentBlinkStates, e.plan.SourceObjectID)
	e.runtime.registry.sessions[e.sessionKey] = member
	return packets, nil
}

func (e *spacetimeBlinkStep) restoreLocked(member *gameplayPeerSession) ([][]byte, error) {
	if member.zone == nil {
		return nil, nil
	}
	enemy, isFound := member.zone.NPCs().NPC(e.plan.SourceObjectID)
	if !isFound {
		return nil, nil
	}
	member.zone.NPCs().ClearIntangible(e.plan.SourceObjectID, e.intangibleEnd)
	if enemy.IsDefeated {
		return nil, nil
	}
	err := member.zone.NPCs().SetNavigationCollision(e.plan.SourceObjectID, true)
	if err != nil {
		return nil, fmt.Errorf("agentBlinkCollisionEnable: %w", err)
	}
	packets, err := npcraknet.ChargeMovementState(e.plan.SourceObjectID, zonenpc.EffectiveMovementSpeedBuff(enemy.Plan), 0, false)
	if err != nil {
		return nil, fmt.Errorf("agentBlinkCollisionRestore: %w", err)
	}
	return packets, nil
}

func (e *gameplayPeerSession) retireSpacetimeBlinkLocked(sourceID uint32, actionGeneration uint64) error {
	for objectID, state := range e.agentBlinkStates {
		if state == nil || (sourceID != 0 && sourceID != objectID) || (actionGeneration != 0 && actionGeneration != state.plan.ActionGeneration) {
			continue
		}
		packets, err := state.restoreLocked(e)
		if err != nil {
			return fmt.Errorf("agentBlinkRetire: %w", err)
		}
		delete(e.agentBlinkStates, objectID)
		e.queueCampaignPackets(packets)
	}
	return nil
}
func (e spacetimeBlinkStep) resume() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && member.isCampaignNPCSourceGenerationActive(e.generation, e.plan.SourceObjectID, e.plan.ActionGeneration)
	e.runtime.registry.mutex.RUnlock()
	restorePackets, restoreErr := e.restore()
	if restoreErr != nil {
		return nil, fmt.Errorf("agentBlinkCleanup: %w", restoreErr)
	}
	if !isCurrent {
		return restorePackets, nil
	}
	if e.runtime.finishScenarioNPCManualAction(e.sessionKey, e.generation, e.plan.SourceObjectID, e.plan.ActionGeneration) {
		return restorePackets, nil
	}
	packets, err := e.runtime.produceEnemyMelee(e.packet, e.sessionKey, e.generation, e.plan.SourceObjectID, e.timestamp+uint64((e.plan.Profile.HitDelay+e.plan.Profile.TeleportAnimationDelay)/time.Millisecond))
	if err != nil {
		e.queueCleanupPackets(restorePackets)
		return e.fail("agentBlinkCage", err)
	}
	return append(restorePackets, packets...), nil
}

func (e spacetimeBlinkStep) failAdmission(step string, cause error) ([][]byte, bool, error) {
	packets, err := e.fail(step, cause)
	if err != nil {
		return packets, true, fmt.Errorf("agentBlinkAdmission: %w", err)
	}
	return packets, true, nil
}

func (e spacetimeBlinkStep) fail(step string, cause error) ([][]byte, error) {
	packets, cleanupErr := e.restore()
	e.queueCleanupPackets(packets)
	e.runtime.releaseActionGeneration(e.sessionKey, e.generation, e.plan.SourceObjectID, e.plan.ActionGeneration)
	return nil, fmt.Errorf("%s: %w", step, errors.Join(cause, cleanupErr))
}

func (e spacetimeBlinkStep) queueCleanupPackets(packets [][]byte) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	source, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || source.generation != e.generation {
		return
	}
	for memberKey, member := range e.runtime.registry.sessions {
		if member.zone == source.zone {
			member.queueCampaignPackets(packets)
			e.runtime.registry.sessions[memberKey] = member
		}
	}
}
