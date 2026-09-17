package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type campaignNPCHealSchedule struct {
	runtime        campaignNPCActionRuntime
	packet         raknet.Packet
	sessionKey     string
	generation     uint64
	sourceObjectID uint32
	targetObjectID uint32
	timestamp      uint64
	profile        zonenpc.ActionProfile
}

type campaignNPCRootedHealSchedule struct {
	runtime        campaignNPCActionRuntime
	packet         raknet.Packet
	sessionKey     string
	generation     uint64
	sourceObjectID uint32
	timestamp      uint64
	profile        zonenpc.ActionProfile
}

func (e campaignNPCRootedHealSchedule) schedule(
	delay time.Duration, produce func() ([][]byte, error),
) error {
	cancel, err := e.packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: delay, Produce: produce,
	}})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		return fmt.Errorf("rootedHealSchedule: %w", err)
	}
	return nil
}

func (e campaignNPCRootedHealSchedule) next() ([][]byte, error) {
	timestamp := e.timestamp + uint64(e.profile.Cooldown/time.Millisecond)
	packets, err := e.runtime.produceZelemShot(
		e.packet, e.sessionKey, e.generation, e.sourceObjectID, timestamp,
	)
	if err != nil {
		e.runtime.releaseAction(e.sessionKey, e.generation, e.sourceObjectID)
		return nil, fmt.Errorf("rootedHealNextAction: %w", err)
	}
	return packets, nil
}

