package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zone "github.com/darkspinnet/darkspin/server/zone"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const (
	stealtherStealthModifierID uint32 = 0x29296981
	stealtherFearModifierID    uint32 = 0xbacab933
	stealtherFearHitDelay             = 300 * time.Millisecond
)

type campaignStealtherSchedule struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
	plan       zonenpc.AttackPlan
	modifier   *campaignNPCModifierRun
}

type campaignNPCFearRun struct {
	modifier       *campaignNPCModifierRun
	sourceObjectID uint32
	targetObjectID uint32
	expiresAt      time.Time
	revision       uint64
	cancel         raknet.CancelSchedule
}

type campaignNPCFearExpiryStep struct {
	runtime    campaignNPCActionRuntime
	sessionKey string
	generation uint64
	revision   uint64
	run        *campaignNPCFearRun
}

func (r campaignNPCActionRuntime) startStealtherStealth(
	packet raknet.Packet, sessionKey string, generation uint64,
	enemy zonenpc.Snapshot, target zone.NPCTarget,
	profile zonenpc.ActionProfile, timestamp uint64,
) ([][]byte, error) {
	if profile.AbilityName != "StealthAttack" ||
		r.registry == nil || r.now == nil {
		return nil, errors.New("enemy stealth runtime unavailable")
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCAttackActiveAt(
		generation, enemy.Plan.ObjectID, target.ObjectID, r.now(),
	)
	if !isCurrent || peerSession.zone.NPCRandom() == nil {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	destination, isDestinationFound, err :=
		zonenavigation.RandomTeleportDestination(
			peerSession.zone.Navigation(), peerSession.zone.NPCRandom(),
			zonenavigation.RandomTeleportRequest{
				SourcePosition:  target.Position,
				FootprintRadius: enemy.Plan.NPCProfile.FootprintRadius,
				MinimumDistance: profile.MinimumRange,
				NormalDistance:  7.5,
				MaximumDistance: profile.Range - 5,
			},
		)
	r.registry.mutex.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("enemyStealthDestination: %w", err)
	}
	if !isDestinationFound {
		return nil, nil
	}
	moveProfile := profile
	moveProfile.Range = 0.1
	moveProfile.MinimumRange = 0
	action := zonenpc.FirstActionPlan{
		ObjectID: enemy.Plan.ObjectID, TargetObjectID: target.ObjectID,
		SourcePosition: enemy.Plan.Position, TargetPosition: destination,
		Profile: moveProfile, IsPursuitNeeded: enemy.Plan.Position != destination,
	}
	schedule := campaignStealtherSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: enemy.Plan.ObjectID,
		timestamp: timestamp,
		plan:      zonenpc.AttackPlan{ActionGeneration: enemy.ActionGeneration, Profile: profile},
	}
	if !action.IsPursuitNeeded {
		return schedule.cast(timestamp)
	}
	packets, err := npcraknet.Pursuit(action)
	if err != nil {
		return nil, fmt.Errorf("enemyStealthMoveMarshal: %w", err)
	}
	resetPacket, err := npcraknet.ResetAnimation(enemy.Plan.ObjectID, timestamp)
	if err != nil {
		return nil, fmt.Errorf("stealthTravelReset: %w", err)
	}
	packets = append([][]byte{resetPacket}, packets...)
	stealthPackets, err := schedule.beginTravelStealth(profile)
	if err != nil {
		return nil, fmt.Errorf("stealthTravel: %w", err)
	}
	err = r.pursuit.schedule(
		packet, sessionKey, generation, enemy.Plan.ObjectID, timestamp,
		destination, moveProfile, schedule.cast,
	)
	if err != nil {
		schedule.rollbackStealth(schedule.modifier)
		return nil, fmt.Errorf("enemyStealthMoveSchedule: %w", err)
	}
	return append(stealthPackets, packets...), nil
}

