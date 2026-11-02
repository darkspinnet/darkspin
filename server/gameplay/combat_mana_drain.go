package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zoneabilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type campaignNPCManaDrainRequest struct {
	runtime          campaignNPCActionRuntime
	packet           raknet.Packet
	sessionKey       string
	generation       uint64
	objectID         uint32
	timestamp        uint64
	actionGeneration uint64
}

func (e campaignNPCManaDrainRequest) fail(
	step string, err error,
) ([][]byte, error) {
	if e.actionGeneration != 0 {
		e.runtime.releaseActionGeneration(e.sessionKey, e.generation, e.objectID, e.actionGeneration)
	} else {
		e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
	}
	return nil, fmt.Errorf("%s: %w", step, err)
}

func (e campaignNPCManaDrainRequest) failGeneration(step string, err error, actionGeneration uint64) ([][]byte, error) {
	e.runtime.releaseActionGeneration(e.sessionKey, e.generation, e.objectID, actionGeneration)
	return nil, fmt.Errorf("%s: %w", step, err)
}

func (e campaignNPCManaDrainRequest) resume(timestamp uint64) ([][]byte, error) {
	packets, err := e.runtime.produceManaDrainOwned(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp, e.actionGeneration,
	)
	if err != nil {
		return e.fail("enemyManaDrainResume", err)
	}
	return packets, nil
}

type campaignNPCManaDrainSchedule struct {
	request campaignNPCManaDrainRequest
	plan    zonenpc.AttackPlan
	run     *campaignNPCDrainRun
	tick    uint32
}

func (e campaignNPCManaDrainSchedule) fail(step string, err error) ([][]byte, error) {
	e.run.Stop()
	e.request.runtime.untrackHealthDrain(e.request.sessionKey, e.request.generation, e.request.objectID, e.run)
	e.request.runtime.settleDrainEffects()
	packets, failureErr := e.request.failGeneration(step, err, e.plan.ActionGeneration)
	if failureErr != nil {
		return packets, fmt.Errorf("manaChannel: %w", failureErr)
	}
	return packets, nil
}

type campaignNPCManaDrainNextStep struct {
	request          campaignNPCManaDrainRequest
	timestamp        uint64
	run              *campaignNPCDrainRun
	actionGeneration uint64
}

func (e campaignNPCManaDrainNextStep) produce() ([][]byte, error) {
	if e.run.IsStopped() {
		return nil, nil
	}
	runtime := e.request.runtime
	runtime.registry.mutex.RLock()
	current, isFound := runtime.registry.sessions[e.request.sessionKey]
	isCurrent := isFound && current.generation == e.request.generation &&
		current.campaignNPCDrainRuns[e.request.objectID] == e.run &&
		current.isCampaignNPCSourceGenerationActive(e.request.generation, e.request.objectID, e.actionGeneration)
	runtime.registry.mutex.RUnlock()
	if !isCurrent {
		e.run.Stop()
		runtime.untrackHealthDrain(e.request.sessionKey, e.request.generation, e.request.objectID, e.run)
		runtime.settleDrainEffects()
		return nil, nil
	}
	runtime.untrackHealthDrain(e.request.sessionKey, e.request.generation, e.request.objectID, e.run)
	if runtime.finishScenarioNPCManualAction(e.request.sessionKey,
		e.request.generation, e.request.objectID, e.actionGeneration) {
		return nil, nil
	}
	packets, err := e.request.resume(e.timestamp)
	if err != nil {
		return e.request.failGeneration("enemyManaDrainNext", err, e.actionGeneration)
	}
	return packets, nil
}

func (e campaignNPCManaDrainSchedule) end(
	timestamp uint64,
) ([][]byte, error) {
	if !e.run.End() {
		return nil, nil
	}
	packets, err := e.effectRemovals()
	if err != nil {
		return nil, fmt.Errorf("manaEndEffects: %w", err)
	}
	e.request.runtime.settleDrainEffects()
	packet, err := npcraknet.AnimationState(
		e.plan.SourceObjectID, e.plan.Profile.EndAnimationName, timestamp,
	)
	if err != nil {
		e.request.runtime.logger.Printf(
			"RakNet mana drain end presentation omitted object=%d: %v",
			e.request.objectID, err,
		)
		packet = nil
	}
	nextTimestamp := timestamp +
		uint64(e.plan.Profile.EndAnimationDelay/time.Millisecond)
	next := campaignNPCManaDrainNextStep{
		request: e.request, timestamp: nextTimestamp, run: e.run, actionGeneration: e.plan.ActionGeneration,
	}
	cancel, err := e.request.runtime.scheduleDrainProducers(e.request.packet, e.request.sessionKey, e.request.generation, e.request.objectID, e.plan.ActionGeneration, e.run,
		[]raknet.ScheduledPacketProducer{{
			Delay: e.plan.Profile.EndAnimationDelay, Produce: next.produce,
		}},
	)
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		return e.fail("enemyManaDrainNextSchedule", err)
	}
	if packet == nil {
		return packets, nil
	}
	return append(packets, packet), nil
}

