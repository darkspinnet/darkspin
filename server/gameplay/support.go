package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type campaignNPCSupportKey struct {
	objectID    uint32
	abilityName string
}

type campaignNPCSupportStep struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	timestamp  uint64
	plan       zonenpc.AttackPlan
	spell      zonenpc.SupportSpell
	rank       uint32
}

// The named support families have three independent gambits and no basic attack.
func (e campaignNPCActionRuntime) produceNPCSupport(packet raknet.Packet,
	sessionKey string, generation uint64, objectID uint32, timestamp uint64,
) ([][]byte, bool, error) {
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || !member.isCampaignNPCSourceActive(generation, objectID) {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	source, isSourceFound := member.zone.NPCs().NPC(objectID)
	spells := zonenpc.SupportSpells(source.Plan)
	if !isSourceFound || len(spells) == 0 {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	var selected zonenpc.SupportSpell
	var targetObjectID uint32
	var targetPosition game.Vec3
	if member.isCampaignNPCActionActiveAt(generation, objectID, e.now()) &&
		member.zone.NPCs().SilenceRemaining(objectID, e.now()) == 0 {
		for _, spell := range spells {
			spell.Profile = zonenpc.ApplyAffixTiming(spell.Profile, source.Plan)
			key := campaignNPCSupportKey{objectID: objectID, abilityName: spell.Profile.AbilityName}
			if timestamp < member.campaignNPCSupportReadiness[key] {
				continue
			}
			targetID, position := member.selectNPCSupportTarget(source, spell)
			if targetID != 0 {
				selected, targetObjectID, targetPosition = spell, targetID, position
				break
			}
		}
	}
	if targetObjectID == 0 {
		target, isTargetFound := member.campaignNPCTarget(generation, source.TargetObjectID)
		maximumRange := float32(0)
		for _, spell := range spells {
			maximumRange = max(maximumRange, min(spell.ConditionRadius, spell.Profile.Range))
		}
		if isTargetFound && target.Position.Sub(source.Plan.Position).Length() > maximumRange+
			source.Plan.ActorFootprintRadius()+target.ActorFootprintRadius {
			member.clearNPCSupportIdles(objectID)
			e.registry.sessions[sessionKey] = member
			e.registry.mutex.Unlock()
			return nil, false, nil
		}
		e.registry.mutex.Unlock()
		packets, err := e.produceNPCSupportIdle(packet, sessionKey, generation, source, timestamp)
		if err != nil {
			return nil, true, fmt.Errorf("supportIdle: %w", err)
		}
		return packets, true, nil
	}
	profile := selected.Profile
	member.clearNPCSupportIdles(objectID)
	source.Plan.SupportIdleMovementBuff = 0
	key := campaignNPCSupportKey{objectID: objectID, abilityName: profile.AbilityName}
	if member.campaignNPCSupportReadiness == nil {
		member.campaignNPCSupportReadiness = make(map[campaignNPCSupportKey]uint64)
	}
	previous := member.campaignNPCSupportReadiness[key]
	member.campaignNPCSupportReadiness[key] = timestamp + uint64((profile.HitDelay+profile.Cooldown)/time.Millisecond)
	plan := zonenpc.AttackPlan{SourceObjectID: objectID, TargetObjectID: targetObjectID,
		SourcePosition: source.Plan.Position, TargetPosition: targetPosition,
		ActionGeneration: source.ActionGeneration, Profile: profile}
	e.registry.sessions[sessionKey] = member
	e.registry.mutex.Unlock()
	packets, err := e.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		e.restoreNPCSupportReadiness(sessionKey, generation, key, previous)
		return nil, true, fmt.Errorf("supportStart: %w", err)
	}
	step := campaignNPCSupportStep{runtime: e, packet: packet, sessionKey: sessionKey,
		generation: generation, timestamp: timestamp, plan: plan, spell: selected, rank: uint32(zonenpc.EffectiveNPCRank(source.Plan))}
	cancel, err := scheduleNPCProducers(e.registry, packet, []raknet.ScheduledPacketProducer{
		{Delay: profile.HitDelay, Produce: step.hit},
		{Delay: max(profile.HitDelay, profile.ReleaseDelay) + time.Millisecond, Produce: step.next},
	})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		e.restoreNPCSupportReadiness(sessionKey, generation, key, previous)
		e.releaseActionGeneration(sessionKey, generation, objectID, source.ActionGeneration)
		return nil, true, fmt.Errorf("supportSchedule: %w", err)
	}
	return packets, true, nil
}