func (e *campaignStealtherSchedule) beginTravelStealth(profile zonenpc.ActionProfile) ([][]byte, error) {
	modifier, err := newCampaignNPCModifierRun(e.runtime.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("travelModifier: %w", err)
	}
	e.modifier = modifier
	e.plan.Profile = profile
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || !peerSession.isCampaignNPCSourceActive(e.generation, e.objectID) {
		e.runtime.registry.mutex.Unlock()
		isCreated, releaseErr := modifier.release(e.runtime.modifierPool)
		return nil, fmt.Errorf("travelSource: %w", errors.Join(
			fmt.Errorf("stealth source unavailable (created=%t)", isCreated), releaseErr))
	}
	err = peerSession.trackCampaignNPCModifier(modifier)
	if err == nil {
		err = peerSession.zone.NPCs().ApplyStealth(e.objectID)
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	if err != nil {
		e.rollbackStealth(modifier)
		return nil, fmt.Errorf("travelState: %w", err)
	}
	stealthPacket, err := npcraknet.StealthState(e.objectID, profile.StealthType)
	if err != nil {
		e.rollbackStealth(modifier)
		return nil, fmt.Errorf("travelPresentation: %w", err)
	}
	modifierPacket, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID: e.objectID, ModifierGUID: stealtherStealthModifierID,
		InstanceID: modifier.instanceID, StackCount: 1,
		StartMilliseconds: e.timestamp, SourceID: e.objectID,
	})
	if err != nil {
		e.rollbackStealth(modifier)
		return nil, fmt.Errorf("travelCreate: %w", err)
	}
	cancel, err := e.packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: profile.Cooldown, Produce: e.expireTravelStealth,
	}})
	if err != nil || cancel == nil {
		e.rollbackStealth(modifier)
		return nil, fmt.Errorf("travelExpiry: %w", errors.Join(err, errors.New("stealth expiry unavailable")))
	}
	modifier.create()
	return [][]byte{stealthPacket, modifierPacket}, nil
}

func (e campaignStealtherSchedule) expireTravelStealth() ([][]byte, error) {
	return e.finishStealth(false, e.timestamp+uint64(e.plan.Profile.Cooldown/time.Millisecond)), nil
}

func (e campaignStealtherSchedule) cast(timestamp uint64) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		e.generation, e.objectID,
	)
	if e.modifier != nil && peerSession.campaignNPCModifiers[e.modifier.instanceID] != e.modifier {
		isCurrent = false
	}
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(e.objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		e.generation, enemy.TargetObjectID,
	)
	profile, isProfileFound := zonenpc.StealtherStealthProfile(enemy.Plan.NounName)
	if !isEnemyFound || !isTargetFound || !isProfileFound {
		e.runtime.registry.mutex.Unlock()
		e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
		return e.finishStealth(true, timestamp), nil
	}
	plan, err := zonenpc.PlanAttackWithProfile(
		enemy, target.ObjectID, target.Position, profile, target.FootprintRadius,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
		return e.finishStealth(true, timestamp), nil
	}
	isTravelStealth := e.modifier != nil
	modifier := e.modifier
	if modifier == nil {
		modifier, err = newCampaignNPCModifierRun(e.runtime.modifierPool)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemyStealthModifierRun: %w", err)
		}
		err = peerSession.zone.NPCs().ApplyStealth(e.objectID)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			_, releaseErr := modifier.release(e.runtime.modifierPool)
			return nil, fmt.Errorf(
				"enemyStealthState: %w", errors.Join(err, releaseErr),
			)
		}
		err = peerSession.trackCampaignNPCModifier(modifier)
		if err != nil {
			peerSession.zone.NPCs().ClearStealth(e.objectID)
			e.runtime.registry.mutex.Unlock()
			_, releaseErr := modifier.release(e.runtime.modifierPool)
			return nil, fmt.Errorf(
				"enemyStealthModifierTrack: %w", errors.Join(err, releaseErr),
			)
		}
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()

	animationPackets, err := e.runtime.startNPCAttack(e.sessionKey, e.generation, plan, timestamp)
	if err != nil {
		e.rollbackStealth(modifier)
		return nil, fmt.Errorf("enemyStealthAnimation: %w", err)
	}
	stealthPacket, err := npcraknet.StealthState(
		e.objectID, profile.StealthType,
	)
	if err != nil {
		e.rollbackStealth(modifier)
		return nil, fmt.Errorf("enemyStealthPresentation: %w", err)
	}
	modifierPacket, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID: e.objectID, ModifierGUID: stealtherStealthModifierID,
		InstanceID: modifier.instanceID, DurationMilliseconds: 0,
		StackCount: 1, StartMilliseconds: timestamp, SourceID: e.objectID,
	})
	if err != nil {
		e.rollbackStealth(modifier)
		return nil, fmt.Errorf("enemyStealthModifierCreate: %w", err)
	}
	e.plan = plan
	e.modifier = modifier
	e.timestamp = timestamp
	cancel, scheduleErr := e.packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: profile.HitDelay, Produce: e.hit,
	}, {
		Delay: profile.ReleaseDelay, Produce: e.resetAnimation,
	}})
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		e.rollbackStealth(modifier)
		return nil, fmt.Errorf("enemyStealthHitSchedule: %w", scheduleErr)
	}
	modifier.create()
	if isTravelStealth {
		return animationPackets, nil
	}
	packets := append([][]byte{stealthPacket}, animationPackets...)
	return append(packets, modifierPacket), nil
}

