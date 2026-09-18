package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type campaignNPCEnergyBuffRun struct {
	modifier  *campaignNPCModifierRun
	targetID  uint32
	expiresAt time.Time
	cancel    raknet.CancelSchedule
}

type campaignNPCEnergyBuffSchedule struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	sourceID   uint32
	targetID   uint32
	timestamp  uint64
	profile    zonenpc.EnergyBuffProfile
}

func (e campaignNPCEnergyBuffSchedule) hit() ([][]byte, error) {
	isDeferred, err := e.runtime.pursuit.deferCommit(
		e.packet, e.sessionKey, e.generation, e.sourceID, e.hit,
	)
	if err != nil {
		return nil, fmt.Errorf("energyBuffDefer: %w", err)
	}
	if isDeferred {
		return nil, nil
	}
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.isCampaignNPCSourceActive(e.generation, e.sourceID) &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	target, isTargetFound := peerSession.zone.NPCs().NPC(e.targetID)
	if !isTargetFound || target.IsDefeated || !target.IsPublished {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if peerSession.campaignNPCEnergyBuffs == nil {
		peerSession.campaignNPCEnergyBuffs = make(map[uint32]*campaignNPCEnergyBuffRun)
	}
	if peerSession.campaignNPCEnergyBuffs[e.targetID] != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	modifier, err := newCampaignNPCModifierRun(e.runtime.modifierPool)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("energyBuffRun: %w", err)
	}
	run := &campaignNPCEnergyBuffRun{
		modifier: modifier, targetID: e.targetID,
		expiresAt: e.runtime.now().Add(e.profile.Cast.ModifierDuration),
	}
	modifier.record = zoneeffect.Modifier{
		InstanceID: modifier.instanceID, GUID: util.HashID(e.profile.Cast.ModifierName),
		SourceObjectID: e.sourceID, TargetObjectID: e.targetID,
		Rank: 1, Duration: e.profile.Cast.ModifierDuration,
		Kind: zoneeffect.ModifierKindBuff, InitiatorObject: e.sourceID,
		StackCount: 1, EnergyDamageBuff: e.profile.DamageIncrease,
	}
	err = peerSession.zone.NPCs().ApplyEnergyBuff(
		e.targetID, run.expiresAt, e.profile.DamageIncrease,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		_, releaseErr := modifier.release(e.runtime.modifierPool)
		return nil, fmt.Errorf("energyBuffApply: %w", errors.Join(err, releaseErr))
	}
	err = peerSession.trackCampaignNPCModifier(modifier)
	if err == nil {
		err = peerSession.zone.Effect().Put(modifier.record)
	}
	if err != nil {
		peerSession.zone.NPCs().ClearEnergyBuff(e.targetID, run.expiresAt)
		peerSession.zone.Effect().Remove(modifier.instanceID)
		e.runtime.registry.mutex.Unlock()
		_, releaseErr := modifier.release(e.runtime.modifierPool)
		return nil, fmt.Errorf("energyBuffTrack: %w", errors.Join(err, releaseErr))
	}
	peerSession.campaignNPCEnergyBuffs[e.targetID] = run
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()

	plan := zonenpc.AttackPlan{
		SourceObjectID: e.sourceID, TargetObjectID: e.targetID,
		Profile: e.profile.Cast,
	}
	createPacket, err := npcraknet.ModifierCreate(
		plan, modifier.instanceID,
		e.timestamp+uint64(e.profile.Cast.HitDelay/time.Millisecond),
	)
	if err != nil {
		e.runtime.rollbackZelemEnergyBuff(e.sessionKey, e.generation, run)
		return nil, fmt.Errorf("energyBuffCreate: %w", err)
	}
	effectPacket, err := npcraknet.ChannelImpact(plan)
	if err != nil {
		e.runtime.rollbackZelemEnergyBuff(e.sessionKey, e.generation, run)
		return nil, fmt.Errorf("energyBuffEffect: %w", err)
	}
	expiry := campaignNPCEnergyBuffExpiry{
		runtime: e.runtime, sessionKey: e.sessionKey,
		generation: e.generation, run: run,
	}
	cancel, scheduleErr := scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{{
		Delay: e.profile.Cast.ModifierDuration, Produce: expiry.produce,
	}})
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		e.runtime.rollbackZelemEnergyBuff(e.sessionKey, e.generation, run)
		return nil, fmt.Errorf("energyBuffExpiry: %w", scheduleErr)
	}
	e.runtime.registry.mutex.Lock()
	latest, isLatestFound := e.runtime.registry.sessions[e.sessionKey]
	isLatest := isLatestFound && latest.generation == e.generation &&
		latest.campaignNPCEnergyBuffs[e.targetID] == run
	if isLatest {
		if latest.campaignNPCEnergyBuffReadiness == nil {
			latest.campaignNPCEnergyBuffReadiness = make(map[uint32]uint64)
		}
		latest.campaignNPCEnergyBuffReadiness[e.sourceID] = e.timestamp +
			uint64(e.profile.Cast.ModifierDuration/time.Millisecond)
		run.cancel = cancel
		run.modifier.create()
		e.runtime.registry.sessions[e.sessionKey] = latest
	}
	e.runtime.registry.mutex.Unlock()
	if !isLatest {
		cancel()
		return nil, nil
	}
	return [][]byte{createPacket, effectPacket}, nil
}

