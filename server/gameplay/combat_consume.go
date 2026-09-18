package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const campaignConsumeApproachTimeout = 5 * time.Second
const campaignConsumeNearRange = 0.95
const campaignMunchBodyScaleAttribute = uint8(113)
const campaignMunchBodyScalePerStack = float32(0.06)
const campaignConsumeCombatInterval = 15 * time.Second

type campaignNPCMunchRun struct {
	modifier  *campaignNPCModifierRun
	expiresAt time.Time
}

type campaignConsumeSchedule struct {
	runtime          campaignNPCActionRuntime
	packet           raknet.Packet
	sessionKey       string
	generation       uint64
	actionGeneration uint64
	sourceObjectID   uint32
	corpseObjectID   uint32
	timestamp        uint64
	profile          zonenpc.ActionProfile
	approachTime     time.Duration
}

func (e campaignConsumeSchedule) fail(
	step string, err error,
) ([][]byte, error) {
	e.releaseClaim()
	e.runtime.releaseAction(e.sessionKey, e.generation, e.sourceObjectID)
	return nil, fmt.Errorf("%s: %w", step, err)
}

func (e campaignConsumeSchedule) releaseClaim() {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if isFound && peerSession.generation == e.generation && peerSession.zone != nil {
		peerSession.zone.NPCs().ReleaseCorpseClaim(e.corpseObjectID)
	}
	e.runtime.registry.mutex.RUnlock()
}

func (e campaignConsumeSchedule) approach() ([][]byte, error) {
	e.timestamp += uint64(campaignPursuitFallbackTick / time.Millisecond)
	e.approachTime += campaignPursuitFallbackTick
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		e.generation, e.sourceObjectID,
	)
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		e.releaseClaim()
		return nil, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(e.sourceObjectID)
	corpse, isCorpseFound := peerSession.zone.NPCs().ClaimedCorpse(e.corpseObjectID)
	if !isSourceFound || !isCorpseFound {
		e.runtime.registry.mutex.Unlock()
		e.releaseClaim()
		return nil, nil
	}
	if peerSession.zone.NPCs().StunRemaining(e.sourceObjectID, e.runtime.now()) > 0 ||
		peerSession.zone.NPCs().SleepRemaining(e.sourceObjectID, e.runtime.now()) > 0 ||
		peerSession.zone.NPCs().RootRemaining(e.sourceObjectID, e.runtime.now()) > 0 {
		e.runtime.registry.mutex.Unlock()
		return e.rescheduleApproach()
	}
	stopRange := campaignConsumeNearRange + source.Plan.NPCProfile.FootprintRadius +
		corpse.Plan.NPCProfile.FootprintRadius
	movementSpeed := graspingDeadMovementSpeed(
		peerSession, source.Plan.Position, e.profile.MovementSpeed,
	)
	slowMovementScale := peerSession.zone.NPCs().SlowMovementScale(
		e.sourceObjectID, e.runtime.now(),
	)
	step, err := peerSession.zone.NPCs().AdvancePursuit(
		peerSession.zone.Navigation(), e.sourceObjectID, corpse.Plan.Position,
		stopRange, movementSpeed*slowMovementScale,
		source.Plan.NPCProfile.FootprintRadius, campaignPursuitFallbackTick,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return e.fail("consumeAdvance", err)
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	if step.IsInRange {
		positionPacket, marshalErr := npcraknet.Position(
			e.sourceObjectID, step.Position,
		)
		if marshalErr != nil {
			return e.fail("consumePosition", marshalErr)
		}
		castPackets, castErr := e.cast()
		if castErr != nil {
			return nil, castErr
		}
		return append([][]byte{positionPacket}, castPackets...), nil
	}
	return e.rescheduleApproach()
}

func (e campaignConsumeSchedule) rescheduleApproach() ([][]byte, error) {
	if e.approachTime >= campaignConsumeApproachTimeout {
		e.releaseClaim()
		return e.next()
	}
	cancel, err := scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{{
		Delay: campaignPursuitFallbackTick, Produce: e.approach,
	}})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		return e.fail("consumeApproachSchedule", err)
	}
	return nil, nil
}