func (e *gameplayPeerSession) selectNPCSupportTarget(source zonenpc.Snapshot,
	spell zonenpc.SupportSpell,
) (uint32, game.Vec3) {
	if spell.IsAlly {
		for _, target := range e.zone.NPCs().LiveSnapshots() {
			if target.IsDefeated || target.HitPoint <= 0 || (!spell.IsSelfAllowed && target.Plan.ObjectID == source.Plan.ObjectID) ||
				target.Faction != source.Faction || target.Plan.NPCProfile.NounType != game.NounTypeCreature ||
				target.Plan.Position.Sub(source.Plan.Position).Length() > min(spell.ConditionRadius, spell.Profile.Range)+target.Plan.ActorFootprintRadius()+source.Plan.ActorFootprintRadius() ||
				e.hasNPCSupportModifier(target.Plan.ObjectID, spell.Profile.ModifierName) {
				continue
			}
			return target.Plan.ObjectID, target.Plan.Position
		}
		return 0, game.Vec3{}
	}
	for _, target := range e.zone.LiveNPCTargets() {
		if target.HitPoint <= 0 ||
			target.Position.Sub(source.Plan.Position).Length() > min(spell.ConditionRadius, spell.Profile.Range)+target.ActorFootprintRadius+source.Plan.ActorFootprintRadius() ||
			e.hasNPCSupportModifier(target.ObjectID, spell.Profile.ModifierName) {
			continue
		}
		return target.ObjectID, target.Position
	}
	return 0, game.Vec3{}
}

func (e *gameplayPeerSession) hasNPCSupportModifier(objectID uint32, name string) bool {
	if e.campaignNPCPoisons[campaignNPCPoisonKey{targetObjectID: objectID, modifierName: name}] != nil {
		return true
	}
	for _, modifier := range e.zone.Effect().Snapshot() {
		if modifier.TargetObjectID == objectID && modifier.GUID == util.HashID(name) {
			return true
		}
	}
	return false
}

func (e campaignNPCActionRuntime) restoreNPCSupportReadiness(sessionKey string,
	generation uint64, key campaignNPCSupportKey, timestamp uint64,
) {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.generation != generation {
		return
	}
	member.campaignNPCSupportReadiness[key] = timestamp
	e.registry.sessions[sessionKey] = member
}

func (e campaignNPCSupportStep) hit() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && member.isCampaignNPCSourceGenerationActive(e.generation, e.plan.SourceObjectID, e.plan.ActionGeneration) &&
		member.isCampaignNPCActionActiveAt(e.generation, e.plan.SourceObjectID, e.runtime.now())
	if isCurrent {
		isCurrent = member.zone.NPCs().SilenceRemaining(e.plan.SourceObjectID, e.runtime.now()) == 0
	}
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	var packets [][]byte
	var err error
	if isCampaignNPCDamageOverTimeProfile(e.plan.Profile) {
		packets, err = e.runtime.applyCampaignNPCPoison(e.packet, e.sessionKey, e.generation, e.plan,
			e.timestamp+uint64(e.plan.Profile.HitDelay/time.Millisecond))
	} else {
		packets, err = e.applyModifier()
	}
	if err != nil {
		return nil, fmt.Errorf("supportApply: %w", err)
	}
	if e.plan.Profile.ImpactEffectName != "" && len(packets) > 0 {
		impactPacket, impactErr := npcraknet.PositionedEffect(e.plan.Profile.ImpactEffectName, e.plan.TargetPosition)
		if impactErr != nil {
			return nil, fmt.Errorf("supportImpact: %w", impactErr)
		}
		packets = append(packets, impactPacket)
	}
	return packets, nil
}

func (e campaignNPCSupportStep) next() ([][]byte, error) {
	if e.runtime.finishScenarioNPCManualAction(e.sessionKey, e.generation, e.plan.SourceObjectID, e.plan.ActionGeneration) {
		return nil, nil
	}
	step := campaignNPCFirstActionStep{runtime: e.runtime, packet: e.packet, sessionKey: e.sessionKey,
		generation: e.generation, objectID: e.plan.SourceObjectID, actionGeneration: e.plan.ActionGeneration,
		timestamp: e.timestamp + uint64(max(e.plan.Profile.HitDelay, e.plan.Profile.ReleaseDelay)/time.Millisecond) + 1}
	packets, err := step.produce()
	if err != nil {
		return nil, fmt.Errorf("supportNext: %w", err)
	}
	return packets, nil
}