func (e campaignNPCEnergyBuffSchedule) next() ([][]byte, error) {
	timestamp := e.timestamp + uint64(e.profile.Cast.Cooldown/time.Millisecond)
	packets, err := e.runtime.produceZelemShot(
		e.packet, e.sessionKey, e.generation, e.sourceID, timestamp,
	)
	if err != nil {
		e.runtime.releaseAction(e.sessionKey, e.generation, e.sourceID)
		return nil, fmt.Errorf("energyBuffNext: %w", err)
	}
	return packets, nil
}

type campaignNPCEnergyBuffExpiry struct {
	runtime    campaignNPCActionRuntime
	sessionKey string
	generation uint64
	run        *campaignNPCEnergyBuffRun
}

func (e campaignNPCEnergyBuffExpiry) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil &&
		peerSession.campaignNPCEnergyBuffs[e.run.targetID] == e.run
	if isCurrent {
		delete(peerSession.campaignNPCEnergyBuffs, e.run.targetID)
		peerSession.zone.NPCs().ClearEnergyBuff(e.run.targetID, e.run.expiresAt)
		peerSession.zone.Effect().Remove(e.run.modifier.instanceID)
		peerSession.untrackCampaignNPCModifier(e.run.modifier)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	isCreated, err := e.run.modifier.release(e.runtime.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("energyBuffRelease: %w", err)
	}
	if !isCreated {
		return nil, nil
	}
	packet, err := effectraknet.ModifierDelete(
		e.run.targetID, e.run.modifier.instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("energyBuffDelete: %w", err)
	}
	return [][]byte{packet}, nil
}