func (e campaignConsumeSchedule) cast() ([][]byte, error) {
	castPacket, err := npcraknet.AnimationState(
		e.sourceObjectID, e.profile.AnimationName, e.timestamp,
	)
	if err != nil {
		return e.fail("consumeAnimation", err)
	}
	cancel, err := scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{
		{Delay: e.profile.HitDelay, Produce: e.hit},
		{Delay: e.profile.ReleaseDelay, Produce: e.fade},
	})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		return e.fail("consumeSchedule", err)
	}
	return [][]byte{castPacket}, nil
}

func (e campaignConsumeSchedule) hit() ([][]byte, error) {
	at := e.runtime.now()
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		e.generation, e.sourceObjectID,
	)
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		e.releaseClaim()
		return nil, nil
	}
	result, err := peerSession.zone.NPCs().ConsumeCorpse(
		e.sourceObjectID, e.corpseObjectID, e.profile.MinimumHealing,
		e.profile.ModifierDamageBuff, e.profile.ModifierMaximumStack,
		e.profile.ModifierDuration, at,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if peerSession.campaignNPCMunchReadiness == nil {
		peerSession.campaignNPCMunchReadiness = make(map[uint32]uint64)
	}
	peerSession.campaignNPCMunchReadiness[e.sourceObjectID] = e.timestamp +
		uint64(campaignConsumeCombatInterval/time.Millisecond)
	run := peerSession.campaignNPCMunches[e.sourceObjectID]
	isNewModifier := run == nil
	if run == nil {
		modifier, modifierErr := newCampaignNPCModifierRun(e.runtime.modifierPool)
		if modifierErr != nil {
			e.runtime.registry.mutex.Unlock()
			return e.fail("consumeModifier", modifierErr)
		}
		run = &campaignNPCMunchRun{modifier: modifier}
		if peerSession.campaignNPCMunches == nil {
			peerSession.campaignNPCMunches = make(map[uint32]*campaignNPCMunchRun)
		}
		trackErr := peerSession.trackCampaignNPCModifier(modifier)
		if trackErr != nil {
			e.runtime.registry.mutex.Unlock()
			_, _ = modifier.release(e.runtime.modifierPool)
			return e.fail("consumeModifierTrack", trackErr)
		}
		peerSession.campaignNPCMunches[e.sourceObjectID] = run
	}
	run.expiresAt = result.ExpiresAt
	run.modifier.record = zoneeffect.Modifier{
		InstanceID: run.modifier.instanceID, GUID: util.HashID(e.profile.ModifierName),
		SourceObjectID: e.sourceObjectID, TargetObjectID: e.sourceObjectID,
		Rank: 1, Duration: e.profile.ModifierDuration,
		Kind: zoneeffect.ModifierKindBuff, InitiatorObject: e.sourceObjectID,
		StackCount: result.ModifierStack,
		DamageBuff: e.profile.ModifierDamageBuff * float32(result.ModifierStack),
	}
	if isNewModifier {
		err = peerSession.zone.Effect().Put(run.modifier.record)
	} else {
		err = peerSession.zone.Effect().Update(run.modifier.record)
	}
	if err != nil {
		peerSession.zone.NPCs().ClearMunch(e.sourceObjectID, result.ExpiresAt)
		if isNewModifier {
			peerSession.untrackCampaignNPCModifier(run.modifier)
			delete(peerSession.campaignNPCMunches, e.sourceObjectID)
		}
		e.runtime.registry.mutex.Unlock()
		if isNewModifier {
			_, _ = run.modifier.release(e.runtime.modifierPool)
		}
		return e.fail("consumeModifierInventory", err)
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	modifierPacket, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: e.sourceObjectID, TargetObjectID: e.sourceObjectID,
		ModifierID: util.HashID(e.profile.ModifierName),
		InstanceID: run.modifier.instanceID, StackCount: result.ModifierStack,
		Duration:  e.profile.ModifierDuration,
		Timestamp: e.timestamp + uint64(e.profile.HitDelay/time.Millisecond),
	})
	if err != nil {
		return e.fail("consumeModifierMarshal", err)
	}
	run.modifier.create()
	expiry := campaignMunchExpiryStep{
		runtime: e.runtime, sessionKey: e.sessionKey, generation: e.generation,
		objectID: e.sourceObjectID, run: run, expiresAt: result.ExpiresAt,
	}
	_, err = scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{{
		Delay: e.profile.ModifierDuration, Produce: expiry.produce,
	}})
	if err != nil {
		return e.fail("consumeExpirySchedule", err)
	}
	packets := [][]byte{modifierPacket}
	bodyScalePacket, bodyScaleErr := raknet.MarshalApplication(
		raknet.AttributeDataUpdateMessage{
			ObjectID: e.sourceObjectID,
			Value: map[uint8]float32{
				campaignMunchBodyScaleAttribute: campaignMunchBodyScalePerStack *
					float32(result.ModifierStack),
			},
		},
	)
	if bodyScaleErr != nil {
		e.runtime.logger.Printf(
			"RakNet campaign consume body scale omitted source=%d: %v",
			e.sourceObjectID, bodyScaleErr,
		)
	} else {
		packets = append(packets, bodyScalePacket)
	}
	if result.HealedAmount > 0 {
		objectiveErr := peerSession.zone.RecordNPCHeal(
			peerSession.zone.Context(), e.sourceObjectID,
			e.sourceObjectID, result.HealedAmount,
		)
		if objectiveErr != nil {
			e.runtime.logger.Printf(
				"RakNet campaign consume heal objective omitted source=%d: %v",
				e.sourceObjectID, objectiveErr,
			)
		}
		healPackets, healErr := npcraknet.HealDelta(
			e.sourceObjectID, result.Source, result.HealedAmount,
		)
		if healErr != nil {
			e.runtime.logger.Printf(
				"RakNet campaign consume heal presentation omitted source=%d: %v",
				e.sourceObjectID, healErr,
			)
			return packets, nil
		}
		packets = append(healPackets, packets...)
	}
	return packets, nil
}