func (e campaignNPCSupportStep) applyModifier() ([][]byte, error) {
	profile := e.plan.Profile
	run, err := newCampaignNPCModifierRun(e.runtime.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("supportAllocate: %w", err)
	}
	kind := zoneeffect.ModifierKindDebuff
	if e.spell.IsAlly {
		kind = zoneeffect.ModifierKindBuff
	}
	run.record = zoneeffect.Modifier{InstanceID: run.instanceID, GUID: profile.ModifierGUID(),
		SourceObjectID: e.plan.SourceObjectID, TargetObjectID: e.plan.TargetObjectID,
		Rank: e.rank, Duration: profile.ModifierDuration, Kind: kind, IsIndefinite: e.spell.IsIndefinite,
		InitiatorObject: e.plan.SourceObjectID, StackCount: 1, EnergyDamageTakenIncrease: e.spell.EnergyDamageIncrease}
	if !e.spell.IsIndefinite {
		run.record.ExpiresAt = e.runtime.now().Add(profile.ModifierDuration)
	}
	e.runtime.registry.mutex.Lock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && member.generation == e.generation && member.zone != nil &&
		!member.hasNPCSupportModifier(e.plan.TargetObjectID, profile.ModifierName)
	if isCurrent && e.spell.IsAlly {
		target, isTargetFound := member.zone.NPCs().NPC(e.plan.TargetObjectID)
		source, isSourceFound := member.zone.NPCs().NPC(e.plan.SourceObjectID)
		isCurrent = isTargetFound && isSourceFound && !target.IsDefeated && target.HitPoint > 0 && target.Faction == source.Faction
	} else if isCurrent {
		target, isTargetFound := member.campaignNPCTarget(e.generation, e.plan.TargetObjectID)
		isCurrent = isTargetFound && target.HitPoint > 0 && !e.runtime.isTargetDebuffImmuneLocked(&member, e.plan.TargetObjectID)
	}
	if isCurrent {
		err = member.trackCampaignNPCModifier(run)
		if err == nil {
			err = member.zone.Effect().Put(run.record)
		}
		if err == nil {
			e.runtime.registry.sessions[e.sessionKey] = member
		}
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent || err != nil {
		e.runtime.rollbackCampaignNPCTimedModifier(e.sessionKey, e.generation, run)
		isReleased, releaseErr := run.release(e.runtime.modifierPool)
		if isReleased {
			return nil, errors.New("support uncreated modifier released as created")
		}
		if err != nil || releaseErr != nil {
			return nil, fmt.Errorf("supportTrack: %w", errors.Join(err, releaseErr))
		}
		return nil, nil
	}
	createPacket, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: e.plan.SourceObjectID, TargetObjectID: e.plan.TargetObjectID,
		ModifierID: profile.ModifierGUID(), InstanceID: run.instanceID, StackCount: 1, Duration: profile.ModifierDuration,
		Timestamp: e.timestamp + uint64(profile.HitDelay/time.Millisecond), IsIndefinite: e.spell.IsIndefinite})
	if err != nil {
		e.runtime.rollbackCampaignNPCTimedModifier(e.sessionKey, e.generation, run)
		return nil, fmt.Errorf("supportCreate: %w", err)
	}
	if !e.spell.IsIndefinite {
		expiry := campaignNPCTimedModifierExpiryStep{runtime: e.runtime, sessionKey: e.sessionKey,
			generation: e.generation, targetObjectID: e.plan.TargetObjectID, run: run}
		cancel, scheduleErr := scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{
			{Delay: profile.ModifierDuration, Produce: expiry.produce},
		})
		if scheduleErr == nil && cancel == nil {
			scheduleErr = errors.New("nil cancellation")
		}
		if scheduleErr != nil {
			e.runtime.rollbackCampaignNPCTimedModifier(e.sessionKey, e.generation, run)
			return nil, fmt.Errorf("supportExpiry: %w", scheduleErr)
		}
		run.cancel = cancel
	}
	if !run.create() {
		if run.cancel != nil {
			run.cancel()
		}
		return nil, nil
	}
	return [][]byte{createPacket}, nil
}