func (e campaignNPCManaDrainSchedule) effectRemovals() ([][]byte, error) {
	e.run.ReleaseEffects()
	return nil, nil
}

func (e campaignNPCManaDrainSchedule) scheduleNext() error {
	next := e
	next.tick++
	cancel, err := e.request.runtime.scheduleDrainProducers(e.request.packet, e.request.sessionKey, e.request.generation, e.request.objectID, e.plan.ActionGeneration, e.run,
		[]raknet.ScheduledPacketProducer{{
			Delay: e.plan.Profile.TickDuration, Produce: next.produce,
		}},
	)
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		return fmt.Errorf("enemyManaDrainTickSchedule: %w", err)
	}
	return nil
}

func (e campaignNPCManaDrainSchedule) produce() ([][]byte, error) {
	if e.run.IsEnded() {
		return nil, nil
	}
	profile := e.plan.Profile
	timestamp := e.request.timestamp + uint64(profile.HitDelay/time.Millisecond) +
		uint64(e.tick)*uint64(profile.TickDuration/time.Millisecond)
	runtime := e.request.runtime
	runtime.registry.mutex.Lock()
	current, isFound := runtime.registry.sessions[e.request.sessionKey]
	isCurrentSession := isFound && current.generation == e.request.generation &&
		current.zone != nil && current.zone.NPCs() != nil
	if !isCurrentSession {
		runtime.registry.mutex.Unlock()
		e.run.End()
		e.run.ReleaseEffects()
		return nil, nil
	}
	source, isSourceFound := current.zone.NPCs().NPC(e.request.objectID)
	isOwned := isSourceFound && source.ActionGeneration == e.plan.ActionGeneration &&
		current.campaignNPCDrainRuns[e.request.objectID] == e.run
	if !isOwned {
		runtime.registry.mutex.Unlock()
		e.run.Stop()
		runtime.untrackHealthDrain(e.request.sessionKey, e.request.generation, e.request.objectID, e.run)
		runtime.settleDrainEffects()
		return nil, nil
	}
	target, isTargetFound := current.campaignNPCTarget(
		e.request.generation, e.plan.TargetObjectID,
	)
	isActive := isSourceFound && isTargetFound && target.IsHero &&
		current.isCampaignNPCAttackActiveAt(
			e.request.generation, e.request.objectID,
			e.plan.TargetObjectID, runtime.now(),
		) && zonegeometry.Distance(source.Plan.Position, target.Position) <=
		profile.MaximumChannelDistance
	if !isActive || target.ManaPoint <= 0 {
		runtime.registry.mutex.Unlock()
		return e.end(timestamp)
	}
	targetSession := &current
	targetSessionKey := e.request.sessionKey
	if target.UserID != current.binding.UserID ||
		target.PeerGeneration != current.generation ||
		target.ObjectID != current.deployedObjectID {
		targetSession = nil
		for sessionKey, candidate := range runtime.registry.sessions {
			if candidate.zone != current.zone ||
				candidate.binding.UserID != target.UserID ||
				candidate.generation != target.PeerGeneration ||
				candidate.deployedObjectID != target.ObjectID {
				continue
			}
			targetCopy := candidate
			targetSession = &targetCopy
			targetSessionKey = sessionKey
			break
		}
	}
	if targetSession == nil {
		runtime.registry.mutex.Unlock()
		return e.end(timestamp)
	}
	_, maximumManaPoint := targetSession.characterResourceMaximum(
		targetSession.deployedCreatureIndex,
	)
	drainedManaPoint := min(
		target.ManaPoint, maximumManaPoint*profile.ManaDrainFraction,
	)
	if drainedManaPoint <= 0 {
		runtime.registry.mutex.Unlock()
		return e.end(timestamp)
	}
	remainingManaPoint := target.ManaPoint - drainedManaPoint
	manaPacket, err := zoneabilityraknet.Mana(
		target.ObjectID, remainingManaPoint,
	)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyManaDrainManaMarshal: %w", err)
	}
	loopPacket, err := npcraknet.AnimationState(
		e.request.objectID, profile.LoopAnimationName, timestamp,
	)
	if err != nil {
		runtime.logger.Printf(
			"RakNet mana drain loop presentation omitted object=%d: %v",
			e.request.objectID, err,
		)
		loopPacket = nil
	}
	err = targetSession.setDeployedManaPoints(remainingManaPoint)
	if err != nil {
		runtime.registry.mutex.Unlock()
		e.run.End()
		e.run.ReleaseEffects()
		return e.fail("enemyManaDrainCommit", err)
	}
	updatedSource, err := current.zone.NPCs().IncreaseMana(
		e.request.objectID, 1,
	)
	if err != nil {
		runtime.registry.mutex.Unlock()
		e.run.End()
		e.run.ReleaseEffects()
		return e.fail("enemyManaDrainSourceMana", err)
	}
	sourceManaPacket, err := zoneabilityraknet.Mana(
		e.request.objectID, updatedSource.ManaPoint,
	)
	if err != nil {
		runtime.logger.Printf(
			"RakNet mana drain source resource presentation omitted object=%d: %v",
			e.request.objectID, err,
		)
		sourceManaPacket = nil
	}
	isOverloaded := updatedSource.ManaPoint >= 5
	damageResult := zonenpc.DamageResult{}
	transition := campaignDamageTransition{}
	if isOverloaded {
		damageResult, err = current.zone.NPCs().Defeat(
			e.request.objectID, e.request.objectID,
		)
		if err == nil {
			transition, err = current.applyCampaignNPCSelfDamageTransition(
				damageResult,
			)
		}
		if err != nil {
			runtime.registry.mutex.Unlock()
			e.run.End()
			e.run.ReleaseEffects()
			return e.fail("enemyManaDrainOverload", err)
		}
	}
	if targetSessionKey == e.request.sessionKey {
		current = *targetSession
	} else {
		runtime.registry.sessions[targetSessionKey] = *targetSession
	}
	runtime.registry.sessions[e.request.sessionKey] = current
	runtime.registry.mutex.Unlock()
	packets := [][]byte{manaPacket}
	if sourceManaPacket != nil {
		packets = append(packets, sourceManaPacket)
	}
	if loopPacket != nil {
		packets = append(packets, loopPacket)
	}
	if isOverloaded {
		e.run.End()
		removalPackets, removalErr := e.effectRemovals()
		if removalErr != nil && !zonenpc.IsDrainerDeath(updatedSource) {
			return e.fail("enemyManaDrainOverloadEffect", removalErr)
		}
		if zonenpc.IsDrainerDeath(updatedSource) {
			packets = append(packets, removalPackets...)
			deathPackets, deathErr := runtime.publishDrainerDeath(
				e.request.packet, drainerDeathOwner{world: current.zone, gameID: current.binding.GameID,
					userID: current.binding.UserID, generation: e.request.generation, sessionKey: e.request.sessionKey},
				updatedSource, damageResult, transition, timestamp, packets, removalErr,
			)
			runtime.settleDrainEffects()
			if deathErr != nil {
				return e.fail("enemyDrainerDeath", deathErr)
			}
			return deathPackets, nil
		}
		deathPackets, deathErr := runtime.publishNPCSelfDeath(
			e.request.packet, e.request.sessionKey, e.request.generation,
			updatedSource, damageResult, transition, timestamp,
		)
		runtime.settleDrainEffects()
		if deathErr != nil {
			return e.fail("enemyManaDrainOverloadPublish", deathErr)
		}
		packets = append(packets, removalPackets...)
		return append(packets, deathPackets...), nil
	}
	isLast := remainingManaPoint <= 0 || e.tick+1 >= profile.NumberOfTicks
	if isLast {
		endPackets, endErr := e.end(timestamp)
		if endErr != nil {
			return e.fail("enemyManaDrainFinish", endErr)
		}
		return append(packets, endPackets...), nil
	}
	err = e.scheduleNext()
	if err != nil {
		e.run.End()
		e.run.ReleaseEffects()
		return e.fail("enemyManaDrainContinue", err)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) publishNPCSelfDeath(
	packet raknet.Packet, sessionKey string, generation uint64,
	source zonenpc.Snapshot, result zonenpc.DamageResult,
	transition campaignDamageTransition, timestamp uint64,
) ([][]byte, error) {
	physics := r.program.NPCDeathPhysics(source.Plan.NounName)
	definition, err := campaignNPCDeathDefinition(source, physics)
	if err != nil {
		return nil, fmt.Errorf("npcSelfDeathDefinition: %w", err)
	}
	publication, err := marshalZoneNPCDamage(
		definition,
		zoneNPCDamageResult{
			hitPoint: result.HitPoint, isDefeated: true, isFound: true,
		},
		source.Plan.ObjectID, result.Damage, false, timestamp, r.effectPool,
	)
	if err != nil {
		return nil, fmt.Errorf("npcSelfDeathDamage: %w", err)
	}
	err = r.projection.recordEnemyDeathObjectives(
		sessionKey, generation, publication.deathRun.DrainProjection(),
	)
	if err != nil {
		publication.deathRun.Stop()
		return nil, fmt.Errorf("npcSelfDeathProjection: %w", err)
	}
	err = r.scheduleEnemyDeath(
		packet, sessionKey, generation, source.Plan.ObjectID,
		publication.deathRun,
	)
	if err != nil {
		publication.deathRun.Stop()
		return nil, fmt.Errorf("npcSelfDeathSchedule: %w", err)
	}
	defeatedSource := source
	defeatedSource.IsDefeated = true
	defeatedSource.HitPoint = 0
	lootPackets, err := r.spawnLoot(
		sessionKey, generation, defeatedSource, timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("npcSelfDeathLoot: %w", err)
	}
	damageRuntime := campaignDamageRuntime{
		registry: r.registry, npc: r, projection: r.projection,
		effectPool: r.effectPool, gameplayJoin: r.gameplayJoin, logger: r.logger,
	}
	transitionPackets, err := damageRuntime.publishTransition(
		packet, sessionKey, generation, transition, timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("npcSelfDeathTransition: %w", err)
	}
	packets := append([][]byte(nil), publication.packets...)
	packets = append(packets, lootPackets...)
	return append(packets, transitionPackets...), nil
}