func (e campaignStealtherSchedule) rollbackStealth(
	modifier *campaignNPCModifierRun,
) {
	e.modifier = modifier
	packets := e.finishStealth(true, e.timestamp)
	isCreated, releaseErr := modifier.release(e.runtime.modifierPool)
	if releaseErr != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("Stealth rollback release object=%d created=%t: %v", e.objectID, isCreated, releaseErr)
	}
	if len(packets) == 0 {
		return
	}
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation
	if isCurrent {
		publishErr := peerSession.publishPackets(packets)
		if publishErr != nil && e.runtime.logger != nil {
			e.runtime.logger.Printf("Stealth rollback presentation queued object=%d: %v", e.objectID, publishErr)
		}
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
}

func (e campaignStealtherSchedule) hit() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isOwned := isFound && peerSession.generation == e.generation &&
		peerSession.campaignNPCModifiers[e.modifier.instanceID] == e.modifier
	if !isOwned {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	isAttackActive := peerSession.isCampaignNPCAttackActiveAt(
		e.generation, e.objectID, e.plan.TargetObjectID, e.runtime.now(),
	)
	if !isAttackActive {
		e.runtime.registry.mutex.Unlock()
		return e.finishStealth(false, e.timestamp), nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(e.objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		e.generation, e.plan.TargetObjectID,
	)
	if !isEnemyFound || !isTargetFound || peerSession.zone.NPCRandom() == nil {
		e.runtime.registry.mutex.Unlock()
		return e.finishStealth(false, e.timestamp), nil
	}
	plan, err := zonenpc.PlanAttackWithProfile(
		enemy, target.ObjectID, target.Position, e.plan.Profile,
		target.FootprintRadius,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return e.finishStealth(false, e.timestamp), nil
	}
	result, err := zonenpc.CommitAttack(
		peerSession.zone.NPCRandom(), plan,
		enemy.Plan.NPCProfile.CriticalRating, e.runtime.program.Critical,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyStealthCommit: %w", err)
	}
	hitTimestamp := e.timestamp + uint64(e.plan.Profile.HitDelay/time.Millisecond)
	packets, statDelta, reflection, isApplied, err :=
		e.runtime.applyEnemyAttackDamage(
			&peerSession, e.generation, plan, result, hitTimestamp,
		)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("enemyStealthDamage: %w", err)
	}
	isTargetAlive := false
	if isApplied {
		latestTarget, isLatestFound := peerSession.campaignNPCTarget(
			e.generation, plan.TargetObjectID,
		)
		isTargetAlive = isLatestFound && latestTarget.HitPoint > 0
	}
	binding := peerSession.binding
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	reflectionPackets, reflectionErr := e.runtime.publishThornBarkReflection(
		e.packet, e.sessionKey, e.generation, hitTimestamp, binding, reflection,
	)
	if reflectionErr != nil {
		return nil, fmt.Errorf("enemyStealthThornBark: %w", reflectionErr)
	}
	packets = append(packets, reflectionPackets...)
	if isApplied {
		err = e.runtime.stats.Record(context.Background(), binding, statDelta)
		if err != nil {
			return nil, fmt.Errorf("enemyStealthStats: %w", err)
		}
	}
	if !isTargetAlive {
		finishPackets := e.finishStealth(false, hitTimestamp)
		return append(packets, finishPackets...), nil
	}
	fearPlan := plan
	fearPlan.Profile.AnimationName = e.plan.Profile.EndAnimationName
	fearPackets, err := e.runtime.startNPCAttack(e.sessionKey, e.generation, fearPlan, hitTimestamp)
	if err != nil {
		return nil, fmt.Errorf("enemyFearAnimation: %w", err)
	}
	cancel, scheduleErr := e.packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: stealtherFearHitDelay, Produce: e.fear,
	}})
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		finishPackets := e.finishStealth(false, hitTimestamp)
		return append(packets, finishPackets...),
			fmt.Errorf("enemyFearSchedule: %w", scheduleErr)
	}
	return append(packets, fearPackets...), nil
}