func (e campaignNPCRootedHealSchedule) tick() ([][]byte, error) {
	isDeferred, err := e.runtime.pursuit.deferCommit(
		e.packet, e.sessionKey, e.generation, e.sourceObjectID, e.tick,
	)
	if err != nil {
		return nil, fmt.Errorf("rootedHealDefer: %w", err)
	}
	if isDeferred {
		return nil, nil
	}
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.isCampaignNPCSourceActive(e.generation, e.sourceObjectID) &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	targets := peerSession.zone.NPCs().WoundedOtherSpeciesAllies(
		e.sourceObjectID, e.profile.Radius,
	)
	if len(targets) == 0 {
		e.runtime.registry.mutex.Unlock()
		err := e.schedule(e.profile.Cooldown, e.next)
		if err != nil {
			return nil, fmt.Errorf("rootedHealNext: %w", err)
		}
		return nil, nil
	}
	type healing struct {
		target zonenpc.Snapshot
		amount float32
	}
	healed := make([]healing, 0, len(targets))
	for _, candidate := range targets {
		updated, amount, err := peerSession.zone.NPCs().Heal(
			candidate.Plan.ObjectID, e.profile.MinimumHealing,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			e.runtime.releaseAction(e.sessionKey, e.generation, e.sourceObjectID)
			return nil, fmt.Errorf("rootedHealApply: %w", err)
		}
		if amount > 0 {
			healed = append(healed, healing{target: updated, amount: amount})
		}
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()

	packets := make([][]byte, 0, len(healed)*2)
	for _, applied := range healed {
		deltaPackets, err := npcraknet.HealDelta(
			e.sourceObjectID, applied.target, applied.amount,
		)
		if err != nil {
			e.runtime.logger.Printf(
				"RakNet campaign rooted heal presentation omitted source=%d target=%d: %v",
				e.sourceObjectID, applied.target.Plan.ObjectID, err,
			)
			continue
		}
		packets = append(packets, deltaPackets...)
	}
	nextTick := e
	nextTick.timestamp += uint64(e.profile.TickDuration / time.Millisecond)
	err = nextTick.schedule(e.profile.TickDuration, nextTick.tick)
	if err != nil {
		e.runtime.releaseAction(e.sessionKey, e.generation, e.sourceObjectID)
		return nil, fmt.Errorf("rootedHealTick: %w", err)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) produceVerdanthBasicHealerRootedHeal(
	packet raknet.Packet, sessionKey string, generation uint64,
	source zonenpc.Snapshot, timestamp uint64,
) ([][]byte, bool, error) {
	profile, isProfileFound := zonenpc.VerdanthBasicHealerRootedHealProfile(
		source.Plan.NounName,
	)
	if !isProfileFound {
		return nil, false, nil
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.isCampaignNPCSourceActive(generation, source.Plan.ObjectID) &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil, true, nil
	}
	if peerSession.zone.NPCs().SilenceRemaining(source.Plan.ObjectID, r.now()) > 0 {
		r.registry.mutex.RUnlock()
		return nil, false, nil
	}
	isAllyNearby := peerSession.zone.NPCs().HasOtherSpeciesAlly(
		source.Plan.ObjectID, 12,
	)
	r.registry.mutex.RUnlock()
	if !isAllyNearby {
		return nil, false, nil
	}
	startPackets, err := npcraknet.RootedHealStart(
		source.Plan.ObjectID, profile, timestamp,
	)
	if err != nil {
		return nil, true, fmt.Errorf("rootedHealStartMarshal: %w", err)
	}
	schedule := campaignNPCRootedHealSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: source.Plan.ObjectID,
		timestamp: timestamp, profile: profile,
	}
	err = schedule.schedule(profile.TickDuration, schedule.tick)
	if err != nil {
		r.releaseAction(sessionKey, generation, source.Plan.ObjectID)
		return nil, true, fmt.Errorf("rootedHealStartSchedule: %w", err)
	}
	return startPackets, true, nil
}

func (e campaignNPCHealSchedule) hit() ([][]byte, error) {
	isDeferred, err := e.runtime.pursuit.deferCommit(
		e.packet, e.sessionKey, e.generation, e.sourceObjectID, e.hit,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyHealDefer: %w", err)
	}
	if isDeferred {
		return nil, nil
	}
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.isCampaignNPCSourceActive(e.generation, e.sourceObjectID)
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	target, isTargetFound := peerSession.zone.NPCs().NPC(e.targetObjectID)
	if !isTargetFound || target.IsDefeated || target.HitPoint <= 0 {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	healed, healedAmount, err := peerSession.zone.NPCs().Heal(
		e.targetObjectID, e.profile.MinimumHealing,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		e.runtime.releaseAction(e.sessionKey, e.generation, e.sourceObjectID)
		return nil, fmt.Errorf("enemyHealApply: %w", err)
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	if healedAmount <= 0 {
		return nil, nil
	}
	objectiveErr := peerSession.zone.RecordNPCHeal(
		peerSession.zone.Context(), e.sourceObjectID, e.targetObjectID,
		healedAmount,
	)
	if objectiveErr != nil {
		e.runtime.logger.Printf(
			"RakNet campaign NPC heal objective omitted source=%d target=%d: %v",
			e.sourceObjectID, e.targetObjectID, objectiveErr,
		)
	}
	packets, err := npcraknet.HealHit(
		e.sourceObjectID, healed, healedAmount, e.profile,
	)
	if err != nil {
		e.runtime.logger.Printf(
			"RakNet campaign NPC heal presentation omitted source=%d target=%d: %v",
			e.sourceObjectID, e.targetObjectID, err,
		)
		return nil, nil
	}
	return packets, nil
}

func (e campaignNPCHealSchedule) next() ([][]byte, error) {
	timestamp := e.timestamp + uint64(e.profile.Cooldown/time.Millisecond)
	packets, err := e.runtime.produceEnemyMelee(
		e.packet, e.sessionKey, e.generation, e.sourceObjectID, timestamp,
	)
	if err != nil {
		e.runtime.releaseAction(e.sessionKey, e.generation, e.sourceObjectID)
		return nil, fmt.Errorf("enemyHealNext: %w", err)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) produceVerdanthSpecialTwoHeal(
	packet raknet.Packet, sessionKey string, generation uint64,
	source zonenpc.Snapshot, timestamp uint64,
) ([][]byte, bool, error) {
	profile, isProfileFound := zonenpc.VerdanthSpecialTwoDirectHealProfile(
		source.Plan.NounName,
	)
	if !isProfileFound {
		return nil, false, nil
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.isCampaignNPCSourceActive(generation, source.Plan.ObjectID)
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil, true, nil
	}
	if peerSession.zone.NPCs().SilenceRemaining(source.Plan.ObjectID, r.now()) > 0 {
		r.registry.mutex.RUnlock()
		return nil, false, nil
	}
	target, isTargetFound := peerSession.zone.NPCs().FirstLowHealthAlly(
		source.Plan.Position, profile.Range, 0.5,
	)
	r.registry.mutex.RUnlock()
	if !isTargetFound {
		return nil, false, nil
	}
	castPackets, err := npcraknet.HealCast(
		source.Plan.ObjectID, target, profile, timestamp,
	)
	if err != nil {
		return nil, true, fmt.Errorf("enemyHealCastMarshal: %w", err)
	}
	schedule := campaignNPCHealSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: source.Plan.ObjectID,
		targetObjectID: target.Plan.ObjectID, timestamp: timestamp,
		profile: profile,
	}
	cancel, scheduleErr := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{
		{Delay: profile.HitDelay, Produce: schedule.hit},
		{Delay: profile.Cooldown, Produce: schedule.next},
	})
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		r.releaseAction(sessionKey, generation, source.Plan.ObjectID)
		return nil, true, fmt.Errorf("enemyHealSchedule: %w", scheduleErr)
	}
	return castPackets, true, nil
}
