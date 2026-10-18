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
	scaldronBlinkInDuration  = 250 * time.Millisecond
	scaldronBlinkLastHit     = scaldronBlinkInDuration + 1066*time.Millisecond
	scaldronBlinkRest        = 400 * time.Millisecond
)

// These are consecutive hits, not ranked timings. AttackBlink adds its
// timetoblinkin to each melee-template hit (Abilities/0x923BCCB3.lua).
var scaldronBlinkHitDelays = [...]time.Duration{
	133 * time.Millisecond, 266 * time.Millisecond, 400 * time.Millisecond,
	533 * time.Millisecond, 666 * time.Millisecond, 800 * time.Millisecond,
	933 * time.Millisecond, 1066 * time.Millisecond,
}

type campaignScaldronBlinkHitStep struct {
	attack campaignNPCAttackSchedule
}

func (e campaignScaldronBlinkHitStep) produce() ([][]byte, error) {
	req := e.attack.request
	req.runtime.registry.mutex.RLock()
	peerSession, isFound := req.runtime.registry.sessions[req.sessionKey]
	if !isFound || !peerSession.isCampaignNPCSourceGenerationActive(
		req.generation, req.objectID, e.attack.plan.ActionGeneration,
	) {
		req.runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(req.objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(req.generation, e.attack.plan.TargetObjectID)
	req.runtime.registry.mutex.RUnlock()
	if !isEnemyFound || !isTargetFound {
		return nil, nil
	}
	// The inherited melee template makes a 90-degree arc with hitArcLength=3.
	arc, err := zoneability.NewMeleeArc(enemy.Plan.Position, enemy.Facing,
		3+enemy.Plan.ActorFootprintRadius(), 90)
	if err != nil {
		return nil, fmt.Errorf("blinkHitArc: %w", err)
	}
	if !arc.Contains(target.Position, target.ActorFootprintRadius) {
		return nil, nil
	}
	packets, err := e.attack.hit()
	if err != nil {
		return nil, fmt.Errorf("blinkMeleeHit: %w", err)
	}
	return packets, nil
}

type campaignScaldronBlinkRestStep struct {
	attack campaignNPCAttackSchedule
}

func (e campaignScaldronBlinkRestStep) produce() ([][]byte, error) {
	req := e.attack.request
	req.runtime.registry.mutex.RLock()
	peerSession, isFound := req.runtime.registry.sessions[req.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceGenerationActive(
		req.generation, req.objectID, e.attack.plan.ActionGeneration,
	)
	req.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packet, err := npcraknet.AnimationState(req.objectID, e.attack.plan.Profile.EndAnimationName,
		req.timestamp+uint64(scaldronBlinkLastHit/time.Millisecond))
	if err != nil {
		return nil, fmt.Errorf("blinkRestAnimation: %w", err)
	}
	return [][]byte{packet}, nil
}

type campaignScaldronBlinkTeleportStep struct {
	runtime          campaignNPCActionRuntime
	packet           raknet.Packet
	sessionKey       string
	generation       uint64
	objectID         uint32
	actionGeneration uint64
	timestamp        uint64
	isNear           bool
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
	isCurrent := isFound && peerSession.isCampaignNPCSourceGenerationActive(
		e.generation, e.objectID, e.actionGeneration,
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
		actionGeneration: e.actionGeneration,
		timestamp:        e.timestamp + uint64(profile.TeleportAnimationDelay/time.Millisecond),
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
	runtime          campaignNPCActionRuntime
	packet           raknet.Packet
	sessionKey       string
	generation       uint64
	objectID         uint32
	actionGeneration uint64
	timestamp        uint64
}

func (e campaignScaldronBlinkResumeStep) produce() ([][]byte, error) {
	packets, err := e.runtime.produceScaldronBlinkBurst(
		e.packet, e.sessionKey, e.generation, e.objectID, e.actionGeneration, e.timestamp,
	)
	if err != nil {
		e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
		return nil, fmt.Errorf("scaldronBlinkResume: %w", err)
	}
	return packets, nil
}

type campaignScaldronBlinkAwayStep struct {
	runtime          campaignNPCActionRuntime
	packet           raknet.Packet
	sessionKey       string
	generation       uint64
	objectID         uint32
	actionGeneration uint64
	timestamp        uint64
}

func (e campaignScaldronBlinkAwayStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceGenerationActive(
		e.generation, e.objectID, e.actionGeneration,
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
		actionGeneration: e.actionGeneration,
		timestamp:        e.timestamp + uint64(scaldronBlinkOutDuration/time.Millisecond),
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
	return r.produceScaldronBlinkBurst(packet, sessionKey, generation, objectID,
		enemy.ActionGeneration, timestamp)
}

func (r campaignNPCActionRuntime) produceScaldronBlinkBurst(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, actionGeneration uint64, timestamp uint64,
) ([][]byte, error) {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || !peerSession.isCampaignNPCSourceGenerationActive(
		generation, objectID, actionGeneration,
	) {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(generation, enemy.TargetObjectID)
	profile, isProfileFound := zonenpc.ActionProfileForNoun(enemy.Plan.NounName)
	r.registry.mutex.RUnlock()
	if !isEnemyFound || !isTargetFound || !isProfileFound {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	// The 15/18/21 range admits the blink ability, not its melee hits.
	// Resume the accepted burst directly after arrival, even if the target has
	// moved away; the client does not recursively blink again before tick().
	profile.Range = 3
	profile.HitDelay = scaldronBlinkInDuration + scaldronBlinkHitDelays[0]
	profile.ReleaseDelay = scaldronBlinkLastHit + scaldronBlinkRest
	plan, err := zonenpc.PlanAreaAttackWithProfile(
		enemy, target.ObjectID, target.Position, profile,
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
		actionGeneration: actionGeneration,
		timestamp:        timestamp + uint64(profile.ReleaseDelay/time.Millisecond),
	}
	producers := make([]raknet.ScheduledPacketProducer, 0, len(scaldronBlinkHitDelays)+3)
	for _, hitDelay := range scaldronBlinkHitDelays {
		delay := scaldronBlinkInDuration + hitDelay
		hit := campaignScaldronBlinkHitStep{attack: attack}
		hit.attack.request.timestamp += uint64(delay / time.Millisecond)
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: delay, Produce: hit.produce,
		})
	}
	rest := campaignScaldronBlinkRestStep{attack: attack}
	producers = append(producers,
		raknet.ScheduledPacketProducer{Delay: scaldronBlinkLastHit, Produce: rest.produce},
		raknet.ScheduledPacketProducer{Delay: profile.ReleaseDelay, Produce: away.produce},
		raknet.ScheduledPacketProducer{Delay: profile.Cooldown, Produce: attack.next},
	)
	_, scheduleErr := scheduleNPCProducers(r.registry, packet, producers)
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
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || !peerSession.isCampaignNPCSourceActive(generation, objectID) {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	enemy, isEnemyFound := peerSession.zone.NPCs().NPC(objectID)
	r.registry.mutex.RUnlock()
	if !isEnemyFound {
		return nil, nil
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
		actionGeneration: enemy.ActionGeneration,
		timestamp:        timestamp + uint64(scaldronBlinkOutDuration/time.Millisecond),
		isNear:           true,
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