func (e campaignStealtherSchedule) finishStealth(
	isNextScheduled bool, timestamp uint64,
) [][]byte {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		e.modifier != nil &&
		peerSession.campaignNPCModifiers[e.modifier.instanceID] == e.modifier
	if isCurrent {
		peerSession.zone.NPCs().ClearStealth(e.objectID)
		peerSession.untrackCampaignNPCModifier(e.modifier)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil
	}
	stealthPacket, stealthErr := npcraknet.StealthState(e.objectID, 0)
	isCreated, err := e.modifier.release(e.runtime.modifierPool)
	if err != nil || !isCreated {
		if !isNextScheduled {
			e.scheduleNext(timestamp)
		}
		if stealthErr != nil {
			return nil
		}
		return [][]byte{stealthPacket}
	}
	packet, err := raknet.MarshalApplication(raknet.ModifierDeletedMessage{
		TargetID: e.objectID, InstanceID: e.modifier.instanceID,
	})
	if err != nil {
		if stealthErr != nil {
			return nil
		}
		return [][]byte{stealthPacket}
	}
	if !isNextScheduled {
		e.scheduleNext(timestamp)
	}
	if stealthErr != nil {
		return [][]byte{packet}
	}
	return [][]byte{packet, stealthPacket}
}

func (e campaignStealtherSchedule) scheduleNext(timestamp uint64) {
	// Travel and its expiry must not consume the visible recovery window.
	// Start the authored cooldown when stealth ends, including interrupted travel.
	delay := e.plan.Profile.Cooldown
	next := campaignNPCFirstActionStep{
		runtime: e.runtime, packet: e.packet, sessionKey: e.sessionKey,
		generation: e.generation, objectID: e.objectID,
		actionGeneration: e.plan.ActionGeneration,
		timestamp:        timestamp + uint64(delay/time.Millisecond),
	}
	cancel, err := e.packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: delay, Produce: next.produce,
	}})
	if err == nil && cancel == nil {
		err = errors.New("stealth recovery cancellation unavailable")
	}
	if err != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("Stealth recovery not scheduled object=%d: %v", e.objectID, err)
	}
}

