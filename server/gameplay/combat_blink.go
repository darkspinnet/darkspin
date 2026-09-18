package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const (
	scaldronBlinkCloseRange  = 4
	scaldronBlinkOutDuration = 400 * time.Millisecond
)

type campaignScaldronBlinkTeleportStep struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
	isNear     bool
}

func (e campaignScaldronBlinkTeleportStep) fail(
	step string, err error,
) ([][]byte, error) {
	e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
	return nil, fmt.Errorf("%s: %w", step, err)
}

func (e campaignScaldronBlinkTeleportStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		e.generation, e.objectID,
	)
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if peerSession.zone.NPCRandom() == nil {
		e.runtime.registry.mutex.Unlock()
		return e.fail("scaldronBlinkRandom", errors.New("random unavailable"))
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(e.objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		e.generation, enemy.TargetObjectID,
	)
	profile, isProfileFound := zonenpc.ActionProfileForNoun(enemy.Plan.NounName)
	if !isEnemyFound || !isTargetFound || !isProfileFound ||
		profile.AbilityName != "ScaldronBasicBlink_AttackBlink" {
		e.runtime.registry.mutex.Unlock()
		e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
		return nil, nil
	}
	minimumDistance := profile.TeleportMinimumDistance
	normalDistance := profile.TeleportNormalDistance
	maximumDistance := profile.TeleportMaximumDistance
	if e.isNear {
		minimumDistance = 3
		normalDistance = 3.5
		maximumDistance = scaldronBlinkCloseRange
	}
	destination, isDestinationFound, err :=
		zonenavigation.RandomTeleportDestination(
			peerSession.zone.Navigation(), peerSession.zone.NPCRandom(),
			zonenavigation.RandomTeleportRequest{
				SourcePosition:  target.Position,
				FootprintRadius: enemy.Plan.NPCProfile.FootprintRadius,
				MinimumDistance: minimumDistance,
				NormalDistance:  normalDistance,
				MaximumDistance: maximumDistance,
			},
		)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return e.fail("scaldronBlinkDestination", err)
	}
	if !isDestinationFound {
		e.runtime.registry.mutex.Unlock()
		e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
		return nil, nil
	}
	err = peerSession.zone.NPCs().SetPosition(e.objectID, destination)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return e.fail("scaldronBlinkPosition", err)
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	plan := zonenpc.AttackPlan{
		SourceObjectID: e.objectID, TargetObjectID: target.ObjectID,
		SourcePosition: enemy.Plan.Position, TargetPosition: target.Position,
		Profile: profile,
	}
	packets, err := npcraknet.Blink(plan, destination, e.timestamp)
	if err != nil {
		return e.fail("scaldronBlinkTeleport", err)
	}
	if !e.isNear {
		return packets, nil
	}
	resume := campaignScaldronBlinkResumeStep{
		runtime: e.runtime, packet: e.packet, sessionKey: e.sessionKey,
		generation: e.generation, objectID: e.objectID,
		timestamp: e.timestamp + uint64(profile.TeleportAnimationDelay/time.Millisecond),
	}
	_, scheduleErr := scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{{
		Delay: profile.TeleportAnimationDelay, Produce: resume.produce,
	}})
	if scheduleErr != nil {
		return e.fail("scaldronBlinkResumeSchedule", scheduleErr)
	}
	return packets, nil
}

type campaignScaldronBlinkResumeStep struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
}

func (e campaignScaldronBlinkResumeStep) produce() ([][]byte, error) {
	packets, err := e.runtime.produceEnemyMelee(
		e.packet, e.sessionKey, e.generation, e.objectID, e.timestamp,
	)
	if err != nil {
		e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
		return nil, fmt.Errorf("scaldronBlinkResume: %w", err)
	}
	return packets, nil
}

type campaignScaldronBlinkAwayStep struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
}

func (e campaignScaldronBlinkAwayStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		e.generation, e.objectID,
	)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	animationPacket, err := npcraknet.AnimationState(
		e.objectID, "sca_minn_sp_02_blink_out", e.timestamp,
	)
	if err != nil {
		e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
		return nil, fmt.Errorf("scaldronBlinkOut: %w", err)
	}
	teleport := campaignScaldronBlinkTeleportStep{
		runtime: e.runtime, packet: e.packet, sessionKey: e.sessionKey,
		generation: e.generation, objectID: e.objectID,
		timestamp: e.timestamp + uint64(scaldronBlinkOutDuration/time.Millisecond),
	}
	_, scheduleErr := scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{{
		Delay: scaldronBlinkOutDuration, Produce: teleport.produce,
	}})
	if scheduleErr != nil {
		e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
		return nil, fmt.Errorf("scaldronBlinkAwaySchedule: %w", scheduleErr)
	}
	return [][]byte{animationPacket}, nil
}

func (r campaignNPCActionRuntime) produceScaldronBasicBlink(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		generation, objectID,
	)
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, enemy.TargetObjectID,
	)
	profile, isProfileFound := zonenpc.ActionProfileForNoun(enemy.Plan.NounName)
	r.registry.mutex.RUnlock()
	if !isEnemyFound || !isTargetFound || !isProfileFound ||
		profile.AbilityName != "ScaldronBasicBlink_AttackBlink" {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	distance := zoneability.Distance(enemy.Plan.Position, target.Position)
	if distance > scaldronBlinkCloseRange {
		return r.startScaldronBlinkNearTeleport(
			packet, sessionKey, generation, objectID, timestamp,
		)
	}
	plan, err := zonenpc.PlanAttackWithProfile(
		enemy, target.ObjectID, target.Position, profile, target.FootprintRadius,
	)
	if err != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, fmt.Errorf("scaldronBlinkAttackPlan: %w", err)
	}
	startPackets, err := r.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, fmt.Errorf("scaldronBlinkAttackStart: %w", err)
	}
	request := campaignNPCAttackRequest{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
		kind: campaignNPCAttackMelee,
	}
	attack := campaignNPCAttackSchedule{request: request, plan: plan}
	away := campaignScaldronBlinkAwayStep{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID,
		timestamp: timestamp + uint64(profile.ReleaseDelay/time.Millisecond),
	}
	_, scheduleErr := scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{
		{Delay: profile.HitDelay, Produce: attack.hit},
		{Delay: profile.ReleaseDelay, Produce: away.produce},
		{Delay: profile.Cooldown, Produce: attack.next},
	})
	if scheduleErr != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, fmt.Errorf("scaldronBlinkAttackSchedule: %w", scheduleErr)
	}
	return startPackets, nil
}

func (r campaignNPCActionRuntime) startScaldronBlinkNearTeleport(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	if sessionKey == "" || generation == 0 || objectID == 0 {
		return nil, errors.New("scaldron blink request invalid")
	}
	animationPacket, err := npcraknet.AnimationState(
		objectID, "sca_minn_sp_02_blink_out", timestamp,
	)
	if err != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, fmt.Errorf("scaldronBlinkNearOut: %w", err)
	}
	teleport := campaignScaldronBlinkTeleportStep{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID,
		timestamp: timestamp + uint64(scaldronBlinkOutDuration/time.Millisecond),
		isNear:    true,
	}
	_, scheduleErr := scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{{
		Delay: scaldronBlinkOutDuration, Produce: teleport.produce,
	}})
	if scheduleErr != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, fmt.Errorf("scaldronBlinkNearSchedule: %w", scheduleErr)
	}
	return [][]byte{animationPacket}, nil
}