func (r campaignNPCActionRuntime) produceZelemSpecialThreeEnergyBuff(
	packet raknet.Packet, sessionKey string, generation uint64,
	source zonenpc.Snapshot, timestamp uint64,
) ([][]byte, bool, error) {
	profile, isProfileFound := zonenpc.ZelemSpecialThreeEnergyBuffProfile(
		source.Plan.NounName,
	)
	if !isProfileFound {
		return nil, false, nil
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil &&
		peerSession.isCampaignNPCSourceActive(generation, source.Plan.ObjectID)
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil, true, nil
	}
	if peerSession.zone.NPCs().SilenceRemaining(source.Plan.ObjectID, r.now()) > 0 {
		r.registry.mutex.RUnlock()
		return nil, false, nil
	}
	if peerSession.campaignNPCEnergyBuffReadiness[source.Plan.ObjectID] > timestamp {
		r.registry.mutex.RUnlock()
		return nil, false, nil
	}
	target, isTargetFound := peerSession.zone.NPCs().FirstEnergyDamageAlly(
		source.Plan.ObjectID, profile.SelectionRange, r.now(),
	)
	r.registry.mutex.RUnlock()
	if !isTargetFound {
		return nil, false, nil
	}
	if zonegeometry.Distance(source.Plan.Position, target.Plan.Position) >
		profile.Cast.Range {
		action, err := campaignNPCActionWithProfile(
			source.Plan, target.Plan.ObjectID, target.Plan.Position,
			profile.Cast, target.Plan.NPCProfile.FootprintRadius,
		)
		if err != nil {
			r.releaseAction(sessionKey, generation, source.Plan.ObjectID)
			return nil, true, fmt.Errorf("energyBuffPursuitAction: %w", err)
		}
		if !action.IsPursuitNeeded {
			return nil, false, nil
		}
		pursuitPackets, err := npcraknet.Pursuit(action)
		if err != nil {
			r.releaseAction(sessionKey, generation, source.Plan.ObjectID)
			return nil, true, fmt.Errorf("energyBuffPursuitMarshal: %w", err)
		}
		resume := campaignNPCProjectileSchedule{
			runtime: r, packet: packet, sessionKey: sessionKey,
			generation: generation, sourceObjectID: source.Plan.ObjectID,
			kind: campaignNPCProjectileZelem,
		}
		err = r.pursuit.scheduleTarget(
			packet, sessionKey, generation, source.Plan.ObjectID,
			target.Plan.ObjectID, timestamp, action.TargetPosition,
			profile.Cast, resume.resume,
		)
		if err != nil {
			r.releaseAction(sessionKey, generation, source.Plan.ObjectID)
			return nil, true, fmt.Errorf("energyBuffPursuitSchedule: %w", err)
		}
		return pursuitPackets, true, nil
	}
	if target.Plan.ObjectID == source.Plan.ObjectID {
		profile.Cast.AnimationName = profile.SelfAnimationName
	}
	plan := zonenpc.AttackPlan{
		SourceObjectID:   source.Plan.ObjectID,
		TargetObjectID:   target.Plan.ObjectID,
		ActionGeneration: source.ActionGeneration,
		SourcePosition:   source.Plan.Position,
		TargetPosition:   target.Plan.Position,
		Profile:          profile.Cast,
	}
	castPackets, err := r.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		return nil, true, fmt.Errorf("energyBuffCast: %w", err)
	}
	schedule := campaignNPCEnergyBuffSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, sourceID: source.Plan.ObjectID,
		targetID: target.Plan.ObjectID, timestamp: timestamp, profile: profile,
	}
	cancel, scheduleErr := scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{
		{Delay: profile.Cast.HitDelay, Produce: schedule.hit},
		{Delay: max(profile.Cast.Cooldown, profile.Cast.ReleaseDelay), Produce: schedule.next},
	})
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		r.releaseAction(sessionKey, generation, source.Plan.ObjectID)
		return nil, true, fmt.Errorf("energyBuffSchedule: %w", scheduleErr)
	}
	return castPackets, true, nil
}

func (r campaignNPCActionRuntime) rollbackZelemEnergyBuff(
	sessionKey string, generation uint64, run *campaignNPCEnergyBuffRun,
) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil &&
		peerSession.campaignNPCEnergyBuffs[run.targetID] == run
	if isCurrent {
		delete(peerSession.campaignNPCEnergyBuffs, run.targetID)
		peerSession.zone.NPCs().ClearEnergyBuff(run.targetID, run.expiresAt)
		peerSession.zone.Effect().Remove(run.modifier.instanceID)
		peerSession.untrackCampaignNPCModifier(run.modifier)
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
	if isCurrent {
		_, _ = run.modifier.release(r.modifierPool)
	}
}

func (r campaignNPCActionRuntime) stopZelemEnergyBuff(
	sessionKey string, generation uint64, targetObjectID uint32,
) ([][]byte, error) {
	if sessionKey == "" || generation == 0 || targetObjectID == 0 {
		return nil, errors.New("energy buff stop invalid")
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	run := peerSession.campaignNPCEnergyBuffs[targetObjectID]
	if run == nil {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	delete(peerSession.campaignNPCEnergyBuffs, targetObjectID)
	peerSession.zone.NPCs().ClearEnergyBuff(targetObjectID, run.expiresAt)
	peerSession.zone.Effect().Remove(run.modifier.instanceID)
	peerSession.untrackCampaignNPCModifier(run.modifier)
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	if run.cancel != nil {
		run.cancel()
	}
	isCreated, err := run.modifier.release(r.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("energyBuffStopRelease: %w", err)
	}
	if !isCreated {
		return nil, nil
	}
	packet, err := effectraknet.ModifierDelete(
		targetObjectID, run.modifier.instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("energyBuffStopDelete: %w", err)
	}
	return [][]byte{packet}, nil
}