func (e campaignStealtherSchedule) fear() ([][]byte, error) {
	fearTimestamp := e.timestamp +
		uint64((e.plan.Profile.HitDelay+stealtherFearHitDelay)/time.Millisecond)
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		e.generation, e.objectID,
	)
	if !isCurrent {
		e.runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(e.objectID)
	targets := append([]zone.NPCTarget(nil), peerSession.zone.LiveNPCTargets()...)
	e.runtime.registry.mutex.RUnlock()
	packets := e.finishStealth(true, fearTimestamp)
	if !isSourceFound {
		e.scheduleNext(fearTimestamp)
		return packets, nil
	}
	for _, candidate := range targets {
		if !isCampaignLeapTarget(
			source.Plan.Position, candidate.Position,
			candidate.FootprintRadius, e.plan.Profile.Radius,
		) {
			continue
		}
		fearPackets, err := e.runtime.applyCampaignNPCFear(
			e.packet, e.sessionKey, e.generation, source,
			candidate, e.plan.Profile, fearTimestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("enemyFearApply: %w", err)
		}
		packets = append(packets, fearPackets...)
	}
	e.scheduleNext(fearTimestamp)
	return packets, nil
}

func (r campaignNPCActionRuntime) applyCampaignNPCFear(
	packet raknet.Packet, sessionKey string, generation uint64,
	source zonenpc.Snapshot, target zone.NPCTarget,
	profile zonenpc.ActionProfile, timestamp uint64,
) ([][]byte, error) {
	isFear := profile.ModifierName == "FearNova" ||
		profile.ModifierName == "NocturnaSpecialHomerFear" ||
		profile.ModifierName == "Modifier_ScaldronBoss_ShadowPanic"
	if !isFear || profile.ModifierDuration <= 0 || r.now == nil ||
		source.Plan.ObjectID == 0 || target.ObjectID == 0 {
		return nil, errors.New("enemy fear request invalid")
	}
	var interruptedBasic *abilityraknet.MeleeRun
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	latestTarget, isTargetFound := peerSession.campaignNPCTarget(
		generation, target.ObjectID,
	)
	if !isTargetFound || latestTarget.HitPoint <= 0 {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	if peerSession.isHeroDebuffImmune(target.ObjectID) {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	if peerSession.campaignNPCFears == nil {
		peerSession.campaignNPCFears = make(map[uint32]*campaignNPCFearRun)
	}
	previous := peerSession.campaignNPCFears[target.ObjectID]
	isNew := previous == nil
	run := previous
	if isNew {
		modifier, err := newCampaignNPCModifierRun(r.modifierPool)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("enemyFearRun: %w", err)
		}
		run = &campaignNPCFearRun{
			modifier: modifier, sourceObjectID: source.Plan.ObjectID,
			targetObjectID: target.ObjectID,
		}
	}
	previousSourceObjectID := run.sourceObjectID
	previousExpiresAt := run.expiresAt
	previousRevision := run.revision
	previousCancel := run.cancel
	previousHeroExpiresAt := peerSession.enemyFearExpiresAt
	previousHeroTargetObjectID := peerSession.enemyFearTargetObjectID
	run.sourceObjectID = source.Plan.ObjectID
	run.expiresAt = r.now().Add(profile.ModifierDuration)
	run.revision++
	revision := run.revision
	peerSession.campaignNPCFears[target.ObjectID] = run
	isHeroFeared := peerSession.extendEnemyFear(target.ObjectID, run.expiresAt)
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	modifierGUID := stealtherFearModifierID
	if profile.ModifierName != "FearNova" {
		modifierGUID = util.HashID(profile.ModifierName)
	}
	modifierPacket, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID: target.ObjectID, ModifierGUID: modifierGUID,
		InstanceID:           run.modifier.instanceID,
		DurationMilliseconds: uint32(profile.ModifierDuration.Milliseconds()),
		StackCount:           1, StartMilliseconds: timestamp,
		SourceID: source.Plan.ObjectID,
	})
	if err != nil {
		r.rollbackCampaignNPCFear(
			sessionKey, generation, run, isNew,
			previousSourceObjectID, previousExpiresAt,
			previousRevision, revision, previousCancel,
			previousHeroExpiresAt, previousHeroTargetObjectID,
		)
		return nil, fmt.Errorf("enemyFearCreate: %w", err)
	}
	expiry := campaignNPCFearExpiryStep{
		runtime: r, sessionKey: sessionKey, generation: generation,
		revision: revision, run: run,
	}
	cancel, scheduleErr := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: profile.ModifierDuration, Produce: expiry.produce,
	}})
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		r.rollbackCampaignNPCFear(
			sessionKey, generation, run, isNew,
			previousSourceObjectID, previousExpiresAt,
			previousRevision, revision, previousCancel,
			previousHeroExpiresAt, previousHeroTargetObjectID,
		)
		return nil, fmt.Errorf("enemyFearSchedule: %w", scheduleErr)
	}
	r.registry.mutex.Lock()
	latest, isLatestFound := r.registry.sessions[sessionKey]
	isLatest := isLatestFound && latest.generation == generation &&
		latest.campaignNPCFears[target.ObjectID] == run &&
		run.revision == revision
	if isLatest && isHeroFeared {
		interruptedBasic = latest.resetInterruptibleActionAdmission()
	}
	if isLatest {
		run.cancel = cancel
		r.registry.sessions[sessionKey] = latest
	}
	r.registry.mutex.Unlock()
	if interruptedBasic != nil {
		interruptedBasic.Stop()
	}
	if !isLatest {
		cancel()
		return nil, nil
	}
	if previousCancel != nil {
		previousCancel()
	}
	if isNew {
		run.modifier.create()
	}
	packets := [][]byte{modifierPacket}
	movementPacket, err := r.startCampaignFearMovement(
		sessionKey, generation, target,
	)
	if err != nil {
		r.logger.Printf(
			"RakNet campaign fear movement skipped target=%d: %v",
			target.ObjectID, err,
		)
		return packets, nil
	}
	if movementPacket != nil {
		packets = append(packets, movementPacket)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) rollbackCampaignNPCFear(
	sessionKey string, generation uint64,
	run *campaignNPCFearRun, isNew bool,
	previousSourceObjectID uint32,
	previousExpiresAt time.Time, previousRevision uint64, revision uint64,
	previousCancel raknet.CancelSchedule,
	previousHeroExpiresAt time.Time, previousHeroTargetObjectID uint32,
) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.campaignNPCFears[run.targetObjectID] == run &&
		run.revision == revision
	if isCurrent {
		if isNew {
			delete(peerSession.campaignNPCFears, run.targetObjectID)
		} else {
			run.sourceObjectID = previousSourceObjectID
			run.expiresAt = previousExpiresAt
			run.revision = previousRevision
			run.cancel = previousCancel
		}
		peerSession.enemyFearExpiresAt = previousHeroExpiresAt
		peerSession.enemyFearTargetObjectID = previousHeroTargetObjectID
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
	if isNew && isCurrent {
		_, _ = run.modifier.release(r.modifierPool)
	}
}