func (e campaignConsumeSchedule) fade() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isFading := isFound && peerSession.generation == e.generation &&
		peerSession.zone.NPCs().StartCorpseFadePublication(e.corpseObjectID)
	e.runtime.registry.mutex.RUnlock()
	packets := make([][]byte, 0, 1)
	if isFading {
		packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{
			ObjectID: []uint32{e.corpseObjectID},
		})
		if err != nil {
			return e.fail("consumeCorpseDelete", err)
		}
		packets = append(packets, packet)
	}
	e.timestamp += uint64(e.profile.ReleaseDelay / time.Millisecond)
	nextPackets, err := e.next()
	if err != nil {
		return nil, err
	}
	return append(packets, nextPackets...), nil
}

func (r campaignNPCActionRuntime) stopCarrionMunch(
	sessionKey string, generation uint64, objectID uint32,
) ([][]byte, error) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation
	var run *campaignNPCMunchRun
	if isCurrent {
		run = peerSession.campaignNPCMunches[objectID]
	}
	if run != nil {
		peerSession.zone.NPCs().ClearMunch(objectID, run.expiresAt)
		peerSession.zone.Effect().Remove(run.modifier.instanceID)
		delete(peerSession.campaignNPCMunches, objectID)
		peerSession.untrackCampaignNPCModifier(run.modifier)
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
	if run == nil {
		return nil, nil
	}
	isCreated, err := run.modifier.release(r.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("consumeStopRelease: %w", err)
	}
	if !isCreated {
		return nil, nil
	}
	packet, err := effectraknet.ModifierDelete(objectID, run.modifier.instanceID)
	if err != nil {
		return nil, fmt.Errorf("consumeStopDelete: %w", err)
	}
	packets := [][]byte{packet}
	bodyScalePacket, bodyScaleErr := raknet.MarshalApplication(
		raknet.AttributeDataUpdateMessage{
			ObjectID: objectID,
			Value:    map[uint8]float32{campaignMunchBodyScaleAttribute: 0},
		},
	)
	if bodyScaleErr != nil {
		r.logger.Printf(
			"RakNet campaign consume body scale reset omitted source=%d: %v",
			objectID, bodyScaleErr,
		)
		return packets, nil
	}
	return append(packets, bodyScalePacket), nil
}

func (e campaignConsumeSchedule) next() ([][]byte, error) {
	step := campaignNPCFirstActionStep{
		runtime: e.runtime, packet: e.packet, sessionKey: e.sessionKey,
		generation: e.generation, objectID: e.sourceObjectID,
		actionGeneration: e.actionGeneration,
		timestamp:        e.timestamp,
	}
	packets, err := step.produce()
	if err != nil {
		return e.fail("consumeNext", err)
	}
	return packets, nil
}