func (r campaignNPCActionRuntime) produceChannelDrain(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	profile, isProfileFound := zonenpc.ActionProfileForPlan(enemy.Plan)
	r.registry.mutex.RUnlock()
	if !isEnemyFound || !isProfileFound {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	if profile.AbilityName == "ManaDrain" {
		return r.produceManaDrain(
			packet, sessionKey, generation, objectID, timestamp,
		)
	}
	return r.produceHealthDrain(
		packet, sessionKey, generation, objectID, timestamp,
	)
}

func (r campaignNPCActionRuntime) produceManaDrain(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	packets, err := r.produceManaDrainOwned(packet, sessionKey, generation, objectID, timestamp, 0)
	if err != nil {
		return packets, fmt.Errorf("manaProduce: %w", err)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) produceManaDrainOwned(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64, actionGeneration uint64,
) ([][]byte, error) {
	request := campaignNPCManaDrainRequest{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp, actionGeneration: actionGeneration,
	}
	if actionGeneration != 0 {
		r.registry.mutex.RLock()
		member, isFound := r.registry.sessions[sessionKey]
		isCurrent := isFound && member.isCampaignNPCSourceGenerationActive(generation, objectID, actionGeneration)
		r.registry.mutex.RUnlock()
		if !isCurrent {
			return nil, nil
		}
	}
	isDeferred, err := r.pursuit.deferAction(
		packet, sessionKey, generation, objectID, timestamp, request.resume,
	)
	if err != nil {
		return request.fail("enemyManaDrainStun", err)
	}
	if isDeferred {
		return nil, nil
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound &&
		peerSession.isCampaignNPCSourceActive(generation, objectID) &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	if actionGeneration != 0 && (!isEnemyFound || enemy.ActionGeneration != actionGeneration) {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	request.actionGeneration = enemy.ActionGeneration
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, enemy.TargetObjectID,
	)
	profile, isProfileFound := zonenpc.ActionProfileForPlan(enemy.Plan)
	r.registry.mutex.RUnlock()
	if !isEnemyFound || enemy.IsDefeated || !isTargetFound {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	if !target.IsHero || target.ManaPoint <= 0 {
		return r.produceEnemyMelee(
			packet, sessionKey, generation, objectID, timestamp,
		)
	}
	if !isProfileFound || profile.AbilityName != "ManaDrain" ||
		profile.Family != zonenpc.ActionChannelDrain ||
		profile.ManaDrainFraction <= 0 || profile.MaximumChannelDistance <= 0 ||
		profile.NumberOfTicks == 0 || profile.TickDuration <= 0 ||
		profile.EndAnimationDelay <= 0 || profile.LoopAnimationName == "" ||
		profile.EndAnimationName == "" || profile.TrailEffectName == "" ||
		profile.TargetEffectName == "" {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, errors.New("enemy mana drain profile unavailable")
	}
	plan, err := zonenpc.PlanControlWithProfile(
		enemy, target.ObjectID, target.Position, profile,
		target.ActorFootprintRadius,
	)
	if err != nil {
		action, actionErr := campaignNPCActionWithProfile(
			enemy.Plan, target.ObjectID, target.Position,
			profile, target.ActorFootprintRadius,
		)
		if actionErr != nil {
			return request.fail("enemyManaDrainPursuitAction", actionErr)
		}
		if !action.IsPursuitNeeded {
			r.releaseAction(sessionKey, generation, objectID)
			return nil, nil
		}
		pursuitPackets, marshalErr := npcraknet.Pursuit(action)
		if marshalErr != nil {
			return request.fail("enemyManaDrainPursuitMarshal", marshalErr)
		}
		scheduleErr := r.pursuit.schedule(
			packet, sessionKey, generation, objectID, timestamp,
			action.TargetPosition, profile, request.resume,
		)
		if scheduleErr != nil {
			return request.fail("enemyManaDrainPursuitSchedule", scheduleErr)
		}
		return pursuitPackets, nil
	}
	startPackets, err := r.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		return request.failGeneration("enemyManaDrainStart", err, plan.ActionGeneration)
	}
	if r.effectPool == nil {
		return request.failGeneration("enemyManaDrainEffectPool", errors.New("effect pool unavailable"), plan.ActionGeneration)
	}
	effects, err := r.allocateDrainEffects(sessionKey, generation, []uint32{objectID, target.ObjectID})
	if err != nil {
		return request.failGeneration("manaEffects", err, plan.ActionGeneration)
	}
	slots := effects.Slots()
	sourceSlot, targetSlot := slots[0].slot, slots[1].slot
	effectPackets, err := zoneabilityraknet.ChannelDrainAttachedEffects(
		profile.TrailEffectName, profile.TargetEffectName,
		objectID, sourceSlot, target.ObjectID, targetSlot,
	)
	if err != nil {
		effects.Retire()
		r.settleDrainEffects()
		return request.failGeneration("enemyManaDrainEffect", err, plan.ActionGeneration)
	}
	run := &campaignNPCDrainRun{}
	run.bindEffects(effects, plan.ActionGeneration)
	if !r.trackHealthDrain(sessionKey, generation, objectID, run) {
		run.End()
		run.ReleaseEffects()
		r.settleDrainEffects()
		return request.failGeneration("manaTrack", errors.New("channel run unavailable"), plan.ActionGeneration)
	}
	schedule := campaignNPCManaDrainSchedule{
		request: request, plan: plan, run: run,
	}
	cancel, err := r.scheduleDrainProducers(packet, sessionKey, generation, objectID, plan.ActionGeneration, run, []raknet.ScheduledPacketProducer{{
		Delay: profile.HitDelay, Produce: schedule.produce,
	}})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		run.End()
		run.ReleaseEffects()
		r.untrackHealthDrain(sessionKey, generation, objectID, run)
		r.settleDrainEffects()
		return request.failGeneration("enemyManaDrainSchedule", err, plan.ActionGeneration)
	}
	err = r.publishDrainEffects(sessionKey, generation, objectID, run, effectPackets)
	if err != nil {
		cancel()
		run.End()
		run.ReleaseEffects()
		r.untrackHealthDrain(sessionKey, generation, objectID, run)
		r.settleDrainEffects()
		return request.failGeneration("manaPublish", err, plan.ActionGeneration)
	}
	return startPackets, nil
}