func (r campaignNPCActionRuntime) stopCampaignNPCFearSource(
	sessionKey string, generation uint64, sourceObjectID uint32,
) ([][]byte, error) {
	if r.registry == nil || r.modifierPool == nil || sourceObjectID == 0 {
		return nil, nil
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	runs := make([]*campaignNPCFearRun, 0, len(peerSession.campaignNPCFears))
	for targetObjectID, run := range peerSession.campaignNPCFears {
		if run == nil || run.sourceObjectID != sourceObjectID {
			continue
		}
		if run.cancel != nil {
			run.cancel()
			run.cancel = nil
		}
		delete(peerSession.campaignNPCFears, targetObjectID)
		if peerSession.enemyFearTargetObjectID == targetObjectID &&
			peerSession.enemyFearExpiresAt == run.expiresAt {
			peerSession.enemyFearExpiresAt = time.Time{}
			peerSession.enemyFearTargetObjectID = 0
		}
		if peerSession.deployedObjectID == targetObjectID {
			err := peerSession.stopPlayerMovement(r.now())
			if err != nil && r.logger != nil {
				r.logger.Printf(
					"RakNet fear source-death movement cleanup omitted target=%d: %v",
					targetObjectID, err,
				)
			}
			err = peerSession.syncZoneHeroPose()
			if err != nil && r.logger != nil {
				r.logger.Printf(
					"RakNet fear source-death pose cleanup omitted target=%d: %v",
					targetObjectID, err,
				)
			}
		}
		runs = append(runs, run)
	}
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	packets := make([][]byte, 0, len(runs))
	for index, run := range runs {
		isCreated, err := run.modifier.release(r.modifierPool)
		if err != nil {
			return nil, fmt.Errorf("fearSourceRelease[%d]: %w", index, err)
		}
		if !isCreated {
			continue
		}
		packet, err := raknet.MarshalApplication(raknet.ModifierDeletedMessage{
			TargetID: run.targetObjectID, InstanceID: run.modifier.instanceID,
		})
		if err != nil {
			return nil, fmt.Errorf("fearSourceDelete[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e campaignNPCFearExpiryStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.campaignNPCFears[e.run.targetObjectID] == e.run &&
		e.run.revision == e.revision
	if isCurrent {
		delete(peerSession.campaignNPCFears, e.run.targetObjectID)
		if peerSession.enemyFearTargetObjectID == e.run.targetObjectID &&
			peerSession.enemyFearExpiresAt == e.run.expiresAt {
			peerSession.enemyFearExpiresAt = time.Time{}
			peerSession.enemyFearTargetObjectID = 0
		}
		if peerSession.deployedObjectID == e.run.targetObjectID {
			_ = peerSession.stopPlayerMovement(e.runtime.now())
			_ = peerSession.syncZoneHeroPose()
		}
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	isCreated, err := e.run.modifier.release(e.runtime.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("enemyFearRelease: %w", err)
	}
	if !isCreated {
		return nil, nil
	}
	modifierPacket, err := raknet.MarshalApplication(raknet.ModifierDeletedMessage{
		TargetID: e.run.targetObjectID, InstanceID: e.run.modifier.instanceID,
	})
	if err != nil {
		return nil, fmt.Errorf("enemyFearDelete: %w", err)
	}
	return [][]byte{modifierPacket}, nil
}

func (r campaignNPCActionRuntime) startCampaignFearMovement(
	sessionKey string, generation uint64, target zone.NPCTarget,
) ([]byte, error) {
	r.registry.mutex.Lock()
	defer r.registry.mutex.Unlock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isLocal := isFound && peerSession.generation == generation &&
		peerSession.zone != nil && target.IsHero &&
		target.UserID == peerSession.binding.UserID &&
		peerSession.deployedObjectID == target.ObjectID &&
		peerSession.zone.NPCRandom() != nil
	if !isLocal {
		return nil, nil
	}
	destination, isDestinationFound, err :=
		zonenavigation.RandomTeleportDestination(
			peerSession.zone.Navigation(), peerSession.zone.NPCRandom(),
			zonenavigation.RandomTeleportRequest{
				SourcePosition:  target.Position,
				FootprintRadius: peerSession.deployedCampaignFootprintRadius(),
				MinimumDistance: 5, NormalDistance: 7.5, MaximumDistance: 10,
			},
		)
	if err != nil {
		return nil, fmt.Errorf("fearDestination: %w", err)
	}
	if !isDestinationFound {
		return nil, nil
	}
	err = peerSession.startEnemyFearMovement(
		r.now(), raknet.Vector3{
			X: destination.X, Y: destination.Y, Z: destination.Z,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("fearMotion: %w", err)
	}
	r.registry.sessions[sessionKey] = peerSession
	packet, err := raknet.MarshalApplication(raknet.ObjectPlayerMoveMessage{
		ObjectID: target.ObjectID, GoalFlags: 0x01,
		GoalPosition: raknet.Vector3{
			X: destination.X, Y: destination.Y, Z: destination.Z,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("fearMovementMarshal: %w", err)
	}
	return packet, nil
}