type campaignMunchExpiryStep struct {
	runtime    campaignNPCActionRuntime
	sessionKey string
	generation uint64
	objectID   uint32
	run        *campaignNPCMunchRun
	expiresAt  time.Time
}

func (e campaignMunchExpiryStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.campaignNPCMunches[e.objectID] == e.run &&
		e.run.expiresAt == e.expiresAt
	if isCurrent {
		peerSession.zone.NPCs().ClearMunch(e.objectID, e.expiresAt)
		peerSession.zone.Effect().Remove(e.run.modifier.instanceID)
		delete(peerSession.campaignNPCMunches, e.objectID)
		peerSession.untrackCampaignNPCModifier(e.run.modifier)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	isCreated, err := e.run.modifier.release(e.runtime.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("consumeModifierRelease: %w", err)
	}
	if !isCreated {
		return nil, nil
	}
	packet, err := effectraknet.ModifierDelete(
		e.objectID, e.run.modifier.instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("consumeModifierDelete: %w", err)
	}
	packets := [][]byte{packet}
	bodyScalePacket, bodyScaleErr := raknet.MarshalApplication(
		raknet.AttributeDataUpdateMessage{
			ObjectID: e.objectID,
			Value:    map[uint8]float32{campaignMunchBodyScaleAttribute: 0},
		},
	)
	if bodyScaleErr != nil {
		e.runtime.logger.Printf(
			"RakNet campaign consume body scale expiry omitted source=%d: %v",
			e.objectID, bodyScaleErr,
		)
		return packets, nil
	}
	return append(packets, bodyScalePacket), nil
}

func (r campaignNPCActionRuntime) produceCarrionConsume(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, bool, error) {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil, false, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(objectID)
	profile, isProfileFound := zonenpc.NocturnaSpecialMunchConsumeProfile(
		source.Plan.NounName,
	)
	if !isSourceFound || !isProfileFound {
		r.registry.mutex.RUnlock()
		return nil, false, nil
	}
	if timestamp < peerSession.campaignNPCMunchReadiness[objectID] {
		r.registry.mutex.RUnlock()
		return nil, false, nil
	}
	candidate := peerSession.zone.NPCs().ConsumableCorpseCandidates(
		objectID, profile.Range,
	)
	r.registry.mutex.RUnlock()
	if len(candidate) == 0 {
		return nil, false, nil
	}
	corpse := candidate[0]
	err := peerSession.zone.NPCs().ClaimCorpse(objectID, corpse.Plan.ObjectID)
	if err != nil {
		return nil, false, nil
	}
	movementProfile := profile
	movementProfile.Family = zonenpc.ActionCone
	movementProfile.Range = campaignConsumeNearRange
	action, err := campaignNPCActionWithProfile(
		source.Plan, corpse.Plan.ObjectID, corpse.Plan.Position, movementProfile,
		corpse.Plan.NPCProfile.FootprintRadius,
	)
	if err != nil {
		peerSession.zone.NPCs().ReleaseCorpseClaim(corpse.Plan.ObjectID)
		return nil, false, fmt.Errorf("consumePursuitPlan: %w", err)
	}
	schedule := campaignConsumeSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey, generation: generation,
		actionGeneration: source.ActionGeneration,
		sourceObjectID:   objectID, corpseObjectID: corpse.Plan.ObjectID,
		timestamp: timestamp, profile: profile,
	}
	if !action.IsPursuitNeeded {
		packets, castErr := schedule.cast()
		return packets, true, castErr
	}
	pursuitPackets, err := npcraknet.Pursuit(action)
	if err != nil {
		schedule.releaseClaim()
		return nil, false, fmt.Errorf("consumePursuitMarshal: %w", err)
	}
	_, err = scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{{
		Delay: campaignPursuitFallbackTick, Produce: schedule.approach,
	}})
	if err != nil {
		schedule.releaseClaim()
		r.releaseAction(sessionKey, generation, objectID)
		return nil, false, fmt.Errorf("consumePursuitSchedule: %w", err)
	}
	return pursuitPackets, true, nil
}
