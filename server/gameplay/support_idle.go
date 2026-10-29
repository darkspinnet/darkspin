package gameplay

import (
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// Build-103 behavior chunks517/861 own this idle instance until an ability
// interrupts it. Fatigue is removed when that instance ends.
type campaignNPCSupportIdle struct {
	registry     *gameplaySessionRegistry
	phase        uint8
	nextAt       time.Time
	pauseUntil   time.Time
	movementBuff float32
	isTired      bool
}

type campaignNPCSupportIdleResume struct {
	idle             *campaignNPCSupportIdle
	runtime          campaignNPCActionRuntime
	packet           raknet.Packet
	sessionKey       string
	generation       uint64
	objectID         uint32
	actionGeneration uint64
}

func (e campaignNPCSupportIdleResume) produce(timestamp uint64) ([][]byte, error) {
	if e.idle == nil {
		return nil, nil
	}
	e.runtime.registry.mutex.Lock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation || member.npcSupportIdles[e.objectID] != e.idle {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if !member.isCampaignNPCSourceGenerationActive(e.generation, e.objectID, e.actionGeneration) {
		member.clearNPCSupportIdles(e.objectID)
		e.runtime.registry.sessions[e.sessionKey] = member
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	source, isSourceFound := member.zone.NPCs().NPC(e.objectID)
	if !isSourceFound {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	now := e.runtime.now()
	movementBuff := (1+zonenpc.EffectiveMovementSpeedBuff(source.Plan))*member.zone.NPCs().SlowMovementScale(e.objectID, now) - 1
	if movementBuff > -.5 {
		previousBuff := e.idle.movementBuff
		e.idle.advance(now, strings.HasPrefix(strings.ToLower(source.Plan.NounName), "nomadscope"))
		if previousBuff != e.idle.movementBuff {
			updatedSource, isSourceUpdated, updateErr := member.zone.NPCs().SetSupportIdleMovementBuff(e.objectID, e.idle.movementBuff)
			if updateErr != nil {
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("supportArrivalFatigue: %w", updateErr)
			}
			if isSourceUpdated {
				slowScale := member.zone.NPCs().SlowMovementScale(e.objectID, now)
				attribute, attributeErr := npcSupportMovementAttribute(e.objectID, (1+zonenpc.EffectiveMovementSpeedBuff(updatedSource.Plan))*slowScale-1)
				if attributeErr != nil {
					e.runtime.registry.mutex.Unlock()
					return nil, fmt.Errorf("supportArrivalAttribute: %w", attributeErr)
				}
				e.idle.queuePacketsLocked(&member, [][]byte{attribute})
			}
		}
	}
	e.runtime.registry.sessions[e.sessionKey] = member
	e.runtime.registry.mutex.Unlock()
	step := campaignNPCFirstActionStep{runtime: e.runtime, packet: e.packet,
		sessionKey: e.sessionKey, generation: e.generation, objectID: e.objectID,
		actionGeneration: e.actionGeneration, timestamp: timestamp}
	packets, err := step.produce()
	if err != nil {
		return nil, fmt.Errorf("supportIdleResume: %w", err)
	}
	return packets, nil
}

func (e *campaignNPCSupportIdle) advance(now time.Time, isFacing bool) {
	if !now.After(e.nextAt) || now.Before(e.pauseUntil) {
		return
	}
	delay := 3 * time.Second
	firstLength := 500 * time.Millisecond
	firstBuff, secondBuff := float32(-.2), float32(-.4)
	if isFacing {
		delay, firstLength = time.Second, 2*time.Second
		firstBuff, secondBuff = -.15, -.3
	}
	switch e.phase {
	case 0:
		e.movementBuff, e.phase, e.nextAt = firstBuff, 1, now.Add(firstLength)
		e.isTired = true
	case 1:
		e.movementBuff, e.phase, e.nextAt = secondBuff, 2, now.Add(500*time.Millisecond)
	case 2:
		e.movementBuff, e.phase, e.nextAt = 0, 0, now.Add(delay)
		e.pauseUntil = now.Add(2 * time.Second)
	}
}

func (e campaignNPCActionRuntime) produceNPCSupportIdle(packet raknet.Packet,
	sessionKey string, generation uint64, source zonenpc.Snapshot, timestamp uint64,
) ([][]byte, error) {
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || !member.isCampaignNPCSourceGenerationActive(generation, source.Plan.ObjectID, source.ActionGeneration) {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	target, isTargetFound := member.campaignNPCTarget(generation, source.TargetObjectID)
	profile, isProfileFound := zonenpc.ActionProfileForPlan(source.Plan)
	isFacing := strings.HasPrefix(strings.ToLower(source.Plan.NounName), "nomadscope")
	delay := 500 * time.Millisecond
	packets := make([][]byte, 0, 3)
	resume := campaignNPCSupportIdleResume{runtime: e, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: source.Plan.ObjectID, actionGeneration: source.ActionGeneration}
	if member.npcSupportIdles == nil {
		member.npcSupportIdles = make(map[uint32]*campaignNPCSupportIdle)
	}
	state := member.npcSupportIdles[source.Plan.ObjectID]
	if state == nil {
		fatigueDelay := 3 * time.Second
		if isFacing {
			fatigueDelay = time.Second
		}
		state = &campaignNPCSupportIdle{registry: e.registry, nextAt: e.now().Add(fatigueDelay)}
		member.npcSupportIdles[source.Plan.ObjectID] = state
	}
	resume.idle = state
	e.registry.sessions[sessionKey] = member
	if e.now().Before(state.pauseUntil) {
		delay = min(delay, state.pauseUntil.Sub(e.now()))
		e.registry.mutex.Unlock()
		waitPackets, waitErr := scheduleNPCSupportWait(e.registry, packet, resume, timestamp, delay)
		if waitErr != nil {
			return nil, fmt.Errorf("supportPauseWait: %w", waitErr)
		}
		return waitPackets, nil
	}
	isMovable := isTargetFound && isProfileFound && member.isCampaignNPCActionActiveAt(generation, source.Plan.ObjectID, e.now()) &&
		member.zone.NPCs().RootRemaining(source.Plan.ObjectID, e.now()) == 0
	if !isMovable {
		e.registry.mutex.Unlock()
		waitPackets, waitErr := scheduleNPCSupportWait(e.registry, packet, resume, timestamp, delay)
		if waitErr != nil {
			return nil, fmt.Errorf("supportInactiveWait: %w", waitErr)
		}
		return waitPackets, nil
	}
	safeDistance := float32(14)
	if isFacing {
		safeDistance = 10
	}
	if source.Plan.Position.Sub(target.Position).Length() >= safeDistance {
		e.registry.mutex.Unlock()
		if isFacing {
			turnPlan := zonenpc.AttackPlan{SourceObjectID: source.Plan.ObjectID, TargetObjectID: target.ObjectID,
				ActionGeneration: source.ActionGeneration, SourcePosition: source.Plan.Position, TargetPosition: target.Position}
			if member.zone.NPCs().CommitFacing(turnPlan) {
				turnPackets, turnErr := npcraknet.FaceTarget(turnPlan, true)
				if turnErr != nil {
					return nil, fmt.Errorf("supportIdleFace: %w", turnErr)
				}
				packets = append(packets, turnPackets...)
			}
		}
		waitPackets, waitErr := scheduleNPCSupportWait(e.registry, packet, resume, timestamp, delay)
		if waitErr != nil {
			return nil, fmt.Errorf("supportSafeWait: %w", waitErr)
		}
		return append(packets, waitPackets...), nil
	}
	minimumDistance, maximumDistance := float32(5), float32(10)
	if state.isTired {
		minimumDistance, maximumDistance = 7, 8
	}
	destination, isDestinationFound, err := supportRetreatDestination(member, source, target.Position, minimumDistance, maximumDistance)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("supportRetreat: %w", err)
	}
	if !isDestinationFound || destination.Sub(source.Plan.Position).Length() < source.Plan.ActorFootprintRadius() {
		e.registry.sessions[sessionKey] = member
		e.registry.mutex.Unlock()
		waitPackets, waitErr := scheduleNPCSupportWait(e.registry, packet, resume, timestamp, delay)
		if waitErr != nil {
			return nil, fmt.Errorf("supportBlockedWait: %w", waitErr)
		}
		return append(packets, waitPackets...), nil
	}
	profile.AbilityName = "SupportFlee"
	if isFacing {
		profile.AbilityName = "SupportFleeFacing"
	}
	profile.Range = 1.5 * source.Plan.ActorFootprintRadius()
	e.registry.sessions[sessionKey] = member
	e.registry.mutex.Unlock()
	action := zonenpc.FirstActionPlan{ObjectID: source.Plan.ObjectID, TargetObjectID: target.ObjectID,
		ActionGeneration: source.ActionGeneration, SourcePosition: source.Plan.Position, TargetPosition: destination,
		Profile: profile, IsPursuitNeeded: true}
	movementPackets, err := npcraknet.Pursuit(action)
	if isFacing {
		movementPackets, err = npcraknet.BoundedStrafe(source.Plan.ObjectID, source.Plan.Position, destination,
			target.ObjectID, target.Position, profile.Range)
	}
	if err != nil {
		return nil, fmt.Errorf("supportRetreatMarshal: %w", err)
	}
	err = e.pursuit.schedule(packet, sessionKey, generation, source.Plan.ObjectID, timestamp, destination, profile, resume.produce)
	if err != nil {
		return nil, fmt.Errorf("supportRetreatSchedule: %w", err)
	}
	return append(packets, movementPackets...), nil
}

func npcSupportMovementAttribute(objectID uint32, movementBuff float32) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: objectID, Value: map[uint8]float32{48: movementBuff}})
	if err != nil {
		return nil, fmt.Errorf("supportMovementMarshal: %w", err)
	}
	return packet, nil
}

// The registry lock owns idle state; the NPC plan owns its live attribute so
// every spawn/replay and movement consumer sees the same additive contribution.
func (e *gameplayPeerSession) clearNPCSupportIdles(sourceObjectID uint32) {
	for objectID, state := range e.npcSupportIdles {
		if sourceObjectID != 0 && sourceObjectID != objectID {
			continue
		}
		delete(e.npcSupportIdles, objectID)
		if e.zone == nil {
			continue
		}
		previous, isFound := e.zone.NPCs().NPC(objectID)
		e.zone.NPCs().ClearSupportIdleMovementBuff(objectID)
		if !isFound || previous.Plan.SupportIdleMovementBuff == 0 {
			continue
		}
		previous.Plan.SupportIdleMovementBuff = 0
		slowScale := e.zone.NPCs().SlowMovementScale(objectID, time.Now())
		movementBuff := (1+zonenpc.EffectiveMovementSpeedBuff(previous.Plan))*slowScale - 1
		packet, err := npcSupportMovementAttribute(objectID, movementBuff)
		if err != nil {
			log.Printf("RakNet support idle movement reset failed object=%d: %v", objectID, err)
			continue
		}
		packets := [][]byte{packet}
		if state == nil || state.registry == nil {
			e.queueCampaignPackets(packets)
			log.Printf("RakNet support idle observer reset unavailable object=%d", objectID)
			continue
		}
		state.queuePacketsLocked(e, packets)
	}
}

func (e *campaignNPCSupportIdle) queuePacketsLocked(owner *gameplayPeerSession, packets [][]byte) {
	owner.queueCampaignPackets(packets)
	for sessionKey, member := range e.registry.sessions {
		if member.zone != owner.zone || member.isZoneTerminal() ||
			(member.binding.UserID == owner.binding.UserID && member.generation == owner.generation) {
			continue
		}
		member.queueCampaignPackets(packets)
		e.registry.sessions[sessionKey] = member
	}
}

func scheduleNPCSupportWait(registry *gameplaySessionRegistry, packet raknet.Packet,
	resume campaignNPCSupportIdleResume, timestamp uint64, delay time.Duration,
) ([][]byte, error) {
	step := campaignNPCFirstActionStep{runtime: resume.runtime, packet: packet, sessionKey: resume.sessionKey,
		generation: resume.generation, objectID: resume.objectID, actionGeneration: resume.actionGeneration,
		timestamp: timestamp + uint64(delay/time.Millisecond)}
	err := scheduleNPCProducer(registry, packet, delay, step.produce)
	if err != nil {
		return nil, fmt.Errorf("supportWaitSchedule: %w", err)
	}
	return nil, nil
}

func supportRetreatDestination(member gameplayPeerSession, source zonenpc.Snapshot,
	target game.Vec3, minimumDistance, maximumDistance float32,
) (game.Vec3, bool, error) {
	if member.zone.NPCRandom() == nil {
		return game.Vec3{}, false, nil
	}
	delta := source.Plan.Position.Sub(target)
	angle := math.Atan2(float64(delta.Y), float64(delta.X))
	span := math.Pi / 4
	for range 5 {
		heading := angle + (member.zone.NPCRandom().Float64()*2-1)*span
		distance := minimumDistance + float32(member.zone.NPCRandom().Float64())*(maximumDistance-minimumDistance)
		candidate := game.Vec3{X: source.Plan.Position.X + float32(math.Cos(heading))*distance,
			Y: source.Plan.Position.Y + float32(math.Sin(heading))*distance, Z: source.Plan.Position.Z}
		destination, isFound, err := zoneaction.NPCDirectMovementDestination(member.zone.Navigation(), source.Plan.Position,
			candidate, source.NavigationRadius(), source.Navigation)
		if err != nil {
			return game.Vec3{}, false, fmt.Errorf("supportRetreatProject: %w", err)
		}
		if isFound {
			return destination, true, nil
		}
		span += math.Pi / 2
	}
	return game.Vec3{}, false, nil
}
